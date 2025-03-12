package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic" // Added for atomic counters
	"time"

	connect3270 "github.com/3270io/3270Connect/connect3270"
	"github.com/3270io/3270Connect/sampleapps/app1"
	app2 "github.com/3270io/3270Connect/sampleapps/app2"

	"path/filepath"

	"github.com/gin-gonic/gin"
)

const version = "1.1.2"

// Configuration holds the settings for the terminal connection and the steps to be executed.
type Configuration struct {
	Host            string
	Port            int
	OutputFilePath  string `json:"OutputFilePath"`
	Steps           []Step
	InputFilePath   string `json:"InputFilePath"` // New field for the input file path
	RampUpBatchSize int    `json:"RampUpBatchSize"`
	RampUpDelay     int    `json:"RampUpDelay"`
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
	headless        bool // Flag to run go3270 in headless mode
	verbose         bool
	runApp          string
	runtimeDuration int // Flag to determine if new workflows should be started when others finish
	done            = make(chan bool)
	wg              sync.WaitGroup
	lastUsedPort    int       // remove preset initial value; will be set from startPort flag
	closeDoneOnce   sync.Once // Declare a sync.Once variable
	startPort       int       // new global flag for starting port
)

var dashboardStarted bool

// New global counters for metrics.
var totalWorkflowsStarted int64
var totalWorkflowsCompleted int64
var totalWorkflowsFailed int64

// New flag for the dashboard port.
var dashboardPort int

var activeWorkflows int
var mutex sync.Mutex

// const rampUpBatchSize = 10      // Number of work items to start in each batch
// const rampUpDelay = time.Second // Delay between starting batches

// Define the showVersion flag at the package level
var showVersion = flag.Bool("version", false, "Show the application version")

// init initializes the command-line flags with default values.
var runAppPort int

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
	flag.IntVar(&dashboardPort, "dashboardPort", 9200, "Port for the dashboard server") // New flag
}

// New global variables for workflow durations.
var timingsMutex sync.Mutex
var workflowDurations []float64

// loadConfiguration reads and decodes a JSON configuration file into a Configuration struct.
func loadConfiguration(filePath string) *Configuration {
	if connect3270.Verbose {
		log.Printf("Loading configuration from %s", filePath)
	}
	configFile, err := os.Open(filePath)
	if err != nil {
		log.Fatalf("Error opening config file at %s: %v", filePath, err)
	}
	defer configFile.Close()

	var config Configuration
	decoder := json.NewDecoder(configFile)
	err = decoder.Decode(&config)
	if err != nil {
		log.Fatalf("Error decoding config JSON: %v", err)
	}

	if config.RampUpBatchSize <= 0 {
		config.RampUpBatchSize = 10
	}
	if config.RampUpDelay <= 0 {
		config.RampUpDelay = 1
	}

	return &config
}

