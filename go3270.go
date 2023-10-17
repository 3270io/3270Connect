package main

import (
	"log"
	"strings"

	go3270 "gitlab.jnnn.gs/jnnngs/go3270/x3270"
)

// Global variable for the HTML file path
var filePath = "output.html"

func main() {
	// Create an emulator instance
	e := go3270.Emulator{
		Host: "10.27.27.62",
		Port: 30050,
	}

	// Initialize the HTML file with run details (call this at the beginning)
	htmlFilePath := "output.html"
	if err := e.InitializeHTMLFile(htmlFilePath); err != nil {
		log.Fatalf("Error initializing HTML file: %v\n", err)
	}

	// Connect to the terminal
	if err := e.Connect(); err != nil {
		log.Fatalf("Error connecting to terminal: %v\n", err)
	}

	// Check for successful login
	v, err := e.GetValue(1, 2, 12) // Adjust the length as needed
	if err != nil {
		log.Fatalf("Error getting value: %v", err)
	}
	v = strings.TrimSpace(v)             // Remove leading and trailing whitespaces
	log.Println("Retrieved value: " + v) // Add this line for debugging
	if v != "Scrn: BANK10" {
		log.Println("Login failed. String found: --->" + v + "<---")
		if err := e.Disconnect(); err != nil {
			log.Fatalf("Error disconnecting: %v", err)
		}
	} else {
		log.Println("Successfully connected to BANK10")
	}

	// Move cursor to position and fill in the username
	//if err := e.MoveCursor(9, 44); err != nil {
	//	log.Fatalf("Error moving cursor: %v\n", err)
	//}
	if err := e.FillString(10, 44, "b0001"); err != nil {
		log.Fatalf("Error setting username: %v\n", err)
	}

	// Move cursor to position and fill in the password
	//if err := e.MoveCursor(11, 44); err != nil {
	//	log.Fatalf("Error moving cursor: %v\n", err)
	//}
	if err := e.FillString(11, 44, "mypass"); err != nil {
		log.Fatalf("Error setting password: %v\n", err)
	}

	// Capture and append the ASCII screen to the HTML file
	if err := e.AsciiScreenGrab(htmlFilePath, true); err != nil {
		log.Fatalf("Error capturing and appending ASCII screen: %v", err)
	}

	// Send the Enter key
	if err := e.Press(go3270.Enter); err != nil {
		log.Fatalf("Error pressing Enter: %v\n", err)
	}

	// Capture and append the ASCII screen to the HTML file
	if err := e.AsciiScreenGrab(htmlFilePath, true); err != nil {
		log.Fatalf("Error capturing and appending ASCII screen: %v", err)
	}

	// Disconnect from the terminal
	if err := e.Disconnect(); err != nil {
		log.Fatalf("Error disconnecting: %v\n", err)
	}

}
