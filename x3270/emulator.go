package go3270

import (
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"gitlab.jnnn.gs/jnnngs/go3270/binaries"
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

//Emulator base struct to x3270 terminal emulator
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
func NewEmulator(host string, port int, scriptPort string) *Emulator {
	return &Emulator{
		Host:       host,
		Port:       port,
		ScriptPort: scriptPort,
	}
}

//moveCursor move cursor to especific row(x) and column(y)
func (e *Emulator) moveCursor(x, y int) error {
	// Adjust the values to start at 0 internally
	xAdjusted := x - 1
	yAdjusted := y - 1
	command := fmt.Sprintf("MoveCursor(%d,%d)", xAdjusted, yAdjusted)
	return e.execCommand(command)
}

//SetString fill field with value passed by parameter
//setString will fill the field that the cursor is marked
func (e *Emulator) SetString(value string) error {
	command := fmt.Sprintf("String(%s)", value)
	return e.execCommand(command)
}

//GetRows returns the number of rows in the saved screen image.
func (e *Emulator) GetRows() (int, error) {
	s, err := e.execCommandOutput("Snap(Rows)")
	if err != nil {
		return 0, err
	}
	i, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("error from x3270 to get numbers of row: %v", err)
	}
	return i, nil
}

//GetColumns returns the number of columns in the saved screen image.
func (e *Emulator) GetColumns() (int, error) {
	s, err := e.execCommandOutput("Snap(Cols)")
	if err != nil {
		return 0, err
	}
	i, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("error from x3270 to get numbers of columns: %v", err)
	}
	return i, nil
}

// FillString fills the field at the specified row (x) and column (y) with the given value
func (e *Emulator) FillString(x, y int, value string) error {
	// Adjust the row and column values to start at 1 internally
	if err := e.moveCursor(x, y); err != nil {
		return fmt.Errorf("error to move cursor: %v", err)
	}
	return e.SetString(value)
}

//Press press a keyboard key
func (e *Emulator) Press(key string) error {
	if !e.validaKeyboard(key) {
		return fmt.Errorf("invalid key %s", key)
	}
	return e.execCommand(key)
}

//validaKeyboard valid if key passed by parameter if a key valid
func (e *Emulator) validaKeyboard(key string) bool {
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
	s, err := e.query("ConnectionState")
	if err != nil || len(strings.TrimSpace(s)) == 0 {
		return false
	}
	return true
}

// GetValue returns content of a specified length at the specified row (x) and column (y)
func (e *Emulator) GetValue(x, y, length int) (string, error) {
	// Adjust the row and column values to start at 1 internally
	xAdjusted := x - 1
	yAdjusted := y - 1
	command := fmt.Sprintf("Ascii(%d,%d,%d)", xAdjusted, yAdjusted, length)
	return e.execCommandOutput(command)
}

//CursorPosition return actual position by cursor
func (e *Emulator) CursorPosition() (string, error) {
	return e.query("cursor")
}

// Connect opens a connection with x3270 or s3270 and the specified host and port
func (e *Emulator) Connect() error {
	if e.Host == "" {
		return errors.New("Host needs to be filled")
	}

	if e.IsConnected() {
		return errors.New("Address already in use")
	}

	if e.ScriptPort == "" {
		e.ScriptPort = "5000"
	}

	if Verbose {
		log.Println("func Connect: using -scriptport: " + e.ScriptPort)
	}

	e.createApp()

	if !e.IsConnected() {
		return fmt.Errorf("Failed to connect to %s", e.hostname())
	}

	return nil
}

//Disconnect close connection with x3270
func (e *Emulator) Disconnect() error {
	if e.IsConnected() {
		return e.execCommand("quit")
	}
	return nil
}

//query returns state information from x3270
func (e *Emulator) query(keyword string) (string, error) {
	command := fmt.Sprintf("query(%s)", keyword)
	return e.execCommandOutput(command)
}

