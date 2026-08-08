package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/3270io/3270Connect/internal/runstore"
	"github.com/3270io/3270Connect/internal/workflow"
)

// Tier is how much a tool is trusted to do without being asked for.
//
// The default is the most restrictive of the two products' defaults, because
// this one's mutating tools do not act on a host — they load it. A
// misconfigured client that can read a screen is a nuisance; one that can put
// five thousand virtual users on a production LPAR is not.
type Tier int

const (
	// TierRead describes, validates and reports. It never opens a session.
	TierRead Tier = iota
	// TierSmoke adds single-session operations: run a workflow once, probe a
	// host, start a bundled sample app.
	TierSmoke
	// TierLoad adds concurrent runs.
	TierLoad
)

func (t Tier) String() string {
	switch t {
	case TierSmoke:
		return "smoke"
	case TierLoad:
		return "load"
	default:
		return "readonly"
	}
}

// ParseTier maps the MCP_TOOLS setting onto a tier, reporting an
// unrecognised value rather than silently choosing one.
func ParseTier(s string) (Tier, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "readonly", "read", "read-only":
		return TierRead, true
	case "smoke", "single":
		return TierSmoke, true
	case "load", "full", "all":
		return TierLoad, true
	}
	return TierRead, false
}

// resolveTier reads the tier from the flag, then the environment.
func resolveTier(flagValue string) Tier {
	raw := strings.TrimSpace(flagValue)
	if raw == "" {
		raw = os.Getenv("MCP_TOOLS")
	}
	tier, ok := ParseTier(raw)
	if !ok {
		logMCP("MCP_TOOLS=%q is not a tier I recognise (readonly, smoke, load); using %s", raw, tier)
	}
	return tier
}

// Caps that apply whatever the tier.
//
// "Run a big load test" is a reasonable thing to say and a bad thing to
// interpret generously. A cap that refuses with a number is better than a run
// at five thousand users nobody sanctioned — and better than quietly running
// something smaller and reporting it as what was asked for.
const (
	defaultMaxConcurrent = 50
	defaultMaxRuntimeSec = 300
)

func envInt(name string, fallback int) int {
	if raw := strings.TrimSpace(os.Getenv(name)); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return n
		}
	}
	return fallback
}

func maxConcurrent() int { return envInt("MCP_MAX_CONCURRENT", defaultMaxConcurrent) }
func maxRuntimeSec() int { return envInt("MCP_MAX_RUNTIME_SEC", defaultMaxRuntimeSec) }

// hostAllowed reports whether a host may be targeted. With MCP_ALLOWED_HOSTS
// unset every host is allowed, which is the right default for a tool run
// against a lab; setting it is how a deployment fences off production.
func hostAllowed(host string) bool {
	patterns := strings.TrimSpace(os.Getenv("MCP_ALLOWED_HOSTS"))
	if patterns == "" {
		return true
	}
	host = strings.ToLower(strings.TrimSpace(host))
	for _, pattern := range strings.Split(patterns, ",") {
		pattern = strings.ToLower(strings.TrimSpace(pattern))
		if pattern == "" {
			continue
		}
		if ok, _ := filepath.Match(pattern, host); ok {
			return true
		}
	}
	return false
}

// MCPTool is one entry in the catalogue.
type MCPTool struct {
	Name        string
	Description string
	InputSchema map[string]any
	Tier        Tier
	Destructive bool
	OpenWorld   bool
	Handle      func(ctx context.Context, args map[string]any) (string, error)
}

// ReadOnly reports whether the tool only observes.
func (t MCPTool) ReadOnly() bool { return t.Tier == TierRead && !t.Destructive }

