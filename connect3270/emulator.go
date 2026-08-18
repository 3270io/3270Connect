package connect3270

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
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

// DefaultModel is the device type negotiated when a workflow does not ask
// for one: the 24x80 colour model 2, which is what this tool has always
// used and what the overwhelming majority of green-screen applications are
// written for.
const DefaultModel = "3279-2"

// These constants represent the keyboard keys
const (
	Enter = "Enter"
	Tab   = "Tab"
	Reset = "Reset"

	// The attention keys. PA1 is the one an operator reaches for to
	// interrupt a running transaction and PA2 typically cancels; neither
	// carries the screen's contents back to the host the way a PF key does,
	// which is exactly why applications use them. There was previously no
	// way to send any of them.
	PA1 = "PA(1)"
	PA2 = "PA(2)"
	PA3 = "PA(3)"

	// Clear erases the screen and sends an AID, and is how a CICS user gets
	// from a transaction back to a blank screen to type the next one.
	Clear = "Clear"

	// Field and cursor movement. BackTab walks to the previous field,
	// Home goes to the first unprotected one, and EraseEOF clears from the
	// cursor to the end of the field — the standard way to overwrite a
	// field that already holds a longer value.
	BackTab    = "BackTab"
	Home       = "Home"
	EraseEOF   = "EraseEOF"
	EraseInput = "EraseInput"
	Newline    = "Newline"
	Up         = "Up"
	Down       = "Down"
	Left       = "Left"
	Right      = "Right"
	BackSpace  = "BackSpace"
	Delete     = "Delete"
	Insert     = "Insert"
	Dup        = "Dup"
	FieldMark  = "FieldMark"

	// SysReq reaches the SSCP rather than the application, which is how a
	// hung LU session is dropped on VTAM hosts. Attn is its TN3270E
	// equivalent for interrupting the application.
	SysReq = "SysReq"
	Attn   = "Attn"

	F1  = "PF(1)"
	F2  = "PF(2)"
	F3  = "PF(3)"
	F4  = "PF(4)"
	F5  = "PF(5)"
	F6  = "PF(6)"
	F7  = "PF(7)"
	F8  = "PF(8)"
	F9  = "PF(9)"
	F10 = "PF(10)"
	F11 = "PF(11)"
	F12 = "PF(12)"
	F13 = "PF(13)"
	F14 = "PF(14)"
	F15 = "PF(15)"
	F16 = "PF(16)"
	F17 = "PF(17)"
	F18 = "PF(18)"
	F19 = "PF(19)"
	F20 = "PF(20)"
	F21 = "PF(21)"
	F22 = "PF(22)"
	F23 = "PF(23)"
	F24 = "PF(24)"
)

const (
	maxRetries            = 10          // Maximum number of retries
	retryDelay            = time.Second // Delay between retries (e.g., 1 second)
	scriptDialTimeout     = 5 * time.Second
	scriptIOTimeout       = 30 * time.Second
	startupPollInterval   = 200 * time.Millisecond
	startupConnectTimeout = 20 * time.Second
	// processExitGrace is how long an emulator gets to exit on its own after
	// being told to quit, before it is killed and its script port reclaimed.
	processExitGrace = 5 * time.Second
	// firstScreenTimeout bounds the wait for the host's first screen after
	// the session comes up. See waitForHostReady.
	firstScreenTimeout = 3 * time.Second
)

// procHandle is a launched emulator process and a channel closed once it has
// been waited on.
type procHandle struct {
	proc *os.Process
	done chan struct{}
}

var errScriptTransport = errors.New("script transport error")

