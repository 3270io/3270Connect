package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"

	"time"

	connect3270 "github.com/3270io/3270Connect/connect3270"
	"github.com/3270io/3270Connect/sampleapps/app1"
	app2 "github.com/3270io/3270Connect/sampleapps/app2"

	"github.com/gin-gonic/gin"
)

const version = "1.0.4.6"

// Configuration holds the settings for the terminal connection and the steps to be executed.
type Configuration struct {
	Host           string
	Port           int
	OutputFilePath string `json:"OutputFilePath"`
	Steps          []Step
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
	lastUsedPort    int       = 5000 // starting port number
	closeDoneOnce   sync.Once        // Declare a sync.Once variable
)

var activeWorkflows int
var mutex sync.Mutex

const rampUpBatchSize = 10      // Number of work items to start in each batch
const rampUpDelay = time.Second // Delay between starting batches

// Define the showVersion flag at the package level
var showVersion = flag.Bool("version", false, "Show the application version")

// init initializes the command-line flags with default values.
func init() {
	flag.StringVar(&configFile, "config", "workflow.json", "Path to the configuration file")
	flag.BoolVar(&showHelp, "help", false, "Show usage information")
	flag.BoolVar(&runAPI, "api", false, "Run as API")
	flag.IntVar(&apiPort, "api-port", 8080, "API port")
	flag.IntVar(&concurrent, "concurrent", 1, "Number of concurrent workflows")
	flag.BoolVar(&headless, "headless", false, "Run go3270 in headless mode")
	flag.BoolVar(&verbose, "verbose", false, "Run go3270 in verbose mode")
	flag.IntVar(&runtimeDuration, "runtime", 0, "Duration to run workflows in seconds. Only used in concurrent mode.")
	flag.StringVar(&runApp, "runApp", "1", "Select which sample 3270 application to run (e.g., '1' for app1, '2' for app2)")
}

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

	return &config
}

