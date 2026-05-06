package resolver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/HeaInSeo/artifact-handoff/pkg/domain"
	"github.com/HeaInSeo/artifact-handoff/pkg/inventory"
)

func TestRegisterArtifactStoresArtifactAndReturnsAvailability(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := NewService(store)

	state, err := service.RegisterArtifact(context.Background(), domain.Artifact{
		SampleRunID:    "sample-1",
		ProducerNodeID: "producer-a",
		OutputName:     "dataset",
		NodeName:       "node-a",
		URI:            "jumi://runs/run-1/nodes/producer-a/outputs/dataset",
		SizeBytes:      2048,
	})
	if err != nil {
		t.Fatalf("register artifact: %v", err)
	}
	if state != domain.AvailabilityStateLocalOnly {
		t.Fatalf("availability state = %s, want %s", state, domain.AvailabilityStateLocalOnly)
	}
	artifact, ok, err := store.GetArtifact(context.Background(), "sample-1", "producer-a", "dataset")
	if err != nil {
		t.Fatalf("get artifact: %v", err)
	}
	if !ok {
		t.Fatal("expected stored artifact")
	}
	if artifact.URI == "" {
		t.Fatal("expected artifact URI to be stored")
	}
	if artifact.CreatedAt.IsZero() {
		t.Fatal("expected CreatedAt to be filled")
	}
	if artifact.SizeBytes != 2048 {
		t.Fatalf("artifact.SizeBytes = %d, want 2048", artifact.SizeBytes)
	}
}

func TestResolveHandoffLocalReuse(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := NewService(store)
	_, err := service.RegisterArtifact(context.Background(), domain.Artifact{
		SampleRunID:    "sample-1",
		ProducerNodeID: "parent-a",
		OutputName:     "model",
		NodeName:       "node-a",
		URI:            "file:///cache/model",
		Digest:         "sha256:abc",
	})
	if err != nil {
		t.Fatalf("register artifact: %v", err)
	}

	resolved, err := service.ResolveHandoff(context.Background(), domain.Binding{
		BindingName:        "model-input",
		SampleRunID:        "sample-1",
		ProducerNodeID:     "parent-a",
		ProducerOutputName: "model",
		ConsumePolicy:      domain.ConsumePolicySameNodeThenRemote,
		Required:           true,
		ExpectedDigest:     "sha256:abc",
	}, "node-a")
	if err != nil {
		t.Fatalf("resolve handoff: %v", err)
	}
	if resolved.Decision != domain.ResolutionDecisionLocalReuse {
		t.Fatalf("expected local reuse, got %s", resolved.Decision)
	}
	if resolved.RequiresMaterialization {
		t.Fatalf("expected no materialization")
	}
}

func TestResolveHandoffRemoteFetch(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := NewService(store)
	_, err := service.RegisterArtifact(context.Background(), domain.Artifact{
		SampleRunID:    "sample-1",
		ProducerNodeID: "parent-a",
		OutputName:     "dataset",
		NodeName:       "node-a",
		URI:            "http://artifact.local/dataset",
	})
	if err != nil {
		t.Fatalf("register artifact: %v", err)
	}

	resolved, err := service.ResolveHandoff(context.Background(), domain.Binding{
		BindingName:        "dataset-input",
		SampleRunID:        "sample-1",
		ProducerNodeID:     "parent-a",
		ProducerOutputName: "dataset",
		ConsumePolicy:      domain.ConsumePolicyRemoteOK,
		Required:           true,
	}, "node-b")
	if err != nil {
		t.Fatalf("resolve handoff: %v", err)
	}
	if resolved.Decision != domain.ResolutionDecisionRemoteFetch {
		t.Fatalf("expected remote fetch, got %s", resolved.Decision)
	}
	if !resolved.RequiresMaterialization {
		t.Fatalf("expected materialization")
	}
}

