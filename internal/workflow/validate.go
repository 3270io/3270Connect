package workflow

import (
	"fmt"
	"strings"

	"github.com/3270io/3270Connect/connect3270"
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
	// The rest of the 3270 keyboard. A workflow could send Enter, Tab and
	// the 24 PF keys and nothing else, which left no way to interrupt a
	// transaction (PA1), clear a screen between transactions (Clear), or
	// blank a field before typing a shorter value over a longer one
	// (EraseEOF) — all of them everyday operations on a real host.
	types = append(types,
		"PressPA1", "PressPA2", "PressPA3",
		"PressClear",
		"PressReset",
		"PressHome",
		"PressBackTab",
		"PressNewline",
		"PressEraseEOF",
		"PressEraseInput",
		"PressSysReq",
		"PressAttn",
	)
	return types
}()

// PressKeys maps a Press* step onto the key the emulator sends. Having one
// map rather than a switch in the runner and a list here is what stops a key
// being added to one and forgotten in the other.
var PressKeys = func() map[string]string {
	keys := map[string]string{
		"PressEnter":      connect3270.Enter,
		"PressTab":        connect3270.Tab,
		"PressPA1":        connect3270.PA1,
		"PressPA2":        connect3270.PA2,
		"PressPA3":        connect3270.PA3,
		"PressClear":      connect3270.Clear,
		"PressReset":      connect3270.Reset,
		"PressHome":       connect3270.Home,
		"PressBackTab":    connect3270.BackTab,
		"PressNewline":    connect3270.Newline,
		"PressEraseEOF":   connect3270.EraseEOF,
		"PressEraseInput": connect3270.EraseInput,
		"PressSysReq":     connect3270.SysReq,
		"PressAttn":       connect3270.Attn,
	}
	pf := []string{
		connect3270.F1, connect3270.F2, connect3270.F3, connect3270.F4,
		connect3270.F5, connect3270.F6, connect3270.F7, connect3270.F8,
		connect3270.F9, connect3270.F10, connect3270.F11, connect3270.F12,
		connect3270.F13, connect3270.F14, connect3270.F15, connect3270.F16,
		connect3270.F17, connect3270.F18, connect3270.F19, connect3270.F20,
		connect3270.F21, connect3270.F22, connect3270.F23, connect3270.F24,
	}
	for i, key := range pf {
		keys[fmt.Sprintf("PressPF%d", i+1)] = key
	}
	return keys
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
	// Caught here rather than at connect time: the emulator answers an
	// unknown model by printing a complaint and negotiating a different one,
	// so a typo would otherwise become a session quietly running at the
	// wrong screen size.
	if _, err := connect3270.NormalizeModel(config.Model); err != nil {
		return err
	}
	if _, err := connect3270.NormalizeOversize(config.Oversize); err != nil {
		return err
	}
	if config.TLSSkipVerify && !config.TLS {
		return fmt.Errorf("TLSSkipVerify has no effect without TLS")
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
		case stepsWithoutArguments[step.Type] || PressKeys[step.Type] != "":
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
			// A CheckValue without a Length reads zero characters, so it
			// compares the expected text against an empty string and fails
			// every time, reporting "Found: " with nothing after it. No
			// workflow written that way has ever passed.
			if step.Type == "CheckValue" && step.Coordinates.Length <= 0 {
				return fmt.Errorf("step %d: CheckValue needs Coordinates.Length, the number of characters to read", i+1)
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
