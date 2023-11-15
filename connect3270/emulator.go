package connect3270

import (
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/3270io/3270Connect/binaries"
)

var (
	// Headless controls whether go3270 runs in headless mode.
	// Set this variable to true to enable headless mode.
	Headless        bool
	terminalCommand string // Stores the terminal command to use
	Verbose         bool
)

// These constants represent the keyboard keys
const (
	Enter = "Enter"
	Tab   = "Tab"
	F1    = "PF(1)"
	F2    = "PF(2)"
	F3    = "PF(3)"
	F4    = "PF(4)"
	F5    = "PF(5)"
	F6    = "PF(6)"
	F7    = "PF(7)"
	F8    = "PF(8)"
	F9    = "PF(9)"
	F10   = "PF(10)"
	F11   = "PF(11)"
	F12   = "PF(12)"
)

const (
	maxRetries = 3           // Maximum number of retries
	retryDelay = time.Second // Delay between retries (e.g., 1 second)
)

// Emulator base struct to x3270 terminal emulator
type Emulator struct {
	Host       string
	Port       int
	ScriptPort string
}

// Coordinates represents the screen coordinates (row and column)
type Coordinates struct {
	Row    int
	Column int
	Length int
}

// NewEmulator creates a new Emulator instance.
// It initializes an Emulator with the given host, port, and scriptPort.
func NewEmulator(host string, port int, scriptPort string) *Emulator {
	return &Emulator{
		Host:       host,
		Port:       port,
		ScriptPort: scriptPort,
	}
}

// WaitForField waits until the screen is ready, the cursor has been positioned
// on a modifiable field, and the keyboard is unlocked.
func (e *Emulator) WaitForField(timeout time.Duration) error {
	// Send the command to wait for a field with the specified timeout
	command := fmt.Sprintf("Wait(%d, InputField)", int(timeout.Seconds()))

	// Retry the MoveCursor operation with a delay in case of failure
	for retries := 0; retries < maxRetries; retries++ {
		output, err := e.execCommand(command)
		if err == nil {
			if output == "" {
				fmt.Printf("Wait command executed successfully (no output)\n")
				return nil
			}

			// Extract the keyboard status from the command output
			statusParts := strings.Fields(output)
			if len(statusParts) > 0 && statusParts[0] != "U" {
				return fmt.Errorf("keyboard not unlocked, state was: %s", statusParts[0])
			}
			//fmt.Printf("Wait command executed successfully %s", statusParts[0])
			//fmt.Printf("Wait command executed successfully\n")
			return nil // Successful operation, exit the retry loop
		}

		time.Sleep(retryDelay)
	}

	return fmt.Errorf("maximum WaitForField retries reached")
}

// moveCursor moves the cursor to the specified row (x) and column (y) with retry logic.
func (e *Emulator) moveCursor(x, y int) error {
	// Retry logic parameters
	maxRetries := 3
	retryDelay := 1 * time.Second

	// Adjust the values to start at 0 internally
	xAdjusted := x - 1
	yAdjusted := y - 1
	command := fmt.Sprintf("MoveCursor(%d,%d)", xAdjusted, yAdjusted)

	// Retry the MoveCursor operation with a delay in case of failure
	for retries := 0; retries < maxRetries; retries++ {
		if _, err := e.execCommand(command); err == nil {
			return nil // Successful operation, exit the retry loop
		}
		//log.Printf("Error moving cursor (Retry %d) to row %d, column %d\n", retries+1, x, y)

		time.Sleep(retryDelay)
	}

	return fmt.Errorf("maximum MoveCursor retries reached")
}

// SetString fills the field at the current cursor position with the given value and retries in case of failure.
func (e *Emulator) SetString(value string) error {
	// Retry logic parameters
	maxRetries := 3
	retryDelay := 1 * time.Second

	command := fmt.Sprintf("String(%s)", value)

	// Retry the SetString operation with a delay in case of failure
	for retries := 0; retries < maxRetries; retries++ {
		if _, err := e.execCommand(command); err == nil {
			return nil // Successful operation, exit the retry loop
		}
		//log.Printf("Error executing String command (Retry %d)\n", retries+1)
		time.Sleep(retryDelay)
	}

	return fmt.Errorf("maximum SetString retries reached")
}

