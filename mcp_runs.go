package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/3270io/3270Connect/internal/runstore"
)

// Starting, stopping and reading load runs.
//
// A run is a detached child process, exactly as the dashboard's
// /start-process already does it. The load engine is a blocking, terminal-
// coupled thing with its own worker pool, progress bars and a stdin prompt
// during its grace period; running it in this process would tangle all of
// that with the protocol on stdout. Spawning it keeps both simple, and means
// a run outlives the conversation that started it — which is what someone
// asking for a ten-minute soak actually wants.

func dialHost(host string, port int) error {
	if strings.TrimSpace(host) == "" || port <= 0 {
		return fmt.Errorf("host and port are required")
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 5*time.Second)
	if err != nil {
		return err
	}
	return conn.Close()
}

// startLoadTest spawns a run and returns its pid.
func startLoadTest(args map[string]any) (string, error) {
	configPath := stringArg(args, "config_path")
	if configPath == "" {
		return "", fmt.Errorf("config_path is required")
	}
	abs, err := filepath.Abs(configPath)
	if err != nil {
		return "", err
	}
	cfg, err := loadConfigurationSafe(abs)
	if err != nil {
		return "", fmt.Errorf("could not read %s: %w", abs, err)
	}
	if !hostAllowed(cfg.Host) {
		return "", fmt.Errorf("%s is not in MCP_ALLOWED_HOSTS", cfg.Host)
	}

	concurrent := intArg(args, "concurrent")
	runtimeSec := intArg(args, "runtime_sec")
	if concurrent < 1 {
		return "", fmt.Errorf("concurrent must be at least 1")
	}
	if runtimeSec < 1 {
		return "", fmt.Errorf("runtime_sec must be at least 1")
	}
	// Refuse rather than quietly running something smaller: a run at a
	// tenth of the requested concurrency, reported as the requested one, is
	// worse than a refusal that names the cap.
	if cap := maxConcurrent(); concurrent > cap {
		return "", fmt.Errorf("concurrent=%d exceeds the limit of %d (MCP_MAX_CONCURRENT). "+
			"Run at or below the limit, or raise it deliberately", concurrent, cap)
	}
	if cap := maxRuntimeSec(); runtimeSec > cap {
		return "", fmt.Errorf("runtime_sec=%d exceeds the limit of %d (MCP_MAX_RUNTIME_SEC). "+
			"Run for less, or raise the limit deliberately", runtimeSec, cap)
	}

	exe, err := os.Executable()
	if err != nil {
		return "", err
	}

	// -headless because there is no terminal to draw progress bars on, and
	// -gracePeriod/-autoShutdown explicitly because the grace-period drain
	// prompts on stdin. A detached child has no stdin; EOF degrades to
	// shutdown, but relying on that is how a run exits early and nobody
	// knows why.
	argv := []string{
		"-config", abs,
		"-concurrent", strconv.Itoa(concurrent),
		"-runtime", strconv.Itoa(runtimeSec),
		"-headless",
		"-gracePeriod", "30",
		"-autoShutdown", "5",
	}
	if port := intArg(args, "start_port"); port > 0 {
		argv = append(argv, "-startPort", strconv.Itoa(port))
	}
	if injection := stringArg(args, "injection_path"); injection != "" {
		injAbs, err := filepath.Abs(injection)
		if err != nil {
			return "", err
		}
		argv = append(argv, "-injectionConfig", injAbs)
	}
	if prom := stringArg(args, "prometheus_listen"); prom != "" {
		argv = append(argv, "-promListen", prom)
	}
	if token := strings.TrimSpace(os.Getenv("RSA_TOKEN")); token != "" {
		argv = append(argv, "-token", token)
	}

	// The child's own output goes to a file, never to this process's stdout,
	// which belongs to the protocol.
	logPath := filepath.Join(os.TempDir(), fmt.Sprintf("3270connect-mcp-%d.log", time.Now().UnixNano()))
	logFile, err := os.Create(logPath)
	if err != nil {
		return "", fmt.Errorf("could not create a log file for the run: %w", err)
	}

	cmd := exec.Command(exe, argv...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return "", fmt.Errorf("could not start the run: %w", err)
	}
	go func() {
		_ = cmd.Wait()
		logFile.Close()
	}()

	return toJSON(map[string]any{
		"pid":         cmd.Process.Pid,
		"host":        cfg.Host,
		"port":        cfg.Port,
		"concurrent":  concurrent,
		"runtime_sec": runtimeSec,
		"log_path":    logPath,
		"note": "Poll get_load_test_metrics while it runs. It continues to its runtime deadline " +
			"unless stopped, so call stop_load_test when the question is answered.",
	})
}

