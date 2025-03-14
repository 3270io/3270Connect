package main

import (
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	connect3270 "github.com/3270io/3270Connect/connect3270"
	"github.com/3270io/3270Connect/sampleapps/app1"
	app2 "github.com/3270io/3270Connect/sampleapps/app2"

	"github.com/gin-gonic/gin"
	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/mem"
	"github.com/sirupsen/logrus"
)

const version = "1.3"

var log = logrus.New()

func init() {
	log.SetFormatter(&logrus.TextFormatter{})
	log.SetOutput(os.Stdout)
	log.SetLevel(logrus.InfoLevel)
}

// Configuration holds the settings for the terminal connection and the steps to be executed.
type Configuration struct {
	Host            string
	Port            int
	OutputFilePath  string `json:"OutputFilePath"`
	Steps           []Step
	InputFilePath   string  `json:"InputFilePath"` // New field for the input file path
	RampUpBatchSize int     `json:"RampUpBatchSize"`
	RampUpDelay     float64 `json:"RampUpDelay"`
}

// Step represents an individual action to be taken on the terminal.
type Step struct {
	Type        string
	Coordinates connect3270.Coordinates // Use go3270 package's Coordinates type
	Text        string
}

var (
	configFile      string
	showHelp        bool
	runAPI          bool
	apiPort         int
	concurrent      int
	headless        bool // Run go3270 in headless mode
	verbose         bool
	runApp          string
	runtimeDuration int // Duration to run workflows (only used in concurrent mode)
	lastUsedPort    int // Will be set from startPort flag
	startPort       int // Starting port for workflow connections
)

var dashboardStarted bool

// Global counters for metrics.
var totalWorkflowsStarted int64
var totalWorkflowsCompleted int64
var totalWorkflowsFailed int64

// Flag for the dashboard port.
var dashboardPort int

var activeWorkflows int
var mutex sync.Mutex

// Global variables for workflow durations.
var timingsMutex sync.Mutex
var workflowDurations []float64

// Global variables for host-wide CPU and memory usage history.
var cpuHistory []float64
var memHistory []float64

var showVersion = flag.Bool("version", false, "Show the application version")

var runAppPort int

// Add a struct and slice for storing logs in memory.
type LogEntry struct {
	PID        string    `json:"pid"`
	Parameters string    `json:"parameters"`
	Log        string    `json:"log"`
	Timestamp  time.Time `json:"timestamp"` // new field
}

var inMemoryLogs []LogEntry
var logMutex sync.Mutex

func storeLog(message string) {
	logMutex.Lock()
	defer logMutex.Unlock()
	pid := os.Getpid()

	// Get the entire command-line arguments except the program name
	args := os.Args[1:]
	parameters := strings.Join(args, " ")

	logEntry := LogEntry{
		PID:        strconv.Itoa(pid),
		Parameters: parameters,
		Log:        message,
		Timestamp:  time.Now(),
	}
	inMemoryLogs = append(inMemoryLogs, logEntry)

	// Save log entry to disk
	logFilePath := filepath.Join("logs", fmt.Sprintf("logs_%d.json", pid))
	file, err := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("Error opening log file: %v", err)
		return
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	if err := encoder.Encode(logEntry); err != nil {
		log.Printf("Error encoding log entry: %v", err)
	}
}

//go:embed templates/dashboard.gohtml
var dashboardTemplateFS embed.FS

var dashboardTemplate *template.Template

func init() {
	flag.StringVar(&configFile, "config", "workflow.json", "Path to the configuration file")
	flag.BoolVar(&showHelp, "help", false, "Show usage information")
	flag.BoolVar(&runAPI, "api", false, "Run as API")
	flag.IntVar(&apiPort, "api-port", 8080, "API port")
	flag.IntVar(&concurrent, "concurrent", 1, "Number of concurrent workflows")
	flag.BoolVar(&headless, "headless", false, "Run go3270 in headless mode")
	flag.BoolVar(&verbose, "verbose", false, "Run go3270 in verbose mode")
	flag.IntVar(&runtimeDuration, "runtime", 0, "Duration to run workflows in seconds. Only used in concurrent mode.")
	flag.StringVar(&runApp, "runApp", "", "Select which sample 3270 application to run (e.g., '1' for app1, '2' for app2)")
	flag.IntVar(&runAppPort, "runApp-port", 3270, "Port for the sample 3270 application (default 3270)")
	flag.IntVar(&startPort, "startPort", 5000, "Starting port number for workflow connections")
	flag.IntVar(&dashboardPort, "dashboardPort", 9200, "Port for the dashboard server")

	// Create logs directory if it doesn't exist
	if err := os.MkdirAll("logs", 0755); err != nil {
		handleError(err, "Error creating logs directory")
	}

	var err error
	dashboardTemplate, err = template.ParseFS(dashboardTemplateFS, "templates/dashboard.gohtml")
	handleError(err, "Error parsing dashboard template")
}