// GetRows returns the number of rows in the saved screen image with retry logic.
func (e *Emulator) GetRows() (int, error) {
	// Retry logic parameters
	maxRetries := 3
	retryDelay := 1 * time.Second

	// Retry the Snap(Rows) operation with a delay in case of failure
	for retries := 0; retries < maxRetries; retries++ {
		s, err := e.execCommandOutput("Snap(Rows)")
		if err == nil {
			i, err := strconv.Atoi(s)
			if err == nil {
				return i, nil // Successful operation, exit the retry loop
			}
		}
		//log.Printf("Error getting number of rows (Retry %d): %v\n", retries+1, err)
		time.Sleep(retryDelay)
	}

	return 0, fmt.Errorf("maximum GetRows retries reached")
}

// GetColumns returns the number of columns in the saved screen image with retry logic.
func (e *Emulator) GetColumns() (int, error) {
	// Retry logic parameters
	maxRetries := 3
	retryDelay := 1 * time.Second

	// Retry the Snap(Cols) operation with a delay in case of failure
	for retries := 0; retries < maxRetries; retries++ {
		s, err := e.execCommandOutput("Snap(Cols)")
		if err == nil {
			i, err := strconv.Atoi(s)
			if err == nil {
				return i, nil // Successful operation, exit the retry loop
			}
		}
		//log.Printf("Error getting number of columns (Retry %d): %v\n", retries+1, err)
		time.Sleep(retryDelay)
	}

	return 0, fmt.Errorf("maximum GetColumns retries reached")
}

// FillString fills the field at the specified row (x) and column (y) with the given value
func (e *Emulator) FillString(x, y int, value string) error {
	// Retry logic parameters
	maxRetries := 3
	retryDelay := 1 * time.Second

	// Adjust the row and column values to start at 1 internally
	if err := e.moveCursor(x, y); err != nil {
		return fmt.Errorf("error moving cursor: %v", err)
	}

	// Retry the SetString operation with a delay in case of failure
	for retries := 0; retries < maxRetries; retries++ {
		err := e.SetString(value) // Declare and define err here
		if err == nil {
			return nil // Successful operation, exit the retry loop
		}
		//log.Printf("Error filling string (Retry %d) at row %d, column %d: %v\n", retries+1, x, y, err)
		time.Sleep(retryDelay)
	}

	return fmt.Errorf("maximum FillString retries reached")
}

// Press press a keyboard key
func (e *Emulator) Press(key string) error {
	if !e.validateKeyboard(key) {
		return fmt.Errorf("invalid key %s", key)
	}

	_, err := e.execCommand(key)
	if err != nil {
		return err
	}

	return nil
}

// validateKeyboard valid if key passed by parameter if a key valid
func (e *Emulator) validateKeyboard(key string) bool {
	switch key {
	case Tab:
		return true
	case Enter:
		return true
	default:
		return false
	}
}

//IsConnected check if a connection with host exist
func (e *Emulator) IsConnected() bool {

	time.Sleep(1 * time.Second) // Optional: Add a delay between steps
	s, err := e.query("ConnectionState")
	if err != nil || len(strings.TrimSpace(s)) == 0 {
		return false
	}
	return true
}

// GetValue returns content of a specified length at the specified row (x) and column (y) with retry logic.
func (e *Emulator) GetValue(x, y, length int) (string, error) {
	// Retry logic parameters
	maxRetries := 3
	retryDelay := 1 * time.Second

	// Adjust the row and column values to start at 1 internally
	xAdjusted := x - 1
	yAdjusted := y - 1
	command := fmt.Sprintf("Ascii(%d,%d,%d)", xAdjusted, yAdjusted, length)

	// Retry the Ascii command with a delay in case of failure
	for retries := 0; retries < maxRetries; retries++ {
		output, err := e.execCommandOutput(command)
		if err == nil {
			return output, nil // Successful operation, exit the retry loop
		}
		//log.Printf("Error executing Ascii command (Retry %d): %v\n", retries+1, err)
		time.Sleep(retryDelay)
	}

	return "", fmt.Errorf("maximum GetValue retries reached")
}

