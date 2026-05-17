// Package metrics defines Prometheus collectors for 3270Connect's
// volume/performance run mode. Importing the package registers the
// collectors; the main binary exposes them via -promListen.
package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	ConnectSeconds = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "tn3270_connect_seconds",
		Help:    "Wall-clock time to establish a TN3270 session.",
		Buckets: prometheus.ExponentialBuckets(0.05, 2, 10),
	})

	StepSeconds = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "tn3270_step_seconds",
		Help:    "Wall-clock time per workflow step.",
		Buckets: prometheus.ExponentialBuckets(0.01, 2, 12),
	}, []string{"action"})

	WorkflowTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "tn3270_workflow_total",
		Help: "Workflows that have terminated, partitioned by outcome.",
	}, []string{"result"}) // success | failure | connect_failed

	ConcurrentWorkers = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "tn3270_concurrent_workers",
		Help: "Active workflow worker count.",
	})
)

func init() {
	prometheus.MustRegister(ConnectSeconds, StepSeconds, WorkflowTotal, ConcurrentWorkers)
}

// ObserveConnectDuration records a Connect timing. Used as the hook bridged
// into connect3270 via connect3270.SetMetricsObserver.
func ObserveConnectDuration(d time.Duration) {
	ConnectSeconds.Observe(d.Seconds())
}

// ObserveStep records the elapsed time of a workflow step.
func ObserveStep(action string, d time.Duration) {
	if action == "" {
		action = "unknown"
	}
	StepSeconds.WithLabelValues(action).Observe(d.Seconds())
}

// IncWorkflow records a workflow's outcome.
func IncWorkflow(result string) {
	if result == "" {
		result = "unknown"
	}
	WorkflowTotal.WithLabelValues(result).Inc()
}

// WorkerStarted increments the active-worker gauge.
func WorkerStarted() { ConcurrentWorkers.Inc() }

// WorkerStopped decrements the active-worker gauge.
func WorkerStopped() { ConcurrentWorkers.Dec() }
