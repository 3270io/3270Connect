// Package runstore reads the per-process metrics files a 3270Connect load
// run publishes.
//
// A load test runs as a detached child process, so nothing about it is in the
// memory of whatever wants to report on it. Each running process rewrites
// metrics_<pid>.json in a shared directory every couple of seconds, and that
// file is the only channel out. Reading it is therefore how the dashboard,
// the CLI and an AI client all find out what a run is doing — which is why
// this is a package rather than a handful of functions beside the runner.
//
// Everything here is a pure reader: no printing, no cleanup, no side effects.
// The dashboard's housekeeping stays where it was, because deleting somebody
// else's file is not something a caller asking "how is the run going?" should
// be made to do.
package runstore

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"
)

// Metrics is the snapshot one 3270Connect process publishes.
type Metrics struct {
	PID                     int       `json:"pid"`
	ActiveWorkflows         int       `json:"activeWorkflows"`
	TotalWorkflowsStarted   int64     `json:"totalWorkflowsStarted"`
	TotalWorkflowsCompleted int64     `json:"totalWorkflowsCompleted"`
	TotalWorkflowsFailed    int64     `json:"totalWorkflowsFailed"`
	Durations               []float64 `json:"durations"`
	CPUUsage                []float64 `json:"cpuUsage"`
	MemoryUsage             []float64 `json:"memoryUsage"`
	Params                  string    `json:"params"`
	RuntimeDuration         int       `json:"runtimeDuration"`
	StartTimestamp          int64     `json:"startTimestamp"`
	ConfigFilePath          string    `json:"configFilePath,omitempty"`
	OutputFilePath          string    `json:"outputFilePath,omitempty"`
	// LiveSteps is where each virtual user has got to right now.
	//
	// It is published here because the map it comes from lives in the child
	// process, so without writing it to the file nothing outside that process
	// can see it — and "47 workers are all waiting on a field at step 6" is
	// the single most useful thing to know about a run that has stalled.
	LiveSteps []WorkflowStatus `json:"liveSteps,omitempty"`
}

