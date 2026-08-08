package workflow

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// jsonFieldNames returns the JSON names of a struct's exported fields,
// skipping those marked "-".
func jsonFieldNames(t *testing.T, v any) []string {
	t.Helper()
	rt := reflect.TypeOf(v)
	var out []string
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if !f.IsExported() {
			continue
		}
		name := f.Name
		if tag := f.Tag.Get("json"); tag != "" {
			part := strings.Split(tag, ",")[0]
			if part == "-" {
				continue
			}
			if part != "" {
				name = part
			}
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func schemaPropertyNames(t *testing.T, schema map[string]any) []string {
	t.Helper()
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema has no properties object: %v", schema)
	}
	var out []string
	for name := range props {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// TestSchemaMatchesTheStructs is the check that makes the hand-written schema
// trustworthy.
//
// This repository has already published a step shape — Action, Value,
// top-level Row and Column — that no workflow could ever have used, and it
// stayed wrong because nothing compared the description to the code. A
// hand-written schema is worth having for the prose it can carry, but only
// with something holding it against the thing it describes.
func TestSchemaMatchesTheStructs(t *testing.T) {
	t.Run("workflow", func(t *testing.T) {
		// LegacyDelay is the one deliberate difference. The struct accepts
		// "Delay" so that a workflow still using the removed setting gets an
		// error naming its replacement, rather than having it silently
		// ignored — but the schema must not offer it as something to write.
		want := without(jsonFieldNames(t, Configuration{}), "Delay")
		got := schemaPropertyNames(t, Schema())
		if !reflect.DeepEqual(want, got) {
			t.Errorf("schema properties do not match Configuration\n schema: %v\n struct: %v", got, want)
		}
		if props := Schema()["properties"].(map[string]any); props["Delay"] != nil {
			t.Error("the schema must not offer Delay; it exists only to be rejected")
		}
	})

	t.Run("step", func(t *testing.T) {
		items, _ := Schema()["properties"].(map[string]any)["Steps"].(map[string]any)["items"].(map[string]any)
		want := jsonFieldNames(t, Step{})
		got := schemaPropertyNames(t, items)
		if !reflect.DeepEqual(want, got) {
			t.Errorf("step schema properties do not match Step\n schema: %v\n struct: %v", got, want)
		}
	})
}

// TestSchemaRejectsTheWrongStepShape states the specific error the schema
// exists to prevent, so that a future edit reintroducing it fails here.
func TestSchemaRejectsTheWrongStepShape(t *testing.T) {
	items, _ := Schema()["properties"].(map[string]any)["Steps"].(map[string]any)["items"].(map[string]any)
	props := items["properties"].(map[string]any)

	for _, wrong := range []string{"Action", "Value", "Row", "Column", "FilePath"} {
		if _, present := props[wrong]; present {
			t.Errorf("%q is not a step key; it is the shape that was wrongly documented", wrong)
		}
	}
	if items["additionalProperties"] != false {
		t.Error("a step schema that allows extra properties would accept the wrong shape silently")
	}

	desc, _ := Schema()["description"].(string)
	if !strings.Contains(desc, "NOT") {
		t.Error("the schema description should warn against the shape people have seen documented")
	}
}

// TestEveryStepTypeValidates round-trips each documented type through
// Validate, so a type listed in the schema but rejected by the validator —
// or accepted by the validator and missing from the schema — is caught.
func TestEveryStepTypeValidates(t *testing.T) {
	for _, stepType := range StepTypes {
		t.Run(stepType, func(t *testing.T) {
			step := Step{Type: stepType}
			switch stepType {
			case "CheckValue", "FillString":
				step.Coordinates.Row = 1
				step.Coordinates.Column = 1
				step.Coordinates.Length = 4
				step.Text = "TEST"
			case "StepDelay":
				step.StepDelay = DelayRange{Min: 1, Max: 2}
			}

			cfg := &Configuration{
				Host:           "example.test",
				Port:           3270,
				OutputFilePath: "out.html",
				Steps:          []Step{step},
			}
			if err := Validate(cfg); err != nil {
				t.Errorf("%s is offered in the schema but rejected by Validate: %v", stepType, err)
			}
		})
	}

	// And the converse: a type not in the list must be refused, with the
	// valid list in the message so the caller can correct it.
	err := Validate(&Configuration{
		Host: "h", Port: 1, Steps: []Step{{Type: "Action"}},
	})
	if err == nil {
		t.Fatal("an unknown step type should be rejected")
	}
	if !strings.Contains(err.Error(), "Connect") {
		t.Errorf("the error should list the valid types, got %q", err)
	}
}

func TestValidateRejectsRemovedSettings(t *testing.T) {
	t.Run("top-level Delay", func(t *testing.T) {
		err := Validate(&Configuration{
			Host: "h", Port: 1, LegacyDelay: 2,
			Steps: []Step{{Type: "Connect"}},
		})
		if err == nil || !strings.Contains(err.Error(), "EveryStepDelay") {
			t.Errorf("removing Delay should point at its replacement, got %v", err)
		}
	})

	t.Run("HumanDelay step", func(t *testing.T) {
		err := Validate(&Configuration{
			Host: "h", Port: 1,
			Steps: []Step{{Type: "HumanDelay"}},
		})
		if err == nil || !strings.Contains(err.Error(), "StepDelay") {
			t.Errorf("removing HumanDelay should point at its replacement, got %v", err)
		}
	})
}

func TestValidateRequirements(t *testing.T) {
	base := func() *Configuration {
		return &Configuration{Host: "h", Port: 3270, Steps: []Step{{Type: "Connect"}}}
	}

	cases := []struct {
		name   string
		mutate func(*Configuration)
		want   string
	}{
		{"no host", func(c *Configuration) { c.Host = "" }, "Host"},
		{"no port", func(c *Configuration) { c.Port = 0 }, "Port"},
		{"screen grab with no output path", func(c *Configuration) {
			c.Steps = append(c.Steps, Step{Type: "AsciiScreenGrab"})
		}, "OutputFilePath"},
		{"coordinates missing", func(c *Configuration) {
			c.Steps = append(c.Steps, Step{Type: "FillString", Text: "x"})
		}, "Coordinates"},
		{"text missing", func(c *Configuration) {
			s := Step{Type: "CheckValue"}
			s.Coordinates.Row, s.Coordinates.Column = 1, 1
			c.Steps = append(c.Steps, s)
		}, "Text"},
		{"step delay with no window", func(c *Configuration) {
			c.Steps = append(c.Steps, Step{Type: "StepDelay"})
		}, "StepDelay"},
		{"inverted delay range", func(c *Configuration) {
			c.EveryStepDelay = DelayRange{Min: 5, Max: 1}
		}, "Min cannot be greater than Max"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base()
			tc.mutate(cfg)
			err := Validate(cfg)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error should mention %q, got %q", tc.want, err)
			}
		})
	}
}

// TestWaitForFieldAcceptsBothForms covers the compatibility shim: the field
// is written as a bare boolean in most workflows and as an object when
// someone wants to tune it.
func TestWaitForFieldAcceptsBothForms(t *testing.T) {
	t.Run("boolean", func(t *testing.T) {
		var cfg Configuration
		if err := json.Unmarshal([]byte(`{"WaitForField": true}`), &cfg); err != nil {
			t.Fatal(err)
		}
		if !cfg.WaitForField.Enabled {
			t.Error("true should enable it")
		}
		if cfg.WaitForField.Delay != DefaultWaitForFieldDelay || cfg.WaitForField.Retries != DefaultWaitForFieldRetries {
			t.Errorf("the boolean form should take the defaults, got %+v", cfg.WaitForField)
		}
	})

	t.Run("object", func(t *testing.T) {
		var cfg Configuration
		if err := json.Unmarshal([]byte(`{"WaitForField": {"Delay": 3, "Retries": 4}}`), &cfg); err != nil {
			t.Fatal(err)
		}
		if !cfg.WaitForField.Enabled {
			t.Error("supplying an object should imply enabled")
		}
		if cfg.WaitForField.Delay != 3 || cfg.WaitForField.Retries != 4 {
			t.Errorf("object values should be kept, got %+v", cfg.WaitForField)
		}
	})
}

// TestSchemaIsSerialisable: the schema is handed to MCP clients verbatim, so
// a value that cannot be marshalled would break tools/list rather than one
// tool call.
func TestSchemaIsSerialisable(t *testing.T) {
	data, err := json.Marshal(Schema())
	if err != nil {
		t.Fatalf("schema does not marshal: %v", err)
	}
	for _, want := range []string{"1-based", "AsciiScreenGrab", "Coordinates"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("schema lost %q", want)
		}
	}
}

// without returns names with one entry removed.
func without(names []string, drop string) []string {
	out := names[:0:0]
	for _, n := range names {
		if n != drop {
			out = append(out, n)
		}
	}
	return out
}
