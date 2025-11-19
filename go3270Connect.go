package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"io/ioutil"
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
	"github.com/3270io/3270Connect/sampleapps/app1"
	app2 "github.com/3270io/3270Connect/sampleapps/app2"

	"github.com/jchv/go-webview2"

	"github.com/gin-gonic/gin"
	"github.com/pterm/pterm"
	"github.com/pterm/pterm/putils"
	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/mem"
)

const version = "1.6.2"

var errorList []error
var errorMutex sync.Mutex

// Configuration holds the settings for the terminal connection and the steps to be executed.
type Configuration struct {
	Host            string
	Port            int
	OutputFilePath  string `json:"OutputFilePath"`
	Steps           []Step
	Token           string  `json:"Token,omitempty"`
	InputFilePath   string  `json:"InputFilePath"`
	RampUpBatchSize int     `json:"RampUpBatchSize"`
	RampUpDelay     float64 `json:"RampUpDelay"`
}

// Step represents an individual action to be taken on the terminal.
type Step struct {
	Type        string
	Coordinates connect3270.Coordinates
	Text        string
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
	configFile       string
	injectionConfig  string
	rsaToken         string
	showHelp         bool
	runAPI           bool
	apiPort          int
	concurrent       int
	headless         bool
	verbose          bool
	runApp           string
	runtimeDuration  int
	lastUsedPort     int
	startPort        int
	tokenWarningOnce sync.Once
)

var dashboardStarted bool

// Global counters for metrics.
var totalWorkflowsStarted int64
var totalWorkflowsCompleted int64
var totalWorkflowsFailed int64

var dashboardPort int

var activeWorkflows int
var mutex sync.Mutex

var timingsMutex sync.Mutex
var workflowDurations []float64

var cpuHistory []float64
var memHistory []float64

var showVersion = flag.Bool("version", false, "Show the application version")
var startDashboard = flag.Bool("dashboard", false, "Start the dashboard and open the webpage")

var enableProgressBar bool

var runAppPort int
var metricsConfigFilePath string
var metricsOutputFilePath string

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
	flag.IntVar(&runtimeDuration, "runtime", 0, "Duration to run workflows in seconds")
	flag.StringVar(&runApp, "runApp", "", "Select which sample 3270 app to run ('1' or '2')")
	flag.IntVar(&runAppPort, "runApp-port", 3270, "Port for the sample 3270 app")
	flag.IntVar(&startPort, "startPort", 5000, "Starting port for workflow connections")
	flag.IntVar(&dashboardPort, "dashboardPort", 9200, "Port for the dashboard server")
	flag.BoolVar(&enableProgressBar, "enableProgressBar", false, "Enable progress bar and hide INFO log messages")

	// Set up pterm with a funky theme
	pterm.DefaultSection.Style = pterm.NewStyle(pterm.FgCyan, pterm.Bold)
	pterm.Info.Prefix = pterm.Prefix{Text: "INFO", Style: pterm.NewStyle(pterm.BgBlue, pterm.FgWhite)}
	pterm.Error.Prefix = pterm.Prefix{Text: "ERROR", Style: pterm.NewStyle(pterm.BgRed, pterm.FgWhite)}
	pterm.Success.Prefix = pterm.Prefix{Text: "SUCCESS", Style: pterm.NewStyle(pterm.BgGreen, pterm.FgBlack)}
	pterm.Warning.Prefix = pterm.Prefix{Text: "WARNING", Style: pterm.NewStyle(pterm.BgYellow, pterm.FgBlack)}

	if err := os.MkdirAll("logs", 0755); err != nil {
		pterm.Error.Println("Failed to create logs dir - universe says no:", err)
	}

	var err error
	dashboardTemplate, err = template.ParseFS(dashboardTemplateFS, "templates/dashboard.gohtml")
	if err != nil {
		pterm.Error.Println("Dashboard template parsing went kaput:", err)
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
	inMemoryLogs = append(inMemoryLogs, logEntry)

	logFilePath := filepath.Join("logs", fmt.Sprintf("logs_%d.json", pid))
	file, err := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		pterm.Error.Println("Log file opening failed - send help:", err)
		return
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	if err := encoder.Encode(logEntry); err != nil {
		pterm.Error.Println("Log encoding broke - computers hate me:", err)
	}
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
	var config Configuration
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
	data, err := ioutil.ReadFile(filePath)
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
		pterm.DefaultTable.WithHasHeader().WithData(pterm.TableData{
			{"Step", "Type", "Text", "Row", "Column", "Length"},
			{"", "", "", "", "", ""}, // Separator
		}).Render()
		for i, step := range steps {
			pterm.DefaultTable.WithData(pterm.TableData{
				{strconv.Itoa(i), step.Type, step.Text, strconv.Itoa(step.Coordinates.Row), strconv.Itoa(step.Coordinates.Column), strconv.Itoa(step.Coordinates.Length)},
			}).Render()
		}
	}
	spinner.Success("Input file loaded - we’re cooking with gas!")
	return steps, nil
}

