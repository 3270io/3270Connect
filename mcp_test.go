package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// connectMCP stands the server up on an in-memory transport and returns a
// connected client. This is the real JSON-RPC path — initialize, tools/list,
// tools/call — with no subprocess to make it flaky.
func connectMCP(t *testing.T, tier Tier) *mcp.ClientSession {
	t.Helper()

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	server := buildMCPServer(tier)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)

	go func() {
		if err := server.Run(ctx, serverTransport); err != nil && ctx.Err() == nil {
			t.Errorf("server.Run: %v", err)
		}
	}()

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func toolNames(t *testing.T, session *mcp.ClientSession) map[string]*mcp.Tool {
	t.Helper()
	res, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	out := map[string]*mcp.Tool{}
	for _, tool := range res.Tools {
		out[tool.Name] = tool
	}
	return out
}

func callTool(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) (string, bool) {
	t.Helper()
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("tools/call %s: %v", name, err)
	}
	var sb strings.Builder
	for _, c := range res.Content {
		if text, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(text.Text)
		}
	}
	return sb.String(), res.IsError
}

// TestTierGating is the property that makes the default safe to hand out:
// nothing at the readonly tier opens a session or generates load.
func TestTierGating(t *testing.T) {
	t.Run("readonly cannot reach a host", func(t *testing.T) {
		tools := toolNames(t, connectMCP(t, TierRead))

		for _, present := range []string{
			"describe_workflow_schema", "validate_workflow", "list_load_tests",
			"get_load_test_metrics", "list_skills", "load_skill",
		} {
			if _, ok := tools[present]; !ok {
				t.Errorf("%q should be available at the readonly tier", present)
			}
		}
		for _, absent := range []string{"run_workflow_once", "start_load_test", "stop_load_test", "save_workflow"} {
			if _, ok := tools[absent]; ok {
				t.Errorf("%q must not be offered at the readonly tier", absent)
			}
		}
	})

	t.Run("smoke runs once but not at concurrency", func(t *testing.T) {
		tools := toolNames(t, connectMCP(t, TierSmoke))
		if _, ok := tools["run_workflow_once"]; !ok {
			t.Error("run_workflow_once should be available at the smoke tier")
		}
		if _, ok := tools["start_load_test"]; ok {
			t.Error("start_load_test must not be offered below the load tier")
		}
	})

	t.Run("load adds concurrency", func(t *testing.T) {
		tools := toolNames(t, connectMCP(t, TierLoad))
		for _, present := range []string{"start_load_test", "stop_load_test"} {
			if _, ok := tools[present]; !ok {
				t.Errorf("%q should be available at the load tier", present)
			}
		}
	})

	t.Run("tiers nest", func(t *testing.T) {
		read, smoke, load := enabledTools(TierRead), enabledTools(TierSmoke), enabledTools(TierLoad)
		has := func(set []MCPTool, name string) bool {
			for _, tool := range set {
				if tool.Name == name {
					return true
				}
			}
			return false
		}
		for _, tool := range read {
			if !has(smoke, tool.Name) {
				t.Errorf("%q is in readonly but not in smoke", tool.Name)
			}
			if tool.Destructive {
				t.Errorf("%q is in the readonly tier but is marked destructive", tool.Name)
			}
		}
		for _, tool := range smoke {
			if !has(load, tool.Name) {
				t.Errorf("%q is in smoke but not in load", tool.Name)
			}
		}
	})
}

func TestParseTierRejectsUnknownValues(t *testing.T) {
	for in, want := range map[string]Tier{
		"": TierRead, "readonly": TierRead, "READ": TierRead,
		"smoke": TierSmoke, "load": TierLoad, " Full ": TierLoad,
	} {
		got, ok := ParseTier(in)
		if !ok || got != want {
			t.Errorf("ParseTier(%q) = %v/%v, want %v/true", in, got, ok, want)
		}
	}

	// An unrecognised value must not be read as "everything", and must say
	// it was not understood so a typo is visible.
	got, ok := ParseTier("looad")
	if ok {
		t.Error("an unknown tier should be reported as unrecognised")
	}
	if got != TierRead {
		t.Errorf("an unknown tier should fall back to the most restrictive, got %v", got)
	}
}

