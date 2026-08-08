package runstore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeMetrics(t *testing.T, dir string, m Metrics) string {
	t.Helper()
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, fmt.Sprintf("metrics_%d.json", m.PID))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadAllSkipsUnreadableFiles(t *testing.T) {
	dir := t.TempDir()

	writeMetrics(t, dir, Metrics{PID: 101, TotalWorkflowsStarted: 5})
	// A file caught mid-write by another process. Every process rewrites its
	// own every couple of seconds, so this is expected rather than
	// exceptional, and must not stop the others being reported.
	if err := os.WriteFile(filepath.Join(dir, "metrics_999.json"), []byte("{tru"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Not a metrics file at all.
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := ReadAll(dir)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(entries) != 1 || entries[0].Metrics.PID != 101 {
		t.Fatalf("expected only the readable snapshot, got %+v", entries)
	}
}

func TestReadAllOnMissingDirectory(t *testing.T) {
	entries, err := ReadAll(filepath.Join(t.TempDir(), "never-created"))
	if err != nil {
		t.Errorf("a missing directory means no runs, not an error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected no entries, got %d", len(entries))
	}
}

func TestExtendDerivesStatus(t *testing.T) {
	// A process that is not running and accounted for every workflow it
	// started has ended; one that stopped mid-flight was killed. The
	// difference is what tells someone whether their results are complete.
	ended := Metrics{
		PID: -1, StartTimestamp: time.Now().Unix() - 100, RuntimeDuration: 10,
		TotalWorkflowsStarted: 4, TotalWorkflowsCompleted: 4,
	}.Extend()
	if ended.Status != "Ended" {
		t.Errorf("a finished run should be Ended, got %q", ended.Status)
	}
	if ended.IsRunning {
		t.Error("pid -1 is not running")
	}
	if ended.TimeLeft != 0 {
		t.Errorf("a run past its deadline has no time left, got %d", ended.TimeLeft)
	}

	killed := Metrics{
		PID: -1, StartTimestamp: time.Now().Unix(), RuntimeDuration: 3600,
		TotalWorkflowsStarted: 10, TotalWorkflowsCompleted: 2, ActiveWorkflows: 3,
	}.Extend()
	if killed.Status != "Killed" {
		t.Errorf("a run that stopped mid-flight should be Killed, got %q", killed.Status)
	}

	// A sample app has no deadline to reach and must not be reported as
	// having ended just because it has been up a while.
	sample := Metrics{
		PID: os.Getpid(), StartTimestamp: time.Now().Unix() - 100,
		RuntimeDuration: 10, Params: "-runApp 1 -runApp-port 3270",
	}.Extend()
	if sample.Status == "Ended" {
		t.Error("a sample app should not be reported as Ended")
	}
}

func TestPercentiles(t *testing.T) {
	// 1..100, so the nearest-rank answers are exact and easy to state.
	durations := make([]float64, 100)
	for i := range durations {
		durations[i] = float64(i + 1)
	}

	got := Percentiles(durations)
	cases := map[string]struct{ got, want float64 }{
		"min":  {got.Min, 1},
		"max":  {got.Max, 100},
		"mean": {got.Mean, 50.5},
		"p50":  {got.P50, 50},
		"p90":  {got.P90, 90},
		"p95":  {got.P95, 95},
		"p99":  {got.P99, 99},
	}
	for name, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", name, c.got, c.want)
		}
	}
	if got.Count != 100 {
		t.Errorf("Count = %d, want 100", got.Count)
	}

	// Every reported percentile must be a duration something actually took,
	// which is why this uses nearest-rank rather than interpolating.
	single := Percentiles([]float64{7})
	if single.P50 != 7 || single.P99 != 7 || single.Count != 1 {
		t.Errorf("a single sample should be every percentile, got %+v", single)
	}

	if empty := Percentiles(nil); empty.Count != 0 || empty.Max != 0 {
		t.Errorf("no samples should summarise to zero, got %+v", empty)
	}

	// The input must not be reordered under the caller.
	unsorted := []float64{5, 1, 3}
	Percentiles(unsorted)
	if unsorted[0] != 5 {
		t.Errorf("Percentiles sorted its argument in place: %v", unsorted)
	}
}

func TestPreferRunningFallsBackToFinishedRuns(t *testing.T) {
	live := Entry{Metrics: ExtendedMetrics{Metrics: Metrics{PID: 1}, IsRunning: true}}
	dead := Entry{Metrics: ExtendedMetrics{Metrics: Metrics{PID: 2}, IsRunning: false}}

	got := PreferRunning([]Entry{live, dead})
	if len(got) != 1 || got[0].Metrics.PID != 1 {
		t.Errorf("with a live run, only live runs should be reported, got %+v", got)
	}

	// With nothing running, the last finished run is more use than an empty
	// answer — it is what someone asking about a run that just ended wants.
	got = PreferRunning([]Entry{dead})
	if len(got) != 1 {
		t.Errorf("with nothing running, finished runs should still be reported, got %+v", got)
	}
	if len(PreferRunning(nil)) != 0 {
		t.Error("no entries should stay no entries")
	}
}

func TestAggregateCombinesProcesses(t *testing.T) {
	a := Entry{Metrics: ExtendedMetrics{Metrics: Metrics{
		ActiveWorkflows: 2, TotalWorkflowsStarted: 10, TotalWorkflowsCompleted: 8,
		Durations: []float64{1, 2},
		LiveSteps: []WorkflowStatus{{ScriptPort: "5000"}},
	}}}
	b := Entry{Metrics: ExtendedMetrics{Metrics: Metrics{
		ActiveWorkflows: 3, TotalWorkflowsStarted: 5, TotalWorkflowsFailed: 1,
		Durations: []float64{3},
		LiveSteps: []WorkflowStatus{{ScriptPort: "5001"}},
	}}}

	agg := Aggregate([]Entry{a, b})
	if agg.ActiveWorkflows != 5 || agg.TotalWorkflowsStarted != 15 ||
		agg.TotalWorkflowsCompleted != 8 || agg.TotalWorkflowsFailed != 1 {
		t.Errorf("counters did not sum: %+v", agg)
	}
	if len(agg.Durations) != 3 {
		t.Errorf("durations should concatenate, got %v", agg.Durations)
	}
	// Live steps are per-worker, so a machine-wide view needs all of them.
	if len(agg.LiveSteps) != 2 {
		t.Errorf("live steps should concatenate across processes, got %v", agg.LiveSteps)
	}
}

func TestReadFindsOneProcess(t *testing.T) {
	dir := t.TempDir()
	writeMetrics(t, dir, Metrics{PID: 101})
	writeMetrics(t, dir, Metrics{PID: 202, TotalWorkflowsStarted: 7})

	entry, ok := Read(dir, 202)
	if !ok {
		t.Fatal("pid 202 not found")
	}
	if entry.Metrics.TotalWorkflowsStarted != 7 {
		t.Errorf("wrong snapshot returned: %+v", entry.Metrics)
	}
	if _, ok := Read(dir, 999); ok {
		t.Error("an unknown pid should not be found")
	}
}