//CursorPosition return actual position by cursor
func (e *Emulator) CursorPosition() (string, error) {
	return e.query("cursor")
}

// Connect opens a connection with x3270 or s3270 and the specified host and port.
func (e *Emulator) Connect() error {
	if Verbose {
		log.Printf("Attempting to connect to host: %s", e.Host)
	}
	if e.Host == "" {
		return errors.New("Host needs to be filled")
	}

	// Retry logic for connecting
	for retries := 0; retries < maxRetries; retries++ {
		if e.IsConnected() {
			return nil // Successfully connected, exit the retry loop
		}

		if e.ScriptPort == "" {
			e.ScriptPort = "5000"
		}

		if Verbose {
			log.Println("func Connect: using -scriptport: " + e.ScriptPort)
		}

		err := e.createApp()
		if err != nil {
			log.Printf("Failed to create app: %v", err)
			defer e.Disconnect()
			return fmt.Errorf("failed to create client to connect: %v", err) // Return the error immediately
		}

		if !e.IsConnected() {
			//log.Printf("Failed to connect to %s (Retry %d)...", e.hostname(), retries+1)
		} else {
			return nil // Successfully connected, exit the retry loop
		}

		time.Sleep(retryDelay)
	}

	return fmt.Errorf("maximum connect retries reached")
}

// Disconnect closes the connection with x3270.
func (e *Emulator) Disconnect() error {
	if Verbose {
		log.Println("Disconnecting from x3270")
	}

	if e.IsConnected() {
		if _, err := e.execCommand("quit"); err != nil {
			return fmt.Errorf("error executing quit command: %v", err)
		}

	}

	return nil
}

//query returns state information from x3270
func (e *Emulator) query(keyword string) (string, error) {
	command := fmt.Sprintf("query(%s)", keyword)
	return e.execCommandOutput(command)
}

// createApp creates a connection to the host using embedded x3270 or s3270
func (e *Emulator) createApp() error {
	var cmd *exec.Cmd

	if Verbose {
		log.Println("func createApp: using -scriptport: " + e.ScriptPort)
	}

	// Determine which binary to use based on Headless flag
	binaryName := "x3270"
	if Headless {
		binaryName = "s3270"
	}

	// Read the embedded binary data from bindata.go
	binaryData, err := binaries.Asset(binaryName)
	if err != nil {
		return fmt.Errorf("error reading embedded binary data: %v", err)
	}

	// Create a temporary directory to store the binary file
	tempDir, err := ioutil.TempDir("", "x3270_binary")
	if err != nil {
		return fmt.Errorf("error creating temporary directory: %v", err)
	}
	defer os.RemoveAll(tempDir) // Clean up the temporary directory when done

	// Create the binary file in the temporary directory
	binaryFilePath := filepath.Join(tempDir, binaryName)
	err = ioutil.WriteFile(binaryFilePath, binaryData, 0755)
	if err != nil {
		return fmt.Errorf("error writing binary data to a temporary file: %v", err)
	}

	if Headless {
		cmd = exec.Command(binaryFilePath, "-scriptport", e.ScriptPort, "-xrm", "x3270.unlockDelay: False", e.hostname())
	} else {
		// Set the unlockDelay option for x3270 when running in headless mode
		cmd = exec.Command(binaryFilePath, "-xrm", "x3270.unlockDelay: False", "-scriptport", e.ScriptPort, e.hostname())
	}

	// Retry logic parameters
	maxRetries := 1
	retryDelay := 1 * time.Second

	// Use Goroutines for potential concurrent operations
	go func() {
		for retries := 0; retries < maxRetries; retries++ {
			if err := cmd.Run(); err == nil {
				return // Successful execution, exit the Goroutine
			}
			//log.Printf("Error creating an instance of 3270 (Retry %d): %v\n", retries+1, err)
			time.Sleep(retryDelay)
		}
		log.Printf("Max retries reached. Could not create an instance of 3270.\n")
	}()

	const maxAttempts = 1
	const sleepDuration = time.Second

	for i := 0; i < maxAttempts; i++ {
		if e.IsConnected() {
			break
		}
		time.Sleep(sleepDuration)
	}

	if !e.IsConnected() {
		return fmt.Errorf("Failed to connect to %s", e.hostname())
	}

	return nil
}