// WorkflowStatus is one virtual user's position in its workflow.
type WorkflowStatus struct {
	ScriptPort  string `json:"scriptPort"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	CurrentStep int    `json:"currentStep"`
	TotalSteps  int    `json:"totalSteps"`
	StepType    string `json:"stepType"`
	StartedAt   int64  `json:"startedAt"`
}

// ExtendedMetrics adds what can only be worked out by looking at the process
// rather than at the file.
type ExtendedMetrics struct {
	Metrics
	Status    string `json:"status"`
	TimeLeft  int64  `json:"timeLeft"`
	IsRunning bool   `json:"isRunning"`
}

// Entry pairs a snapshot with the file it came from, so a caller that does
// want to tidy up knows what to delete.
type Entry struct {
	Metrics  ExtendedMetrics
	Path     string
	Modified time.Time
}

// Dir returns the directory processes publish their metrics to. It is shared
// across processes on purpose: one dashboard, or one MCP server, sees every
// run on the machine.
func Dir() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return filepath.Join(".", "dashboard"), fmt.Errorf("user config directory unavailable: %w", err)
	}
	return filepath.Join(configDir, "3270Connect", "dashboard"), nil
}

// ReadAll returns every readable snapshot in dir.
//
// A file that cannot be read or parsed is skipped rather than failing the
// call: they are rewritten every two seconds by other processes, so catching
// one mid-write is expected and is not a reason to report nothing.
func ReadAll(dir string) ([]Entry, error) {
	files, err := filepath.Glob(filepath.Join(dir, "metrics_*.json"))
	if err != nil {
		return nil, fmt.Errorf("list metrics files in %s: %w", dir, err)
	}

	out := make([]Entry, 0, len(files))
	for _, path := range files {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var m Metrics
		if err := json.Unmarshal(data, &m); err != nil {
			continue
		}
		out = append(out, Entry{Metrics: m.Extend(), Path: path, Modified: info.ModTime()})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Metrics.PID < out[j].Metrics.PID })
	return out, nil
}

// Read returns the snapshot for one process.
func Read(dir string, pid int) (Entry, bool) {
	entries, err := ReadAll(dir)
	if err != nil {
		return Entry{}, false
	}
	for _, e := range entries {
		if e.Metrics.PID == pid {
			return e, true
		}
	}
	return Entry{}, false
}

// Extend works out a snapshot's status and remaining time.
func (m Metrics) Extend() ExtendedMetrics {
	timeElapsed := time.Now().Unix() - m.StartTimestamp
	timeLeft := int64(m.RuntimeDuration) - timeElapsed
	if timeLeft < 0 {
		timeLeft = 0
	}

	status := "Running"
	isRunning := IsProcessRunning(m.PID)
	completedOrFailed := m.TotalWorkflowsCompleted + m.TotalWorkflowsFailed
	allWorkflowsAccounted := m.TotalWorkflowsStarted > 0 &&
		completedOrFailed >= m.TotalWorkflowsStarted &&
		m.ActiveWorkflows == 0

	// A sample app has no runtime deadline to reach, so its params are
	// checked here rather than it being reported as having ended.
	if m.RuntimeDuration > 0 && timeLeft == 0 && (m.Params != "" && !strings.Contains(m.Params, "-runApp")) {
		status = "Ended"
	}
	if !isRunning {
		switch {
		case allWorkflowsAccounted:
			status = "Ended"
		case status != "Ended":
			status = "Killed"
		}
	}

	return ExtendedMetrics{Metrics: m, Status: status, TimeLeft: timeLeft, IsRunning: isRunning}
}

// IsProcessRunning reports whether a pid is alive.
//
// On failure it answers "yes". A run wrongly reported as dead is cleaned up
// and its metrics deleted; one wrongly reported as alive is merely listed for
// a while longer.
func IsProcessRunning(pid int) bool {
	if pid <= 0 {
		return false
	}
	if runtime.GOOS == "windows" {
		out, err := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid)).Output()
		if err != nil {
			return true
		}
		return strings.Contains(string(out), fmt.Sprint(pid))
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

// PreferRunning narrows a list to live processes, unless none are live — in
// which case the most recent finished run is more useful than nothing.
func PreferRunning(entries []Entry) []Entry {
	running := make([]Entry, 0, len(entries))
	for _, e := range entries {
		if e.Metrics.IsRunning {
			running = append(running, e)
		}
	}
	if len(running) == 0 {
		return entries
	}
	return running
}

// Aggregate sums counters and concatenates samples across processes.
func Aggregate(entries []Entry) Metrics {
	var agg Metrics
	for _, e := range entries {
		agg.ActiveWorkflows += e.Metrics.ActiveWorkflows
		agg.TotalWorkflowsStarted += e.Metrics.TotalWorkflowsStarted
		agg.TotalWorkflowsCompleted += e.Metrics.TotalWorkflowsCompleted
		agg.TotalWorkflowsFailed += e.Metrics.TotalWorkflowsFailed
		agg.Durations = append(agg.Durations, e.Metrics.Durations...)
		agg.CPUUsage = append(agg.CPUUsage, e.Metrics.CPUUsage...)
		agg.MemoryUsage = append(agg.MemoryUsage, e.Metrics.MemoryUsage...)
		agg.LiveSteps = append(agg.LiveSteps, e.Metrics.LiveSteps...)
	}
	return agg
}

// Summary describes a distribution of workflow durations.
//
// Count is the number of samples the percentiles were computed from, and it
// is not the number of workflows the run completed: Durations is a rolling
// window of the most recent few hundred. Anything reporting these figures has
// to say so, or "p99 for the run" gets quoted from the last five minutes of
// it.
type Summary struct {
	Count int     `json:"count"`
	Min   float64 `json:"min"`
	Max   float64 `json:"max"`
	Mean  float64 `json:"mean"`
	P50   float64 `json:"p50"`
	P90   float64 `json:"p90"`
	P95   float64 `json:"p95"`
	P99   float64 `json:"p99"`
}

// Percentiles summarises a set of workflow durations in seconds.
func Percentiles(durations []float64) Summary {
	if len(durations) == 0 {
		return Summary{}
	}

	sorted := append([]float64(nil), durations...)
	sort.Float64s(sorted)

	var total float64
	for _, d := range sorted {
		total += d
	}

	return Summary{
		Count: len(sorted),
		Min:   sorted[0],
		Max:   sorted[len(sorted)-1],
		Mean:  total / float64(len(sorted)),
		P50:   quantile(sorted, 0.50),
		P90:   quantile(sorted, 0.90),
		P95:   quantile(sorted, 0.95),
		P99:   quantile(sorted, 0.99),
	}
}

// quantile picks the nearest-rank value from an ascending slice.
//
// Nearest-rank rather than interpolated: every value it can return is a
// duration some workflow actually took, so "p95 is 2.4s" names a real run
// rather than a number between two of them.
func quantile(sorted []float64, q float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	rank := int(math.Ceil(q * float64(len(sorted))))
	if rank < 1 {
		rank = 1
	}
	if rank > len(sorted) {
		rank = len(sorted)
	}
	return sorted[rank-1]
}
