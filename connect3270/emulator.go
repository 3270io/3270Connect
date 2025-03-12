package connect3270

import (
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/3270io/3270Connect/binaries"
)

var (
	// Headless controls whether go3270 runs in headless mode.
	// Set this variable to true to enable headless mode.
	Headless          bool
	Verbose           bool
	x3270BinaryPath   string
	s3270BinaryPath   string
	x3270ifBinaryPath string
	binaryFileMutex   sync.Mutex
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
	F13   = "PF(13)"
	F14   = "PF(14)"
	F15   = "PF(15)"
	F16   = "PF(16)"
	F17   = "PF(17)"
	F18   = "PF(18)"
	F19   = "PF(19)"
	F20   = "PF(20)"
	F21   = "PF(21)"
	F22   = "PF(22)"
	F23   = "PF(23)"
	F24   = "PF(24)"
)

const (
	maxRetries = 10          // Maximum number of retries
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

	// If coordinates are provided, move the cursor
	if x > 0 && y > 0 {
		if err := e.moveCursor(x, y); err != nil {
			return fmt.Errorf("error moving cursor: %v", err)
		}
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
	case F13, F14, F15, F16, F17, F18, F19, F20, F21, F22, F23, F24:
		return true
	default:
		return false
	}
}

// IsConnected check if a connection with host exist
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

// CursorPosition return actual position by cursor
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
			log.Println("ScriptPort not set, using default 5000")
			e.ScriptPort = "5000"
		}

		if Verbose {
			log.Println("func Connect: using -scriptport: " + e.ScriptPort)
		}

		var err error
		for attempt := 0; attempt < maxRetries; attempt++ {
			err = e.createApp()
			if err == nil {
				break
			}
			log.Printf("createApp failed (attempt %d/%d): %v", attempt+1, maxRetries, err)
			time.Sleep(retryDelay)
		}
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

// query returns state information from x3270
func (e *Emulator) query(keyword string) (string, error) {
	command := fmt.Sprintf("query(%s)", keyword)
	return e.execCommandOutput(command)
}