// ErrInvalidConfiguration marks a session that is misconfigured rather than
// unlucky — an unknown model, an unreadable oversize. Connect reports it once
// instead of retrying ten times, because the eleventh attempt would be wrong
// in exactly the same way.
var ErrInvalidConfiguration = errors.New("invalid terminal configuration")

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

	// Model is the 3270 device type to negotiate: "2".."5", or the full
	// form "3278-4" / "3279-5". Empty means DefaultModel, the 24x80 colour
	// model 2 the tool has always used. The model decides the alternate
	// screen size, so a workflow written against a 43x80 or 27x132 host
	// needs this set or the host will only ever offer it 24 rows.
	Model string

	// Oversize asks for a screen larger than the model defines, as
	// "<cols>x<rows>" (e.g. "132x50"). Only hosts that support the larger
	// geometry will use it. Empty leaves the model's own size.
	Oversize string

	// LUName requests a specific logical unit at connect time. Hosts that
	// route by LU — most CICS and TSO installations do — need it, and there
	// was previously no way to ask for one.
	LUName string

	// TLS wraps the host connection in TLS (the "L:" host prefix). Modern
	// z/OS installations increasingly accept nothing else.
	TLS bool

	// InsecureSkipVerify disables host certificate validation. It exists
	// for the internal host with a private CA and a self-signed
	// certificate; it is off by default and stays that way.
	InsecureSkipVerify bool

	scriptConn   net.Conn
	scriptReader *bufio.Reader
	scriptMu     sync.Mutex

	// Last status line seen on the scripting connection. Every command
	// carries one, so this is current as of the previous command rather
	// than something that has to be asked for.
	statusMu sync.RWMutex
	status   Status

	// The launched emulator process, so a session that will not shut down
	// cleanly can still be stopped rather than left holding its script port
	// for the rest of a load test.
	procMu sync.Mutex
	proc   *procHandle

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
	var failure string
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
			// The emulator explains the failure on a data line and then
			// says "error". Carry the explanation into the error rather
			// than replacing it with a generic one: "Ascii: Invalid
			// argument" says the coordinates are off the screen, where
			// "x3270 reported an error" says nothing at all.
			msg := strings.TrimSpace(strings.TrimPrefix(trimmed, "error"))
			if detail := strings.TrimSpace(NormalizeDataLines(strings.Join(lines, "\n"))); detail != "" {
				msg = detail
			}
			if msg == "" {
				msg = "x3270 reported an error"
			}
			failure = msg
			return "", &ActionError{Message: failure}
		case isStatusLine(trimmed):
			// Every reply carries one of these. Record it and keep it out
			// of the payload: it is protocol state, and a caller capturing
			// a screen wants the screen, not a line of emulator status
			// pasted onto the bottom of it.
			e.setStatus(ParseStatus(trimmed))
		default:
			lines = append(lines, trimmed)
		}
	}
}

// ActionError is an error the emulator itself reported, as opposed to a
// transport failure. It carries the emulator's own message, which is the
// only thing that says whether a command failed because the host was busy —
// worth retrying — or because the command could never have worked.
type ActionError struct {
	Message string
}

func (e *ActionError) Error() string { return e.Message }

