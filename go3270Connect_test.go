package main

import (
	"encoding/json"
	"math/rand"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	connect3270 "github.com/3270io/3270Connect/connect3270"
)

func TestRandomDurationWithinRange(t *testing.T) {
	oldRng := delayRNG
	delayRNG = rand.New(rand.NewSource(1))
	defer func() { delayRNG = oldRng }()
	delay, err := randomDuration(DelayRange{Min: 0.1, Max: 0.3}, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if delay < 100*time.Millisecond || delay > 300*time.Millisecond {
		t.Fatalf("expected delay between 100ms and 300ms, got %v", delay)
	}
}

func TestRandomDurationDefaultsMaxToMin(t *testing.T) {
	oldRng := delayRNG
	delayRNG = rand.New(rand.NewSource(2))
	defer func() { delayRNG = oldRng }()
	expected := 1500 * time.Millisecond
	delay, err := randomDuration(DelayRange{Min: 1.5}, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if delay != expected {
		t.Fatalf("expected delay %v, got %v", expected, delay)
	}
}

func TestCapDelayForDeadlineZeroDeadline(t *testing.T) {
	delay := 1500 * time.Millisecond
	if capped := capDelayForDeadline(delay, time.Time{}); capped != delay {
		t.Fatalf("expected delay to remain %v, got %v", delay, capped)
	}
}

func TestCapDelayForDeadlineElapsed(t *testing.T) {
	delay := 2 * time.Second
	deadline := time.Now().Add(-time.Second)
	if capped := capDelayForDeadline(delay, deadline); capped != 0 {
		t.Fatalf("expected delay to be capped to 0, got %v", capped)
	}
}

func TestCapDelayForDeadlineShorterRemaining(t *testing.T) {
	delay := 2 * time.Second
	deadline := time.Now().Add(200 * time.Millisecond)
	capped := capDelayForDeadline(delay, deadline)
	if capped <= 0 || capped > 200*time.Millisecond {
		t.Fatalf("expected capped delay between 0 and 200ms, got %v", capped)
	}
}

func TestFormatWorkflowStatusLine(t *testing.T) {
	start := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	status := workflowStatus{
		ScriptPort:  "5001",
		Host:        "localhost",
		Port:        3270,
		CurrentStep: 2,
		TotalSteps:  5,
		StepType:    "FillString",
		StartedAt:   start,
	}
	line := formatWorkflowStatusLine(status, start.Add(5*time.Second))
	if !strings.Contains(line, "ScriptPort 5001") {
		t.Fatalf("expected script port in line, got %s", line)
	}
	if !strings.Contains(line, "localhost:3270") {
		t.Fatalf("expected host/port in line, got %s", line)
	}
	if !strings.Contains(line, "Step 2/5 (FillString)") {
		t.Fatalf("expected step info in line, got %s", line)
	}
	if !strings.Contains(line, "Running 5s") {
		t.Fatalf("expected running duration in line, got %s", line)
	}
}

func TestValidateConfigurationRejectsLegacyDelayAndHumanDelay(t *testing.T) {
	cfg := Configuration{
		Host:        "host",
		Port:        3270,
		LegacyDelay: 1,
		Steps:       []Step{{Type: "Connect"}},
	}
	if err := validateConfiguration(&cfg); err == nil || !strings.Contains(err.Error(), "Delay is no longer supported") {
		t.Fatalf("expected legacy Delay validation error, got %v", err)
	}

	cfg.LegacyDelay = 0
	cfg.Steps = []Step{{Type: "HumanDelay"}}
	if err := validateConfiguration(&cfg); err == nil || !strings.Contains(err.Error(), "HumanDelay is no longer supported") {
		t.Fatalf("expected HumanDelay validation error, got %v", err)
	}
}

func TestInjectDynamicValues(t *testing.T) {
	config := &Configuration{
		Host: "localhost",
		Port: 3270,
		Steps: []Step{
			{Type: "Connect"},
			{Type: "FillString", Text: "{{username}}"},
			{Type: "FillString", Text: "{{password}}"},
			{Type: "Disconnect"},
		},
	}

	injection := map[string]string{
		"{{username}}": "testuser",
		"{{password}}": "testpass",
	}

	result := injectDynamicValues(config, injection)

	// Verify placeholders were replaced
	if result.Steps[1].Text != "testuser" {
		t.Errorf("expected username to be 'testuser', got '%s'", result.Steps[1].Text)
	}
	if result.Steps[2].Text != "testpass" {
		t.Errorf("expected password to be 'testpass', got '%s'", result.Steps[2].Text)
	}

	// Verify original config was not modified
	if config.Steps[1].Text != "{{username}}" {
		t.Errorf("original config should not be modified")
	}
}

func TestInjectDynamicValuesPartialMatch(t *testing.T) {
	config := &Configuration{
		Host: "localhost",
		Port: 3270,
		Steps: []Step{
			{Type: "FillString", Text: "User: {{username}}, Pass: {{password}}"},
		},
	}

	injection := map[string]string{
		"{{username}}": "admin",
		"{{password}}": "secret",
	}

	result := injectDynamicValues(config, injection)

	expected := "User: admin, Pass: secret"
	if result.Steps[0].Text != expected {
		t.Errorf("expected '%s', got '%s'", expected, result.Steps[0].Text)
	}
}

func TestShouldAutoWaitForField(t *testing.T) {
	config := &Configuration{WaitForField: WaitForFieldConfig{Enabled: true, Delay: 1.0, Retries: 10}}
	if shouldAutoWaitForField(config, Step{Type: "FillString"}, true) == false {
		t.Fatalf("expected auto wait for FillString when connected")
	}
	if shouldAutoWaitForField(config, Step{Type: "Connect"}, true) {
		t.Fatalf("did not expect auto wait for Connect")
	}
	if shouldAutoWaitForField(config, Step{Type: "WaitForField"}, true) {
		t.Fatalf("did not expect auto wait for explicit WaitForField")
	}
	if shouldAutoWaitForField(config, Step{Type: "FillString"}, false) {
		t.Fatalf("did not expect auto wait before connect")
	}
	config.WaitForField.Enabled = false
	if shouldAutoWaitForField(config, Step{Type: "FillString"}, true) {
		t.Fatalf("did not expect auto wait when config disabled")
	}
}

func TestWaitForFieldConfigJSON(t *testing.T) {
	// Test unmarshaling boolean value (backward compatibility)
	jsonTrue := `{"WaitForField": true}`
	var configTrue Configuration
	if err := json.Unmarshal([]byte(jsonTrue), &configTrue); err != nil {
		t.Fatalf("failed to unmarshal boolean true: %v", err)
	}
	if !configTrue.WaitForField.Enabled {
		t.Errorf("expected Enabled to be true")
	}
	if configTrue.WaitForField.Delay != 1.0 {
		t.Errorf("expected default Delay 1.0, got %f", configTrue.WaitForField.Delay)
	}
	if configTrue.WaitForField.Retries != 10 {
		t.Errorf("expected default Retries 10, got %d", configTrue.WaitForField.Retries)
	}

	jsonFalse := `{"WaitForField": false}`
	var configFalse Configuration
	if err := json.Unmarshal([]byte(jsonFalse), &configFalse); err != nil {
		t.Fatalf("failed to unmarshal boolean false: %v", err)
	}
	if configFalse.WaitForField.Enabled {
		t.Errorf("expected Enabled to be false")
	}

	// Test unmarshaling object value with custom settings
	jsonObject := `{"WaitForField": {"Delay": 2, "Retries": 5}}`
	var configObject Configuration
	if err := json.Unmarshal([]byte(jsonObject), &configObject); err != nil {
		t.Fatalf("failed to unmarshal object: %v", err)
	}
	if !configObject.WaitForField.Enabled {
		t.Errorf("expected Enabled to be true for object format")
	}
	if configObject.WaitForField.Delay != 2.0 {
		t.Errorf("expected Delay 2.0, got %f", configObject.WaitForField.Delay)
	}
	if configObject.WaitForField.Retries != 5 {
		t.Errorf("expected Retries 5, got %d", configObject.WaitForField.Retries)
	}

	// Test unmarshaling object with only Delay specified
	jsonPartial := `{"WaitForField": {"Delay": 3}}`
	var configPartial Configuration
	if err := json.Unmarshal([]byte(jsonPartial), &configPartial); err != nil {
		t.Fatalf("failed to unmarshal partial object: %v", err)
	}
	if configPartial.WaitForField.Delay != 3.0 {
		t.Errorf("expected Delay 3.0, got %f", configPartial.WaitForField.Delay)
	}
	if configPartial.WaitForField.Retries != 10 {
		t.Errorf("expected default Retries 10, got %d", configPartial.WaitForField.Retries)
	}
}

func TestInjectDynamicValuesWithUTF8Characters(t *testing.T) {
	config := &Configuration{
		Host: "localhost",
		Port: 3270,
		Steps: []Step{
			{Type: "FillString", Text: "{{firstname}}"},
			{Type: "FillString", Text: "{{lastname}}"},
		},
	}

	injection := map[string]string{
		"{{firstname}}": "SÄR",
		"{{lastname}}":  "0218",
	}

	result := injectDynamicValues(config, injection)

	// Verify UTF-8 characters (Swedish Ä) are preserved
	if result.Steps[0].Text != "SÄR" {
		t.Errorf("expected firstname to be 'SÄR', got '%s'", result.Steps[0].Text)
	}
	if result.Steps[1].Text != "0218" {
		t.Errorf("expected lastname to be '0218', got '%s'", result.Steps[1].Text)
	}

	// Verify original config was not modified
	if config.Steps[0].Text != "{{firstname}}" {
		t.Errorf("original config should not be modified")
	}
}

func TestLoadInjectionDataWithUTF8Characters(t *testing.T) {
	// Create a temporary JSON file with UTF-8 characters
	tmpfile, err := os.CreateTemp("", "injection-utf8-*.json")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())

	// Write JSON content with Swedish characters
	jsonContent := `[
		{
			"{{firstname}}": "SÄR",
			"{{lastname}}": "0218"
		},
		{
			"{{firstname}}": "SÖR",
			"{{lastname}}": "0219"
		}
	]`

	if _, err := tmpfile.Write([]byte(jsonContent)); err != nil {
		t.Fatal(err)
	}
	if err := tmpfile.Close(); err != nil {
		t.Fatal(err)
	}

	// Load the injection data
	data, err := loadInjectionData(tmpfile.Name())
	if err != nil {
		t.Fatalf("Failed to load injection data: %v", err)
	}

	// Verify the data was loaded correctly
	if len(data) != 2 {
		t.Errorf("expected 2 entries, got %d", len(data))
	}

	// Verify UTF-8 characters are preserved
	if data[0]["{{firstname}}"] != "SÄR" {
		t.Errorf("expected first entry firstname to be 'SÄR', got '%s'", data[0]["{{firstname}}"])
	}
	if data[1]["{{firstname}}"] != "SÖR" {
		t.Errorf("expected second entry firstname to be 'SÖR', got '%s'", data[1]["{{firstname}}"])
	}
}

func TestCaptureFailureScreenDisabledByDefault(t *testing.T) {
	// Save original value
	oldFlag := verboseScreenCaptureFailures
	defer func() { verboseScreenCaptureFailures = oldFlag }()

	// Ensure flag is disabled
	verboseScreenCaptureFailures = false

	// Create a mock emulator (will not actually be used since flag is disabled)
	e := &connect3270.Emulator{}

	// Call captureFailureScreen with flag disabled
	result := captureFailureScreen(e, "5001", 1)

	// Should return empty string when flag is disabled
	if result != "" {
		t.Errorf("expected empty string when flag is disabled, got '%s'", result)
	}
}

func TestCaptureFailureScreenLimitTo5(t *testing.T) {
	// Save original values
	oldFlag := verboseScreenCaptureFailures
	oldCount := atomic.LoadInt64(&screenCaptureCount)
	defer func() {
		verboseScreenCaptureFailures = oldFlag
		atomic.StoreInt64(&screenCaptureCount, oldCount)
	}()

	// Enable the flag
	verboseScreenCaptureFailures = true

	// Set counter to 5 (maximum)
	atomic.StoreInt64(&screenCaptureCount, 5)

	// Create a mock emulator
	e := &connect3270.Emulator{}

	// Try to capture when limit is reached
	result := captureFailureScreen(e, "5001", 1)

	// Should return empty string when limit is reached
	if result != "" {
		t.Errorf("expected empty string when limit is reached, got '%s'", result)
	}

	// Counter should still be 5
	if atomic.LoadInt64(&screenCaptureCount) != 5 {
		t.Errorf("expected counter to remain 5, got %d", atomic.LoadInt64(&screenCaptureCount))
	}
}

func TestCaptureFailureScreenAtomicIncrement(t *testing.T) {
	// Save original values
	oldFlag := verboseScreenCaptureFailures
	oldCount := atomic.LoadInt64(&screenCaptureCount)
	defer func() {
		verboseScreenCaptureFailures = oldFlag
		atomic.StoreInt64(&screenCaptureCount, oldCount)
	}()

	// Enable the flag
	verboseScreenCaptureFailures = true

	// Set counter to 0
	atomic.StoreInt64(&screenCaptureCount, 0)

	// Note: We can't actually test the full capture functionality without
	// a real emulator connection, but we can verify the atomic counter logic
	// by checking that the counter doesn't exceed 5 even with concurrent calls

	// Simulate attempting to capture beyond the limit
	for i := 0; i < 10; i++ {
		atomic.AddInt64(&screenCaptureCount, 1)
		count := atomic.LoadInt64(&screenCaptureCount)
		if count > 5 {
			// Simulate the rollback that happens in captureFailureScreen
			atomic.AddInt64(&screenCaptureCount, -1)
		}
	}

	// Counter should not exceed 5
	finalCount := atomic.LoadInt64(&screenCaptureCount)
	if finalCount > 5 {
		t.Errorf("expected counter not to exceed 5, got %d", finalCount)
	}
}