// createApp creates a connection to the host using embedded x3270 or s3270
func (e *Emulator) createApp() error {
	if Verbose {
		log.Println("func createApp: using -scriptport: " + e.ScriptPort)
	}

	binaryFilePath, err := e.prepareBinaryFilePath()
	if err != nil {
		log.Printf("Error preparing binary file path: %v", err)
		return err
	}
	if Verbose {
		log.Printf("createApp binaryFilePath: %s", binaryFilePath)
	}

	// Choose the correct model type
	modelType := "3279-2" // Adjust this based on your application's requirements

	var cmd *exec.Cmd
	var resourceString string

	// Conditional resource string based on OS
	if runtime.GOOS == "windows" {
		resourceString = "wc3270.unlockDelay: False"
	} else {
		resourceString = "x3270.unlockDelay: False"
	}

	if Headless {
		cmd = exec.Command(binaryFilePath, "-scriptport", e.ScriptPort, "-xrm", resourceString, "-model", modelType, e.hostname())
	} else {
		cmd = exec.Command(binaryFilePath, "-xrm", resourceString, "-scriptport", e.ScriptPort, "-model", modelType, e.hostname())
	}

	if Verbose {
		log.Printf("Executing command: %s %v", cmd.Path, cmd.Args)
	}

	// Capture stderr
	stderr, err := cmd.StderrPipe()
	if err != nil {
		log.Printf("Failed to get stderr pipe: %v", err)
		return err
	}

	if err := cmd.Start(); err != nil {
		log.Printf("Error starting 3270 instance: %v", err)
		return err
	}

	go func() {
		for retries := 0; retries < maxRetries; retries++ {
			errMsg, _ := ioutil.ReadAll(stderr)
			if Verbose && len(errMsg) > 0 {
				log.Printf("3270 stderr: %s", string(errMsg))
			}
			if err := cmd.Wait(); err == nil {
				if Verbose {
					log.Printf("Successfully started 3270 instance")
				}
				return // Successful execution, exit the Goroutine
			}
			log.Printf("Error creating 3270 instance (Retry %d): %v", retries+1, err)
			time.Sleep(retryDelay)
		}
		log.Printf("Max retries reached. Could not create an instance of 3270.")
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

// hostname return hostname formatted
func (e *Emulator) hostname() string {
	return fmt.Sprintf("%s:%d", e.Host, e.Port)
}

// execCommand executes a command on the connected x3270 or s3270 instance based on Headless flag
func (e *Emulator) execCommand(command string) (string, error) {
	if Verbose {
		log.Printf("Executing command: %s", command)
	}

	x3270ifBinaryPath, err := e.getX3270ifPath()
	if err != nil {
		return "", err
	}

	if Verbose {
		log.Printf("func execCommand: CMD: %s -t %s %s\n", x3270ifBinaryPath, e.ScriptPort, command)
	}

	// Retry logic for executing the command
	for retries := 0; retries < maxRetries; retries++ {
		cmd := exec.Command(x3270ifBinaryPath, "-S", "-t", e.ScriptPort, command)
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

	x3270ifBinaryPath, err := e.getX3270ifPath()
	if err != nil {
		return "", err
	}

	if Verbose {
		log.Printf("func execCommandOutput: CMD: %s -t %s %s\n", x3270ifBinaryPath, e.ScriptPort, command)
	}

	// Execute the command using the selected binary file
	cmd := exec.Command(x3270ifBinaryPath, "-t", e.ScriptPort, command)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	return string(output), nil
}

// InitializeOutput initializes the output file with run details
func (e *Emulator) InitializeOutput(filePath string, runAPI bool) error {
	if Verbose {
		log.Printf("Initializing Output file at path: %s", filePath)
	}
	// Get the current date and time
	currentTime := time.Now().Format("2006-01-02 15:04:05")

	// Create the output content with run details
	outputContent := ""
	if !runAPI {
		outputContent += fmt.Sprintf("<html><head><title>ASCII Screen Capture</title></head><body>")
		outputContent += fmt.Sprintf("<h1>ASCII Screen Capture</h1>")
		outputContent += fmt.Sprintf("<p>Run Date and Time: %s</p>", currentTime)
	}

	// Open or create the output file for overwriting if in API mode
	// and for appending if not in API mode
	var file *os.File
	var err error
	if runAPI {
		file, err = os.Create(filePath) // Clears the file in API mode
	} else {
		file, err = os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644) // Appends in non-API mode
	}
	if err != nil {
		return fmt.Errorf("error opening or creating file: %v", err)
	}
	defer file.Close()

	// Write the output content to the file
	if _, err := file.WriteString(outputContent); err != nil {
		return fmt.Errorf("error writing to file: %v", err)
	}

	return nil
}

// AsciiScreenGrab captures an ASCII screen and saves it to a file.
// If apiMode is true, it saves plain ASCII text. Otherwise, it formats the output as output.
func (e *Emulator) AsciiScreenGrab(filePath string, apiMode bool) error {
	if Verbose {
		log.Printf("Capturing ASCII screen and saving to file: %s", filePath)
	}

	// Retry logic for capturing ASCII screen
	for retries := 0; retries < maxRetries; retries++ {
		output, err := e.execCommandOutput("Ascii()")
		if err == nil {
			var content string
			if apiMode {
				// In API mode, just use plain ASCII output
				content = output
			} else {
				// In non-API mode, format the output as output
				content = fmt.Sprintf("<pre>%s</pre>\n", output)
				content += "</body></html>"
			}

			// Open or create the file for appending or overwriting
			file, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err != nil {
				log.Printf("Error opening or creating file: %v", err)
				return err
			}

			// Write the content to the file
			if _, err := file.WriteString(content); err != nil {
				log.Printf("Error writing to file: %v", err)
				file.Close() // Ensure the file is closed in case of an error
				return err
			}

			file.Close() // Ensure the file is properly closed
			return nil
		}
		time.Sleep(retryDelay)
	}

	return fmt.Errorf("maximum capture retries reached")
}

// ReadOutputFile reads the contents of the specified HTML file and returns it as a string.
func (e *Emulator) ReadOutputFile(tempFilePath string) (string, error) {
	file, err := os.Open(tempFilePath)
	if err != nil {
		return "", fmt.Errorf("error opening temporary file: %v", err)
	}
	defer file.Close()

	content, err := ioutil.ReadAll(file)
	if err != nil {
		return "", fmt.Errorf("error reading temporary file: %v", err)
	}

	return string(content), nil
}

// getOrCreateBinaryFile checks if a binary file exists for the given binary name, and creates it if it doesn't
func getOrCreateBinaryFile(binaryName string) (string, error) {
	var filePath string
	switch binaryName {
	case "x3270", "s3270", "wc3270":
		filePath = filepath.Join(os.TempDir(), binaryName+getExecutableExtension())
	case "x3270if":
		filePath = filepath.Join(os.TempDir(), binaryName+getExecutableExtension())
	default:
		return "", fmt.Errorf("unknown binary name: %s", binaryName)
	}

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		// File does not exist, create it
		assetPath := filepath.Join("binaries", getOSDirectory(), binaryName+getExecutableExtension())
		binaryData, err := binaries.Asset(assetPath)
		if err != nil {
			return "", fmt.Errorf("error reading embedded binary data: %v", err)
		}

		if err := ioutil.WriteFile(filePath, binaryData, 0755); err != nil {
			return "", fmt.Errorf("error writing binary data to a file: %v", err)
		}
	}

	return filePath, nil
}

// getOSDirectory returns the appropriate directory name based on the OS
func getOSDirectory() string {
	switch runtime.GOOS {
	case "windows":
		return "windows"
	default:
		return "linux"
	}
}

// getExecutableExtension returns the appropriate file extension for executables based on the OS
func getExecutableExtension() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// prepareBinaryFilePath prepares and returns the path for the appropriate binary file based on the Headless flag.
func (e *Emulator) prepareBinaryFilePath() (string, error) {
	binaryFileMutex.Lock()
	defer binaryFileMutex.Unlock()

	var binaryName string
	var binaryFilePath *string
	if Headless {
		binaryName = "s3270"
		binaryFilePath = &s3270BinaryPath
	} else {
		if runtime.GOOS == "windows" {
			binaryName = "wc3270" // Assuming wc3270 combines functionalities on Windows
		} else {
			binaryName = "x3270"
		}
		binaryFilePath = &x3270BinaryPath
	}

	if *binaryFilePath == "" {
		var err error
		*binaryFilePath, err = getOrCreateBinaryFile(binaryName)
		if err != nil {
			if Verbose {
				log.Printf("Error in getOrCreateBinaryFile: %v", err)
			}
			return "", err
		}
	}

	return *binaryFilePath, nil
}

// getX3270ifPath retrieves the path for the x3270if binary.
func (e *Emulator) getX3270ifPath() (string, error) {
	binaryFileMutex.Lock()
	defer binaryFileMutex.Unlock()

	if x3270ifBinaryPath == "" {
		var err error
		x3270ifBinaryPath, err = getOrCreateBinaryFile("x3270if")
		if err != nil {
			return "", err
		}
	}

	return x3270ifBinaryPath, nil
}
