package workflow

// Schema returns the JSON Schema for a workflow document.
//
// It is hand-written rather than reflected, because the useful part of a
// schema for something composing a workflow is the prose: that coordinates
// are 1-based, that AsciiScreenGrab writes to the top-level OutputFilePath,
// that Delay and HumanDelay were removed. Reflection produces the field names
// and none of that.
//
// The risk with hand-writing it is drift, so schema_test.go checks the
// property names against the struct tags and round-trips every step type
// through Validate. That check is the reason this can be trusted; without it
// this file would be a second, quietly diverging description.
func Schema() map[string]any {
	return map[string]any{
		"$schema":     "https://json-schema.org/draft/2020-12/schema",
		"title":       "3270Connect workflow",
		"description": workflowDescription,
		"type":        "object",
		"required":    []string{"Host", "Port", "Steps"},
		"properties": map[string]any{
			"Host": map[string]any{
				"type":        "string",
				"description": "Hostname or IP address of the TN3270 host.",
			},
			"Port": map[string]any{
				"type":        "integer",
				"minimum":     1,
				"maximum":     65535,
				"description": "TCP port of the TN3270 host.",
			},
			"CodePage": map[string]any{
				"type":        "string",
				"description": "Host EBCDIC code page, e.g. \"cp037\", \"cp285\", \"cp278\" (alias \"finnish\"). Omit for the emulator default.",
			},
			"OutputFilePath": map[string]any{
				"type":        "string",
				"description": "Where captured screens are written. Required if any step uses AsciiScreenGrab.",
			},
			"InputFilePath": map[string]any{
				"type":        "string",
				"description": "Optional alternative to Steps: a file of steps in the input-file DSL.",
			},
			"Token": map[string]any{
				"type":        "string",
				"description": "Value substituted for {{token}} placeholders in step Text. Prefer the -token flag or the .env file over writing a real token here.",
			},
			"WaitForField": map[string]any{
				"description": "Wait for the host keyboard to unlock before each step. true for defaults, or an object to tune them.",
				"oneOf": []any{
					map[string]any{"type": "boolean"},
					map[string]any{
						"type": "object",
						"properties": map[string]any{
							"Delay":   map[string]any{"type": "number", "minimum": 0, "description": "Seconds to wait per attempt (default 1)."},
							"Retries": map[string]any{"type": "integer", "minimum": 0, "description": "Attempts before giving up (default 10)."},
						},
						"additionalProperties": false,
					},
				},
			},
			"EveryStepDelay":  delayRangeSchema("Randomised pause between every step, to model typing and host reaction time."),
			"EndOfTaskDelay":  delayRangeSchema("Randomised pause after the final step, to model user think-time between repeats."),
			"RampUpBatchSize": map[string]any{"type": "integer", "minimum": 1, "description": "Workflows started per ramp-up batch during a concurrent run (default 10)."},
			"RampUpDelay":     map[string]any{"type": "number", "minimum": 0, "description": "Seconds between ramp-up batches (default 1)."},
			"GracePeriod":     map[string]any{"type": "number", "minimum": 0, "description": "Seconds to let in-flight workflows finish after the runtime deadline (default 30)."},
			"AutoShutdownTimeout": map[string]any{
				"type": "number", "minimum": 0,
				"description": "Seconds on the shutdown countdown once the grace period elapses (default 10).",
			},
			"Steps": map[string]any{
				"type":        "array",
				"minItems":    1,
				"description": "The actions to perform, in order. Start with Connect and end with Disconnect.",
				"items":       stepSchema(),
			},
		},
		"additionalProperties": false,
	}
}

func delayRangeSchema(description string) map[string]any {
	return map[string]any{
		"type":        "object",
		"description": description,
		"properties": map[string]any{
			"Min": map[string]any{"type": "number", "minimum": 0, "description": "Shortest delay in seconds."},
			"Max": map[string]any{"type": "number", "minimum": 0, "description": "Longest delay in seconds. Defaults to Min when omitted."},
		},
		"additionalProperties": false,
	}
}

func stepSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"Type"},
		"properties": map[string]any{
			"Type": map[string]any{
				"type":        "string",
				"enum":        StepTypes,
				"description": stepTypeDescription,
			},
			"Coordinates": map[string]any{
				"type":        "object",
				"description": "Screen position for CheckValue and FillString. 1-based: the top-left cell is Row 1, Column 1.",
				"properties": map[string]any{
					"Row":    map[string]any{"type": "integer", "minimum": 1, "description": "1-based row."},
					"Column": map[string]any{"type": "integer", "minimum": 1, "description": "1-based column."},
					"Length": map[string]any{"type": "integer", "minimum": 0, "description": "Characters to read, for CheckValue."},
				},
				"additionalProperties": false,
			},
			"Text": map[string]any{
				"type":        "string",
				"description": "Text to type (FillString) or expect (CheckValue). May contain {{token}} or an injection placeholder such as {{username}}.",
			},
			"Delay": map[string]any{
				"type": "number", "minimum": 0,
				"description": "Timeout in seconds for a WaitForField step.",
			},
			"StepDelay": delayRangeSchema("Pause inserted by a StepDelay step. Min or Max must be positive."),
		},
		"additionalProperties": false,
	}
}

const workflowDescription = `A 3270Connect workflow: a TN3270 host, and the ordered steps to perform against it.

A step's keys are Type, Coordinates and Text. They are NOT "Action", "Value",
or top-level "Row"/"Column" — that shape appears in some older notes and no
workflow written that way has ever run. Coordinates are 1-based.

The same document drives a single run and a concurrent load test; the
difference is on the command line (-concurrent and -runtime), not in the file.`

const stepTypeDescription = `The action to perform.

  InitializeOutput  Re-initialise the output file. Rarely needed; it happens automatically.
  Connect           Open the session. Normally the first step.
  FillString        Type Text into the field at Coordinates.
  CheckValue        Assert that Coordinates holds Text, for Length characters.
  PressEnter        Send Enter.
  PressTab          Send Tab.
  PressPF1..PF24    Send a PF key.
  WaitForField      Wait for the keyboard to unlock, up to Delay seconds.
  StepDelay         Pause for a random time inside StepDelay's Min/Max.
  AsciiScreenGrab   Append the current screen to OutputFilePath.
  Disconnect        Close the session. Normally the last step.`
