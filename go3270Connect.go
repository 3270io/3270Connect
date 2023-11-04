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
	app1 "github.com/3270io/3270Connect/sampleapps"

	"github.com/gin-gonic/gin"
)

const version = "1.0.2"

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
	runApp          bool
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
	flag.BoolVar(&runApp, "runApp", false, "Run app1 sample 3270 application")

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

// runWorkflow executes the workflow steps for a single instance.
// runWorkflow executes the workflow steps for a single instance.
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

	// Iterate through the steps in the configuration
	for _, step := range config.Steps {
		switch step.Type {
		case "InitializeHTMLFile":
			if err := e.InitializeHTMLFile(htmlFilePath); err != nil {
				log.Printf("Error initializing HTML file: %v", err)
			}
		case "Connect":
			if err := e.Connect(); err != nil {
				log.Printf("Error connecting to terminal: %v", err)
			}
		case "CheckValue":
			v, err := e.GetValue(step.Coordinates.Row, step.Coordinates.Column, step.Coordinates.Length)
			if err != nil {
				log.Printf("Error getting value: %v", err)
			}
			v = strings.TrimSpace(v)
			if connect3270.Verbose {
				log.Println("Retrieved value: " + v)
			}
			if v != step.Text {
				log.Printf("Login failed. Expected: %s, Found: %s", step.Text, v)
				if err := e.Disconnect(); err != nil {
					log.Printf("Error disconnecting: %v", err)
				}
				return
			}
		case "FillString":
			if err := e.FillString(step.Coordinates.Row, step.Coordinates.Column, step.Text); err != nil {
				log.Printf("Error setting text: %v", err)
			}
		case "AsciiScreenGrab":
			if err := e.AsciiScreenGrab(htmlFilePath, true); err != nil {
				log.Printf("Error capturing and appending ASCII screen: %v", err)
			}
		case "PressEnter":
			if err := e.Press(connect3270.Enter); err != nil {
				log.Printf("Error pressing Enter: %v", err)
			}
		case "Disconnect":
			if err := e.Disconnect(); err != nil {
				log.Printf("Error disconnecting: %v", err)
			}
		default:
			log.Printf("Unknown step type: %s", step.Type)
		}
	}

	mutex.Lock()
	activeWorkflows--
	mutex.Unlock()

	if connect3270.Verbose {
		log.Printf("Workflow for scriptPort %d completed successfully", scriptPort)
	}
}

// runAPIWorkflow runs the program in API mode, accepting and executing workflow configurations via HTTP requests.
func runAPIWorkflow() {
	if connect3270.Verbose {
		log.Println("Starting API server mode")
	}
	r := gin.Default()

	r.POST("/api/execute", func(c *gin.Context) {
		var workflowConfig Configuration
		if err := c.ShouldBindJSON(&workflowConfig); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Create an emulator instance
		e := connect3270.Emulator{
			Host: workflowConfig.Host,
			Port: workflowConfig.Port,
		}

		// Initialize the HTML file with run details (call this at the beginning)
		htmlFilePath := workflowConfig.HTMLFilePath
		if err := e.InitializeHTMLFile(htmlFilePath); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Iterate through the steps in the configuration
		for _, step := range workflowConfig.Steps {
			switch step.Type {
			case "InitializeHTMLFile":
				if err := e.InitializeHTMLFile(htmlFilePath); err != nil {
					log.Fatalf("Error initializing HTML file: %v\n", err)
				}
			case "Connect":
				if err := e.Connect(); err != nil {
					log.Fatalf("Error connecting to terminal: %v\n", err)
				}
			case "CheckValue":
				v, err := e.GetValue(step.Coordinates.Row, step.Coordinates.Column, step.Coordinates.Length)
				if err != nil {
					log.Fatalf("Error getting value: %v", err)
				}
				v = strings.TrimSpace(v)
				if connect3270.Verbose {
					log.Println("Retrieved value: " + v)
				}
				if v != step.Text {
					log.Printf("Login failed. Expected: %s, Found: %s\n", step.Text, v)
					if err := e.Disconnect(); err != nil {
						log.Fatalf("Error disconnecting: %v", err)
					}
					return
				}
			case "FillString":
				if err := e.FillString(step.Coordinates.Row, step.Coordinates.Column, step.Text); err != nil {
					log.Fatalf("Error setting text: %v\n", err)
				}
			case "AsciiScreenGrab":
				if err := e.AsciiScreenGrab(htmlFilePath, true); err != nil {
					log.Fatalf("Error capturing and appending ASCII screen: %v", err)
				}
			case "PressEnter":
				if err := e.Press(connect3270.Enter); err != nil {
					log.Fatalf("Error pressing Enter: %v\n", err)
				}
			case "Disconnect":
				if err := e.Disconnect(); err != nil {
					log.Fatalf("Error disconnecting: %v\n", err)
				}
			default:
				log.Printf("Unknown step type: %s\n", step.Type)
			}
		}

		c.JSON(http.StatusOK, gin.H{"message": "Workflow executed successfully"})
	})

	apiAddr := fmt.Sprintf(":%d", apiPort)
	log.Printf("API server is running on %s", apiAddr)
	r.Run(apiAddr)
}

