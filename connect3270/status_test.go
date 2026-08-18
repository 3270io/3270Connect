package connect3270

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"

	"strings"
	"sync"
	"testing"
)

func TestParseStatusReadsTheLine(t *testing.T) {
	// A real line, copied from an s3270 session with the sample host.
	s := ParseStatus("U F U C(127.0.0.1) I 2 24 80 4 20 0x0 0.000")
	if !s.Valid {
		t.Fatalf("expected a valid status, got %+v", s)
	}
	if s.KeyboardLocked || s.KeyboardError {
		t.Errorf("keyboard should be unlocked: %+v", s)
	}
	if !s.Formatted {
		t.Errorf("screen should be formatted: %+v", s)
	}
	if !s.Connected || s.Host != "127.0.0.1" {
		t.Errorf("connection: %+v", s)
	}
	if s.Mode != "I" {
		t.Errorf("mode: %q", s.Mode)
	}
	if s.Model != 2 || s.Rows != 24 || s.Cols != 80 {
		t.Errorf("geometry: %+v", s)
	}
	// The wire is 0-based; workflow coordinates are 1-based.
	if s.CursorRow != 5 || s.CursorCol != 21 {
		t.Errorf("cursor: row %d column %d, want 5/21", s.CursorRow, s.CursorCol)
	}
}

func TestParseStatusDisconnectedAndLocked(t *testing.T) {
	s := ParseStatus("L U U N N 4 43 80 0 0 0x0 0.000")
	if !s.Valid {
		t.Fatalf("expected a valid status")
	}
	if !s.KeyboardLocked || s.KeyboardError {
		t.Errorf("expected a wait lock, not an error lock: %+v", s)
	}
	if s.Connected {
		t.Errorf("N means not connected: %+v", s)
	}
	if s.Rows != 43 || s.Model != 4 {
		t.Errorf("a model 4 alternate screen: %+v", s)
	}

	if e := ParseStatus("E F P C(mvs) I 2 24 80 0 0 0x0 0.000"); !e.KeyboardError || !e.KeyboardLocked {
		t.Errorf("E is the error lock a Reset clears: %+v", e)
	}
}

func TestParseStatusRejectsAnythingElse(t *testing.T) {
	for _, line := range []string{"", "ok", "data: hello", "U F U"} {
		if s := ParseStatus(line); s.Valid {
			t.Errorf("ParseStatus(%q) claimed to understand it: %+v", line, s)
		}
	}
}

func TestQuoteActionArgKeepsTheValueIntact(t *testing.T) {
	cases := []struct{ in, want string }{
		// The one that mattered: an unquoted comma is an argument
		// separator, so "SMITH,JOHN" was typed as "SMITHJOHN".
		{"SMITH,JOHN", `"SMITH,JOHN"`},
		{"AB)CD", `"AB)CD"`},
		{`back\slash`, `"back\\slash"`},
		{`say "hi"`, `"say \"hi\""`},
		{"plain", `"plain"`},
		{"", `""`},
		// A bare newline would end the command and turn the rest of the
		// value into a second action.
		{"two\nlines", `"two\nlines"`},
		{"tab\there", `"tab\there"`},
	}
	for _, tc := range cases {
		if got := quoteActionArg(tc.in); got != tc.want {
			t.Errorf("quoteActionArg(%q) = %s, want %s", tc.in, got, tc.want)
		}
	}
}

func TestConnectionStateIsConnected(t *testing.T) {
	connected := []string{
		"connected-3270", "connected-tn3270e", "connected-nvt",
		"connected-unbound", "CONNECTED-3270",
	}
	for _, s := range connected {
		if !connectionStateIsConnected(s) {
			t.Errorf("%q should count as connected", s)
		}
	}
	// "not-connected" is the answer that used to be read as a yes.
	notConnected := []string{
		"not-connected", "", "resolving", "tcp-pending", "tls-pending",
		"telnet-pending", "reconnecting",
	}
	for _, s := range notConnected {
		if connectionStateIsConnected(s) {
			t.Errorf("%q should not count as connected", s)
		}
	}
}

