package metrics

import (
	"strings"
	"testing"
)

func TestRegistryRender(t *testing.T) {
	reg := NewRegistry()
	reg.IncCounter("ah_resolve_requests_total")
	reg.IncCounter("ah_artifacts_registered_total")
	reg.SetGauge("ah_gc_backlog_bytes", 512)

	rendered := reg.Render()
	if !strings.Contains(rendered, "ah_resolve_requests_total 1") {
		t.Fatalf("missing counter in render: %s", rendered)
	}
	if !strings.Contains(rendered, "ah_artifacts_registered_total 1") {
		t.Fatalf("missing artifact counter in render: %s", rendered)
	}
	if !strings.Contains(rendered, "ah_gc_backlog_bytes 512") {
		t.Fatalf("missing gauge in render: %s", rendered)
	}
}
