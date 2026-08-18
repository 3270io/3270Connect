package connect3270

// Compatibility tests.
//
// Every bug this file guards against was found the same way: by running the
// embedded emulator against a real TN3270 host and comparing what it does
// with what this package assumed it did. Unit tests could not have found any
// of them, because the assumptions were wrong in both the code and the test.
//
// So these tests do that same thing on every run. They start a 3270 host in
// process, unpack and launch the real s3270, drive a session through it, and
// check both ends: what the emulator reports, and what the host actually
// received. That makes them the check that survives the things unit tests do
// not notice — a new embedded emulator binary, a new x3270 release renaming
// an action or a Query keyword, a different platform's build of it.
//
// They are skipped under -short, and skipped with a reason if the embedded
// emulator cannot be unpacked for this platform.

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/racingmars/go3270"

	"github.com/3270io/3270Connect/internal/profiler"
)

// The fixture screen, in the emulator's 1-based coordinates:
//
//	row  1        the title, protected
//	row  5 col 21 NAME, writable, 20 characters, stopped at column 41
//	row  6 col 21 NEXT, writable, 20 characters — what an overflowing
//	              NAME would spill into
//	row 23        a protected footer
const (
	fixtureNameRow    = 5
	fixtureNameCol    = 21
	fixtureFieldWidth = 20
	fixtureNextRow    = 6
	fixtureTitle      = "COMPAT FIXTURE"
)

var fixtureScreen = go3270.Screen{
	{Row: 0, Col: 27, Intense: true, Content: fixtureTitle},
	{Row: 4, Col: 0, Content: "NAME . . ."},
	{Row: 4, Col: 19, Name: "name", Write: true},
	{Row: 4, Col: 40, Autoskip: true},
	{Row: 5, Col: 0, Content: "NEXT . . ."},
	{Row: 5, Col: 19, Name: "next", Write: true},
	{Row: 5, Col: 40, Autoskip: true},
	{Row: 22, Col: 0, Content: "PF3 EXIT"},
}

// compatHost is a TN3270 host that shows one screen and remembers what was
// submitted to it. Remembering is the point: an emulator that reports success
// while typing the wrong thing onto the screen is exactly the failure this
// package has had, so the assertions are made against what the host received
// rather than against what the emulator said.
type compatHost struct {
	ln net.Listener

	mu        sync.Mutex
	submitted []map[string]string
	clients   []net.Conn
}

func startCompatHost(t *testing.T) *compatHost {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("starting the fixture host: %v", err)
	}
	h := &compatHost{ln: ln}
	go h.serve()
	t.Cleanup(func() { ln.Close() })
	return h
}

func (h *compatHost) port() int {
	return h.ln.Addr().(*net.TCPAddr).Port
}

func (h *compatHost) serve() {
	for {
		conn, err := h.ln.Accept()
		if err != nil {
			return
		}
		go h.handle(conn)
	}
}

func (h *compatHost) handle(conn net.Conn) {
	defer conn.Close()
	h.mu.Lock()
	h.clients = append(h.clients, conn)
	h.mu.Unlock()
	go3270.NegotiateTelnet(conn)

	values := map[string]string{}
	for {
		// Cursor on the first writable field, and the screen is redrawn
		// whatever the client sent, so no key under test can end the
		// session out from under a later assertion.
		response, err := go3270.ShowScreen(fixtureScreen, values, 4, 20, conn)
		if err != nil {
			return
		}
		if len(response.Values) > 0 {
			h.mu.Lock()
			h.submitted = append(h.submitted, response.Values)
			h.mu.Unlock()
			values = response.Values
		}
	}
}

// dropClients hangs up on every connected session, which is what a host
// going away looks like from the emulator's side: the emulator is still
// running, and the session it is holding is not.
func (h *compatHost) dropClients() {
	h.mu.Lock()
	clients := h.clients
	h.clients = nil
	h.mu.Unlock()
	for _, c := range clients {
		c.Close()
	}
}

// lastSubmitted returns the values from the most recent submission, waiting
// briefly for one to arrive.
func (h *compatHost) lastSubmitted(t *testing.T) map[string]string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		h.mu.Lock()
		n := len(h.submitted)
		var last map[string]string
		if n > 0 {
			last = h.submitted[n-1]
		}
		h.mu.Unlock()
		if last != nil {
			return last
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("the host received nothing to submit")
	return nil
}

