package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	connect3270 "github.com/3270io/3270Connect/connect3270"
	"github.com/3270io/3270Connect/internal/profiler"
	"github.com/3270io/3270Connect/internal/workflow"
)

// Finding a workflow to run, and finding out what a host supports.
//
// Both answer a question that comes before a load test rather than during
// one: which of the JSON files lying around is the billing workflow, and does
// this host do what the workflow assumes.

// workflowSummary is what a listing says about one document.
type workflowSummary struct {
	Path   string `json:"path"`
	Source string `json:"source"`
	Host   string `json:"host,omitempty"`
	Port   int    `json:"port,omitempty"`
	Steps  int    `json:"steps,omitempty"`
	Valid  bool   `json:"valid"`
	// Problem says why an invalid document is invalid. A listing that showed
	// only the sound ones would leave someone hunting for a file that is right
	// there and one field short.
	Problem string `json:"problem,omitempty"`
}

// listWorkflows reports the workflow documents available to run: JSON files
// in a directory, plus everything installed extensions contribute.
func listWorkflows(dir string) (string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		dir = "."
	}
	if _, err := os.Stat(dir); err != nil {
		return "", fmt.Errorf("cannot read %s: %w", dir, err)
	}

	var out []workflowSummary

	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("cannot read %s: %w", dir, err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		summary, isWorkflow := summariseWorkflow(path, "file", data)
		// A directory of JSON is mostly not workflows — metrics files, a
		// package.json, an injection config. Something that does not even
		// have steps is not a broken workflow, it is a different file, and
		// listing it as broken would bury the ones that are.
		if isWorkflow {
			out = append(out, summary)
		}
	}

	catalogue, _ := skillCatalogue()
	for _, doc := range catalogue.Workflows() {
		summary, _ := summariseWorkflow(doc.File, doc.Source.String(), doc.Data)
		out = append(out, summary)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })

	if len(out) == 0 {
		return toJSON(map[string]any{
			"workflows": []workflowSummary{},
			"note": fmt.Sprintf("No workflow documents in %s. "+
				"Compose one with describe_workflow_schema and validate_workflow, then save_workflow.", dir),
		})
	}
	return toJSON(map[string]any{"workflows": out})
}

// summariseWorkflow parses one document, reporting whether it looks like a
// workflow at all and, if so, whether it would run.
func summariseWorkflow(path, source string, data []byte) (workflowSummary, bool) {
	summary := workflowSummary{Path: path, Source: source}

	var cfg workflow.Configuration
	if err := json.Unmarshal(data, &cfg); err != nil {
		return summary, false
	}
	if len(cfg.Steps) == 0 {
		return summary, false
	}

	summary.Host = cfg.Host
	summary.Port = cfg.Port
	summary.Steps = len(cfg.Steps)
	if err := workflow.Validate(&cfg); err != nil {
		summary.Problem = err.Error()
		return summary, true
	}
	summary.Valid = true
	return summary, true
}

// profileHostCapabilities connects once and reports what the host supports.
//
// Read-only — it runs Query actions and presses nothing — but it opens a real
// session against a real host, which is why it is not in the read tier. The
// answer is the same CompatibilityProfile document `-profile` writes, so a
// profile taken here and one taken in CI are the same artefact.
func profileHostCapabilities(ctx context.Context, host string, port int, codePage string) (string, error) {
	host = strings.TrimSpace(host)
	if host == "" || port <= 0 {
		return "", fmt.Errorf("host and port are required")
	}
	if !hostAllowed(host) {
		return "", fmt.Errorf("%s is not in MCP_ALLOWED_HOSTS", host)
	}
	if codePage = strings.TrimSpace(codePage); codePage == "" {
		codePage = "cp037"
	}

	scriptPort, err := getNextAvailablePort()
	if err != nil {
		return "", fmt.Errorf("could not find a free script port: %w", err)
	}

	e := connect3270.NewEmulator(host, port, fmt.Sprintf("%d", scriptPort))
	e.CodePage = codePage
	defer func() { _ = e.Disconnect() }()

	if err := e.Connect(); err != nil {
		return "", fmt.Errorf("could not connect to %s:%d: %w", host, port, err)
	}

	probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	p, err := profiler.Probe(probeCtx, e, profiler.ProbeOptions{
		Tool:    "3270Connect",
		Version: version,
		Host:    host,
		Port:    port,
	})
	if err != nil {
		return "", fmt.Errorf("probe failed: %w", err)
	}
	return toJSON(p)
}

// workflowTools are the two catalogue-and-capability tools, kept here rather
// than inline in allTools because both have real bodies.
func workflowTools() []MCPTool {
	return []MCPTool{
		{
			Name: "list_workflows",
			Description: "List the workflow documents available to run: the JSON files in a directory, plus any an " +
				"installed extension contributes. Each entry says how many steps it has, which host it targets, and " +
				"whether it would pass validation — so a file that is one field short of running is visible rather " +
				"than something to discover at the start of a load test.",
			Tier: TierRead,
			InputSchema: objectSchema(map[string]any{
				"dir": map[string]any{
					"type":        "string",
					"description": "Directory to look in. Defaults to the working directory.",
				},
			}),
			Handle: func(_ context.Context, args map[string]any) (string, error) {
				return listWorkflows(stringArg(args, "dir"))
			},
		},
		{
			Name: "profile_host",
			Description: "Connect once and report what the host supports: screen model, colour, extended attributes, " +
				"character sets and the query replies behind them. Read-only — it presses no keys and submits nothing. " +
				"Worth doing before writing a workflow against an unfamiliar host, because it answers whether the " +
				"screen size the coordinates assume is the screen size you will get.",
			Tier:      TierSmoke,
			OpenWorld: true,
			InputSchema: objectSchema(map[string]any{
				"host":      map[string]any{"type": "string", "description": "Hostname or IP address."},
				"port":      map[string]any{"type": "integer", "minimum": 1, "maximum": 65535},
				"code_page": map[string]any{"type": "string", "description": "Host EBCDIC code page, e.g. \"cp037\" or \"cp278\". Defaults to cp037."},
			}, "host", "port"),
			Handle: func(ctx context.Context, args map[string]any) (string, error) {
				return profileHostCapabilities(ctx, stringArg(args, "host"), intArg(args, "port"), stringArg(args, "code_page"))
			},
		},
	}
}
