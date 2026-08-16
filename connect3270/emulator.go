package connect3270

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pterm/pterm"

	"github.com/3270io/3270Connect/binaries"
)

var (
	// Headless controls whether go3270 runs in headless mode.
	// Set this variable to true to enable headless mode.
	Headless          bool
	Verbose           bool
	x3270BinaryPath   string
	s3270BinaryPath   string
	binaryFileMutex   sync.Mutex
	shutdownRequested atomic.Bool
)

// These constants represent the keyboard keys
const (
	Enter = "Enter"
	Tab   = "Tab"
	Reset = "Reset"
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
	maxRetries            = 10          // Maximum number of retries
	retryDelay            = time.Second // Delay between retries (e.g., 1 second)
	scriptDialTimeout     = 5 * time.Second
	scriptIOTimeout       = 30 * time.Second
	startupPollInterval   = 200 * time.Millisecond
	startupConnectTimeout = 20 * time.Second
)

var errScriptTransport = errors.New("script transport error")

// Emulator base struct to x3270 terminal emulator
type Emulator struct {
	Host       string
	Port       int
	ScriptPort string
	// CodePage selects the host EBCDIC code page / character set for the
	// session. When non-empty it is passed to the underlying x3270/s3270
	// process via its -codepage option (e.g. "cp037", "cp285", "cp278" or
	// the alias "finnish"). Empty leaves the emulator default in place.
	CodePage string

	scriptConn   net.Conn
	scriptReader *bufio.Reader
	scriptMu     sync.Mutex

	// Where in the workflow this emulator is, as SetCaptureContext was last
	// told. Read by AsciiScreenGrab so a captured screen records the step it
	// came from; guarded because the runner writes it between steps.
	captureMu    sync.Mutex
	captureStep  int
	captureTotal int
	captureType  string

	connectMu       sync.RWMutex
	connectDuration time.Duration
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

// RequestShutdown signals emulator operations to abort promptly (used when run duration expires).
func RequestShutdown() {
	shutdownRequested.Store(true)
}

// ResetShutdown clears the shutdown flag for a fresh run.
func ResetShutdown() {
	shutdownRequested.Store(false)
}

// ShutdownRequested reports whether shutdown has been requested.
func ShutdownRequested() bool {
	return shutdownRequested.Load()
}

func (e *Emulator) scriptAddress() (string, error) {
	port := strings.TrimSpace(e.ScriptPort)
	if port == "" {
		return "", fmt.Errorf("script port not set")
	}
	return net.JoinHostPort("127.0.0.1", port), nil
}

func (e *Emulator) ensureScriptConnLocked() error {
	if e.scriptConn != nil {
		return nil
	}
	addr, err := e.scriptAddress()
	if err != nil {
		return err
	}
	conn, err := net.DialTimeout("tcp", addr, scriptDialTimeout)
	if err != nil {
		return err
	}
	e.scriptConn = conn
	e.scriptReader = bufio.NewReader(conn)
	return nil
}

func (e *Emulator) closeScriptConnLocked() {
	if e.scriptConn != nil {
		e.scriptConn.Close()
		e.scriptConn = nil
	}
	e.scriptReader = nil
}

func (e *Emulator) closeScriptConn() {
	e.scriptMu.Lock()
	defer e.scriptMu.Unlock()
	e.closeScriptConnLocked()
}

func (e *Emulator) sendScriptCommand(command string) (string, error) {
	e.scriptMu.Lock()
	defer e.scriptMu.Unlock()

	if err := e.ensureScriptConnLocked(); err != nil {
		return "", fmt.Errorf("%w: %w", errScriptTransport, err)
	}

	conn := e.scriptConn
	reader := e.scriptReader
	if conn == nil || reader == nil {
		return "", fmt.Errorf("%w: script connection not initialized", errScriptTransport)
	}
	deadline := time.Now().Add(scriptIOTimeout)
	_ = conn.SetWriteDeadline(deadline)
	if !strings.HasSuffix(command, "\n") {
		command += "\n"
	}
	if _, err := io.WriteString(conn, command); err != nil {
		e.closeScriptConnLocked()
		return "", fmt.Errorf("%w: %w", errScriptTransport, err)
	}
	_ = conn.SetReadDeadline(deadline)
	var lines []string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			e.closeScriptConnLocked()
			return "", fmt.Errorf("%w: %w", errScriptTransport, err)
		}
		trimmed := strings.TrimRight(line, "\r\n")
		switch {
		case trimmed == "ok":
			return strings.Join(lines, "\n"), nil
		case strings.HasPrefix(trimmed, "error"):
			msg := strings.TrimSpace(strings.TrimPrefix(trimmed, "error"))
			if msg == "" {
				msg = "x3270 reported an error"
			}
			return "", errors.New(msg)
		default:
			lines = append(lines, trimmed)
		}
	}
}

