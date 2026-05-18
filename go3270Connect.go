package main

import (
	"bufio"
	"bytes"
	crand "crypto/rand"
	"embed"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	connect3270 "github.com/3270io/3270Connect/connect3270"
	metricsPkg "github.com/3270io/3270Connect/internal/metrics"
	"github.com/3270io/3270Connect/sampleapps/app1"
	app2 "github.com/3270io/3270Connect/sampleapps/app2"
	"github.com/charmbracelet/lipgloss"

	"github.com/gin-gonic/gin"
	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/mem"
)

const version = "1.9"

const (
	cpuHistoryLimit              = 120
	memHistoryLimit              = 120
	workflowDurationHistoryLimit = 500
	inMemoryLogLimit             = 500
	dashboardCleanupInterval     = time.Minute
	liveStatsHistoryLimit        = 12
	defaultGracePeriod           = 30 * time.Second
	promptTimeout                = 10 * time.Second
)

var errorList []error
var errorMutex sync.Mutex

// DelayRange represents a randomized delay window in seconds. When Max is
// omitted (zero) but Min is set, Max defaults to Min. Set both Min and Max to
// zero to disable the delay entirely.
type DelayRange struct {
	Min float64 `json:"Min,omitempty"`
	Max float64 `json:"Max,omitempty"`
}

// WaitForFieldConfig holds the configuration for WaitForField behavior.
// It supports both simple boolean values (for backward compatibility) and
// detailed configuration with custom delay and retry settings.
type WaitForFieldConfig struct {
	Enabled bool    `json:"-"` // Not directly unmarshaled
	Delay   float64 `json:"Delay,omitempty"`
	Retries int     `json:"Retries,omitempty"`
}

// UnmarshalJSON implements custom JSON unmarshaling to support both boolean and object formats.
// Examples: "WaitForField": true or "WaitForField": { "Delay": 2, "Retries": 10 }
func (w *WaitForFieldConfig) UnmarshalJSON(data []byte) error {
	// Try to unmarshal as boolean first
	var boolVal bool
	if err := json.Unmarshal(data, &boolVal); err == nil {
		w.Enabled = boolVal
		// Set defaults when using boolean format
		if w.Delay == 0 {
			w.Delay = 1.0
		}
		if w.Retries == 0 {
			w.Retries = 10
		}
		return nil
	}

	// Try to unmarshal as object
	type Alias WaitForFieldConfig
	aux := &struct {
		*Alias
	}{
		Alias: (*Alias)(w),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	w.Enabled = true // If object format is used, assume enabled
	// Set defaults if not provided
	if w.Delay == 0 {
		w.Delay = 1.0
	}
	if w.Retries == 0 {
		w.Retries = 10
	}
	return nil
}

// MarshalJSON implements custom JSON marshaling.
func (w WaitForFieldConfig) MarshalJSON() ([]byte, error) {
	// If using default values, just marshal as boolean
	if w.Delay == 1.0 && w.Retries == 10 {
		return json.Marshal(w.Enabled)
	}
	// Otherwise, marshal as object with just the custom fields
	return json.Marshal(map[string]interface{}{
		"Delay":   w.Delay,
		"Retries": w.Retries,
	})
}

// Configuration holds the settings for the terminal connection and the steps to be executed.
type Configuration struct {
	Host            string
	Port            int
	OutputFilePath  string             `json:"OutputFilePath"`
	WaitForField    WaitForFieldConfig `json:"WaitForField,omitempty"`
	Steps           []Step
	EveryStepDelay  DelayRange `json:"EveryStepDelay,omitempty"`
	EndOfTaskDelay  DelayRange `json:"EndOfTaskDelay,omitempty"`
	Token           string     `json:"Token,omitempty"`
	InputFilePath   string     `json:"InputFilePath"`
	RampUpBatchSize int        `json:"RampUpBatchSize"`
	RampUpDelay     float64    `json:"RampUpDelay"`
	LegacyDelay     float64    `json:"Delay,omitempty"`
}

// Step represents an individual action to be taken on the terminal.
type Step struct {
	Type        string
	Coordinates connect3270.Coordinates
	Text        string
	Delay       float64    `json:"Delay,omitempty"`
	StepDelay   DelayRange `json:"StepDelay,omitempty"`
}

var configPrinter *MessagePrinter

func runtimeEnvironmentString() string {
	args := strings.Join(os.Args[1:], " ")
	if args == "" {
		return getExecutablePath()
	}
	return getExecutablePath() + " " + args
}

func cliArgsString() string {
	args := strings.TrimSpace(strings.Join(os.Args[1:], " "))
	if args == "" {
		return "(none)"
	}
	return args
}

func formatSeconds(seconds float64) string {
	if seconds == 0 {
		return "0s"
	}
	if seconds == float64(int64(seconds)) {
		return fmt.Sprintf("%ds", int64(seconds))
	}
	// Keep it compact but readable.
	return fmt.Sprintf("%.2fs", seconds)
}

func formatDelayRange(r DelayRange) string {
	min := r.Min
	max := r.Max
	if min == 0 && max == 0 {
		return "disabled"
	}
	if max == 0 {
		max = min
	}
	if min == max {
		return formatSeconds(min)
	}
	return fmt.Sprintf("%s - %s", formatSeconds(min), formatSeconds(max))
}

func formatWaitForField(cfg WaitForFieldConfig) string {
	if !cfg.Enabled {
		return "disabled"
	}
	delay := cfg.Delay
	retries := cfg.Retries
	if delay == 0 {
		delay = 1.0
	}
	if retries == 0 {
		retries = 10
	}
	return fmt.Sprintf("enabled (delay %s, retries %d)", formatSeconds(delay), retries)
}

func workflowMetadataText(configPath string, config *Configuration) string {
	label := strings.TrimSpace(configPath)
	if label == "" {
		label = "(none)"
	}

	outputPath := strings.TrimSpace(config.OutputFilePath)
	if outputPath == "" {
		outputPath = "(auto temp file)"
	}

	return strings.Join([]string{
		//fmt.Sprintf("Config file: %s", label),
		fmt.Sprintf("CLI args: %s", cliArgsString()),
		fmt.Sprintf("Host: %s", config.Host),
		fmt.Sprintf("Port: %d", config.Port),
		fmt.Sprintf("EveryStepDelay: %s", formatDelayRange(config.EveryStepDelay)),
		fmt.Sprintf("WaitForField: %s", formatWaitForField(config.WaitForField)),
		fmt.Sprintf("OutputFilePath: %s", outputPath),
		fmt.Sprintf("RampUpBatchSize: %d", config.RampUpBatchSize),
		fmt.Sprintf("RampUpDelay: %s", formatSeconds(config.RampUpDelay)),
		fmt.Sprintf("EndOfTaskDelay: %s", formatDelayRange(config.EndOfTaskDelay)),
	}, "\n")
}

func printWorkflowMetadata(configPath string, config *Configuration) {
	if configPrinter == nil {
		// Fallback: should never happen because init() wires it.
		//pterm.Info.Println("Workflow Configuration")
		pterm.Println(workflowMetadataText(configPath, config))
		pterm.Println()
		return
	}
	//configPrinter.Println("Workflow Configuration")

	// Print the summary lines with light highlighting on values.
	label := strings.TrimSpace(configPath)
	if label == "" {
		label = "(none)"
	}

	outputPath := strings.TrimSpace("")
	if config != nil {
		outputPath = strings.TrimSpace(config.OutputFilePath)
	}
	if outputPath == "" {
		outputPath = "(auto temp file)"
	}

	//configPrinter.Printf("Config file: %s\n", pterm.LightGreen(label))
	configPrinter.Printf("CLI args: %s", pterm.White(cliArgsString()))
	if config == nil {
		configPrinter.Println("(no workflow configuration loaded)")
		pterm.Println()
		return
	}
	configPrinter.Printf("Host: %s", pterm.LightGreen(config.Host))
	configPrinter.Printf("Port: %s", pterm.LightGreen(fmt.Sprintf("%d", config.Port)))
	configPrinter.Printf("EveryStepDelay: %s", pterm.LightGreen(formatDelayRange(config.EveryStepDelay)))
	configPrinter.Printf("WaitForField: %s", pterm.LightGreen(formatWaitForField(config.WaitForField)))
	configPrinter.Printf("OutputFilePath: %s", pterm.LightGreen(outputPath))
	configPrinter.Printf("RampUpBatchSize: %s", pterm.LightGreen(fmt.Sprintf("%d", config.RampUpBatchSize)))
	configPrinter.Printf("RampUpDelay: %s", pterm.LightGreen(formatSeconds(config.RampUpDelay)))
	configPrinter.Printf("EndOfTaskDelay: %s", pterm.LightGreen(formatDelayRange(config.EndOfTaskDelay)))
	pterm.Println()
}

func resolveTokenPlaceholder(original, token string) string {
	if !strings.Contains(original, "{{token}}") {
		return original
	}

	if token == "" {
		tokenWarningOnce.Do(func() {
			pterm.Warning.Println("Placeholder {{token}} detected in workflow text, but no token value was supplied.")
		})
		return original
	}

	return strings.ReplaceAll(original, "{{token}}", token)
}

var (
	configFile                   string
	injectionConfig              string
	rsaToken                     string
	showHelp                     bool
	runAPI                       bool
	apiPort                      int
	concurrent                   int
	headless                     bool
	verbose                      bool
	verboseFailures              bool
	verboseScreenCaptureFailures bool
	runApp                       string
	runtimeDuration              int
	lastUsedPort                 int
	startPort                    int
	tokenWarningOnce             sync.Once
	promListen                   string
	profileMode                  bool
	profileOut                   string
	profileHost                  string
	profilePort                  int
	profileTLS                   bool
	profileCollectRaw            bool
)

var dashboardStarted bool

// Global counters for metrics.
var totalWorkflowsStarted int64
var totalWorkflowsCompleted int64
var totalWorkflowsFailed int64
var screenCaptureCount int64 // Atomic counter for screen captures (max 5)

var dashboardPort int

var activeWorkflows int
var mutex sync.Mutex
var workflowStatusMu sync.Mutex
var workflowStatuses = make(map[string]*workflowStatus)

type workflowStatus struct {
	ScriptPort  string
	Host        string
	Port        int
	CurrentStep int
	TotalSteps  int
	StepType    string
	StartedAt   time.Time
}

var timingsMutex sync.Mutex
var workflowDurations []float64
var workflowDurationSum float64
var (
	delayRNGMu           sync.Mutex // protects delayRNG for concurrent workflow runs
	delayRNGOnce         sync.Once
	delayRNG             *rand.Rand
	negativeDelayLogOnce sync.Once
)

func init() {
	delayRNG = newDelayRNG()
}

var workflowDurationCount int64

var metricsMutex sync.Mutex
var cpuHistory []float64
var memHistory []float64
var totalCPUUsage float64
var totalCPUSamples int64
var totalMemUsage float64
var totalMemSamples int64
var lastCPUUsage float64
var lastMemUsage float64
var lastCleanupRun time.Time

var showVersion = flag.Bool("version", false, "Show the application version")
var startDashboard = flag.Bool("dashboard", false, "Start the dashboard and open the webpage")

var enableProgressBar bool

func init() {
	flag.BoolVar(&enableProgressBar, "bar", false, "Show progress bars and hide INFO log messages")
	flag.BoolVar(&enableProgressBar, "enableProgressBar", false, "DEPRECATED: use -bar instead")
}

var runAppPort int
var metricsConfigFilePath string
var metricsOutputFilePath string
var workflowTimeout int
var showConnectionErrors bool

type LogEntry struct {
	PID        string    `json:"pid"`
	Parameters string    `json:"parameters"`
	Log        string    `json:"log"`
	Timestamp  time.Time `json:"timestamp"`
}

var inMemoryLogs []LogEntry
var logMutex sync.Mutex

//go:embed templates/dashboard.gohtml
//go:embed templates/static/*
var dashboardTemplateFS embed.FS

var dashboardTemplate *template.Template

var programStart time.Time

func appendLimitedFloat(slice *[]float64, value float64, limit int) {
	*slice = append(*slice, value)
	if limit <= 0 {
		return
	}
	if len(*slice) > limit {
		*slice = (*slice)[len(*slice)-limit:]
	}
}

func appendLimitedLog(slice *[]LogEntry, entry LogEntry, limit int) {
	*slice = append(*slice, entry)
	if limit <= 0 {
		return
	}
	if len(*slice) > limit {
		excess := len(*slice) - limit
		*slice = (*slice)[excess:]
	}
}

func appendLimitedString(slice *[]string, value string, limit int) {
	*slice = append(*slice, value)
	if limit <= 0 {
		return
	}
	if len(*slice) > limit {
		*slice = (*slice)[len(*slice)-limit:]
	}
}

func registerWorkflowStatus(scriptPort string, config *Configuration, totalSteps int) {
	if scriptPort == "" || config == nil {
		return
	}
	workflowStatusMu.Lock()
	workflowStatuses[scriptPort] = &workflowStatus{
		ScriptPort:  scriptPort,
		Host:        config.Host,
		Port:        config.Port,
		CurrentStep: 0,
		TotalSteps:  totalSteps,
		StepType:    "starting",
		StartedAt:   time.Now(),
	}
	workflowStatusMu.Unlock()
}

func updateWorkflowStatus(scriptPort string, stepIndex int, stepType string) {
	if scriptPort == "" {
		return
	}
	workflowStatusMu.Lock()
	if status, ok := workflowStatuses[scriptPort]; ok {
		status.CurrentStep = stepIndex
		status.StepType = stepType
	}
	workflowStatusMu.Unlock()
}

func clearWorkflowStatus(scriptPort string) {
	if scriptPort == "" {
		return
	}
	workflowStatusMu.Lock()
	delete(workflowStatuses, scriptPort)
	workflowStatusMu.Unlock()
}

func snapshotWorkflowStatuses() []workflowStatus {
	workflowStatusMu.Lock()
	defer workflowStatusMu.Unlock()
	statuses := make([]workflowStatus, 0, len(workflowStatuses))
	for _, status := range workflowStatuses {
		if status != nil {
			statuses = append(statuses, *status)
		}
	}
	if len(statuses) < 2 {
		return statuses
	}
	sort.Slice(statuses, func(i, j int) bool {
		return statuses[i].ScriptPort < statuses[j].ScriptPort
	})
	return statuses
}

func splitWorkflowStatuses(statuses []workflowStatus) (connectOnly []workflowStatus, nonConnect []workflowStatus) {
	if len(statuses) == 0 {
		return nil, nil
	}
	for _, status := range statuses {
		if status.StepType == "Connect" {
			connectOnly = append(connectOnly, status)
			continue
		}
		nonConnect = append(nonConnect, status)
	}
	return connectOnly, nonConnect
}

func formatWorkflowStatusLine(status workflowStatus, now time.Time) string {
	stepLabel := status.StepType
	if status.TotalSteps > 0 {
		stepLabel = fmt.Sprintf("%d/%d (%s)", status.CurrentStep, status.TotalSteps, status.StepType)
	}
	elapsed := now.Sub(status.StartedAt).Seconds()
	return fmt.Sprintf("ScriptPort %s | %s:%d | Step %s | Running %s", status.ScriptPort, status.Host, status.Port, stepLabel, formatSeconds(elapsed))
}

func logActiveWorkflowStatuses() {
	statuses := snapshotWorkflowStatuses()
	logWorkflowStatuses(statuses)
}

func logWorkflowStatuses(statuses []workflowStatus) {
	if len(statuses) == 0 {
		return
	}
	pterm.Info.Println("Active workflows still running:")
	for _, status := range statuses {
		line := formatWorkflowStatusLine(status, time.Now())
		pterm.Info.Printf(" - %s", line)
	}
}

func recordWorkflowDuration(duration float64) {
	timingsMutex.Lock()
	appendLimitedFloat(&workflowDurations, duration, workflowDurationHistoryLimit)
	workflowDurationSum += duration
	workflowDurationCount++
	timingsMutex.Unlock()
}

func getAverageWorkflowDuration() float64 {
	timingsMutex.Lock()
	defer timingsMutex.Unlock()
	if workflowDurationCount == 0 {
		return 0
	}
	return workflowDurationSum / float64(workflowDurationCount)
}

func getAverageCPUUsage() float64 {
	metricsMutex.Lock()
	defer metricsMutex.Unlock()
	if totalCPUSamples == 0 {
		return 0
	}
	return totalCPUUsage / float64(totalCPUSamples)
}

func getAverageMemoryUsage() float64 {
	metricsMutex.Lock()
	defer metricsMutex.Unlock()
	if totalMemSamples == 0 {
		return 0
	}
	return totalMemUsage / float64(totalMemSamples)
}

func getLastCPUUsage() float64 {
	metricsMutex.Lock()
	defer metricsMutex.Unlock()
	return lastCPUUsage
}

func getLastMemoryUsage() float64 {
	metricsMutex.Lock()
	defer metricsMutex.Unlock()
	return lastMemUsage
}

func maybeCleanupDashboardArtifacts() {
	metricsMutex.Lock()
	shouldSkip := time.Since(lastCleanupRun) < dashboardCleanupInterval
	if !shouldSkip {
		lastCleanupRun = time.Now()
	}
	metricsMutex.Unlock()
	if shouldSkip {
		return
	}
	dashboardDir := dashboardMetricsDir()
	// Trigger cleanup by reading and evaluating metrics files.
	readDashboardMetrics(dashboardDir)
}

func init() {
	flag.StringVar(&configFile, "config", "workflow.json", "Path to the configuration file")
	flag.StringVar(&injectionConfig, "injectionConfig", "", "Path to the injection configuration file")
	flag.StringVar(&rsaToken, "token", "", "RSA token value to substitute for {{token}} placeholders")
	flag.BoolVar(&showHelp, "help", false, "Show usage information")
	flag.BoolVar(&runAPI, "api", false, "Run as API")
	flag.IntVar(&apiPort, "api-port", 8080, "API port")
	flag.IntVar(&concurrent, "concurrent", 1, "Number of concurrent workflows")
	flag.BoolVar(&headless, "headless", false, "Run go3270 in headless mode")
	flag.BoolVar(&verbose, "verbose", false, "Run go3270 in verbose mode")
	flag.BoolVar(&verboseFailures, "verboseFailures", false, "Log failures even when verbose is off")
	flag.BoolVar(&verboseScreenCaptureFailures, "verboseScreenCaptureFailures", false, "Capture screen on failures (max 5)")
	flag.IntVar(&runtimeDuration, "runtime", 0, "Duration to run workflows in seconds")
	flag.StringVar(&runApp, "runApp", "", "Select which sample 3270 app to run ('1' or '2')")
	flag.IntVar(&runAppPort, "runApp-port", 3270, "Port for the sample 3270 app")
	flag.IntVar(&startPort, "startPort", 5000, "Starting port for workflow connections")
	flag.IntVar(&workflowTimeout, "workflowTimeout", 0, "Hard timeout per workflow in seconds (0 to disable)")
	flag.BoolVar(&showConnectionErrors, "showConnectionErrors", false, "Treat connection failures as errors and report them")
	flag.IntVar(&dashboardPort, "dashboardPort", 9200, "Port for the dashboard server")
	flag.StringVar(&promListen, "promListen", "", "Address to expose Prometheus metrics on /metrics (e.g. \":9091\"). Disabled when empty.")
	flag.BoolVar(&profileMode, "profile", false, "Run as a one-shot host compatibility profiler: connect, probe, write the CompatibilityProfile JSON, and exit.")
	flag.StringVar(&profileOut, "profileOut", "", "When -profile is set, write the JSON to this path instead of stdout.")
	flag.StringVar(&profileHost, "profileHost", "", "Host to profile (overrides workflow config Host when -profile is set).")
	flag.IntVar(&profilePort, "profilePort", 0, "Port to profile (overrides workflow config Port when -profile is set).")
	flag.BoolVar(&profileTLS, "profileTLS", false, "Mark the profiled host as TLS-protected.")
	flag.BoolVar(&profileCollectRaw, "profileCollectRaw", false, "Include raw s3270 Query responses in the profile output.")

	// Set up pterm with a funky theme
	pterm.DefaultSection.Style = pterm.NewStyle(pterm.FgCyan, pterm.Bold)
	pterm.Info.Prefix = Prefix{Text: "INFO", Style: pterm.NewStyle(pterm.BgBlue, pterm.FgWhite)}
	pterm.Error.Prefix = Prefix{Text: "ERROR", Style: pterm.NewStyle(pterm.BgRed, pterm.FgWhite)}
	pterm.Success.Prefix = Prefix{Text: "SUCCESS", Style: pterm.NewStyle(pterm.BgGreen, pterm.FgBlack)}
	pterm.Warning.Prefix = Prefix{Text: "WARNING", Style: pterm.NewStyle(pterm.BgYellow, pterm.FgBlack)}

	configPrinter = &MessagePrinter{
		Prefix: Prefix{Text: "CONFIG", Style: pterm.NewStyle(Color{value: "#7c3aed", background: true}, pterm.FgWhite)},
		style:  lipgloss.NewStyle(),
	}

	if err := os.MkdirAll("logs", 0755); err != nil {
		pterm.Error.Println("Failed to create logs directory:", err)
	}

	var err error
	dashboardTemplate, err = template.ParseFS(dashboardTemplateFS, "templates/dashboard.gohtml")
	if err != nil {
		pterm.Error.Println("Failed to parse dashboard template:", err)
	} else {
		//pterm.Success.Println("Dashboard template loaded - ready to rock!")
	}
}

func storeLog(message string) {
	logMutex.Lock()
	defer logMutex.Unlock()
	pid := os.Getpid()
	args := os.Args[1:]
	parameters := strings.Join(args, " ")

	logEntry := LogEntry{
		PID:        strconv.Itoa(pid),
		Parameters: parameters,
		Log:        message,
		Timestamp:  time.Now(),
	}
	appendLimitedLog(&inMemoryLogs, logEntry, inMemoryLogLimit)

	logFilePath := filepath.Join("logs", fmt.Sprintf("logs_%d.json", pid))
	file, err := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		pterm.Error.Println("Log file opening failed - send help:", err)
		return
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	if err := encoder.Encode(logEntry); err != nil {
		pterm.Error.Println("Failed to encode logs:", err)
	}
}

// getExecutablePath resolves the most up-to-date 3270Connect binary.
func getExecutablePath() string {
	exeName := "3270Connect"
	if runtime.GOOS == "windows" {
		exeName += ".exe"
	}

	// Build candidate list: prefer repo/dist outputs (CI), then local builds, then
	// the currently running binary (when it already is 3270Connect).
	candidates := make([]string, 0, 6)
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(cwd, "dist", exeName),
			filepath.Join(cwd, exeName),
		)
	}
	if runningPath, err := os.Executable(); err == nil {
		baseDir := filepath.Dir(runningPath)
		if filepath.Base(runningPath) == exeName {
			candidates = append(candidates, runningPath)
		}
		candidates = append(candidates,
			filepath.Join(baseDir, exeName),
			filepath.Join(baseDir, "dist", exeName),
		)
	}

	for _, candidate := range candidates {
		if fileExists(candidate) {
			return candidate
		}
	}

	// Fallback to relative paths for environments that expect the legacy layout.
	if runtime.GOOS == "windows" {
		return "./3270Connect.exe"
	}
	return "./3270Connect"
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func loadConfiguration(filePath string) *Configuration {
	//spinner, _ := pterm.DefaultSpinner.Start("Loading config - hold onto your hats!")
	if connect3270.Verbose {
		pterm.Info.Printf("Loading configuration from %s\n", filePath)
	}
	configFile, err := os.Open(filePath)
	if err != nil {
		pterm.Error.Printf("Error opening config file at %s: %v", filePath, err)
		os.Exit(1)
	}
	defer configFile.Close()
	config := Configuration{
		WaitForField: WaitForFieldConfig{Enabled: true, Delay: 1.0, Retries: 10}, // default to waiting after Connect unless disabled in config
	}
	decoder := json.NewDecoder(configFile)
	err = decoder.Decode(&config)
	if err != nil {
		pterm.Error.Printf("Error decoding config JSON: %v", err)
	}
	if config.RampUpBatchSize <= 0 {
		config.RampUpBatchSize = 10
	}
	if config.RampUpDelay <= 0 {
		config.RampUpDelay = 1.0
	}
	err = validateConfiguration(&config)
	if err != nil {
		pterm.Error.Printf("Invalid configuration: %v", err)
	}
	//spinner.Success("Config loaded - we’re golden!")
	return &config
}