// runWorkflow executes the workflow steps for a single instance and skips the entire workflow if any step fails.
func runWorkflow(scriptPort int, config *Configuration) error {
	if connect3270.Verbose {
		log.Printf("Starting workflow for scriptPort %d", scriptPort)
	}

	mutex.Lock()
	activeWorkflows++
	mutex.Unlock()

	// Create an emulator instance
	e := connect3270.Emulator{
		Host:       config.Host,
		Port:       config.Port,
		ScriptPort: strconv.Itoa(scriptPort), // Convert int to string
	}

	// Create a temporary file for this workflow run
	tmpFile, err := ioutil.TempFile("", "workflowOutput_")
	if err != nil {
		log.Printf("Error creating temporary file: %v", err)
		return err
	}
	defer tmpFile.Close()
	tmpFileName := tmpFile.Name()

	e.InitializeOutput(tmpFileName, runAPI)

	// Flag to track if any step fails
	workflowFailed := false

	// Iterate through the steps in the configuration
	for _, step := range config.Steps {
		if workflowFailed {
			// If any step has already failed, skip the remaining steps
			break
		}

		switch step.Type {
		case "InitializeOutput":
			// Return an error instead of using log.Fatalf, so it can be handled properly
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
				break // Skip remaining steps if CheckValue fails
			}
			v = strings.TrimSpace(v)
			if connect3270.Verbose {
				log.Println("Retrieved value: " + v)
			}
			if v != step.Text {
				log.Printf("Login failed. Expected: %s, Found: %s", step.Text, v)
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
		case "Disconnect":
			if err := e.Disconnect(); err != nil {
				log.Printf("Error disconnecting: %v", err)
				workflowFailed = true
			}
		default:
			log.Printf("Unknown step type: %s", step.Type)
		}
		//time.Sleep(1 * time.Second) // Optional: Add a delay between steps
	}

	mutex.Lock()
	activeWorkflows--
	mutex.Unlock()

	if workflowFailed {
		// Log that the workflow failed and skip any additional processing
		log.Printf("Workflow for scriptPort %d failed", scriptPort)
	} else {
		if connect3270.Verbose {
			log.Printf("Workflow for scriptPort %d completed successfully", scriptPort)
		}
	}

	if workflowFailed {
		// Log that the workflow failed and skip any additional processing
		log.Printf("Workflow for scriptPort %d failed", scriptPort)
	} else {
		if connect3270.Verbose {
			log.Printf("Workflow for scriptPort %d completed successfully", scriptPort)
		}
		// Rename the temporary file to the desired output file path
		err := os.Rename(tmpFileName, config.OutputFilePath)
		if err != nil {
			log.Printf("Error renaming temporary file to output file: %v", err)
			return err
		}
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

	// Create a temporary file for this workflow run
	tmpFile, err := ioutil.TempFile("", "workflowOutput_")
	if err != nil {
		log.Printf("Error creating temporary file: %v", err)
	}
	defer tmpFile.Close()
	tmpFileName := tmpFile.Name()

	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()
	r.SetTrustedProxies(nil)

	r.POST("/api/execute", func(c *gin.Context) {
		var workflowConfig Configuration
		if err := c.ShouldBindJSON(&workflowConfig); err != nil {
			sendErrorResponse(c, http.StatusBadRequest, "Invalid request payload", err)
			return
		}

		// Create a new Emulator instance for each request
		scriptPort := getNextAvailablePort()
		e := connect3270.NewEmulator(workflowConfig.Host, workflowConfig.Port, strconv.Itoa(scriptPort))

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
		return e.FillString(step.Coordinates.Row, step.Coordinates.Column, step.Text)
	case "AsciiScreenGrab":
		return e.AsciiScreenGrab(tmpFileName, runAPI)
	case "PressEnter":
		return e.Press(connect3270.Enter)
	case "Disconnect":
		return e.Disconnect()
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

// main is the entry point of the program. It parses the command-line flags, sets global settings, and either runs the program in API mode or executes the workflows.
func main() {

	// Check if '-runApp' is provided without an argument and if so, set the default to "1".
	foundRunApp := false
	for i, arg := range os.Args {
		if arg == "-runApp" {
			foundRunApp = true
			// If '-runApp' is the last argument or the next argument is another flag, set the default to "1".
			if i+1 == len(os.Args) || strings.HasPrefix(os.Args[i+1], "-") {
				os.Args[i] = "-runApp=1" // Correctly set the default value for '-runApp'.
			}
		}
	}

	flag.Parse()

	// Now showVersion is accessible here, and you can dereference it to get the value.
	if *showVersion {
		printVersionAndExit()
	}

	if showHelp {
		printHelpAndExit()
	}

	setGlobalSettings()

	// If runApp is not empty and not "1" (which is default), then override the default value
	if foundRunApp {
		switch runApp {
		case "1":
			app1.RunApplication()
			return
		case "2":
			app2.RunApplication()
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
			//runWorkflows(concurrent, config)
			runConcurrentWorkflows(config)
		} else {
			runWorkflow(7000, config) // 7000 or a default port for non-concurrent execution
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
	activeChan := make(chan struct{}, concurrent)
	var wg sync.WaitGroup
	var closeLogDoneOnce, closeRuntimeDoneOnce sync.Once
	logDone := make(chan struct{})
	runtimeDone := make(chan struct{})

	// Always run logActiveWorkflows goroutine for the entire duration of concurrent workflows
	go logActiveWorkflows(logDone)

	// Handle runtime duration, controlling the initiation of new workflows
	go handleRuntimeDuration(runtimeDone, &closeRuntimeDoneOnce)

	// Start the concurrent workflows
	startWorkflowsRampUp(activeChan, config, runtimeDone, &wg)

	// Wait for all workflows to complete
	wg.Wait()

	// Close the logDone channel to stop logActiveWorkflows
	closeLogDoneOnce.Do(func() {
		close(logDone)
	})

	// Close the runtimeDone channel to stop startWorkflowsRampUp
	closeRuntimeDoneOnce.Do(func() {
		close(runtimeDone)
	})

	log.Println("All workflows completed")
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

func startWorkflowsRampUp(activeChan chan struct{}, config *Configuration, runtimeDone chan struct{}, wg *sync.WaitGroup) {
	if connect3270.Verbose {
		log.Println("Starting startWorkflowsRampUp")
	}
	for {
		select {
		case <-runtimeDone:
			// When runtime is done, stop starting new workflows
			if connect3270.Verbose {
				log.Println("Runtime duration reached, stopping new workflow initiation")
			}
			return
		default:
			// Only start a new workflow if we haven't reached the concurrent limit
			if getActiveWorkflows() < concurrent {
				if connect3270.Verbose {
					log.Println("Initiating a new workflow")
				}
				startWorkflowBatch(activeChan, config, wg)
			}
			time.Sleep(rampUpDelay) // Sleep for a brief period before checking again
		}
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
		time.Sleep(rampUpDelay) // Throttle batch initiation
	}

	workflowsToStart := min(rampUpBatchSize, availableSlots)
	//activeWorkflows += workflowsToStart
	mutex.Unlock()

	for j := 0; j < workflowsToStart; j++ {
		if activeWorkflows >= concurrent {
			break
		}
		activeWorkflows++

		wg.Add(1)

		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("Recovered from panic in goroutine: %v", r)
				}
			}()
			defer wg.Done()
			activeWorkflows--

			mutex.Lock()
			lastUsedPort++
			portToUse := lastUsedPort
			mutex.Unlock()

			activeChan <- struct{}{}
			runWorkflow(portToUse, config)
			<-activeChan
		}()

	}
	//time.Sleep(rampUpDelay)
}

func getNextAvailablePort() int {
	mutex.Lock()
	defer mutex.Unlock()
	lastUsedPort++
	return lastUsedPort
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