// deterministic reports whether retrying the action could ever produce a
// different answer. A syntax error or an out-of-range coordinate will fail
// identically every time, so retrying it three times a second apart only
// delays the report and then replaces the emulator's explanation with
// "maximum retries reached".
func (e *ActionError) deterministic() bool {
	msg := strings.ToLower(e.Message)
	for _, marker := range []string{
		"syntax error",
		"invalid argument",
		"unknown action",
		"unknown parameter",
		"too few arguments",
		"too many arguments",
		"invalid model",
	} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

// isKeyboardLock reports whether the emulator refused because the host has
// locked the keyboard after an operator error — typing into a protected
// field, or past the end of one.
func isKeyboardLock(err error) bool {
	var actionErr *ActionError
	if !errors.As(err, &actionErr) {
		return false
	}
	msg := strings.ToLower(actionErr.Message)
	return strings.Contains(msg, "keyboard locked") || strings.Contains(msg, "operator error")
}

// isDeterministicFailure reports whether err is an emulator complaint that
// will not change on a retry.
func isDeterministicFailure(err error) bool {
	var actionErr *ActionError
	if errors.As(err, &actionErr) {
		return actionErr.deterministic()
	}
	return false
}

// setStatus records the status line from the most recent reply.
func (e *Emulator) setStatus(s Status) {
	if !s.Valid {
		return
	}
	e.statusMu.Lock()
	e.status = s
	e.statusMu.Unlock()
}

// Status returns the emulator state as of the last command. The zero value,
// with Valid false, means no command has run yet.
func (e *Emulator) Status() Status {
	e.statusMu.RLock()
	defer e.statusMu.RUnlock()
	return e.status
}

// ScreenSize returns the rows and columns the host is currently using, as
// last reported. Zeroes mean the size is not yet known.
//
// It is read from the status line rather than cached at connect time on
// purpose: a host that issues an erase/write-alternate switches a model 4
// session from 24 rows to 43 mid-run, and a workflow addressing row 30 is
// then perfectly valid where a moment earlier it was not.
func (e *Emulator) ScreenSize() (rows, cols int) {
	s := e.Status()
	return s.Rows, s.Cols
}

// checkCoordinates rejects a position that is off the current screen.
//
// The emulator does not: MoveCursor to row 30 of a 24-row screen is answered
// with "ok" and the cursor clamped to the last row, so a workflow written
// for a model 4 host, run against a model 2 session, quietly types its input
// into whatever field happens to be at the bottom of the screen. That is the
// failure mode worth spending an error message on.
func (e *Emulator) checkCoordinates(row, col int) error {
	if row < 1 || col < 1 {
		return fmt.Errorf("coordinates are 1-based; row %d column %d is not a screen position", row, col)
	}
	rows, cols := e.ScreenSize()
	if rows <= 0 || cols <= 0 {
		// Geometry not known yet — let the emulator have its say.
		return nil
	}
	if row > rows || col > cols {
		return fmt.Errorf("row %d column %d is outside the %dx%d screen this host negotiated (set Model for a larger screen)", row, col, rows, cols)
	}
	return nil
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
	// The emulator's Wait takes whole seconds, so a sub-second timeout would
	// truncate to Wait(0, ...). Round up instead of asking for zero.
	waitSeconds := int(timeout.Seconds())
	if timeout > 0 && waitSeconds < 1 {
		waitSeconds = 1
	}

	// First, try to wait for the screen to unlock
	unlockCommand := fmt.Sprintf("Wait(%d, Unlock)", waitSeconds)
	_, unlockErr := e.execCommand(unlockCommand)

	// The keyboard state arrives on the status line of every reply, so it
	// costs nothing to read and needs no separate Query round trip.
	if Verbose {
		log.Printf("Keyboard state after Unlock wait: %s", e.Status().Raw)
	}

	// Check if unlock failed or the keyboard is still locked
	needsReset := unlockErr != nil || e.Status().KeyboardLocked

	// If we need to reset, send Reset command and retry unlock
	if needsReset {
		if err := e.Press(Reset); err == nil {
			// Retry unlock after reset
			time.Sleep(retryDelay)
			_, unlockErr = e.execCommand(unlockCommand)
			if Verbose {
				log.Printf("Keyboard state after Reset and Unlock: %s", e.Status().Raw)
			}
		}
	}

	// Send the command to wait for a field with the specified timeout
	command := fmt.Sprintf("Wait(%d, InputField)", waitSeconds)

	// Retry the InputField wait operation with a delay in case of failure
	for retries := 0; retries < maxRetries; retries++ {
		_, err := e.execCommand(command)
		if err == nil {
			if !e.Status().KeyboardLocked {
				return nil // Successful operation, exit the retry loop
			}
			// If InputField wait reports locked state, try sending Reset
			if retries == 0 {
				if resetErr := e.Press(Reset); resetErr == nil {
					time.Sleep(retryDelay)
					continue // Retry after reset
				}
			}
			state := "locked"
			if e.Status().KeyboardError {
				state = "locked with an error condition"
			}
			return fmt.Errorf("keyboard not unlocked, state was: %s", state)
		}

		time.Sleep(retryDelay)
	}

	// A screen with no fields on it has no input field to wait for, and
	// waiting for one until the retries run out is not a useful way to
	// discover that. An unformatted screen is the read-only case.
	//
	// This used to ask Query(Fields) and Query(KeyboardLockDetail), neither
	// of which x3270 has: both answered "Query: Unknown parameter", so the
	// read-only case never triggered and every failure carried a diagnostic
	// reading "(unable to query: Query: Unknown parameter)". Formatted is a
	// real query, and it is on the status line of every reply besides.
	status := e.Status()
	if !status.Valid {
		if formatted, err := e.query("Formatted"); err == nil {
			if !strings.Contains(strings.ToLower(NormalizeDataLines(formatted)), "unformatted") {
				status = e.Status()
			} else {
				return nil
			}
		}
	}
	if status.Valid && !status.Formatted {
		return nil
	}

	state := "locked, waiting for the host"
	switch {
	case !status.Valid:
		state = "the emulator did not answer"
	case status.KeyboardError:
		state = "locked by an operator error, which a Reset clears"
	case !status.KeyboardLocked:
		state = "unlocked, but no input field appeared"
	}
	if status.Raw != "" {
		return fmt.Errorf("the keyboard did not become ready after %d attempts: %s | status: %s",
			maxRetries, state, status.Raw)
	}
	return fmt.Errorf("the keyboard did not become ready after %d attempts: %s", maxRetries, state)
}

// moveCursor moves the cursor to the specified row (x) and column (y) with retry logic.
func (e *Emulator) moveCursor(x, y int) error {
	// Retry logic parameters
	maxRetries := 3
	retryDelay := 1 * time.Second

	if err := e.checkCoordinates(x, y); err != nil {
		return err
	}

	// Adjust the values to start at 0 internally
	xAdjusted := x - 1
	yAdjusted := y - 1
	command := fmt.Sprintf("MoveCursor(%d,%d)", xAdjusted, yAdjusted)

	// Retry the MoveCursor operation with a delay in case of failure
	var lastErr error
	for retries := 0; retries < maxRetries; retries++ {
		_, err := e.execCommand(command)
		if err == nil {
			return nil // Successful operation, exit the retry loop
		}
		lastErr = err
		if isDeterministicFailure(err) {
			return fmt.Errorf("MoveCursor to row %d column %d: %w", x, y, err)
		}
		time.Sleep(retryDelay)
	}

	return fmt.Errorf("maximum MoveCursor retries reached: %w", lastErr)
}

// SetString fills the field at the current cursor position with the given value and retries in case of failure.
func (e *Emulator) SetString(value string) error {
	// Retry logic parameters
	maxRetries := 3
	retryDelay := 1 * time.Second

	// Quoted, so the value is typed as written. See quoteActionArg.
	command := fmt.Sprintf("String(%s)", quoteActionArg(value))

	// Retry the SetString operation with a delay in case of failure
	var lastErr error
	for retries := 0; retries < maxRetries; retries++ {
		_, err := e.execCommand(command)
		if err == nil {
			return nil // Successful operation, exit the retry loop
		}
		lastErr = err
		if isDeterministicFailure(err) {
			return fmt.Errorf("String: %w", err)
		}
		if isKeyboardLock(err) {
			// The lock does not clear itself, so retrying into it fails the
			// same way three times and then leaves the session locked for
			// every step that follows. Reset is what an operator presses.
			_ = e.Press(Reset)
		}
		time.Sleep(retryDelay)
	}

	return fmt.Errorf("maximum SetString retries reached: %w", lastErr)
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
		// The reply to that MoveCursor said whether the cursor landed on a
		// protected field. Typing there locks the keyboard with an operator
		// error and leaves it locked, so every step after this one fails
		// too — and the report names whichever of them ran into the lock
		// rather than the step that caused it.
		if s := e.Status(); s.Valid && s.Formatted && s.Protected {
			return fmt.Errorf("row %d column %d is a protected field on this screen; the host does not accept input there", x, y)
		}
	}

	if err := e.checkFieldFits(value); err != nil {
		return err
	}

	// Retry the SetString operation with a delay in case of failure
	var lastErr error
	for retries := 0; retries < maxRetries; retries++ {
		err := e.SetString(value) // Declare and define err here
		if err == nil {
			return nil // Successful operation, exit the retry loop
		}
		lastErr = err
		if isDeterministicFailure(err) {
			return fmt.Errorf("filling row %d column %d: %w", x, y, err)
		}
		time.Sleep(retryDelay)
	}

	return fmt.Errorf("maximum FillString retries reached: %w", lastErr)
}