// main is the entry point of the program. It parses the command-line flags, sets global settings, and either runs the program in API mode or executes the workflows.
func main() {
	if connect3270.Verbose {
		log.Println("Program started")
	}

	clearTmpFiles()

	showVersion := flag.Bool("version", false, "Show the application version")
	flag.Parse()

	if *showVersion {
		fmt.Printf("3270Connect Version: %s\n", version)
		os.Exit(0)
	}

	if showHelp {
		fmt.Printf("3270Connect Version: %s\n", version)
		flag.Usage()
		return
	}

	if runApp {
		// Run the bank application
		app1.RunApplication()
		return
	}

	connect3270.Headless = headless
	connect3270.Verbose = verbose

	if runAPI {
		runAPIWorkflow()
	} else {
		config := loadConfiguration(configFile)
		if concurrent > 1 {
			// Use a buffered channel to control the number of active workflows
			activeChan := make(chan struct{}, concurrent)

			// Goroutine to log active workflows
			go func() {
				for {
					select {
					case <-done:
						return
					default:
						mutex.Lock()
						log.Printf("Currently active workflows: %d", activeWorkflows)
						mutex.Unlock()
						time.Sleep(1 * time.Second)
					}
				}
			}()

			// Goroutine to handle runtime duration
			if runtimeDuration > 0 {
				go func() {
					time.Sleep(time.Duration(runtimeDuration) * time.Second)
					log.Println("Runtime duration reached. Not starting new workflows...")
					closeDoneOnce.Do(func() {
						close(done)
					})
				}()

				// Ramp-up logic (continue to use rampUpBatchSize and rampUpDelay)
				go func() {
					for {
						if activeWorkflows >= concurrent {
							time.Sleep(1 * time.Second)
							continue
						}

						for j := 0; j < rampUpBatchSize && activeWorkflows < concurrent; j++ {
							select {
							case <-done:
								// If runtime duration is reached, don't start new workflows
								return
							default:
								mutex.Lock()
								lastUsedPort++
								portToUse := lastUsedPort
								mutex.Unlock()
								activeChan <- struct{}{} // Block here if activeChan is full

								// Increment the WaitGroup for the new goroutine
								wg.Add(1)

								// Start the work item in a goroutine
								go func(port int) {
									defer wg.Done() // Decrement the WaitGroup when the goroutine completes
									runWorkflow(port, config)
									<-activeChan // Release a spot in the channel once the workflow is done
								}(portToUse)
							}
						}
						time.Sleep(rampUpDelay)
					}
				}()

				// Continue displaying active workflows after runtime duration message
				<-done // Wait for runtime duration to end

				// Wait until all active workflows have completed
				for activeWorkflows > 0 {
					time.Sleep(1 * time.Second)
				}

				log.Println("Active workflows is now zero. Shutting down...")

				// Close the 'done' channel using sync.Once to ensure it's only closed once
				closeDoneOnce.Do(func() {
					close(done)
				})

			} else {
				//log.Printf("Else statement for runtime not > 0") // Debugging line
				// Run concurrent workflows without runtime duration
				//log.Printf("#concurrent %d", concurrent) // Debugging line
				go func() {
					for i := 0; i < concurrent; i++ {
						//log.Printf("#1") // Debugging line
						mutex.Lock()
						lastUsedPort++
						portToUse := lastUsedPort
						//activeWorkflows++
						mutex.Unlock()
						//log.Printf("#2")         // Debugging line
						activeChan <- struct{}{} // Block here if activeChan is full
						//log.Printf("#3")         // Debugging line
						// Increment the WaitGroup for the new goroutine
						wg.Add(1)
						//log.Printf("#4") // Debugging line
						// Start the work item in a goroutine
						go func(port int) {
							//log.Printf("#5") // Debugging line
							defer wg.Done() // Decrement the WaitGroup when the goroutine completes
							//log.Printf("Starting workflow for port %d", port) // Debugging line

							// Add more debugging lines here if needed

							runWorkflow(port, config)

							// Add more debugging lines here if needed

							<-activeChan // Release a spot in the channel once the workflow is done
							//log.Printf("Workflow for port %d completed", port) // Debugging line

							// Decrement the activeWorkflows counter
							mutex.Lock()
							//activeWorkflows--
							if activeWorkflows == 0 {
								log.Println("Active workflows is now zero. Shutting down...")
								close(done) // Close the 'done' channel when activeWorkflows reaches zero
							}
							mutex.Unlock()
						}(portToUse)
					}
				}()

				<-done // Wait for runtime duration to end
			}

		} else {
			// Run concurrent workflows without runtime duration
			runWorkflows(concurrent, config)
		}
	}
}
