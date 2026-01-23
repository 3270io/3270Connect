package connect3270

import (
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
