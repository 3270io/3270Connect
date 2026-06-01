package connect3270

import (
	"strings"
	"testing"
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
