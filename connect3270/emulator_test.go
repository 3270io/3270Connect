package connect3270

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestResetKeyConstant tests that the Reset key constant is defined correctly
func TestResetKeyConstant(t *testing.T) {
	if Reset != "Reset" {
		t.Errorf("Reset constant should be 'Reset', got '%s'", Reset)
	}
}

// TestValidateKeyboardWithReset tests that validateKeyboard accepts the Reset key
func TestValidateKeyboardWithReset(t *testing.T) {
	e := &Emulator{}

	// Test that Reset key is valid
	if !e.validateKeyboard(Reset) {
		t.Error("validateKeyboard should return true for Reset key")
	}

	// Also verify that other keys still work
	if !e.validateKeyboard(Enter) {
		t.Error("validateKeyboard should return true for Enter key")
	}

	if !e.validateKeyboard(Tab) {
		t.Error("validateKeyboard should return true for Tab key")
	}

	if !e.validateKeyboard(F1) {
		t.Error("validateKeyboard should return true for F1 key")
	}

	// Test that invalid keys are still rejected
	if e.validateKeyboard("InvalidKey") {
		t.Error("validateKeyboard should return false for invalid key")
	}
}

// TestPressResetKey tests that Press function accepts Reset key
func TestPressResetKey(t *testing.T) {
	e := &Emulator{}

	// We can't actually test the execution without a real connection,
	// but we can verify that the validation passes for Reset key
	// and the function doesn't panic
	if !e.validateKeyboard(Reset) {
		t.Error("Press should accept Reset key as valid")
	}
}

// argIndex returns the index of target in args, or -1 if not present.
func argIndex(args []string, target string) int {
	for i, a := range args {
		if a == target {
			return i
		}
	}
	return -1
}

// TestBuildEmulatorArgsOmitsCodePageWhenUnset verifies no -codepage option is
// added when CodePage is empty, preserving the historical default behavior.
func TestBuildEmulatorArgsOmitsCodePageWhenUnset(t *testing.T) {
	prevHeadless := Headless
	Headless = false
	defer func() { Headless = prevHeadless }()

	e := &Emulator{Host: "mvs.example.com", Port: 992, ScriptPort: "5000"}
	args := e.buildEmulatorArgs("3279-2")

	if argIndex(args, "-codepage") != -1 {
		t.Fatalf("expected no -codepage when CodePage is empty, got args: %v", args)
	}
	if got := args[len(args)-1]; got != "mvs.example.com:992" {
		t.Fatalf("expected hostname as last arg, got %q (args: %v)", got, args)
	}
}

// TestBuildEmulatorArgsIncludesCodePage verifies -codepage <value> is inserted
// immediately before the host:port positional argument when CodePage is set.
func TestBuildEmulatorArgsIncludesCodePage(t *testing.T) {
	prevHeadless := Headless
	Headless = false
	defer func() { Headless = prevHeadless }()

	e := &Emulator{Host: "mvs.example.com", Port: 992, ScriptPort: "5000", CodePage: "cp278"}
	args := e.buildEmulatorArgs("3279-2")

	idx := argIndex(args, "-codepage")
	if idx == -1 {
		t.Fatalf("expected -codepage in args, got: %v", args)
	}
	if idx+1 >= len(args) || args[idx+1] != "cp278" {
		t.Fatalf("expected -codepage followed by cp278, got: %v", args)
	}
	if got := args[len(args)-1]; got != "mvs.example.com:992" {
		t.Fatalf("expected hostname as last arg, got %q (args: %v)", got, args)
	}
	// The code page value must sit immediately before the host:port argument.
	if idx+1 != len(args)-2 {
		t.Fatalf("expected -codepage value immediately before hostname, got: %v", args)
	}
}

// TestBuildEmulatorArgsTrimsCodePage verifies surrounding whitespace is trimmed
// from the configured code page before it is passed to the emulator.
func TestBuildEmulatorArgsTrimsCodePage(t *testing.T) {
	prevHeadless := Headless
	Headless = false
	defer func() { Headless = prevHeadless }()

	e := &Emulator{Host: "h", Port: 23, ScriptPort: "5000", CodePage: "  finnish  "}
	args := e.buildEmulatorArgs("3279-2")

	idx := argIndex(args, "-codepage")
	if idx == -1 || args[idx+1] != "finnish" {
		t.Fatalf("expected trimmed -codepage finnish, got: %v", args)
	}
}

// TestBuildEmulatorArgsHeadlessIncludesCodePage verifies the code page is also
// honored in headless (s3270) mode while keeping the host as the final arg.
func TestBuildEmulatorArgsHeadlessIncludesCodePage(t *testing.T) {
	prevHeadless := Headless
	Headless = true
	defer func() { Headless = prevHeadless }()

	e := &Emulator{Host: "h", Port: 23, ScriptPort: "5050", CodePage: "cp037"}
	args := e.buildEmulatorArgs("3279-2")

	if idx := argIndex(args, "-codepage"); idx == -1 || args[idx+1] != "cp037" {
		t.Fatalf("expected -codepage cp037 in headless args, got: %v", args)
	}
	if argIndex(args, "-scriptport") == -1 {
		t.Fatalf("expected -scriptport in headless args, got: %v", args)
	}
	if got := args[len(args)-1]; got != "h:23" {
		t.Fatalf("expected hostname as last arg in headless mode, got %q (args: %v)", got, args)
	}
}