func TestResolveHandoffDigestMismatchReturnsError(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := NewService(store)
	_, err := service.RegisterArtifact(context.Background(), domain.Artifact{
		SampleRunID:    "sample-digest",
		ProducerNodeID: "parent-a",
		OutputName:     "dataset",
		NodeName:       "node-a",
		URI:            "http://artifact.local/dataset",
		Digest:         "sha256:actual",
	})
	if err != nil {
		t.Fatalf("register artifact: %v", err)
	}

	_, err = service.ResolveHandoff(context.Background(), domain.Binding{
		BindingName:        "dataset-input",
		SampleRunID:        "sample-digest",
		ProducerNodeID:     "parent-a",
		ProducerOutputName: "dataset",
		ConsumePolicy:      domain.ConsumePolicyRemoteOK,
		Required:           true,
		ExpectedDigest:     "sha256:expected",
	}, "node-b")
	if err == nil {
		t.Fatal("resolve handoff error = nil, want digest mismatch error")
	}
}

func TestResolveHandoffReturnsPendingWhenProducerNotTerminalAndArtifactMissing(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := NewService(store)

	resolved, err := service.ResolveHandoff(context.Background(), domain.Binding{
		BindingName:        "dataset-input",
		SampleRunID:        "sample-pending",
		ProducerNodeID:     "parent-a",
		ProducerOutputName: "dataset",
		ConsumePolicy:      domain.ConsumePolicyRemoteOK,
		Required:           true,
	}, "node-b")
	if err != nil {
		t.Fatalf("resolve handoff: %v", err)
	}
	if resolved.Status != domain.ResolutionStatusPending {
		t.Fatalf("status = %s, want %s", resolved.Status, domain.ResolutionStatusPending)
	}
	if resolved.Decision != domain.ResolutionDecisionUnavailable {
		t.Fatalf("decision = %s, want %s", resolved.Decision, domain.ResolutionDecisionUnavailable)
	}
}

func TestResolveHandoffReturnsMissingWhenProducerSucceededButArtifactMissing(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := NewService(store)
	if err := service.NotifyNodeTerminal(context.Background(), "sample-missing", "parent-a", "Succeeded"); err != nil {
		t.Fatalf("notify terminal: %v", err)
	}

	resolved, err := service.ResolveHandoff(context.Background(), domain.Binding{
		BindingName:        "dataset-input",
		SampleRunID:        "sample-missing",
		ProducerNodeID:     "parent-a",
		ProducerOutputName: "dataset",
		ConsumePolicy:      domain.ConsumePolicyRemoteOK,
		Required:           true,
	}, "node-b")
	if err != nil {
		t.Fatalf("resolve handoff: %v", err)
	}
	if resolved.Status != domain.ResolutionStatusMissing {
		t.Fatalf("status = %s, want %s", resolved.Status, domain.ResolutionStatusMissing)
	}
	if resolved.Decision != domain.ResolutionDecisionUnavailable {
		t.Fatalf("decision = %s, want %s", resolved.Decision, domain.ResolutionDecisionUnavailable)
	}
}

func TestResolveHandoffReturnsProducerFailedWhenProducerFailedAndArtifactMissing(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := NewService(store)
	if err := service.NotifyNodeTerminal(context.Background(), "sample-producer-failed", "parent-a", "Failed"); err != nil {
		t.Fatalf("notify terminal: %v", err)
	}

	resolved, err := service.ResolveHandoff(context.Background(), domain.Binding{
		BindingName:        "dataset-input",
		SampleRunID:        "sample-producer-failed",
		ProducerNodeID:     "parent-a",
		ProducerOutputName: "dataset",
		ConsumePolicy:      domain.ConsumePolicyRemoteOK,
		Required:           true,
	}, "node-b")
	if err != nil {
		t.Fatalf("resolve handoff: %v", err)
	}
	if resolved.Status != domain.ResolutionStatusMissing {
		t.Fatalf("status = %s, want %s", resolved.Status, domain.ResolutionStatusMissing)
	}
	if resolved.Decision != domain.ResolutionDecisionProducerFailed {
		t.Fatalf("decision = %s, want %s", resolved.Decision, domain.ResolutionDecisionProducerFailed)
	}
}