func runWorkflow(scriptPort int, config *Configuration) error {
	startTime := time.Now()
	atomic.AddInt64(&totalWorkflowsStarted, 1)
	if connect3270.Verbose {
		pterm.Info.Printf("Starting workflow for scriptPort %d\n", scriptPort)
	}
	storeLog(fmt.Sprintf("Starting workflow for scriptPort %d", scriptPort))
	mutex.Lock()
	activeWorkflows++
	mutex.Unlock()
	e := connect3270.NewEmulator(config.Host, config.Port, strconv.Itoa(scriptPort))
	tmpFile, err := ioutil.TempFile("", "workflowOutput_")
	if err != nil {
		return handleError(err, fmt.Sprintf("Temp file creation failed - disk’s playing hide and seek: %v", err))
	}
	tmpFileName := tmpFile.Name()
	tmpFile.Close()
	e.InitializeOutput(tmpFileName, runAPI)
	workflowFailed := false
	var steps []Step
	if config.InputFilePath != "" {
		steps, err = loadInputFile(config.InputFilePath)
		if err != nil {
			return handleError(err, fmt.Sprintf("Input file load crashed - file has gone rogue: %v\n", err))
		}
	} else {
		steps = config.Steps
	}

	for _, step := range steps {
		if workflowFailed {
			break
		}
		err := executeStep(e, step, tmpFileName, config.Token)
		if err != nil {
			workflowFailed = true
			addError(err)
		}
	}

	mutex.Lock()
	activeWorkflows--
	mutex.Unlock()
	duration := time.Since(startTime).Seconds()
	timingsMutex.Lock()
	workflowDurations = append(workflowDurations, duration)
	timingsMutex.Unlock()

	if workflowFailed {
		atomic.AddInt64(&totalWorkflowsFailed, 1)
	} else {
		if connect3270.Verbose {
			storeLog(fmt.Sprintf("Workflow for scriptPort %d completed successfully", scriptPort))
		}
		if config.OutputFilePath != "" {
			_ = os.Remove(config.OutputFilePath)
			if err := os.Rename(tmpFileName, config.OutputFilePath); err != nil {
				pid := os.Getpid()
				uniqueOutputPath := fmt.Sprintf("%s.%d", config.OutputFilePath, pid)
				if err2 := os.Rename(tmpFileName, uniqueOutputPath); err2 != nil {
					addError(err2)
				} else if verbose {
					pterm.Info.Printf("Renamed to unique output file: %s\n", uniqueOutputPath)
				}
				return err
			}
		}
		atomic.AddInt64(&totalWorkflowsCompleted, 1)
	}
	return nil
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
		var workflowConfig Configuration
		if err := c.ShouldBindJSON(&workflowConfig); err != nil {
			sendErrorResponse(c, http.StatusBadRequest, "Invalid request payload - JSON’s drunk", err)
			return
		}
		if workflowConfig.Token == "" && rsaToken != "" {
			workflowConfig.Token = rsaToken
		}
		tmpFile, err := ioutil.TempFile("", "workflowOutput_")
		if err != nil {
			pterm.Error.Println("Temp file creation failed - disk’s napping:", err)
			sendErrorResponse(c, http.StatusInternalServerError, "Failed to create temp file", err)
			return
		}
		defer tmpFile.Close()
		tmpFileName := tmpFile.Name()
		scriptPort := getNextAvailablePort()
		e := connect3270.NewEmulator(workflowConfig.Host, workflowConfig.Port, strconv.Itoa(scriptPort))
		err = e.InitializeOutput(tmpFileName, true)
		if err != nil {
			sendErrorResponse(c, http.StatusInternalServerError, "Output init failed - setup’s cursed", err)
			return
		}
		for _, step := range workflowConfig.Steps {
			if err := executeStep(e, step, tmpFileName, workflowConfig.Token); err != nil {
				sendErrorResponse(c, http.StatusInternalServerError, fmt.Sprintf("Step '%s' failed - oof", step.Type), err)
				e.Disconnect()
				return
			}
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
	case "Disconnect":
		return e.Disconnect()
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

	pterm.DefaultBigText.
		WithLetters(
			putils.LettersFromStringWithStyle("3270", pterm.FgLightGreen.ToStyle()),
			putils.LettersFromStringWithStyle("Connect", pterm.FgWhite.ToStyle()),
		).
		Render()

	pterm.DefaultBasicText.Println("Version: " + pterm.LightGreen(version))
	pterm.DefaultBasicText.Println("Website: " + pterm.LightGreen("https://3270.io"))
	pterm.DefaultBasicText.Println("Author: " + pterm.LightGreen("EyUp"))

	pterm.Info.Println("Runtime Environment: " + pterm.LightYellow("./3270Connect ") + pterm.White(strings.Join(os.Args[1:], " ")))
	pterm.Println()
}

func LaunchEmbeddedIfDoubleClicked() {
	if !*startDashboard {
		pterm.Warning.Println("Dashboard mode not enabled. Skipping embedded browser launch.")
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
	if runAPI {
		runAPIWorkflow()
	} else {
		if concurrent > 1 || runtimeDuration > 0 {
			runConcurrentWorkflows(config, injectionConfig)

		} else {
			runWorkflow(lastUsedPort, config)
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

func openDashboardEmbedded() {
	if !*startDashboard {
		pterm.Warning.Println("Dashboard mode not enabled. Skipping embedded browser launch.")
		return
	}

	debug := false
	w := webview2.New(debug)
	defer func() {
		if r := recover(); r != nil {
			pterm.Error.Println("Recovered from a panic in openDashboardEmbedded:", r)
		}
		w.Destroy()
	}()

	w.SetTitle("3270Connect Dashboard")
	w.SetSize(1024, 768, webview2.HintNone)

	// Set the icon for the WebView panel
	iconPath := "logo.png" // Ensure this file exists in the working directory
	if _, err := os.Stat(iconPath); err == nil {
		pterm.Info.Printf("Icon file %s found. Proceeding without setting icon (not supported in webview2).\n", iconPath)
	} else {
		pterm.Warning.Printf("Icon file %s not found. Skipping icon setup.\n", iconPath)
	}

	w.Navigate("http://localhost:9200/dashboard")

	// Handle window close event to terminate the PID
	// Handle process termination after the WebView2 window is closed
	defer func() {
		pterm.Info.Println("WebView2 window closed. Initiating shutdown.")
		pid := os.Getpid()
		proc, err := os.FindProcess(pid)
		if err != nil {
			pterm.Error.Printf("Failed to find process with PID %d: %v\n", pid, err)
			return
		}
		if err := proc.Kill(); err != nil {
			pterm.Error.Printf("Failed to terminate process with PID %d: %v\n", pid, err)
		} else {
			pterm.Success.Printf("Process with PID %d terminated successfully.\n", pid)
		}
	}()

	w.Run()
}

var stopTicker chan struct{}

func runConcurrentWorkflows(config *Configuration, injectionConfig string) {
	overallStart := time.Now()
	semaphore := make(chan struct{}, concurrent)
	var wg sync.WaitGroup

	var injectData []map[string]string

	// Load injection data if the file is provided
	if injectionConfig != "" {
		if _, err := os.Stat(injectionConfig); err == nil {
			injectData, err = loadInjectionData(injectionConfig)
			if err != nil {
				pterm.Error.Printf("Failed to load injection data: %v\n", err)
				return
			} else {
				pterm.Info.Printf("Loaded %d injection entries from %s\n", len(injectData), injectionConfig)
			}
		} else {
			pterm.Warning.Printf("Injection file %s not found. Proceeding without injection.\n", injectionConfig)
		}
	}

	// Ensure we have at least one injection entry or proceed with the base configuration
	if len(injectData) == 0 {
		injectData = []map[string]string{{}} // Use an empty map if no injection data is provided
	}

	var multi pterm.MultiPrinter
	var durationBar *pterm.ProgressbarPrinter
	if !enableProgressBar {
		pterm.Info.Println("Progress bar disabled. Showing INFO log messages instead.")
	} else {
		// Initialize MultiPrinter for all output
		multi = pterm.DefaultMultiPrinter

		// Define a fixed width for titles to align text
		const titleWidth = 30

		// Uniform progress bars
		durationBar, _ = pterm.DefaultProgressbar.
			WithTotal(runtimeDuration).
			WithTitle(pterm.Sprintf("%-*s", titleWidth, "  Run Duration  ")).
			WithWriter(multi.NewWriter()).
			WithBarCharacter("-").
			WithBarStyle(pterm.NewStyle(pterm.FgCyan)).
			WithShowPercentage(true).
			WithShowCount(false).
			WithShowElapsedTime(true).
			Start()

		activeBar, _ := pterm.DefaultProgressbar.
			WithTotal(concurrent).
			WithTitle(pterm.Sprintf("%-*s", titleWidth, "Active vUsers")).
			WithWriter(multi.NewWriter()).
			WithBarCharacter("█").
			WithBarStyle(pterm.NewStyle(pterm.FgCyan)).
			WithShowPercentage(true).
			WithShowCount(false).
			WithShowElapsedTime(true).
			Start()

		cpuBar, _ := pterm.DefaultProgressbar.
			WithTotal(100).
			WithTitle(pterm.Sprintf("%-*s", titleWidth, "CPU Usage")).
			WithWriter(multi.NewWriter()).
			WithBarCharacter("█").
			WithBarStyle(pterm.NewStyle(pterm.FgGreen)).
			WithShowPercentage(true).
			WithShowCount(false).
			WithShowElapsedTime(true).
			Start()

		memBar, _ := pterm.DefaultProgressbar.
			WithTotal(100).
			WithTitle(pterm.Sprintf("%-*s", titleWidth, "Memory Usage")).
			WithWriter(multi.NewWriter()).
			WithBarCharacter("█").
			WithBarStyle(pterm.NewStyle(pterm.FgGreen)).
			WithShowPercentage(true).
			WithShowCount(false).
			WithShowElapsedTime(true).
			Start()

		// Start the MultiPrinter
		// Channel to stop the progress bar updates
		stopTicker = make(chan struct{})
		// Channel to stop the progress bar updates
		stopTicker := make(chan struct{})

		// Goroutine for real-time progress bar updates
		go func() {
			ticker := time.NewTicker(1 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					elapsed := int(time.Since(overallStart).Seconds())
					if durationBar != nil {
						durationBar.Current = min(elapsed, runtimeDuration)
						if elapsed < runtimeDuration {
							durationBar.UpdateTitle(pterm.Sprintf("%-*s", titleWidth, fmt.Sprintf("Run Duration (%ds left)", runtimeDuration-elapsed)))
						} else {
							durationBar.UpdateTitle(pterm.Sprintf("%-*s", titleWidth, "Run Duration (Completed)"))
						}
					}
					if cpuBar != nil {
						cpuPercent, _ := cpu.Percent(0, false)
						if len(cpuPercent) > 0 {
							cpuBar.Current = int(cpuPercent[0])
						}
					}
					if memBar != nil {
						memStats, _ := mem.VirtualMemory()
						if memStats != nil {
							memBar.Current = int(memStats.UsedPercent)
						}
					}
					if activeBar != nil {
						activeBar.Current = len(semaphore)
						activeBar.UpdateTitle(pterm.Sprintf("%-*s", titleWidth, fmt.Sprintf("Active vUsers (%d/%d)", len(semaphore), concurrent)))
					}
					// Log status update
					cpuPercentLog, _ := cpu.Percent(0, false)
					memStatsLog, _ := mem.VirtualMemory()
					cpuVal := 0.0
					if len(cpuPercentLog) > 0 {
						cpuVal = cpuPercentLog[0]
					}
					memVal := 0.0
					if memStatsLog != nil {
						memVal = memStatsLog.UsedPercent
					}
					storeLog(fmt.Sprintf("Elapsed: %d, Active workflows: %d, CPU usage: %.2f%%, Memory usage: %.2f%%", elapsed, len(semaphore), cpuVal, memVal))
				case <-stopTicker:
					return
				}
			}
		}()
	}

	injectionCursor := 0

	// Scheduling workflows using nested loops
	for time.Since(overallStart) < time.Duration(runtimeDuration)*time.Second {
		for time.Since(overallStart) < time.Duration(runtimeDuration)*time.Second {
			currentActive := len(semaphore)
			started := startWorkflowBatch(semaphore, config, &wg, injectData, &injectionCursor)
			if started == 0 {
				time.Sleep(time.Duration(config.RampUpDelay * float64(time.Second)))
				break
			}
			storeLog(fmt.Sprintf("Increasing batch by %d, current size is %d, new total target is %d",
				started, currentActive, currentActive+started))
			if !enableProgressBar {
				pterm.Info.Println(fmt.Sprintf("Increasing batch by %d, current size is %d, new total target is %d",
					started, currentActive, currentActive+started))
			}

			cpuPercent, _ := cpu.Percent(0, false)
			memStats, _ := mem.VirtualMemory()
			storeLog(fmt.Sprintf("Currently active workflows: %d, CPU usage: %.2f%%, memory usage: %.2f%%",
				len(semaphore), cpuPercent[0], memStats.UsedPercent))
			if !enableProgressBar {
				pterm.Info.Println(fmt.Sprintf("Currently active workflows: %d, CPU usage: %.2f%%, memory usage: %.2f%%",
					len(semaphore), cpuPercent[0], memStats.UsedPercent))
			}
			time.Sleep(time.Duration(config.RampUpDelay * float64(time.Second)))
		}
		cpuPercent, _ := cpu.Percent(0, false)
		memStats, _ := mem.VirtualMemory()
		storeLog(fmt.Sprintf("Currently active workflows: %d, CPU usage: %.2f%%, memory usage: %.2f%%",
			len(semaphore), cpuPercent[0], memStats.UsedPercent))
		if enableProgressBar {
			pterm.Info.Println(fmt.Sprintf("Currently active workflows: %d, CPU usage: %.2f%%, memory usage: %.2f%%",
				len(semaphore), cpuPercent[0], memStats.UsedPercent))
		}
		time.Sleep(time.Duration(config.RampUpDelay * float64(time.Second)))
	}

	if enableProgressBar {
		multi.Stop()
	}

	// Notify that no new workflows will be scheduled
	pterm.Info.Println("Run duration complete. Waiting for current workflows to finish...")
	wg.Wait()
	storeLog("All workflows completed after runtimeDuration ended.")

	if enableProgressBar {
		// Stop the progress bar updates
		stopTicker <- struct{}{}

		// Final update to duration bar
		elapsed := int(time.Since(overallStart).Seconds())
		durationBar.WithTotal(elapsed)
		durationBar.Current = elapsed
		const titleWidth = 30
		durationBar.UpdateTitle(pterm.Sprintf("%-*s", titleWidth, fmt.Sprintf("Run Duration (%ds elapsed)", elapsed)))
	}

	// Calculate averages for CPU and Memory usage
	var avgCPU, avgMem float64
	mutex.Lock()
	if len(cpuHistory) > 0 {
		var cpuSum float64
		for _, val := range cpuHistory {
			cpuSum += val
		}
		avgCPU = cpuSum / float64(len(cpuHistory))
	}
	if len(memHistory) > 0 {
		var memSum float64
		for _, val := range memHistory {
			memSum += val
		}
		avgMem = memSum / float64(len(memHistory))
	}
	mutex.Unlock()

	// Calculate average workflow completion time
	var avgWorkflowTime float64
	timingsMutex.Lock()
	if len(workflowDurations) > 0 {
		var totalDuration float64
		for _, d := range workflowDurations {
			totalDuration += d
		}
		avgWorkflowTime = totalDuration / float64(len(workflowDurations))
	}
	timingsMutex.Unlock()

	// Capture final stats
	finalActive := len(semaphore)
	finalStarted := atomic.LoadInt64(&totalWorkflowsStarted)
	finalCompleted := atomic.LoadInt64(&totalWorkflowsCompleted)
	finalFailed := atomic.LoadInt64(&totalWorkflowsFailed)

	clear()
	printBanner()

	pterm.Success.Println("All workflows wrapped up - Time for a victory lap!")

	// Display summary report
	elapsed := int(time.Since(overallStart).Seconds())
	pterm.DefaultSection.WithStyle(pterm.NewStyle(pterm.FgCyan)).Println("Run Summary - Performance Report")
	pterm.DefaultTable.
		WithHasHeader().
		WithBoxed(true).
		WithLeftAlignment().
		WithData(pterm.TableData{
			{"Metric", "Value", "Status"},
			{"Total Workflows Started", fmt.Sprintf("%d", finalStarted), "🚀 Launched"},
			{"Total Workflows Completed", fmt.Sprintf("%d", finalCompleted), "✅ Done"},
			{"Total Workflows Failed", fmt.Sprintf("%d", finalFailed), func() string {
				if finalFailed > 0 {
					return "💥 Oof"
				}
				return "🎉 Perfect"
			}()},
			{"Final Active vUsers", fmt.Sprintf("%d/%d", finalActive, concurrent), func() string {
				if finalActive > 0 {
					return "💥 Oof"
				}
				return "🎉 Perfect"
			}()},
			{"Average CPU Usage", fmt.Sprintf("%.1f%%", avgCPU), cpuStatus(avgCPU)},
			{"Average Memory Usage", fmt.Sprintf("%.1f%%", avgMem), memStatus(avgMem)},
			{"Average Workflow Time", fmt.Sprintf("%.2fs", avgWorkflowTime), "⏱️ Avg Duration"},
			{"Run Duration", fmt.Sprintf("%ds", elapsed), "⏱️ Completed"},
		}).Render()

	// Note: If you already print the dashboard message in main, you might remove this duplicate.
	storeLog("All workflows completed")
	updateMetricsFile()
}

func startWorkflowBatch(
	semaphore chan struct{},
	baseConfig *Configuration,
	wg *sync.WaitGroup,
	injectData []map[string]string,
	injectionCursor *int,
) int {
	mutex.Lock()
	availableSlots := concurrent - activeWorkflows
	if availableSlots <= 0 {
		mutex.Unlock()
		return 0
	}
	workflowsToStart := min(baseConfig.RampUpBatchSize, availableSlots)
	mutex.Unlock()

	if workflowsToStart <= 0 {
		return 0
	}

	for i := 0; i < workflowsToStart; i++ {
		semaphore <- struct{}{}
		wg.Add(1)

		var injection map[string]string
		if len(injectData) > 0 {
			idx := 0
			if injectionCursor != nil {
				idx = *injectionCursor % len(injectData)
				*injectionCursor++
			}
			injection = injectData[idx]
		} else {
			injection = map[string]string{}
		}

		userConfig := injectDynamicValues(baseConfig, injection)

		go func(cfg *Configuration) {
			defer func() {
				if r := recover(); r != nil {
					msg := fmt.Sprintf("Recovered from panic in workflow batch: %v", r)
					storeLog(msg)
					if connect3270.Verbose {
						pterm.Error.Println(msg)
					}
				}
			}()
			defer wg.Done()
			defer func() { <-semaphore }()

			portToUse := getNextAvailablePort()
			if err := runWorkflow(portToUse, cfg); err != nil {
				storeLog(fmt.Sprintf("Workflow on port %d error: %v", portToUse, err))
				if connect3270.Verbose {
					pterm.Error.Printf("Workflow on port %d error: %v\n", portToUse, err)
				}
			}
		}(userConfig)
	}

	return workflowsToStart
}

// Helper functions for summary status
func cpuStatus(cpu float64) string {
	switch {
	case cpu < 50:
		return "🟢 Optimal"
	case cpu < 80:
		return "🟡 Moderate"
	default:
		return "🔴 High"
	}
}

func memStatus(mem float64) string {
	switch {
	case mem < 50:
		return "🟢 Optimal"
	case mem < 80:
		return "🟡 Moderate"
	default:
		return "🔴 High"
	}
}

func clear() {
	print("\033[H\033[2J")
}

func getNextAvailablePort() int {
	mutex.Lock()
	defer mutex.Unlock()
	for {
		lastUsedPort++
		if isPortAvailable(lastUsedPort) {
			return lastUsedPort
		}
		if connect3270.Verbose {
			pterm.Warning.Printf("Port %d is taken - port party’s full!\n", lastUsedPort)
		}
	}
}

func isPortAvailable(port int) bool {
	addr := ":" + strconv.Itoa(port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		if connect3270.Verbose {
			pterm.Info.Printf("Port %d in use - next contestant please!\n", port)
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
		// Allow steps that do not require additional configuration.
		if step.Type == "Connect" ||
			step.Type == "AsciiScreenGrab" ||
			step.Type == "PressEnter" ||
			step.Type == "PressTab" ||
			step.Type == "Disconnect" ||
			(strings.HasPrefix(step.Type, "PressPF")) {
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
		pterm.Warning.Printf("Dashboard already vibing on port %d - skipping the encore!\n", dashboardPort)
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
		_, extendedList := readDashboardMetrics(dashboardDir)
		payload := struct {
			AggregatedMetrics Metrics           `json:"aggregated"`
			ExtendedMetrics   []ExtendedMetrics `json:"extendedMetrics"`
			Timestamp         int64             `json:"timestamp"`
		}{
			AggregatedMetrics: aggregateExtendedMetrics(extendedList),
			ExtendedMetrics:   extendedList,
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

func shouldCleanupMetric(m ExtendedMetrics) bool {
	// Keep all metrics, including completed processes, so they remain visible in the dashboard
	return false
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
		if _, err := os.Stat(f); os.IsNotExist(err) {
			continue
		}

		data, err := ioutil.ReadFile(f)
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
		if shouldCleanupMetric(extendedMetric) {
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
	cpuPercents, err := cpu.Percent(0, false)
	var hostCPU float64 = 0
	if err == nil && len(cpuPercents) > 0 {
		hostCPU = cpuPercents[0]
	}
	memStats, err := mem.VirtualMemory()
	var hostMem float64 = 0
	if err == nil {
		hostMem = memStats.UsedPercent
	}
	mutex.Lock()
	cpuHistory = append(cpuHistory, hostCPU)
	memHistory = append(memHistory, hostMem)
	mutex.Unlock()
	timingsMutex.Lock()
	durationsCopy := make([]float64, len(workflowDurations))
	copy(durationsCopy, workflowDurations)
	timingsMutex.Unlock()
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
		CPUUsage:                cpuHistory,
		MemoryUsage:             memHistory,
		Params:                  parameters,
		RuntimeDuration:         runtimeDuration,
		StartTimestamp:          programStart.Unix(),
		ConfigFilePath:          configPath,
		OutputFilePath:          outputPath,
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
	if err := ioutil.WriteFile(filePath, data, 0644); err != nil {
		pterm.Warning.Printf("Metrics file write failed for pid %d - disk’s grumpy: %v\n", pid, err)
	}
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
		if _, err := os.Stat(f); os.IsNotExist(err) {
			continue
		}

		data, err := ioutil.ReadFile(f)
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
		if shouldCleanupMetric(extendedMetric) {
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
	
	if !isRunning {
		// Process is no longer running
		if m.RuntimeDuration > 0 && timeElapsed >= int64(m.RuntimeDuration) {
			// Process completed its full runtime duration
			status = "Completed"
		} else {
			// Process was killed or terminated early
			status = "Killed"
		}
	} else if m.RuntimeDuration > 0 && timeLeft == 0 && (m.Params != "" && !strings.Contains(m.Params, "-runApp")) {
		// Process is still running but time is up
		status = "Ending"
	}

	return ExtendedMetrics{
		Metrics:   m,
		Status:    status,
		TimeLeft:  timeLeft,
		IsRunning: isRunning,
	}
}

func monitorSystemUsage() {
	for {
		cpuPercents, err := cpu.Percent(1*time.Second, true)
		if err == nil && len(cpuPercents) > 0 {
			var sum float64
			for _, p := range cpuPercents {
				sum += p
			}
			overall := sum / float64(len(cpuPercents))
			mutex.Lock()
			cpuHistory = append(cpuHistory, overall)
			if len(cpuHistory) > 100 {
				cpuHistory = cpuHistory[1:]
			}
			mutex.Unlock()
		}
		memStats, err := mem.VirtualMemory()
		if err == nil {
			mutex.Lock()
			memHistory = append(memHistory, memStats.UsedPercent)
			if len(memHistory) > 100 {
				memHistory = memHistory[1:]
			}
			mutex.Unlock()
		}
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
					defer file.Close()
					decoder := json.NewDecoder(file)
					for {
						var logEntry LogEntry
						if err := decoder.Decode(&logEntry); err != nil {
							if err == io.EOF {
								break
							}
							pterm.Warning.Println("Log entry decoding failed:", err)
							continue
						}
						filtered = append(filtered, logEntry)
					}
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
					defer file.Close()
					decoder := json.NewDecoder(file)
					for {
						var logEntry LogEntry
						if err := decoder.Decode(&logEntry); err != nil {
							if err == io.EOF {
								break
							}
							pterm.Warning.Println("Log entry decoding failed:", err)
							continue
						}
						filtered = append(filtered, logEntry)
					}
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
		metric, err := loadExtendedMetricByPID(pid)
		if err != nil {
			if os.IsNotExist(err) {
				http.Error(w, "No metrics file found for PID "+pid, http.StatusNotFound)
			} else {
				http.Error(w, "Unable to load metrics: "+err.Error(), http.StatusInternalServerError)
			}
			return
		}

		// Calculate summary statistics
		var avgWorkflowTime float64
		if len(metric.Durations) > 0 {
			var sum float64
			for _, d := range metric.Durations {
				sum += d
			}
			avgWorkflowTime = sum / float64(len(metric.Durations))
		}

		var avgCPU float64
		if len(metric.CPUUsage) > 0 {
			var sum float64
			for _, c := range metric.CPUUsage {
				sum += c
			}
			avgCPU = sum / float64(len(metric.CPUUsage))
		}

		var avgMemory float64
		if len(metric.MemoryUsage) > 0 {
			var sum float64
			for _, m := range metric.MemoryUsage {
				sum += m
			}
			avgMemory = sum / float64(len(metric.MemoryUsage))
		}

		// Calculate elapsed time
		elapsed := time.Now().Unix() - metric.StartTimestamp
		if !metric.IsRunning && metric.RuntimeDuration > 0 {
			// For completed processes, use the runtime duration
			elapsed = int64(metric.RuntimeDuration)
		}

		summary := map[string]interface{}{
			"pid":                     metric.PID,
			"status":                  metric.Status,
			"params":                  metric.Params,
			"totalWorkflowsStarted":   metric.TotalWorkflowsStarted,
			"totalWorkflowsCompleted": metric.TotalWorkflowsCompleted,
			"totalWorkflowsFailed":    metric.TotalWorkflowsFailed,
			"activeWorkflows":         metric.ActiveWorkflows,
			"avgWorkflowTime":         avgWorkflowTime,
			"avgCPU":                  avgCPU,
			"avgMemory":               avgMemory,
			"runDuration":             elapsed,
			"runtimeDuration":         metric.RuntimeDuration,
			"isRunning":               metric.IsRunning,
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if err := json.NewEncoder(w).Encode(summary); err != nil {
			http.Error(w, "Failed to encode summary: "+err.Error(), http.StatusInternalServerError)
		}
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
	if connect3270.Verbose {
		pterm.Info.Println("Counting active workflows - herd the cats!")
	}
	mutex.Lock()
	defer mutex.Unlock()
	return activeWorkflows
}

func showErrors() {
	errorMutex.Lock()
	defer errorMutex.Unlock()
	if len(errorList) == 0 {
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
	runApp := r.FormValue("runApp")
	if runApp != "" {
		storeLog("Sample app mode detected")
		runAppPort := r.FormValue("runAppPort")
		// Construct command for sample app mode
		command := fmt.Sprintf("./3270Connect -runApp %s -runApp-port %s", runApp, runAppPort)
		go func() {
			pterm.Info.Printf("Executing sample app command: %s\n", command)
			storeLog("Executing sample app command: " + command)
			// Adjust for OS differences if needed
			commandParts := strings.Fields(command)
			executable := commandParts[0]
			args := commandParts[1:]

			cmd := exec.Command(executable, args...)
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
		config.OutputFilePath = override
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

	tempFilePath := filepath.Join(os.TempDir(), handler.Filename)
	if err := os.WriteFile(tempFilePath, updatedJSON, 0644); err != nil {
		http.Error(w, "Failed to save file", http.StatusInternalServerError)
		return
	}

	// Retrieve the injection configuration file (optional)
	var injectionConfigPath string
	injectionFile, injectionHandler, err := r.FormFile("injectionConfig")
	if err == nil {
		defer injectionFile.Close()
		injectionConfigPath = filepath.Join(os.TempDir(), injectionHandler.Filename)
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
		"./3270Connect",
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

	data, err := ioutil.ReadFile(metricsFile)
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

	//pterm.Info.Printf("Clearing existing workflow metrics for PID %d\n", pid)
	storeLog(fmt.Sprintf("Clearing existing workflow metrics for PID %d", pid))
	metrics.ActiveWorkflows = 0
	metrics.TotalWorkflowsStarted = 0
	metrics.TotalWorkflowsCompleted = 0
	metrics.TotalWorkflowsFailed = 0
	metrics.Durations = []float64{}
	metrics.CPUUsage = []float64{}
	metrics.MemoryUsage = []float64{}

	extendedMetrics := metrics.extend()
	extendedMetrics.Status = "Killed"

	updatedData, err := json.Marshal(extendedMetrics)
	if err != nil {
		pterm.Warning.Printf("Failed to marshal updated metrics for PID %d: %v\n", pid, err)
		storeLog(fmt.Sprintf("Failed to marshal updated metrics for PID %d: %v", pid, err))
		return
	}
	if err := ioutil.WriteFile(metricsFile, updatedData, 0644); err != nil {
		pterm.Warning.Printf("Failed to write updated metrics for PID %d: %v\n", pid, err)
		storeLog(fmt.Sprintf("Failed to write updated metrics for PID %d: %v", pid, err))
		return
	}
	//pterm.Info.Printf("Successfully updated metrics for PID %d to status 'Killed'\n", pid)
	storeLog(fmt.Sprintf("Successfully updated metrics for PID %d to status 'Killed'", pid))
}

func loadInjectionData(filePath string) ([]map[string]string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var injectData []map[string]string
	decoder := json.NewDecoder(file)
	err = decoder.Decode(&injectData)
	if err != nil {
		return nil, err
	}

	return injectData, nil
}

func injectDynamicValues(config *Configuration, injection map[string]string) *Configuration {
	newConfig := *config // Create a copy of the configuration
	newConfig.Steps = make([]Step, len(config.Steps))
	copy(newConfig.Steps, config.Steps)

	for i, step := range newConfig.Steps {
		for placeholder, value := range injection {
			if strings.Contains(step.Text, placeholder) {
				newConfig.Steps[i].Text = strings.ReplaceAll(step.Text, placeholder, value)
			}
		}
	}

	return &newConfig
}