func (e *Emulator) scriptRequest(command string) (string, error) {
	output, err := e.sendScriptCommand(command)
	if err == nil {
		return output, nil
	}
	if errors.Is(err, errScriptTransport) {
		return e.sendScriptCommand(command)
	}
	return "", err
}

// WaitForField waits until the screen is ready, the cursor has been positioned
// on a modifiable field, and the keyboard is unlocked.
func (e *Emulator) WaitForField(timeout time.Duration, maxRetries int) error {
	// First, try to wait for the screen to unlock
	unlockCommand := fmt.Sprintf("Wait(%d, Unlock)", int(timeout.Seconds()))
	unlockOutput, unlockErr := e.execCommand(unlockCommand)

	// Query keyboard lock state after Wait(timeout, Unlock)
	if kbLockState, kbErr := e.query("KeyboardLock"); kbErr == nil {
		if Verbose {
			log.Printf("Keyboard lock state after Unlock wait: %s", kbLockState)
		}
	}

	// Check if unlock failed or status is not "U"
	needsReset := false
	if unlockErr != nil {
		needsReset = true
	} else if unlockOutput != "" {
		statusParts := strings.Fields(unlockOutput)
		if len(statusParts) > 0 && statusParts[0] != "U" {
			needsReset = true
		}
	}

	// If we need to reset, send Reset command and retry unlock
	if needsReset {
		if err := e.Press(Reset); err == nil {
			// Retry unlock after reset
			time.Sleep(retryDelay)
			unlockOutput, unlockErr = e.execCommand(unlockCommand)

			// Query keyboard lock state again after reset
			if kbLockState, kbErr := e.query("KeyboardLock"); kbErr == nil {
				if Verbose {
					log.Printf("Keyboard lock state after Reset and Unlock: %s", kbLockState)
				}
			}
		}
	}

	// Send the command to wait for a field with the specified timeout
	command := fmt.Sprintf("Wait(%d, InputField)", int(timeout.Seconds()))

	// Retry the InputField wait operation with a delay in case of failure
	for retries := 0; retries < maxRetries; retries++ {
		output, err := e.execCommand(command)
		if err == nil {
			if output == "" {
				if Verbose {
					log.Println("Wait command executed successfully (no output)")
				}
				return nil
			}

			// Extract the keyboard status from the command output
			statusParts := strings.Fields(output)
			if len(statusParts) > 0 && statusParts[0] != "U" {
				// If InputField wait reports locked state, try sending Reset
				if retries == 0 {
					if resetErr := e.Press(Reset); resetErr == nil {
						time.Sleep(retryDelay)
						continue // Retry after reset
					}
				}
				return fmt.Errorf("keyboard not unlocked, state was: %s", statusParts[0])
			}
			//fmt.Printf("Wait command executed successfully %s", statusParts[0])
			//fmt.Printf("Wait command executed successfully\n")
			return nil // Successful operation, exit the retry loop
		}

		time.Sleep(retryDelay)
	}

	// Smart WaitForField: Check if screen has any modifiable fields
	// If no modifiable fields exist, treat as success (read-only screen)
	// Note: We intentionally ignore queryErr here. If the query fails, we proceed
	// with the original "maximum retries reached" error, which is the expected behavior.
	if fields, queryErr := e.query("Fields"); queryErr == nil {
		// The x3270 Fields query returns field attributes including "unprotected" for modifiable fields.
		// A simple case-insensitive search is sufficient as "unprotected" is a standard x3270 field attribute.
		if !strings.Contains(strings.ToLower(fields), "unprotected") {
			// No modifiable fields found; treat as success for read-only screens
			return nil
		}
	}

	// On failure, query KeyboardLockDetail for diagnostic information
	var kbLockDetail string
	if detail, detailErr := e.query("KeyboardLockDetail"); detailErr == nil {
		kbLockDetail = detail
	} else {
		kbLockDetail = fmt.Sprintf("(unable to query: %v)", detailErr)
	}

	return fmt.Errorf("maximum WaitForField retries reached | KeyboardLockDetail: %s", kbLockDetail)
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

// validateKeyboard validates if the key passed by parameter is a valid key
func (e *Emulator) validateKeyboard(key string) bool {
	switch key {
	case Tab:
		return true
	case Enter:
		return true
	case Reset:
		return true
	case F1, F2, F3, F4, F5, F6, F7, F8, F9, F10, F11, F12:
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
			return normalizeAsciiData(output), nil // Successful operation, exit the retry loop
		}
		//log.Printf("Error executing Ascii command (Retry %d): %v\n", retries+1, err)
		time.Sleep(retryDelay)
	}

	return "", fmt.Errorf("maximum GetValue retries reached")
}

