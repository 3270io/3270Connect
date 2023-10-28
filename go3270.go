package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"

	go3270 "gitlab.jnnn.gs/jnnngs/go3270/x3270"

	"github.com/gin-gonic/gin"
)

// Configuration struct to hold the configuration settings
type Configuration struct {
	Host         string
	Port         int
	HTMLFilePath string `json:"HTMLFilePath"`
	Steps        []Step
}

type Step struct {
	Type        string
	Coordinates go3270.Coordinates // Use go3270 package's Coordinates type
	Text        string
}

var (
	configFile string
	showHelp   bool
	runAPI     bool
	apiPort    int
	concurrent int
	headless   bool // Flag to run go3270 in headless mode
	verbose    bool
	done       = make(chan bool)
	wg         sync.WaitGroup
)

func init() {
	flag.StringVar(&configFile, "config", "workflow.json", "Path to the configuration file")
	flag.BoolVar(&showHelp, "help", false, "Show usage information")
	flag.BoolVar(&runAPI, "api", false, "Run as API")
	flag.IntVar(&apiPort, "api-port", 8080, "API port")
	flag.IntVar(&concurrent, "concurrent", 1, "Number of concurrent workflows")
	flag.BoolVar(&headless, "headless", false, "Run go3270 in headless mode")
	flag.BoolVar(&verbose, "verbose", false, "Run go3270 in verbose mode")
}

func loadConfiguration(filePath string) *Configuration {
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

func runWorkflows(numOfWorkflows int, config *Configuration, concurrent bool) {
	for i := 1; i <= numOfWorkflows; i++ {
		if concurrent {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				runWorkflow(i, config)
			}(i)
		} else {
			runWorkflow(i, config)
		}
	}

	if concurrent {
		wg.Wait()
	}
}

func runWorkflow(scriptPort int, config *Configuration) {
	// Create an emulator instance
	e := go3270.Emulator{
		Host:       config.Host,
		Port:       config.Port,
		ScriptPort: strconv.Itoa(scriptPort), // Convert int to string
	}

	// Initialize the HTML file with run details (call this at the beginning)
	htmlFilePath := config.HTMLFilePath
	if err := e.InitializeHTMLFile(htmlFilePath); err != nil {
		log.Fatalf("Error initializing HTML file: %v\n", err)
	}

	// Iterate through the steps in the configuration
	for _, step := range config.Steps {
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
			if go3270.Verbose {
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
			if err := e.Press(go3270.Enter); err != nil {
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
}

func runAPIWorkflow() {
	r := gin.Default()

	r.POST("/api/execute", func(c *gin.Context) {
		var workflowConfig Configuration
		if err := c.ShouldBindJSON(&workflowConfig); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Create an emulator instance
		e := go3270.Emulator{
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
				if go3270.Verbose {
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
				if err := e.Press(go3270.Enter); err != nil {
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

func main() {
	flag.Parse()

	if showHelp {
		flag.Usage()
		return
	}

	// Set the headless flag in the go3270 package based on the global flag
	go3270.Headless = headless

	// Set the verbose mode in your package
	go3270.Verbose = verbose

	// Log whether headless mode is enabled
	if go3270.Verbose {
		if go3270.Headless {
			log.Println("Running in headless mode")
		} else {
			log.Println("Running in interactive mode")
		}
	}

	if runAPI {
		runAPIWorkflow()
	} else {
		if concurrent > 1 {
			for i := 1; i <= concurrent; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					runWorkflow(5000+i, loadConfiguration(configFile))
				}(i)
			}
			wg.Wait()
		} else {
			runWorkflow(5000, loadConfiguration(configFile))
		}
	}
}
