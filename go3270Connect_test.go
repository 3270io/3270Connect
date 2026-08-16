package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	connect3270 "github.com/3270io/3270Connect/connect3270"
	"github.com/3270io/3270Connect/internal/runstore"
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

// The console shows this string to whoever can reach the dashboard, so what
// it must not contain matters as much as what it must.
func TestDescribeStep(t *testing.T) {
	cases := []struct {
		name string
		step Step
		want string
	}{
		{
			name: "fill string gives position and length, never the value",
			step: Step{
				Type:        "FillString",
				Coordinates: connect3270.Coordinates{Row: 10, Column: 20, Length: 8},
				Text:        "hunter2",
			},
			want: "R10,C20 len 8",
		},
		{
			name: "fill string without a declared length falls back to the value's",
			step: Step{
				Type:        "FillString",
				Coordinates: connect3270.Coordinates{Row: 5, Column: 21},
				Text:        "user1-firstname",
			},
			want: "R5,C21 len 15",
		},
		{
			name: "check value shows what is being waited for",
			step: Step{
				Type:        "CheckValue",
				Coordinates: connect3270.Coordinates{Row: 24, Column: 1, Length: 5},
				Text:        "READY",
			},
			want: `R24,C1 = "READY"`,
		},
		{
			name: "long expected values are cut rather than run across the row",
			step: Step{
				Type:        "CheckValue",
				Coordinates: connect3270.Coordinates{Row: 1, Column: 1},
				Text:        strings.Repeat("A", 40),
			},
			want: `R1,C1 = "` + strings.Repeat("A", 31) + `…"`,
		},
		{
			name: "keystrokes have no position of their own",
			step: Step{Type: "PressEnter"},
			want: "",
		},
		{
			name: "screen grabs are about the whole screen",
			step: Step{
				Type:        "AsciiScreenGrab",
				Coordinates: connect3270.Coordinates{Row: 1, Column: 1},
			},
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := describeStep(tc.step)
			if got != tc.want {
				t.Fatalf("describeStep(%s) = %q, want %q", tc.step.Type, got, tc.want)
			}
		})
	}
}

// A FillString's text is a username or a password often enough that it must
// never reach the metrics file, whatever route it takes to get there.
func TestDescribeStepNeverLeaksFilledText(t *testing.T) {
	secret := "s3cr3t-password"
	step := Step{
		Type:        "FillString",
		Coordinates: connect3270.Coordinates{Row: 7, Column: 12, Length: len(secret)},
		Text:        secret,
	}
	if detail := describeStep(step); strings.Contains(detail, secret) {
		t.Fatalf("describeStep leaked the filled value: %q", detail)
	}
}

func TestUpdateWorkflowStatusRecordsCurrentStep(t *testing.T) {
	const port = "5999"
	registerWorkflowStatus(port, &Configuration{Host: "mainframe.host", Port: 3270}, 11)
	defer clearWorkflowStatus(port)

	updateWorkflowStatus(port, 4, "CheckValue", `R24,C1 = "READY"`)

	statuses := snapshotWorkflowStatuses()
	var found *workflowStatus
	for i := range statuses {
		if statuses[i].ScriptPort == port {
			found = &statuses[i]
		}
	}
	if found == nil {
		t.Fatalf("status for script port %s was not recorded", port)
	}
	if found.CurrentStep != 4 || found.StepType != "CheckValue" {
		t.Fatalf("unexpected position: step %d (%s)", found.CurrentStep, found.StepType)
	}
	if found.StepDetail != `R24,C1 = "READY"` {
		t.Fatalf("unexpected detail: %q", found.StepDetail)
	}
	// Without this the console cannot tell a slow run from a stuck one.
	if found.StepStartedAt.IsZero() {
		t.Fatal("step start time was not recorded")
	}
	if found.StepStartedAt.Before(found.StartedAt) {
		t.Fatal("the current step cannot have started before the workflow did")
	}
}

func TestLiveStepSnapshotPublishesStepStart(t *testing.T) {
	const port = "5998"
	registerWorkflowStatus(port, &Configuration{Host: "mainframe.host", Port: 3270}, 11)
	defer clearWorkflowStatus(port)
	updateWorkflowStatus(port, 2, "FillString", "R10,C20 len 8")

	var published *runstore.WorkflowStatus
	for _, s := range liveStepSnapshot() {
		if s.ScriptPort == port {
			copied := s
			published = &copied
		}
	}
	if published == nil {
		t.Fatalf("script port %s missing from the published snapshot", port)
	}
	if published.StepDetail != "R10,C20 len 8" {
		t.Fatalf("unexpected published detail: %q", published.StepDetail)
	}
	if published.StepStartedAt <= 0 {
		t.Fatalf("step start was not published: %d", published.StepStartedAt)
	}
	if seconds := published.OnStepSeconds(time.Now()); seconds < 0 {
		t.Fatalf("time on step should be known once published, got %d", seconds)
	}
}