func loadInputFile(filePath string) ([]Step, error) {
	spinner, _ := pterm.DefaultSpinner.Start("Loading input file - fingers crossed!")
	if connect3270.Verbose {
		pterm.Info.Printf("Loading input file: %s\n", filePath)
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		spinner.Fail("Input file read failed - disk gremlins:", err)
		return nil, fmt.Errorf("error reading input file: %v", err)
	}
	if connect3270.Verbose {
		pterm.Success.Printf("Successfully read input file: %d bytes\n", len(data))
	}
	var steps []Step
	steps = append(steps, Step{Type: "Connect"})
	if connect3270.Verbose {
		pterm.Info.Println("Added initial Connect step")
	}
	lines := strings.Split(string(data), "\n")
	for idx, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if connect3270.Verbose {
			pterm.Info.Printf("Processing line %d: %s", idx+1, line)
		}
		if strings.HasPrefix(line, "yield ps.sendKeys") {
			key := strings.TrimPrefix(line, "yield ps.sendKeys(")
			key = strings.TrimSuffix(key, ");")
			key = strings.Trim(key, "'")
			stepType := ""
			switch key {
			case "ControlKey.TAB":
				stepType = "PressTab"
			case "ControlKey.ENTER":
				stepType = "PressEnter"
			case "ControlKey.F1":
				stepType = "PressPF1"
			case "ControlKey.F2":
				stepType = "PressPF2"
			case "ControlKey.F3":
				stepType = "PressPF3"
			case "ControlKey.F4":
				stepType = "PressPF4"
			case "ControlKey.F5":
				stepType = "PressPF5"
			case "ControlKey.F6":
				stepType = "PressPF6"
			case "ControlKey.F7":
				stepType = "PressPF7"
			case "ControlKey.F8":
				stepType = "PressPF8"
			case "ControlKey.F9":
				stepType = "PressPF9"
			case "ControlKey.F10":
				stepType = "PressPF10"
			case "ControlKey.F11":
				stepType = "PressPF11"
			case "ControlKey.F12":
				stepType = "PressPF12"
			case "ControlKey.F13":
				stepType = "PressPF13"
			case "ControlKey.F14":
				stepType = "PressPF14"
			case "ControlKey.F15":
				stepType = "PressPF15"
			case "ControlKey.F16":
				stepType = "PressPF16"
			case "ControlKey.F17":
				stepType = "PressPF17"
			case "ControlKey.F18":
				stepType = "PressPF18"
			case "ControlKey.F19":
				stepType = "PressPF19"
			case "ControlKey.F20":
				stepType = "PressPF20"
			case "ControlKey.F21":
				stepType = "PressPF21"
			case "ControlKey.F22":
				stepType = "PressPF22"
			case "ControlKey.F23":
				stepType = "PressPF23"
			case "ControlKey.F24":
				stepType = "PressPF24"
			default:
				stepType = "FillString"
			}
			step := Step{Type: stepType, Text: key}
			steps = append(steps, step)
			if connect3270.Verbose {
				pterm.Info.Printf("Added step: %s with text: %s\n", stepType, key)
			}
		} else if strings.HasPrefix(line, "yield wait.forText") {
			parts := strings.Split(line, ",")
			if len(parts) >= 2 {
				text := strings.TrimPrefix(parts[0], "yield wait.forText('")
				text = strings.TrimSuffix(text, "'")
				position := strings.TrimPrefix(parts[1], "new Position(")
				position = strings.TrimSuffix(position, ");")
				posParts := strings.Split(position, ",")
				if len(posParts) == 2 {
					row, errRow := strconv.Atoi(strings.TrimSpace(posParts[0]))
					column, errCol := strconv.Atoi(strings.TrimSpace(posParts[1]))
					if errRow != nil || errCol != nil {
						if connect3270.Verbose {
							pterm.Warning.Printf("Error parsing position in line %d - numbers hate me\n", idx+1)
						}
						continue
					}
					step := Step{
						Type: "CheckValue",
						Coordinates: connect3270.Coordinates{
							Row:    row,
							Column: column,
							Length: len(text),
						},
						Text: text,
					}
					steps = append(steps, step)
					if connect3270.Verbose {
						pterm.Info.Printf("Added CheckValue step: text '%s' at (%d,%d), length %d\n", text, row, column, len(text))
					}
				}
			}
		} else if strings.HasPrefix(line, "// Fill in the first name at row") || strings.HasPrefix(line, "// Fill in the last name at row") {
			parts := strings.Split(line, " ")
			if len(parts) >= 8 {
				row, errRow := strconv.Atoi(parts[6])
				column, errCol := strconv.Atoi(parts[9])
				if errRow != nil || errCol != nil {
					if connect3270.Verbose {
						pterm.Warning.Printf("Error parsing coords in line %d - math is hard\n", idx+1)
					}
					continue
				}
				if idx+1 < len(lines) {
					nextLine := strings.TrimSpace(lines[idx+1])
					if strings.HasPrefix(nextLine, "yield ps.sendKeys") {
						key := strings.TrimPrefix(nextLine, "yield ps.sendKeys(")
						key = strings.TrimSuffix(key, ");")
						key = strings.Trim(key, "'")
						step := Step{
							Type: "FillString",
							Coordinates: connect3270.Coordinates{
								Row:    row,
								Column: column,
							},
							Text: key,
						}
						steps = append(steps, step)
						if connect3270.Verbose {
							pterm.Info.Printf("Added FillString step: text '%s' at (%d,%d)\n", key, row, column)
						}
					}
				}
			}
		}
	}
	steps = append(steps, Step{Type: "Disconnect"})
	if connect3270.Verbose {
		pterm.Info.Println("Added final Disconnect step")
		pterm.DefaultTable.WithHasHeader().WithData(TableData{
			{"Step", "Type", "Text", "Row", "Column", "Length"},
			{"", "", "", "", "", ""}, // Separator
		}).Render()
		for i, step := range steps {
			pterm.DefaultTable.WithData(TableData{
				{strconv.Itoa(i), step.Type, step.Text, strconv.Itoa(step.Coordinates.Row), strconv.Itoa(step.Coordinates.Column), strconv.Itoa(step.Coordinates.Length)},
			}).Render()
		}
	}
	spinner.Success("Input file loaded - we’re cooking with gas!")
	return steps, nil
}

func shouldAutoWaitForField(config *Configuration, step Step, connected bool) bool {
	if config == nil || !config.WaitForField.Enabled || !connected {
		return false
	}
	switch step.Type {
	case "Connect", "Disconnect", "InitializeOutput", "WaitForField", "CheckValue", "AsciiScreenGrab", "StepDelay":
		return false
	default:
		return true
	}
}

// captureFailureScreen captures the terminal screen to a file when a workflow step fails.
// It limits the total number of captures to 5 across all concurrent workflows using an atomic counter.
// Returns the file path if successful, or an empty string if capture was skipped or failed.
func captureFailureScreen(e *connect3270.Emulator, scriptPort string, stepIndex int) string {
	if !verboseScreenCaptureFailures {
		return ""
	}

	// Atomically increment and check the counter
	newCount := atomic.AddInt64(&screenCaptureCount, 1)
	if newCount > 5 {
		// We exceeded the limit, decrement back
		atomic.AddInt64(&screenCaptureCount, -1)
		return ""
	}

	// Generate filename: failure_[scriptPort]_step[stepIndex]_[timestamp].txt
	timestamp := time.Now().Unix()
	filename := fmt.Sprintf("failure_%s_step%d_%d.txt", scriptPort, stepIndex, timestamp)

	// Capture the screen using AsciiScreenGrab with apiMode=true for plain text
	err := e.AsciiScreenGrab(filename, true)
	if err != nil {
		// Failed to capture, decrement the counter
		atomic.AddInt64(&screenCaptureCount, -1)
		return ""
	}

	return filename
}

func runWorkflow(scriptPort int, config *Configuration) error {
	e := connect3270.NewEmulator(config.Host, config.Port, strconv.Itoa(scriptPort))
	return runWorkflowWithEmulator(e, config, time.Time{})
}