// createApp creates a connection to the host using embedded x3270 or s3270
func (e *Emulator) createApp() {
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
		log.Fatalf("Error reading embedded binary data: %v", err)
	}

	// Write the binary data to a temporary file
	binaryFilePath := "/tmp/" + binaryName
	err = ioutil.WriteFile(binaryFilePath, binaryData, 0755)
	if err != nil {
		log.Fatalf("Error writing binary data to a temporary file: %v", err)
	}

	if Headless {
		cmd = exec.Command(binaryFilePath, "-scriptport", e.ScriptPort, "-xrm", "x3270.unlockDelay: False", e.hostname())
	} else {
		// Set the unlockDelay option for x3270 when running in headless mode
		cmd = exec.Command(binaryFilePath, "-xrm", "x3270.unlockDelay: False", "-scriptport", e.ScriptPort, e.hostname())
	}

	go func() {
		if err := cmd.Run(); err != nil {
			log.Fatalf("Error creating an instance of 3270: %v\n", err)
		}
	}()
	const maxAttempts = 10
	const sleepDuration = time.Second

	for i := 0; i < maxAttempts; i++ {
		if e.IsConnected() {
			break
		}
		time.Sleep(sleepDuration)
	}

	if !e.IsConnected() {
		log.Fatalf("Failed to connect to %s\n", e.hostname())
	}

	// Clean up the temporary binary file
	defer os.Remove(binaryFilePath)
}

//hostname return hostname formatted
func (e *Emulator) hostname() string {
	return fmt.Sprintf("%s:%d", e.Host, e.Port)
}

// execCommand executes a command on the connected x3270 or s3270 instance based on Headless flag
func (e *Emulator) execCommand(command string) error {
	// Set terminalCommand based on Headless flag
	if Headless {
		terminalCommand = "x3270if"
	} else {
		terminalCommand = "x3270if"
	}

	// Determine which binary to use based on Headless flag
	binaryName := "x3270if"
	if Headless {
		binaryName = "x3270if"
	}

	// Read the embedded binary data from bindata.go
	binaryData, err := binaries.Asset(binaryName)
	if err != nil {
		return fmt.Errorf("error reading embedded binary data: %v", err)
	}

	// Write the binary data to a temporary file
	binaryFilePath := "/tmp/" + binaryName
	err = ioutil.WriteFile(binaryFilePath, binaryData, 0755)
	if err != nil {
		return fmt.Errorf("error writing binary data to a temporary file: %v", err)
	}

	if Verbose {
		log.Printf("func execCommand: CMD: %s -t %s %s\n", binaryFilePath, e.ScriptPort, command)
	}

	cmd := exec.Command(binaryFilePath, "-t", e.ScriptPort, command)
	if err := cmd.Run(); err != nil {
		return err
	}
	time.Sleep(1 * time.Second)
	defer os.Remove(binaryFilePath) // Clean up the temporary binary file when done
	return nil
}

// execCommandOutput executes a command on the connected x3270 or s3270 instance based on Headless flag and returns output
func (e *Emulator) execCommandOutput(command string) (string, error) {

	// Determine which binary to use based on Headless flag
	binaryName := "x3270if"
	if Headless {
		binaryName = "x3270if"
	}

	// Read the embedded binary data from bindata.go
	binaryData, err := binaries.Asset(binaryName)
	if err != nil {
		return "", fmt.Errorf("error reading embedded binary data: %v", err)
	}

	// Write the binary data to a temporary file
	binaryFilePath := "/tmp/" + binaryName
	err = ioutil.WriteFile(binaryFilePath, binaryData, 0755)
	if err != nil {
		return "", fmt.Errorf("error writing binary data to a temporary file: %v", err)
	}

	if Verbose {
		log.Printf("func execCommand: CMD: %s -t %s %s\n", binaryFilePath, e.ScriptPort, command)
	}

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

// AsciiScreenGrab captures an ASCII screen and saves it to an HTML file with run details
func (e *Emulator) AsciiScreenGrab(filePath string, append bool) error {
	output, err := e.execCommandOutput("Ascii()") // Capture the entire screen
	if err != nil {
		return fmt.Errorf("error capturing ASCII screen: %v", err)
	}

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
	if append {
		file, err = os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	} else {
		file, err = os.Create(filePath)
	}
	if err != nil {
		return fmt.Errorf("error opening or creating file: %v", err)
	}
	defer file.Close()

	// Write the HTML content to the file
	if _, err := file.WriteString(htmlContent); err != nil {
		return fmt.Errorf("error writing to file: %v", err)
	}

	return nil
}
