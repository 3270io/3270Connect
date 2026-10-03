package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWorkflowExitCode(t *testing.T) {
	for _, c := range []struct {
		failed, connections int64
		interrupted         bool
		want                int
	}{
		{0, 0, false, 0}, {1, 0, false, 4}, {0, 1, false, 3}, {1, 1, false, 3}, {1, 1, true, 130},
	} {
		if got := workflowExitCode(c.failed, c.connections, c.interrupted); got != c.want {
			t.Fatalf("%+v: got %d", c, got)
		}
	}
}

func TestSampleWorkflowIsValidAndDoesNotOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.json")
	if err := writeSampleWorkflow(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var config Configuration
	if err = json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	if err = validateConfiguration(&config); err != nil {
		t.Fatal(err)
	}
	if len(config.Steps) < 8 {
		t.Fatal("sample must include assertions and a meaningful action")
	}
	if err = writeSampleWorkflow(path); err == nil {
		t.Fatal("existing workflow was overwritten")
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(data) {
		t.Fatal("existing content changed")
	}
}
