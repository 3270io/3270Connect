// Package workflow defines the JSON document 3270Connect executes and the
// rules for what makes one valid.
//
// It is separate from the runner deliberately. Validating a workflow is
// something callers need without running it — a model composing one wants a
// validate loop, and CI wants to check a file before a load test uses it —
// and shelling out to a subprocess to find out whether a string parses is not
// a reasonable way to answer that.
//
// Keeping the schema next to the structs and the validator has a second
// effect that matters more than it sounds: schema_test.go ties the three
// together, so the published description of a workflow cannot drift from
// what the program actually accepts. The repository has already shipped
// documentation for a step shape that no workflow could ever have used.
package workflow

import (
	"encoding/json"

	"github.com/3270io/3270Connect/connect3270"
)

// Defaults applied when a workflow does not state otherwise.
const (
	DefaultWaitForFieldDelay   = 1.0
	DefaultWaitForFieldRetries = 10
	DefaultRampUpBatchSize     = 10
	DefaultRampUpDelay         = 1.0
	DefaultGracePeriod         = 30.0
	DefaultAutoShutdownTimeout = 10.0
)

// DelayRange represents a randomized delay window in seconds. When Max is
// omitted (zero) but Min is set, Max defaults to Min. Set both Min and Max to
// zero to disable the delay entirely.
type DelayRange struct {
	Min float64 `json:"Min,omitempty"`
	Max float64 `json:"Max,omitempty"`
}

// WaitForFieldConfig holds the configuration for WaitForField behavior.
// It supports both simple boolean values (for backward compatibility) and
// detailed configuration with custom delay and retry settings.
type WaitForFieldConfig struct {
	Enabled bool    `json:"-"` // Not directly unmarshaled
	Delay   float64 `json:"Delay,omitempty"`
	Retries int     `json:"Retries,omitempty"`
}

// UnmarshalJSON implements custom JSON unmarshaling to support both boolean and object formats.
// Examples: "WaitForField": true or "WaitForField": { "Delay": 2, "Retries": 10 }
func (w *WaitForFieldConfig) UnmarshalJSON(data []byte) error {
	// Try to unmarshal as boolean first
	var boolVal bool
	if err := json.Unmarshal(data, &boolVal); err == nil {
		w.Enabled = boolVal
		// Set defaults when using boolean format
		if w.Delay == 0 {
			w.Delay = DefaultWaitForFieldDelay
		}
		if w.Retries == 0 {
			w.Retries = DefaultWaitForFieldRetries
		}
		return nil
	}

	// Try to unmarshal as object
	type Alias WaitForFieldConfig
	aux := &struct {
		*Alias
	}{
		Alias: (*Alias)(w),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	w.Enabled = true // If object format is used, assume enabled
	// Set defaults if not provided
	if w.Delay == 0 {
		w.Delay = DefaultWaitForFieldDelay
	}
	if w.Retries == 0 {
		w.Retries = DefaultWaitForFieldRetries
	}
	return nil
}

// MarshalJSON implements custom JSON marshaling.
func (w WaitForFieldConfig) MarshalJSON() ([]byte, error) {
	// If using default values, just marshal as boolean
	if w.Delay == DefaultWaitForFieldDelay && w.Retries == DefaultWaitForFieldRetries {
		return json.Marshal(w.Enabled)
	}
	// Otherwise, marshal as object with just the custom fields
	return json.Marshal(map[string]interface{}{
		"Delay":   w.Delay,
		"Retries": w.Retries,
	})
}

// Configuration holds the settings for the terminal connection and the steps to be executed.
type Configuration struct {
	Host string
	Port int
	// CodePage selects the host EBCDIC code page / character set for the 3270
	// session (e.g. "cp037", "cp285", "cp278" or the alias "finnish"). It is
	// passed to the underlying x3270/s3270 emulator via its -codepage option.
	// Leave empty to use the emulator default. The -codePage CLI flag overrides
	// this value when set.
	CodePage       string             `json:"CodePage,omitempty"`
	OutputFilePath string             `json:"OutputFilePath"`
	WaitForField   WaitForFieldConfig `json:"WaitForField,omitempty"`
	Steps          []Step
	EveryStepDelay DelayRange `json:"EveryStepDelay,omitempty"`
	EndOfTaskDelay DelayRange `json:"EndOfTaskDelay,omitempty"`
	Token          string     `json:"Token,omitempty"`
	InputFilePath  string     `json:"InputFilePath"`

	RampUpBatchSize int     `json:"RampUpBatchSize"`
	RampUpDelay     float64 `json:"RampUpDelay"`
	// LegacyDelay exists only so a workflow still using the removed top-level
	// Delay is rejected with an explanation rather than silently ignored.
	LegacyDelay         float64 `json:"Delay,omitempty"`
	GracePeriod         float64 `json:"GracePeriod,omitempty"`
	AutoShutdownTimeout float64 `json:"AutoShutdownTimeout,omitempty"`
}

// Step represents an individual action to be taken on the terminal.
//
// Coordinates are 1-based, matching how a 3270 screen is described in its own
// documentation and in every terminal emulator's status line.
type Step struct {
	Type        string
	Coordinates connect3270.Coordinates
	Text        string
	Delay       float64    `json:"Delay,omitempty"`
	StepDelay   DelayRange `json:"StepDelay,omitempty"`
}