func TestNormalizeModel(t *testing.T) {
	valid := map[string]string{
		"":         DefaultModel,
		"2":        "3279-2",
		"4":        "3279-4",
		"3278-4":   "3278-4",
		"3279-5":   "3279-5",
		"3279-4-E": "3279-4",
		"  3 ":     "3279-3",
	}
	for in, want := range valid {
		got, err := NormalizeModel(in)
		if err != nil {
			t.Errorf("NormalizeModel(%q): %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("NormalizeModel(%q) = %q, want %q", in, got, want)
		}
	}
	// The emulator answers these by printing a complaint and negotiating a
	// different model, so they have to be caught here.
	for _, in := range []string{"6", "1", "3270-2", "3279", "model2", "-"} {
		if got, err := NormalizeModel(in); err == nil {
			t.Errorf("NormalizeModel(%q) = %q, expected an error", in, got)
		}
	}
}

func TestNormalizeOversize(t *testing.T) {
	got, err := NormalizeOversize(" 132X50 ")
	if err != nil || got != "132x50" {
		t.Errorf("NormalizeOversize = %q, %v", got, err)
	}
	if got, err := NormalizeOversize(""); err != nil || got != "" {
		t.Errorf("empty oversize should stay empty: %q, %v", got, err)
	}
	for _, in := range []string{"132", "132x", "0x50", "wide", "132x-1"} {
		if _, err := NormalizeOversize(in); err == nil {
			t.Errorf("NormalizeOversize(%q) should have failed", in)
		}
	}
}

func TestHostTargetCarriesTLSAndLU(t *testing.T) {
	e := &Emulator{Host: "mvs.example.com", Port: 992}
	if got := e.hostTarget(); got != "mvs.example.com:992" {
		t.Errorf("plain: %q", got)
	}
	e.LUName = "LU01"
	if got := e.hostTarget(); got != "LU01@mvs.example.com:992" {
		t.Errorf("with LU: %q", got)
	}
	e.TLS = true
	if got := e.hostTarget(); got != "L:LU01@mvs.example.com:992" {
		t.Errorf("with TLS: %q", got)
	}
}

func TestBuildEmulatorArgsCarriesModelAndTLS(t *testing.T) {
	prevHeadless := Headless
	Headless = true
	defer func() { Headless = prevHeadless }()

	e := &Emulator{
		Host: "mvs.example.com", Port: 992, ScriptPort: "5000",
		Oversize: "132x50", TLS: true, InsecureSkipVerify: true,
	}
	args := e.buildEmulatorArgs("3279-5")

	if i := argIndex(args, "-model"); i < 0 || args[i+1] != "3279-5" {
		t.Errorf("model not passed through: %v", args)
	}
	if i := argIndex(args, "-oversize"); i < 0 || args[i+1] != "132x50" {
		t.Errorf("oversize not passed through: %v", args)
	}
	if argIndex(args, "-noverifycert") < 0 {
		t.Errorf("InsecureSkipVerify should disable certificate validation: %v", args)
	}
	if got := args[len(args)-1]; got != "L:mvs.example.com:992" {
		t.Errorf("host target should be last and TLS-prefixed, got %q", got)
	}
}

func TestBuildEmulatorArgsLeavesCertificateVerificationOn(t *testing.T) {
	e := &Emulator{Host: "mvs.example.com", Port: 992, ScriptPort: "5000", TLS: true}
	if argIndex(e.buildEmulatorArgs("3279-2"), "-noverifycert") >= 0 {
		t.Errorf("certificate validation must stay on unless asked otherwise")
	}
}

// fakeEmulator is a stand-in for s3270 speaking its scripting protocol over a
// loopback socket, so the transport can be tested without a host.
type fakeEmulator struct {
	t        *testing.T
	ln       net.Listener
	status   string
	mu       sync.Mutex
	commands []string
	replies  map[string][]string
	fail     map[string]bool
}

func newFakeEmulator(t *testing.T) *fakeEmulator {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	f := &fakeEmulator{
		t:       t,
		ln:      ln,
		status:  "U F U C(mvs.example.com) I 2 24 80 4 20 0x0 0.000",
		replies: map[string][]string{},
		fail:    map[string]bool{},
	}
	go f.serve()
	t.Cleanup(func() { ln.Close() })
	return f
}

func (f *fakeEmulator) port() string {
	_, port, _ := net.SplitHostPort(f.ln.Addr().String())
	return port
}

func (f *fakeEmulator) emulator() *Emulator {
	return &Emulator{Host: "mvs.example.com", Port: 23, ScriptPort: f.port()}
}

func (f *fakeEmulator) serve() {
	for {
		conn, err := f.ln.Accept()
		if err != nil {
			return
		}
		go func() {
			defer conn.Close()
			reader := bufio.NewReader(conn)
			for {
				line, err := reader.ReadString('\n')
				if err != nil {
					return
				}
				command := strings.TrimSpace(line)
				f.mu.Lock()
				f.commands = append(f.commands, command)
				data := f.replies[commandName(command)]
				failed := f.fail[commandName(command)]
				status := f.status
				f.mu.Unlock()

				var b strings.Builder
				for _, d := range data {
					fmt.Fprintf(&b, "data: %s\n", d)
				}
				b.WriteString(status + "\n")
				if failed {
					b.WriteString("error\n")
				} else {
					b.WriteString("ok\n")
				}
				if _, err := conn.Write([]byte(b.String())); err != nil {
					return
				}
			}
		}()
	}
}

func commandName(command string) string {
	if i := strings.IndexByte(command, '('); i >= 0 {
		return strings.ToLower(command[:i])
	}
	return strings.ToLower(command)
}

func (f *fakeEmulator) reply(command string, lines ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replies[strings.ToLower(command)] = lines
}

func (f *fakeEmulator) failCommand(command string, lines ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replies[strings.ToLower(command)] = lines
	f.fail[strings.ToLower(command)] = true
}

func (f *fakeEmulator) setStatus(status string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status = status
}

func (f *fakeEmulator) sent() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.commands...)
}