//hostname return hostname formatted
func (e *Emulator) hostname() string {
	return fmt.Sprintf("%s:%d", e.Host, e.Port)
}

// execCommand executes a command on the connected x3270 or s3270 instance based on Headless flag
func (e *Emulator) execCommand(command string) (string, error) {
	if Verbose {
		log.Printf("Executing command: %s", command)
	}

	// Determine the appropriate terminal command based on the Headless flag
	terminalCommand := "x3270if"

	// Determine which binary to use based on Headless flag
	binaryName := terminalCommand

	// Read the embedded binary data from bindata.go
	binaryData, err := binaries.Asset(binaryName)
	if err != nil {
		return "", fmt.Errorf("error reading embedded binary data: %v", err)
	}

	// Create a temporary directory to store the binary file
	tempDir, err := ioutil.TempDir("", "x3270_binary")
	if err != nil {
		return "", fmt.Errorf("error creating temporary directory: %v", err)
	}
	defer os.RemoveAll(tempDir) // Clean up the temporary directory when done

	// Generate a random unique name for the binary file
	binaryFileName := fmt.Sprintf("%s_%d", binaryName, time.Now().UnixNano())
	binaryFilePath := filepath.Join(tempDir, binaryFileName)

	// Write the binary data to the temporary file
	err = ioutil.WriteFile(binaryFilePath, binaryData, 0755)
	if err != nil {
		return "", fmt.Errorf("error writing binary data to a temporary file: %v", err)
	}

	if Verbose {
		log.Printf("func execCommand: CMD: %s -t %s %s\n", binaryFilePath, e.ScriptPort, command)
	}

	// Retry logic for executing the command
	for retries := 0; retries < maxRetries; retries++ {
		cmd := exec.Command(binaryFilePath, "-S", "-t", e.ScriptPort, command)
		if output, err := cmd.Output(); err == nil {
			return string(output), nil
		} else if strings.Contains(err.Error(), "text file busy") {
			//log.Printf("Error executing command (Retry %d): %v", retries+1, err)
			time.Sleep(retryDelay)
		} else {
			return "", err // Exit and return error if it's not "text file busy"
		}
	}

	return "", fmt.Errorf("maximum command execution retries reached")
}

// execCommandOutput executes a command on the connected x3270 or s3270 instance based on Headless flag and returns output
func (e *Emulator) execCommandOutput(command string) (string, error) {
	if Verbose {
		log.Printf("Executing command with output: %s", command)
	}

	// Determine which binary to use based on Headless flag
	binaryName := "x3270if"

	// Read the embedded binary data from bindata.go
	binaryData, err := binaries.Asset(binaryName)
	if err != nil {
		return "", fmt.Errorf("error reading embedded binary data: %v", err)
	}

	// Create a temporary directory to store the binary file
	tempDir, err := ioutil.TempDir("", "x3270_binary")
	if err != nil {
		return "", fmt.Errorf("error creating temporary directory: %v", err)
	}
	defer os.RemoveAll(tempDir) // Clean up the temporary directory when done

	// Generate a random unique name for the binary file
	binaryFileName := fmt.Sprintf("%s_%d", binaryName, time.Now().UnixNano())
	binaryFilePath := filepath.Join(tempDir, binaryFileName)

	// Write the binary data to the temporary file
	err = ioutil.WriteFile(binaryFilePath, binaryData, 0755)
	if err != nil {
		return "", fmt.Errorf("error writing binary data to a temporary file: %v", err)
	}

	if Verbose {
		log.Printf("func execCommand: CMD: %s -t %s %s\n", binaryFilePath, e.ScriptPort, command)
	}

	// Execute the command using the temporary binary file
	cmd := exec.Command(binaryFilePath, "-t", e.ScriptPort, command)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	defer os.Remove(binaryFilePath) // Clean up the temporary binary file when done

	return string(output), nil
}

