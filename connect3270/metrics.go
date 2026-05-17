package connect3270

import "time"

// MetricsObserver hooks let the main package wire Prometheus collectors
// without dragging the prometheus dependency into this lower-level
// emulator package. The defaults are no-ops, so callers that do not
// configure metrics pay nothing.
type MetricsObserver struct {
	// ConnectDuration is called once per successful Connect() with the
	// elapsed wall-clock time.
	ConnectDuration func(time.Duration)
}

var activeMetrics MetricsObserver

// SetMetricsObserver registers the metrics hooks. Safe to call once at
// startup; not safe to swap while connects are in flight.
func SetMetricsObserver(o MetricsObserver) {
	activeMetrics = o
}

func observeConnectDuration(d time.Duration) {
	if activeMetrics.ConnectDuration != nil {
		activeMetrics.ConnectDuration(d)
	}
}
