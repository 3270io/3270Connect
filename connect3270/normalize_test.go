package connect3270

import (
	"strings"
	"testing"
)

func benchScreen() string {
	var b strings.Builder
	for i := 0; i < 24; i++ {
		b.WriteString("data: " + strings.Repeat("ABCD 1234 ", 8) + "\r\n")
	}
	b.WriteString("U F U C(host) I 2 24 80 4 20 0x0 0.000\n")
	return b.String()
}

func BenchmarkNormalizeDataLines(b *testing.B) {
	raw := benchScreen()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = NormalizeDataLines(raw)
	}
}

func BenchmarkNormalizeAsciiData(b *testing.B) {
	raw := "data: HELLO WORLD   \n"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = normalizeAsciiData(raw)
	}
}

// referenceNormalizeDataLines is the Split-and-Join form joinDataLines
// replaced; the test below holds the single-pass version to its output.
func referenceNormalizeDataLines(raw string) string {
	var kept []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimRight(line, "\r")
		if !strings.HasPrefix(strings.TrimSpace(line), "data:") {
			continue
		}
		value := strings.TrimPrefix(strings.TrimLeft(line, " \t"), "data:")
		kept = append(kept, strings.TrimPrefix(value, " "))
	}
	return strings.Join(kept, "\n")
}

func TestNormalizeDataLinesMatchesReference(t *testing.T) {
	cases := []string{
		"", "\n", "ok\n", "data: a\nok\n", "data: a\r\ndata:b\r\nU F U\n",
		"  data:  x \n\tdata:\n\ndata: \nerror\n", "data: last no newline",
		"\vdata: odd\ndata:\r\r\n", "noise\ndata: a\nmore\ndata: b\n",
	}
	for _, c := range cases {
		if got, want := NormalizeDataLines(c), referenceNormalizeDataLines(c); got != want {
			t.Errorf("NormalizeDataLines(%q) = %q, want %q", c, got, want)
		}
	}
}