// NormalizeQueryResponse reduces a raw Query reply to the value it carries.
//
// s3270 answers on the scripting protocol with the value on a "data:" line
// followed by a status line and "ok". Callers that want to parse the value —
// the host compatibility profiler is one — need the value alone, and Query
// deliberately hands back the reply untouched.
func NormalizeQueryResponse(raw string) string {
	return normalizeAsciiData(raw)
}

// normalizeAsciiData trims the s3270/x3270 "data:" prefix and drops status lines.
func normalizeAsciiData(raw string) string {
	lines := strings.Split(raw, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "data:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		}
	}
	return strings.TrimSpace(raw)
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

	start := time.Now()

	// Retry logic for connecting
	for retries := 0; retries < maxRetries; retries++ {
		if ShutdownRequested() {
			return fmt.Errorf("shutdown requested")
		}

		if e.ScriptPort == "" {
			log.Println("ScriptPort not set, using default 5000")
			e.ScriptPort = "5000"
		}

		if Verbose {
			log.Printf("Connect attempt %d/%d using -scriptport: %s", retries+1, maxRetries, e.ScriptPort)
		}

		// Reset any lingering script connection before the next attempt.
		e.closeScriptConn()

		if err := e.createApp(); err != nil {
			// Don't log shutdown errors as errors - they are expected during graceful shutdown
			if err.Error() != "shutdown requested" {
				if retries+1 == maxRetries {
					msg := fmt.Sprintf("ERROR createApp failed (attempt %d/%d): %v", retries+1, maxRetries, err)
					pterm.Error.Println(msg)
				}
			}
			e.rotateScriptPort() // Avoid retrying on a potentially poisoned script port.
			time.Sleep(retryDelay)
			continue
		}

		if e.IsConnected() {
			d := time.Since(start)
			e.connectMu.Lock()
			e.connectDuration = d
			e.connectMu.Unlock()
			observeConnectDuration(d)
			return nil // Successfully connected, exit the retry loop
		}

		// Emulator did not report connected; clean up and retry to avoid poisoning the worker's script port.
		_ = e.Disconnect()
		e.rotateScriptPort()
		time.Sleep(retryDelay)
	}

	return fmt.Errorf("maximum connect retries reached")
}

// LastConnectDuration returns the elapsed time of the most recent successful
// Connect call. Zero if no connect has completed.
func (e *Emulator) LastConnectDuration() time.Duration {
	e.connectMu.RLock()
	defer e.connectMu.RUnlock()
	return e.connectDuration
}

// Query exposes the x3270/s3270 Query(arg) action publicly. Returns the raw
// response with each line's "data: " prefix preserved or stripped as the
// caller expects; an empty arg returns the catch-all Query() response.
// Empty response and a nil error means the host does not answer the query.
func (e *Emulator) Query(arg string) (string, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return e.execCommandOutput("query")
	}
	return e.query(arg)
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
	e.closeScriptConn()

	return nil
}

// query returns state information from x3270
func (e *Emulator) query(keyword string) (string, error) {
	command := fmt.Sprintf("query(%s)", keyword)
	return e.execCommandOutput(command)
}