// loadInputFile reads and parses the new input file format.
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

	// Add a Connect step as the first step.
	steps = append(steps, Step{
		Type: "Connect",
	})
	if connect3270.Verbose {
		log.Printf("Added initial Connect step")
	}

	// Parse the input file and extract steps.
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
			// Extract the key to be sent.
			key := strings.TrimPrefix(line, "yield ps.sendKeys(")
			key = strings.TrimSuffix(key, ");")
			key = strings.Trim(key, "'")

			// Determine the step type based on the key.
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

			// Create a new step and add it to the steps slice.
			step := Step{
				Type: stepType,
				Text: key,
			}
			steps = append(steps, step)
			if connect3270.Verbose {
				log.Printf("Added step: %s with text: %s", stepType, key)
			}
		} else if strings.HasPrefix(line, "yield wait.forText") {
			// Extract the text and position.
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
							Length: len(text), // Set the length to the length of the text.
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
			// Extract the coordinates from the comment line.
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
				// The next line should be the actual FillString step.
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

	// Add a Disconnect step as the last step.
	steps = append(steps, Step{
		Type: "Disconnect",
	})
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

// runWorkflow executes the workflow steps for a single instance and skips the entire workflow if any step fails.
func runWorkflow(scriptPort int, config *Configuration) error {
	startTime := time.Now()                    // <-- new timing start
	atomic.AddInt64(&totalWorkflowsStarted, 1) // Increment workflow start counter
	if connect3270.Verbose {
		log.Printf("Starting workflow for scriptPort %d", scriptPort)
	}

	// Increment activeWorkflows
	mutex.Lock()
	activeWorkflows++
	mutex.Unlock()

	e := connect3270.Emulator{
		Host:       config.Host,
		Port:       config.Port,
		ScriptPort: strconv.Itoa(scriptPort),
	}

	tmpFile, err := ioutil.TempFile("", "workflowOutput_")
	if err != nil {
		log.Printf("Error creating temporary file: %v", err)
		return err
	}
	tmpFileName := tmpFile.Name()
	tmpFile.Close() // Ensure the temporary file is closed immediately after creation

	e.InitializeOutput(tmpFileName, runAPI)

	workflowFailed := false

	// Load steps from the input file if specified
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
		case "Disconnect":
			if err := e.Disconnect(); err != nil {
				log.Printf("Error disconnecting: %v", err)
				workflowFailed = true
			}
		default:
			log.Printf("Unknown step type: %s", step.Type)
		}
	}

	// Decrement activeWorkflows when done.
	mutex.Lock()
	activeWorkflows--
	mutex.Unlock()

	// Record the duration and append it.
	duration := time.Since(startTime).Seconds()
	timingsMutex.Lock()
	workflowDurations = append(workflowDurations, duration)
	timingsMutex.Unlock()

	if workflowFailed {
		log.Printf("Workflow for scriptPort %d failed", scriptPort)
		atomic.AddInt64(&totalWorkflowsFailed, 1) // Update failed counter
	} else {
		if connect3270.Verbose {
			log.Printf("Workflow for scriptPort %d completed successfully", scriptPort)
		}
		// Ensure the file is properly closed before renaming it
		// Remove existing output file (if any) to prevent "Access is denied" errors.
		_ = os.Remove(config.OutputFilePath)
		if err := os.Rename(tmpFileName, config.OutputFilePath); err != nil {
			//log.Printf("Error renaming temporary file to output file: %v", err)
			pid := os.Getpid()
			uniqueOutputPath := fmt.Sprintf("%s.%d", config.OutputFilePath, pid)
			if err2 := os.Rename(tmpFileName, uniqueOutputPath); err2 != nil {
				log.Printf("Error renaming temporary file to unique output file: %v", err2)
			} else {
				log.Printf("Renamed temporary file to unique output file: %s", uniqueOutputPath)
			}
			return err
		}
		atomic.AddInt64(&totalWorkflowsCompleted, 1) // Update completed counter
	}

	return nil
}

