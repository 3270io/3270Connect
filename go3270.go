package main

import (
	"encoding/json"
	"flag"
	"log"
	"os"
	"strings"

	go3270 "gitlab.jnnn.gs/jnnngs/go3270/x3270"
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
	showHelp   bool // Define a flag for help
)

func init() {
	flag.StringVar(&configFile, "config", "workflow.json", "Path to the configuration file")
	flag.BoolVar(&showHelp, "help", false, "Show usage information") // Add the help flag
}

func main() {
	// Parse command-line flags
	flag.Parse()

	// Display usage information and exit if -help is provided
	if showHelp {
		flag.Usage()
		return
	}

	// Read the configuration from the external JSON file
	configFile, err := os.Open(configFile)
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

	// Create an emulator instance
	e := go3270.Emulator{
		Host: config.Host,
		Port: config.Port,
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
			//log.Printf("step.Coordinates.Row value: %d", step.Coordinates.Row)
			//log.Printf("step.Coordinates.Column value: %d", step.Coordinates.Column)
			//log.Printf("step.Length value: %d", step.Coordinates.Length)

			v, err := e.GetValue(step.Coordinates.Row, step.Coordinates.Column, step.Coordinates.Length)
			if err != nil {
				log.Fatalf("Error getting value: %v", err)
			}
			v = strings.TrimSpace(v)
			log.Println("Retrieved value: " + v)
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