var runDetailsAppended bool // Track if run details have been appended

// Initialize HTML file with run details
func (e *Emulator) InitializeHTMLFile(filePath string) error {
	if Verbose {
		log.Printf("Initializing HTML file at path: %s", filePath)
	}
	// Get the current date and time
	currentTime := time.Now().Format("2006-01-02 15:04:05")

	// Create the HTML content with run details
	htmlContent := fmt.Sprintf("<html><head><title>ASCII Screen Capture</title></head><body>")
	htmlContent += fmt.Sprintf("<h1>ASCII Screen Capture</h1>")
	htmlContent += fmt.Sprintf("<p>Run Date and Time: %s</p>", currentTime)

	// Open or create the HTML file for overwriting
	file, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("error opening or creating file: %v", err)
	}
	defer file.Close()

	// Write the HTML content to the file
	if _, err := file.WriteString(htmlContent); err != nil {
		return fmt.Errorf("error writing to file: %v", err)
	}

	runDetailsAppended = true // Mark run details as appended

	return nil
}

// AsciiScreenGrab captures an ASCII screen and saves it to an HTML file with run details,
// with added retry logic.
func (e *Emulator) AsciiScreenGrab(filePath string, append bool) error {
	if Verbose {
		log.Printf("Capturing ASCII screen and saving to file: %s", filePath)
	}

	// Retry logic for capturing ASCII screen
	for retries := 0; retries < maxRetries; retries++ {
		output, err := e.execCommandOutput("Ascii()") // Capture the entire screen
		if err == nil {
			// Successfully captured, exit the retry loop
			// Get the current date and time
			//currentTime := time.Now().Format("2006-01-02 15:04:05")

			// Create the HTML content with run details
			//htmlContent := fmt.Sprintf("<html><head><title>ASCII Screen Capture</title></head><body>")
			//htmlContent += fmt.Sprintf("<h1>ASCII Screen Capture</h1>")
			//htmlContent += fmt.Sprintf("<p>Run Date and Time: %s</p>", currentTime)
			htmlContent := fmt.Sprintf("<pre>%s</pre>\n", output)
			htmlContent += fmt.Sprintf("</body></html>")

			// Open or create the HTML file for appending or overwriting
			var file *os.File
			var err error
			if append {
				file, err = os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			} else {
				file, err = os.Create(filePath)
			}
			if err != nil {
				log.Printf("Error opening or creating file: %v", err)
				return err
			}
			defer file.Close()

			// Write the HTML content to the file
			if _, err := file.WriteString(htmlContent); err != nil {
				log.Printf("Error writing to file: %v", err)
				return err
			}
			return nil
		}
		//log.Printf("Error capturing ASCII screen (Retry %d): %v", retries+1, err)
		time.Sleep(retryDelay)
	}

	return fmt.Errorf("maximum capture retries reached")
}

// ReadHTMLFile reads the contents of the specified HTML file and returns it as a string.
func (e *Emulator) ReadHTMLFile(filePath string) (string, error) {
	// Check if the file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return "", fmt.Errorf("file does not exist: %s", filePath)
	}

	// Open the file for reading
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("error opening file: %v", err)
	}
	defer file.Close() // Ensure the file is closed after the function finishes

	// Read the contents of the file
	content, err := ioutil.ReadAll(file)
	if err != nil {
		return "", fmt.Errorf("error reading file: %v", err)
	}

	// Return the contents of the file as a string
	return string(content), nil
}
