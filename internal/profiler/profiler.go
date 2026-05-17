package profiler

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ProberHost is the subset of connect3270.Emulator that Probe needs. The
// emulator already satisfies this interface; tests can substitute their own
// fake.
type ProberHost interface {
	Query(arg string) (string, error)
}

// ConnectTimingSource is an optional interface that lets Probe pick up the
// elapsed time of the most recent connect. *connect3270.Emulator implements
// it.
type ConnectTimingSource interface {
	LastConnectDuration() time.Duration
}

// AsciiScreenSource is an optional interface that lets Probe capture the
// first screen for banner fingerprinting. *connect3270.Emulator implements
// it via AsciiScreenGrab(string)/AsciiScreen.
type AsciiScreenSource interface {
	AsciiScreen() (string, error)
}

// ProbeOptions tunes Probe behaviour. Zero value is safe.
type ProbeOptions struct {
	PerActionTimeout time.Duration
	CollectRaw       bool
	INDFileProbe     bool
	Tool             string
	Version          string
	Host             string
	Port             int
	TLS              bool
	RunID            string
	Now              func() time.Time
}

const defaultPerActionTimeout = 3 * time.Second

// Probe runs a read-only sequence of Query actions against h and returns a
// populated CompatibilityProfile. Queries that fail or return empty are
// recorded in Capabilities.Unknown; only context cancellation produces a
// hard error.
func Probe(ctx context.Context, h ProberHost, opts ProbeOptions) (*CompatibilityProfile, error) {
	if h == nil {
		return nil, fmt.Errorf("profiler: host is nil")
	}
	if opts.PerActionTimeout <= 0 {
		opts.PerActionTimeout = defaultPerActionTimeout
	}
	if opts.Tool == "" {
		opts.Tool = "3270Connect"
	}
	if opts.Version == "" {
		opts.Version = "dev"
	}
	now := time.Now
	if opts.Now != nil {
		now = opts.Now
	}
	runID := strings.TrimSpace(opts.RunID)
	if runID == "" {
		runID = randomRunID()
	}

	probeStart := now()
	profile := &CompatibilityProfile{
		SchemaVersion: SchemaVersion,
		RunID:         runID,
		Timestamp:     probeStart.UTC(),
		Profiler:      ProfilerMeta{Tool: opts.Tool, Version: opts.Version},
		Capabilities:  CapabilityProfile{INDFile: TriUnknown},
	}
	if opts.TLS {
		profile.Host.TLS = true
	}
	if opts.CollectRaw {
		profile.Raw = make(map[string]string)
	}

	for _, q := range probeSequence() {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		resp, err := queryWithTimeout(ctx, h, q.name, opts.PerActionTimeout)
		if err != nil || strings.TrimSpace(resp) == "" {
			profile.Capabilities.Unknown = appendUnique(profile.Capabilities.Unknown, q.name)
			continue
		}
		if profile.Raw != nil {
			profile.Raw[q.name] = resp
		}
		q.apply(profile, resp)
	}

	// First-screen capture: best-effort, only if the host provides it.
	if ass, ok := h.(AsciiScreenSource); ok {
		firstScreenStart := now()
		if screen, err := ass.AsciiScreen(); err == nil && screen != "" {
			profile.Timing.FirstScreenMS = now().Sub(firstScreenStart).Milliseconds()
			applyScreenFingerprint(profile, screen)
		}
	}

	if cts, ok := h.(ConnectTimingSource); ok {
		if d := cts.LastConnectDuration(); d > 0 {
			profile.Timing.ConnectMS = d.Milliseconds()
		}
	}

	if opts.INDFileProbe {
		profile.Capabilities.INDFile = TriUnknown
	}

	// Fall back to caller-supplied identity if Query(Host) did not answer.
	if profile.Host.Host == "" {
		profile.Host.Host = opts.Host
	}
	if profile.Host.Port == 0 {
		profile.Host.Port = opts.Port
	}

	sort.Strings(profile.Capabilities.Unknown)
	return profile, nil
}

type queryStep struct {
	name  string
	apply func(p *CompatibilityProfile, response string)
}

func probeSequence() []queryStep {
	return []queryStep{
		{"Host", applyQueryHost},
		{"ConnectionState", applyQueryConnectionState},
		{"Bind", applyQueryBind},
		{"Cursor", applyQueryCursor},
		{"Model", applyQueryModel},
		{"BindPluName", applyQueryBindPluName},
		{"Tn3270eFunctions", applyQueryTn3270eFunctions},
		{"ScreenCurSize", applyQueryScreenCurSize},
	}
}

func queryWithTimeout(ctx context.Context, h ProberHost, arg string, timeout time.Duration) (string, error) {
	type result struct {
		resp string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		resp, err := h.Query(arg)
		ch <- result{resp: resp, err: err}
	}()
	select {
	case r := <-ch:
		return r.resp, r.err
	case <-time.After(timeout):
		return "", fmt.Errorf("query %s timed out after %s", arg, timeout)
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func applyScreenFingerprint(p *CompatibilityProfile, text string) {
	preview := normaliseScreenText(text)
	if preview == "" {
		return
	}
	sum := sha256.Sum256([]byte(preview))
	p.Host.BannerSignature = hex.EncodeToString(sum[:])
	if len(preview) > 240 {
		p.Host.BannerPreview = preview[:240]
	} else {
		p.Host.BannerPreview = preview
	}
}

func appendUnique(s []string, v string) []string {
	for _, existing := range s {
		if existing == v {
			return s
		}
	}
	return append(s, v)
}

func randomRunID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("run-%d", time.Now().UnixNano())
	}
	return "run-" + hex.EncodeToString(b[:])
}

func normaliseScreenText(text string) string {
	var b strings.Builder
	b.Grow(len(text))
	prevSpace := true
	for _, r := range text {
		switch {
		case r == 0:
			r = ' '
		case r < 0x20 || r == 0x7f:
			r = ' '
		case r > 0x7e:
			r = '?'
		}
		if r == ' ' {
			if prevSpace {
				continue
			}
			prevSpace = true
		} else {
			prevSpace = false
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}
