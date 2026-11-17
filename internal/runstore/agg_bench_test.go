package runstore

import "testing"

func BenchmarkAggregate(b *testing.B) {
	entries := make([]Entry, 50)
	for i := range entries {
		m := &entries[i].Metrics
		m.Durations = make([]float64, 300)
		m.CPUUsage = make([]float64, 300)
		m.MemoryUsage = make([]float64, 300)
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Aggregate(entries)
	}
}