// runAPIWorkflow runs the program in API mode, accepting and executing workflow configurations via HTTP requests.
func runAPIWorkflow() {
	if connect3270.Verbose {
		log.Println("Starting API server mode")
	}

	// Set the global Headless mode for all emulator instances
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

		// Create a new temporary file for this request
		tmpFile, err := ioutil.TempFile("", "workflowOutput_")
		if err != nil {
			log.Printf("Error creating temporary file: %v", err)
			sendErrorResponse(c, http.StatusInternalServerError, "Failed to create temporary file", err)
			return
		}
		defer tmpFile.Close()
		tmpFileName := tmpFile.Name()

		// Create a new Emulator instance for each request
		scriptPort := getNextAvailablePort()
		e := connect3270.NewEmulator(workflowConfig.Host, workflowConfig.Port, strconv.Itoa(scriptPort))

		// Initialize the output file
		err = e.InitializeOutput(tmpFileName, true)
		if err != nil {
			sendErrorResponse(c, http.StatusInternalServerError, "Failed to initialize output file", err)
			return
		}

		// Execute the workflow steps
		for _, step := range workflowConfig.Steps {
			if err := executeStep(e, step, tmpFileName); err != nil {
				sendErrorResponse(c, http.StatusInternalServerError, fmt.Sprintf("Workflow step '%s' failed", step.Type), err)
				e.Disconnect() // Ensure disconnection in case of error
				return
			}
		}

		// Read the contents of the output file after executing the workflow
		outputContents, err := e.ReadOutputFile(tmpFileName)
		if err != nil {
			sendErrorResponse(c, http.StatusInternalServerError, "Failed to read output file", err)
			return
		}

		e.Disconnect() // Disconnect after completing the workflow

		// Return the output file contents
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

// executeStep executes a single step in the workflow.
func executeStep(e *connect3270.Emulator, step Step, tmpFileName string) error {
	// Implement the logic for each step type
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

// new function to print ASCII banner
func printBanner() {
	// Simple ASCII banner with version info
	fmt.Println(`
	 ____ ___ ______ ___   _____                            _
	|___ \__ \____  / _ \ / ____|                          | |
	  __) | ) |  / / | | | |     ___  _ __  _ __   ___  ___| |_
	 |__ < / /  / /| | | | |    / _ \| '_ \| '_ \ / _ \/ __| __|
	 ___) / /_ / / | |_| | |___| (_) | | | | | | |  __/ (__| |_
	|____/____/_/   \___/ \_____\___/|_| |_|_| |_|\___|\___|\__|

Version: ` + version)
}

// main is the entry point of the program. It parses the command-line flags, sets global settings, and either runs the program in API mode or executes the workflows.
func main() {
	flag.Parse()
	printBanner() // Print the ASCII banner at startup

	// Set the initial lastUsedPort from the startPort flag value.
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

	// Start the dashboard if running in concurrent or runtime mode.
	if concurrent > 1 || runtimeDuration > 0 {
		go runDashboard() // Dashboard runs in its own goroutine.
	}

	// Check if runApp is specified
	if runApp != "" {
		switch runApp {
		case "1":
			app1.RunApplication(runAppPort) // Pass the port to the application
			return
		case "2":
			app2.RunApplication(runAppPort) // Pass the port to the application
			return
		// Add additional cases for other apps
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
		// If a dashboard is running, leave it up and inform the user.
		if concurrent > 1 && dashboardStarted {
			log.Printf("All workflows completed but the dashboard is still running on port %d. Press Ctrl+C to exit.", dashboardPort)
			select {} // Block forever so the dashboard keeps running
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

	// Start progress logging in a separate goroutine.
	go logActiveWorkflowsUntilDone()

	// Create a channel to act as a semaphore limiting the number of concurrent workflows.
	semaphore := make(chan struct{}, concurrent)
	var wg sync.WaitGroup

	// Loop until the overall runtime is reached.
	for time.Since(overallStart) < time.Duration(runtimeDuration)*time.Second {
		// Start a batch of workflows, up to rampUpBatchSize, or until runtime is reached.
		for time.Since(overallStart) < time.Duration(runtimeDuration)*time.Second {
			log.Printf("Increasing batch by %d, current size is %d, new total target is %d",
				config.RampUpBatchSize,
				len(semaphore),
				len(semaphore)+config.RampUpBatchSize)

			for i := 0; i < config.RampUpBatchSize; i++ {
				semaphore <- struct{}{}
				wg.Add(1)
				go func() {
					defer wg.Done()
					portToUse := getNextAvailablePort()
					err := runWorkflow(portToUse, config)
					if err != nil && connect3270.Verbose {
						log.Printf("Workflow on port %d error: %v", portToUse, err)
					}
					// Release the semaphore slot when done.
					<-semaphore
				}()
			}
			// Log the current number of active workflows.
			log.Printf("Currently active workflows: %d", len(semaphore))
			// Wait a short delay before starting the next workflow to gradually reach full concurrency.
			time.Sleep(time.Duration(config.RampUpDelay) * time.Second)
		}
		// Log the current number of active workflows after each batch.
		log.Printf("Currently active workflows: %d", len(semaphore))
		// Wait for a short delay before starting the next batch.
		time.Sleep(time.Duration(config.RampUpDelay) * time.Second)
	}

	// Wait for any in-flight workflows to finish.
	wg.Wait()
	log.Println("All workflows completed after runtimeDuration ended.")
}

// Optional logger goroutine:
func logActiveWorkflowsUntilDone() {
	for {
		active := getActiveWorkflows()
		log.Printf("Currently active workflows: %d", active)
		time.Sleep(1 * time.Second)
	}
}

func logActiveWorkflows(logDone chan struct{}) {
	if connect3270.Verbose {
		log.Println("Starting logActiveWorkflows")
	}
	for {
		select {
		case <-logDone:
			if connect3270.Verbose {
				log.Println("Stopping logActiveWorkflows")
			}
			return
		default:
			activeCount := getActiveWorkflows()
			log.Printf("Currently active workflows: %d", activeCount)
			time.Sleep(1 * time.Second)
		}
	}
}

func handleRuntimeDuration(runtimeDone chan struct{}, closeDoneOnce *sync.Once) {
	if runtimeDuration > 0 {
		time.Sleep(time.Duration(runtimeDuration) * time.Second)
		log.Println("Runtime duration reached. Not starting new workflows...")
	}
	closeDoneOnce.Do(func() {
		close(runtimeDone)
	})
}

func getActiveWorkflows() int {
	if connect3270.Verbose {
		log.Println("Starting getActiveWorkflows")
	}
	mutex.Lock()
	defer mutex.Unlock()
	return activeWorkflows
}

func startWorkflowsRampUp(overallStart time.Time, activeChan chan struct{}, config *Configuration, wg *sync.WaitGroup) {
	for {
		// Check if we're still within the overall runtimeDuration
		if time.Since(overallStart) >= time.Duration(runtimeDuration)*time.Second {
			if connect3270.Verbose {
				log.Println("Overall runtimeDuration reached, stopping new workflow initiation")
			}
			return
		}

		// Only start a new workflow if we haven't reached the concurrent limit
		if getActiveWorkflows() < concurrent {
			if connect3270.Verbose {
				log.Println("Initiating a new workflow")
			}
			startWorkflowBatch(activeChan, config, wg)
		}
		time.Sleep(time.Duration(config.RampUpDelay) * time.Second) // Sleep briefly before checking again
	}
}

func startWorkflowBatch(activeChan chan struct{}, config *Configuration, wg *sync.WaitGroup) {
	if connect3270.Verbose {
		log.Println("Starting startWorkflowBatch")
	}

	mutex.Lock()
	availableSlots := concurrent - activeWorkflows
	if availableSlots <= 0 {
		mutex.Unlock()
		time.Sleep(time.Duration(config.RampUpDelay) * time.Second) // Throttle batch initiation
		return
	}
	workflowsToStart := min(config.RampUpBatchSize, availableSlots)
	mutex.Unlock()

	for j := 0; j < workflowsToStart; j++ {
		wg.Add(1)

		// Mark the slot as taken *before* starting the goroutine:
		mutex.Lock()
		activeWorkflows++
		lastUsedPort++
		portToUse := lastUsedPort
		mutex.Unlock()

		go func() {
			defer wg.Done()
			// optional: use the channel if you like
			activeChan <- struct{}{}

			// Now run the workflow. Remove the code in runWorkflow that increments/decrements activeWorkflows.
			err := runWorkflow(portToUse, config)
			if err != nil {
				log.Printf("Workflow on port %d returned an error: %v", portToUse, err)
			}

			// Release the concurrency slot.
			mutex.Lock()
			activeWorkflows--
			mutex.Unlock()

			<-activeChan
		}()
	}
}

func getNextAvailablePort() int {
	mutex.Lock()
	defer mutex.Unlock()
	for {
		lastUsedPort++
		if isPortAvailable(lastUsedPort) {
			return lastUsedPort
		}
		log.Printf("Port %d is in use, trying next port", lastUsedPort)
	}
}

// new helper to check if a port is available
func isPortAvailable(port int) bool {
	addr := ":" + strconv.Itoa(port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return false
	}
	ln.Close()
	return true
}

// Helper function to find the minimum of two integers
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
			// These steps don't require additional fields.
			continue
		case "CheckValue", "FillString":
			// These steps require Coordinates and Text.
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

// New function to launch the dashboard server.
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

	// Clear old metrics PID files since we are serving the dashboard on this instance.
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

	http.HandleFunc("/dashboard", func(w http.ResponseWriter, r *http.Request) {
		// Load each metrics file individually
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
		}
		metricsJSON, _ := json.Marshal(metricsList)

		// Set up auto-refresh form settings.
		w.Header().Set("Content-Type", "text/html")
		autoRefresh := r.URL.Query().Get("autoRefresh")
		refreshPeriod := r.URL.Query().Get("refreshPeriod")
		if refreshPeriod == "" {
			refreshPeriod = "5" // Default refresh period of 5 seconds
		}
		checked := ""
		if autoRefresh == "true" {
			checked = "checked"
		}
		sel5, sel10, sel15, sel30 := "", "", "", ""
		switch refreshPeriod {
		case "5":
			sel5 = "selected"
		case "10":
			sel10 = "selected"
		case "15":
			sel15 = "selected"
		case "30":
			sel30 = "selected"
		}
		// Begin HTML output.
		fmt.Fprintf(w, "<html><head><title>3270Connect Dashboard</title>")
		if autoRefresh == "true" {
			fmt.Fprintf(w, `<meta http-equiv="refresh" content="%s">`, refreshPeriod)
		}
		fmt.Fprintf(w, `<script src="https://cdn.jsdelivr.net/npm/chart.js"></script>
		<style>
		  body { font-family: Arial, sans-serif; margin: 20px; }
		  canvas { border: 1px solid #ccc; }
		</style>
		</head>`)
		fmt.Fprintf(w, "<body>")
		fmt.Fprintf(w, "<h1>3270Connect Dashboard (Per PID Metrics)</h1>")
		// Aggregate counts from all metrics files.
		var totalStarted, totalCompleted, totalFailed, active int
		for _, m := range metricsList {
			totalStarted += int(m.TotalWorkflowsStarted)
			totalCompleted += int(m.TotalWorkflowsCompleted)
			totalFailed += int(m.TotalWorkflowsFailed)
			active += m.ActiveWorkflows
		}
		fmt.Fprintf(w, "<p>Active Workflows (aggregated): %d</p>", active)
		fmt.Fprintf(w, "<p>Total Workflows Started (aggregated): %d</p>", totalStarted)
		fmt.Fprintf(w, "<p>Total Workflows Completed (aggregated): %d</p>", totalCompleted)
		fmt.Fprintf(w, "<p>Total Workflows Failed (aggregated): %d</p>", totalFailed)
		// Render auto-refresh form.
		fmt.Fprintf(w, `<form id="autoRefreshForm" method="get" style="margin-bottom:20px;">
			<label for="autoRefreshToggle">Auto Refresh: </label>
			<input type="checkbox" id="autoRefreshToggle" name="autoRefresh" value="true" %s onchange="this.form.submit()">
			&nbsp;&nbsp;
			<label for="refreshPeriodSelect">Refresh Period (seconds): </label>
			<select id="refreshPeriodSelect" name="refreshPeriod" onchange="this.form.submit()">
				<option value="5" %s>5</option>
				<option value="10" %s>10</option>
				<option value="15" %s>15</option>
				<option value="30" %s>30</option>
			</select>
		</form>`, checked, sel5, sel10, sel15, sel30)
		// Canvas for the graph.
		fmt.Fprintf(w, `<canvas id="durationChart" width="800" height="400"></canvas>`)
		// JavaScript to plot a line for each PID's durations.
		fmt.Fprintf(w, `<script>
				document.addEventListener('DOMContentLoaded', function() {
					var metricsData = %s;
					// Determine the maximum number of workflow durations among all PIDs.
					var maxCount = 0;
					metricsData.forEach(function(metric) {
						if (metric.durations && metric.durations.length > maxCount) {
							maxCount = metric.durations.length;
						}
					});
					var labels = [];
					for (var i = 0; i < maxCount; i++) {
						labels.push("Workflow " + (i + 1));
					}
					// Predefined colors for multiple lines.
					var colors = ['rgba(75, 192, 192, 1)', 'rgba(192, 75, 192, 1)', 'rgba(192, 192, 75, 1)', 'rgba(75, 75, 192, 1)', 'rgba(192, 75, 75, 1)'];
					var datasets = [];
					metricsData.forEach(function(metric, index) {
						datasets.push({
							label: "PID " + metric.pid,
							data: metric.durations,
							borderColor: colors[index %% colors.length],
							backgroundColor: colors[index %% colors.length].replace("1)", "0.2)"),
							fill: false,
							tension: 0.1
						});
					});
					var ctx = document.getElementById("durationChart").getContext("2d");
					new Chart(ctx, {
						type: "line",
						data: {
							labels: labels,
							datasets: datasets
						},
						options: {
							animation: { duration: 0 },
							scales: {
								y: {
									beginAtZero: true,
									title: { display: true, text: "Duration (seconds)" }
								}
							}
						}
					});
				});
				</script>`, metricsJSON)
		fmt.Fprintf(w, "</body></html>")
	})
	log.Printf("Dashboard available at http://localhost:%d/dashboard", dashboardPort)
	// Periodically update our local metrics.
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

// Modify Metrics struct to include workflow durations.
type Metrics struct {
	PID                     int       `json:"pid"`
	ActiveWorkflows         int       `json:"activeWorkflows"`
	TotalWorkflowsStarted   int64     `json:"totalWorkflowsStarted"`
	TotalWorkflowsCompleted int64     `json:"totalWorkflowsCompleted"`
	TotalWorkflowsFailed    int64     `json:"totalWorkflowsFailed"`
	Durations               []float64 `json:"durations"` // New field: durations in seconds
}

// New function to update local metrics file.
func updateMetricsFile() {
	pid := os.Getpid()
	timingsMutex.Lock()
	durationsCopy := make([]float64, len(workflowDurations))
	copy(durationsCopy, workflowDurations)
	timingsMutex.Unlock()
	metrics := Metrics{
		PID:                     pid,
		ActiveWorkflows:         getActiveWorkflows(),
		TotalWorkflowsStarted:   atomic.LoadInt64(&totalWorkflowsStarted),
		TotalWorkflowsCompleted: atomic.LoadInt64(&totalWorkflowsCompleted),
		TotalWorkflowsFailed:    atomic.LoadInt64(&totalWorkflowsFailed),
		Durations:               durationsCopy,
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

// Modify aggregateMetrics to merge durations arrays across files.
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
	}
	return agg
}