// checkFieldFits rejects a value longer than the field it is aimed at.
//
// The emulator does not stop typing at the end of a field: the tail runs on
// into whichever field comes next. An over-long name goes into the name
// field and then into the one below it, with no error anywhere — and on a
// logon screen the field below the user name is usually the password.
//
// The check is skipped for a value carrying a tab or a newline, because
// those move between fields deliberately and "how long is the field" is then
// the wrong question.
func (e *Emulator) checkFieldFits(value string) error {
	if value == "" || strings.ContainsAny(value, "\t\n\r") {
		return nil
	}
	if s := e.Status(); s.Valid && !s.Formatted {
		// Nothing to overflow into on an unformatted screen.
		return nil
	}

	field, err := e.execCommandOutput("AsciiField()")
	if err != nil {
		// The emulator would not say. Better to type than to refuse on the
		// strength of a failed measurement.
		return nil
	}
	// One data line per row the field spans; the field is their total
	// length, so the newlines are not part of it.
	length := len([]rune(strings.ReplaceAll(NormalizeDataLines(field), "\n", "")))
	if length == 0 {
		return nil
	}
	if n := len([]rune(value)); n > length {
		return fmt.Errorf("the value is %d characters and the field holds %d; typing it would overflow into the next field", n, length)
	}
	return nil
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
	case PA1, PA2, PA3:
		return true
	case Clear, SysReq, Attn:
		return true
	case BackTab, Home, EraseEOF, EraseInput, Newline:
		return true
	case Up, Down, Left, Right, BackSpace, Delete, Insert, Dup, FieldMark:
		return true
	case F1, F2, F3, F4, F5, F6, F7, F8, F9, F10, F11, F12:
		return true
	case F13, F14, F15, F16, F17, F18, F19, F20, F21, F22, F23, F24:
		return true
	default:
		return false
	}
}

// IsConnected reports whether the emulator has a live session with the host.
//
// It asks the emulator and reads the answer. The previous version treated any
// reply as "connected" — including the emulator plainly answering
// "not-connected" — and slept a second first, which put two seconds on every
// connect and meant an unreachable host was reported as connected.
func (e *Emulator) IsConnected() bool {
	s, err := e.query("ConnectionState")
	if err != nil {
		return false
	}
	if state := strings.TrimSpace(NormalizeDataLines(s)); state != "" {
		return connectionStateIsConnected(state)
	}
	// No answer to parse: fall back to the status line the reply carried.
	return e.Status().Connected
}