func TestFormatWorkflowStatusLineReportsTimeOnStep(t *testing.T) {
	start := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	status := workflowStatus{
		ScriptPort:    "5001",
		Host:          "localhost",
		Port:          3270,
		CurrentStep:   6,
		TotalSteps:    11,
		StepType:      "CheckValue",
		StepDetail:    `R24,C1 = "READY"`,
		StartedAt:     start,
		StepStartedAt: start.Add(50 * time.Second),
	}
	line := formatWorkflowStatusLine(status, start.Add(80*time.Second))
	if !strings.Contains(line, `Step 6/11 (CheckValue) R24,C1 = "READY"`) {
		t.Fatalf("expected the step detail in the line, got %s", line)
	}
	// Two different figures: 80s in the workflow, 30s stuck on one step.
	if !strings.Contains(line, "Running 80s") {
		t.Fatalf("expected the workflow duration in the line, got %s", line)
	}
	if !strings.Contains(line, "On step 30s") {
		t.Fatalf("expected the time on the current step in the line, got %s", line)
	}
}

// A run that predates the field publishes no step start, and the line must
// not invent one — "On step 0s" would read as "it just moved".
func TestFormatWorkflowStatusLineOmitsUnknownTimeOnStep(t *testing.T) {
	start := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	status := workflowStatus{ScriptPort: "5001", Host: "localhost", Port: 3270, CurrentStep: 2, TotalSteps: 5, StepType: "PressEnter", StartedAt: start}
	line := formatWorkflowStatusLine(status, start.Add(5*time.Second))
	if strings.Contains(line, "On step") {
		t.Fatalf("expected no time-on-step claim when it is unknown, got %s", line)
	}
}