func TestResolveHandoffReturnsMissingWhenSampleAlreadyGCEligible(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := NewService(store)
	baseNow := time.Date(2026, 4, 21, 10, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return baseNow }
	if _, err := service.RegisterArtifact(context.Background(), domain.Artifact{
		SampleRunID:    "sample-gc",
		ProducerNodeID: "parent-a",
		OutputName:     "dataset",
		NodeName:       "node-a",
		URI:            "http://artifact.local/dataset",
	}); err != nil {
		t.Fatalf("register artifact: %v", err)
	}
	if err := service.NotifyNodeTerminal(context.Background(), "sample-gc", "parent-a", "Succeeded"); err != nil {
		t.Fatalf("notify terminal: %v", err)
	}
	if err := service.FinalizeSampleRun(context.Background(), "sample-gc"); err != nil {
		t.Fatalf("finalize sample run: %v", err)
	}
	service.now = func() time.Time { return baseNow.Add(16 * time.Minute) }
	if err := service.EvaluateGC(context.Background(), "sample-gc"); err != nil {
		t.Fatalf("evaluate gc: %v", err)
	}

	resolved, err := service.ResolveHandoff(context.Background(), domain.Binding{
		BindingName:        "dataset-input",
		SampleRunID:        "sample-gc",
		ProducerNodeID:     "parent-a",
		ProducerOutputName: "dataset",
		ConsumePolicy:      domain.ConsumePolicyRemoteOK,
		Required:           true,
	}, "node-b")
	if err != nil {
		t.Fatalf("resolve handoff: %v", err)
	}
	if resolved.Status != domain.ResolutionStatusMissing {
		t.Fatalf("status = %s, want %s", resolved.Status, domain.ResolutionStatusMissing)
	}
	if resolved.Decision != domain.ResolutionDecisionUnavailable {
		t.Fatalf("decision = %s, want %s", resolved.Decision, domain.ResolutionDecisionUnavailable)
	}
}

func TestNotifyNodeTerminalRecordsState(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := NewService(store)

	if err := service.NotifyNodeTerminal(context.Background(), "sample-1", "child-a", "Succeeded"); err != nil {
		t.Fatalf("notify terminal: %v", err)
	}
	record, ok, err := store.GetNodeTerminal(context.Background(), "sample-1", "child-a")
	if err != nil {
		t.Fatalf("get node terminal: %v", err)
	}
	if !ok {
		t.Fatalf("expected terminal record")
	}
	if record.TerminalState != "Succeeded" {
		t.Fatalf("unexpected terminal state: %s", record.TerminalState)
	}
}

func TestNotifyNodeTerminalRejectsUnsupportedState(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := NewService(store)

	if err := service.NotifyNodeTerminal(context.Background(), "sample-1", "child-a", "Running"); err == nil {
		t.Fatal("expected unsupported terminal state to fail")
	}
}

func TestFinalizeSampleRunStoresLifecycle(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := NewService(store)
	fixedNow := time.Date(2026, 4, 21, 10, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return fixedNow }
	_, err := service.RegisterArtifact(context.Background(), domain.Artifact{
		SampleRunID:    "sample-1",
		ProducerNodeID: "parent-a",
		OutputName:     "dataset",
		SizeBytes:      2048,
	})
	if err != nil {
		t.Fatalf("register artifact: %v", err)
	}
	if err := service.NotifyNodeTerminal(context.Background(), "sample-1", "parent-a", "Succeeded"); err != nil {
		t.Fatalf("notify terminal: %v", err)
	}
	if err := service.FinalizeSampleRun(context.Background(), "sample-1"); err != nil {
		t.Fatalf("finalize sample run: %v", err)
	}
	lifecycle, ok, err := service.GetSampleRunLifecycle(context.Background(), "sample-1")
	if err != nil {
		t.Fatalf("get lifecycle: %v", err)
	}
	if !ok {
		t.Fatal("expected lifecycle")
	}
	if !lifecycle.Finalized {
		t.Fatal("expected finalized lifecycle")
	}
	if lifecycle.RetentionPolicySource != "service_default" {
		t.Fatalf("retentionPolicySource = %q, want service_default", lifecycle.RetentionPolicySource)
	}
	if lifecycle.RetentionDuration != 15*time.Minute {
		t.Fatalf("retentionDuration = %s, want 15m", lifecycle.RetentionDuration)
	}
	if lifecycle.RetentionUntil == nil || !lifecycle.RetentionUntil.Equal(fixedNow.Add(15*time.Minute)) {
		t.Fatalf("retentionUntil = %v, want %v", lifecycle.RetentionUntil, fixedNow.Add(15*time.Minute))
	}
	if lifecycle.RetainedArtifactCount != 1 {
		t.Fatalf("retainedArtifactCount = %d, want 1", lifecycle.RetainedArtifactCount)
	}
	if lifecycle.RetainedArtifactBytes != 2048 {
		t.Fatalf("retainedArtifactBytes = %d, want 2048", lifecycle.RetainedArtifactBytes)
	}
	if lifecycle.TerminalNodeCount != 1 {
		t.Fatalf("terminalNodeCount = %d, want 1", lifecycle.TerminalNodeCount)
	}
	if lifecycle.SucceededNodeCount != 1 {
		t.Fatalf("succeededNodeCount = %d, want 1", lifecycle.SucceededNodeCount)
	}
	if lifecycle.GCBlockedReason != "gc_not_evaluated" {
		t.Fatalf("gcBlockedReason = %q, want gc_not_evaluated", lifecycle.GCBlockedReason)
	}
}