// GetValue returns content of a specified length at the specified row (x) and column (y) with retry logic.
func (e *Emulator) GetValue(x, y, length int) (string, error) {
	// Retry logic parameters
	maxRetries := 3
	retryDelay := 1 * time.Second

	if err := e.checkCoordinates(x, y); err != nil {
		return "", err
	}

	// Adjust the row and column values to start at 1 internally
	xAdjusted := x - 1
	yAdjusted := y - 1
	command := fmt.Sprintf("Ascii(%d,%d,%d)", xAdjusted, yAdjusted, length)

	// Retry the Ascii command with a delay in case of failure
	var lastErr error
	for retries := 0; retries < maxRetries; retries++ {
		output, err := e.execCommandOutput(command)
		if err == nil {
			return normalizeAsciiData(output), nil // Successful operation, exit the retry loop
		}
		lastErr = err
		if isDeterministicFailure(err) {
			return "", fmt.Errorf("reading row %d column %d for %d characters: %w", x, y, length, err)
		}
		time.Sleep(retryDelay)
	}

	return "", fmt.Errorf("maximum GetValue retries reached: %w", lastErr)
}

// NormalizeDataLines reduces a raw s3270 reply to the data it carries.
//
// s3270 answers on the scripting protocol with the payload on one or more
// "data:" lines followed by a status line. This keeps the data lines, strips
// the prefix from each and joins them, leaving the rest of the spacing alone
// because a screen's indentation is part of the screen.
//
// It is deliberately the same transform 3270Web applies in its own host
// package, which is what lets a CompatibilityProfile from one tool be compared
// with the other's. A reply carrying no data lines yields "", which the
// profiler reads as "the terminal did not say" rather than as a value.
func NormalizeDataLines(raw string) string {
	const prefix = "data:"
	var kept []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimRight(line, "\r")
		if !strings.HasPrefix(strings.TrimSpace(line), prefix) {
			continue
		}
		value := strings.TrimPrefix(strings.TrimLeft(line, " \t"), prefix)
		kept = append(kept, strings.TrimPrefix(value, " "))
	}
	return strings.Join(kept, "\n")
}

// normalizeAsciiData reduces an Ascii(row, col, length) reply to the
// characters it read.
//
// A read that runs past the end of a row continues on the next one, and the
// emulator answers with one data line per row. Keeping only the first, as
// this used to, silently shortened the value: a CheckValue for twenty
// characters from column 70 compared against the eleven before the row ended
// and reported a match. The lines are joined without a separator because the
// 3270 buffer is one continuous run of characters — the row break is a
// property of the screen, not of the value.
//
// The result is trimmed as a whole, so a single-row read is unchanged and a
// wrapped one keeps the spacing between its halves.
func normalizeAsciiData(raw string) string {
	hasData := false
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "data:") {
			hasData = true
			break
		}
	}
	if !hasData {
		return strings.TrimSpace(raw)
	}
	return strings.TrimSpace(strings.ReplaceAll(NormalizeDataLines(raw), "\n", ""))
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
	var lastConnectErr error

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
			if errors.Is(err, ErrInvalidConfiguration) {
				return err
			}
			// Don't log shutdown errors as errors - they are expected during graceful shutdown
			if err.Error() != "shutdown requested" {
				if retries+1 == maxRetries {
					// Plain text through the standard logger rather than a
					// coloured banner. This is a library, and its one piece
					// of terminal output was the one thing in a CI log that
					// arrived wrapped in escape sequences.
					log.Printf("connect failed after %d attempts: %v", maxRetries, err)
					lastConnectErr = err
				}
			}
			e.rotateScriptPort() // Avoid retrying on a potentially poisoned script port.
			time.Sleep(retryDelay)
			continue
		}

		if e.IsConnected() {
			e.waitForHostReady()
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

	if lastConnectErr != nil {
		// The reason, not just the count: "Connection failed" or a rejected
		// certificate is what the operator needs, and it used to be printed
		// and then dropped rather than returned.
		return fmt.Errorf("could not connect to %s after %d attempts: %w", e.hostTarget(), maxRetries, lastConnectErr)
	}
	return fmt.Errorf("could not connect to %s after %d attempts", e.hostTarget(), maxRetries)
}

