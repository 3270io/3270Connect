package workflow

import (
	"fmt"
	"strings"
)

// StepTypes lists every step Validate accepts, in the order they are
// documented. It is exported because a schema, a tool description and an
// error message all need the same list, and three copies of it is how a
// step gets added to one and forgotten in the others.
var StepTypes = func() []string {
	types := []string{
		"InitializeOutput",
		"Connect",
		"CheckValue",
		"FillString",
		"AsciiScreenGrab",
		"PressEnter",
		"PressTab",
		"WaitForField",
		"StepDelay",
		"Disconnect",
	}
	for i := 1; i <= 24; i++ {
		types = append(types, fmt.Sprintf("PressPF%d", i))
	}
	return types
}()

// stepsNeedingCoordinates are the steps that act on a specific position and
// therefore require both coordinates and text.
var stepsNeedingCoordinates = map[string]bool{
	"CheckValue": true,
	"FillString": true,
}

// stepsWithoutArguments are the steps that carry no further configuration.
var stepsWithoutArguments = map[string]bool{
	"InitializeOutput": true,
	"Connect":          true,
	"AsciiScreenGrab":  true,
	"PressEnter":       true,
	"PressTab":         true,
	"WaitForField":     true,
	"Disconnect":       true,
	"StepDelay":        true,
}

// Validate reports whether a workflow is well-formed, and is the single gate
// every caller goes through — the CLI, the API handler and the MCP tool.
//
// It returns errors rather than printing, so it can answer a caller that
// wants to know without also wanting a terminal written to.
func Validate(config *Configuration) error {
	if config == nil {
		return fmt.Errorf("no workflow supplied")
	}
	if config.Host == "" {
		return fmt.Errorf("Host is required")
	}
	if config.Port <= 0 {
		return fmt.Errorf("Port must be a positive TCP port number")
	}
	if config.LegacyDelay > 0 {
		return fmt.Errorf("the top-level Delay setting was removed; use EveryStepDelay with Min and Max instead")
	}
	if err := validateDelayRange("EveryStepDelay", config.EveryStepDelay, true); err != nil {
		return err
	}
	if err := validateDelayRange("EndOfTaskDelay", config.EndOfTaskDelay, true); err != nil {
		return err
	}

	if config.OutputFilePath == "" {
		for _, step := range config.Steps {
			if step.Type == "AsciiScreenGrab" {
				return fmt.Errorf("OutputFilePath is required because a step uses AsciiScreenGrab, which writes there")
			}
		}
	}

	for i, step := range config.Steps {
		if step.Type == "HumanDelay" {
			return fmt.Errorf("step %d: HumanDelay was removed; use a StepDelay step with Min and Max instead", i+1)
		}

		switch {
		case stepsWithoutArguments[step.Type] || strings.HasPrefix(step.Type, "PressPF"):
			if step.Type == "StepDelay" {
				if err := validateDelayRange("StepDelay", step.StepDelay, false); err != nil {
					return fmt.Errorf("step %d: %w", i+1, err)
				}
			}

		case stepsNeedingCoordinates[step.Type]:
			// Coordinates are 1-based, so zero means "not set" rather than
			// "the first row".
			if step.Coordinates.Row == 0 || step.Coordinates.Column == 0 {
				return fmt.Errorf("step %d: %s needs Coordinates with a 1-based Row and Column", i+1, step.Type)
			}
			if step.Text == "" {
				return fmt.Errorf("step %d: %s needs Text", i+1, step.Type)
			}

		default:
			return fmt.Errorf("step %d: unknown step type %q. Valid types: %s",
				i+1, step.Type, strings.Join(StepTypes, ", "))
		}
	}
	return nil
}

func validateDelayRange(name string, dr DelayRange, allowZero bool) error {
	if dr.Min < 0 || dr.Max < 0 {
		return fmt.Errorf("%s must be zero or positive", name)
	}
	if dr.Max > 0 && dr.Min > dr.Max {
		return fmt.Errorf("%s Min cannot be greater than Max", name)
	}
	if !allowZero && dr.Min == 0 && dr.Max == 0 {
		return fmt.Errorf("%s requires a positive Min or Max value", name)
	}
	return nil
}