func runWorkflowWithEmulator(e *connect3270.Emulator, config *Configuration, overallDeadline time.Time) error {
	// Check if shutdown was requested before starting workflow execution
	if connect3270.ShutdownRequested() {
		return nil // Graceful stop: do not count as started or failed
	}
	// If the run-wide deadline has already passed, skip starting a new workflow.
	if !overallDeadline.IsZero() && time.Now().After(overallDeadline) {
		return nil
	}
	scriptPortLabel := e.ScriptPort
	startTime := time.Now()
	var workflowDeadline time.Time
	if workflowTimeout > 0 {
		workflowDeadline = startTime.Add(time.Duration(workflowTimeout) * time.Second)
	}
	atomic.AddInt64(&totalWorkflowsStarted, 1)
	if connect3270.Verbose {
		pterm.Info.Printf("Starting workflow for scriptPort %s\n", scriptPortLabel)
	}
	storeLog(fmt.Sprintf("Starting workflow for scriptPort %s", scriptPortLabel))
	mutex.Lock()
	activeWorkflows++
	mutex.Unlock()
	metricsPkg.WorkerStarted()
	defer func() {
		mutex.Lock()
		activeWorkflows--
		mutex.Unlock()
		metricsPkg.WorkerStopped()
	}()
	e.Host = config.Host
	e.Port = config.Port

	// Always start from a clean session to avoid reusing stale emulator state between pooled runs.
	_ = e.Disconnect()
	defer e.Disconnect()
	tmpFileName := config.OutputFilePath
	cleanupTempFile := false
	if tmpFileName == "" {
		tmpFile, err := os.CreateTemp("", "workflowOutput_")
		if err != nil {
			return handleError(err, fmt.Sprintf("Temp file creation failed - disk’s playing hide and seek: %v", err))
		}
		tmpFileName = tmpFile.Name()
		tmpFile.Close()
		cleanupTempFile = true
	}
	defer func() {
		if cleanupTempFile {
			os.Remove(tmpFileName)
		}
	}()
	if err := e.InitializeOutput(tmpFileName, runAPI); err != nil {
		return handleError(err, fmt.Sprintf("Failed to initialize output: %v", err))
	}
	workflowFailed := false
	connectFailed := false
	var steps []Step
	var err error
	if config.InputFilePath != "" {
		steps, err = loadInputFile(config.InputFilePath)
		if err != nil {
			return handleError(err, fmt.Sprintf("Failed to load input file: %v\n", err))
		}
	} else {
		steps = config.Steps
	}
	workflowKey := scriptPortLabel
	registerWorkflowStatus(workflowKey, config, len(steps))
	defer clearWorkflowStatus(workflowKey)

	connected := false

	for idx, step := range steps {
		if workflowFailed {
			break
		}
		if !workflowDeadline.IsZero() && time.Now().After(workflowDeadline) {
			workflowFailed = true
			addError(fmt.Errorf("workflow timed out after %ds", time.Since(startTime)/time.Second))
			break
		}
		if connect3270.ShutdownRequested() {
			break
		}
		updateWorkflowStatus(workflowKey, idx+1, step.Type)
		if idx > 0 {
			delay, err := randomDuration(config.EveryStepDelay, true)
			if err != nil {
				addError(err)
			}
			if delay > 0 {
				time.Sleep(delay)
			}
		}
		if shouldAutoWaitForField(config, step, connected) {
			timeout := time.Duration(config.WaitForField.Delay * float64(time.Second))
			if waitErr := e.WaitForField(timeout, config.WaitForField.Retries); waitErr != nil {
				if waitErr.Error() == "shutdown requested" {
					break
				}
				workflowFailed = true
				addError(waitErr)

				// Capture screen if verboseScreenCaptureFailures is enabled
				captureFile := captureFailureScreen(e, scriptPortLabel, idx+1)

				if verboseFailures {
					msg := fmt.Sprintf("Workflow failure on scriptPort %s at step %d (%s): %v", scriptPortLabel, idx+1, step.Type, waitErr)
					if captureFile != "" {
						msg += fmt.Sprintf(" | Screen captured to: %s", captureFile)
					}
					storeLog(msg)
					pterm.Error.Println(msg)
				}
				continue
			}
		}
		stepStart := time.Now()
		err := executeStep(e, step, tmpFileName, config.Token)
		metricsPkg.ObserveStep(step.Type, time.Since(stepStart))
		if err != nil {
			if err.Error() == "shutdown requested" {
				break // Graceful stop: do not count as failure
			}
			if step.Type == "Connect" {
				connectFailed = true
				if showConnectionErrors {
					addError(err)
				}
				break // Stop executing further steps when connection could not be established
			} else {
				workflowFailed = true
				addError(err)

				// Capture screen if verboseScreenCaptureFailures is enabled
				captureFile := captureFailureScreen(e, scriptPortLabel, idx+1)

				if verboseFailures {
					msg := fmt.Sprintf("Workflow failure on scriptPort %s at step %d (%s): %v", scriptPortLabel, idx+1, step.Type, err)
					if captureFile != "" {
						msg += fmt.Sprintf(" | Screen captured to: %s", captureFile)
					}
					storeLog(msg)
					pterm.Error.Println(msg)
				}
			}
		}
		if err == nil {
			if step.Type == "Connect" {
				connected = true
			}
			if step.Type == "Disconnect" {
				connected = false
			}
		}
	}

	if !workflowFailed && !connectFailed && !connect3270.ShutdownRequested() {
		delay, err := randomDuration(config.EndOfTaskDelay, true)
		if err != nil {
			addError(err)
		}
		if delay > 0 {
			delay = capDelayForDeadline(delay, overallDeadline)
			if delay > 0 {
				time.Sleep(delay)
			}
		}
	}

	duration := time.Since(startTime).Seconds()
	recordWorkflowDuration(duration)
	if connect3270.ShutdownRequested() {
		return nil
	}

	if workflowFailed {
		atomic.AddInt64(&totalWorkflowsFailed, 1)
		metricsPkg.IncWorkflow("failure")
	} else if connectFailed {
		if showConnectionErrors {
			msg := fmt.Sprintf("Workflow for scriptPort %s failed to connect; not counted as workflow failure", scriptPortLabel)
			storeLog(msg)
			if connect3270.Verbose {
				pterm.Warning.Println(msg)
			}
		}
		metricsPkg.IncWorkflow("connect_failed")
	} else {
		if connect3270.Verbose {
			storeLog(fmt.Sprintf("Workflow for scriptPort %s completed successfully", scriptPortLabel))
		}
		atomic.AddInt64(&totalWorkflowsCompleted, 1)
		metricsPkg.IncWorkflow("success")
	}
	return nil
}

func newDelayRNG() *rand.Rand {
	seedBytes := make([]byte, 8)
	if _, err := crand.Read(seedBytes); err == nil {
		return rand.New(rand.NewSource(int64(binary.LittleEndian.Uint64(seedBytes))))
	}
	return rand.New(rand.NewSource(time.Now().UnixNano()))
}

func secondsToDuration(seconds float64) time.Duration {
	if seconds <= 0 {
		return 0
	}
	return time.Duration(seconds * float64(time.Second))
}

func capDelayForDeadline(delay time.Duration, deadline time.Time) time.Duration {
	if delay <= 0 || deadline.IsZero() {
		return delay
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return 0
	}
	if remaining < delay {
		return remaining
	}
	return delay
}

func randomDuration(rangeConfig DelayRange, allowZero bool) (time.Duration, error) {
	if rangeConfig.Min < 0 || rangeConfig.Max < 0 {
		err := fmt.Errorf("DelayRange contains negative values; ignoring delay configuration")
		negativeDelayLogOnce.Do(func() {
			if connect3270.Verbose {
				pterm.Warning.Println(err.Error())
			}
			storeLog(err.Error())
			addError(err)
		})
		return 0, err
	}
	min := rangeConfig.Min
	max := rangeConfig.Max
	if min == 0 && max == 0 {
		if allowZero {
			return 0, nil
		}
		return 0, fmt.Errorf("DelayRange requires a positive Min or Max value")
	}
	// If only Min is provided, treat it as a fixed delay.
	if max == 0 {
		max = min
	}
	if max < min {
		return 0, fmt.Errorf("DelayRange Max cannot be less than Min")
	}
	delaySeconds := min
	if max > min {
		delayRNGOnce.Do(func() {
			if delayRNG == nil {
				delayRNG = newDelayRNG()
			}
		})
		delayRNGMu.Lock()
		randomPortion := delayRNG.Float64()
		delayRNGMu.Unlock()
		delaySeconds += randomPortion * (max - min)
	}
	return time.Duration(delaySeconds * float64(time.Second)), nil
}

func runAPIWorkflow() {
	if connect3270.Verbose {
		pterm.Info.Println("Starting API server mode - buckle up!")
	}
	connect3270.Headless = true
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()
	r.SetTrustedProxies(nil)
	r.POST("/api/execute", func(c *gin.Context) {
		workflowConfig := Configuration{WaitForField: WaitForFieldConfig{Enabled: true, Delay: 1.0, Retries: 10}}
		if err := c.ShouldBindJSON(&workflowConfig); err != nil {
			sendErrorResponse(c, http.StatusBadRequest, "Invalid request payload - JSON’s drunk", err)
			return
		}
		if workflowConfig.Token == "" && rsaToken != "" {
			workflowConfig.Token = rsaToken
		}
		if err := validateConfiguration(&workflowConfig); err != nil {
			sendErrorResponse(c, http.StatusBadRequest, "Invalid workflow configuration", err)
			return
		}
		tmpFile, err := os.CreateTemp("", "workflowOutput_")
		if err != nil {
			pterm.Error.Println("Failed to create temporary file:", err)
			sendErrorResponse(c, http.StatusInternalServerError, "Failed to create temp file", err)
			return
		}
		defer tmpFile.Close()
		tmpFileName := tmpFile.Name()
		defer os.Remove(tmpFileName)
		scriptPort, err := getNextAvailablePort()
		if err != nil {
			sendErrorResponse(c, http.StatusServiceUnavailable, "No script port available", err)
			return
		}
		e := connect3270.NewEmulator(workflowConfig.Host, workflowConfig.Port, strconv.Itoa(scriptPort))
		err = e.InitializeOutput(tmpFileName, true)
		if err != nil {
			sendErrorResponse(c, http.StatusInternalServerError, "Failed to initialize output", err)
			return
		}
		connected := false
		for idx, step := range workflowConfig.Steps {
			if idx > 0 {
				delay, err := randomDuration(workflowConfig.EveryStepDelay, true)
				if err != nil {
					sendErrorResponse(c, http.StatusBadRequest, "Invalid delay configuration", err)
					e.Disconnect()
					return
				}
				if delay > 0 {
					time.Sleep(delay)
				}
			}
			if shouldAutoWaitForField(&workflowConfig, step, connected) {
				timeout := time.Duration(workflowConfig.WaitForField.Delay * float64(time.Second))
				if waitErr := e.WaitForField(timeout, workflowConfig.WaitForField.Retries); waitErr != nil {
					sendErrorResponse(c, http.StatusInternalServerError, "WaitForField failed", waitErr)
					e.Disconnect()
					return
				}
			}
			if err := executeStep(e, step, tmpFileName, workflowConfig.Token); err != nil {
				sendErrorResponse(c, http.StatusInternalServerError, fmt.Sprintf("Step '%s' failed - oof", step.Type), err)
				e.Disconnect()
				return
			}
			if step.Type == "Connect" {
				connected = true
			}
			if step.Type == "Disconnect" {
				connected = false
			}
		}
		if delay, err := randomDuration(workflowConfig.EndOfTaskDelay, true); err != nil {
			sendErrorResponse(c, http.StatusBadRequest, "Invalid end-of-task delay", err)
		} else if delay > 0 {
			time.Sleep(delay)
		}
		outputContents, err := e.ReadOutputFile(tmpFileName)
		if err != nil {
			sendErrorResponse(c, http.StatusInternalServerError, "Output read failed - file’s shy", err)
			return
		}
		e.Disconnect()
		c.JSON(http.StatusOK, gin.H{
			"returnCode": http.StatusOK,
			"status":     "okay",
			"message":    "Workflow executed successfully - high five!",
			"output":     outputContents,
		})
	})
	apiAddr := fmt.Sprintf("localhost:%d", apiPort) // Bind to localhost
	pterm.Success.Printf("API server rocking on %s - let’s roll!\n", apiAddr)
	if err := r.Run(apiAddr); err != nil {
		pterm.Error.Printf("API server crashed - send coffee: %v\n", err)
	}
}

func executeStep(e *connect3270.Emulator, step Step, tmpFileName string, token string) error {
	switch step.Type {
	case "InitializeOutput":
		return e.InitializeOutput(tmpFileName, runAPI)
	case "Connect":
		return e.Connect()
	case "CheckValue":
		expected := resolveTokenPlaceholder(step.Text, token)
		value, err := e.GetValue(step.Coordinates.Row, step.Coordinates.Column, step.Coordinates.Length)
		if err != nil {
			return err
		}
		value = strings.TrimSpace(value)
		if value != strings.TrimSpace(expected) {
			return fmt.Errorf("CheckValue failed. Expected: %s, Found: %s", expected, value)
		}
		return nil
	case "FillString":
		text := resolveTokenPlaceholder(step.Text, token)
		if step.Coordinates.Row == 0 && step.Coordinates.Column == 0 {
			return e.SetString(text)
		}
		return e.FillString(step.Coordinates.Row, step.Coordinates.Column, text)
	case "AsciiScreenGrab":
		return e.AsciiScreenGrab(tmpFileName, runAPI)
	case "PressEnter":
		return e.Press(connect3270.Enter)
	case "PressTab":
		return e.Press(connect3270.Tab)
	case "WaitForField":
		timeout := time.Second
		retries := 10
		if step.Delay > 0 {
			timeout = time.Duration(step.Delay * float64(time.Second))
		}
		return e.WaitForField(timeout, retries)
	case "Disconnect":
		if err := e.Disconnect(); err != nil {
			// Disconnect failures often mean the emulator is already gone; don't fail the workflow for that.
			msg := fmt.Sprintf("Disconnect ignored: %v", err)
			if connect3270.Verbose {
				pterm.Warning.Println(msg)
			} else {
				storeLog(msg)
			}
			return nil
		}
		return nil
	case "PressPF1":
		return e.Press(connect3270.F1)
	case "PressPF2":
		return e.Press(connect3270.F2)
	case "PressPF3":
		return e.Press(connect3270.F3)
	case "PressPF4":
		return e.Press(connect3270.F4)
	case "PressPF5":
		return e.Press(connect3270.F5)
	case "PressPF6":
		return e.Press(connect3270.F6)
	case "PressPF7":
		return e.Press(connect3270.F7)
	case "PressPF8":
		return e.Press(connect3270.F8)
	case "PressPF9":
		return e.Press(connect3270.F9)
	case "PressPF10":
		return e.Press(connect3270.F10)
	case "PressPF11":
		return e.Press(connect3270.F11)
	case "PressPF12":
		return e.Press(connect3270.F12)
	case "PressPF13":
		return e.Press(connect3270.F13)
	case "PressPF14":
		return e.Press(connect3270.F14)
	case "PressPF15":
		return e.Press(connect3270.F15)
	case "PressPF16":
		return e.Press(connect3270.F16)
	case "PressPF17":
		return e.Press(connect3270.F17)
	case "PressPF18":
		return e.Press(connect3270.F18)
	case "PressPF19":
		return e.Press(connect3270.F19)
	case "PressPF20":
		return e.Press(connect3270.F20)
	case "PressPF21":
		return e.Press(connect3270.F21)
	case "PressPF22":
		return e.Press(connect3270.F22)
	case "PressPF23":
		return e.Press(connect3270.F23)
	case "PressPF24":
		return e.Press(connect3270.F24)
	case "StepDelay":
		stepDelay, err := randomDuration(step.StepDelay, false)
		if err != nil {
			return err
		}
		if stepDelay <= 0 {
			return fmt.Errorf("StepDelay requires a positive Min or Max value")
		}
		time.Sleep(stepDelay)
		return nil
	default:
		return fmt.Errorf("unknown step type: %s", step.Type)
	}
}

func sendErrorResponse(c *gin.Context, statusCode int, message string, err error) {
	if connect3270.Verbose {
		pterm.Info.Println("Sending error response - oopsie daisy!")
	}
	c.JSON(statusCode, gin.H{
		"returnCode": statusCode,
		"status":     "error",
		"message":    message,
		"error":      err.Error(),
	})
}

func printBanner() {

	clear()

	pterm.RenderBanner("3270Connect", "")
	pterm.Println()
	pterm.Info.Println("Version: " + pterm.LightGreen(version))
	pterm.Info.Println("Website: " + pterm.LightGreen("https://3270.io"))
	pterm.Info.Println("Author: " + pterm.LightGreen("EyUp.io"))
	//pterm.Info.Println("Runtime Environment: " + pterm.LightYellow(getExecutablePath()+" ") + pterm.White(strings.Join(os.Args[1:], " ")))
	pterm.Println()
}

