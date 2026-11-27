package connect3270

import "testing"

func BenchmarkParseStatus(b *testing.B) {
	const line = "U F U C(localhost) I 4 24 80 5 10 0x0 0.123"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if !ParseStatus(line).Valid {
			b.Fatal("status not parsed")
		}
	}
}
