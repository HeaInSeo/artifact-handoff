package metrics

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMetricsHandlerExportsCounterAndGauge(t *testing.T) {
	m, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	m.IncArtifactsRegistered()
	m.IncResolveRequests()
	m.SetGCBacklogBytes(512)

	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))

	body := rec.Body.String()
	for _, want := range []string{
		"ah_artifacts_registered_total",
		"ah_resolve_requests_total",
		"ah_gc_backlog_bytes",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in Prometheus output:\n%s", want, body)
		}
	}
}

func TestMetricsHandlerReturns200(t *testing.T) {
	m, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))

	if rec.Code != 200 {
		t.Fatalf("handler returned %d: %s", rec.Code, rec.Body.String())
	}
}
