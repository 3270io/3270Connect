package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// validWorkflowDoc is the shape the schema actually describes: Type,
// Coordinates and Text, coordinates 1-based.
const validWorkflowDoc = `{
  "Host": "mvs.example.com",
  "Port": 3270,
  "OutputFilePath": "out.html",
  "Steps": [
    {"Type": "Connect"},
    {"Type": "FillString", "Coordinates": {"Row": 10, "Column": 20, "Length": 8}, "Text": "USER"},
    {"Type": "PressEnter"},
    {"Type": "Disconnect"}
  ]
}`

// invalidWorkflowDoc has steps, so it is recognisably a workflow, but
// AsciiScreenGrab with no OutputFilePath is exactly the sort of one-field
// mistake worth surfacing before a run rather than during one.
const invalidWorkflowDoc = `{
  "Host": "mvs.example.com",
  "Port": 3270,
  "Steps": [{"Type": "Connect"}, {"Type": "AsciiScreenGrab"}]
}`

func decodeWorkflowList(t *testing.T, raw string) []workflowSummary {
	t.Helper()
	var payload struct {
		Workflows []workflowSummary `json:"workflows"`
		Note      string            `json:"note"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("list_workflows did not return JSON: %v (%s)", err, raw)
	}
	return payload.Workflows
}

// TestListWorkflowsReportsValidityNotJustNames. A listing of filenames tells
// you nothing you could not get from ls; the point is which of them would
// actually run.
func TestListWorkflowsReportsValidityNotJustNames(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("billing.json", validWorkflowDoc)
	write("broken.json", invalidWorkflowDoc)
	// Not workflows: a metrics file and something that is not JSON at all.
	write("metrics_1234.json", `{"TotalWorkflowsCompleted": 42}`)
	write("notes.txt", validWorkflowDoc)

	raw, err := listWorkflows(dir)
	if err != nil {
		t.Fatalf("listWorkflows: %v", err)
	}
	found := decodeWorkflowList(t, raw)

	if len(found) != 2 {
		t.Fatalf("expected the two documents with steps, got %d: %s", len(found), raw)
	}

	byName := map[string]workflowSummary{}
	for _, w := range found {
		byName[filepath.Base(w.Path)] = w
	}

	good := byName["billing.json"]
	if !good.Valid {
		t.Errorf("billing.json should be valid, got problem %q", good.Problem)
	}
	if good.Host != "mvs.example.com" || good.Port != 3270 || good.Steps != 4 {
		t.Errorf("billing.json summary is wrong: %+v", good)
	}

	bad := byName["broken.json"]
	if bad.Valid {
		t.Error("broken.json should not be reported as valid")
	}
	if bad.Problem == "" {
		t.Error("an invalid workflow must say why, or the listing is no better than ls")
	}
	if bad.Steps != 2 {
		t.Errorf("an invalid workflow should still report what it has, got %+v", bad)
	}
}

// TestListWorkflowsIgnoresJSONThatIsNotAWorkflow: a working directory is
// mostly not workflows — metrics files, injection configs, package.json.
// Listing them as broken workflows would bury the ones that are.
func TestListWorkflowsIgnoresJSONThatIsNotAWorkflow(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"metrics_9.json": `{"TotalWorkflowsCompleted": 3}`,
		"package.json":   `{"name":"thing","version":"1.0.0"}`,
		"garbage.json":   `not json`,
		"empty.json":     `{"Host":"h","Port":1,"Steps":[]}`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	raw, err := listWorkflows(dir)
	if err != nil {
		t.Fatalf("listWorkflows: %v", err)
	}
	if got := decodeWorkflowList(t, raw); len(got) != 0 {
		t.Errorf("nothing here is a workflow, got %+v", got)
	}
	// And an empty answer says what to do next rather than just being empty.
	if !strings.Contains(raw, "describe_workflow_schema") {
		t.Errorf("an empty listing should point at how to make one, got %s", raw)
	}
}

func TestListWorkflowsRefusesAMissingDirectory(t *testing.T) {
	if _, err := listWorkflows(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Error("a directory that does not exist should be an error, not an empty list")
	}
}

// TestListWorkflowsIsOfferedAtTheReadTier — finding out what is available to
// run does not open a session.
func TestListWorkflowsIsOfferedAtTheReadTier(t *testing.T) {
	tools := toolNames(t, connectMCP(t, TierRead))
	if _, ok := tools["list_workflows"]; !ok {
		t.Error("list_workflows should be available at the readonly tier")
	}
	if _, ok := tools["profile_host"]; ok {
		t.Error("profile_host opens a session and must not be in the readonly tier")
	}

	if _, ok := toolNames(t, connectMCP(t, TierSmoke))["profile_host"]; !ok {
		t.Error("profile_host should be available at the smoke tier")
	}
}

// TestProfileHostRespectsTheHostFence. It opens a real connection, so the
// same fence that governs test_connection and the load tools governs it.
func TestProfileHostRespectsTheHostFence(t *testing.T) {
	t.Setenv("MCP_ALLOWED_HOSTS", "lab-*.example.com")

	session := connectMCP(t, TierSmoke)
	text, isErr := callTool(t, session, "profile_host", map[string]any{
		"host": "prod-mvs.example.com", "port": 992,
	})
	if !isErr {
		t.Fatal("a host outside the fence should be refused")
	}
	if !strings.Contains(text, "MCP_ALLOWED_HOSTS") {
		t.Errorf("the refusal should name the setting that governs it, got %q", text)
	}

	// And the refusal happens before anything is dialled: a fenced-off host
	// must not be reachable even by timing.
	if strings.Contains(strings.ToLower(text), "connect") {
		t.Errorf("the fence should refuse before dialling, got %q", text)
	}
}

func TestProfileHostRequiresHostAndPort(t *testing.T) {
	session := connectMCP(t, TierSmoke)
	if _, isErr := callTool(t, session, "profile_host", map[string]any{"host": "", "port": 0}); !isErr {
		t.Error("profile_host with no host should be an error")
	}
}