func LaunchEmbeddedIfDoubleClicked() {
	if !*startDashboard {
		//pterm.Warning.Println("Dashboard mode not enabled. Skipping embedded browser launch.")
		return
	}

	//if !isTerminal() {
	*startDashboard = true
	flag.Set("dashboard", "true")
	pterm.Info.Println("Launching dashboard in GUI mode (double-click detected)")

	// Start dashboard in background
	go runDashboard()

	// Give it time to start
	time.Sleep(1 * time.Second)
	storeLog("Starting dashboard mode - no terminal detected")
	// Launch the embedded browser
	openDashboardEmbedded()
	//}
}

func main() {
	flag.Parse()
	metricsConfigFilePath = configFile

	// Wire Prometheus metrics into the emulator. Cheap when -promListen is
	// unset because the collectors are no-ops without a listener.
	connect3270.SetMetricsObserver(connect3270.MetricsObserver{
		ConnectDuration: metricsPkg.ObserveConnectDuration,
	})
	if strings.TrimSpace(promListen) != "" {
		startPrometheusListener(promListen)
	}

	if profileMode {
		runProfileMode()
		return
	}

	printBanner()
	// If no command-line parameters are provided, force dashboard mode.
	if len(os.Args) == 1 {
		*startDashboard = true
		flag.Set("dashboard", "true")
		pterm.Info.Println("No command-line parameters detected. Forcing dashboard mode.")
	}

	// If the dashboard is not started, the program will exit.
	//if *startDashboard {
	//	runDashboard()
	//	os.Exit(0)
	//}
	LaunchEmbeddedIfDoubleClicked()

	mutex.Lock()
	lastUsedPort = startPort
	mutex.Unlock()
	programStart = time.Now()
	if *showVersion {
		pterm.Info.Printf("3270Connect Version: %s \n", version)
		os.Exit(0)
	}
	if showHelp {
		pterm.Info.Printf("3270Connect Version: %s - Here’s the manual!\n", version)
		flag.Usage()
		os.Exit(0)
	}
	setGlobalSettings()
	if concurrent > 1 || runtimeDuration > 0 {
		go runDashboard()
	}
	go monitorSystemUsage()
	if runApp != "" {
		storeLog(fmt.Sprintf("RunApp selected: Sample App %s launched on port %d - PID: %d", runApp, runAppPort, os.Getpid()))
		switch runApp {
		case "1":
			app1.RunApplication(runAppPort)
			return
		case "2":
			app2.RunApplication(runAppPort)
			return
		default:
			pterm.Error.Printf("Invalid runApp value: %s - Did you mean 1 or 2?\n", runApp)
		}
	}

	// Prevent workflows from starting in dashboard-only mode
	if *startDashboard {
		pterm.Info.Println("Dashboard-only mode enabled. Skipping workflow execution.")
		select {} // Keep the program running for the dashboard
	}

	config := loadConfiguration(configFile)
	metricsOutputFilePath = config.OutputFilePath
	if rsaToken != "" {
		config.Token = rsaToken
	}
	if !runAPI {
		printWorkflowMetadata(configFile, config)
	}
	if runAPI {
		runAPIWorkflow()
	} else {
		if concurrent > 1 || runtimeDuration > 0 {
			runConcurrentWorkflows(config, injectionConfig, configFile)

		} else {
			// Load and apply injection data if configured
			if injectionConfig != "" {
				if _, err := os.Stat(injectionConfig); err == nil {
					injectData, loadErr := loadInjectionData(injectionConfig)
					if loadErr != nil {
						pterm.Error.Printf("Failed to load injection data: %v\n", loadErr)
					} else if len(injectData) > 0 {
						pterm.Info.Printf("Loaded %d injection entries from %s\n", len(injectData), injectionConfig)
						// Use the first entry for single workflow execution
						config = injectDynamicValues(config, injectData[0])
					}
				} else {
					pterm.Warning.Printf("Injection file %s not found. Proceeding without injection.\n", injectionConfig)
				}
			}
			runWorkflow(lastUsedPort, config)
			printSingleWorkflowSummary(configFile, config)
		}
		if concurrent > 1 && dashboardStarted {
			pterm.Info.Printf("All workflows completed but the dashboard is still running on port %d. Press Ctrl+C to exit.", dashboardPort)
			select {}
		}
	}
	showErrors()
}

func setGlobalSettings() {
	connect3270.Headless = headless
	connect3270.Verbose = verbose
}

var stopTicker chan struct{}

type workflowJob struct {
	config         *Configuration
	injectionIndex int
}

type injectionLockPool struct {
	mu     sync.Mutex
	inUse  []bool
	cursor int
}

func newInjectionLockPool(size int) *injectionLockPool {
	if size <= 0 {
		return nil
	}
	return &injectionLockPool{
		inUse: make([]bool, size),
	}
}

func (p *injectionLockPool) acquireNext() (int, bool) {
	if p == nil || len(p.inUse) == 0 {
		return -1, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	size := len(p.inUse)
	for i := 0; i < size; i++ {
		idx := (p.cursor + i) % size
		if !p.inUse[idx] {
			p.inUse[idx] = true
			p.cursor = (idx + 1) % size
			return idx, true
		}
	}
	return -1, false
}

func (p *injectionLockPool) release(index int) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if index < 0 || index >= len(p.inUse) {
		return
	}
	p.inUse[index] = false
}

func shouldWarnInjectionConcurrency(injectionEntries, requestedConcurrency int) bool {
	return injectionEntries > 0 && requestedConcurrency > 0 && injectionEntries < requestedConcurrency
}

func logWarningMessage(message string) {
	pterm.Warning.Println(message)
	storeLog(message)
}

type workflowWorker struct {
	id            int
	jobs          <-chan *workflowJob
	wg            *sync.WaitGroup
	emulator      *connect3270.Emulator
	deadline      time.Time
	injectionPool *injectionLockPool
}

func newWorkflowWorker(id int, jobs <-chan *workflowJob, wg *sync.WaitGroup, deadline time.Time, injectionPool *injectionLockPool) *workflowWorker {
	return &workflowWorker{
		id:            id,
		jobs:          jobs,
		wg:            wg,
		emulator:      connect3270.NewEmulator("", 0, ""),
		deadline:      deadline,
		injectionPool: injectionPool,
	}
}

func (w *workflowWorker) releaseInjectionLock(index int) {
	if w.injectionPool != nil && index >= 0 {
		w.injectionPool.release(index)
	}
}

func (w *workflowWorker) start() {
	defer w.wg.Done()
	for job := range w.jobs {
		if job == nil {
			continue
		}
		cfg := job.config
		if cfg == nil {
			w.releaseInjectionLock(job.injectionIndex)
			continue
		}
		// Check if shutdown was requested before starting new workflow
		if connect3270.ShutdownRequested() {
			if connect3270.Verbose {
				storeLog(fmt.Sprintf("Worker %d skipping workflow due to shutdown request", w.id))
			}
			w.releaseInjectionLock(job.injectionIndex)
			continue
		}
		scriptPort, err := getNextAvailablePort()
		if err != nil {
			storeLog(fmt.Sprintf("Worker %d skipping workflow: %v", w.id, err))
			if connect3270.Verbose {
				pterm.Error.Printf("Worker %d skipping workflow: %v\n", w.id, err)
			}
			addError(fmt.Errorf("worker %d: %w", w.id, err))
			w.releaseInjectionLock(job.injectionIndex)
			continue
		}
		w.emulator.ScriptPort = strconv.Itoa(scriptPort)
		if connect3270.Verbose {
			storeLog(fmt.Sprintf("Worker %d using script port %d", w.id, scriptPort))
		}
		w.emulator.Host = cfg.Host
		w.emulator.Port = cfg.Port
		if err := runWorkflowWithEmulator(w.emulator, cfg, w.deadline); err != nil {
			storeLog(fmt.Sprintf("Worker %d workflow error: %v", w.id, err))
			if connect3270.Verbose {
				pterm.Error.Printf("Worker %d workflow error: %v\n", w.id, err)
			}
		}
		w.releaseInjectionLock(job.injectionIndex)
	}
	_ = w.emulator.Disconnect()
}