// TestSchemaAndValidateOverTheProtocol walks the authoring loop a model
// actually uses: ask for the schema, send something wrong, read why, fix it.
func TestSchemaAndValidateOverTheProtocol(t *testing.T) {
	session := connectMCP(t, TierRead)

	schema, isErr := callTool(t, session, "describe_workflow_schema", nil)
	if isErr {
		t.Fatalf("describe_workflow_schema failed: %s", schema)
	}
	// The wrong shape is in circulation, so the schema has to warn about it
	// rather than merely omit it.
	for _, want := range []string{"Coordinates", "1-based", "NOT"} {
		if !strings.Contains(schema, want) {
			t.Errorf("the schema should mention %q", want)
		}
	}

	// The shape from the old, incorrect documentation.
	out, _ := callTool(t, session, "validate_workflow", map[string]any{
		"workflow": map[string]any{
			"Host": "h", "Port": 3270,
			"Steps": []any{map[string]any{"Action": "Connect"}},
		},
	})
	if !strings.Contains(out, `"valid": false`) {
		t.Errorf("a step using Action should not validate, got %s", out)
	}

	// The real shape.
	out, isErr = callTool(t, session, "validate_workflow", map[string]any{
		"workflow": map[string]any{
			"Host": "h", "Port": 3270,
			"Steps": []any{
				map[string]any{"Type": "Connect"},
				map[string]any{"Type": "FillString", "Coordinates": map[string]any{"Row": 5, "Column": 10, "Length": 8}, "Text": "user"},
				map[string]any{"Type": "Disconnect"},
			},
		},
	})
	if isErr || !strings.Contains(out, `"valid": true`) {
		t.Errorf("a well-formed workflow should validate, got %s", out)
	}
}

// TestCapsRefuseRatherThanShrink: a run at a tenth of the requested
// concurrency, reported as the requested one, is worse than a refusal.
func TestCapsRefuseRatherThanShrink(t *testing.T) {
	t.Setenv("MCP_MAX_CONCURRENT", "10")
	t.Setenv("MCP_MAX_RUNTIME_SEC", "60")

	session := connectMCP(t, TierLoad)

	out, isErr := callTool(t, session, "start_load_test", map[string]any{
		"config_path": "workflow.json", "concurrent": 5000, "runtime_sec": 30,
	})
	if !isErr {
		t.Error("a request over the concurrency cap should be refused")
	}
	if !strings.Contains(out, "10") || !strings.Contains(out, "MCP_MAX_CONCURRENT") {
		t.Errorf("the refusal should name the cap and the setting, got %q", out)
	}

	out, isErr = callTool(t, session, "start_load_test", map[string]any{
		"config_path": "workflow.json", "concurrent": 2, "runtime_sec": 9999,
	})
	if !isErr {
		t.Error("a request over the runtime cap should be refused")
	}
	if !strings.Contains(out, "MCP_MAX_RUNTIME_SEC") {
		t.Errorf("the refusal should name the runtime cap, got %q", out)
	}
}

// TestStopLoadTestRefusesForeignPids is the guard on a tool that sends
// signals, driven by a model reading a number out of its own earlier output.
func TestStopLoadTestRefusesForeignPids(t *testing.T) {
	session := connectMCP(t, TierLoad)

	// A pid that exists but published no 3270Connect metrics: this test
	// process itself, and pid 1.
	for _, pid := range []int{1, 999999} {
		out, isErr := callTool(t, session, "stop_load_test", map[string]any{"pid": pid})
		if !isErr {
			t.Errorf("stopping pid %d should be refused", pid)
		}
		if !strings.Contains(out, "list_load_tests") {
			t.Errorf("the refusal should say how to find a real pid, got %q", out)
		}
	}
}

func TestAllowedHostsFencesOffTargets(t *testing.T) {
	t.Setenv("MCP_ALLOWED_HOSTS", "*.test.internal, 127.0.0.1")

	if !hostAllowed("host1.test.internal") {
		t.Error("a matching host should be allowed")
	}
	if !hostAllowed("127.0.0.1") {
		t.Error("an exact match should be allowed")
	}
	if hostAllowed("prod.example.com") {
		t.Error("a host outside the list must be refused")
	}

	session := connectMCP(t, TierSmoke)
	out, isErr := callTool(t, session, "test_connection", map[string]any{
		"host": "prod.example.com", "port": 23,
	})
	if !isErr || !strings.Contains(out, "MCP_ALLOWED_HOSTS") {
		t.Errorf("a fenced-off host should be refused, naming the setting, got %q", out)
	}
}

func TestAllowedHostsUnsetAllowsEverything(t *testing.T) {
	t.Setenv("MCP_ALLOWED_HOSTS", "")
	if !hostAllowed("anything.example.com") {
		t.Error("with no allow-list configured, every host should be allowed")
	}
}