// newCompatSession starts a host, connects a real emulator to it and returns
// both, cleaning up after the test.
func newCompatSession(t *testing.T, configure func(*Emulator)) (*compatHost, *Emulator) {
	t.Helper()
	requireEmulator(t)

	prevHeadless := Headless
	Headless = true
	t.Cleanup(func() { Headless = prevHeadless })

	host := startCompatHost(t)
	e := NewEmulator("127.0.0.1", host.port(), strconv.Itoa(freePort(t)))
	if configure != nil {
		configure(e)
	}
	t.Cleanup(func() { _ = e.Disconnect() })

	if err := e.Connect(); err != nil {
		t.Fatalf("connecting to the fixture host: %v", err)
	}
	return host, e
}

// requireEmulator skips the test when the embedded emulator cannot be run
// here, and says why rather than passing quietly.
func requireEmulator(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("compatibility tests drive a real emulator process; skipped under -short")
	}
	binaryFileMutex.Lock()
	s3270BinaryPath = ""
	binaryFileMutex.Unlock()
	path, err := getOrCreateBinaryFile("s3270")
	if err != nil {
		t.Skipf("no embedded s3270 for this platform: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Skipf("embedded s3270 could not be unpacked: %v", err)
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("finding a free port: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// TestCompatSessionReportsItsState covers the reading that was wrong in both
// directions: a disconnected session reporting itself connected, and the
// negotiated geometry being unavailable without asking for it.
func TestCompatSessionReportsItsState(t *testing.T) {
	_, e := newCompatSession(t, nil)

	if !e.IsConnected() {
		t.Fatalf("a connected session should report itself connected")
	}

	s := e.Status()
	if !s.Valid {
		t.Fatalf("no status was recorded from the replies")
	}
	if !s.Connected {
		t.Errorf("status should show the session connected: %+v", s)
	}
	if !s.Formatted {
		t.Errorf("the fixture screen is formatted: %+v", s)
	}
	if s.Rows != 24 || s.Cols != 80 {
		t.Errorf("a model 2 session is 24x80, got %dx%d", s.Rows, s.Cols)
	}
	if s.Model != 2 {
		t.Errorf("the default model is 2, got %d", s.Model)
	}

	if err := e.Disconnect(); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}
	if e.IsConnected() {
		t.Errorf("a disconnected session should not report itself connected")
	}
}

// TestCompatDroppedSessionIsReported is the regression test for the reading
// that mattered most: Query(ConnectionState) answers "not-connected", and
// treating any answer as a yes meant a session that had gone away reported
// itself up. The emulator is still running here — only the host has gone —
// which is the case a check on the emulator process cannot see.
func TestCompatDroppedSessionIsReported(t *testing.T) {
	host, e := newCompatSession(t, nil)

	if !e.IsConnected() {
		t.Fatalf("the session should start connected")
	}

	host.ln.Close()
	host.dropClients()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if !e.IsConnected() {
			// And the emulator itself is still there to have answered.
			if state, err := e.Query("ConnectionState"); err != nil {
				t.Fatalf("the emulator should still be answering: %v", err)
			} else if !strings.Contains(NormalizeDataLines(state), "not-connected") {
				t.Errorf("expected not-connected, got %q", NormalizeDataLines(state))
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Errorf("a dropped session still reports itself connected: %s", e.Status().Raw)
}

// TestCompatModelIsNegotiated checks that asking for a larger model produces
// one. The device type was hard-coded, so this could not have been true.
func TestCompatModelIsNegotiated(t *testing.T) {
	_, e := newCompatSession(t, func(e *Emulator) { e.Model = "4" })

	if got := e.Status().Model; got != 4 {
		t.Errorf("asked for model 4, negotiated %d", got)
	}
	// The fixture writes the primary screen, so the session is still 24
	// rows; the model shows in the size it *could* use.
	max, err := e.Query("ScreenSizeMax")
	if err != nil {
		t.Fatalf("Query(ScreenSizeMax): %v", err)
	}
	if !strings.Contains(NormalizeDataLines(max), "43") {
		t.Errorf("a model 4 has a 43-row alternate screen, got %q", NormalizeDataLines(max))
	}
}

// TestCompatTextReachesTheHostIntact is the regression test for the quiet
// data corruption: an unquoted comma is an argument separator, so the host
// used to receive SMITHJOHN when the workflow said SMITH,JOHN.
func TestCompatTextReachesTheHostIntact(t *testing.T) {
	host, e := newCompatSession(t, nil)

	const value = `SMITH,JOHN "X"`
	if err := e.FillString(fixtureNameRow, fixtureNameCol, value); err != nil {
		t.Fatalf("FillString: %v", err)
	}
	if err := e.Press(Enter); err != nil {
		t.Fatalf("Enter: %v", err)
	}

	got := strings.TrimSpace(host.lastSubmitted(t)["name"])
	if got != value {
		t.Errorf("the host received %q, the workflow said %q", got, value)
	}
}

// TestCompatOverflowIsRefused covers the other quiet corruption: the
// emulator does not stop typing at the end of a field, so a value longer
// than the field runs on into the next one — on a logon screen, usually the
// password.
func TestCompatOverflowIsRefused(t *testing.T) {
	host, e := newCompatSession(t, nil)

	long := strings.Repeat("X", fixtureFieldWidth+10)
	err := e.FillString(fixtureNameRow, fixtureNameCol, long)
	if err == nil {
		t.Fatalf("a %d-character value in a %d-character field should be refused", len(long), fixtureFieldWidth)
	}
	if !strings.Contains(err.Error(), "overflow") {
		t.Errorf("the error should say what would happen: %v", err)
	}

	// And nothing was typed, so the field below is untouched.
	if err := e.Press(Enter); err != nil {
		t.Fatalf("Enter: %v", err)
	}
	if next := strings.TrimSpace(host.lastSubmitted(t)["next"]); next != "" {
		t.Errorf("the field below should be empty, the host received %q", next)
	}
}

// TestCompatValueThatFitsIsAccepted keeps the overflow check from being a
// blanket refusal: a value exactly as long as the field is fine.
func TestCompatValueThatFitsIsAccepted(t *testing.T) {
	host, e := newCompatSession(t, nil)

	exact := strings.Repeat("Y", fixtureFieldWidth)
	if err := e.FillString(fixtureNameRow, fixtureNameCol, exact); err != nil {
		t.Fatalf("a value exactly the width of the field should be accepted: %v", err)
	}
	if err := e.Press(Enter); err != nil {
		t.Fatalf("Enter: %v", err)
	}
	if got := strings.TrimSpace(host.lastSubmitted(t)["name"]); got != exact {
		t.Errorf("the host received %q, want %q", got, exact)
	}
}

// TestCompatProtectedFieldIsRefused covers the cascade: typing into a
// protected field locks the keyboard with an operator error, and the lock
// does not clear itself, so every step after it fails too and the report
// names the wrong one.
func TestCompatProtectedFieldIsRefused(t *testing.T) {
	_, e := newCompatSession(t, nil)

	// Row 1 is the title: protected.
	err := e.FillString(1, 30, "X")
	if err == nil {
		t.Fatalf("typing into a protected field should be refused")
	}
	if !strings.Contains(err.Error(), "protected") {
		t.Errorf("the error should say the field is protected: %v", err)
	}

	// The session is still usable, which is the part that matters.
	if err := e.FillString(fixtureNameRow, fixtureNameCol, "STILL WORKS"); err != nil {
		t.Errorf("the session should still be usable after a refused write: %v", err)
	}
}

// TestCompatOffScreenIsRefused covers the silent clamp: MoveCursor past the
// last row is answered "ok" with the cursor moved somewhere else entirely.
func TestCompatOffScreenIsRefused(t *testing.T) {
	_, e := newCompatSession(t, nil)

	if err := e.FillString(30, 1, "X"); err == nil {
		t.Errorf("row 30 of a 24-row screen should be refused")
	} else if !strings.Contains(err.Error(), "24x80") {
		t.Errorf("the error should name the screen size: %v", err)
	}

	if _, err := e.GetValue(30, 1, 5); err == nil {
		t.Errorf("reading row 30 of a 24-row screen should be refused")
	}
}

// TestCompatReadAcrossARowBoundary covers a read that runs past the end of a
// row: the emulator answers with one line per row, and keeping only the
// first silently shortened the value a CheckValue then compared.
func TestCompatReadAcrossARowBoundary(t *testing.T) {
	_, e := newCompatSession(t, nil)

	// Twenty characters from column 70 of row 5: ten before the row ends
	// and ten on the row after it.
	got, err := e.GetValue(5, 70, 20)
	if err != nil {
		t.Fatalf("GetValue: %v", err)
	}
	// The fixture puts "NEXT . . ." at the start of the following row, so
	// the second half of the read has to be in there.
	if !strings.Contains(got, "NEXT") {
		t.Errorf("a read spanning a row boundary should return both halves, got %q", got)
	}
}

// TestCompatCaptureHoldsTheScreenAlone covers what every capture this tool
// wrote used to contain: the transport's "data:" prefix on every line and
// the emulator's status line pasted underneath the screen.
func TestCompatCaptureHoldsTheScreenAlone(t *testing.T) {
	_, e := newCompatSession(t, nil)

	path := filepath.Join(t.TempDir(), "capture.html")
	if err := e.AsciiScreenGrab(path, false); err != nil {
		t.Fatalf("AsciiScreenGrab: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the capture: %v", err)
	}
	got := string(body)

	if !strings.Contains(got, fixtureTitle) {
		t.Errorf("the capture should hold the screen: %q", got)
	}
	if strings.Contains(got, "data:") {
		t.Errorf("the capture carries the transport prefix: %q", got)
	}
	if strings.Contains(got, "C(127.0.0.1)") {
		t.Errorf("the capture carries the emulator status line: %q", got)
	}

	// Plain text in API mode, and 80 columns wide because that is what the
	// screen is.
	plain := filepath.Join(t.TempDir(), "capture.txt")
	if err := e.AsciiScreenGrab(plain, true); err != nil {
		t.Fatalf("AsciiScreenGrab (api): %v", err)
	}
	text, _ := os.ReadFile(plain)
	for i, line := range strings.Split(strings.TrimRight(string(text), "\n"), "\n") {
		if len([]rune(line)) != 80 {
			t.Errorf("captured line %d is %d columns, want 80: %q", i+1, len([]rune(line)), line)
			break
		}
	}
}

// TestCompatEveryKeyNamesARealAction is the guard against the class of bug
// where this package asks the emulator for something it does not have.
//
// The emulator lists its own actions, so the keys can be checked against it
// rather than against anyone's memory of the manual. A new embedded binary
// that renamed or dropped one fails here instead of on a host.
func TestCompatEveryKeyNamesARealAction(t *testing.T) {
	_, e := newCompatSession(t, nil)

	listed, err := e.Query("Actions")
	if err != nil {
		t.Fatalf("Query(Actions): %v", err)
	}
	actions := map[string]bool{}
	for _, name := range strings.Fields(NormalizeDataLines(listed)) {
		actions[strings.ToLower(strings.TrimSuffix(name, "()"))] = true
	}
	if len(actions) == 0 {
		t.Fatalf("the emulator listed no actions: %q", listed)
	}

	for _, key := range allKeys() {
		// "PF(3)" and "PA(1)" name the PF and PA actions.
		name := key
		if i := strings.IndexByte(name, '('); i >= 0 {
			name = name[:i]
		}
		if !actions[strings.ToLower(name)] {
			t.Errorf("key %q names action %q, which this emulator does not have", key, name)
		}
	}
}

// TestCompatKeysAreAccepted presses the keys that do not submit the screen,
// which is the other half of the same check: named correctly and accepted.
func TestCompatKeysAreAccepted(t *testing.T) {
	_, e := newCompatSession(t, nil)

	for _, key := range []string{
		Tab, BackTab, Home, Newline, EraseEOF, EraseInput, Reset,
		Up, Down, Left, Right, Insert, Delete, BackSpace,
	} {
		if err := e.Press(key); err != nil {
			t.Errorf("Press(%s): %v", key, err)
		}
	}
}

// TestCompatSubmittingKeysReachTheHost checks the keys that carry an AID,
// including the attention keys that had no way of being sent at all.
func TestCompatSubmittingKeysReachTheHost(t *testing.T) {
	_, e := newCompatSession(t, nil)

	for _, key := range []string{Enter, F1, F3, F24, PA1, PA2, PA3, Clear} {
		if err := e.Press(key); err != nil {
			t.Errorf("Press(%s): %v", key, err)
			continue
		}
		// The host redraws on every AID, so the session must still be up.
		if err := e.WaitForField(2*time.Second, 3); err != nil {
			t.Errorf("after %s the screen did not come back: %v", key, err)
		}
		if !e.IsConnected() {
			t.Fatalf("the session dropped after %s", key)
		}
	}
}

// TestCompatProfilerQueriesAreAnswered guards the bug the host profiler
// shipped with: it probed two Query keywords x3270 has never had, so every
// profile listed them as unanswered — in the field a reader uses to spot one
// host behaving differently from another.
//
// A query the emulator does not recognise is indistinguishable, in a
// profile, from a host declining to answer. Here they are distinguishable,
// because the emulator says so.
func TestCompatProfilerQueriesAreAnswered(t *testing.T) {
	_, e := newCompatSession(t, nil)

	for _, name := range profiler.ProbeQueries() {
		resp, err := e.Query(name)
		if err != nil {
			t.Errorf("Query(%s) failed: %v", name, err)
			continue
		}
		if strings.Contains(strings.ToLower(resp), "unknown parameter") {
			t.Errorf("Query(%s) is not a query this emulator has: %q", name, strings.TrimSpace(resp))
		}
	}
}

// TestCompatWaitForFieldSucceedsOnAFormattedScreen covers the wait every
// step depends on, against a screen that really does have an input field.
func TestCompatWaitForFieldSucceedsOnAFormattedScreen(t *testing.T) {
	_, e := newCompatSession(t, nil)

	if err := e.WaitForField(3*time.Second, 3); err != nil {
		t.Errorf("WaitForField on a formatted screen with input fields: %v", err)
	}
	if e.Status().KeyboardLocked {
		t.Errorf("the keyboard should be unlocked after WaitForField: %s", e.Status().Raw)
	}
}

// TestCompatDisconnectReleasesTheScriptPort covers the leak: an emulator
// that is not stopped keeps its script port for the rest of a load test, and
// a concurrent run eventually exhausts the range.
func TestCompatDisconnectReleasesTheScriptPort(t *testing.T) {
	_, e := newCompatSession(t, nil)
	port := e.ScriptPort

	if err := e.Disconnect(); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}

	deadline := time.Now().Add(processExitGrace + 3*time.Second)
	for time.Now().Before(deadline) {
		ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", port))
		if err == nil {
			ln.Close()
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Errorf("script port %s was still held %s after Disconnect", port, processExitGrace)
}

// TestCompatUnreachableHostFailsQuickly covers the wait that used to be
// spent on an emulator that had already exited: the failure took ten
// attempts of twenty seconds each to report something the emulator said in
// the first second.
func TestCompatUnreachableHostFailsQuickly(t *testing.T) {
	requireEmulator(t)
	prevHeadless := Headless
	Headless = true
	t.Cleanup(func() { Headless = prevHeadless })

	// A port nothing is listening on.
	dead := freePort(t)
	e := NewEmulator("127.0.0.1", dead, strconv.Itoa(freePort(t)))
	t.Cleanup(func() { _ = e.Disconnect() })

	start := time.Now()
	err := e.Connect()
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("connecting to a closed port should fail")
	}
	if elapsed > 60*time.Second {
		t.Errorf("reporting an unreachable host took %s", elapsed)
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("%d", dead)) {
		t.Errorf("the error should name the host it could not reach: %v", err)
	}
}

// allKeys is every key this package can send, which is what
// TestCompatEveryKeyNamesARealAction checks against the emulator.
func allKeys() []string {
	keys := []string{
		Enter, Tab, Reset, PA1, PA2, PA3, Clear,
		BackTab, Home, EraseEOF, EraseInput, Newline,
		Up, Down, Left, Right, BackSpace, Delete, Insert, Dup, FieldMark,
		SysReq, Attn,
	}
	pf := []string{
		F1, F2, F3, F4, F5, F6, F7, F8, F9, F10, F11, F12,
		F13, F14, F15, F16, F17, F18, F19, F20, F21, F22, F23, F24,
	}
	return append(keys, pf...)
}