func runConcurrentWorkflows(config *Configuration, injectionConfig string, configPath string) {
	if runtimeDuration <= 0 {
		pterm.Warning.Println("Runtime duration must be greater than zero for concurrent execution.")
		return
	}
	connect3270.ResetShutdown()
	overallStart := time.Now()
	workerCount := concurrent
	if workerCount <= 0 {
		workerCount = 1
	}
	deadline := overallStart.Add(time.Duration(runtimeDuration) * time.Second)
	jobs := make(chan *workflowJob, workerCount)
	var workerWG sync.WaitGroup

	var injectData []map[string]string
	var injectionPool *injectionLockPool
	if injectionConfig != "" {
		if _, err := os.Stat(injectionConfig); err == nil {
			var loadErr error
			injectData, loadErr = loadInjectionData(injectionConfig)
			if loadErr != nil {
				pterm.Error.Printf("Failed to load injection data: %v\n", loadErr)
				close(jobs)
				workerWG.Wait()
				return
			}
			pterm.Info.Printf("Loaded %d injection entries from %s\n", len(injectData), injectionConfig)
			injectionPool = newInjectionLockPool(len(injectData))
			if shouldWarnInjectionConcurrency(len(injectData), workerCount) {
				logWarningMessage(fmt.Sprintf("Injection entries (%d) are fewer than requested concurrency (%d); starts may be skipped while entries are locked.", len(injectData), workerCount))
			}
		} else {
			pterm.Warning.Printf("Injection file %s not found. Proceeding without injection.\n", injectionConfig)
		}
	}
	if len(injectData) == 0 {
		injectData = []map[string]string{{}}
	}
	for i := 0; i < workerCount; i++ {
		workerWG.Add(1)
		worker := newWorkflowWorker(i, jobs, &workerWG, deadline, injectionPool)
		go worker.start()
	}

	var (
		multi       MultiPrinter
		durationBar *ProgressbarPrinter
		activeBar   *ProgressbarPrinter
		cpuBar      *ProgressbarPrinter
		memBar      *ProgressbarPrinter
	)
	const titleWidth = 30
	padTitle := func(s string) string {
		w := lipgloss.Width(s)
		if w < titleWidth {
			return s + strings.Repeat(" ", titleWidth-w)
		}
		return s
	}
	tickerInterval := time.Second
	if enableProgressBar {
		multi = pterm.DefaultMultiPrinter
		durationBar, _ = pterm.DefaultProgressbar.
			WithTotal(runtimeDuration).
			WithTitle(padTitle("⏱️ Run Duration")).
			WithWriter(multi.NewWriter()).
			WithBarCharacter("-").
			WithBarStyle(pterm.NewStyle(pterm.FgCyan)).
			WithShowPercentage(true).
			WithShowCount(false).
			WithShowElapsedTime(true).
			Start()

		activeBar, _ = pterm.DefaultProgressbar.
			WithTotal(workerCount).
			WithTitle(padTitle("🟢 Active vUsers")).
			WithWriter(multi.NewWriter()).
			WithBarCharacter("=").
			WithBarStyle(pterm.NewStyle(pterm.FgCyan)).
			WithShowPercentage(true).
			WithShowCount(false).
			WithShowElapsedTime(true).
			Start()

		cpuBar, _ = pterm.DefaultProgressbar.
			WithTotal(100).
			WithTitle(padTitle("🧠 CPU Usage")).
			WithWriter(multi.NewWriter()).
			WithBarCharacter("=").
			WithBarStyle(pterm.NewStyle(pterm.FgGreen)).
			WithShowPercentage(true).
			WithShowCount(false).
			WithShowElapsedTime(true).
			Start()

		memBar, _ = pterm.DefaultProgressbar.
			WithTotal(100).
			WithTitle(padTitle("💾 Memory Usage")).
			WithWriter(multi.NewWriter()).
			WithBarCharacter("=").
			WithBarStyle(pterm.NewStyle(pterm.FgGreen)).
			WithShowPercentage(true).
			WithShowCount(false).
			WithShowElapsedTime(true).
			Start()
		pterm.Println()
	} else {
		pterm.Info.Println("Progress bar disabled. Live stats update every 5s (use -bar for gauges).")
		tickerInterval = 5 * time.Second
	}

	deadline = overallStart.Add(time.Duration(runtimeDuration) * time.Second)

	stopTicker = make(chan struct{})
	go func() {
		ticker := time.NewTicker(tickerInterval)
		defer ticker.Stop()
		var lastFailCount int64
		for {
			select {
			case <-ticker.C:
				elapsed := int(time.Since(overallStart).Seconds())
				active := getActiveWorkflows()
				cpuVal := getLastCPUUsage()
				memVal := getLastMemoryUsage()
				started := atomic.LoadInt64(&totalWorkflowsStarted)
				completed := atomic.LoadInt64(&totalWorkflowsCompleted)
				failed := atomic.LoadInt64(&totalWorkflowsFailed)
				totalRows := formatWorkflowTotalsRows(started, completed, failed)

				if enableProgressBar {
					if durationBar != nil {
						durationBar.Current = min(elapsed, runtimeDuration)
						if elapsed < runtimeDuration {
							durationBar.UpdateTitle(padTitle(fmt.Sprintf("⏱️ Run Duration (%ds left)", runtimeDuration-elapsed)))
						} else {
							durationBar.UpdateTitle(padTitle("⏱️ Run Duration (Completed)"))
						}
					}
					if cpuBar != nil {
						cpuBar.Current = int(cpuVal)
					}
					if memBar != nil {
						memBar.Current = int(memVal)
					}
					if activeBar != nil {
						activeBar.Current = active
						activeBar.UpdateTitle(padTitle(fmt.Sprintf("🟢 Active vUsers (%d/%d)", active, workerCount)))
					}
					pterm.RenderProgressBarsWithRows([]*ProgressbarPrinter{activeBar, durationBar, cpuBar, memBar}, totalRows)
				} else {
					row := formatLiveStatsRow(time.Now(), elapsed, runtimeDuration, active, workerCount, started, completed, failed, cpuVal, memVal)
					if failed > lastFailCount {
						pterm.Error.Println(row)
						lastFailCount = failed
					} else {
						pterm.Info.Println(row)
					}
				}

				storeLog(fmt.Sprintf("Elapsed: %d, Active workflows: %d, CPU usage: %.2f%%, Memory usage: %.2f%%", elapsed, active, cpuVal, memVal))
			case <-stopTicker:
				return
			}

			if time.Now().After(deadline) {
				active := getActiveWorkflows()
				started := atomic.LoadInt64(&totalWorkflowsStarted)
				completed := atomic.LoadInt64(&totalWorkflowsCompleted)
				failed := atomic.LoadInt64(&totalWorkflowsFailed)
				if active == 0 && started == completed+failed {
					return
				}
			}
		}
	}()

	injectionCursor := 0
	rampDelay := time.Duration(config.RampUpDelay * float64(time.Second))
	if rampDelay <= 0 {
		rampDelay = time.Second
	}

	stoppedScheduling := false
	for time.Now().Before(deadline) {
		if deadline.Sub(time.Now()) <= rampDelay {
			stoppedScheduling = true
			break // Don't launch new work when we're at/near the deadline; let in-flight finish.
		}
		availableSlots := workerCount - getActiveWorkflows()
		if availableSlots <= 0 {
			time.Sleep(rampDelay)
			continue
		}

		workflowsToStart := min(config.RampUpBatchSize, availableSlots)
		startedThisBatch := 0
		for startedThisBatch < workflowsToStart && time.Now().Before(deadline) {
			injectionIndex := -1
			var cfg *Configuration
			if injectionPool != nil {
				lockedIndex, ok := injectionPool.acquireNext()
				if !ok {
					skippedStarts := workflowsToStart - startedThisBatch
					logWarningMessage(fmt.Sprintf("All %d injection entries are currently in use and locked; skipping %d workflow start(s) this cycle.", len(injectData), skippedStarts))
					break
				}
				injectionIndex = lockedIndex
				cfg = injectDynamicValues(config, injectData[injectionIndex])
			} else {
				cfg = injectDynamicValues(config, injectData[injectionCursor])
				injectionCursor = (injectionCursor + 1) % len(injectData)
			}
			job := &workflowJob{config: cfg, injectionIndex: injectionIndex}
			select {
			case jobs <- job:
				startedThisBatch++
			default:
				if injectionPool != nil && injectionIndex >= 0 {
					injectionPool.release(injectionIndex)
				}
				// Avoid blocking so we can honor the runtime deadline.
				startedThisBatch = workflowsToStart
			}
		}

		active := getActiveWorkflows()
		cpuVal := getLastCPUUsage()
		memVal := getLastMemoryUsage()
		storeLog(fmt.Sprintf("Scheduled %d workflows, active: %d, CPU: %.2f%%, MEM: %.2f%%", startedThisBatch, active, cpuVal, memVal))
		if active < workerCount {
			started := atomic.LoadInt64(&totalWorkflowsStarted)
			completed := atomic.LoadInt64(&totalWorkflowsCompleted)
			failed := atomic.LoadInt64(&totalWorkflowsFailed)
			combinedMsg := formatPowerupRow(time.Now(), overallStart, runtimeDuration, active, workerCount, startedThisBatch, started, completed, failed, cpuVal, memVal)
			infoIfBarsDisabled(combinedMsg)
			storeLog(combinedMsg)
		}

		time.Sleep(rampDelay)
	}
	if stoppedScheduling {
		remain := deadline.Sub(time.Now())
		if remain < 0 {
			remain = 0
		}
		msg := fmt.Sprintf("Stopped scheduling new workflows to honor deadline (%.1fs remaining).", remain.Seconds())
		infoIfBarsDisabled(msg)
		storeLog(msg)
	}

	if stopTicker != nil {
		close(stopTicker)
		stopTicker = nil
	}

	if enableProgressBar {
		elapsed := int(time.Since(overallStart).Seconds())
		started := atomic.LoadInt64(&totalWorkflowsStarted)
		completed := atomic.LoadInt64(&totalWorkflowsCompleted)
		failed := atomic.LoadInt64(&totalWorkflowsFailed)
		totalRows := formatWorkflowTotalsRows(started, completed, failed)
		if durationBar != nil {
			durationBar.WithTotal(elapsed)
			durationBar.Current = elapsed
			const titleWidth = 30
			durationBar.UpdateTitle(padTitle(fmt.Sprintf("⏱️ Run Duration (%ds elapsed)", elapsed)))
		}
		pterm.RenderProgressBarsWithRows([]*ProgressbarPrinter{activeBar, durationBar, cpuBar, memBar}, totalRows)
		pterm.Println()
		multi.Stop()
	}

	pterm.Success.Println("⏱️ Run duration complete. Waiting for current workflows to finish...")
	close(jobs)

	graceDone := make(chan struct{})
	go func() {
		workerWG.Wait()
		close(graceDone)
	}()
	gracePeriod := defaultGracePeriod
	connectOnlyEndedAtRunEnd := 0
	connectOnlyMarked := false
	graceSucceeded := false
	active := getActiveWorkflows()
	if active == 0 {
		<-graceDone
	} else {
		statuses := snapshotWorkflowStatuses()
		connectOnly, nonConnect := splitWorkflowStatuses(statuses)
		if len(nonConnect) == 0 {
			if !connectOnlyMarked {
				connectOnlyEndedAtRunEnd = len(connectOnly)
				connectOnlyMarked = true
			}
			if showConnectionErrors {
				pterm.Warning.Printf("Ending %d workflow(s) still connecting at run end.", len(connectOnly))
				logWorkflowStatuses(connectOnly)
			}
			connect3270.RequestShutdown()
			_ = waitForGraceTimeout(graceDone, gracePeriod)
		} else {
			pterm.Info.Printf("Grace period: waiting up to %s for %d workflow(s) to finish.", formatSeconds(gracePeriod.Seconds()), len(nonConnect))
			graceReader := bufio.NewReader(os.Stdin)
			if !waitForNonConnectCompletion(gracePeriod) {
				_, nonConnect = splitWorkflowStatuses(snapshotWorkflowStatuses())
				for len(nonConnect) > 0 {
					pterm.Warning.Printf("Grace period of %s elapsed; %d workflow(s) still running.", formatSeconds(gracePeriod.Seconds()), len(nonConnect))
					logWorkflowStatuses(nonConnect)
					if !promptToContinueWaiting(graceReader, gracePeriod) {
						connect3270.RequestShutdown()
						pterm.Warning.Println("Shutdown requested. Waiting for workflows to stop...")
						if waitForGraceTimeout(graceDone, gracePeriod) {
							pterm.Success.Println("All workflows stopped after shutdown request.")
						} else {
							pterm.Warning.Println("Grace period elapsed while waiting for workers; forcing shutdown.")
						}
						break
					}
					pterm.Info.Printf("Continuing to wait up to %s for workflows to finish...", formatSeconds(gracePeriod.Seconds()))
					if waitForNonConnectCompletion(gracePeriod) {
						logGracePeriodSuccess(gracePeriod)
						graceSucceeded = true
						waitForWorkersSettle(graceDone)
						break
					}
					_, nonConnect = splitWorkflowStatuses(snapshotWorkflowStatuses())
					if len(nonConnect) == 0 {
						logGracePeriodSuccess(gracePeriod)
						graceSucceeded = true
						waitForWorkersSettle(graceDone)
						break
					}
				}
			} else {
				logGracePeriodSuccess(gracePeriod)
				graceSucceeded = true
				waitForWorkersSettle(graceDone)
			}
		}

		statuses = snapshotWorkflowStatuses()
		connectOnly, nonConnect = splitWorkflowStatuses(statuses)
		if len(nonConnect) == 0 && len(connectOnly) > 0 {
			if !connectOnlyMarked {
				connectOnlyEndedAtRunEnd = len(connectOnly)
				connectOnlyMarked = true
			}
			if showConnectionErrors {
				pterm.Warning.Printf("Ending %d workflow(s) still connecting at run end.\n", len(connectOnly))
				logWorkflowStatuses(connectOnly)
			}
			connect3270.RequestShutdown()
			_ = waitForGraceTimeout(graceDone, gracePeriod)
		}
		if len(nonConnect) > 0 {
			_ = waitForGraceTimeout(graceDone, gracePeriod)
		}
	}
	storeLog("All workflows completed after runtimeDuration ended.")

	avgCPU := getAverageCPUUsage()
	avgMem := getAverageMemoryUsage()
	avgWorkflowTime := getAverageWorkflowDuration()
	finalActive := getActiveWorkflows()
	finalStarted := atomic.LoadInt64(&totalWorkflowsStarted)
	finalCompleted := atomic.LoadInt64(&totalWorkflowsCompleted)
	finalFailed := atomic.LoadInt64(&totalWorkflowsFailed)
	statusSnapshot := snapshotWorkflowStatuses()
	activeFromStatus := len(statusSnapshot)
	connectOnlyExcluded := connectOnlyEndedAtRunEnd
	connectOnlyNow, _ := splitWorkflowStatuses(statusSnapshot)
	if len(connectOnlyNow) > connectOnlyExcluded {
		connectOnlyExcluded = len(connectOnlyNow)
	}
	adjustedStarted := finalStarted - int64(connectOnlyExcluded)
	if adjustedStarted < 0 {
		adjustedStarted = 0
	}
	if activeFromStatus > 0 {
		finalActive = activeFromStatus
	}
	adjustedActive := finalActive - connectOnlyExcluded
	if adjustedActive < 0 {
		adjustedActive = 0
	}
	if graceSucceeded && !connect3270.ShutdownRequested() {
		adjustedActive = 0
	}
	adjustedCompleted := finalCompleted
	if !connect3270.ShutdownRequested() && activeFromStatus == 0 {
		expectedCompleted := adjustedStarted - finalFailed
		if expectedCompleted > adjustedCompleted {
			adjustedCompleted = expectedCompleted
		}
	}

	clear()
	printBanner()
	printWorkflowMetadata(configPath, config)
	pterm.Success.Println("All workflows wrapped up - Time for a victory lap!")
	elapsed := int(time.Since(overallStart).Seconds())
	pterm.Println()
	pterm.DefaultSection.WithStyle(pterm.NewStyle(pterm.FgCyan)).Println("Run Summary - Performance Report")
	pterm.Println()
	pterm.DefaultTable.
		WithHasHeader().
		WithLeftAlignment().
		WithData(TableData{
			{"Metric", "Value", "Status"},
			{"Total Workflows Started", fmt.Sprintf("%d", adjustedStarted), "🚀 Launch Party"},
			{"Total Workflows Completed", fmt.Sprintf("%d", adjustedCompleted), "🏁 Victory Lap"},
			{"Total Workflows Failed", fmt.Sprintf("%d", finalFailed), func() string {
				if finalFailed > 0 {
					return "💥 Gremlins"
				}
				return "🧼 Squeaky"
			}()},
			{"Final Active vUsers", fmt.Sprintf("%d/%d", adjustedActive, workerCount), func() string {
				if adjustedActive > 0 {
					return "🐝 Still Buzzing"
				}
				return "🧘 All Zen"
			}()},
			{"Average CPU Usage", fmt.Sprintf("%.1f%%", avgCPU), cpuStatus(avgCPU)},
			{"Average Memory Usage", fmt.Sprintf("%.1f%%", avgMem), memStatus(avgMem)},
			{"Average Workflow Time", fmt.Sprintf("%.2fs", avgWorkflowTime), "⏱️ Pace Setter"},
			{"Run Duration", fmt.Sprintf("%ds", elapsed), "🛎️ Completed"},
		}).Render()

	summaryText := generateSummaryText(configPath, config, adjustedStarted, adjustedCompleted, finalFailed, adjustedActive, avgCPU, avgMem, avgWorkflowTime, float64(elapsed))
	summaryFile := filepath.Join("logs", fmt.Sprintf("summary_%d.txt", os.Getpid()))
	if err := os.WriteFile(summaryFile, []byte(summaryText), 0644); err != nil {
		pterm.Warning.Printf("Failed to save summary: %v\n", err)
	}

	storeLog("All workflows completed")
	updateMetricsFile()
}

// Helper functions for summary status
func cpuStatus(cpu float64) string {
	switch {
	case cpu < 50:
		return "🧊 Chill"
	case cpu < 80:
		return "🌶️ Toasty"
	default:
		return "🔥 Melting"
	}
}

func memStatus(mem float64) string {
	switch {
	case mem < 50:
		return "🧊 Chill"
	case mem < 80:
		return "🌶️ Toasty"
	default:
		return "🔥 Melting"
	}
}

func generateSummaryText(configPath string, config *Configuration, finalStarted, finalCompleted, finalFailed int64, finalActive int, avgCPU, avgMem, avgWorkflowTime, elapsed float64) string {
	var sb strings.Builder
	sb.WriteString("All workflows wrapped up - Time for a victory lap!\n\n")
	//sb.WriteString("Runtime Environment: ")
	sb.WriteString(runtimeEnvironmentString())
	sb.WriteString("\n")
	sb.WriteString("Workflow Configuration: ")
	sb.WriteString(workflowMetadataText(configPath, config))
	sb.WriteString("\n")
	sb.WriteString("Run Summary - Performance Report\n")
	sb.WriteString(fmt.Sprintf("Total Workflows Started: %d\n", finalStarted))
	sb.WriteString(fmt.Sprintf("Total Workflows Completed: %d\n", finalCompleted))
	sb.WriteString(fmt.Sprintf("Total Workflows Failed: %d\n", finalFailed))
	sb.WriteString(fmt.Sprintf("Final Active vUsers: %d\n", finalActive))
	sb.WriteString(fmt.Sprintf("Average CPU Usage: %.1f%%\n", avgCPU))
	sb.WriteString(fmt.Sprintf("Average Memory Usage: %.1f%%\n", avgMem))
	sb.WriteString(fmt.Sprintf("Average Workflow Time: %.2fs\n", avgWorkflowTime))
	sb.WriteString(fmt.Sprintf("Run Duration: %.0fs\n", elapsed))
	return sb.String()
}

const (
	colWidthTime      = 8
	colWidthActive    = 10
	colWidthStarted   = 6
	colWidthCompleted = 6
	colWidthFailed    = 4
	colWidthElapsed   = 6
	colWidthRemaining = 6
	colWidthCPU       = 8
	colWidthMem       = 8
	colWidthRamp      = 6
	colWidthGap       = 6
)

func formatWorkflowTotalsRows(started, completed, failed int64) []string {
	return []string{
		pterm.FgCyan.Sprintf("🚀 Started   : %d", started),
		pterm.FgLightGreen.Sprintf("🏁 Completed : %d", completed),
		pterm.FgRed.Sprintf("💥 Failed    : %d", failed),
	}
}

func formatLiveStatsRow(ts time.Time, elapsed, runtimeDuration, active, workerCount int, started, completed, failed int64, cpuUsage, memUsage float64) string {
	remaining := max(runtimeDuration-elapsed, 0)
	parts := []string{
		pterm.FgBlue.Sprintf("%-*s", colWidthTime, ts.Format("15:04:05")),
		pterm.FgGreen.Sprintf("%-*s", colWidthActive, fmt.Sprintf("🟢 %d/%d", active, workerCount)),
		pterm.FgCyan.Sprintf("%-*s", colWidthStarted, fmt.Sprintf("🚀 %d", started)),
		pterm.FgLightGreen.Sprintf("%-*s", colWidthCompleted, fmt.Sprintf("🏁 %d", completed)),
		pterm.FgRed.Sprintf("%-*s", colWidthFailed, fmt.Sprintf("💥 %d", failed)),
		pterm.FgYellow.Sprintf("%-*s", colWidthElapsed, fmt.Sprintf("⏱️ %ds", elapsed)),
		pterm.FgMagenta.Sprintf("%-*s", colWidthRemaining, fmt.Sprintf("⏳ %ds", remaining)),
		pterm.FgCyan.Sprintf("%-*s", colWidthCPU, fmt.Sprintf("🧠 %.1f%%", cpuUsage)),
		pterm.FgMagenta.Sprintf("%-*s", colWidthMem, fmt.Sprintf("💾 %.1f%%", memUsage)),
	}
	return strings.Join(parts, " | ")
}

func formatPowerupRow(ts time.Time, overallStart time.Time, runtimeDuration int, active, workerCount, addedThisBatch int, started, completed, failed int64, cpuUsage, memUsage float64) string {
	elapsed := int(time.Since(overallStart).Seconds())
	remaining := max(runtimeDuration-elapsed, 0)
	parts := []string{
		pterm.FgBlue.Sprintf("%-*s", colWidthTime, ts.Format("15:04:05")),
		pterm.FgGreen.Sprintf("%-*s", colWidthActive, fmt.Sprintf("🟢 %d/%d", active, workerCount)),
		pterm.FgCyan.Sprintf("%-*s", colWidthStarted, fmt.Sprintf("🚀 %d", started)),
		pterm.FgLightGreen.Sprintf("%-*s", colWidthCompleted, fmt.Sprintf("🏁 %d", completed)),
		pterm.FgRed.Sprintf("%-*s", colWidthFailed, fmt.Sprintf("💥 %d", failed)),
		pterm.FgYellow.Sprintf("%-*s", colWidthElapsed, fmt.Sprintf("⏱️ %ds", elapsed)),
		pterm.FgMagenta.Sprintf("%-*s", colWidthRemaining, fmt.Sprintf("⏳ %ds", remaining)),
		pterm.FgCyan.Sprintf("%-*s", colWidthCPU, fmt.Sprintf("🧠 %.1f%%", cpuUsage)),
		pterm.FgMagenta.Sprintf("%-*s", colWidthMem, fmt.Sprintf("💾 %.1f%%", memUsage)),
		pterm.FgLightGreen.Sprintf("%-*s", colWidthRamp, fmt.Sprintf("⚡ +%d", addedThisBatch)),
		//pterm.FgYellow.Sprintf("%-*s", colWidthGap, fmt.Sprintf("🪫 %d", workerCount-active)),
	}
	return strings.Join(parts, " | ")
}

func infoIfBarsDisabled(msg string) {
	if enableProgressBar {
		return
	}
	pterm.Info.Println(msg)
}

func infofIfBarsDisabled(format string, args ...interface{}) {
	if enableProgressBar {
		return
	}
	pterm.Info.Printf(format, args...)
}

func waitForGraceTimeout(done <-chan struct{}, gracePeriod time.Duration) bool {
	if gracePeriod <= 0 {
		<-done
		return true
	}
	select {
	case <-done:
		return true
	case <-time.After(gracePeriod):
		return false
	}
}

func waitForCompletionWithGrace(done <-chan struct{}, gracePeriod time.Duration) bool {
	if waitForGraceTimeout(done, gracePeriod) {
		logGracePeriodSuccess(gracePeriod)
		return true
	}
	return false
}

