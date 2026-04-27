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

func TestFinalizeSampleRunStoresLifecycle(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := NewService(store)
	fixedNow := time.Date(2026, 4, 21, 10, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return fixedNow }
	_, err := service.RegisterArtifact(context.Background(), domain.Artifact{
		SampleRunID:    "sample-1",
		ProducerNodeID: "parent-a",
		OutputName:     "dataset",
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