// waitForHostReady gives the host a bounded moment to paint its first screen
// before Connect reports success.
//
// A session is "connected" as soon as the telnet negotiation finishes, which
// is before the host has written anything: a capture taken immediately after
// Connect could come back blank. This waits for the keyboard to be handed
// back, which is the host saying it has finished writing — for a formatted
// screen and an unformatted one alike.
//
// Best effort by design. A host that never unlocks the keyboard is a problem
// for the steps that follow to report, not a reason to fail the connect, so
// the outcome is deliberately ignored.
func (e *Emulator) waitForHostReady() {
	_, _ = e.execCommand(fmt.Sprintf("Wait(%d, Unlock)", int(firstScreenTimeout.Seconds())))
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

// Disconnect shuts the emulator down and releases its script port.
//
// The quit is unconditional. It used to be sent only when IsConnected said
// yes, which was harmless while that always said yes — but an emulator whose
// host has dropped the session still has a process and still holds its script
// port, and during a concurrent run those accumulate until the port range
// runs out. If quit does not land, the process is killed rather than left.
func (e *Emulator) Disconnect() error {
	if Verbose {
		log.Println("Disconnecting from x3270")
	}

	_, err := e.execCommand("quit")
	e.closeScriptConn()

	// "quit" makes the emulator exit, so it frequently closes the
	// connection before answering. That is success, not a failure.
	if err != nil && errors.Is(err, errScriptTransport) {
		err = nil
	}

	e.stopProcess()

	if err != nil {
		return fmt.Errorf("error executing quit command: %v", err)
	}
	return nil
}

// stopProcess makes sure the launched emulator is gone.
//
// It waits for the exit in the background rather than making the caller wait
// for it, and only kills a process that is still running — createApp closes
// the handle's done channel once it has been reaped, so this cannot signal a
// pid that has since been given to something else.
func (e *Emulator) stopProcess() {
	e.procMu.Lock()
	h := e.proc
	e.proc = nil
	e.procMu.Unlock()
	if h == nil {
		return
	}
	go func() {
		select {
		case <-h.done:
		case <-time.After(processExitGrace):
			_ = h.proc.Kill()
		}
	}()
}

// query returns state information from x3270
func (e *Emulator) query(keyword string) (string, error) {
	command := fmt.Sprintf("query(%s)", keyword)
	return e.execCommandOutput(command)
}

// NormalizeModel turns a model as a workflow may write it into the form the
// emulator's -model option takes, and rejects one it does not.
//
// Rejecting matters more than normalising. The emulator answers an unknown
// model number by printing "Invalid model number" to stderr — which this tool
// only shows in verbose mode — and then carrying on with a different model
// entirely, so a typo becomes a session quietly negotiated as something other
// than what the workflow asked for.
//
// Accepted: "2".."5", "3278-<n>", "3279-<n>", and either with a trailing
// "-E". A bare "3278"/"3279" is not accepted: the emulator reads it as no
// model number at all and falls back to its own default.
func NormalizeModel(model string) (string, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		return DefaultModel, nil
	}

	family := "3279"
	number := model
	if idx := strings.Index(model, "-"); idx > 0 {
		family = model[:idx]
		number = model[idx+1:]
		// "3279-4-E": the extended suffix is the emulator's own doing and
		// is not part of what -model accepts.
		if cut := strings.Index(number, "-"); cut >= 0 {
			number = number[:cut]
		}
	}
	if family != "3278" && family != "3279" {
		return "", fmt.Errorf("unknown terminal model %q: the family must be 3278 (monochrome) or 3279 (colour)", model)
	}

	n, err := strconv.Atoi(strings.TrimSpace(number))
	if err != nil || n < 2 || n > 5 {
		return "", fmt.Errorf("unknown terminal model %q: model numbers are 2 (24x80), 3 (32x80), 4 (43x80) and 5 (27x132)", model)
	}

	return fmt.Sprintf("%s-%d", family, n), nil
}

// NormalizeOversize checks an oversize geometry, which the emulator takes as
// "<cols>x<rows>".
func NormalizeOversize(oversize string) (string, error) {
	oversize = strings.TrimSpace(oversize)
	if oversize == "" {
		return "", nil
	}
	lower := strings.ToLower(oversize)
	parts := strings.SplitN(lower, "x", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("oversize %q must be written as <cols>x<rows>, e.g. 132x50", oversize)
	}
	cols, errC := strconv.Atoi(strings.TrimSpace(parts[0]))
	rows, errR := strconv.Atoi(strings.TrimSpace(parts[1]))
	if errC != nil || errR != nil || cols <= 0 || rows <= 0 {
		return "", fmt.Errorf("oversize %q must be written as <cols>x<rows>, e.g. 132x50", oversize)
	}
	return fmt.Sprintf("%dx%d", cols, rows), nil
}