func waitForWorkersSettle(done <-chan struct{}) {
	_ = waitForGraceTimeout(done, 2*time.Second)
}

func waitForNonConnectCompletion(gracePeriod time.Duration) bool {
	if gracePeriod < 0 {
		gracePeriod = 0
	}
	deadline := time.Now().Add(gracePeriod)
	for {
		_, nonConnect := splitWorkflowStatuses(snapshotWorkflowStatuses())
		if len(nonConnect) == 0 {
			return true
		}
		if gracePeriod > 0 && time.Now().After(deadline) {
			return false
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func logGracePeriodSuccess(gracePeriod time.Duration) {
	pterm.Success.Printf("All workflows finished within the %s grace period.\n", formatSeconds(gracePeriod.Seconds()))
}

type stdinResult struct {
	input string
	err   error
}

func promptToContinueWaiting(reader *bufio.Reader, gracePeriod time.Duration) bool {
	for {
		remaining := int(promptTimeout.Seconds())

		pterm.Warning.Printf("Grace period of %s elapsed. Continue waiting? (y/N) [auto-shutdown in %ds]: ",
			formatSeconds(gracePeriod.Seconds()), remaining)

		inputCh := make(chan stdinResult, 1)
		go func() {
			line, err := reader.ReadString('\n')
			inputCh <- stdinResult{input: line, err: err}
		}()

		ticker := time.NewTicker(1 * time.Second)
		deadline := time.Now().Add(promptTimeout)

		timedOut := false
		var result stdinResult
		gotInput := false

	countdownLoop:
		for {
			select {
			case result = <-inputCh:
				gotInput = true
				break countdownLoop
			case <-ticker.C:
				remaining = int(time.Until(deadline).Seconds())
				if remaining <= 0 {
					timedOut = true
					break countdownLoop
				}
				fmt.Printf("\033[2K\r")
				pterm.Warning.Printf("Grace period of %s elapsed. Continue waiting? (y/N) [auto-shutdown in %ds]: ",
					formatSeconds(gracePeriod.Seconds()), remaining)
			}
		}
		ticker.Stop()

		if timedOut {
			fmt.Println()
			pterm.Warning.Println("No response received. Auto-selecting shutdown (N).")
			storeLog("Grace period prompt timed out; auto-selecting shutdown.")
			return false
		}

		if gotInput {
			if result.err != nil {
				pterm.Warning.Printf("Failed to read grace period response: %v\n", result.err)
				storeLog(fmt.Sprintf("Failed to read grace period response: %v", result.err))
				return false
			}
			input := strings.TrimSpace(strings.ToLower(result.input))
			switch input {
			case "y", "yes":
				return true
			case "", "n", "no":
				return false
			default:
				pterm.Info.Println("Please enter y or n.")
				continue
			}
		}
	}
}

func printSingleWorkflowSummary(configPath string, config *Configuration) {
	avgCPU := getAverageCPUUsage()
	avgMem := getAverageMemoryUsage()
	avgWorkflowTime := getAverageWorkflowDuration()

	// Capture final stats
	finalStarted := atomic.LoadInt64(&totalWorkflowsStarted)
	finalCompleted := atomic.LoadInt64(&totalWorkflowsCompleted)
	finalFailed := atomic.LoadInt64(&totalWorkflowsFailed)

	elapsed := int(time.Since(programStart).Seconds())

	pterm.Success.Println("Workflow completed - Time for a victory lap!")
	printWorkflowMetadata(configPath, config)

	// Display summary report
	pterm.DefaultSection.WithStyle(pterm.NewStyle(pterm.FgCyan)).Println("Run Summary - Performance Report")
	pterm.DefaultTable.
		WithHasHeader().
		WithLeftAlignment().
		WithData(TableData{
			{"Metric", "Value", "Status"},
			{"Total Workflows Started", fmt.Sprintf("%d", finalStarted), "🚀 Launch Party"},
			{"Total Workflows Completed", fmt.Sprintf("%d", finalCompleted), "🏁 Victory Lap"},
			{"Total Workflows Failed", fmt.Sprintf("%d", finalFailed), func() string {
				if finalFailed > 0 {
					return "💥 Gremlins"
				}
				return "🧼 Squeaky"
			}()},
			{"Average CPU Usage", fmt.Sprintf("%.1f%%", avgCPU), cpuStatus(avgCPU)},
			{"Average Memory Usage", fmt.Sprintf("%.1f%%", avgMem), memStatus(avgMem)},
			{"Average Workflow Time", fmt.Sprintf("%.2fs", avgWorkflowTime), "⏱️ Pace Setter"},
			{"Run Duration", fmt.Sprintf("%ds", elapsed), "🛎️ Completed"},
		}).Render()

	// Save summary to file
	summaryText := generateSummaryText(configPath, config, finalStarted, finalCompleted, finalFailed, 0, avgCPU, avgMem, avgWorkflowTime, float64(elapsed))
	summaryFile := filepath.Join("logs", fmt.Sprintf("summary_%d.txt", os.Getpid()))
	if err := os.WriteFile(summaryFile, []byte(summaryText), 0644); err != nil {
		pterm.Warning.Printf("Failed to save summary: %v\n", err)
	}

	storeLog("Workflow completed")
	updateMetricsFile()
}

func clear() {
	print("\033[H\033[2J")
}

var errPortRangeExhausted = errors.New("no available script port in configured range")

func getNextAvailablePort() (int, error) {
	const (
		maxPort      = 65000
		waitInterval = 100 * time.Millisecond
		maxWait      = 30 * time.Second
	)
	deadline := time.Now().Add(maxWait)
	for {
		mutex.Lock()
		checked := 0
		rangeSize := maxPort - startPort + 1
		for checked < rangeSize {
			lastUsedPort++
			if lastUsedPort > maxPort {
				lastUsedPort = startPort
			}
			if isPortAvailable(lastUsedPort) {
				port := lastUsedPort
				mutex.Unlock()
				return port, nil
			}
			checked++
			if connect3270.Verbose {
				pterm.Warning.Printf("Port %d in use, trying next\n", lastUsedPort)
			}
		}
		mutex.Unlock()
		if time.Now().After(deadline) {
			return 0, errPortRangeExhausted
		}
		time.Sleep(waitInterval)
	}
}

func isPortAvailable(port int) bool {
	addr := ":" + strconv.Itoa(port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		if connect3270.Verbose {
			pterm.Info.Printf("Port %d in use\n", port)
		}
		return false
	}
	ln.Close()
	return true
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func validateDelayRange(name string, dr DelayRange, allowZero bool) error {
	if dr.Min < 0 || dr.Max < 0 {
		return fmt.Errorf("%s must be zero or positive", name)
	}
	if dr.Max > 0 && dr.Min > dr.Max {
		return fmt.Errorf("%s Min cannot be greater than Max", name)
	}
	if !allowZero && dr.Min == 0 && dr.Max == 0 {
		return fmt.Errorf("%s requires a positive Min or Max value", name)
	}
	return nil
}

func validateConfiguration(config *Configuration) error {
	if connect3270.Verbose {
		pterm.Info.Println("Validating config - let’s see if it’s naughty or nice!")
	}
	if config.Host == "" {
		return fmt.Errorf("host is empty - where’s the party at?")
	}
	if config.Port <= 0 {
		return fmt.Errorf("port is invalid - ports cant be negative silly")
	}
	if config.LegacyDelay > 0 {
		return fmt.Errorf("Delay is no longer supported; use EveryStepDelay.Min/Max instead")
	}
	if err := validateDelayRange("EveryStepDelay", config.EveryStepDelay, true); err != nil {
		return err
	}
	if err := validateDelayRange("EndOfTaskDelay", config.EndOfTaskDelay, true); err != nil {
		return err
	}
	if config.OutputFilePath == "" {
		hasScreenGrab := false
		for _, step := range config.Steps {
			if step.Type == "AsciiScreenGrab" {
				hasScreenGrab = true
				break
			}
		}
		if hasScreenGrab {
			return fmt.Errorf("output file path is empty - screen grab needs a home")
		}
	}

	for _, step := range config.Steps {
		if step.Type == "HumanDelay" {
			return fmt.Errorf("HumanDelay is no longer supported; use StepDelay with Min/Max instead")
		}
		// Allow steps that do not require additional configuration.
		if step.Type == "Connect" ||
			step.Type == "AsciiScreenGrab" ||
			step.Type == "PressEnter" ||
			step.Type == "PressTab" ||
			step.Type == "WaitForField" ||
			step.Type == "Disconnect" ||
			step.Type == "StepDelay" ||
			(strings.HasPrefix(step.Type, "PressPF")) {
			if step.Type == "StepDelay" {
				if err := validateDelayRange("StepDelay", step.StepDelay, false); err != nil {
					return err
				}
			}
			continue
		}
		// Steps that require coordinates and text.
		if step.Type == "CheckValue" || step.Type == "FillString" {
			if step.Coordinates.Row == 0 || step.Coordinates.Column == 0 {
				return fmt.Errorf("coords missing in %s step - lost in space", step.Type)
			}
			if step.Text == "" {
				return fmt.Errorf("text empty in %s step - cat got your tongue?", step.Type)
			}
			continue
		}
		// Unknown step type.
		return fmt.Errorf("unknown step type: %s - what’s this nonsense?", step.Type)
	}
	return nil
}

func runDashboard() {

	// Serve embedded static files
	staticFiles, err := fs.Sub(dashboardTemplateFS, "templates/static")
	if err != nil {
		pterm.Error.Println("Failed to load embedded static files:", err)
		return
	}
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFiles))))

	// Register the start-process endpoint
	http.HandleFunc("/start-process", startProcessHandler)
	http.HandleFunc("/kill", killProcessHandler) // register kill endpoint
	http.HandleFunc("/test-connection", testConnectionHandler)

	addr := fmt.Sprintf("localhost:%d", dashboardPort) // Bind to localhost
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		//pterm.Warning.Printf("Dashboard already vibing on port %d - skipping the encore!\n", dashboardPort)
		go func() {
			for {
				updateMetricsFile()
				time.Sleep(2 * time.Second)
			}
		}()
		return
	}
	dashboardStarted = true
	//openDashboardEmbedded()
	spinner, _ := pterm.DefaultSpinner.WithRemoveWhenDone(true).Start("Cleaning up old metrics - sweeping the floor!")
	dashboardDir := dashboardMetricsDir()
	files, err := filepath.Glob(filepath.Join(dashboardDir, "metrics_*.json"))
	if err != nil {
		spinner.Warning("Error listing old metrics - file system’s trolling:", err)
	} else {
		for _, f := range files {
			if err := os.Remove(f); err != nil {
				pterm.Warning.Printf("Failed to yeet old metrics file %s: %v\n", f, err)
			} else {
				//pterm.Info.Printf("Old metrics file %s gone - poof!\n", f)
			}
		}
	}
	logFiles, err := filepath.Glob(filepath.Join("logs", "logs_*.json"))
	if err == nil {
		for _, lf := range logFiles {
			if err := os.Remove(lf); err != nil {
				//pterm.Warning.Printf("Failed to nuke old log file %s: %v\n", lf, err)
			} else {
				//pterm.Info.Printf("Old log file %s vaporized!\n", lf)
			}
		}
	}
	spinner.Success("Cleanup done - dashboard’s fresh as a daisy!")

	setupConsoleHandler()
	setupTerminalConsoleHandler()
	setupWorkflowPreviewHandler()
	setupOutputPreviewHandler()
	setupSummaryHandler()
	http.HandleFunc("/dashboard", func(w http.ResponseWriter, r *http.Request) {
		// Check if the dashboardTemplate is nil
		if dashboardTemplate == nil {
			pterm.Error.Println("Dashboard template is nil. Ensure the template is loaded correctly.")
			http.Error(w, "Internal Server Error: Dashboard template not loaded", http.StatusInternalServerError)
			return
		}

		metricsList, extendedList := readDashboardMetrics(dashboardDir)
		metricsJSON, _ := json.Marshal(metricsList)
		autoRefresh := r.URL.Query().Get("autoRefresh")
		refreshPeriod := r.URL.Query().Get("refreshPeriod")
		if refreshPeriod == "" {
			refreshPeriod = "5"
		}
		checked := ""
		if autoRefresh == "true" {
			checked = "checked"
		}
		sel1, sel5, sel10, sel15, sel30 := "", "", "", "", ""
		switch refreshPeriod {
		case "1":
			sel1 = "selected"
		case "5":
			sel5 = "selected"
		case "10":
			sel10 = "selected"
		case "15":
			sel15 = "selected"
		case "30":
			sel30 = "selected"
		}
		agg := aggregateExtendedMetrics(extendedList)
		extendedJSON, err := json.Marshal(extendedList)
		if err != nil {
			pterm.Error.Printf("Error marshaling extended metrics: %v\n", err)
		}

		data := struct {
			ActiveWorkflows                 int
			TotalWorkflowsStarted           int64
			TotalWorkflowsCompleted         int64
			TotalWorkflowsFailed            int64
			Checked                         string
			Sel1, Sel5, Sel10, Sel15, Sel30 string
			Year                            int
			AutoRefreshEnabled              bool
			RefreshPeriod                   string
			MetricsJSON                     string
			ExtendedMetricsList             []ExtendedMetrics
			ExtendedJSON                    string
			Version                         string
		}{
			ActiveWorkflows:         agg.ActiveWorkflows,
			TotalWorkflowsStarted:   agg.TotalWorkflowsStarted,
			TotalWorkflowsCompleted: agg.TotalWorkflowsCompleted,
			TotalWorkflowsFailed:    agg.TotalWorkflowsFailed,
			Checked:                 checked,
			Sel1:                    sel1,
			Sel5:                    sel5,
			Sel10:                   sel10,
			Sel15:                   sel15,
			Sel30:                   sel30,
			Year:                    time.Now().Year(),
			AutoRefreshEnabled:      autoRefresh == "true",
			RefreshPeriod:           refreshPeriod,
			MetricsJSON:             string(metricsJSON),
			ExtendedMetricsList:     extendedList,
			ExtendedJSON:            string(extendedJSON),
			Version:                 version, // Holds the value of the const `version`
		}
		// Use a buffer to write the template output first, then write it all at once
		// This prevents partial responses from being written if the connection closes
		var buf bytes.Buffer
		if err := dashboardTemplate.Execute(&buf, data); err != nil {
			pterm.Error.Printf("Dashboard template execution failed - HTML's throwing a tantrum: %v\n", err)
			// Only try to send error if we haven't written to the response yet
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		// Write the complete response at once
		if _, err := buf.WriteTo(w); err != nil {
			// Connection was closed by client, just log it without the scary message
			// This is normal when browser refreshes or navigates away
			if connect3270.Verbose {
				pterm.Warning.Printf("Client closed connection during dashboard response: %v\n", err)
			}
		}
	})
	http.HandleFunc("/dashboard/data", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		_, extendedList := readDashboardMetrics(dashboardDir)

		// Prefer live processes for UI stats; fall back to latest snapshot if nothing running.
		filtered := make([]ExtendedMetrics, 0, len(extendedList))
		for _, m := range extendedList {
			if m.IsRunning {
				filtered = append(filtered, m)
			}
		}
		if len(filtered) == 0 {
			filtered = extendedList
		}

		payload := struct {
			AggregatedMetrics Metrics           `json:"aggregated"`
			ExtendedMetrics   []ExtendedMetrics `json:"extendedMetrics"`
			Timestamp         int64             `json:"timestamp"`
		}{
			AggregatedMetrics: aggregateExtendedMetrics(filtered),
			ExtendedMetrics:   filtered,
			Timestamp:         time.Now().Unix(),
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(payload); err != nil {
			pterm.Warning.Printf("Failed to marshal dashboard data response: %v\n", err)
		}
	})
	pterm.Info.Printf("Dashboard live at %s - check it out!\n", pterm.FgBlue.Sprintf("http://localhost:%d/dashboard", dashboardPort))
	pterm.Println()
	go func() {
		for {
			updateMetricsFile()
			time.Sleep(2 * time.Second)
		}
	}()
	if err := http.Serve(listener, nil); err != nil {
		pterm.Error.Printf("Dashboard server crashed - send a medic: %v\n", err)
	}
}

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
}

type ExtendedMetrics struct {
	Metrics
	Status    string `json:"status"`
	TimeLeft  int64  `json:"timeLeft"`
	IsRunning bool   `json:"isRunning"`
}

