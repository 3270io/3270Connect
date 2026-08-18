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
	// The responses are in the shapes s3270 actually answers with, so that
	// a passing test means the parsers read a real emulator rather than an
	// invented one. That was how two queries no emulator answers went
	// unnoticed: the fake answered them.
	h := &fakeHost{queries: map[string]string{
		"Host":            "host mvs.example 992 tls",
		"ConnectionState": "connected-tn3270e",
		"Model":           "IBM-3279-4-E",
		"BindPluName":     "LU01",
		"Tn3270eOptions":  "BIND-IMAGE RESPONSES SYSREQ",
		"ScreenCurSize":   "24 80",
		"ScreenSizeMax":   "rows 43 columns 80",
		"Cursor":          "1 1",
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
	if p.Device.TerminalType != "IBM-3279-4-E" || p.Device.Rows != 24 || p.Device.Cols != 80 {
		t.Errorf("device: %+v", p.Device)
	}
	if !p.Device.AltScreen {
		t.Errorf("alt_screen: a model 4 reporting a 43-row maximum has one, got %+v", p.Device)
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
	if _, ok := p.Raw["Tn3270eOptions"]; !ok {
		t.Errorf("Raw[Tn3270eOptions] not captured")
	}
}

// TestProbeSequenceNamesRealQueries guards the failure this sequence has
// already had once: a probe naming a Query the emulator does not have looks
// exactly like a host declining to answer, so it lands in
// capabilities.unknown and stays there, on every host, forever.
//
// The list is x3270's own Query keywords. Anything added to the sequence has
// to be one of them.
func TestProbeSequenceNamesRealQueries(t *testing.T) {
	known := map[string]bool{
		"About": true, "Actions": true, "BindPluName": true, "BuildOptions": true,
		"CodePage": true, "CodePages": true, "ConnectTime": true, "ConnectionState": true,
		"Copyright": true, "Cursor": true, "Cursor1": true, "Formatted": true,
		"Host": true, "LocalEncoding": true, "LuName": true, "Model": true,
		"Prefixes": true, "Proxies": true, "Proxy": true, "ScreenCurSize": true,
		"ScreenSizeCurrent": true, "ScreenSizeMax": true, "ScreenTraceFile": true,
		"StatsRx": true, "StatsTx": true, "Tasks": true, "TelnetMyOptions": true,
		"TelnetHostOptions": true, "TerminalName": true, "Tls": true,
		"TlsCertInfo": true, "TlsProvider": true, "TlsSessionInfo": true,
		"TlsSubjectNames": true, "Tn3270eOptions": true, "TraceFile": true,
		"Version": true,
	}
	for _, q := range probeSequence() {
		if !known[q.name] {
			t.Errorf("probe queries %q, which x3270 does not answer; every host will report it as unknown", q.name)
		}
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