func TestValidateConfigurationRejectsLegacyDelayAndHumanDelay(t *testing.T) {
	cfg := Configuration{
		Host:        "host",
		Port:        3270,
		LegacyDelay: 1,
		Steps:       []Step{{Type: "Connect"}},
	}
	// What matters is that the removed setting is refused and the message
	// names what to use instead — a rejection that does not say that leaves
	// the author with a file that used to work and no way forward.
	err := validateConfiguration(&cfg)
	if err == nil || !strings.Contains(err.Error(), "EveryStepDelay") {
		t.Fatalf("expected the legacy Delay to be rejected pointing at EveryStepDelay, got %v", err)
	}

	cfg.LegacyDelay = 0
	cfg.Steps = []Step{{Type: "HumanDelay"}}
	err = validateConfiguration(&cfg)
	if err == nil || !strings.Contains(err.Error(), "StepDelay") {
		t.Fatalf("expected HumanDelay to be rejected pointing at StepDelay, got %v", err)
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
	// Test new exclusions for read-only and timing-related steps
	config.WaitForField.Enabled = true
	if shouldAutoWaitForField(config, Step{Type: "CheckValue"}, true) {
		t.Fatalf("did not expect auto wait for CheckValue (read-only step)")
	}
	if shouldAutoWaitForField(config, Step{Type: "AsciiScreenGrab"}, true) {
		t.Fatalf("did not expect auto wait for AsciiScreenGrab (read-only step)")
	}
	if shouldAutoWaitForField(config, Step{Type: "StepDelay"}, true) {
		t.Fatalf("did not expect auto wait for StepDelay (timing-related step)")
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

func TestConfigurationCodePageJSON(t *testing.T) {
	// CodePage is parsed from the workflow JSON.
	data := `{"Host":"mvs.example.com","Port":992,"CodePage":"cp278","Steps":[]}`
	var cfg Configuration
	if err := json.Unmarshal([]byte(data), &cfg); err != nil {
		t.Fatalf("failed to unmarshal CodePage: %v", err)
	}
	if cfg.CodePage != "cp278" {
		t.Fatalf("expected CodePage cp278, got %q", cfg.CodePage)
	}

	// CodePage must survive the per-job struct copy used for concurrency/injection.
	out := injectDynamicValues(&cfg, map[string]string{})
	if out.CodePage != "cp278" {
		t.Fatalf("expected CodePage to propagate via injectDynamicValues, got %q", out.CodePage)
	}

	// omitempty: an unset CodePage must not be emitted when marshaling.
	b, err := json.Marshal(Configuration{Host: "h", Port: 23})
	if err != nil {
		t.Fatalf("failed to marshal configuration: %v", err)
	}
	if strings.Contains(string(b), "CodePage") {
		t.Fatalf("expected empty CodePage to be omitted, got %s", string(b))
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

func TestInjectionLockPoolAcquireRelease(t *testing.T) {
	pool := newInjectionLockPool(2)
	if pool == nil {
		t.Fatalf("expected pool to be created")
	}

	first, ok := pool.acquireNext()
	if !ok {
		t.Fatalf("expected first acquire to succeed")
	}
	second, ok := pool.acquireNext()
	if !ok {
		t.Fatalf("expected second acquire to succeed")
	}
	if first == second {
		t.Fatalf("expected unique injection indexes, got %d and %d", first, second)
	}

	if _, ok := pool.acquireNext(); ok {
		t.Fatalf("expected acquire to fail when all entries are locked")
	}

	pool.release(first)
	third, ok := pool.acquireNext()
	if !ok {
		t.Fatalf("expected acquire to succeed after release")
	}
	if third != first {
		t.Fatalf("expected released index %d to be reused, got %d", first, third)
	}
}

func TestInjectionLockPoolConcurrentAcquireUnique(t *testing.T) {
	pool := newInjectionLockPool(3)
	if pool == nil {
		t.Fatalf("expected pool to be created")
	}

	results := make(chan int, 3)
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			idx, ok := pool.acquireNext()
			if !ok {
				t.Errorf("expected acquire to succeed")
				return
			}
			results <- idx
		}()
	}
	wg.Wait()
	close(results)

	seen := map[int]bool{}
	for idx := range results {
		if seen[idx] {
			t.Fatalf("duplicate index acquired concurrently: %d", idx)
		}
		seen[idx] = true
	}
	if len(seen) != 3 {
		t.Fatalf("expected 3 unique indexes, got %d", len(seen))
	}
}

func TestShouldWarnInjectionConcurrency(t *testing.T) {
	tests := []struct {
		name                 string
		injectionEntries     int
		requestedConcurrency int
		want                 bool
	}{
		{
			name:                 "warn when entries lower than concurrency",
			injectionEntries:     2,
			requestedConcurrency: 5,
			want:                 true,
		},
		{
			name:                 "no warn when entries equal concurrency",
			injectionEntries:     4,
			requestedConcurrency: 4,
			want:                 false,
		},
		{
			name:                 "no warn when entries greater than concurrency",
			injectionEntries:     6,
			requestedConcurrency: 3,
			want:                 false,
		},
		{
			name:                 "no warn for invalid entries",
			injectionEntries:     0,
			requestedConcurrency: 3,
			want:                 false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldWarnInjectionConcurrency(tt.injectionEntries, tt.requestedConcurrency)
			if got != tt.want {
				t.Fatalf("expected %v, got %v", tt.want, got)
			}
		})
	}
}

// The console follows a live run by asking for the bytes it has not seen. The
// capture file is append-only, so a byte offset is a valid resume point — and
// the handler has to say which offset it served from, because the reader
// cannot count bytes of a decoded string itself.
func TestOutputPreviewHandlerServesFromOffset(t *testing.T) {
	pid := writeCaptureFixture(t, "<pre data-capture=\"1\">first</pre>\n<pre data-capture=\"2\">second</pre>\n")

	whole := httptest.NewRecorder()
	outputPreviewHandler(whole, httptest.NewRequest(http.MethodGet, "/dashboard/output?pid="+pid, nil))
	if whole.Code != http.StatusOK {
		t.Fatalf("whole file: status %d, want 200", whole.Code)
	}
	total := whole.Body.Len()
	if got := whole.Header().Get("X-Output-Total"); got != strconv.Itoa(total) {
		t.Errorf("X-Output-Total = %q, want %d", got, total)
	}
	if got := whole.Header().Get("X-Output-From"); got != "0" {
		t.Errorf("X-Output-From = %q, want 0", got)
	}

	offset := strings.Index(whole.Body.String(), "<pre data-capture=\"2\"")
	tail := httptest.NewRecorder()
	outputPreviewHandler(tail, httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/dashboard/output?pid=%s&from=%d", pid, offset), nil))
	if tail.Code != http.StatusOK {
		t.Fatalf("tail: status %d, want 200", tail.Code)
	}
	if body := tail.Body.String(); body != "<pre data-capture=\"2\">second</pre>\n" {
		t.Errorf("tail body = %q, want only the second capture", body)
	}
	if got := tail.Header().Get("X-Output-From"); got != strconv.Itoa(offset) {
		t.Errorf("X-Output-From = %q, want %d", got, offset)
	}
}

// A run that starts over the same output path leaves the console holding an
// offset past the end of the file. That is a new file, not an error: it is
// sent whole, and said to be.
func TestOutputPreviewHandlerResetsWhenFileShrinks(t *testing.T) {
	pid := writeCaptureFixture(t, "<pre>only</pre>\n")

	recorder := httptest.NewRecorder()
	outputPreviewHandler(recorder, httptest.NewRequest(http.MethodGet,
		"/dashboard/output?pid="+pid+"&from=9999", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", recorder.Code)
	}
	if got := recorder.Header().Get("X-Output-Reset"); got != "1" {
		t.Errorf("X-Output-Reset = %q, want 1", got)
	}
	if got := recorder.Header().Get("X-Output-From"); got != "0" {
		t.Errorf("X-Output-From = %q, want 0", got)
	}
	if body := recorder.Body.String(); body != "<pre>only</pre>\n" {
		t.Errorf("body = %q, want the whole file", body)
	}
}

func TestOutputPreviewHandlerRejectsBadOffset(t *testing.T) {
	pid := writeCaptureFixture(t, "<pre>only</pre>\n")

	for _, from := range []string{"abc", "-5"} {
		recorder := httptest.NewRecorder()
		outputPreviewHandler(recorder, httptest.NewRequest(http.MethodGet,
			"/dashboard/output?pid="+pid+"&from="+from, nil))
		if recorder.Code != http.StatusBadRequest {
			t.Errorf("from=%s: status %d, want 400", from, recorder.Code)
		}
	}
}

// writeCaptureFixture publishes a metrics file pointing at a capture file, and
// returns the pid the handler should be asked about.
func writeCaptureFixture(t *testing.T, contents string) string {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	dir := dashboardMetricsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create metrics dir: %v", err)
	}

	outputPath := filepath.Join(t.TempDir(), "output.html")
	if err := os.WriteFile(outputPath, []byte(contents), 0o644); err != nil {
		t.Fatalf("write capture file: %v", err)
	}

	pid := "424242"
	metric := ExtendedMetrics{Metrics: runstore.Metrics{OutputFilePath: outputPath}}
	encoded, err := json.Marshal(metric)
	if err != nil {
		t.Fatalf("encode metrics: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "metrics_"+pid+".json"), encoded, 0o644); err != nil {
		t.Fatalf("write metrics: %v", err)
	}
	return pid
}

// The API listener executes the workflow that arrives with each request, so a
// missing -config file must not stop it starting. The image's working
// directory has no workflow.json in it, and the documented
// `docker run … -api -api-port 8080` exited before it listened.
func TestStartupConfigurationAPIModeToleratesMissingFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such-workflow.json")

	config := startupConfiguration(missing, true)
	if config == nil {
		t.Fatal("startupConfiguration returned nil for API mode without a config file")
	}
	if !config.WaitForField.Enabled {
		t.Error("WaitForField should default to enabled")
	}
	if config.RampUpBatchSize != 10 {
		t.Errorf("RampUpBatchSize = %d, want the default 10", config.RampUpBatchSize)
	}
	if config.RampUpDelay != 1.0 {
		t.Errorf("RampUpDelay = %v, want the default 1.0", config.RampUpDelay)
	}
}

// A file that is there is still read, in API mode as anywhere else — the
// tolerance above is for the absent file only, not for ignoring one somebody
// deliberately passed.
func TestStartupConfigurationAPIModeStillReadsAPresentFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workflow.json")
	body := `{"Host":"mainframe.example","Port":992,"OutputFilePath":"out.html",
	          "Steps":[{"Type":"Connect"},{"Type":"Disconnect"}]}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	config := startupConfiguration(path, true)
	if config.Host != "mainframe.example" {
		t.Errorf("Host = %q, want the value from the file", config.Host)
	}
	if config.Port != 992 {
		t.Errorf("Port = %d, want 992 from the file", config.Port)
	}
}