// TestWaitForFieldErrorIncludesKeyboardLockDetail tests that WaitForField error
// messages include KeyboardLockDetail information when retries are exhausted
func TestWaitForFieldErrorIncludesKeyboardLockDetail(t *testing.T) {
	// We can't fully test WaitForField without a real connection,
	// but we can verify that when it would fail, the error format includes
	// the KeyboardLockDetail message.
	
	// This test verifies the error message structure by checking that the
	// failure path includes the expected "KeyboardLockDetail:" text
	// The actual implementation will add this detail when query succeeds
	expectedSubstring := "KeyboardLockDetail:"
	
	// Create an emulator (won't be connected)
	e := &Emulator{
		Host:       "test.host",
		Port:       23,
		ScriptPort: "5000",
	}
	
	// Call WaitForField with minimal retries (will fail without connection)
	// This should timeout and include KeyboardLockDetail in the error
	err := e.WaitForField(1, 1)
	
	// Verify error occurred (expected since there's no connection)
	if err == nil {
		t.Fatal("Expected WaitForField to fail without connection")
	}
	
	// Verify the error message contains the KeyboardLockDetail marker
	if !strings.Contains(err.Error(), expectedSubstring) {
		t.Errorf("WaitForField error should contain '%s', got: %v", expectedSubstring, err)
	}
}

// A captured screen has to say which worker and which step produced it: a
// concurrent run appends every worker's screens to one file, and without the
// attributes the console cannot tell one virtual user's screens from another's.
func TestCaptureAttrsCarryWorkerAndStep(t *testing.T) {
	e := NewEmulator("mainframe.example", 3270, "5002")
	e.SetCaptureContext(6, 12, "AsciiScreenGrab")

	attrs := e.captureAttrs(time.Unix(1700000000, 0))

	for _, want := range []string{
		`data-port="5002"`,
		`data-host="mainframe.example"`,
		`data-hostport="3270"`,
		`data-step="6"`,
		`data-steps="12"`,
		`data-type="AsciiScreenGrab"`,
		`data-at="1700000000000"`,
	} {
		if !strings.Contains(attrs, want) {
			t.Errorf("captureAttrs() = %q, want it to contain %q", attrs, want)
		}
	}
}

// Every capture the process writes is numbered, in the order the writes
// landed. Concurrent workers interleave in the file, so the sequence is the
// only reliable ordering a reader has.
func TestCaptureAttrsNumberSequentially(t *testing.T) {
	e := NewEmulator("host", 3270, "5001")
	first := e.captureAttrs(time.Unix(1700000000, 0))
	second := e.captureAttrs(time.Unix(1700000001, 0))

	firstSeq := captureSeqOf(t, first)
	secondSeq := captureSeqOf(t, second)
	if secondSeq != firstSeq+1 {
		t.Errorf("capture sequence went %d then %d, want consecutive", firstSeq, secondSeq)
	}
}

func captureSeqOf(t *testing.T, attrs string) int {
	t.Helper()
	const key = `data-capture="`
	start := strings.Index(attrs, key)
	if start < 0 {
		t.Fatalf("attributes %q carry no capture sequence", attrs)
	}
	rest := attrs[start+len(key):]
	end := strings.Index(rest, `"`)
	if end < 0 {
		t.Fatalf("attributes %q have an unterminated capture sequence", attrs)
	}
	value, err := strconv.Atoi(rest[:end])
	if err != nil {
		t.Fatalf("capture sequence %q is not a number: %v", rest[:end], err)
	}
	return value
}

// A capture context left unset is a capture with no step to report, not a
// capture claiming to be step zero.
func TestCaptureAttrsOmitUnsetContext(t *testing.T) {
	e := NewEmulator("", 0, "")
	attrs := e.captureAttrs(time.Unix(1700000000, 0))

	for _, unwanted := range []string{"data-port", "data-host", "data-step", "data-steps", "data-type"} {
		if strings.Contains(attrs, unwanted) {
			t.Errorf("captureAttrs() = %q, want no %s", attrs, unwanted)
		}
	}
}

// A host name with a quote in it must not break out of the attribute it is
// written into — the console parses these back out.
func TestCaptureAttrsEscapeValues(t *testing.T) {
	e := NewEmulator(`ho"st<&`, 23, "5001")
	attrs := e.captureAttrs(time.Unix(1700000000, 0))

	if !strings.Contains(attrs, `data-host="ho&quot;st&lt;&amp;"`) {
		t.Errorf("captureAttrs() = %q, want the host escaped", attrs)
	}
}