func TestStatusLineIsRecordedAndKeptOutOfTheData(t *testing.T) {
	f := newFakeEmulator(t)
	e := f.emulator()
	f.reply("ascii", "  HELLO  ", "  WORLD  ")

	out, err := e.execCommandOutput("Ascii()")
	if err != nil {
		t.Fatalf("Ascii(): %v", err)
	}
	if strings.Contains(out, "C(mvs.example.com)") {
		t.Errorf("the status line leaked into the payload: %q", out)
	}
	if !strings.Contains(out, "data:   HELLO") {
		t.Errorf("screen data missing: %q", out)
	}

	s := e.Status()
	if !s.Valid || !s.Connected || s.Rows != 24 || s.Cols != 80 {
		t.Errorf("status not recorded: %+v", s)
	}
	if rows, cols := e.ScreenSize(); rows != 24 || cols != 80 {
		t.Errorf("ScreenSize = %dx%d", rows, cols)
	}
}

func TestIsConnectedBelievesTheEmulator(t *testing.T) {
	f := newFakeEmulator(t)
	e := f.emulator()

	f.reply("query", "connected-3270")
	if !e.IsConnected() {
		t.Errorf("connected-3270 is connected")
	}

	// The answer that used to be read as a yes.
	f.reply("query", "not-connected")
	f.setStatus("U U U N N 2 24 80 0 0 0x0 0.000")
	if e.IsConnected() {
		t.Errorf("not-connected is not connected")
	}
}

func TestGetValueRejectsAPositionOffTheScreen(t *testing.T) {
	f := newFakeEmulator(t)
	e := f.emulator()
	f.reply("query", "connected-3270")
	// Learn the geometry the way a running session does.
	e.IsConnected()

	if _, err := e.GetValue(30, 1, 5); err == nil {
		t.Fatalf("reading row 30 of a 24-row screen should fail")
	} else if !strings.Contains(err.Error(), "24x80") {
		t.Errorf("the error should say what the screen is: %v", err)
	}

	// And the emulator was never asked, so no time was spent retrying it.
	for _, c := range f.sent() {
		if strings.HasPrefix(strings.ToLower(c), "ascii(") {
			t.Errorf("an off-screen read should not reach the emulator: %q", c)
		}
	}
}

func TestFillStringQuotesTheValue(t *testing.T) {
	f := newFakeEmulator(t)
	e := f.emulator()

	if err := e.SetString("SMITH,JOHN"); err != nil {
		t.Fatalf("SetString: %v", err)
	}
	sent := f.sent()
	want := `String("SMITH,JOHN")`
	if len(sent) == 0 || sent[len(sent)-1] != want {
		t.Errorf("sent %q, want %q", sent, want)
	}
}

