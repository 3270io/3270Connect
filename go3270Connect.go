package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	connect3270 "github.com/3270io/3270Connect/connect3270"
	"github.com/3270io/3270Connect/sampleapps/app1"
	app2 "github.com/3270io/3270Connect/sampleapps/app2"

	"github.com/gin-gonic/gin"
)

const version = "1.0.4.0"

// Configuration holds the settings for the terminal connection and the steps to be executed.
type Configuration struct {
	Host         string
	Port         int
	HTMLFilePath string `json:"HTMLFilePath"`
	Steps        []Step
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

const rampUpBatchSize = 5       // Number of work items to start in each batch
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

func clearTmpFiles() {
	files, err := filepath.Glob("/tmp/x3270*")
	if err != nil {
		log.Fatalf("Error reading /tmp directory: %v", err)
	}

	for _, file := range files {
		if err := os.Remove(file); err != nil {
			log.Printf("Failed to remove %s: %v", file, err)
		} else {
			if connect3270.Verbose {
				log.Printf("Removed leftover file: %s", file)
			}
		}
	}
}

// loadConfiguration reads and decodes a JSON configuration file into a Configuration struct.
func loadConfiguration(filePath string) *Configuration {
	if connect3270.Verbose {
		log.Printf("Loading configuration from %s", filePath)
	}
	configFile, err := os.Open(filePath)
	if err != nil {
		log.Fatalf("Error opening config file: %v", err)
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

// runWorkflows executes the workflow multiple times, either concurrently or sequentially.
func runWorkflows(numOfWorkflows int, config *Configuration) {
	if connect3270.Verbose {
		log.Printf("Starting %d workflows", numOfWorkflows)
	}

	tasks := make(chan int, numOfWorkflows) // Corrected this line

	// Start workers
	for i := 0; i < numOfWorkflows; i++ {
		go func() {
			for scriptPort := range tasks {
				runWorkflow(scriptPort, config)
				time.Sleep(1 * time.Second)
				wg.Done()
			}
		}()
	}

	// Feed tasks to the channel
	for i := 1; i <= numOfWorkflows; i++ {
		wg.Add(1)
		mutex.Lock()
		lastUsedPort++
		tasks <- lastUsedPort
		mutex.Unlock()
	}

	wg.Wait()
	close(tasks) // Close the tasks channel after all tasks have been fed
}

// runWorkflow executes the workflow steps for a single instance and skips the entire workflow if any step fails.
func runWorkflow(scriptPort int, config *Configuration) {
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

	// Initialize the HTML file with run details (call this at the beginning)
	htmlFilePath := config.HTMLFilePath
	if err := e.InitializeHTMLFile(htmlFilePath); err != nil {
		log.Printf("Error initializing HTML file: %v", err)
	}

	// Flag to track if any step fails
	workflowFailed := false

	// Iterate through the steps in the configuration
	for _, step := range config.Steps {
		if workflowFailed {
			// If any step has already failed, skip the remaining steps
			break
		}

		switch step.Type {
		case "InitializeHTMLFile":
			if err := e.InitializeHTMLFile(htmlFilePath); err != nil {
				log.Printf("Error initializing HTML file: %v", err)
				workflowFailed = true
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
			if err := e.AsciiScreenGrab(htmlFilePath, true); err != nil {
				log.Printf("Error capturing and appending ASCII screen: %v", err)
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
}

// runAPIWorkflow runs the program in API mode, accepting and executing workflow configurations via HTTP requests.
func runAPIWorkflow() {
	if connect3270.Verbose {
		log.Println("Starting API server mode")
	}

	// Set headless mode for API
	connect3270.Headless = true

	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()
	r.SetTrustedProxies(nil)

	r.POST("/api/execute", func(c *gin.Context) {
		var workflowConfig Configuration
		if err := c.ShouldBindJSON(&workflowConfig); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"returnCode": http.StatusBadRequest,
				"status":     "error",
				"message":    "Invalid request payload",
				"error":      err.Error(),
			})
			return
		}

		// Create an emulator instance
		e := connect3270.Emulator{
			Host: workflowConfig.Host,
			Port: workflowConfig.Port,
		}

		// Attempt to initialize the HTML file with run details
		htmlFilePath := workflowConfig.HTMLFilePath
		if err := e.InitializeHTMLFile(htmlFilePath); err != nil {
			sendErrorResponse(c, http.StatusInternalServerError, "Failed to initialize HTML file", err)
			return
		}

		// Defer the disconnection of the emulator to ensure it's done regardless of error or not.
		defer func() {
			if err := e.Disconnect(); err != nil {
				log.Printf("Error disconnecting: %v\n", err)
			}
		}()

		// Execute the workflow steps
		for _, step := range workflowConfig.Steps {
			if err := executeStep(&e, step, htmlFilePath); err != nil {
				sendErrorResponse(c, http.StatusInternalServerError, fmt.Sprintf("Workflow step '%s' failed", step.Type), err)
				return
			}
		}

		// After executing the workflow, read the contents of the HTML file
		htmlContents, err := e.ReadHTMLFile(htmlFilePath)
		if err != nil {
			sendErrorResponse(c, http.StatusInternalServerError, "Failed to read HTML file", err)
			return
		}

		// Return both the status message and the HTML file contents
		c.JSON(http.StatusOK, gin.H{
			"returnCode":   http.StatusOK,
			"status":       "okay",
			"message":      "Workflow executed successfully",
			"htmlContents": htmlContents,
		})
	})

	apiAddr := fmt.Sprintf(":%d", apiPort)
	log.Printf("API server is running on %s", apiAddr)
	r.Run(apiAddr)
}

func executeStep(e *connect3270.Emulator, step Step, htmlFilePath string) error {
	// Implement the logic for each step type
	switch step.Type {
	case "InitializeHTMLFile":
		// Return an error instead of using log.Fatalf, so it can be handled properly
		err := e.InitializeHTMLFile(htmlFilePath)
		if err != nil {
			return fmt.Errorf("error initializing HTML file: %v", err)
		}
	case "Connect":
		return e.Connect()
	case "CheckValue":
		_, err := e.GetValue(step.Coordinates.Row, step.Coordinates.Column, step.Coordinates.Length)
		return err
	case "FillString":
		return e.FillString(step.Coordinates.Row, step.Coordinates.Column, step.Text)
	case "AsciiScreenGrab":
		return e.AsciiScreenGrab(htmlFilePath, true) // Use the passed htmlFilePath
	case "PressEnter":
		return e.Press(connect3270.Enter)
	case "Disconnect":
		return e.Disconnect()
	default:
		return fmt.Errorf("unknown step type: %s", step.Type)
	}
	return nil // No error occurred, return nil
}

func sendErrorResponse(c *gin.Context, statusCode int, message string, err error) {
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
	done := make(chan struct{})
	var wg sync.WaitGroup
	var closeDoneOnce sync.Once

	// Goroutine for logging active workflows
	go logActiveWorkflows(done)

	// Goroutine to handle runtime duration
	go handleRuntimeDuration(done, &closeDoneOnce)

	// Ramp-up logic
	go startWorkflowsRampUp(activeChan, config, done, &wg, &closeDoneOnce)

	// Block until all workflows are done
	wg.Wait()

	// Close the 'done' channel using sync.Once to ensure it's only closed once
	closeDoneOnce.Do(func() {
		close(done)
	})
}

func logActiveWorkflows(done chan struct{}) {
	for {
		select {
		case <-done:
			return
		default:
			activeCount := getActiveWorkflows() // Use your own counter with safe access
			log.Printf("Currently active workflows: %d", activeCount)
			time.Sleep(1 * time.Second)
		}
	}
}

func handleRuntimeDuration(done chan struct{}, closeDoneOnce *sync.Once) {
	if runtimeDuration > 0 {
		time.Sleep(time.Duration(runtimeDuration) * time.Second)
		log.Println("Runtime duration reached. Not starting new workflows...")
		closeDoneOnce.Do(func() {
			close(done)
		})
	}
}

func incrementActiveWorkflows() {
	mutex.Lock()
	defer mutex.Unlock()
	activeWorkflows++
}

func decrementActiveWorkflows() {
	mutex.Lock()
	defer mutex.Unlock()
	activeWorkflows--
}

func getActiveWorkflows() int {
	mutex.Lock()
	defer mutex.Unlock()
	return activeWorkflows
}

func startWorkflowsRampUp(activeChan chan struct{}, config *Configuration, done chan struct{}, wg *sync.WaitGroup, closeDoneOnce *sync.Once) {
	for {
		select {
		case <-done:
			return
		default:
			if getActiveWorkflows() < concurrent {
				startWorkflowBatch(activeChan, config, wg) // Pass config as a pointer
			}
			time.Sleep(rampUpDelay)
		}
	}
}

func startWorkflowBatch(activeChan chan struct{}, config *Configuration, wg *sync.WaitGroup) {
	for j := 0; j < rampUpBatchSize; j++ {
		if getActiveWorkflows() >= concurrent {
			// If the active workflows reach the limit, don't start more.
			break
		}

		wg.Add(1)                  // Increment the WaitGroup counter before starting the goroutine
		incrementActiveWorkflows() // Safely increment the active workflows count

		go func() {
			defer wg.Done()                  // Signal the WaitGroup that the goroutine has finished
			defer decrementActiveWorkflows() // Safely decrement the active workflows count

			// Acquire a lock to safely increment the shared port counter
			mutex.Lock()
			lastUsedPort++
			portToUse := lastUsedPort
			mutex.Unlock()

			activeChan <- struct{}{}       // Block here if activeChan is full
			runWorkflow(portToUse, config) // Execute the workflow, passing a pointer to config
			<-activeChan                   // Release a spot in the channel once the workflow is done
		}()
	}
}