// buildEmulatorArgs assembles the command-line arguments for launching the
// embedded x3270/s3270/wc3270 process. The argument order mirrors the
// historical invocation so existing behavior is unchanged; the host EBCDIC
// code page (-codepage) is inserted only when Emulator.CodePage is set, and
// the host:port target is always the final positional argument.
func (e *Emulator) buildEmulatorArgs(modelType string) []string {
	resourceString := "x3270.unlockDelay: False"
	if Headless {
		resourceString = "s3270.unlockDelay: False"
	} else if runtime.GOOS == "windows" {
		resourceString = "wc3270.unlockDelay: False"
	}

	var args []string
	if Headless {
		args = []string{"-utf8", "-scriptport", e.ScriptPort, "-xrm", resourceString, "-model", modelType}
	} else {
		args = []string{"-utf8", "-xrm", resourceString, "-scriptport", e.ScriptPort, "-model", modelType}
	}

	// Host EBCDIC code page / character set (e.g. cp037, cp285, cp278). When
	// unset, the emulator uses its built-in default code page.
	if codePage := strings.TrimSpace(e.CodePage); codePage != "" {
		args = append(args, "-codepage", codePage)
		if Verbose {
			log.Printf("Using host code page (-codepage %s) for %s", codePage, e.hostname())
		}
	}

	args = append(args, e.hostname())
	return args
}

// createApp creates a connection to the host using embedded x3270 or s3270
func (e *Emulator) createApp() error {
	if Verbose {
		log.Println("func createApp: using -scriptport: " + e.ScriptPort)
	}
	e.closeScriptConn()

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

	cmd := exec.Command(binaryFilePath, e.buildEmulatorArgs(modelType)...)

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
		defer stderr.Close()
		errMsg, _ := ioutil.ReadAll(stderr)
		if Verbose && len(errMsg) > 0 {
			log.Printf("3270 stderr: %s", string(errMsg))
		}
		if err := cmd.Wait(); err != nil && Verbose {
			log.Printf("Error waiting for 3270 instance: %v", err)
		}
	}()

	deadline := time.Now().Add(startupConnectTimeout)
	connected := false
	attempt := 0
	for time.Now().Before(deadline) {
		if ShutdownRequested() {
			return fmt.Errorf("shutdown requested")
		}
		if e.IsConnected() {
			connected = true
			break
		}
		if Verbose {
			log.Printf("Waiting for emulator session (%s) to report connected (attempt %d, %.1fs left)", e.hostname(), attempt+1, time.Until(deadline).Seconds())
		}
		time.Sleep(startupPollInterval)
		attempt++
	}

	if !connected {
		// Ensure the launched emulator process does not linger and hold the script port.
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		e.closeScriptConn()
		return fmt.Errorf("timed out waiting for emulator to connect to %s after %.1fs", e.hostname(), startupConnectTimeout.Seconds())
	}

	return nil
}

// rotateScriptPort selects the next available script port to reduce collisions and stuck sessions.
func (e *Emulator) rotateScriptPort() {
	current := 5000
	if p, err := strconv.Atoi(strings.TrimSpace(e.ScriptPort)); err == nil && p > 0 {
		current = p
	}
	for i := 0; i < 20; i++ {
		candidate := current + i + 1
		if isTCPPortAvailable(candidate) {
			e.ScriptPort = strconv.Itoa(candidate)
			if Verbose {
				log.Printf("Rotated script port to %s", e.ScriptPort)
			}
			return
		}
	}
	// Fallback: increment even if availability check failed.
	e.ScriptPort = strconv.Itoa(current + 1)
	if Verbose {
		log.Printf("Fallback rotating script port to %s", e.ScriptPort)
	}
}

func isTCPPortAvailable(port int) bool {
	ln, err := net.Listen("tcp", ":"+strconv.Itoa(port))
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
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
	return e.scriptRequest(command)
}