func loadConfiguration(filePath string) *Configuration {
	if connect3270.Verbose {
		log.Printf("Loading configuration from %s", filePath)
	}
	configFile, err := os.Open(filePath)
	handleError(err, fmt.Sprintf("Error opening config file at %s", filePath))
	defer configFile.Close()
	var config Configuration
	decoder := json.NewDecoder(configFile)
	err = decoder.Decode(&config)
	handleError(err, "Error decoding config JSON")
	if config.RampUpBatchSize <= 0 {
		config.RampUpBatchSize = 10
	}
	if config.RampUpDelay <= 0 {
		config.RampUpDelay = 1.0
	}
	err = validateConfiguration(&config)
	handleError(err, "Invalid configuration")
	return &config
}

func handleError(err error, message string) {
	if err != nil {
		log.Fatalf("%s: %v", message, err)
	}
}

func loadInputFile(filePath string) ([]Step, error) {
	if connect3270.Verbose {
		log.Printf("Loading input file: %s", filePath)
	}
	data, err := ioutil.ReadFile(filePath)
	if err != nil {
		log.Printf("Error reading input file %s: %v", filePath, err)
		return nil, fmt.Errorf("error reading input file: %v", err)
	}
	if connect3270.Verbose {
		log.Printf("Successfully read input file: %d bytes", len(data))
	}
	var steps []Step
	steps = append(steps, Step{Type: "Connect"})
	if connect3270.Verbose {
		log.Printf("Added initial Connect step")
	}
	lines := strings.Split(string(data), "\n")
	for idx, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if connect3270.Verbose {
			log.Printf("Processing line %d: %s", idx+1, line)
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
				log.Printf("Added step: %s with text: %s", stepType, key)
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
							log.Printf("Error parsing position values in line %d: row or column conversion error", idx+1)
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
						log.Printf("Added CheckValue step: text '%s' at position (%d,%d), length %d", text, row, column, len(text))
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
						log.Printf("Error parsing coordinates in line %d: row or column conversion error", idx+1)
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
							log.Printf("Added FillString step: text '%s' at position (%d,%d)", key, row, column)
						}
					}
				}
			}
		}
	}

	steps = append(steps, Step{Type: "Disconnect"})
	if connect3270.Verbose {
		log.Printf("Added final Disconnect step")
	}
	if connect3270.Verbose {
		log.Println("Workflow steps loaded:")
		for index, step := range steps {
			log.Printf("Step %d: Type: %s, Text: '%s', Coordinates: {Row: %d, Column: %d, Length: %d}",
				index, step.Type, step.Text, step.Coordinates.Row, step.Coordinates.Column, step.Coordinates.Length)
		}
	}
	return steps, nil
}

