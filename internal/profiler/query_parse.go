package profiler

import (
	"strconv"
	"strings"
)

// applyQueryHost parses Query(Host). See the equivalent function in the
// 3270Web profiler for the format catalogue; the two implementations must
// agree on parsing rules so cross-tool diff'ing stays meaningful.
func applyQueryHost(p *CompatibilityProfile, resp string) {
	resp = strings.TrimSpace(resp)
	if resp == "" {
		return
	}
	fields := strings.Fields(resp)
	if len(fields) == 0 {
		return
	}
	switch strings.ToLower(fields[0]) {
	case "host", "connect":
		fields = fields[1:]
	}
	if len(fields) == 0 {
		return
	}
	hostField := fields[0]
	if strings.Contains(hostField, ":") {
		parts := strings.SplitN(hostField, ":", 2)
		hostField = parts[0]
		if port, err := strconv.Atoi(parts[1]); err == nil && p.Host.Port == 0 {
			p.Host.Port = port
		}
	}
	if p.Host.Host == "" {
		p.Host.Host = hostField
	}
	for _, f := range fields[1:] {
		lower := strings.ToLower(f)
		if lower == "tls" || lower == "ssl" || lower == "secure" {
			p.Host.TLS = true
			continue
		}
		if port, err := strconv.Atoi(f); err == nil && p.Host.Port == 0 {
			p.Host.Port = port
		}
	}
}

func applyQueryConnectionState(p *CompatibilityProfile, resp string) {
	resp = strings.TrimSpace(strings.ToLower(resp))
	if resp == "" {
		return
	}
	if strings.Contains(resp, "tn3270e") {
		p.Protocol.TN3270E = true
		p.Protocol.StructuredFields = true
	}
}

func applyQueryBind(p *CompatibilityProfile, resp string) {
	resp = strings.TrimSpace(resp)
	if resp == "" {
		return
	}
	p.Protocol.BindImagePresent = true
	p.Protocol.StructuredFields = true
	lower := strings.ToLower(resp)
	if rows := tokenValue(lower, "rows"); rows > 0 {
		p.Device.Rows = rows
	}
	if cols := tokenValue(lower, "cols"); cols > 0 {
		p.Device.Cols = cols
	}
	if strings.Contains(lower, "alt") {
		p.Device.AltScreen = true
	}
	if strings.Contains(lower, "color") || strings.Contains(lower, "colour") {
		p.Device.Color = true
	}
	if strings.Contains(lower, "extended") || strings.Contains(lower, "ext-attr") {
		p.Device.ExtendedAttributes = true
	}
}

func applyQueryCursor(p *CompatibilityProfile, resp string) { _ = resp }

func applyQueryModel(p *CompatibilityProfile, resp string) {
	resp = strings.TrimSpace(resp)
	if resp == "" {
		return
	}
	parts := strings.Fields(resp)
	model := parts[0]
	if p.Device.TerminalType == "" {
		p.Device.TerminalType = model
	}
	lower := strings.ToLower(model)
	if strings.Contains(lower, "3279") {
		p.Device.Color = true
	}
	if strings.HasSuffix(lower, "-e") {
		p.Device.ExtendedAttributes = true
	}
}

func applyQueryBindPluName(p *CompatibilityProfile, resp string) {
	name := strings.TrimSpace(resp)
	if name == "" {
		return
	}
	p.Protocol.LUName = name
}

func applyQueryTn3270eFunctions(p *CompatibilityProfile, resp string) {
	if strings.TrimSpace(resp) == "" {
		return
	}
	seen := make(map[string]struct{})
	var out []string
	for _, field := range strings.Fields(strings.ReplaceAll(resp, ",", " ")) {
		f := strings.ToUpper(strings.TrimSpace(field))
		if f == "" {
			continue
		}
		if _, ok := seen[f]; ok {
			continue
		}
		seen[f] = struct{}{}
		out = append(out, f)
	}
	if len(out) > 0 {
		p.Protocol.NegotiatedFunctions = out
		p.Protocol.TN3270E = true
		p.Protocol.StructuredFields = true
	}
}

func applyQueryScreenCurSize(p *CompatibilityProfile, resp string) {
	parts := strings.Fields(resp)
	if len(parts) < 2 {
		return
	}
	if rows, err := strconv.Atoi(parts[0]); err == nil && p.Device.Rows == 0 {
		p.Device.Rows = rows
	}
	if cols, err := strconv.Atoi(parts[1]); err == nil && p.Device.Cols == 0 {
		p.Device.Cols = cols
	}
}

func tokenValue(s, label string) int {
	idx := strings.Index(s, label)
	if idx < 0 {
		return 0
	}
	rest := strings.TrimLeft(s[idx+len(label):], " \t=:")
	if rest == "" {
		return 0
	}
	end := len(rest)
	for i, r := range rest {
		if r < '0' || r > '9' {
			end = i
			break
		}
	}
	if end == 0 {
		return 0
	}
	n, err := strconv.Atoi(rest[:end])
	if err != nil {
		return 0
	}
	return n
}