// stopLoadTest signals a run, having first checked it is one of ours.
//
// Without the check this is a tool that sends signals to arbitrary pids on
// the machine, driven by a model reading a number out of its own earlier
// output. A published metrics file naming this program is what makes a pid
// ours.
func stopLoadTest(pid int) (string, error) {
	if pid <= 0 {
		return "", fmt.Errorf("a pid is required")
	}
	if pid == os.Getpid() {
		return "", fmt.Errorf("that is this MCP server's own pid")
	}

	entry, ok := runstore.Read(metricsDir(), pid)
	if !ok {
		return "", fmt.Errorf("pid %d has published no 3270Connect metrics, so it is not a run this tool can stop. "+
			"Call list_load_tests to see what is running", pid)
	}
	if !entry.Metrics.IsRunning {
		return fmt.Sprintf("Run %d has already finished (%s).", pid, entry.Metrics.Status), nil
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return "", fmt.Errorf("could not find process %d: %w", pid, err)
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return "", fmt.Errorf("could not stop process %d: %w", pid, err)
	}
	return fmt.Sprintf("Asked run %d to stop. Its in-flight workflows finish during the grace period.", pid), nil
}

// startSampleApp launches a bundled 3270 application to test against.
func startSampleApp(app, port int) (string, error) {
	if app != 1 && app != 2 {
		return "", fmt.Errorf("app must be 1 or 2")
	}
	if port <= 0 {
		port = 3270
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}

	logPath := filepath.Join(os.TempDir(), fmt.Sprintf("3270connect-sampleapp-%d.log", port))
	logFile, err := os.Create(logPath)
	if err != nil {
		return "", err
	}
	cmd := exec.Command(exe, "-runApp", strconv.Itoa(app), "-runApp-port", strconv.Itoa(port))
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return "", fmt.Errorf("could not start the sample app: %w", err)
	}
	go func() {
		_ = cmd.Wait()
		logFile.Close()
	}()

	note := "Connect a workflow to 127.0.0.1 on this port."
	if app == 2 {
		note = "App 2 fetches RSS feeds, so it needs outbound internet access. App 1 is self-contained."
	}
	return toJSON(map[string]any{
		"pid": cmd.Process.Pid, "app": app, "host": "127.0.0.1", "port": port, "note": note,
	})
}

// listLoadTests reports every run on the machine.
func listLoadTests() (string, error) {
	entries, err := runstore.ReadAll(metricsDir())
	if err != nil {
		return "", err
	}
	if len(entries) == 0 {
		return "No 3270Connect runs found. Start one with start_load_test.", nil
	}

	out := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		out = append(out, map[string]any{
			"pid":        e.Metrics.PID,
			"status":     e.Metrics.Status,
			"running":    e.Metrics.IsRunning,
			"time_left":  e.Metrics.TimeLeft,
			"started":    e.Metrics.TotalWorkflowsStarted,
			"completed":  e.Metrics.TotalWorkflowsCompleted,
			"failed":     e.Metrics.TotalWorkflowsFailed,
			"active":     e.Metrics.ActiveWorkflows,
			"parameters": e.Metrics.Params,
			"config":     e.Metrics.ConfigFilePath,
		})
	}
	return toJSON(map[string]any{"count": len(out), "runs": out})
}

