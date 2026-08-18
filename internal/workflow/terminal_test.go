package workflow

import (
	"strings"
	"testing"
)

func validWorkflow(steps ...Step) *Configuration {
	if len(steps) == 0 {
		steps = []Step{{Type: "Connect"}, {Type: "Disconnect"}}
	}
	return &Configuration{Host: "mvs.example.com", Port: 23, Steps: steps}
}

// TestEveryPressStepHasAKey is the check that keeps the two halves of the
// keyboard together: a step type nobody mapped to a key would be accepted by
// Validate and then fail at the point of pressing it, on a real host, in the
// middle of a run.
func TestEveryPressStepHasAKey(t *testing.T) {
	for _, stepType := range StepTypes {
		if !strings.HasPrefix(stepType, "Press") {
			continue
		}
		if PressKeys[stepType] == "" {
			t.Errorf("%s is a valid step but sends no key", stepType)
		}
	}
	for stepType := range PressKeys {
		found := false
		for _, known := range StepTypes {
			if known == stepType {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s sends a key but is not a valid step type", stepType)
		}
	}
}

func TestValidateAcceptsTheWholeKeyboard(t *testing.T) {
	for stepType := range PressKeys {
		if err := Validate(validWorkflow(Step{Type: "Connect"}, Step{Type: stepType})); err != nil {
			t.Errorf("Validate rejected %s: %v", stepType, err)
		}
	}
}

func TestValidateChecksTheTerminalSettings(t *testing.T) {
	for _, model := range []string{"", "2", "4", "3278-3", "3279-5"} {
		c := validWorkflow()
		c.Model = model
		if err := Validate(c); err != nil {
			t.Errorf("Model %q should be accepted: %v", model, err)
		}
	}
	for _, model := range []string{"7", "3270-2", "big"} {
		c := validWorkflow()
		c.Model = model
		if err := Validate(c); err == nil {
			t.Errorf("Model %q should be rejected", model)
		}
	}

	c := validWorkflow()
	c.Oversize = "132"
	if err := Validate(c); err == nil {
		t.Errorf("an oversize without rows should be rejected")
	}

	// Skipping certificate validation on a connection that is not encrypted
	// is not a setting, it is a misunderstanding.
	c = validWorkflow()
	c.TLSSkipVerify = true
	if err := Validate(c); err == nil {
		t.Errorf("TLSSkipVerify without TLS should be rejected")
	}
	c.TLS = true
	if err := Validate(c); err != nil {
		t.Errorf("TLSSkipVerify with TLS should be accepted: %v", err)
	}
}