// hostTarget renders the positional host argument, which carries rather more
// than a host and a port:
//
//	[L:][LUname@]host:port
//
// "L:" asks for TLS, and an LU name in front of the host asks the host to
// bind the session to that logical unit. Neither was reachable before, which
// ruled out every host that requires TLS and every one that routes by LU.
func (e *Emulator) hostTarget() string {
	target := e.hostname()
	if lu := strings.TrimSpace(e.LUName); lu != "" {
		target = lu + "@" + target
	}
	if e.TLS {
		target = "L:" + target
	}
	return target
}

// buildEmulatorArgs assembles the command-line arguments for launching the
// embedded x3270/s3270/wc3270 process. The argument order mirrors the
// historical invocation so existing behavior is unchanged; the host EBCDIC
// code page (-codepage) is inserted only when Emulator.CodePage is set, and
// the host target is always the final positional argument.
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

	// A screen larger than the model defines. Only used by hosts that
	// support the geometry; the rest carry on at the model's own size.
	if oversize, err := NormalizeOversize(e.Oversize); err == nil && oversize != "" {
		args = append(args, "-oversize", oversize)
	}

	// Certificate validation is on unless the caller turns it off.
	if e.TLS && e.InsecureSkipVerify {
		args = append(args, "-noverifycert")
	}

	args = append(args, e.hostTarget())
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

	// The device type to negotiate. Rejected here rather than left to the
	// emulator, which would substitute a different model and connect anyway.
	modelType, err := NormalizeModel(e.Model)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidConfiguration, err)
	}
	if oversize := strings.TrimSpace(e.Oversize); oversize != "" {
		if _, err := NormalizeOversize(oversize); err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidConfiguration, err)
		}
	}

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

	handle := &procHandle{proc: cmd.Process, done: make(chan struct{})}
	e.procMu.Lock()
	e.proc = handle
	e.procMu.Unlock()

	var startupErr atomic.Pointer[string]
	go func() {
		defer close(handle.done)
		defer stderr.Close()
		errMsg, _ := ioutil.ReadAll(stderr)
		if len(errMsg) > 0 {
			text := strings.TrimSpace(string(errMsg))
			startupErr.Store(&text)
			if Verbose {
				log.Printf("3270 stderr: %s", text)
			}
		}
		if err := cmd.Wait(); err != nil && Verbose {
			log.Printf("Error waiting for 3270 instance: %v", err)
		}
	}()

	deadline := time.Now().Add(startupConnectTimeout)
	connected := false
	exited := false
	attempt := 0
	for time.Now().Before(deadline) {
		if ShutdownRequested() {
			return fmt.Errorf("shutdown requested")
		}
		// A refused connection, a name that does not resolve or a
		// certificate the emulator will not accept all end the same way:
		// it prints the reason and exits. Waiting out the full startup
		// timeout for a process that is already gone turns a one-second
		// answer into twenty, and ten connect attempts into two hundred
		// seconds of nothing happening.
		select {
		case <-handle.done:
			exited = true
		default:
		}
		if exited {
			break
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
		e.procMu.Lock()
		e.proc = nil
		e.procMu.Unlock()
		// Whatever the emulator complained about is the useful half of this
		// error — a refused connection, a certificate it would not accept, a
		// name that does not resolve. It used to be visible only in verbose
		// mode, so the reported failure was a bare timeout.
		if detail := startupErr.Load(); detail != nil && *detail != "" {
			return fmt.Errorf("could not connect to %s: %s", e.hostTarget(), firstLine(*detail))
		}
		if exited {
			return fmt.Errorf("could not connect to %s: the emulator exited without reporting a reason", e.hostTarget())
		}
		return fmt.Errorf("timed out waiting for emulator to connect to %s after %.1fs", e.hostTarget(), startupConnectTimeout.Seconds())
	}

	return nil
}

