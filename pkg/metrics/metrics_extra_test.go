package metrics

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// TestMetrics_AllCountersAndGauge exercises every Inc* method and SetGCBacklogBytes,
// then verifies they all appear in Prometheus output.
func TestMetrics_AllCountersAndGauge(t *testing.T) {
	m, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Call every exported increment method exactly once.
	m.IncArtifactsRegistered()
	m.IncResolveRequests()
	m.IncFallback()
	m.IncGRPCRegisterArtifact()
	m.IncGRPCRegisterArtifactErrors()
	m.IncGRPCResolveHandoff()
	m.IncGRPCResolveHandoffErrors()
	m.IncGRPCNotifyNodeTerminal()
	m.IncGRPCFinalizeSampleRun()
	m.IncGRPCEvaluateGC()
	m.IncGRPCGetLifecycle()
	m.SetGCBacklogBytes(1024.5)

	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))

	body := rec.Body.String()
	for _, want := range []string{
		"ah_artifacts_registered_total",
		"ah_resolve_requests_total",
		"ah_fallback_total",
		"ah_grpc_register_artifact_total",
		"ah_grpc_register_artifact_errors_total",
		"ah_grpc_resolve_handoff_total",
		"ah_grpc_resolve_handoff_errors_total",
		"ah_grpc_notify_node_terminal_total",
		"ah_grpc_finalize_sample_run_total",
		"ah_grpc_evaluate_gc_total",
		"ah_grpc_get_lifecycle_total",
		"ah_gc_backlog_bytes",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in Prometheus output", want)
		}
	}
}

// TestMetrics_Handler_StatusOK ensures Handler() returns a non-nil handler that
// produces HTTP 200.
func TestMetrics_Handler_StatusOK(t *testing.T) {
	m, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if m.Handler() == nil {
		t.Fatal("Handler() returned nil")
	}
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	if rec.Code != 200 {
		t.Fatalf("HTTP status = %d, want 200", rec.Code)
	}
}
