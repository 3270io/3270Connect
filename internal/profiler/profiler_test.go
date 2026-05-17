package profiler

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

type fakeHost struct {
	queries map[string]string
}

func (f *fakeHost) Query(arg string) (string, error) {
	if f.queries == nil {
		return "", nil
	}
	return f.queries[arg], nil
}

func TestProbe_RichResponse(t *testing.T) {
	h := &fakeHost{queries: map[string]string{
		"Host":             "host mvs.example 992 tls",
		"ConnectionState":  "tn3270e mvs",
		"Bind":             "rows 32 cols 80 alt color extended",
		"Model":            "IBM-3279-2-E",
		"BindPluName":      "LU01",
		"Tn3270eFunctions": "BIND-IMAGE RESPONSES SYSREQ",
		"ScreenCurSize":    "24 80",
		"Cursor":           "1 1",
	}}
	now := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)
	p, err := Probe(context.Background(), h, ProbeOptions{Tool: "3270Connect", Version: "test", Now: func() time.Time { return now }, CollectRaw: true, RunID: "fixed"})
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if p.SchemaVersion != SchemaVersion {
		t.Errorf("schema_version: %q", p.SchemaVersion)
	}
	if p.Profiler.Tool != "3270Connect" {
		t.Errorf("tool: %q", p.Profiler.Tool)
	}
	if p.Host.Host != "mvs.example" || p.Host.Port != 992 || !p.Host.TLS {
		t.Errorf("host identity: %+v", p.Host)
	}
	if p.Device.TerminalType != "IBM-3279-2-E" || p.Device.Rows != 32 || p.Device.Cols != 80 {
		t.Errorf("device: %+v", p.Device)
	}
	if !p.Protocol.TN3270E || !p.Protocol.BindImagePresent || !p.Protocol.StructuredFields {
		t.Errorf("protocol: %+v", p.Protocol)
	}
	if p.Protocol.LUName != "LU01" {
		t.Errorf("lu_name: %q", p.Protocol.LUName)
	}
	if len(p.Protocol.NegotiatedFunctions) != 3 {
		t.Errorf("negotiated functions: %v", p.Protocol.NegotiatedFunctions)
	}
	if len(p.Capabilities.Unknown) != 0 {
		t.Errorf("expected no unknown, got %v", p.Capabilities.Unknown)
	}
	if _, ok := p.Raw["Bind"]; !ok {
		t.Errorf("Raw[Bind] not captured")
	}
}

func TestProbe_MuteHost(t *testing.T) {
	h := &fakeHost{}
	p, err := Probe(context.Background(), h, ProbeOptions{Tool: "3270Connect", Version: "test", RunID: "mute"})
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if len(p.Capabilities.Unknown) != len(probeSequence()) {
		t.Errorf("expected all %d queries unknown, got %d: %v",
			len(probeSequence()), len(p.Capabilities.Unknown), p.Capabilities.Unknown)
	}
}

func TestProbe_StableJSONShape(t *testing.T) {
	// Cross-tool contract: the JSON shape must match 3270Web's. We assert
	// the top-level keys exactly.
	h := &fakeHost{queries: map[string]string{"Model": "3279-2"}}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p, _ := Probe(context.Background(), h, ProbeOptions{RunID: "x", Now: func() time.Time { return now }})
	b, _ := json.Marshal(p)
	var generic map[string]any
	if err := json.Unmarshal(b, &generic); err != nil {
		t.Fatalf("decode: %v", err)
	}
	wantTopKeys := []string{"schema_version", "run_id", "timestamp", "profiler", "host", "device", "protocol", "capabilities", "timing"}
	for _, k := range wantTopKeys {
		if _, ok := generic[k]; !ok {
			t.Errorf("missing top-level key %q in %s", k, b)
		}
	}
}