func TestDeterministicFailuresAreNotRetried(t *testing.T) {
	f := newFakeEmulator(t)
	e := f.emulator()
	f.failCommand("string", "String: Syntax error in action name")

	err := e.SetString("anything")
	if err == nil {
		t.Fatalf("expected an error")
	}
	// The emulator's own explanation, not "maximum retries reached".
	if !strings.Contains(err.Error(), "Syntax error") {
		t.Errorf("error should carry the emulator's message: %v", err)
	}
	if n := len(f.sent()); n != 1 {
		t.Errorf("a syntax error was sent %d times; it fails the same way every time", n)
	}
}

func TestTransientFailuresAreStillRetried(t *testing.T) {
	f := newFakeEmulator(t)
	e := f.emulator()
	f.failCommand("string", "Keyboard locked")

	if err := e.SetString("anything"); err == nil {
		t.Fatalf("expected an error")
	}
	if n := len(f.sent()); n != 3 {
		t.Errorf("a transient failure should be retried, sent %d times", n)
	}
}

func TestAsciiScreenGrabWritesTheScreenAndNothingElse(t *testing.T) {
	f := newFakeEmulator(t)
	e := f.emulator()
	f.reply("ascii", "SCREEN ONE", "a < b & c")

	path := filepath.Join(t.TempDir(), "out.html")
	if err := e.AsciiScreenGrab(path, false); err != nil {
		t.Fatalf("AsciiScreenGrab: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	got := string(body)

	if strings.Contains(got, "data:") {
		t.Errorf("the transport prefix was captured as part of the screen: %q", got)
	}
	if strings.Contains(got, "C(mvs.example.com) I 2 24 80") {
		t.Errorf("the status line was captured as part of the screen: %q", got)
	}
	if !strings.Contains(got, "SCREEN ONE") {
		t.Errorf("screen missing: %q", got)
	}
	if !strings.Contains(got, "a &lt; b &amp; c") {
		t.Errorf("host text must be escaped, not rendered as markup: %q", got)
	}
	if strings.Contains(got, "</body></html>") {
		t.Errorf("a capture should not close a document it will be appended to: %q", got)
	}
}

func TestAsciiScreenGrabAPIModeIsPlainText(t *testing.T) {
	f := newFakeEmulator(t)
	e := f.emulator()
	f.reply("ascii", "PLAIN")

	path := filepath.Join(t.TempDir(), "out.txt")
	if err := e.AsciiScreenGrab(path, true); err != nil {
		t.Fatalf("AsciiScreenGrab: %v", err)
	}
	body, _ := os.ReadFile(path)
	if strings.TrimSpace(string(body)) != "PLAIN" {
		t.Errorf("API mode should return the screen alone, got %q", body)
	}
}

func TestWaitForFieldReadsTheKeyboardFromTheStatusLine(t *testing.T) {
	f := newFakeEmulator(t)
	e := f.emulator()

	// Unlocked: one Wait for the unlock, one for the input field.
	if err := e.WaitForField(1, 2); err != nil {
		t.Fatalf("WaitForField on an unlocked keyboard: %v", err)
	}
	waits := 0
	for _, c := range f.sent() {
		if strings.HasPrefix(c, "Wait(") {
			waits++
		}
		if strings.Contains(strings.ToLower(c), "keyboardlock") {
			t.Errorf("the keyboard state is on every status line; it needs no query: %q", c)
		}
	}
	if waits == 0 {
		t.Errorf("expected a Wait, sent %v", f.sent())
	}
}

// TestWaitForFieldRoundsSubSecondTimeoutsUp guards a truncation: the
// emulator's Wait takes whole seconds, so a 500ms timeout would ask for
// Wait(0, ...).
func TestWaitForFieldRoundsSubSecondTimeoutsUp(t *testing.T) {
	f := newFakeEmulator(t)
	e := f.emulator()

	_ = e.WaitForField(500e6, 1) // 500ms
	for _, c := range f.sent() {
		if strings.HasPrefix(c, "Wait(0,") {
			t.Errorf("a sub-second timeout was truncated to zero: %q", c)
		}
	}
}

func TestCheckCoordinatesWithoutGeometryDefersToTheEmulator(t *testing.T) {
	e := &Emulator{}
	if err := e.checkCoordinates(99, 99); err != nil {
		t.Errorf("with no negotiated size yet, the emulator decides: %v", err)
	}
	if err := e.checkCoordinates(0, 1); err == nil {
		t.Errorf("row 0 is not a 1-based position")
	}
}