func TestEvaluateGCSetsEligibility(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := NewService(store)
	baseNow := time.Date(2026, 4, 21, 10, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return baseNow }
	if _, err := service.RegisterArtifact(context.Background(), domain.Artifact{
		SampleRunID:    "sample-1",
		ProducerNodeID: "parent-a",
		OutputName:     "dataset",
		SizeBytes:      2048,
	}); err != nil {
		t.Fatalf("register artifact: %v", err)
	}
	if err := service.NotifyNodeTerminal(context.Background(), "sample-1", "parent-a", "Succeeded"); err != nil {
		t.Fatalf("notify terminal: %v", err)
	}
	if err := service.FinalizeSampleRun(context.Background(), "sample-1"); err != nil {
		t.Fatalf("finalize sample run: %v", err)
	}
	service.now = func() time.Time { return baseNow.Add(16 * time.Minute) }
	if err := service.EvaluateGC(context.Background(), "sample-1"); err != nil {
		t.Fatalf("evaluate gc: %v", err)
	}
	lifecycle, ok, err := service.GetSampleRunLifecycle(context.Background(), "sample-1")
	if err != nil {
		t.Fatalf("get lifecycle: %v", err)
	}
	if !ok {
		t.Fatal("expected lifecycle")
	}
	if !lifecycle.GCEligible {
		t.Fatal("expected gc eligible lifecycle")
	}
	if lifecycle.GCBlockedReason != "" {
		t.Fatalf("gcBlockedReason = %q, want empty", lifecycle.GCBlockedReason)
	}
	if lifecycle.RetainedArtifactBytes != 2048 {
		t.Fatalf("retainedArtifactBytes = %d, want 2048", lifecycle.RetainedArtifactBytes)
	}
	if rendered := service.Metrics().Render(); !strings.Contains(rendered, "ah_gc_backlog_bytes 2048") {
		t.Fatalf("expected gc backlog gauge to reflect eligible retained artifact, got %s", rendered)
	}
}

func TestEvaluateGCBlocksWhenTerminalNodesMissing(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := NewService(store)
	if _, err := service.RegisterArtifact(context.Background(), domain.Artifact{
		SampleRunID:    "sample-2",
		ProducerNodeID: "parent-a",
		OutputName:     "dataset",
	}); err != nil {
		t.Fatalf("register artifact: %v", err)
	}
	if err := service.FinalizeSampleRun(context.Background(), "sample-2"); err != nil {
		t.Fatalf("finalize sample run: %v", err)
	}
	if err := service.EvaluateGC(context.Background(), "sample-2"); err != nil {
		t.Fatalf("evaluate gc: %v", err)
	}
	lifecycle, ok, err := service.GetSampleRunLifecycle(context.Background(), "sample-2")
	if err != nil {
		t.Fatalf("get lifecycle: %v", err)
	}
	if !ok {
		t.Fatal("expected lifecycle")
	}
	if lifecycle.GCEligible {
		t.Fatal("expected gc to remain blocked")
	}
	if lifecycle.GCBlockedReason != "terminal_nodes_missing" {
		t.Fatalf("gcBlockedReason = %q, want terminal_nodes_missing", lifecycle.GCBlockedReason)
	}
}