// loadTestMetrics reports counters and percentiles for one run, or for every
// running process when no pid is given.
func loadTestMetrics(pid int) (string, error) {
	entries, err := runstore.ReadAll(metricsDir())
	if err != nil {
		return "", err
	}
	if len(entries) == 0 {
		return "No 3270Connect runs found.", nil
	}

	var selected []runstore.Entry
	if pid > 0 {
		for _, e := range entries {
			if e.Metrics.PID == pid {
				selected = append(selected, e)
			}
		}
		if len(selected) == 0 {
			return "", fmt.Errorf("no run with pid %d; call list_load_tests to see what is running", pid)
		}
	} else {
		selected = runstore.PreferRunning(entries)
	}

	agg := runstore.Aggregate(selected)
	summary := runstore.Percentiles(agg.Durations)

	pids := make([]int, 0, len(selected))
	running := false
	for _, e := range selected {
		pids = append(pids, e.Metrics.PID)
		running = running || e.Metrics.IsRunning
	}
	sort.Ints(pids)

	return toJSON(map[string]any{
		"pids":    pids,
		"running": running,
		"workflows": map[string]any{
			"started":   agg.TotalWorkflowsStarted,
			"completed": agg.TotalWorkflowsCompleted,
			"failed":    agg.TotalWorkflowsFailed,
			"active":    agg.ActiveWorkflows,
		},
		"duration_seconds": summary,
		// Stated on every reply rather than left to the tool description,
		// because this is the caveat most easily dropped when a figure is
		// quoted onwards.
		"duration_sample_note": fmt.Sprintf(
			"Percentiles are over the %d most recently completed workflows, which is a rolling window and not the whole run. Quote the count with the figure.",
			summary.Count),
		"resource_note": "CPU and memory samples are for the load generator, not the host under test.",
	})
}

// liveWorkflowStatus reports where each virtual user currently is.
func liveWorkflowStatus(pid int) (string, error) {
	entries, err := runstore.ReadAll(metricsDir())
	if err != nil {
		return "", err
	}

	type row struct {
		PID         int    `json:"pid"`
		ScriptPort  string `json:"scriptPort"`
		Host        string `json:"host"`
		Port        int    `json:"port"`
		CurrentStep int    `json:"currentStep"`
		TotalSteps  int    `json:"totalSteps"`
		StepType    string `json:"stepType"`
		RunningFor  int64  `json:"runningForSeconds"`
	}

	now := time.Now().Unix()
	var rows []row
	byStep := map[string]int{}
	for _, e := range entries {
		if pid > 0 && e.Metrics.PID != pid {
			continue
		}
		for _, s := range e.Metrics.LiveSteps {
			elapsed := int64(0)
			if s.StartedAt > 0 {
				elapsed = now - s.StartedAt
			}
			rows = append(rows, row{
				PID: e.Metrics.PID, ScriptPort: s.ScriptPort, Host: s.Host, Port: s.Port,
				CurrentStep: s.CurrentStep, TotalSteps: s.TotalSteps, StepType: s.StepType,
				RunningFor: elapsed,
			})
			byStep[s.StepType]++
		}
	}

	if len(rows) == 0 {
		return "No workflows are in flight. Either nothing is running, or the run has not published a snapshot yet — they are written every two seconds.", nil
	}

	return toJSON(map[string]any{
		"workers": rows,
		"by_step": byStep,
		// The clustering is the point of this tool, so it is said rather
		// than left to be noticed in the table.
		"note": "Workers clustered on one step mean the host is slow at that transaction rather than slow in general.",
	})
}

// stepLatencies scrapes a run's Prometheus endpoint.
func stepLatencies(ctx context.Context, url string) (string, error) {
	if url == "" {
		return "", fmt.Errorf("prometheus_url is required")
	}
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		url = "http://" + url
	}
	if !strings.HasSuffix(url, "/metrics") {
		url = strings.TrimRight(url, "/") + "/metrics"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("could not reach %s: %w. Per-step timings exist only on a run's Prometheus endpoint — "+
			"start the run with -promListen :9091 (via start_load_test's prometheus_listen) to expose them", url, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return "", err
	}

	// Keep only this program's series. A Go process exports a great deal
	// about its own runtime, none of which answers a question about a host.
	var kept []string
	for _, line := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(line, "tn3270_") {
			kept = append(kept, line)
		}
	}
	if len(kept) == 0 {
		return "", fmt.Errorf("%s answered but exported no tn3270_ metrics; it may not be a 3270Connect run", url)
	}
	return strings.Join(kept, "\n"), nil
}

// runArtifact returns a file a run wrote beside the working directory.
func runArtifact(name string) (string, error) {
	path := filepath.Join("logs", name)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("could not read %s: %w. A run writes these into ./logs relative to where it was started", path, err)
	}
	return string(data), nil
}