// firstLine keeps an error report to the line that says what happened, and
// to characters that can safely be printed: the emulator's own diagnostics
// have been seen to carry raw bytes after the message.
func firstLine(s string) string {
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		s = s[:idx]
	}
	var b strings.Builder
	for _, r := range s {
		if r == '\t' || (r >= 0x20 && r != 0x7f) {
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
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
// hostname renders the host and port the way the emulator's command line
// expects them.
//
// net.JoinHostPort rather than "%s:%d" because of IPv6: a literal address
// has colons of its own, and "::1:3271" is not a host and a port, it is a
// syntax error the emulator refuses before it tries to connect. Bracketed,
// "[::1]:3271", it is accepted — including in front of an LU name and behind
// a TLS prefix. A name or an IPv4 address is unchanged by this.
func (e *Emulator) hostname() string {
	return net.JoinHostPort(e.Host, strconv.Itoa(e.Port))
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
	out, err := e.execCommandOutput("Ascii()")
	if err != nil {
		return "", err
	}
	return NormalizeDataLines(out), nil
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
	var lastErr error
	for retries := 0; retries < maxRetries; retries++ {
		output, err := e.execCommandOutput("Ascii()")
		if err == nil {
			// The screen, and only the screen. What the emulator sends is
			// the screen behind a "data: " prefix on every line with its
			// status line on the end, and every capture this tool has ever
			// written carried both — an 80-column screen shifted six
			// columns right, with a line of protocol state below it.
			screen := NormalizeDataLines(output)

			var content string
			if apiMode {
				// In API mode, just use plain ASCII output
				content = screen
			} else {
				// In non-API mode, format the output as output.
				// Written in one call: concurrent workers append to the same
				// file, and a screen split across two writes would interleave
				// with another worker's.
				//
				// Escaped, because a host screen is not HTML: a screen
				// holding "<" or "&" used to be rendered as markup, and a
				// capture served by the console is not a place to be
				// pasting a host's characters in unescaped.
				content = fmt.Sprintf("<pre%s>%s</pre>\n", e.captureAttrs(time.Now()), escapeText(screen))
			}

			if err := writeScreenGrab(filePath, content); err != nil {
				return err
			}
			return nil
		}
		lastErr = err
		if isDeterministicFailure(err) {
			return fmt.Errorf("capturing screen: %w", err)
		}
		time.Sleep(retryDelay)
	}

	return fmt.Errorf("maximum capture retries reached: %w", lastErr)
}

// escapeText makes host screen text safe to sit inside an HTML element.
func escapeText(value string) string {
	replacer := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return replacer.Replace(value)
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

// getOrCreateBinaryFile unpacks the embedded emulator and returns its path.
//
// Two things about where it goes and what it is called are deliberate.
//
// It goes in a directory of this user's own, created 0700, rather than
// straight into the shared temporary directory. The old path was a fixed
// /tmp/s3270: on any machine with more than one user, whoever created that
// name first decided what this tool executed.
//
// It is named after a hash of the contents, and an existing file is only
// reused if its contents still hash to that name. Upgrading 3270Connect used
// to leave the previous emulator in place for as long as the file survived,
// so a new release kept driving sessions with the old binary.
func getOrCreateBinaryFile(binaryName string) (string, error) {
	switch binaryName {
	case "x3270", "s3270", "wc3270":
	default:
		return "", fmt.Errorf("unknown binary name: %s", binaryName)
	}

	assetPath := filepath.Join("binaries", getOSDirectory(), binaryName+getExecutableExtension())
	binaryData, err := binaries.Asset(assetPath)
	if err != nil {
		return "", fmt.Errorf("error reading embedded binary data: %v", err)
	}
	sum := sha256.Sum256(binaryData)
	digest := hex.EncodeToString(sum[:8])

	dir, err := emulatorCacheDir()
	if err != nil {
		return "", err
	}
	filePath := filepath.Join(dir, fmt.Sprintf("%s-%s%s", binaryName, digest, getExecutableExtension()))

	if existing, err := os.ReadFile(filePath); err == nil {
		if sha256.Sum256(existing) == sum {
			return filePath, nil
		}
	}

	// Written under a unique name and moved into place, so a second process
	// unpacking the same emulator at the same time cannot be executing a
	// half-written file.
	tmp, err := os.CreateTemp(dir, binaryName+"-*.partial")
	if err != nil {
		return "", fmt.Errorf("error creating emulator file: %v", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(binaryData); err != nil {
		tmp.Close()
		return "", fmt.Errorf("error writing binary data to a file: %v", err)
	}
	if err := tmp.Chmod(0o700); err != nil {
		tmp.Close()
		return "", fmt.Errorf("error making the emulator executable: %v", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("error writing binary data to a file: %v", err)
	}
	if err := os.Rename(tmpName, filePath); err != nil {
		// Another process may have won the race; if what is there now is
		// the right binary, that is exactly as good.
		if existing, readErr := os.ReadFile(filePath); readErr == nil && sha256.Sum256(existing) == sum {
			return filePath, nil
		}
		return "", fmt.Errorf("error installing the emulator: %v", err)
	}

	return filePath, nil
}

// emulatorCacheDir is where the unpacked emulator lives: this user's cache
// directory, or a private directory under the temporary one if there is no
// cache directory to be had.
func emulatorCacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil || strings.TrimSpace(base) == "" {
		base = os.TempDir()
	}
	dir := filepath.Join(base, "3270Connect", "emulator")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("error preparing the emulator directory: %v", err)
	}
	return dir, nil
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