func TestEvaluateGCBlocksWhenSampleRunNotFinalized(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := NewService(store)
	if err := service.EvaluateGC(context.Background(), "sample-unfinalized"); err != nil {
		t.Fatalf("evaluate gc: %v", err)
	}
	lifecycle, ok, err := service.GetSampleRunLifecycle(context.Background(), "sample-unfinalized")
	if err != nil {
		t.Fatalf("get lifecycle: %v", err)
	}
	if !ok {
		t.Fatal("expected lifecycle")
	}
	if lifecycle.GCEligible {
		t.Fatal("expected gc to remain blocked when sample run is not finalized")
	}
	if lifecycle.GCBlockedReason != "sample_run_not_finalized" {
		t.Fatalf("gcBlockedReason = %q, want sample_run_not_finalized", lifecycle.GCBlockedReason)
	}
}

func TestEvaluateGCBlocksWhenRetentionWindowActive(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := NewService(store)
	baseNow := time.Date(2026, 4, 21, 10, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return baseNow }
	if _, err := service.RegisterArtifact(context.Background(), domain.Artifact{
		SampleRunID:    "sample-3",
		ProducerNodeID: "parent-a",
		OutputName:     "dataset",
	}); err != nil {
		t.Fatalf("register artifact: %v", err)
	}
	if err := service.NotifyNodeTerminal(context.Background(), "sample-3", "parent-a", "Succeeded"); err != nil {
		t.Fatalf("notify terminal: %v", err)
	}
	if err := service.FinalizeSampleRun(context.Background(), "sample-3"); err != nil {
		t.Fatalf("finalize sample run: %v", err)
	}
	service.now = func() time.Time { return baseNow.Add(5 * time.Minute) }
	if err := service.EvaluateGC(context.Background(), "sample-3"); err != nil {
		t.Fatalf("evaluate gc: %v", err)
	}
	lifecycle, ok, err := service.GetSampleRunLifecycle(context.Background(), "sample-3")
	if err != nil {
		t.Fatalf("get lifecycle: %v", err)
	}
	if !ok {
		t.Fatal("expected lifecycle")
	}
	if lifecycle.GCEligible {
		t.Fatal("expected gc to remain blocked during retention window")
	}
	if lifecycle.GCBlockedReason != "retention_window_active" {
		t.Fatalf("gcBlockedReason = %q, want retention_window_active", lifecycle.GCBlockedReason)
	}
	if rendered := service.Metrics().Render(); !strings.Contains(rendered, "ah_gc_backlog_bytes 0") {
		t.Fatalf("expected gc backlog gauge to stay zero while blocked, got %s", rendered)
	}
}