func objectSchema(props map[string]any, required ...string) map[string]any {
	if props == nil {
		props = map[string]any{}
	}
	schema := map[string]any{
		"type":                 "object",
		"properties":           props,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

// allTools returns the catalogue, sorted by name.
func allTools() []MCPTool {
	tools := []MCPTool{
		// --- Authoring -------------------------------------------------
		{
			Name: "describe_workflow_schema",
			Description: "Return the JSON Schema for a workflow document, with the step types and what each needs. " +
				"Call this before writing a workflow: the step keys are Type, Coordinates and Text — not Action, Value " +
				"or top-level Row/Column, a shape that appeared in this project's own notes and never ran. Coordinates are 1-based.",
			Tier:        TierRead,
			InputSchema: objectSchema(nil),
			Handle: func(context.Context, map[string]any) (string, error) {
				return toJSON(workflow.Schema())
			},
		},
		{
			Name: "validate_workflow",
			Description: "Check a workflow document against the same rules the runner applies, and report what is wrong. " +
				"Use it in a loop while composing one — it costs nothing and catches a malformed step before it fails on every virtual user at once.",
			Tier: TierRead,
			InputSchema: objectSchema(map[string]any{
				"workflow": map[string]any{"type": "object", "description": "The workflow document to check."},
			}, "workflow"),
			Handle: func(_ context.Context, args map[string]any) (string, error) {
				cfg, err := workflowFromArgs(args)
				if err != nil {
					return "", err
				}
				if err := workflow.Validate(cfg); err != nil {
					return toJSON(map[string]any{"valid": false, "error": err.Error()})
				}
				return toJSON(map[string]any{"valid": true, "steps": len(cfg.Steps)})
			},
		},
		{
			Name: "save_workflow",
			Description: "Write a validated workflow to a file so a run can use it. Refuses to write an invalid one, " +
				"because a file that cannot run is worse than no file — it looks finished.",
			Tier: TierSmoke,
			InputSchema: objectSchema(map[string]any{
				"workflow": map[string]any{"type": "object", "description": "The workflow document."},
				"path":     map[string]any{"type": "string", "description": "Where to write it, e.g. \"workflow.json\"."},
			}, "workflow", "path"),
			Handle: func(_ context.Context, args map[string]any) (string, error) {
				cfg, err := workflowFromArgs(args)
				if err != nil {
					return "", err
				}
				if err := workflow.Validate(cfg); err != nil {
					return "", fmt.Errorf("not saved, the workflow is not valid: %w", err)
				}
				path := stringArg(args, "path")
				if path == "" {
					return "", fmt.Errorf("path is required")
				}
				data, err := json.MarshalIndent(cfg, "", "  ")
				if err != nil {
					return "", err
				}
				if err := os.WriteFile(path, data, 0o644); err != nil {
					return "", fmt.Errorf("could not write %s: %w", path, err)
				}
				abs, _ := filepath.Abs(path)
				return fmt.Sprintf("Wrote %s (%d steps).", abs, len(cfg.Steps)), nil
			},
		},

		// --- Probing ---------------------------------------------------
		{
			Name:        "test_connection",
			Description: "Check that a TN3270 host is reachable and accepting connections. Do this before a load test rather than discovering it a hundred workers at a time.",
			Tier:        TierRead,
			OpenWorld:   true,
			InputSchema: objectSchema(map[string]any{
				"host": map[string]any{"type": "string", "description": "Hostname or IP address."},
				"port": map[string]any{"type": "integer", "minimum": 1, "maximum": 65535},
			}, "host", "port"),
			Handle: func(_ context.Context, args map[string]any) (string, error) {
				host := stringArg(args, "host")
				port := intArg(args, "port")
				if !hostAllowed(host) {
					return "", fmt.Errorf("%s is not in MCP_ALLOWED_HOSTS", host)
				}
				if err := dialHost(host, port); err != nil {
					return toJSON(map[string]any{"reachable": false, "error": err.Error()})
				}
				return toJSON(map[string]any{"reachable": true, "host": host, "port": port})
			},
		},
		{
			Name: "run_workflow_once",
			Description: "Run a workflow to completion with a single session and return the captured screens. " +
				"This is the smoke test: if one pass does not work, a hundred concurrent ones only produce a hundred copies of the same failure.",
			Tier:        TierSmoke,
			Destructive: true,
			OpenWorld:   true,
			InputSchema: objectSchema(map[string]any{
				"workflow": map[string]any{"type": "object", "description": "The workflow document to run."},
			}, "workflow"),
			Handle: func(_ context.Context, args map[string]any) (string, error) {
				cfg, err := workflowFromArgs(args)
				if err != nil {
					return "", err
				}
				if !hostAllowed(cfg.Host) {
					return "", fmt.Errorf("%s is not in MCP_ALLOWED_HOSTS", cfg.Host)
				}
				output, err := executeWorkflowOnce(cfg)
				if err != nil {
					return "", err
				}
				return output, nil
			},
		},

		// --- Load ------------------------------------------------------
		{
			Name: "start_load_test",
			Description: "Start a concurrent load test as a background process and return its pid. " +
				"Ramp up rather than starting at the target: a first run at full concurrency against an unfamiliar host tells you it broke, at a level you have no comparison for. " +
				"Poll get_load_test_metrics while it runs, and stop_load_test when the question is answered.",
			Tier:        TierLoad,
			Destructive: true,
			OpenWorld:   true,
			InputSchema: objectSchema(map[string]any{
				"config_path":       map[string]any{"type": "string", "description": "Path to a workflow file, e.g. one written by save_workflow."},
				"concurrent":        map[string]any{"type": "integer", "minimum": 1, "description": "Virtual users. Capped by MCP_MAX_CONCURRENT."},
				"runtime_sec":       map[string]any{"type": "integer", "minimum": 1, "description": "How long to hold the load. Capped by MCP_MAX_RUNTIME_SEC. Under 60s the run is mostly ramp-up."},
				"injection_path":    map[string]any{"type": "string", "description": "Optional injection file: one entry per virtual user, so they do not share a login."},
				"start_port":        map[string]any{"type": "integer", "minimum": 1024, "description": "Base script port (default 5000)."},
				"prometheus_listen": map[string]any{"type": "string", "description": "Expose Prometheus metrics here, e.g. \":9091\". Required for per-step timings — get_step_latencies has no other source."},
			}, "config_path", "concurrent", "runtime_sec"),
			Handle: func(_ context.Context, args map[string]any) (string, error) {
				return startLoadTest(args)
			},
		},
		{
			Name:        "stop_load_test",
			Description: "Stop a load test this tool started. Runs continue to their runtime deadline otherwise, generating load nobody is watching.",
			Tier:        TierLoad,
			Destructive: true,
			InputSchema: objectSchema(map[string]any{
				"pid": map[string]any{"type": "integer", "description": "Process id from start_load_test or list_load_tests."},
			}, "pid"),
			Handle: func(_ context.Context, args map[string]any) (string, error) {
				return stopLoadTest(intArg(args, "pid"))
			},
		},
		{
			Name:        "start_sample_app",
			Description: "Start a bundled sample 3270 application to test against without a mainframe. App 1 is a self-contained form; app 2 is an RSS reader and needs internet access, so app 1 is the one for an air-gapped smoke test.",
			Tier:        TierSmoke,
			InputSchema: objectSchema(map[string]any{
				"app":  map[string]any{"type": "integer", "enum": []any{1, 2}, "description": "1 (form, self-contained) or 2 (RSS reader, needs internet)."},
				"port": map[string]any{"type": "integer", "minimum": 1024, "description": "Port to listen on (default 3270)."},
			}, "app"),
			Handle: func(_ context.Context, args map[string]any) (string, error) {
				return startSampleApp(intArg(args, "app"), intArg(args, "port"))
			},
		},

		// --- Results ---------------------------------------------------
		{
			Name:        "list_load_tests",
			Description: "List every 3270Connect run on this machine, running or recently finished, with its pid, status and parameters.",
			Tier:        TierRead,
			InputSchema: objectSchema(nil),
			Handle: func(context.Context, map[string]any) (string, error) {
				return listLoadTests()
			},
		},
		{
			Name: "get_load_test_metrics",
			Description: "Return a run's counters and workflow-duration percentiles. " +
				"Note that the durations are a rolling window of the most recent few hundred completed workflows, not the whole run — the returned count says how many, and any percentile quoted should be quoted with it.",
			Tier: TierRead,
			InputSchema: objectSchema(map[string]any{
				"pid": map[string]any{"type": "integer", "description": "Omit to aggregate every running process."},
			}),
			Handle: func(_ context.Context, args map[string]any) (string, error) {
				return loadTestMetrics(intArg(args, "pid"))
			},
		},
		{
			Name: "get_live_workflow_status",
			Description: "Show where each virtual user has got to right now: its host, current step and step type. " +
				"This is what to look at when throughput drops — every worker on the same step means the host is slow at one transaction, which is a different problem from being slow in general.",
			Tier: TierRead,
			InputSchema: objectSchema(map[string]any{
				"pid": map[string]any{"type": "integer", "description": "Omit to include every running process."},
			}),
			Handle: func(_ context.Context, args map[string]any) (string, error) {
				return liveWorkflowStatus(intArg(args, "pid"))
			},
		},
		{
			Name: "get_step_latencies",
			Description: "Return per-step timings from a run's Prometheus endpoint. This is the only source of per-step timing — " +
				"without it you can say a workflow took four seconds but not which step took them. The run must have been started with -promListen.",
			Tier: TierRead,
			InputSchema: objectSchema(map[string]any{
				"prometheus_url": map[string]any{"type": "string", "description": "Metrics endpoint, e.g. \"http://127.0.0.1:9091/metrics\"."},
			}, "prometheus_url"),
			Handle: func(ctx context.Context, args map[string]any) (string, error) {
				return stepLatencies(ctx, stringArg(args, "prometheus_url"))
			},
		},
		{
			Name:        "get_run_summary",
			Description: "Return the text summary a finished run wrote, including its parameters and totals.",
			Tier:        TierRead,
			InputSchema: objectSchema(map[string]any{
				"pid": map[string]any{"type": "integer", "description": "Process id of the run."},
			}, "pid"),
			Handle: func(_ context.Context, args map[string]any) (string, error) {
				return runArtifact(fmt.Sprintf("summary_%d.txt", intArg(args, "pid")))
			},
		},
		{
			Name:        "get_console_log",
			Description: "Return a run's log lines. Use it when a run failed and the counters do not say why.",
			Tier:        TierRead,
			InputSchema: objectSchema(map[string]any{
				"pid": map[string]any{"type": "integer", "description": "Process id of the run."},
			}, "pid"),
			Handle: func(_ context.Context, args map[string]any) (string, error) {
				return runArtifact(fmt.Sprintf("logs_%d.json", intArg(args, "pid")))
			},
		},
	}

	tools = append(tools, workflowTools()...)
	tools = append(tools, skillTools()...)

	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	return tools
}

// enabledTools returns the tools available at a tier.
func enabledTools(tier Tier) []MCPTool {
	var out []MCPTool
	for _, t := range allTools() {
		if t.Tier <= tier {
			out = append(out, t)
		}
	}
	return out
}

func lookupTool(name string) (MCPTool, bool) {
	for _, t := range allTools() {
		if t.Name == name {
			return t, true
		}
	}
	return MCPTool{}, false
}

// workflowFromArgs decodes the workflow argument, which arrives as a decoded
// JSON object and has to go back through the marshaller to reach the typed
// form — including the custom WaitForField handling.
func workflowFromArgs(args map[string]any) (*workflow.Configuration, error) {
	raw, ok := args["workflow"]
	if !ok {
		return nil, fmt.Errorf("workflow is required")
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("workflow is not valid JSON: %w", err)
	}
	cfg := &workflow.Configuration{
		WaitForField: workflow.WaitForFieldConfig{
			Enabled: true,
			Delay:   workflow.DefaultWaitForFieldDelay,
			Retries: workflow.DefaultWaitForFieldRetries,
		},
	}
	if err := json.Unmarshal(encoded, cfg); err != nil {
		return nil, fmt.Errorf("workflow could not be read: %w", err)
	}
	return cfg, nil
}

func toJSON(v any) (string, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func stringArg(args map[string]any, key string) string {
	s, _ := args[key].(string)
	return strings.TrimSpace(s)
}

func intArg(args map[string]any, key string) int {
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(v))
		return n
	}
	return 0
}

// metricsDir is where runs publish their snapshots.
func metricsDir() string {
	dir, err := runstore.Dir()
	if err != nil {
		logMCP("%v", err)
	}
	return dir
}

func logMCP(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}