// execCommandOutput executes a command on the connected x3270 or s3270 instance based on Headless flag and returns output
func (e *Emulator) execCommandOutput(command string) (string, error) {
	if Verbose {
		log.Printf("Executing command with output: %s", command)
	}
	return e.scriptRequest(command)
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
		outputContent += fmt.Sprintf("<html><head><title>ASCII Screen Capture</title>")
		outputContent += `<style>
body {
	background-color: #031611;
	color: #4effb3;
	font-family: 'Courier New', Courier, monospace;
	margin: 0;
	padding: 20px;
}
h1 {
	color: #4effb3;
	text-shadow: 0 0 16px rgba(78, 255, 176, 0.28);
	letter-spacing: 0.06em;
	font-size: 2em;
	margin-bottom: 10px;
}
p {
	color: #cafee9;
	margin-bottom: 20px;
}
pre {
	color: #4effb3;
	background-color: #031611;
	border: 1px solid rgba(78, 255, 176, 0.38);
	padding: 15px;
	border-radius: 8px;
	overflow-x: auto;
	font-family: 'Courier New', Courier, monospace;
	line-height: 1.4;
}
</style></head><body>`
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

// AsciiScreen returns the current screen as plain ASCII text without
// touching the filesystem. Used by the profiler for banner fingerprinting.
func (e *Emulator) AsciiScreen() (string, error) {
	return e.execCommandOutput("Ascii()")
}

// SetCaptureContext records the step the emulator is working on, so screens
// written by AsciiScreenGrab can say which step of which workflow produced
// them. The workflow runner calls it as it walks the steps; leaving it unset
// simply omits the step from the capture's attributes.
func (e *Emulator) SetCaptureContext(step, totalSteps int, stepType string) {
	e.captureMu.Lock()
	e.captureStep = step
	e.captureTotal = totalSteps
	e.captureType = stepType
	e.captureMu.Unlock()
}

// captureSeq numbers every screen this process writes. One counter for the
// whole process rather than one per emulator: concurrent workers append to
// the same file, and a reader needs the order they landed in, not the order
// each worker thinks it captured them.
var captureSeq atomic.Int64

// captureAttrs renders the metadata a capture carries as HTML attributes on
// its <pre> element. Attributes rather than a comment or a wrapper: browsers
// have shown this file unchanged since the first release and must carry on
// doing so, while the console can now tell one worker's screens from
// another's instead of showing every screen in the run as one wall of text.
func (e *Emulator) captureAttrs(now time.Time) string {
	e.captureMu.Lock()
	step, total, stepType := e.captureStep, e.captureTotal, e.captureType
	e.captureMu.Unlock()

	attrs := fmt.Sprintf(` data-capture="%d" data-at="%d"`, captureSeq.Add(1), now.UnixMilli())
	if e.ScriptPort != "" {
		attrs += fmt.Sprintf(` data-port="%s"`, escapeAttr(e.ScriptPort))
	}
	if e.Host != "" {
		attrs += fmt.Sprintf(` data-host="%s"`, escapeAttr(e.Host))
		if e.Port > 0 {
			attrs += fmt.Sprintf(` data-hostport="%d"`, e.Port)
		}
	}
	if step > 0 {
		attrs += fmt.Sprintf(` data-step="%d"`, step)
	}
	if total > 0 {
		attrs += fmt.Sprintf(` data-steps="%d"`, total)
	}
	if stepType != "" {
		attrs += fmt.Sprintf(` data-type="%s"`, escapeAttr(stepType))
	}
	return attrs
}

// escapeAttr makes a value safe to sit inside a double-quoted HTML attribute.
func escapeAttr(value string) string {
	replacer := strings.NewReplacer("&", "&amp;", `"`, "&quot;", "<", "&lt;", ">", "&gt;")
	return replacer.Replace(value)
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
				// In non-API mode, format the output as output.
				// Written in one call: concurrent workers append to the same
				// file, and a screen split across two writes would interleave
				// with another worker's.
				content = fmt.Sprintf("<pre%s>%s</pre>\n", e.captureAttrs(time.Now()), output)
				content += "</body></html>"
			}

			if err := writeScreenGrab(filePath, content); err != nil {
				return err
			}
			return nil
		}
		time.Sleep(retryDelay)
	}

	return fmt.Errorf("maximum capture retries reached")
}

func writeScreenGrab(filePath, content string) (err error) {
	file, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("Error opening or creating file: %v", err)
		return err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			log.Printf("Error closing file: %v", closeErr)
			err = closeErr
		}
	}()
	if _, err = file.WriteString(content); err != nil {
		log.Printf("Error writing to file: %v", err)
		return err
	}
	return nil
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