func TestEvaluateGCRefreshesStaleLifecycleSnapshot(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := NewService(store)
	baseNow := time.Date(2026, 4, 21, 10, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return baseNow }
	if _, err := service.RegisterArtifact(context.Background(), domain.Artifact{
		SampleRunID:    "sample-stale",
		ProducerNodeID: "parent-a",
		OutputName:     "dataset",
		SizeBytes:      3072,
	}); err != nil {
		t.Fatalf("register artifact: %v", err)
	}
	if err := service.NotifyNodeTerminal(context.Background(), "sample-stale", "parent-a", "Succeeded"); err != nil {
		t.Fatalf("notify terminal: %v", err)
	}
	stale := domain.SampleRunLifecycle{
		SampleRunID:           "sample-stale",
		Finalized:             true,
		RetentionPolicySource: "stale",
		RetentionDuration:     15 * time.Minute,
		RetentionUntil:        timePtr(baseNow.Add(15 * time.Minute)),
		RetainedArtifactCount: 0,
		TerminalNodeCount:     0,
	}
	if err := store.UpsertSampleRunLifecycle(context.Background(), stale); err != nil {
		t.Fatalf("upsert stale lifecycle: %v", err)
	}
	service.now = func() time.Time { return baseNow.Add(16 * time.Minute) }
	if err := service.EvaluateGC(context.Background(), "sample-stale"); err != nil {
		t.Fatalf("evaluate gc: %v", err)
	}
	lifecycle, ok, err := service.GetSampleRunLifecycle(context.Background(), "sample-stale")
	if err != nil {
		t.Fatalf("get lifecycle: %v", err)
	}
	if !ok {
		t.Fatal("expected lifecycle")
	}
	if lifecycle.RetainedArtifactCount != 1 {
		t.Fatalf("retainedArtifactCount = %d, want 1", lifecycle.RetainedArtifactCount)
	}
	if lifecycle.RetainedArtifactBytes != 3072 {
		t.Fatalf("retainedArtifactBytes = %d, want 3072", lifecycle.RetainedArtifactBytes)
	}
	if lifecycle.TerminalNodeCount != 1 {
		t.Fatalf("terminalNodeCount = %d, want 1", lifecycle.TerminalNodeCount)
	}
	if !lifecycle.GCEligible {
		t.Fatal("expected gc eligibility after refreshed snapshot")
	}
}