// TestSkillsOverTheProtocol covers two-level disclosure and the dedup.
func TestSkillsOverTheProtocol(t *testing.T) {
	session := connectMCP(t, TierRead)

	list, isErr := callTool(t, session, "list_skills", nil)
	if isErr {
		t.Fatalf("list_skills failed: %s", list)
	}
	for _, want := range []string{"ramp-up-load-test", "interpret-results", "find-concurrency-knee"} {
		if !strings.Contains(list, want) {
			t.Errorf("list_skills is missing %q", want)
		}
	}
	// A listing is metadata; the bodies are what load_skill is for.
	if strings.Contains(list, "## When to use") {
		t.Error("list_skills should not carry skill bodies")
	}

	body, isErr := callTool(t, session, "load_skill", map[string]any{"name": "ramp-up-load-test"})
	if isErr {
		t.Fatalf("load_skill failed: %s", body)
	}
	var loaded struct {
		Body            string   `json:"body"`
		InstructionRefs []string `json:"instruction_refs"`
	}
	if err := json.Unmarshal([]byte(body), &loaded); err != nil {
		t.Fatalf("load_skill did not return JSON: %v", err)
	}
	if len(loaded.InstructionRefs) == 0 {
		t.Error("ramp-up-load-test should cite instruction fragments")
	}
	// Named, not inlined — that is what keeps the first load small.
	if strings.Contains(loaded.Body, "# Before you generate load") {
		t.Error("load_skill inlined an instruction fragment instead of naming it")
	}

	// A repeat costs a line rather than the body again.
	again, _ := callTool(t, session, "load_skill", map[string]any{"name": "ramp-up-load-test"})
	if !strings.Contains(again, "Already loaded") {
		t.Errorf("a repeat load should return a reminder, got %q", again)
	}
	if len(again) >= len(body) {
		t.Errorf("the reminder (%d chars) should be much shorter than the body (%d)", len(again), len(body))
	}

	// And the fragment itself is reachable.
	frag, isErr := callTool(t, session, "load_instruction", map[string]any{"name": "reading-latency"})
	if isErr {
		t.Fatalf("load_instruction failed: %s", frag)
	}
	if !strings.Contains(frag, "Percentiles, not means") {
		t.Errorf("load_instruction should return the fragment body, got %q", frag)
	}
}

func TestUnknownSkillNameExplainsItself(t *testing.T) {
	session := connectMCP(t, TierRead)
	out, isErr := callTool(t, session, "load_skill", map[string]any{"name": "no-such-skill"})
	if !isErr {
		t.Error("an unknown skill should be an error")
	}
	if !strings.Contains(out, "list_skills") {
		t.Errorf("the error should point at how to find a valid name, got %q", out)
	}
}

func TestPromptDescribesTheActiveTier(t *testing.T) {
	for tier, want := range map[Tier]string{
		TierRead:  "cannot open a session",
		TierSmoke: "not at concurrency",
		TierLoad:  "Every tool is available",
	} {
		session := connectMCP(t, tier)
		res, err := session.GetPrompt(context.Background(), &mcp.GetPromptParams{Name: "load_test_3270"})
		if err != nil {
			t.Fatalf("prompts/get at tier %v: %v", tier, err)
		}
		var sb strings.Builder
		for _, m := range res.Messages {
			if text, ok := m.Content.(*mcp.TextContent); ok {
				sb.WriteString(text.Text)
			}
		}
		got := sb.String()
		if !strings.Contains(got, want) {
			t.Errorf("the %v prompt should mention %q", tier, want)
		}
		// The skill index is generated, so a skill added on disk is visible
		// without anyone editing the prompt.
		if !strings.Contains(got, "ramp-up-load-test") {
			t.Errorf("the %v prompt should carry the skill index", tier)
		}
	}
}

func TestAnnotationsReflectTheRegistry(t *testing.T) {
	tools := toolNames(t, connectMCP(t, TierLoad))

	if got := tools["describe_workflow_schema"]; got.Annotations == nil || !got.Annotations.ReadOnlyHint {
		t.Error("describe_workflow_schema should be annotated read-only")
	}
	start := tools["start_load_test"]
	if start.Annotations == nil || start.Annotations.DestructiveHint == nil || !*start.Annotations.DestructiveHint {
		t.Error("start_load_test should be annotated destructive")
	}
	if start.Annotations.ReadOnlyHint {
		t.Error("start_load_test must not be annotated read-only")
	}
	if start.Annotations.OpenWorldHint == nil || !*start.Annotations.OpenWorldHint {
		t.Error("start_load_test reaches a host, so it should be annotated open-world")
	}
}

// TestMetricsReplyCarriesItsCaveat: the rolling-window caveat is the one most
// easily dropped when a percentile is quoted onwards, so it travels with the
// numbers rather than living only in the tool description.
func TestMetricsReplyCarriesItsCaveat(t *testing.T) {
	session := connectMCP(t, TierRead)
	out, _ := callTool(t, session, "get_load_test_metrics", nil)
	if strings.Contains(out, "No 3270Connect runs found") {
		t.Skip("no runs on this machine to report on")
	}
	if !strings.Contains(out, "rolling window") {
		t.Errorf("the metrics reply should carry its sampling caveat, got %q", out)
	}
}