func isProcessRunning(pid int) bool {
	if pid <= 0 {
		return false
	}
	if runtime.GOOS == "windows" {
		cmd := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid))
		output, err := cmd.Output()
		if err != nil {
			pterm.Warning.Printf("Failed to query tasklist for pid %d: %v\n", pid, err)
			return true
		}
		return bytes.Contains(output, []byte(fmt.Sprintf("%d", pid)))
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		return false
	}
	return true
}

func shouldCleanupMetric(m ExtendedMetrics, modTime time.Time) bool {
	if m.IsRunning || m.Status != "Killed" {
		return false
	}
	// Only clean up long-dead "killed" entries to avoid nuking live stats.
	if modTime.IsZero() {
		return false
	}
	return time.Since(modTime) > 10*time.Minute
}

func cleanupProcessArtifacts(pid int, metricsFile string) {
	if metricsFile != "" {
		if err := os.Remove(metricsFile); err != nil && !os.IsNotExist(err) {
			pterm.Warning.Printf("Failed to remove stale metrics file %s for pid %d: %v\n", metricsFile, pid, err)
		}
	}
	logFilePath := filepath.Join("logs", fmt.Sprintf("logs_%d.json", pid))
	if err := os.Remove(logFilePath); err != nil && !os.IsNotExist(err) {
		pterm.Warning.Printf("Failed to remove stale log file %s for pid %d: %v\n", logFilePath, pid, err)
	}
}

func dashboardMetricsDir() string {
	configDir, err := os.UserConfigDir()
	if err != nil {
		pterm.Warning.Printf("User config directory unavailable, defaulting to local dashboard folder: %v\n", err)
		return filepath.Join(".", "dashboard")
	}
	return filepath.Join(configDir, "3270Connect", "dashboard")
}

func readDashboardMetrics(baseDir string) ([]Metrics, []ExtendedMetrics) {
	files, err := filepath.Glob(filepath.Join(baseDir, "metrics_*.json"))
	if err != nil {
		pterm.Warning.Printf("Error listing metrics files from %s: %v\n", baseDir, err)
		return nil, nil
	}
	var metricsList []Metrics
	var extendedList []ExtendedMetrics
	for _, f := range files {
		fi, err := os.Stat(f)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			pterm.Warning.Printf("Stat on metrics file %s failed: %v\n", f, err)
			continue
		}

		data, err := os.ReadFile(f)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			pterm.Warning.Printf("Error reading metrics file %s: %v\n", f, err)
			continue
		}
		var m Metrics
		if err := json.Unmarshal(data, &m); err != nil {
			pterm.Warning.Printf("Error unmarshaling metrics %s: %v\n", f, err)
			continue
		}
		extendedMetric := m.extend()
		if shouldCleanupMetric(extendedMetric, fi.ModTime()) {
			cleanupProcessArtifacts(extendedMetric.PID, f)
			continue
		}
		metricsList = append(metricsList, m)
		extendedList = append(extendedList, extendedMetric)
	}
	return metricsList, extendedList
}

func aggregateExtendedMetrics(metrics []ExtendedMetrics) Metrics {
	var agg Metrics
	for _, metric := range metrics {
		agg.ActiveWorkflows += metric.ActiveWorkflows
		agg.TotalWorkflowsStarted += metric.TotalWorkflowsStarted
		agg.TotalWorkflowsCompleted += metric.TotalWorkflowsCompleted
		agg.TotalWorkflowsFailed += metric.TotalWorkflowsFailed
		agg.Durations = append(agg.Durations, metric.Durations...)
		agg.CPUUsage = append(agg.CPUUsage, metric.CPUUsage...)
		agg.MemoryUsage = append(agg.MemoryUsage, metric.MemoryUsage...)
	}
	return agg
}

func updateMetricsFile() {
	metricsMutex.Lock()
	cpuCopy := make([]float64, len(cpuHistory))
	copy(cpuCopy, cpuHistory)
	memCopy := make([]float64, len(memHistory))
	copy(memCopy, memHistory)
	metricsMutex.Unlock()

	timingsMutex.Lock()
	durationsCopy := make([]float64, len(workflowDurations))
	copy(durationsCopy, workflowDurations)
	timingsMutex.Unlock()

	// Fallback sampling in case monitorSystemUsage hasn't populated history yet.
	if len(cpuCopy) == 0 {
		if cpuPercents, err := cpu.Percent(0, false); err == nil && len(cpuPercents) > 0 {
			cpuCopy = append(cpuCopy, cpuPercents[0])
		}
	}
	if len(memCopy) == 0 {
		if memStats, err := mem.VirtualMemory(); err == nil && memStats != nil {
			memCopy = append(memCopy, memStats.UsedPercent)
		}
	}

	pid := os.Getpid()
	args := os.Args[1:]
	parameters := strings.Join(args, " ")
	configPath := metricsConfigFilePath
	if configPath == "" {
		configPath = configFile
	}
	if configPath != "" {
		if absPath, err := filepath.Abs(configPath); err == nil {
			configPath = absPath
		}
	}
	outputPath := metricsOutputFilePath
	if outputPath != "" {
		if absPath, err := filepath.Abs(outputPath); err == nil {
			outputPath = absPath
		}
	}
	metrics := Metrics{
		PID:                     pid,
		ActiveWorkflows:         getActiveWorkflows(),
		TotalWorkflowsStarted:   atomic.LoadInt64(&totalWorkflowsStarted),
		TotalWorkflowsCompleted: atomic.LoadInt64(&totalWorkflowsCompleted),
		TotalWorkflowsFailed:    atomic.LoadInt64(&totalWorkflowsFailed),
		Durations:               durationsCopy,
		CPUUsage:                cpuCopy,
		MemoryUsage:             memCopy,
		Params:                  parameters,
		RuntimeDuration:         runtimeDuration,
		StartTimestamp: func() int64 {
			if programStart.IsZero() {
				return time.Now().Unix()
			}
			return programStart.Unix()
		}(),
		ConfigFilePath: configPath,
		OutputFilePath: outputPath,
	}

	// Process extended metrics by using the extend() method on metrics.
	extendedMetrics := metrics.extend()

	data, err := json.Marshal(extendedMetrics)
	if err != nil {
		pterm.Warning.Printf("Extended metrics marshaling failed for pid %d - JSON’s sulking: %v\n", pid, err)
		return
	}
	dashboardDir := dashboardMetricsDir()
	os.MkdirAll(dashboardDir, 0755)
	filePath := filepath.Join(dashboardDir, fmt.Sprintf("metrics_%d.json", pid))
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		pterm.Warning.Printf("Metrics file write failed for pid %d - disk’s grumpy: %v\n", pid, err)
	}
	maybeCleanupDashboardArtifacts()
}

func aggregateMetrics() Metrics {
	dashboardDir, err := os.UserConfigDir()
	if err != nil {
		pterm.Warning.Println("User config dir fetch failed:", err)
		dashboardDir = filepath.Join(".", "dashboard")
	} else {
		dashboardDir = filepath.Join(dashboardDir, "3270Connect", "dashboard")
	}
	files, err := filepath.Glob(filepath.Join(dashboardDir, "metrics_*.json"))
	if err != nil {
		pterm.Warning.Println("Metrics files listing failed:", err)
		return Metrics{}
	}
	var agg Metrics
	for _, f := range files {
		// Check if file exists before attempting to read it
		fi, err := os.Stat(f)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			pterm.Warning.Printf("Stat on metrics file %s failed: %v\n", f, err)
			continue
		}

		data, err := os.ReadFile(f)
		if err != nil {
			// File may have been deleted between Stat and ReadFile, silently continue
			if os.IsNotExist(err) {
				continue
			}
			pterm.Warning.Printf("Reading file %s failed: %v\n", f, err)
			continue
		}
		var m Metrics
		if err := json.Unmarshal(data, &m); err != nil {
			pterm.Warning.Printf("Unmarshaling file %s failed: %v\n", f, err)
			continue
		}
		extendedMetric := m.extend()
		if shouldCleanupMetric(extendedMetric, fi.ModTime()) {
			cleanupProcessArtifacts(extendedMetric.PID, f)
			continue
		}
		agg.TotalWorkflowsStarted += extendedMetric.TotalWorkflowsStarted
		agg.TotalWorkflowsCompleted += extendedMetric.TotalWorkflowsCompleted
		agg.TotalWorkflowsFailed += extendedMetric.TotalWorkflowsFailed
		agg.ActiveWorkflows += extendedMetric.ActiveWorkflows
		agg.Durations = append(agg.Durations, extendedMetric.Durations...)
		agg.CPUUsage = append(agg.CPUUsage, extendedMetric.CPUUsage...)
		agg.MemoryUsage = append(agg.MemoryUsage, extendedMetric.MemoryUsage...)
		agg.RuntimeDuration = extendedMetric.RuntimeDuration // Keep last or overwrite
		agg.StartTimestamp = extendedMetric.StartTimestamp
	}
	return agg
}

func (m Metrics) extend() ExtendedMetrics {
	timeElapsed := time.Now().Unix() - m.StartTimestamp
	timeLeft := int64(m.RuntimeDuration) - timeElapsed
	if timeLeft < 0 {
		timeLeft = 0
	}
	status := "Running" // Default status for missing or incomplete metrics
	isRunning := isProcessRunning(m.PID)
	completedOrFailed := m.TotalWorkflowsCompleted + m.TotalWorkflowsFailed
	allWorkflowsAccounted := m.TotalWorkflowsStarted > 0 &&
		completedOrFailed >= m.TotalWorkflowsStarted &&
		m.ActiveWorkflows == 0
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

	return ExtendedMetrics{
		Metrics:   m,
		Status:    status,
		TimeLeft:  timeLeft,
		IsRunning: isRunning,
	}
}

func monitorSystemUsage() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		cpuPercents, err := cpu.Percent(0, false)
		if err == nil && len(cpuPercents) > 0 {
			var sum float64
			for _, p := range cpuPercents {
				sum += p
			}
			overall := sum / float64(len(cpuPercents))
			metricsMutex.Lock()
			appendLimitedFloat(&cpuHistory, overall, cpuHistoryLimit)
			totalCPUUsage += overall
			totalCPUSamples++
			lastCPUUsage = overall
			metricsMutex.Unlock()
		}
		memStats, err := mem.VirtualMemory()
		if err == nil && memStats != nil {
			metricsMutex.Lock()
			appendLimitedFloat(&memHistory, memStats.UsedPercent, memHistoryLimit)
			totalMemUsage += memStats.UsedPercent
			totalMemSamples++
			lastMemUsage = memStats.UsedPercent
			metricsMutex.Unlock()
		}

		// Keep dashboard system interface metrics fresh even if the dashboard update loop isn't running.
		updateMetricsFile()
	}
}

func setupConsoleHandler() {
	http.HandleFunc("/console", func(w http.ResponseWriter, r *http.Request) {
		pidFilter := r.URL.Query().Get("pid")
		var filtered []LogEntry
		if pidFilter != "" {
			logFilePath := filepath.Join("logs", fmt.Sprintf("logs_%s.json", pidFilter))
			file, err := os.Open(logFilePath)
			if err != nil {
				if os.IsNotExist(err) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					json.NewEncoder(w).Encode([]LogEntry{})
					return
				}
				pterm.Warning.Printf("Log file opening failed for PID %s: %v\n", pidFilter, err)
				http.Error(w, "Error opening log file", http.StatusInternalServerError)
				return
			}
			defer file.Close()
			decoder := json.NewDecoder(file)
			for {
				var logEntry LogEntry
				if err := decoder.Decode(&logEntry); err != nil {
					if err == io.EOF {
						break
					}
					pterm.Warning.Println("Log entry decoding failed:", err)
					http.Error(w, "Error decoding log entry", http.StatusInternalServerError)
					return
				}
				filtered = append(filtered, logEntry)
			}
		} else {
			logFiles, err := filepath.Glob(filepath.Join("logs", "logs_*.json"))
			if err == nil {
				for _, lf := range logFiles {
					file, err := os.Open(lf)
					if err != nil {
						pterm.Warning.Printf("Log file %s opening failed: %v\n", lf, err)
						continue
					}
					func() {
						defer file.Close()
						decoder := json.NewDecoder(file)
						for {
							var logEntry LogEntry
							if err := decoder.Decode(&logEntry); err != nil {
								if err == io.EOF {
									break
								}
								pterm.Warning.Println("Log entry decoding failed:", err)
								break // Exit decoding loop on error
							}
							filtered = append(filtered, logEntry)
						}
					}()
				}
			}
		}
		sort.Slice(filtered, func(i, j int) bool {
			return filtered[i].Timestamp.After(filtered[j].Timestamp)
		})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(filtered)
	})
}

func setupTerminalConsoleHandler() {
	http.HandleFunc("/terminal-console", func(w http.ResponseWriter, r *http.Request) {
		pidFilter := r.URL.Query().Get("pid")
		var filtered []LogEntry
		if pidFilter != "" {
			logFilePath := filepath.Join("logs", fmt.Sprintf("logs_%s.json", pidFilter))
			file, err := os.Open(logFilePath)
			if err != nil {
				if os.IsNotExist(err) {
					w.Header().Set("Content-Type", "text/plain")
					w.WriteHeader(http.StatusOK)
					return
				}
				pterm.Warning.Printf("Log file opening failed for PID %s: %v\n", pidFilter, err)
				http.Error(w, "Error opening log file", http.StatusInternalServerError)
				return
			}
			defer file.Close()
			decoder := json.NewDecoder(file)
			for {
				var logEntry LogEntry
				if err := decoder.Decode(&logEntry); err != nil {
					if err == io.EOF {
						break
					}
					pterm.Warning.Println("Log entry decoding failed:", err)
					http.Error(w, "Error decoding log entry", http.StatusInternalServerError)
					return
				}
				filtered = append(filtered, logEntry)
			}
		} else {
			logFiles, err := filepath.Glob(filepath.Join("logs", "logs_*.json"))
			if err == nil {
				for _, lf := range logFiles {
					file, err := os.Open(lf)
					if err != nil {
						pterm.Warning.Printf("Log file %s opening failed: %v\n", lf, err)
						continue
					}
					func() {
						defer file.Close()
						decoder := json.NewDecoder(file)
						for {
							var logEntry LogEntry
							if err := decoder.Decode(&logEntry); err != nil {
								if err == io.EOF {
									break
								}
								pterm.Warning.Println("Log entry decoding failed:", err)
								break // Exit decoding loop on error
							}
							filtered = append(filtered, logEntry)
						}
					}()
				}
			}
		}
		sort.Slice(filtered, func(i, j int) bool {
			return filtered[i].Timestamp.After(filtered[j].Timestamp)
		})
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		for _, entry := range filtered {
			w.Write([]byte(fmt.Sprintf("%s\n", entry.Log)))
		}
	})
}

func setupWorkflowPreviewHandler() {
	http.HandleFunc("/dashboard/workflow", func(w http.ResponseWriter, r *http.Request) {
		pid := r.URL.Query().Get("pid")
		metric, err := loadExtendedMetricByPID(pid)
		if err != nil {
			if os.IsNotExist(err) {
				http.Error(w, "No metrics file found for PID "+pid, http.StatusNotFound)
			} else {
				http.Error(w, "Unable to load metrics: "+err.Error(), http.StatusInternalServerError)
			}
			return
		}
		configPath := metric.ConfigFilePath
		if configPath == "" {
			http.Error(w, "Workflow configuration is not available for PID "+pid, http.StatusNotFound)
			return
		}
		file, err := os.Open(configPath)
		if err != nil {
			if os.IsNotExist(err) {
				http.Error(w, "Workflow file not found: "+configPath, http.StatusNotFound)
			} else {
				http.Error(w, "Failed to open workflow file: "+err.Error(), http.StatusInternalServerError)
			}
			return
		}
		defer file.Close()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if _, err := io.Copy(w, file); err != nil {
			http.Error(w, "Failed to stream workflow file: "+err.Error(), http.StatusInternalServerError)
		}
	})
}

func setupOutputPreviewHandler() {
	http.HandleFunc("/dashboard/output", func(w http.ResponseWriter, r *http.Request) {
		pid := r.URL.Query().Get("pid")
		metric, err := loadExtendedMetricByPID(pid)
		if err != nil {
			if os.IsNotExist(err) {
				http.Error(w, "No metrics file found for PID "+pid, http.StatusNotFound)
			} else {
				http.Error(w, "Unable to load metrics: "+err.Error(), http.StatusInternalServerError)
			}
			return
		}
		outputPath := metric.OutputFilePath
		if outputPath == "" {
			http.Error(w, "Output file path is not configured for PID "+pid, http.StatusNotFound)
			return
		}
		file, err := os.Open(outputPath)
		if err != nil {
			if os.IsNotExist(err) {
				http.Error(w, "Output file not found: "+outputPath, http.StatusNotFound)
			} else {
				http.Error(w, "Failed to open output file: "+err.Error(), http.StatusInternalServerError)
			}
			return
		}
		defer file.Close()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if _, err := io.Copy(w, file); err != nil {
			http.Error(w, "Failed to stream output file: "+err.Error(), http.StatusInternalServerError)
		}
	})
}