func TestHTTPRegisterArtifact(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := NewService(store)
	handler := NewHTTPHandler(service)

	req := httptest.NewRequest(http.MethodPost, "/v1/artifacts:register", strings.NewReader(`{
		"sampleRunID":"sample-http",
		"producerNodeID":"producer-a",
		"outputName":"result.json",
		"nodeName":"node-a",
		"uri":"jumi://runs/run-http/nodes/producer-a/outputs/result.json"
	}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("register status = %d, want 200", resp.Code)
	}
	var body struct {
		AvailabilityState string `json:"availabilityState"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if body.AvailabilityState != string(domain.AvailabilityStateLocalOnly) {
		t.Fatalf("availabilityState = %q, want %q", body.AvailabilityState, domain.AvailabilityStateLocalOnly)
	}
}

func TestHTTPRegisterArtifactAcceptsEnvelope(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := NewService(store)
	handler := NewHTTPHandler(service)

	req := httptest.NewRequest(http.MethodPost, "/v1/artifacts:register", strings.NewReader(`{
		"artifact": {
			"sampleRunId":"sample-http-env",
			"producerNodeId":"producer-a",
			"outputName":"result.json",
			"artifactId":"sample-http-env:producer-a:result.json",
			"nodeName":"node-a",
			"uri":"jumi://runs/run-http/nodes/producer-a/outputs/result.json"
		}
	}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("register status = %d, want 200", resp.Code)
	}
	artifact, ok, err := store.GetArtifact(context.Background(), "sample-http-env", "producer-a", "result.json")
	if err != nil {
		t.Fatalf("get artifact: %v", err)
	}
	if !ok {
		t.Fatal("expected stored artifact")
	}
	if artifact.ArtifactID != "sample-http-env:producer-a:result.json" {
		t.Fatalf("artifactId = %q, want sample-http-env:producer-a:result.json", artifact.ArtifactID)
	}
}

func TestHTTPGetArtifact(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := NewService(store)
	handler := NewHTTPHandler(service)

	if _, err := service.RegisterArtifact(context.Background(), domain.Artifact{
		SampleRunID:    "sample-http-get",
		ProducerNodeID: "producer-a",
		OutputName:     "report",
		ArtifactID:     "sample-http-get:producer-a:report",
		Digest:         "sha256:abc123",
		NodeName:       "node-a",
		URI:            "jumi://runs/run-http-get/nodes/producer-a/outputs/report",
		SizeBytes:      4096,
	}); err != nil {
		t.Fatalf("register artifact: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/artifacts:get?sampleRunId=sample-http-get&producerNodeId=producer-a&outputName=report", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("get artifact status = %d, want 200", resp.Code)
	}
	var body struct {
		Artifact domain.Artifact `json:"artifact"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if body.Artifact.Digest != "sha256:abc123" {
		t.Fatalf("artifact digest = %q, want sha256:abc123", body.Artifact.Digest)
	}
	if body.Artifact.SizeBytes != 4096 {
		t.Fatalf("artifact sizeBytes = %d, want 4096", body.Artifact.SizeBytes)
	}
}

func TestHTTPGetArtifactReturnsNotFound(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := NewService(store)
	handler := NewHTTPHandler(service)

	req := httptest.NewRequest(http.MethodGet, "/v1/artifacts:get?sampleRunId=missing&producerNodeId=producer-a&outputName=report", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("get artifact status = %d, want 404", resp.Code)
	}
}

func TestHTTPListArtifactsBySampleRun(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := NewService(store)
	handler := NewHTTPHandler(service)

	for _, artifact := range []domain.Artifact{
		{
			SampleRunID:    "sample-http-list",
			ProducerNodeID: "producer-a",
			OutputName:     "report",
			ArtifactID:     "sample-http-list:producer-a:report",
			Digest:         "sha256:one",
			URI:            "jumi://runs/run-http-list/nodes/producer-a/outputs/report",
			SizeBytes:      512,
		},
		{
			SampleRunID:    "sample-http-list",
			ProducerNodeID: "producer-b",
			OutputName:     "metrics",
			ArtifactID:     "sample-http-list:producer-b:metrics",
			Digest:         "sha256:two",
			URI:            "jumi://runs/run-http-list/nodes/producer-b/outputs/metrics",
			SizeBytes:      128,
		},
	} {
		if _, err := service.RegisterArtifact(context.Background(), artifact); err != nil {
			t.Fatalf("register artifact: %v", err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/artifacts:list?sampleRunId=sample-http-list", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("list artifacts status = %d, want 200", resp.Code)
	}
	var body struct {
		Artifacts []domain.Artifact `json:"artifacts"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(body.Artifacts) != 2 {
		t.Fatalf("artifact count = %d, want 2", len(body.Artifacts))
	}
}

func timePtr(v time.Time) *time.Time {
	return &v
}

func TestHTTPFinalizeAndEvaluateGC(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := NewService(store)
	handler := NewHTTPHandler(service)

	finalizeReq := httptest.NewRequest(http.MethodPost, "/v1/sampleRuns:finalize", strings.NewReader(`{"sampleRunId":"sample-http"}`))
	finalizeReq.Header.Set("Content-Type", "application/json")
	finalizeResp := httptest.NewRecorder()
	handler.ServeHTTP(finalizeResp, finalizeReq)
	if finalizeResp.Code != http.StatusOK {
		t.Fatalf("finalize status = %d, want 200", finalizeResp.Code)
	}

	gcReq := httptest.NewRequest(http.MethodPost, "/v1/sampleRuns:evaluateGC", strings.NewReader(`{"sampleRunId":"sample-http"}`))
	gcReq.Header.Set("Content-Type", "application/json")
	gcResp := httptest.NewRecorder()
	handler.ServeHTTP(gcResp, gcReq)
	if gcResp.Code != http.StatusOK {
		t.Fatalf("evaluate gc status = %d, want 200", gcResp.Code)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/v1/sampleRuns:lifecycle?sampleRunId=sample-http", nil)
	getResp := httptest.NewRecorder()
	handler.ServeHTTP(getResp, getReq)
	if getResp.Code != http.StatusOK {
		t.Fatalf("get lifecycle status = %d, want 200", getResp.Code)
	}
	var lifecycleBody map[string]any
	if err := json.Unmarshal(getResp.Body.Bytes(), &lifecycleBody); err != nil {
		t.Fatalf("unmarshal lifecycle response: %v", err)
	}
	if _, ok := lifecycleBody["sampleRunId"]; !ok {
		t.Fatalf("expected sampleRunId key in lifecycle response: %s", getResp.Body.String())
	}
	if _, ok := lifecycleBody["gcEligible"]; !ok {
		t.Fatalf("expected gcEligible key in lifecycle response: %s", getResp.Body.String())
	}
}