func runWorkflow(scriptPort int, config *Configuration) error {
	startTime := time.Now()
	atomic.AddInt64(&totalWorkflowsStarted, 1)
	if connect3270.Verbose {
		log.Printf("Starting workflow for scriptPort %d", scriptPort)
	}
	storeLog((fmt.Sprintf("Starting workflow for scriptPort %d", scriptPort)))
	mutex.Lock()
	activeWorkflows++
	mutex.Unlock()
	e := connect3270.NewEmulator(config.Host, config.Port, strconv.Itoa(scriptPort))
	tmpFile, err := ioutil.TempFile("", "workflowOutput_")
	if err != nil {
		log.Printf("Error creating temporary file: %v", err)
		return err
	}
	tmpFileName := tmpFile.Name()
	tmpFile.Close()
	e.InitializeOutput(tmpFileName, runAPI)
	workflowFailed := false
	var steps []Step
	if config.InputFilePath != "" {
		steps, err = loadInputFile(config.InputFilePath)
		if err != nil {
			log.Printf("Error loading input file: %v", err)
			return err
		}
	} else {
		steps = config.Steps
	}
	for _, step := range steps {
		if workflowFailed {
			break
		}
		switch step.Type {
		case "InitializeOutput":
			err := e.InitializeOutput(tmpFileName, runAPI)
			if err != nil {
				return fmt.Errorf("error initializing output file: %v", err)
			}
		case "Connect":
			if err := e.Connect(); err != nil {
				log.Printf("Error connecting to terminal: %v", err)
				workflowFailed = true
			}
			e.WaitForField(30)
		case "CheckValue":
			v, err := e.GetValue(step.Coordinates.Row, step.Coordinates.Column, step.Coordinates.Length)
			if err != nil {
				log.Printf("Error getting value: %v", err)
				workflowFailed = true
				break
			}
			v = strings.TrimSpace(v)
			if connect3270.Verbose {
				log.Println("Retrieved value: " + v)
			}
			if v != step.Text {
				log.Printf("CheckValue failed. Expected: %s, Found: %s", step.Text, v)
				workflowFailed = true
			}
		case "FillString":
			if err := e.FillString(step.Coordinates.Row, step.Coordinates.Column, step.Text); err != nil {
				log.Printf("Error setting text: %v", err)
				workflowFailed = true
			}
		case "AsciiScreenGrab":
			if err := e.AsciiScreenGrab(tmpFileName, runAPI); err != nil {
				log.Printf("Error in AsciiScreenGrab: %v", err)
				workflowFailed = true
			}
		case "PressEnter":
			if err := e.Press(connect3270.Enter); err != nil {
				log.Printf("Error pressing Enter: %v", err)
				workflowFailed = true
			}
		case "PressTab":
			if err := e.Press(connect3270.Tab); err != nil {
				log.Printf("Error pressing Tab: %v", err)
				workflowFailed = true
			}
		case "Disconnect":
			if err := e.Disconnect(); err != nil {
				log.Printf("Error disconnecting: %v", err)
				workflowFailed = true
			}
		case "PressPF1":
			if err := e.Press(connect3270.F1); err != nil {
				log.Printf("Error pressing PF1: %v", err)
				workflowFailed = true
			}
		case "PressPF2":
			if err := e.Press(connect3270.F2); err != nil {
				log.Printf("Error pressing PF2: %v", err)
				workflowFailed = true
			}
		case "PressPF3":
			if err := e.Press(connect3270.F3); err != nil {
				log.Printf("Error pressing PF3: %v", err)
				workflowFailed = true
			}
		case "PressPF4":
			if err := e.Press(connect3270.F4); err != nil {
				log.Printf("Error pressing PF4: %v", err)
				workflowFailed = true
			}
		case "PressPF5":
			if err := e.Press(connect3270.F5); err != nil {
				log.Printf("Error pressing PF5: %v", err)
				workflowFailed = true
			}
		case "PressPF6":
			if err := e.Press(connect3270.F6); err != nil {
				log.Printf("Error pressing PF6: %v", err)
				workflowFailed = true
			}
		case "PressPF7":
			if err := e.Press(connect3270.F7); err != nil {
				log.Printf("Error pressing PF7: %v", err)
				workflowFailed = true
			}
		case "PressPF8":
			if err := e.Press(connect3270.F8); err != nil {
				log.Printf("Error pressing PF8: %v", err)
				workflowFailed = true
			}
		case "PressPF9":
			if err := e.Press(connect3270.F9); err != nil {
				log.Printf("Error pressing PF9: %v", err)
				workflowFailed = true
			}
		case "PressPF10":
			if err := e.Press(connect3270.F10); err != nil {
				log.Printf("Error pressing PF10: %v", err)
				workflowFailed = true
			}
		case "PressPF11":
			if err := e.Press(connect3270.F11); err != nil {
				log.Printf("Error pressing PF11: %v", err)
				workflowFailed = true
			}
		case "PressPF12":
			if err := e.Press(connect3270.F12); err != nil {
				log.Printf("Error pressing PF12: %v", err)
				workflowFailed = true
			}
		case "PressPF13":
			if err := e.Press(connect3270.F13); err != nil {
				log.Printf("Error pressing PF13: %v", err)
				workflowFailed = true
			}
		case "PressPF14":
			if err := e.Press(connect3270.F14); err != nil {
				log.Printf("Error pressing PF14: %v", err)
				workflowFailed = true
			}
		case "PressPF15":
			if err := e.Press(connect3270.F15); err != nil {
				log.Printf("Error pressing PF15: %v", err)
				workflowFailed = true
			}
		case "PressPF16":
			if err := e.Press(connect3270.F16); err != nil {
				log.Printf("Error pressing PF16: %v", err)
				workflowFailed = true
			}
		case "PressPF17":
			if err := e.Press(connect3270.F17); err != nil {
				log.Printf("Error pressing PF17: %v", err)
				workflowFailed = true
			}
		case "PressPF18":
			if err := e.Press(connect3270.F18); err != nil {
				log.Printf("Error pressing PF18: %v", err)
				workflowFailed = true
			}
		case "PressPF19":
			if err := e.Press(connect3270.F19); err != nil {
				log.Printf("Error pressing PF19: %v", err)
				workflowFailed = true
			}
		case "PressPF20":
			if err := e.Press(connect3270.F20); err != nil {
				log.Printf("Error pressing PF20: %v", err)
				workflowFailed = true
			}
		case "PressPF21":
			if err := e.Press(connect3270.F21); err != nil {
				log.Printf("Error pressing PF21: %v", err)
				workflowFailed = true
			}
		case "PressPF22":
			if err := e.Press(connect3270.F22); err != nil {
				log.Printf("Error pressing PF22: %v", err)
				workflowFailed = true
			}
		case "PressPF23":
			if err := e.Press(connect3270.F23); err != nil {
				log.Printf("Error pressing PF23: %v", err)
				workflowFailed = true
			}
		case "PressPF24":
			if err := e.Press(connect3270.F24); err != nil {
				log.Printf("Error pressing PF24: %v", err)
				workflowFailed = true
			}
		default:
			log.Printf("Unknown step type: %s", step.Type)
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
		log.Printf("Workflow for scriptPort %d failed", scriptPort)
		storeLog(fmt.Sprintf("Workflow for scriptPort %d failed", scriptPort))
		atomic.AddInt64(&totalWorkflowsFailed, 1)
	} else {
		if connect3270.Verbose {
			log.Printf("Workflow for scriptPort %d completed successfully", scriptPort)
			storeLog(fmt.Sprintf("Workflow for scriptPort %d completed successfully", scriptPort))
		}
		if config.OutputFilePath != "" {
			_ = os.Remove(config.OutputFilePath)
			if err := os.Rename(tmpFileName, config.OutputFilePath); err != nil {
				pid := os.Getpid()
				uniqueOutputPath := fmt.Sprintf("%s.%d", config.OutputFilePath, pid)
				if err2 := os.Rename(tmpFileName, uniqueOutputPath); err2 != nil {
					log.Printf("Error renaming temporary file to unique output file: %v", err2)
				} else if verbose {
					log.Printf("Renamed temporary file to unique output file: %s", uniqueOutputPath)
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
		log.Println("Starting API server mode")
	}
	connect3270.Headless = true
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()
	r.SetTrustedProxies(nil)
	r.POST("/api/execute", func(c *gin.Context) {
		var workflowConfig Configuration
		if err := c.ShouldBindJSON(&workflowConfig); err != nil {
			sendErrorResponse(c, http.StatusBadRequest, "Invalid request payload", err)
			return
		}
		tmpFile, err := ioutil.TempFile("", "workflowOutput_")
		if err != nil {
			log.Printf("Error creating temporary file: %v", err)
			sendErrorResponse(c, http.StatusInternalServerError, "Failed to create temporary file", err)
			return
		}
		defer tmpFile.Close()
		tmpFileName := tmpFile.Name()
		scriptPort := getNextAvailablePort()
		e := connect3270.NewEmulator(workflowConfig.Host, workflowConfig.Port, strconv.Itoa(scriptPort))
		err = e.InitializeOutput(tmpFileName, true)
		if err != nil {
			sendErrorResponse(c, http.StatusInternalServerError, "Failed to initialize output file", err)
			return
		}
		for _, step := range workflowConfig.Steps {
			if err := executeStep(e, step, tmpFileName); err != nil {
				sendErrorResponse(c, http.StatusInternalServerError, fmt.Sprintf("Workflow step '%s' failed", step.Type), err)
				e.Disconnect()
				return
			}
		}
		outputContents, err := e.ReadOutputFile(tmpFileName)
		if err != nil {
			sendErrorResponse(c, http.StatusInternalServerError, "Failed to read output file", err)
			return
		}
		e.Disconnect()
		c.JSON(http.StatusOK, gin.H{
			"returnCode": http.StatusOK,
			"status":     "okay",
			"message":    "Workflow executed successfully",
			"output":     outputContents,
		})
	})
	apiAddr := fmt.Sprintf(":%d", apiPort)
	log.Printf("API server is running on %s", apiAddr)
	if err := r.Run(apiAddr); err != nil {
		log.Fatalf("Failed to start API server: %v", err)
	}
}

func executeStep(e *connect3270.Emulator, step Step, tmpFileName string) error {
	switch step.Type {
	case "InitializeOutput":
		return e.InitializeOutput(tmpFileName, runAPI)
	case "Connect":
		return e.Connect()
	case "CheckValue":
		_, err := e.GetValue(step.Coordinates.Row, step.Coordinates.Column, step.Coordinates.Length)
		return err
	case "FillString":
		if step.Coordinates.Row == 0 && step.Coordinates.Column == 0 {
			return e.SetString(step.Text)
		}
		return e.FillString(step.Coordinates.Row, step.Coordinates.Column, step.Text)
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
		log.Println("Starting sendErrorResponse")
	}
	c.JSON(statusCode, gin.H{
		"returnCode": statusCode,
		"status":     "error",
		"message":    message,
		"error":      err.Error(),
	})
}

func printBanner() {
	fmt.Println(`
	 ____ ___ ______ ___   _____                            _
	|___ \__ \____  / _ \ / ____|                          | |
	  __) | ) |  / / | | | |     ___  _ __  _ __   ___  ___| |_
	 |__ < / /  / /| | | | |    / _ \| '_ \| '_ \ / _ \/ __| __|
	 ___) / /_ / / | |_| | |___| (_) | | | | | | |  __/ (__| |_
	|____/____/_/   \___/ \_____\___/|_| |_|_| |_|\___|\___|\__|

Version: ` + version)
}

func main() {
	flag.Parse()
	printBanner()
	mutex.Lock()
	lastUsedPort = startPort
	mutex.Unlock()
	if *showVersion {
		printVersionAndExit()
	}
	if showHelp {
		printHelpAndExit()
	}
	setGlobalSettings()
	if concurrent > 1 || runtimeDuration > 0 {
		go runDashboard()
	}
	go monitorSystemUsage()
	if runApp != "" {
		switch runApp {
		case "1":
			app1.RunApplication(runAppPort)
			return
		case "2":
			app2.RunApplication(runAppPort)
			return
		default:
			log.Fatalf("Invalid runApp value: %s. Please enter a valid app number.", runApp)
		}
	}
	config := loadConfiguration(configFile)
	if runAPI {
		runAPIWorkflow()
	} else {
		if concurrent > 1 {
			runConcurrentWorkflows(config)
		} else {
			runWorkflow(7000, config)
		}
		if concurrent > 1 && dashboardStarted {
			log.Printf("All workflows completed but the dashboard is still running on port %d. Press Ctrl+C to exit.", dashboardPort)
			storeLog(fmt.Sprintf("All workflows completed but the dashboard is still running on port %d. Press Ctrl+C to exit.", dashboardPort))
			select {}
		}
	}
}

func printVersionAndExit() {
	fmt.Printf("3270Connect Version: %s\n", version)
	os.Exit(0)
}

func printHelpAndExit() {
	fmt.Printf("3270Connect Version: %s\n", version)
	flag.Usage()
	os.Exit(0)
}

func setGlobalSettings() {
	connect3270.Headless = headless
	connect3270.Verbose = verbose
}

func runConcurrentWorkflows(config *Configuration) {
	overallStart := time.Now()
	semaphore := make(chan struct{}, concurrent)
	var wg sync.WaitGroup
	for time.Since(overallStart) < time.Duration(runtimeDuration)*time.Second {
		for time.Since(overallStart) < time.Duration(runtimeDuration)*time.Second {
			freeSlots := concurrent - len(semaphore)
			if freeSlots <= 0 {
				time.Sleep(time.Duration(config.RampUpDelay * float64(time.Second)))
				break
			}
			batchSize := min(freeSlots, config.RampUpBatchSize)
			log.Printf("Increasing batch by %d, current size is %d, new total target is %d",
				batchSize, len(semaphore), len(semaphore)+batchSize)
			storeLog(fmt.Sprintf("Increasing batch by %d, current size is %d, new total target is %d",
				batchSize, len(semaphore), len(semaphore)+batchSize))
			for i := 0; i < batchSize; i++ {
				semaphore <- struct{}{}
				wg.Add(1)
				go func() {
					defer wg.Done()
					portToUse := getNextAvailablePort()
					err := runWorkflow(portToUse, config)
					if err != nil && connect3270.Verbose {
						log.Printf("Workflow on port %d error: %v", portToUse, err)
					}
					<-semaphore
				}()
			}
			cpuPercent, _ := cpu.Percent(0, false)
			memStats, _ := mem.VirtualMemory()
			log.Printf("Currently active workflows: %d, CPU usage: %.2f%%, memory usage: %.2f%%",
				len(semaphore), cpuPercent[0], memStats.UsedPercent)
			storeLog(fmt.Sprintf("Currently active workflows: %d, CPU usage: %.2f%%, memory usage: %.2f%%",
				len(semaphore), cpuPercent[0], memStats.UsedPercent))
			time.Sleep(time.Duration(config.RampUpDelay * float64(time.Second)))
		}
		cpuPercent, _ := cpu.Percent(0, false)
		memStats, _ := mem.VirtualMemory()
		log.Printf("Currently active workflows: %d, CPU usage: %.2f%%, memory usage: %.2f%%",
			len(semaphore), cpuPercent[0], memStats.UsedPercent)
		storeLog(fmt.Sprintf("Currently active workflows: %d, CPU usage: %.2f%%, memory usage: %.2f%%",
			len(semaphore), cpuPercent[0], memStats.UsedPercent))
		time.Sleep(time.Duration(config.RampUpDelay * float64(time.Second)))
	}
	wg.Wait()
	log.Println("All workflows completed after runtimeDuration ended.")
	storeLog("All workflows completed after runtimeDuration ended.")
}

func getActiveWorkflows() int {
	if connect3270.Verbose {
		log.Println("Starting getActiveWorkflows")
	}
	mutex.Lock()
	defer mutex.Unlock()
	return activeWorkflows
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
			log.Printf("Port %d is in use, trying next port", lastUsedPort)
		}
	}
}

func isPortAvailable(port int) bool {
	addr := ":" + strconv.Itoa(port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		if connect3270.Verbose {
			log.Printf("Port %d is in use, trying next port", port)
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
		log.Println("Starting validateConfiguration")
	}
	if config.Host == "" {
		return fmt.Errorf("host is empty")
	}
	if config.Port <= 0 {
		return fmt.Errorf("port is invalid")
	}
	if config.OutputFilePath == "" {
		return fmt.Errorf("output file path is empty")
	}
	for _, step := range config.Steps {
		switch step.Type {
		case "Connect", "AsciiScreenGrab", "PressEnter", "Disconnect":
			continue
		case "CheckValue", "FillString":
			if step.Coordinates.Row == 0 || step.Coordinates.Column == 0 {
				return fmt.Errorf("coordinates are incomplete in a %s step", step.Type)
			}
			if step.Text == "" {
				return fmt.Errorf("text is empty in a %s step", step.Type)
			}
		default:
			return fmt.Errorf("unknown step type: %s", step.Type)
		}
	}
	return nil
}

// runDashboard launches the dashboard server. It now serves two charts:
// 1. A "Per PID Metrics" duration chart.
// 2. A "cpuMemChart" that uses host-level CPU and Memory usage from the metrics file
// with the smallest PID.
// The metadata section uses a Bootstrap jumbotron and flex grid.
func runDashboard() {
	addr := fmt.Sprintf(":%d", dashboardPort)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Printf("Dashboard already running on port %d, skipping local dashboard.", dashboardPort)
		go func() {
			for {
				updateMetricsFile()
				time.Sleep(2 * time.Second)
			}
		}()
		return
	}
	dashboardStarted = true
	{
		dashboardDir, err := os.UserConfigDir()
		if err != nil {
			log.Printf("Error fetching user config directory: %v", err)
			dashboardDir = filepath.Join(".", "dashboard")
		} else {
			dashboardDir = filepath.Join(dashboardDir, "3270Connect", "dashboard")
		}
		files, err := filepath.Glob(filepath.Join(dashboardDir, "metrics_*.json"))
		if err != nil {
			log.Printf("Error listing old metrics files: %v", err)
		} else {
			for _, f := range files {
				if err := os.Remove(f); err != nil {
					log.Printf("Error removing old metrics file %s: %v", f, err)
				} else {
					log.Printf("Removed old metrics file: %s", f)
				}
			}
		}
	}

	// Clean up old JSON logs before starting the dashboard
	logFiles, err := filepath.Glob(filepath.Join("logs", "logs_*.json"))
	if err == nil {
		for _, lf := range logFiles {
			if err := os.Remove(lf); err != nil {
				log.Printf("Error removing old console log file %s: %v", lf, err)
			} else {
				log.Printf("Removed old console log file: %s", lf)
			}
		}
	}

	setupConsoleHandler()
	setupTerminalConsoleHandler()
	http.HandleFunc("/dashboard", func(w http.ResponseWriter, r *http.Request) {
		dashboardDir, err := os.UserConfigDir()
		if err != nil {
			log.Printf("Error fetching user config directory: %v", err)
			dashboardDir = filepath.Join(".", "dashboard")
		} else {
			dashboardDir = filepath.Join(dashboardDir, "3270Connect", "dashboard")
		}
		files, err := filepath.Glob(filepath.Join(dashboardDir, "metrics_*.json"))
		if err != nil {
			log.Printf("Error listing metrics files: %v", err)
			files = []string{}
		}
		var metricsList []Metrics
		var totalStarted, totalCompleted, totalFailed, active int
		for _, f := range files {
			data, err := ioutil.ReadFile(f)
			if err != nil {
				log.Printf("Error reading file %s: %v", f, err)
				continue
			}
			var m Metrics
			if err := json.Unmarshal(data, &m); err != nil {
				log.Printf("Error unmarshaling file %s: %v", f, err)
				continue
			}
			metricsList = append(metricsList, m)
			totalStarted += int(m.TotalWorkflowsStarted)
			totalCompleted += int(m.TotalWorkflowsCompleted)
			totalFailed += int(m.TotalWorkflowsFailed)
			active += m.ActiveWorkflows
		}
		// To display host CPU and Memory, choose the metrics file with the smallest PID.
		var hostMetrics *Metrics
		if len(metricsList) > 0 {
			hostMetrics = &metricsList[0]
			for i := 1; i < len(metricsList); i++ {
				if metricsList[i].PID < hostMetrics.PID {
					hostMetrics = &metricsList[i]
				}
			}
		}
		metricsJSON, _ := json.Marshal(metricsList)
		// Escape any literal "%" in the JSON so that they are not mistaken for format specifiers.
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
		agg := aggregateMetrics()
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
		}
		if err := dashboardTemplate.Execute(w, data); err != nil {
			log.Printf("Error executing dashboard template: %v", err)
		}
	})

	log.Printf("Dashboard available at http://localhost:%d/dashboard", dashboardPort)
	go func() {
		for {
			updateMetricsFile()
			time.Sleep(2 * time.Second)
		}
	}()
	if err := http.Serve(listener, nil); err != nil {
		log.Printf("Dashboard server error: %v", err)
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
	// Get the entire command-line arguments except the program name
	args := os.Args[1:]
	parameters := strings.Join(args, " ")
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
	}
	data, err := json.Marshal(metrics)
	if err != nil {
		log.Printf("Error marshaling metrics for pid %d: %v", pid, err)
		return
	}
	dashboardDir, err := os.UserConfigDir()
	if err != nil {
		log.Printf("Error fetching user config directory: %v", err)
		dashboardDir = filepath.Join(".", "dashboard")
	} else {
		dashboardDir = filepath.Join(dashboardDir, "3270Connect", "dashboard")
	}
	os.MkdirAll(dashboardDir, 0755)
	filePath := filepath.Join(dashboardDir, fmt.Sprintf("metrics_%d.json", pid))
	if err := ioutil.WriteFile(filePath, data, 0644); err != nil {
		log.Printf("Error writing metrics file for pid %d: %v", pid, err)
	}
}

func aggregateMetrics() Metrics {
	dashboardDir, err := os.UserConfigDir()
	if err != nil {
		log.Printf("Error fetching user config directory: %v", err)
		dashboardDir = filepath.Join(".", "dashboard")
	} else {
		dashboardDir = filepath.Join(dashboardDir, "3270Connect", "dashboard")
	}
	files, err := filepath.Glob(filepath.Join(dashboardDir, "metrics_*.json"))
	if err != nil {
		log.Printf("Error listing metrics files: %v", err)
		return Metrics{}
	}
	var agg Metrics
	for _, f := range files {
		data, err := ioutil.ReadFile(f)
		if err != nil {
			log.Printf("Error reading file %s: %v", f, err)
			continue
		}
		var m Metrics
		if err := json.Unmarshal(data, &m); err != nil {
			log.Printf("Error unmarshaling file %s: %v", f, err)
			continue
		}
		agg.TotalWorkflowsStarted += m.TotalWorkflowsStarted
		agg.TotalWorkflowsCompleted += m.TotalWorkflowsCompleted
		agg.TotalWorkflowsFailed += m.TotalWorkflowsFailed
		agg.ActiveWorkflows += m.ActiveWorkflows
		agg.Durations = append(agg.Durations, m.Durations...)
		agg.CPUUsage = append(agg.CPUUsage, m.CPUUsage...)
		agg.MemoryUsage = append(agg.MemoryUsage, m.MemoryUsage...)
	}
	return agg
}

func monitorSystemUsage() {
	for {
		// Measure per-core CPU usage over a 1-second interval.
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

		// Get memory usage as before.
		memStats, err := mem.VirtualMemory()
		if err == nil {
			mutex.Lock()
			memHistory = append(memHistory, memStats.UsedPercent)
			if len(memHistory) > 100 {
				memHistory = memHistory[1:]
			}
			mutex.Unlock()
		}
		// No additional sleep needed here since cpu.Percent already waited for 1 second.
	}
}

// Add new HTTP handler for console logs
func setupConsoleHandler() {
	http.HandleFunc("/console", func(w http.ResponseWriter, r *http.Request) {
		pidFilter := r.URL.Query().Get("pid")
		var filtered []LogEntry

		if pidFilter != "" {
			// Read logs from disk for the specified PID
			logFilePath := filepath.Join("logs", fmt.Sprintf("logs_%s.json", pidFilter))
			file, err := os.Open(logFilePath)
			if err != nil {
				log.Printf("Error opening log file: %v", err)
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
					log.Printf("Error decoding log entry: %v", err)
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
						log.Printf("Error opening log file: %v", err)
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
							log.Printf("Error decoding log entry: %v", err)
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
			// Read logs from disk for the specified PID
			logFilePath := filepath.Join("logs", fmt.Sprintf("logs_%s.json", pidFilter))
			file, err := os.Open(logFilePath)
			if err != nil {
				log.Printf("Error opening log file: %v", err)
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
					log.Printf("Error decoding log entry: %v", err)
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
						log.Printf("Error opening log file: %v", err)
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
							log.Printf("Error decoding log entry: %v", err)
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
			w.Write([]byte(fmt.Sprintf("%s | PID: %s | Params: %s | Log: %s\n", entry.Timestamp.Format(time.RFC3339), entry.PID, entry.Parameters, entry.Log)))
		}
	})
}
