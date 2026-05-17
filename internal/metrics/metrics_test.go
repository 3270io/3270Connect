package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// TestMetricsEndpointExposesAllFamilies confirms the four collectors defined
// in metrics.go appear in the /metrics output once observations have been
// recorded. The init() function in metrics.go registers them with the
// default registry, so promhttp.Handler() picks them up.
func TestMetricsEndpointExposesAllFamilies(t *testing.T) {
	// Drive each collector at least once so it appears in the output.
	ObserveConnectDuration(120 * time.Millisecond)
	ObserveStep("FillString", 5*time.Millisecond)
	IncWorkflow("success")
	WorkerStarted()
	defer WorkerStopped()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	promhttp.Handler().ServeHTTP(rec, req)

	body := rec.Body.String()
	required := []string{
		"tn3270_connect_seconds",
		"tn3270_step_seconds",
		"tn3270_workflow_total",
		"tn3270_concurrent_workers",
	}
	for _, name := range required {
		if !strings.Contains(body, name) {
			t.Errorf("metrics output missing %q\nbody (first 500 chars): %s", name, truncate(body, 500))
		}
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