func setupSummaryHandler() {
	http.HandleFunc("/dashboard/summary", func(w http.ResponseWriter, r *http.Request) {
		pid := r.URL.Query().Get("pid")
		summaryFile := filepath.Join("logs", fmt.Sprintf("summary_%s.txt", pid))
		file, err := os.Open(summaryFile)
		if err != nil {
			http.Error(w, "Summary not found", http.StatusNotFound)
			return
		}
		defer file.Close()
		w.Header().Set("Content-Type", "text/plain")
		io.Copy(w, file)
	})
}

func loadExtendedMetricByPID(pid string) (*ExtendedMetrics, error) {
	if pid == "" {
		return nil, fmt.Errorf("missing pid")
	}
	dir := dashboardMetricsDir()
	filePath := filepath.Join(dir, fmt.Sprintf("metrics_%s.json", pid))
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	var metric ExtendedMetrics
	if err := json.Unmarshal(data, &metric); err != nil {
		return nil, err
	}
	return &metric, nil
}

func getActiveWorkflows() int {
	mutex.Lock()
	defer mutex.Unlock()
	return activeWorkflows
}

func showErrors() {
	errorMutex.Lock()
	defer errorMutex.Unlock()
	if len(errorList) == 0 {
		pterm.Println()
		pterm.Info.Println("No errors encountered during the workflows.")
		return
	}

	pterm.Error.Println("Errors Summary:")
	errorCount := make(map[string]int)
	for _, err := range errorList {
		errorCount[err.Error()]++
	}

	for errMsg, count := range errorCount {
		pterm.Error.Printf("%d occurrence(s) of: %s\n", count, errMsg)
	}
}

func handleError(err error, message string) error {
	pterm.Error.Println(message)
	addError(err)
	return err
}

func addError(err error) {
	errorMutex.Lock()
	defer errorMutex.Unlock()
	errorList = append(errorList, err)
}

// Add a new endpoint to handle the process initiation request
func startProcessHandler(w http.ResponseWriter, r *http.Request) {
	storeLog("Received start process request")
	if r.Method != http.MethodPost {
		storeLog("Invalid request method for start process")
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	// Check for sample app parameters
	runApp := strings.TrimSpace(r.FormValue("runApp"))
	if runApp != "" {
		storeLog("Sample app mode detected")
		runAppPort := strings.TrimSpace(r.FormValue("runAppPort"))
		if _, err := strconv.Atoi(runApp); err != nil {
			http.Error(w, "Invalid runApp value", http.StatusBadRequest)
			return
		}
		if port, err := strconv.Atoi(runAppPort); err != nil || port < 1 || port > 65535 {
			http.Error(w, "Invalid runAppPort value", http.StatusBadRequest)
			return
		}
		executablePath := getExecutablePath()
		args := []string{"-runApp", runApp, "-runApp-port", runAppPort}
		go func() {
			logLine := fmt.Sprintf("%s %s", executablePath, strings.Join(args, " "))
			pterm.Info.Printf("Executing sample app command: %s\n", logLine)
			storeLog("Executing sample app command: " + logLine)

			cmd := exec.Command(executablePath, args...)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				pterm.Error.Printf("Failed to execute sample app command: %v\n", err)
			}
		}()
		storeLog("Sample app started successfully")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Sample app started successfully"))
		return
	}

	storeLog("Processing normal workflow")
	// Normal workflow: retrieve the uploaded file
	if err := r.ParseMultipartForm(10 << 20); err != nil { // 10 MB max file size
		http.Error(w, "Failed to parse form data", http.StatusBadRequest)
		return
	}

	file, handler, err := r.FormFile("configFile")
	if err != nil {
		http.Error(w, "Failed to retrieve file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	fileBytes, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "Failed to read configuration file", http.StatusInternalServerError)
		return
	}

	var config Configuration
	if err := json.Unmarshal(fileBytes, &config); err != nil {
		storeLog("Failed to parse configuration JSON: " + err.Error())
		http.Error(w, "Invalid configuration file", http.StatusBadRequest)
		return
	}

	if override := strings.TrimSpace(r.FormValue("overrideHost")); override != "" {
		config.Host = override
	}
	if override := strings.TrimSpace(r.FormValue("overridePort")); override != "" {
		portValue, convErr := strconv.Atoi(override)
		if convErr != nil {
			http.Error(w, "Invalid port override", http.StatusBadRequest)
			return
		}
		config.Port = portValue
	}
	if override := strings.TrimSpace(r.FormValue("overrideOutputFilePath")); override != "" {
		cleaned := filepath.Clean(override)
		if filepath.IsAbs(cleaned) || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) || cleaned == ".." {
			http.Error(w, "overrideOutputFilePath must be a relative path within the working directory", http.StatusBadRequest)
			return
		}
		config.OutputFilePath = cleaned
	}
	if override := strings.TrimSpace(r.FormValue("overrideRampUpBatchSize")); override != "" {
		batchValue, convErr := strconv.Atoi(override)
		if convErr != nil {
			http.Error(w, "Invalid ramp up batch size override", http.StatusBadRequest)
			return
		}
		config.RampUpBatchSize = batchValue
	}
	if override := strings.TrimSpace(r.FormValue("overrideRampUpDelay")); override != "" {
		delayValue, convErr := strconv.ParseFloat(override, 64)
		if convErr != nil {
			http.Error(w, "Invalid ramp up delay override", http.StatusBadRequest)
			return
		}
		config.RampUpDelay = delayValue
	}

	if err := validateConfiguration(&config); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	updatedJSON, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		http.Error(w, "Failed to serialize configuration", http.StatusInternalServerError)
		return
	}

	safeConfigName := filepath.Base(handler.Filename)
	if safeConfigName == "" || safeConfigName == "." || safeConfigName == string(filepath.Separator) {
		http.Error(w, "Invalid configuration filename", http.StatusBadRequest)
		return
	}
	tempFilePath := filepath.Join(os.TempDir(), safeConfigName)
	if err := os.WriteFile(tempFilePath, updatedJSON, 0644); err != nil {
		http.Error(w, "Failed to save file", http.StatusInternalServerError)
		return
	}

	// Retrieve the injection configuration file (optional)
	var injectionConfigPath string
	injectionFile, injectionHandler, err := r.FormFile("injectionConfig")
	if err == nil {
		defer injectionFile.Close()
		safeInjectionName := filepath.Base(injectionHandler.Filename)
		if safeInjectionName == "" || safeInjectionName == "." || safeInjectionName == string(filepath.Separator) {
			http.Error(w, "Invalid injection configuration filename", http.StatusBadRequest)
			return
		}
		injectionConfigPath = filepath.Join(os.TempDir(), safeInjectionName)
		injectionTempFile, err := os.Create(injectionConfigPath)
		if err != nil {
			http.Error(w, "Failed to save injection configuration file", http.StatusInternalServerError)
			return
		}
		defer injectionTempFile.Close()

		if _, err := io.Copy(injectionTempFile, injectionFile); err != nil {
			http.Error(w, "Failed to save injection configuration file", http.StatusInternalServerError)
			return
		}
	}

	// Retrieve other form fields
	concurrent := r.FormValue("concurrent")
	runtime := r.FormValue("runtime")
	startPort := r.FormValue("startPort")
	headless := r.FormValue("headless") == "on" // use "on" for checked
	tokenValue := strings.TrimSpace(r.FormValue("token"))

	commandArgs := []string{
		getExecutablePath(),
		"-config", tempFilePath,
		"-concurrent", concurrent,
		"-runtime", runtime,
		"-startPort", startPort,
	}
	if headless {
		commandArgs = append(commandArgs, "-headless")
	}
	if injectionConfigPath != "" {
		commandArgs = append(commandArgs, "-injectionConfig", injectionConfigPath)
	}
	if tokenValue != "" {
		commandArgs = append(commandArgs, "-token", tokenValue)
	}

	maskedArgs := make([]string, len(commandArgs))
	copy(maskedArgs, commandArgs)
	for i := 0; i < len(maskedArgs); i++ {
		if maskedArgs[i] == "-token" && i+1 < len(maskedArgs) {
			maskedArgs[i+1] = "[REDACTED]"
		}
	}
	commandForLog := strings.Join(maskedArgs, " ")
	storeLog("Command to execute: " + commandForLog)
	go func(args []string, logCommand string) {
		pterm.Info.Printf("Executing command: %s\n", logCommand)

		cmd := exec.Command(args[0], args[1:]...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			pterm.Error.Printf("Failed to execute command: %v\n", err)
		}
	}(commandArgs, commandForLog)
	storeLog("Process started successfully")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Process started successfully"))
}

func testConnectionHandler(w http.ResponseWriter, r *http.Request) {
	storeLog("Received test connection request")
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	defer r.Body.Close()
	var payload struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	}

	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		storeLog("Failed to decode test connection payload: " + err.Error())
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	host := strings.TrimSpace(payload.Host)
	if host == "" {
		http.Error(w, "Host is required", http.StatusBadRequest)
		return
	}
	if payload.Port <= 0 {
		http.Error(w, "Port must be a positive integer", http.StatusBadRequest)
		return
	}

	address := net.JoinHostPort(host, strconv.Itoa(payload.Port))
	storeLog("Testing connectivity to " + address)
	conn, err := net.DialTimeout("tcp", address, 5*time.Second)
	if err != nil {
		storeLog("Connection test failed for " + address + ": " + err.Error())
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "error",
			"message": fmt.Sprintf("Unable to connect to %s: %v", address, err),
		})
		return
	}
	conn.Close()
	storeLog("Connection test succeeded for " + address)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"message": fmt.Sprintf("Successfully connected to %s", address),
	})
}

func isTerminal() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	// Check if Stdout is a character device
	return (fi.Mode() & os.ModeCharDevice) != 0
}

func killProcessHandler(w http.ResponseWriter, r *http.Request) {
	storeLog("Received kill request")
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	pidStr := r.URL.Query().Get("pid")
	if pidStr == "" {
		http.Error(w, "Missing PID", http.StatusBadRequest)
		return
	}
	storeLog("Attempting to kill process with PID: " + pidStr)
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		storeLog("Invalid PID: " + pidStr)
		http.Error(w, "Invalid PID", http.StatusBadRequest)
		return
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		storeLog("Process not found: " + pidStr)
		http.Error(w, "Process not found", http.StatusNotFound)
		return
	}
	if pid == os.Getpid() {
		storeLog("Attempting to kill the dashboard process itself")
		http.Error(w, "Cannot kill the dashboard process itself", http.StatusForbidden)
		return
	}
	if err := proc.Kill(); err != nil {
		storeLog("Failed to kill process gracefully, attempting hard kill for PID: " + pidStr)
		var hardKillErr error
		if runtime.GOOS == "windows" {
			hardKillErr = exec.Command("taskkill", "/PID", pidStr, "/F").Run()
		} else {
			hardKillErr = exec.Command("kill", "-9", pidStr).Run()
		}
		if hardKillErr != nil {
			storeLog("Failed to hard kill process: " + pidStr)
			http.Error(w, "Failed to kill process", http.StatusInternalServerError)
			return
		}
	}

	// Update the metrics file to reflect the "Killed" status
	updateKilledStatus(pid)

	// Force the dashboard to reload the updated metrics
	updateMetricsFile()

	storeLog("Process killed successfully PID: " + pidStr)
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Process killed successfully"))
}

func updateKilledStatus(pid int) {
	//pterm.Info.Printf("Updating killed status for process with PID %d\n", pid)
	storeLog(fmt.Sprintf("Updating killed status for process with PID %d", pid))

	dashboardDir, err := os.UserConfigDir()
	if err != nil {
		pterm.Warning.Println("Failed to get UserConfigDir, defaulting to local dashboard directory")
		dashboardDir = filepath.Join(".", "dashboard")
	} else {
		dashboardDir = filepath.Join(dashboardDir, "3270Connect", "dashboard")
	}
	metricsFile := filepath.Join(dashboardDir, fmt.Sprintf("metrics_%d.json", pid))
	//pterm.Info.Printf("Reading metrics file: %s\n", metricsFile)
	storeLog(fmt.Sprintf("Reading metrics file: %s", metricsFile))

	data, err := os.ReadFile(metricsFile)
	if err != nil {
		pterm.Warning.Printf("Failed to read metrics file for PID %d: %v\n", pid, err)
		storeLog(fmt.Sprintf("Failed to read metrics file for PID %d: %v", pid, err))
		return
	}
	var metrics Metrics
	if err := json.Unmarshal(data, &metrics); err != nil {
		pterm.Warning.Printf("Failed to unmarshal metrics for PID %d: %v\n", pid, err)
		storeLog(fmt.Sprintf("Failed to unmarshal metrics for PID %d: %v", pid, err))
		return
	}

	//pterm.Info.Printf("Clearing active workflows for PID %d\n", pid)
	storeLog(fmt.Sprintf("Clearing active workflows for killed PID %d", pid))
	// Only clear active workflows - preserve execution statistics for accurate aggregation
	metrics.ActiveWorkflows = 0

	extendedMetrics := metrics.extend()
	extendedMetrics.Status = "Killed"

	updatedData, err := json.Marshal(extendedMetrics)
	if err != nil {
		pterm.Warning.Printf("Failed to marshal updated metrics for PID %d: %v\n", pid, err)
		storeLog(fmt.Sprintf("Failed to marshal updated metrics for PID %d: %v", pid, err))
		return
	}
	if err := os.WriteFile(metricsFile, updatedData, 0644); err != nil {
		pterm.Warning.Printf("Failed to write updated metrics for PID %d: %v\n", pid, err)
		storeLog(fmt.Sprintf("Failed to write updated metrics for PID %d: %v", pid, err))
		return
	}
	//pterm.Info.Printf("Successfully updated metrics for PID %d to status 'Killed'\n", pid)
	storeLog(fmt.Sprintf("Successfully updated metrics for PID %d to status 'Killed'", pid))
}

func loadInjectionData(filePath string) ([]map[string]string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var raw interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse injection data: %w", err)
	}

	convertEntries := func(items []interface{}) ([]map[string]string, error) {
		entries := make([]map[string]string, 0, len(items))
		for idx, item := range items {
			obj, ok := item.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("injection entry %d must be an object", idx)
			}
			entry := make(map[string]string, len(obj))
			for key, val := range obj {
				entry[key] = fmt.Sprint(val)
			}
			entries = append(entries, entry)
		}
		if len(entries) == 0 {
			return nil, fmt.Errorf("injection data contains no entries")
		}
		return entries, nil
	}

	switch v := raw.(type) {
	case []interface{}:
		return convertEntries(v)
	case map[string]interface{}:
		// Support wrappers like {"entries": [...] } or {"data": [...]}.
		if entriesVal, ok := v["entries"]; ok {
			if arr, ok := entriesVal.([]interface{}); ok {
				return convertEntries(arr)
			}
			return nil, fmt.Errorf("injection 'entries' must be an array")
		}
		if dataVal, ok := v["data"]; ok {
			if arr, ok := dataVal.([]interface{}); ok {
				return convertEntries(arr)
			}
			return nil, fmt.Errorf("injection 'data' must be an array")
		}
		// Treat plain object as a single entry.
		entry := make(map[string]string, len(v))
		for key, val := range v {
			entry[key] = fmt.Sprint(val)
		}
		if len(entry) == 0 {
			return nil, fmt.Errorf("injection object is empty")
		}
		return []map[string]string{entry}, nil
	default:
		return nil, fmt.Errorf("unsupported injection data format")
	}
}

func injectDynamicValues(config *Configuration, injection map[string]string) *Configuration {
	newConfig := *config // Create a copy of the configuration
	newConfig.Steps = make([]Step, len(config.Steps))
	copy(newConfig.Steps, config.Steps)

	for i, step := range newConfig.Steps {
		for placeholder, value := range injection {
			if strings.Contains(step.Text, placeholder) {
				newConfig.Steps[i].Text = strings.ReplaceAll(newConfig.Steps[i].Text, placeholder, value)
			}
		}
	}

	return &newConfig
}
