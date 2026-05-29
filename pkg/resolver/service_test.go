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

func newTestService(t testing.TB, store inventory.Store) *Service {
	t.Helper()
	t.Setenv("AH_ALLOW_ANY_HTTP_SOURCE", "true")
	svc, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func scrapeMetrics(t *testing.T, h http.Handler) string {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	return rec.Body.String()
}

func hasMetricValue(body, metricName, value string) bool {
	suffix := " " + value
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, metricName) && strings.HasSuffix(line, suffix) {
			return true
		}
	}
	return false
}

func TestRegisterArtifactStoresArtifactAndReturnsAvailability(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := newTestService(t, store)

	state, err := service.RegisterArtifact(context.Background(), domain.Artifact{
		SampleRunID:       "sample-1",
		ProducerNodeID:    "producer-a",
		ProducerAttemptID: "attempt-1",
		OutputName:        "dataset",
		NodeName:          "node-a",
		URI:               "jumi://runs/run-1/nodes/producer-a/outputs/dataset",
		SizeBytes:         2048,
	})
	if err != nil {
		t.Fatalf("register artifact: %v", err)
	}
	if state != domain.AvailabilityStateBoth {
		t.Fatalf("availability state = %s, want %s", state, domain.AvailabilityStateBoth)
	}
	artifact, ok, err := store.GetArtifact(context.Background(), "sample-1", "producer-a", "attempt-1", "dataset")
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
	service := newTestService(t, store)
	_, err := service.RegisterArtifact(context.Background(), domain.Artifact{
		SampleRunID:       "sample-1",
		ProducerNodeID:    "parent-a",
		ProducerAttemptID: "attempt-1",
		OutputName:        "model",
		NodeName:          "node-a",
		Digest:            "sha256:abc",
		Locations: []domain.Location{{
			NodeLocal: &domain.NodeLocalLocation{NodeName: "node-a", Path: "/var/lib/jumi-artifacts/cas/sha256/abc"},
		}},
	})
	if err != nil {
		t.Fatalf("register artifact: %v", err)
	}

	resolved, err := service.ResolveHandoff(context.Background(), domain.Binding{
		BindingName:        "model-input",
		SampleRunID:        "sample-1",
		ProducerNodeID:     "parent-a",
		ProducerAttemptID:  "attempt-1",
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
	if resolved.PlacementIntent.Mode != domain.PlacementIntentModePreferredNode {
		t.Fatalf("placement intent mode = %s, want preferred_node", resolved.PlacementIntent.Mode)
	}
	if resolved.PlacementIntent.NodeName != "node-a" {
		t.Fatalf("placement intent node = %s, want node-a", resolved.PlacementIntent.NodeName)
	}
	if resolved.MaterializationPlan.Mode != domain.MaterializationModeLocalReuse {
		t.Fatalf("materialization mode = %s, want local_reuse", resolved.MaterializationPlan.Mode)
	}
	if resolved.MaterializationPlan.SourceLocation == nil || resolved.MaterializationPlan.SourceLocation.NodeLocal == nil {
		t.Fatalf("sourceLocation = %#v, want nodeLocal source", resolved.MaterializationPlan.SourceLocation)
	}
	if resolved.MaterializationPlan.SourceLocation.NodeLocal.Path != "/var/lib/jumi-artifacts/cas/sha256/abc" {
		t.Fatalf("nodeLocal.path = %q, want node-local CAS path", resolved.MaterializationPlan.SourceLocation.NodeLocal.Path)
	}
}

func TestRegisterArtifactStoresLogicalURIAndLocations(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := newTestService(t, store)

	_, err := service.RegisterArtifact(context.Background(), domain.Artifact{
		SampleRunID:       "sample-logical",
		ProducerNodeID:    "producer-a",
		ProducerAttemptID: "attempt-2",
		OutputName:        "dataset",
		LogicalURI:        "jumi://runs/sample-logical/nodes/producer-a/outputs/dataset",
		NodeName:          "node-a",
		Digest:            "sha256:abc",
		Locations: []domain.Location{{
			NodeLocal: &domain.NodeLocalLocation{NodeName: "node-a", Path: "/var/lib/jumi-artifacts/cas/sha256/abc"},
		}},
	})
	if err != nil {
		t.Fatalf("register artifact: %v", err)
	}

	artifact, ok, err := store.GetArtifact(context.Background(), "sample-logical", "producer-a", "attempt-2", "dataset")
	if err != nil {
		t.Fatalf("get artifact: %v", err)
	}
	if !ok {
		t.Fatal("expected stored artifact")
	}
	if artifact.LogicalURI != "jumi://runs/sample-logical/nodes/producer-a/outputs/dataset" {
		t.Fatalf("logicalUri = %q, want logical URI", artifact.LogicalURI)
	}
	if len(artifact.Locations) != 1 || artifact.Locations[0].NodeLocal == nil {
		t.Fatalf("locations = %#v, want one nodeLocal location", artifact.Locations)
	}
}

func TestRegisterArtifactCreatesInitialSources(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := newTestService(t, store)

	_, err := service.RegisterArtifact(context.Background(), domain.Artifact{
		SampleRunID:       "sample-sources",
		ProducerNodeID:    "producer-a",
		ProducerAttemptID: "attempt-1",
		OutputName:        "dataset",
		ArtifactID:        "sample-sources/producer-a/attempt-1/dataset",
		Digest:            "sha256:abc123",
		LogicalURI:        "jumi://runs/sample-sources/nodes/producer-a/outputs/dataset",
		NodeName:          "node-a",
		URI:               "http://artifact-source.local/artifacts/abc123",
		Locations: []domain.Location{{
			NodeLocal: &domain.NodeLocalLocation{NodeName: "node-a", Path: "/var/lib/jumi-artifacts/cas/sha256/abc123"},
		}},
	})
	if err != nil {
		t.Fatalf("register artifact: %v", err)
	}

	sources, err := service.ListSources(context.Background(), "sample-sources/producer-a/attempt-1/dataset")
	if err != nil {
		t.Fatalf("list sources: %v", err)
	}
	if len(sources) != 2 {
		t.Fatalf("len(sources) = %d, want 2", len(sources))
	}
	var sawNodeLocal, sawHTTP bool
	for _, source := range sources {
		if source.ArtifactID != "sample-sources/producer-a/attempt-1/dataset" {
			t.Fatalf("source.ArtifactID = %q, want artifact id", source.ArtifactID)
		}
		if source.State != domain.SourceStateReady {
			t.Fatalf("source.State = %q, want ready", source.State)
		}
		switch {
		case source.Location.NodeLocal != nil:
			sawNodeLocal = true
			if source.BackendID != "node-local-default" {
				t.Fatalf("node-local backendID = %q, want node-local-default", source.BackendID)
			}
		case source.Location.HTTP != nil:
			sawHTTP = true
			if source.BackendID != "legacy-http" {
				t.Fatalf("http backendID = %q, want legacy-http", source.BackendID)
			}
		}
	}
	if !sawNodeLocal || !sawHTTP {
		t.Fatalf("sources missing backend types: nodeLocal=%v http=%v", sawNodeLocal, sawHTTP)
	}
}

func TestRegisterArtifactRejectsHTTPHeadersAndDoesNotPersist(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := newTestService(t, store)

	_, err := service.RegisterArtifact(context.Background(), domain.Artifact{
		SampleRunID:       "sample-reject",
		ProducerNodeID:    "producer-a",
		ProducerAttemptID: "attempt-1",
		OutputName:        "dataset",
		ArtifactID:        "sample-reject/producer-a/attempt-1/dataset",
		Digest:            "sha256:abc123",
		Locations: []domain.Location{{
			HTTP: &domain.HTTPSource{
				URI:     "http://artifact-source.local/artifacts/abc123",
				Headers: map[string]string{"Authorization": "Bearer t"},
			},
		}},
	})
	if err == nil {
		t.Fatal("expected register artifact to reject credential-bearing HTTP headers")
	}

	if _, ok, err := store.GetArtifact(context.Background(), "sample-reject", "producer-a", "attempt-1", "dataset"); err != nil {
		t.Fatalf("get artifact: %v", err)
	} else if ok {
		t.Fatal("artifact was stored despite registration rejection")
	}
	sources, err := store.ListArtifactSources(context.Background(), "sample-reject/producer-a/attempt-1/dataset")
	if err != nil {
		t.Fatalf("list artifact sources: %v", err)
	}
	if len(sources) != 0 {
		t.Fatalf("sources = %#v, want none after registration rejection", sources)
	}
}

func TestListSourcesExcludesDeletedByDefault(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := newTestService(t, store)
	artifactID := "sample-sources/producer-a/attempt-1/dataset"

	if err := store.PutArtifactSources(context.Background(), artifactID, []domain.ArtifactSource{
		{
			SourceID:   "src-ready",
			ArtifactID: artifactID,
			BackendID:  "legacy-http",
			Digest:     "sha256:abc123",
			State:      domain.SourceStateReady,
			Location: domain.Location{
				HTTP: &domain.HTTPSource{URI: "http://artifact-source.local/artifacts/abc123"},
			},
		},
		{
			SourceID:   "src-deleted",
			ArtifactID: artifactID,
			BackendID:  "legacy-http",
			Digest:     "sha256:abc123",
			State:      domain.SourceStateDeleted,
			Location: domain.Location{
				HTTP: &domain.HTTPSource{URI: "http://artifact-source.local/artifacts/old"},
			},
		},
	}); err != nil {
		t.Fatalf("put artifact sources: %v", err)
	}

	sources, err := service.ListSources(context.Background(), artifactID)
	if err != nil {
		t.Fatalf("list sources: %v", err)
	}
	if len(sources) != 1 {
		t.Fatalf("len(sources) = %d, want 1", len(sources))
	}
	if sources[0].SourceID != "src-ready" {
		t.Fatalf("sources[0].SourceID = %q, want src-ready", sources[0].SourceID)
	}
}

func TestResolveHandoffRemoteFetch(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := newTestService(t, store)
	_, err := service.RegisterArtifact(context.Background(), domain.Artifact{
		SampleRunID:       "sample-1",
		ProducerNodeID:    "parent-a",
		ProducerAttemptID: "attempt-1",
		OutputName:        "dataset",
		NodeName:          "node-a",
		Digest:            "sha256:dataset",
		URI:               "http://artifact.local/dataset",
	})
	if err != nil {
		t.Fatalf("register artifact: %v", err)
	}

	resolved, err := service.ResolveHandoff(context.Background(), domain.Binding{
		BindingName:        "dataset-input",
		SampleRunID:        "sample-1",
		ProducerNodeID:     "parent-a",
		ProducerAttemptID:  "attempt-1",
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
	if resolved.MaterializationPlan.Mode == domain.MaterializationModeNone {
		t.Fatalf("expected materialization")
	}
	if resolved.MaterializationPlan.Mode != domain.MaterializationModeRemoteFetch {
		t.Fatalf("materialization mode = %s, want remote_fetch", resolved.MaterializationPlan.Mode)
	}
	if resolved.MaterializationPlan.URI == "" {
		t.Fatal("expected materialization URI")
	}
}

func TestResolveHandoffDigestMismatchReturnsStatus(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := newTestService(t, store)
	_, err := service.RegisterArtifact(context.Background(), domain.Artifact{
		SampleRunID:       "sample-digest",
		ProducerNodeID:    "parent-a",
		ProducerAttemptID: "attempt-1",
		OutputName:        "dataset",
		NodeName:          "node-a",
		URI:               "http://artifact.local/dataset",
		Digest:            "sha256:actual",
	})
	if err != nil {
		t.Fatalf("register artifact: %v", err)
	}

	resolved, err := service.ResolveHandoff(context.Background(), domain.Binding{
		BindingName:        "dataset-input",
		SampleRunID:        "sample-digest",
		ProducerNodeID:     "parent-a",
		ProducerAttemptID:  "attempt-1",
		ProducerOutputName: "dataset",
		ConsumePolicy:      domain.ConsumePolicyRemoteOK,
		Required:           true,
		ExpectedDigest:     "sha256:expected",
	}, "node-b")
	if err != nil {
		t.Fatalf("resolve handoff: %v", err)
	}
	if resolved.Status != domain.ResolutionStatusDigestMismatch {
		t.Fatalf("status = %s, want %s", resolved.Status, domain.ResolutionStatusDigestMismatch)
	}
	if resolved.Retryable {
		t.Fatal("digest mismatch should not be retryable")
	}
}

func TestRegisterArtifactRemoteOnlyWhenNoNodeName(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := newTestService(t, store)

	state, err := service.RegisterArtifact(context.Background(), domain.Artifact{
		SampleRunID:       "sample-remote",
		ProducerNodeID:    "producer-a",
		ProducerAttemptID: "attempt-1",
		OutputName:        "dataset",
		URI:               "s3://bucket/path/dataset",
	})
	if err != nil {
		t.Fatalf("register artifact: %v", err)
	}
	if state != domain.AvailabilityStateRemoteOnly {
		t.Fatalf("availability state = %s, want %s", state, domain.AvailabilityStateRemoteOnly)
	}
}

func TestRegisterArtifactPopulatesCanonicalID(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := newTestService(t, store)

	_, err := service.RegisterArtifact(context.Background(), domain.Artifact{
		SampleRunID:       "run-1",
		ProducerNodeID:    "node-a",
		ProducerAttemptID: "attempt-1",
		OutputName:        "output",
		URI:               "s3://bucket/output",
	})
	if err != nil {
		t.Fatalf("register artifact: %v", err)
	}
	artifact, ok, err := store.GetArtifact(context.Background(), "run-1", "node-a", "attempt-1", "output")
	if err != nil || !ok {
		t.Fatalf("get artifact: %v, ok=%v", err, ok)
	}
	want := "run-1/node-a/attempt-1/output"
	if artifact.ArtifactID != want {
		t.Fatalf("artifactID = %q, want %q", artifact.ArtifactID, want)
	}
}

func TestRegisterArtifactIdempotentForSameDigest(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := newTestService(t, store)

	artifact := domain.Artifact{
		SampleRunID:       "run-idem",
		ProducerNodeID:    "node-a",
		ProducerAttemptID: "attempt-1",
		OutputName:        "output",
		Digest:            "sha256:abc",
		URI:               "jumi://runs/run-idem/nodes/node-a/outputs/output",
	}
	if _, err := service.RegisterArtifact(context.Background(), artifact); err != nil {
		t.Fatalf("first register: %v", err)
	}
	// Re-registering with the same digest must succeed (idempotent).
	if _, err := service.RegisterArtifact(context.Background(), artifact); err != nil {
		t.Fatalf("second register (same digest) must be idempotent: %v", err)
	}
}

func TestRegisterArtifactRejectsDigestConflictForSameArtifactKey(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := newTestService(t, store)

	if _, err := service.RegisterArtifact(context.Background(), domain.Artifact{
		SampleRunID:       "run-conflict",
		ProducerNodeID:    "node-a",
		ProducerAttemptID: "attempt-1",
		OutputName:        "output",
		Digest:            "sha256:original",
		URI:               "jumi://runs/run-conflict/nodes/node-a/outputs/output",
	}); err != nil {
		t.Fatalf("first register: %v", err)
	}

	_, err := service.RegisterArtifact(context.Background(), domain.Artifact{
		SampleRunID:       "run-conflict",
		ProducerNodeID:    "node-a",
		ProducerAttemptID: "attempt-1",
		OutputName:        "output",
		Digest:            "sha256:tampered", // different digest, same key
		URI:               "jumi://runs/run-conflict/nodes/node-a/outputs/output",
	})
	if err == nil {
		t.Fatal("expected conflict error for re-registration with different digest, got nil")
	}
}

func TestRegisterArtifactRejectsDigestClearingForSameArtifactKey(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := newTestService(t, store)

	if _, err := service.RegisterArtifact(context.Background(), domain.Artifact{
		SampleRunID:       "run-clear",
		ProducerNodeID:    "node-a",
		ProducerAttemptID: "attempt-1",
		OutputName:        "output",
		Digest:            "sha256:original",
		URI:               "jumi://runs/run-clear/nodes/node-a/outputs/output",
	}); err != nil {
		t.Fatalf("first register: %v", err)
	}

	_, err := service.RegisterArtifact(context.Background(), domain.Artifact{
		SampleRunID:       "run-clear",
		ProducerNodeID:    "node-a",
		ProducerAttemptID: "attempt-1",
		OutputName:        "output",
		Digest:            "", // clearing the digest must be rejected
		URI:               "jumi://runs/run-clear/nodes/node-a/outputs/output",
	})
	if err == nil {
		t.Fatal("expected error when clearing existing digest, got nil")
	}
}

func TestRegisterArtifactRejectsNonCanonicalArtifactID(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := newTestService(t, store)

	_, err := service.RegisterArtifact(context.Background(), domain.Artifact{
		SampleRunID:       "run-1",
		ProducerNodeID:    "node-a",
		ProducerAttemptID: "attempt-1",
		OutputName:        "output",
		ArtifactID:        "run-1:node-a:output", // legacy colon format — rejected by service layer
		URI:               "jumi://runs/run-1/nodes/node-a/outputs/output",
	})
	if err == nil {
		t.Fatal("expected error for non-canonical ArtifactID, got nil")
	}
}

func TestResolvePlanningMode_SameNodeOnly_MissingNodeName(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := newTestService(t, store)
	// Register artifact with URI but no NodeName (REMOTE_ONLY availability).
	if _, err := service.RegisterArtifact(context.Background(), domain.Artifact{
		SampleRunID:       "run-nonode",
		ProducerNodeID:    "node-a",
		ProducerAttemptID: "attempt-1",
		OutputName:        "output",
		URI:               "jumi://runs/run-nonode/nodes/node-a/outputs/output",
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	resolved, err := service.ResolveHandoff(context.Background(), domain.Binding{
		BindingName:        "input",
		SampleRunID:        "run-nonode",
		ProducerNodeID:     "node-a",
		ProducerAttemptID:  "attempt-1",
		ProducerOutputName: "output",
		ConsumePolicy:      domain.ConsumePolicySameNodeOnly,
	}, "") // planning mode
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolved.Status != domain.ResolutionStatusUnavailable {
		t.Fatalf("status = %q, want UNAVAILABLE (no locality to satisfy SameNodeOnly)", resolved.Status)
	}
}

func TestResolvePlanningMode_RemoteOK_MissingURI(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := newTestService(t, store)
	// Register artifact with NodeName but no URI (LOCAL_ONLY availability).
	if _, err := service.RegisterArtifact(context.Background(), domain.Artifact{
		SampleRunID:       "run-nouri",
		ProducerNodeID:    "node-a",
		ProducerAttemptID: "attempt-1",
		OutputName:        "output",
		NodeName:          "k8s-node-1",
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	resolved, err := service.ResolveHandoff(context.Background(), domain.Binding{
		BindingName:        "input",
		SampleRunID:        "run-nouri",
		ProducerNodeID:     "node-a",
		ProducerAttemptID:  "attempt-1",
		ProducerOutputName: "output",
		ConsumePolicy:      domain.ConsumePolicyRemoteOK,
	}, "") // planning mode
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolved.Status != domain.ResolutionStatusUnavailable {
		t.Fatalf("status = %q, want UNAVAILABLE (no URI for remote fetch)", resolved.Status)
	}
}

func TestResolvePlanningMode_SameNodeThenRemote_FallsBackToRemoteWhenNoNodeName(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := newTestService(t, store)
	if _, err := service.RegisterArtifact(context.Background(), domain.Artifact{
		SampleRunID:       "run-nonode2",
		ProducerNodeID:    "node-a",
		ProducerAttemptID: "attempt-1",
		OutputName:        "output",
		Digest:            "sha256:nonode2",
		URI:               "http://artifact-source.local/artifacts/nonode2",
		// NodeName intentionally empty
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	resolved, err := service.ResolveHandoff(context.Background(), domain.Binding{
		BindingName:        "input",
		SampleRunID:        "run-nonode2",
		ProducerNodeID:     "node-a",
		ProducerAttemptID:  "attempt-1",
		ProducerOutputName: "output",
		ConsumePolicy:      domain.ConsumePolicySameNodeThenRemote,
	}, "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolved.Status != domain.ResolutionStatusResolved {
		t.Fatalf("status = %q, want RESOLVED (degrade to remote fetch)", resolved.Status)
	}
	if resolved.PlacementIntent.Mode != domain.PlacementIntentModeNone {
		t.Fatalf("placementIntent.mode = %q, want none (no locality hint available)", resolved.PlacementIntent.Mode)
	}
	if resolved.MaterializationPlan.Mode != domain.MaterializationModeRemoteFetch {
		t.Fatalf("materializationPlan.mode = %q, want remote_fetch", resolved.MaterializationPlan.Mode)
	}
}

func TestResolveHandoffRejectsMissingProducerAttemptID(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := newTestService(t, store)

	_, err := service.ResolveHandoff(context.Background(), domain.Binding{
		BindingName:        "input",
		SampleRunID:        "run-1",
		ProducerNodeID:     "node-a",
		ProducerAttemptID:  "", // intentionally empty
		ProducerOutputName: "output",
		ConsumePolicy:      domain.ConsumePolicyRemoteOK,
	}, "node-b")
	if err == nil {
		t.Fatal("expected error for missing producerAttemptID, got nil")
	}
}

func TestResolveHandoffRejectsEmptyConsumePolicy(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := newTestService(t, store)

	_, err := service.ResolveHandoff(context.Background(), domain.Binding{
		BindingName:        "input",
		SampleRunID:        "run-1",
		ProducerNodeID:     "node-a",
		ProducerAttemptID:  "attempt-1",
		ProducerOutputName: "output",
		ConsumePolicy:      "", // intentionally empty
	}, "node-b")
	if err == nil {
		t.Fatal("expected error for empty consumePolicy, got nil")
	}
}

func TestGetArtifactRejectsMissingAttemptID(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := newTestService(t, store)

	_, _, err := service.GetArtifact(context.Background(), "run-1", "node-a", "", "output")
	if err == nil {
		t.Fatal("expected error for missing attemptID, got nil")
	}
}

func TestResolveHandoffRejectsUnknownConsumePolicy(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := newTestService(t, store)
	if _, err := service.RegisterArtifact(context.Background(), domain.Artifact{
		SampleRunID:       "sample-policy",
		ProducerNodeID:    "parent-a",
		ProducerAttemptID: "attempt-1",
		OutputName:        "dataset",
		NodeName:          "node-a",
		URI:               "s3://bucket/dataset",
	}); err != nil {
		t.Fatalf("register artifact: %v", err)
	}

	_, err := service.ResolveHandoff(context.Background(), domain.Binding{
		BindingName:        "dataset-input",
		SampleRunID:        "sample-policy",
		ProducerNodeID:     "parent-a",
		ProducerAttemptID:  "attempt-1",
		ProducerOutputName: "dataset",
		ConsumePolicy:      "InvalidPolicy",
	}, "node-b")
	if err == nil {
		t.Fatal("resolve handoff error = nil, want unknown consume policy error")
	}
}

// Planning mode tests: targetNodeName == "" means Spawner has not scheduled the Pod yet.
// AH must return placement guidance so Spawner can build the PodSpec.

func TestResolveHandoffPlanningMode_SameNodeOnly(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := newTestService(t, store)
	if _, err := service.RegisterArtifact(context.Background(), domain.Artifact{
		SampleRunID:       "run-plan",
		ProducerNodeID:    "node-a",
		ProducerAttemptID: "attempt-1",
		OutputName:        "output",
		NodeName:          "k8s-node-1",
		Digest:            "sha256:plan1",
		URI:               "jumi://runs/run-plan/nodes/node-a/outputs/output",
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	resolved, err := service.ResolveHandoff(context.Background(), domain.Binding{
		BindingName:        "input",
		SampleRunID:        "run-plan",
		ProducerNodeID:     "node-a",
		ProducerAttemptID:  "attempt-1",
		ProducerOutputName: "output",
		ConsumePolicy:      domain.ConsumePolicySameNodeOnly,
	}, "") // targetNodeName empty = planning mode
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolved.Status != domain.ResolutionStatusResolved {
		t.Fatalf("status = %q, want RESOLVED (planning mode must not return MISSING)", resolved.Status)
	}
	if resolved.Decision != domain.ResolutionDecisionLocalReuse {
		t.Fatalf("decision = %q, want local_reuse", resolved.Decision)
	}
	if resolved.PlacementIntent.Mode != domain.PlacementIntentModeRequiredNode {
		t.Fatalf("placementIntent.mode = %q, want required_node", resolved.PlacementIntent.Mode)
	}
	if resolved.PlacementIntent.NodeName != "k8s-node-1" {
		t.Fatalf("placementIntent.nodeName = %q, want k8s-node-1", resolved.PlacementIntent.NodeName)
	}
	if resolved.MaterializationPlan.Mode != domain.MaterializationModeLocalReuse {
		t.Fatalf("materializationPlan.mode = %q, want local_reuse", resolved.MaterializationPlan.Mode)
	}
}

func TestResolveHandoffPlanningMode_SameNodeThenRemote(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := newTestService(t, store)
	if _, err := service.RegisterArtifact(context.Background(), domain.Artifact{
		SampleRunID:       "run-plan2",
		ProducerNodeID:    "node-a",
		ProducerAttemptID: "attempt-1",
		OutputName:        "output",
		NodeName:          "k8s-node-1",
		Digest:            "sha256:plan2",
		URI:               "http://artifact-source.local/artifacts/plan2",
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	resolved, err := service.ResolveHandoff(context.Background(), domain.Binding{
		BindingName:        "input",
		SampleRunID:        "run-plan2",
		ProducerNodeID:     "node-a",
		ProducerAttemptID:  "attempt-1",
		ProducerOutputName: "output",
		ConsumePolicy:      domain.ConsumePolicySameNodeThenRemote,
	}, "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolved.Status != domain.ResolutionStatusResolved {
		t.Fatalf("status = %q, want RESOLVED", resolved.Status)
	}
	// PreferredNode hint must be present so Spawner can request the producer node.
	if resolved.PlacementIntent.Mode != domain.PlacementIntentModePreferredNode {
		t.Fatalf("placementIntent.mode = %q, want preferred_node", resolved.PlacementIntent.Mode)
	}
	if resolved.PlacementIntent.NodeName != "k8s-node-1" {
		t.Fatalf("placementIntent.nodeName = %q, want k8s-node-1", resolved.PlacementIntent.NodeName)
	}
	// MaterializationPlan is remote_fetch as fallback for when scheduling lands elsewhere.
	if resolved.MaterializationPlan.Mode != domain.MaterializationModeRemoteFetch {
		t.Fatalf("materializationPlan.mode = %q, want remote_fetch", resolved.MaterializationPlan.Mode)
	}
	if len(resolved.MaterializationCandidates) != 2 {
		t.Fatalf("materializationCandidates len = %d, want 2", len(resolved.MaterializationCandidates))
	}
	if resolved.MaterializationCandidates[0].Mode != domain.MaterializationModeLocalReuse {
		t.Fatalf("materializationCandidates[0].mode = %q, want local_reuse", resolved.MaterializationCandidates[0].Mode)
	}
	if resolved.MaterializationCandidates[1].Mode != domain.MaterializationModeRemoteFetch {
		t.Fatalf("materializationCandidates[1].mode = %q, want remote_fetch", resolved.MaterializationCandidates[1].Mode)
	}
}

func TestResolveHandoffPlanningMode_NodeLocalAndHTTPCreatesOrderedCandidates(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := newTestService(t, store)
	if _, err := service.RegisterArtifact(context.Background(), domain.Artifact{
		SampleRunID:       "run-plan-http-local",
		ProducerNodeID:    "node-a",
		ProducerAttemptID: "attempt-1",
		OutputName:        "output",
		NodeName:          "k8s-node-1",
		Digest:            "sha256:abc123",
		URI:               "http://artifact-source.local/artifacts/abc123",
		Locations: []domain.Location{{
			NodeLocal: &domain.NodeLocalLocation{
				NodeName: "k8s-node-1",
				Path:     "/var/lib/jumi-artifacts/cas/sha256/abc123",
			},
		}},
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	resolved, err := service.ResolveHandoff(context.Background(), domain.Binding{
		BindingName:        "input",
		SampleRunID:        "run-plan-http-local",
		ProducerNodeID:     "node-a",
		ProducerAttemptID:  "attempt-1",
		ProducerOutputName: "output",
		ConsumePolicy:      domain.ConsumePolicySameNodeThenRemote,
		ChildInputName:     "result",
	}, "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolved.PlacementIntent.Mode != domain.PlacementIntentModePreferredNode {
		t.Fatalf("placementIntent.mode = %q, want preferred_node", resolved.PlacementIntent.Mode)
	}
	if len(resolved.MaterializationCandidates) != 2 {
		t.Fatalf("materializationCandidates len = %d, want 2", len(resolved.MaterializationCandidates))
	}
	if resolved.MaterializationCandidates[0].Mode != domain.MaterializationModeLocalReuse {
		t.Fatalf("materializationCandidates[0].mode = %q, want local_reuse", resolved.MaterializationCandidates[0].Mode)
	}
	if resolved.MaterializationCandidates[1].Mode != domain.MaterializationModeRemoteFetch {
		t.Fatalf("materializationCandidates[1].mode = %q, want remote_fetch", resolved.MaterializationCandidates[1].Mode)
	}
	if resolved.MaterializationPlan.Mode != domain.MaterializationModeRemoteFetch {
		t.Fatalf("legacy materializationPlan.mode = %q, want remote_fetch", resolved.MaterializationPlan.Mode)
	}
}

func TestResolveHandoffPlanningMode_BackfillsNodeLocalConditionFromArtifactNodeName(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := newTestService(t, store)
	if _, err := service.RegisterArtifact(context.Background(), domain.Artifact{
		SampleRunID:       "run-plan-local-fallback-node",
		ProducerNodeID:    "node-a",
		ProducerAttemptID: "attempt-1",
		OutputName:        "output",
		NodeName:          "k8s-node-1",
		Digest:            "sha256:fallbacknode",
		Locations: []domain.Location{{
			NodeLocal: &domain.NodeLocalLocation{
				Path: "/var/lib/jumi-artifacts/cas/sha256/fallbacknode",
			},
		}},
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	resolved, err := service.ResolveHandoff(context.Background(), domain.Binding{
		BindingName:        "input",
		SampleRunID:        "run-plan-local-fallback-node",
		ProducerNodeID:     "node-a",
		ProducerAttemptID:  "attempt-1",
		ProducerOutputName: "output",
		ConsumePolicy:      domain.ConsumePolicySameNodeOnly,
	}, "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(resolved.MaterializationCandidates) != 1 {
		t.Fatalf("materializationCandidates = %#v, want one local candidate", resolved.MaterializationCandidates)
	}
	conditions := resolved.MaterializationCandidates[0].Conditions
	found := false
	for _, cond := range conditions {
		if cond.Kind == "scheduled_on_node" && cond.NodeName == "k8s-node-1" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("conditions = %#v, want scheduled_on_node=k8s-node-1", conditions)
	}
}

func TestResolveHandoffPlanningMode_NodeLocalOnlySameNodeThenRemoteRequiresPlacement(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := newTestService(t, store)
	if _, err := service.RegisterArtifact(context.Background(), domain.Artifact{
		SampleRunID:       "run-plan-local-only",
		ProducerNodeID:    "node-a",
		ProducerAttemptID: "attempt-1",
		OutputName:        "output",
		NodeName:          "k8s-node-1",
		Digest:            "sha256:localonly",
		Locations: []domain.Location{{
			NodeLocal: &domain.NodeLocalLocation{
				NodeName: "k8s-node-1",
				Path:     "/var/lib/jumi-artifacts/cas/sha256/localonly",
			},
		}},
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	resolved, err := service.ResolveHandoff(context.Background(), domain.Binding{
		BindingName:        "input",
		SampleRunID:        "run-plan-local-only",
		ProducerNodeID:     "node-a",
		ProducerAttemptID:  "attempt-1",
		ProducerOutputName: "output",
		ConsumePolicy:      domain.ConsumePolicySameNodeThenRemote,
	}, "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolved.PlacementIntent.Mode != domain.PlacementIntentModeRequiredNode {
		t.Fatalf("placementIntent.mode = %q, want required_node", resolved.PlacementIntent.Mode)
	}
	if resolved.MaterializationPlan.Mode != domain.MaterializationModeLocalReuse {
		t.Fatalf("materializationPlan.mode = %q, want local_reuse", resolved.MaterializationPlan.Mode)
	}
	if len(resolved.MaterializationCandidates) != 1 || resolved.MaterializationCandidates[0].Mode != domain.MaterializationModeLocalReuse {
		t.Fatalf("materializationCandidates = %#v, want one local_reuse candidate", resolved.MaterializationCandidates)
	}
}

func TestResolveHandoffPlanningMode_IgnoresUnreachableNodeLocalSource(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := newTestService(t, store)
	if _, err := service.RegisterArtifact(context.Background(), domain.Artifact{
		SampleRunID:       "run-plan-unreachable",
		ProducerNodeID:    "node-a",
		ProducerAttemptID: "attempt-1",
		OutputName:        "output",
		NodeName:          "k8s-node-1",
		Digest:            "sha256:abc123",
		URI:               "http://artifact-source.local/artifacts/abc123",
		Locations: []domain.Location{{
			NodeLocal: &domain.NodeLocalLocation{
				NodeName: "k8s-node-1",
				Path:     "/var/lib/jumi-artifacts/cas/sha256/abc123",
			},
		}},
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	stored, ok, err := store.GetArtifact(context.Background(), "run-plan-unreachable", "node-a", "attempt-1", "output")
	if err != nil || !ok {
		t.Fatalf("get artifact: ok=%v err=%v", ok, err)
	}
	sources, err := store.ListArtifactSources(context.Background(), stored.ArtifactID)
	if err != nil {
		t.Fatalf("list sources: %v", err)
	}
	for i := range sources {
		if sources[i].Location.NodeLocal != nil {
			sources[i].State = domain.SourceStateUnreachable
		}
	}
	if err := store.PutArtifactSources(context.Background(), stored.ArtifactID, sources); err != nil {
		t.Fatalf("put sources: %v", err)
	}

	resolved, err := service.ResolveHandoff(context.Background(), domain.Binding{
		BindingName:        "input",
		SampleRunID:        "run-plan-unreachable",
		ProducerNodeID:     "node-a",
		ProducerAttemptID:  "attempt-1",
		ProducerOutputName: "output",
		ConsumePolicy:      domain.ConsumePolicySameNodeThenRemote,
	}, "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(resolved.MaterializationCandidates) != 1 {
		t.Fatalf("materializationCandidates len = %d, want 1", len(resolved.MaterializationCandidates))
	}
	if resolved.MaterializationCandidates[0].Mode != domain.MaterializationModeRemoteFetch {
		t.Fatalf("materializationCandidates[0].mode = %q, want remote_fetch", resolved.MaterializationCandidates[0].Mode)
	}
}

func TestResolveHandoffPlanningMode_ExcludesNonReadyNodeLocalSources(t *testing.T) {
	states := []domain.SourceState{
		domain.SourceStateStale,
		domain.SourceStateUnreachable,
		domain.SourceStateDeleted,
	}
	for _, state := range states {
		t.Run(string(state), func(t *testing.T) {
			store := inventory.NewMemoryStore()
			service := newTestService(t, store)
			if _, err := service.RegisterArtifact(context.Background(), domain.Artifact{
				SampleRunID:       "run-plan-nonready-" + string(state),
				ProducerNodeID:    "node-a",
				ProducerAttemptID: "attempt-1",
				OutputName:        "output",
				NodeName:          "k8s-node-1",
				Digest:            "sha256:abc123",
				URI:               "http://artifact-source.local/artifacts/abc123",
				Locations: []domain.Location{{
					NodeLocal: &domain.NodeLocalLocation{
						NodeName: "k8s-node-1",
						Path:     "/var/lib/jumi-artifacts/cas/sha256/abc123",
					},
				}},
			}); err != nil {
				t.Fatalf("register: %v", err)
			}
			stored, ok, err := store.GetArtifact(context.Background(), "run-plan-nonready-"+string(state), "node-a", "attempt-1", "output")
			if err != nil || !ok {
				t.Fatalf("get artifact: ok=%v err=%v", ok, err)
			}
			sources, err := store.ListArtifactSources(context.Background(), stored.ArtifactID)
			if err != nil {
				t.Fatalf("list sources: %v", err)
			}
			for i := range sources {
				if sources[i].Location.NodeLocal != nil {
					sources[i].State = state
				}
			}
			if err := store.PutArtifactSources(context.Background(), stored.ArtifactID, sources); err != nil {
				t.Fatalf("put sources: %v", err)
			}

			resolved, err := service.ResolveHandoff(context.Background(), domain.Binding{
				BindingName:        "input",
				SampleRunID:        "run-plan-nonready-" + string(state),
				ProducerNodeID:     "node-a",
				ProducerAttemptID:  "attempt-1",
				ProducerOutputName: "output",
				ConsumePolicy:      domain.ConsumePolicySameNodeThenRemote,
			}, "")
			if err != nil {
				t.Fatalf("resolve: %v", err)
			}
			if len(resolved.MaterializationCandidates) != 1 {
				t.Fatalf("materializationCandidates len = %d, want 1", len(resolved.MaterializationCandidates))
			}
			if resolved.MaterializationCandidates[0].Mode != domain.MaterializationModeRemoteFetch {
				t.Fatalf("materializationCandidates[0].mode = %q, want remote_fetch", resolved.MaterializationCandidates[0].Mode)
			}
		})
	}
}

func TestResolveHandoffPlanningMode_IgnoresDigestMismatchedNodeLocalSource(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := newTestService(t, store)
	if _, err := service.RegisterArtifact(context.Background(), domain.Artifact{
		SampleRunID:       "run-plan-digest-mismatch",
		ProducerNodeID:    "node-a",
		ProducerAttemptID: "attempt-1",
		OutputName:        "output",
		NodeName:          "k8s-node-1",
		Digest:            "sha256:abc123",
		URI:               "http://artifact-source.local/artifacts/abc123",
		Locations: []domain.Location{{
			NodeLocal: &domain.NodeLocalLocation{
				NodeName: "k8s-node-1",
				Path:     "/var/lib/jumi-artifacts/cas/sha256/abc123",
			},
		}},
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	stored, ok, err := store.GetArtifact(context.Background(), "run-plan-digest-mismatch", "node-a", "attempt-1", "output")
	if err != nil || !ok {
		t.Fatalf("get artifact: ok=%v err=%v", ok, err)
	}
	sources, err := store.ListArtifactSources(context.Background(), stored.ArtifactID)
	if err != nil {
		t.Fatalf("list sources: %v", err)
	}
	for i := range sources {
		if sources[i].Location.NodeLocal != nil {
			sources[i].Digest = "sha256:def456"
		}
	}
	if err := store.PutArtifactSources(context.Background(), stored.ArtifactID, sources); err != nil {
		t.Fatalf("put sources: %v", err)
	}

	resolved, err := service.ResolveHandoff(context.Background(), domain.Binding{
		BindingName:        "input",
		SampleRunID:        "run-plan-digest-mismatch",
		ProducerNodeID:     "node-a",
		ProducerAttemptID:  "attempt-1",
		ProducerOutputName: "output",
		ConsumePolicy:      domain.ConsumePolicySameNodeThenRemote,
	}, "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(resolved.MaterializationCandidates) != 1 {
		t.Fatalf("materializationCandidates len = %d, want 1", len(resolved.MaterializationCandidates))
	}
	if resolved.MaterializationCandidates[0].Mode != domain.MaterializationModeRemoteFetch {
		t.Fatalf("materializationCandidates[0].mode = %q, want remote_fetch", resolved.MaterializationCandidates[0].Mode)
	}
}

func TestResolveHandoffPlanningMode_IgnoresReadySourceWithoutDigest(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := newTestService(t, store)
	artifactID := "run-plan-no-source-digest/node-a/attempt-1/output"
	artifact := domain.Artifact{
		SampleRunID:       "run-plan-no-source-digest",
		ProducerNodeID:    "node-a",
		ProducerAttemptID: "attempt-1",
		OutputName:        "output",
		ArtifactID:        artifactID,
		Digest:            "sha256:abc123",
		NodeName:          "k8s-node-1",
	}
	if err := store.PutArtifact(context.Background(), artifact); err != nil {
		t.Fatalf("put artifact: %v", err)
	}
	if err := store.PutArtifactSources(context.Background(), artifactID, []domain.ArtifactSource{{
		SourceID:   "src-http",
		ArtifactID: artifactID,
		BackendID:  "legacy-http",
		State:      domain.SourceStateReady,
		Location: domain.Location{
			HTTP: &domain.HTTPSource{URI: "http://artifact-source.local/artifacts/abc123"},
		},
	}}); err != nil {
		t.Fatalf("put sources: %v", err)
	}
	notifyTerminal(t, service, "run-plan-no-source-digest", "node-a", "attempt-1", "Succeeded")

	resolved, err := service.ResolveHandoff(context.Background(), domain.Binding{
		BindingName:        "input",
		SampleRunID:        "run-plan-no-source-digest",
		ProducerNodeID:     "node-a",
		ProducerAttemptID:  "attempt-1",
		ProducerOutputName: "output",
		ConsumePolicy:      domain.ConsumePolicyRemoteOK,
	}, "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(resolved.MaterializationCandidates) != 0 {
		t.Fatalf("materializationCandidates = %#v, want none", resolved.MaterializationCandidates)
	}
}

func TestResolveHandoffPlanningMode_RejectsHTTPSourceWithoutAllowlistByDefault(t *testing.T) {
	t.Setenv("AH_ALLOW_ANY_HTTP_SOURCE", "false")

	store := inventory.NewMemoryStore()
	service, err := NewService(store)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	if _, err := service.RegisterArtifact(context.Background(), domain.Artifact{
		SampleRunID:       "run-plan-http-default-reject",
		ProducerNodeID:    "node-a",
		ProducerAttemptID: "attempt-1",
		OutputName:        "output",
		Digest:            "sha256:plan3",
		URI:               "http://artifact-source.local/artifacts/plan3",
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	notifyTerminal(t, service, "run-plan-http-default-reject", "node-a", "attempt-1", "Succeeded")

	resolved, err := service.ResolveHandoff(context.Background(), domain.Binding{
		BindingName:        "input",
		SampleRunID:        "run-plan-http-default-reject",
		ProducerNodeID:     "node-a",
		ProducerAttemptID:  "attempt-1",
		ProducerOutputName: "output",
		ConsumePolicy:      domain.ConsumePolicyRemoteOK,
	}, "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolved.Status != domain.ResolutionStatusUnavailable {
		t.Fatalf("status = %q, want UNAVAILABLE", resolved.Status)
	}
}

func TestResolveHandoffPlanningMode_RejectsCandidatesWithoutExpectedDigest(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := newTestService(t, store)
	if _, err := service.RegisterArtifact(context.Background(), domain.Artifact{
		SampleRunID:       "run-plan-no-digest",
		ProducerNodeID:    "node-a",
		ProducerAttemptID: "attempt-1",
		OutputName:        "output",
		NodeName:          "k8s-node-1",
		URI:               "http://artifact-source.local/artifacts/abc123",
		Locations: []domain.Location{{
			NodeLocal: &domain.NodeLocalLocation{
				NodeName: "k8s-node-1",
				Path:     "/var/lib/jumi-artifacts/cas/sha256/abc123",
			},
		}},
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	resolved, err := service.ResolveHandoff(context.Background(), domain.Binding{
		BindingName:        "input",
		SampleRunID:        "run-plan-no-digest",
		ProducerNodeID:     "node-a",
		ProducerAttemptID:  "attempt-1",
		ProducerOutputName: "output",
		ConsumePolicy:      domain.ConsumePolicySameNodeThenRemote,
	}, "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolved.Status != domain.ResolutionStatusUnavailable {
		t.Fatalf("status = %q, want UNAVAILABLE", resolved.Status)
	}
	if len(resolved.MaterializationCandidates) != 0 {
		t.Fatalf("materializationCandidates = %#v, want none", resolved.MaterializationCandidates)
	}
}

func TestResolveHandoffPlanningMode_RemoteOK(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := newTestService(t, store)
	if _, err := service.RegisterArtifact(context.Background(), domain.Artifact{
		SampleRunID:       "run-plan3",
		ProducerNodeID:    "node-a",
		ProducerAttemptID: "attempt-1",
		OutputName:        "output",
		NodeName:          "k8s-node-1",
		Digest:            "sha256:plan3",
		URI:               "http://artifact-source.local/artifacts/plan3",
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	resolved, err := service.ResolveHandoff(context.Background(), domain.Binding{
		BindingName:        "input",
		SampleRunID:        "run-plan3",
		ProducerNodeID:     "node-a",
		ProducerAttemptID:  "attempt-1",
		ProducerOutputName: "output",
		ConsumePolicy:      domain.ConsumePolicyRemoteOK,
	}, "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolved.Status != domain.ResolutionStatusResolved {
		t.Fatalf("status = %q, want RESOLVED", resolved.Status)
	}
	if resolved.PlacementIntent.Mode != domain.PlacementIntentModeNone {
		t.Fatalf("placementIntent.mode = %q, want none", resolved.PlacementIntent.Mode)
	}
	if resolved.MaterializationPlan.Mode != domain.MaterializationModeRemoteFetch {
		t.Fatalf("materializationPlan.mode = %q, want remote_fetch", resolved.MaterializationPlan.Mode)
	}
}

// Post-scheduling check: targetNodeName is known, consumer is on a different node.
func TestResolveHandoffPostScheduling_SameNodeOnly_Violation(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := newTestService(t, store)
	if _, err := service.RegisterArtifact(context.Background(), domain.Artifact{
		SampleRunID:       "run-post",
		ProducerNodeID:    "node-a",
		ProducerAttemptID: "attempt-1",
		OutputName:        "output",
		NodeName:          "k8s-node-1",
		URI:               "jumi://runs/run-post/nodes/node-a/outputs/output",
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	resolved, err := service.ResolveHandoff(context.Background(), domain.Binding{
		BindingName:        "input",
		SampleRunID:        "run-post",
		ProducerNodeID:     "node-a",
		ProducerAttemptID:  "attempt-1",
		ProducerOutputName: "output",
		ConsumePolicy:      domain.ConsumePolicySameNodeOnly,
	}, "k8s-node-2") // different node → violation
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolved.Status != domain.ResolutionStatusPolicyBlocked {
		t.Fatalf("status = %q, want POLICY_BLOCKED (post-scheduling SameNodeOnly violation)", resolved.Status)
	}
	if resolved.PlacementIntent.Mode != domain.PlacementIntentModeRequiredNode {
		t.Fatalf("placementIntent.mode = %q, want required_node", resolved.PlacementIntent.Mode)
	}
}

func TestResolveHandoffDigestMismatch_ArtifactHasNoDigest(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := newTestService(t, store)
	if _, err := service.RegisterArtifact(context.Background(), domain.Artifact{
		SampleRunID:       "sample-nodigest",
		ProducerNodeID:    "parent-a",
		ProducerAttemptID: "attempt-1",
		OutputName:        "dataset",
		NodeName:          "node-a",
		URI:               "s3://bucket/dataset",
		// Digest intentionally omitted
	}); err != nil {
		t.Fatalf("register artifact: %v", err)
	}

	resolved, err := service.ResolveHandoff(context.Background(), domain.Binding{
		BindingName:        "dataset-input",
		SampleRunID:        "sample-nodigest",
		ProducerNodeID:     "parent-a",
		ProducerAttemptID:  "attempt-1",
		ProducerOutputName: "dataset",
		ConsumePolicy:      domain.ConsumePolicyRemoteOK,
		ExpectedDigest:     "sha256:abc123",
	}, "node-b")
	if err != nil {
		t.Fatalf("resolve handoff: %v", err)
	}
	if resolved.Status != domain.ResolutionStatusDigestMismatch {
		t.Fatalf("status = %s, want %s", resolved.Status, domain.ResolutionStatusDigestMismatch)
	}
	if resolved.Retryable {
		t.Fatal("digest mismatch should not be retryable")
	}
}

func TestResolveHandoffReturnsPendingWhenProducerNotTerminalAndArtifactMissing(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := newTestService(t, store)

	resolved, err := service.ResolveHandoff(context.Background(), domain.Binding{
		BindingName:        "dataset-input",
		SampleRunID:        "sample-pending",
		ProducerNodeID:     "parent-a",
		ProducerAttemptID:  "attempt-1",
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
	if !resolved.Retryable {
		t.Fatal("expected pending to be retryable")
	}
}

func TestResolveHandoffReturnsMissingWhenProducerSucceededButArtifactMissing(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := newTestService(t, store)
	if err := service.NotifyNodeTerminal(context.Background(), "sample-missing", "parent-a", "attempt-1", "Succeeded"); err != nil {
		t.Fatalf("notify terminal: %v", err)
	}

	resolved, err := service.ResolveHandoff(context.Background(), domain.Binding{
		BindingName:        "dataset-input",
		SampleRunID:        "sample-missing",
		ProducerNodeID:     "parent-a",
		ProducerAttemptID:  "attempt-1",
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
	service := newTestService(t, store)
	if err := service.NotifyNodeTerminal(context.Background(), "sample-producer-failed", "parent-a", "attempt-1", "Failed"); err != nil {
		t.Fatalf("notify terminal: %v", err)
	}

	resolved, err := service.ResolveHandoff(context.Background(), domain.Binding{
		BindingName:        "dataset-input",
		SampleRunID:        "sample-producer-failed",
		ProducerNodeID:     "parent-a",
		ProducerAttemptID:  "attempt-1",
		ProducerOutputName: "dataset",
		ConsumePolicy:      domain.ConsumePolicyRemoteOK,
		Required:           true,
	}, "node-b")
	if err != nil {
		t.Fatalf("resolve handoff: %v", err)
	}
	if resolved.Status != domain.ResolutionStatusProducerFailed {
		t.Fatalf("status = %s, want %s", resolved.Status, domain.ResolutionStatusProducerFailed)
	}
	if resolved.Decision != domain.ResolutionDecisionProducerFailed {
		t.Fatalf("decision = %s, want %s", resolved.Decision, domain.ResolutionDecisionProducerFailed)
	}
}

func TestResolveHandoffReturnsMissingWhenSampleAlreadyGCEligible(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := newTestService(t, store)
	baseNow := time.Date(2026, 4, 21, 10, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return baseNow }
	if _, err := service.RegisterArtifact(context.Background(), domain.Artifact{
		SampleRunID:       "sample-gc",
		ProducerNodeID:    "parent-a",
		ProducerAttemptID: "attempt-1",
		OutputName:        "dataset",
		NodeName:          "node-a",
		URI:               "http://artifact.local/dataset",
	}); err != nil {
		t.Fatalf("register artifact: %v", err)
	}
	if err := service.NotifyNodeTerminal(context.Background(), "sample-gc", "parent-a", "attempt-1", "Succeeded"); err != nil {
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
		ProducerAttemptID:  "attempt-1",
		ProducerOutputName: "dataset",
		ConsumePolicy:      domain.ConsumePolicyRemoteOK,
		Required:           true,
	}, "node-b")
	if err != nil {
		t.Fatalf("resolve handoff: %v", err)
	}
	if resolved.Status != domain.ResolutionStatusGCExpired {
		t.Fatalf("status = %s, want %s", resolved.Status, domain.ResolutionStatusGCExpired)
	}
	if resolved.Decision != domain.ResolutionDecisionUnavailable {
		t.Fatalf("decision = %s, want %s", resolved.Decision, domain.ResolutionDecisionUnavailable)
	}
}

func TestNotifyNodeTerminalRecordsState(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := newTestService(t, store)

	if err := service.NotifyNodeTerminal(context.Background(), "sample-1", "child-a", "attempt-1", "Succeeded"); err != nil {
		t.Fatalf("notify terminal: %v", err)
	}
	record, ok, err := store.GetNodeTerminal(context.Background(), "sample-1", "child-a", "attempt-1")
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
	service := newTestService(t, store)

	if err := service.NotifyNodeTerminal(context.Background(), "sample-1", "child-a", "attempt-1", "Running"); err == nil {
		t.Fatal("expected unsupported terminal state to fail")
	}
}

func TestFinalizeSampleRunStoresLifecycle(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := newTestService(t, store)
	fixedNow := time.Date(2026, 4, 21, 10, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return fixedNow }
	_, err := service.RegisterArtifact(context.Background(), domain.Artifact{
		SampleRunID:       "sample-1",
		ProducerNodeID:    "parent-a",
		ProducerAttemptID: "attempt-1",
		OutputName:        "dataset",
		SizeBytes:         2048,
	})
	if err != nil {
		t.Fatalf("register artifact: %v", err)
	}
	if err := service.NotifyNodeTerminal(context.Background(), "sample-1", "parent-a", "attempt-1", "Succeeded"); err != nil {
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
	service := newTestService(t, store)
	baseNow := time.Date(2026, 4, 21, 10, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return baseNow }
	if _, err := service.RegisterArtifact(context.Background(), domain.Artifact{
		SampleRunID:       "sample-1",
		ProducerNodeID:    "parent-a",
		ProducerAttemptID: "attempt-1",
		OutputName:        "dataset",
		SizeBytes:         2048,
	}); err != nil {
		t.Fatalf("register artifact: %v", err)
	}
	if err := service.NotifyNodeTerminal(context.Background(), "sample-1", "parent-a", "attempt-1", "Succeeded"); err != nil {
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
	if rendered := scrapeMetrics(t, service.Metrics().Handler()); !hasMetricValue(rendered, "ah_gc_backlog_bytes", "2048") {
		t.Fatalf("expected gc backlog gauge to reflect eligible retained artifact, got %s", rendered)
	}
}

func TestEvaluateGCBlocksWhenTerminalNodesMissing(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := newTestService(t, store)
	if _, err := service.RegisterArtifact(context.Background(), domain.Artifact{
		SampleRunID:       "sample-2",
		ProducerNodeID:    "parent-a",
		ProducerAttemptID: "attempt-1",
		OutputName:        "dataset",
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
	service := newTestService(t, store)
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
	service := newTestService(t, store)
	baseNow := time.Date(2026, 4, 21, 10, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return baseNow }
	if _, err := service.RegisterArtifact(context.Background(), domain.Artifact{
		SampleRunID:       "sample-3",
		ProducerNodeID:    "parent-a",
		ProducerAttemptID: "attempt-1",
		OutputName:        "dataset",
	}); err != nil {
		t.Fatalf("register artifact: %v", err)
	}
	if err := service.NotifyNodeTerminal(context.Background(), "sample-3", "parent-a", "attempt-1", "Succeeded"); err != nil {
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
	if rendered := scrapeMetrics(t, service.Metrics().Handler()); !hasMetricValue(rendered, "ah_gc_backlog_bytes", "0") {
		t.Fatalf("expected gc backlog gauge to stay zero while blocked, got %s", rendered)
	}
}

func TestEvaluateGCRefreshesStaleLifecycleSnapshot(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := newTestService(t, store)
	baseNow := time.Date(2026, 4, 21, 10, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return baseNow }
	if _, err := service.RegisterArtifact(context.Background(), domain.Artifact{
		SampleRunID:       "sample-stale",
		ProducerNodeID:    "parent-a",
		ProducerAttemptID: "attempt-1",
		OutputName:        "dataset",
		SizeBytes:         3072,
	}); err != nil {
		t.Fatalf("register artifact: %v", err)
	}
	if err := service.NotifyNodeTerminal(context.Background(), "sample-stale", "parent-a", "attempt-1", "Succeeded"); err != nil {
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
	service := newTestService(t, store)
	handler := NewHTTPHandler(service)

	req := httptest.NewRequest(http.MethodPost, "/v1/artifacts:register", strings.NewReader(`{
		"sampleRunID":"sample-http",
		"producerNodeID":"producer-a",
		"producerAttemptId":"attempt-1",
		"outputName":"result.json",
		"logicalUri":"jumi://runs/sample-http/nodes/producer-a/outputs/result.json",
		"nodeName":"node-a",
		"locations":[{"nodeLocal":{"nodeName":"node-a","path":"/var/lib/jumi-artifacts/cas/sha256/http"}}]
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
	service := newTestService(t, store)
	handler := NewHTTPHandler(service)

	req := httptest.NewRequest(http.MethodPost, "/v1/artifacts:register", strings.NewReader(`{
		"artifact": {
			"sampleRunId":"sample-http-env",
			"producerNodeId":"producer-a",
			"producerAttemptId":"attempt-1",
			"outputName":"result.json",
			"artifactId":"sample-http-env:producer-a:result.json",
			"logicalUri":"jumi://runs/sample-http-env/nodes/producer-a/outputs/result.json",
			"nodeName":"node-a",
			"locations":[{"nodeLocal":{"nodeName":"node-a","path":"/var/lib/jumi-artifacts/cas/sha256/env"}}]
		}
	}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("register status = %d, want 200", resp.Code)
	}
	artifact, ok, err := store.GetArtifact(context.Background(), "sample-http-env", "producer-a", "attempt-1", "result.json")
	if err != nil {
		t.Fatalf("get artifact: %v", err)
	}
	if !ok {
		t.Fatal("expected stored artifact")
	}
	if artifact.ArtifactID != "sample-http-env/producer-a/attempt-1/result.json" {
		t.Fatalf("artifactId = %q, want canonical artifact identity", artifact.ArtifactID)
	}
	if artifact.LogicalURI != "jumi://runs/sample-http-env/nodes/producer-a/outputs/result.json" {
		t.Fatalf("logicalUri = %q, want logical URI", artifact.LogicalURI)
	}
}

func TestHTTPRegisterArtifactCanonicalizesLegacyArtifactID(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := newTestService(t, store)
	handler := NewHTTPHandler(service)

	req := httptest.NewRequest(http.MethodPost, "/v1/artifacts:register", strings.NewReader(`{
		"artifact": {
			"sampleRunId":"sample-http-legacy",
			"producerNodeId":"producer-a",
			"producerAttemptId":"attempt-1",
			"outputName":"result.json",
			"artifactId":"sample-http-legacy:producer-a:result.json",
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
	artifact, ok, err := store.GetArtifact(context.Background(), "sample-http-legacy", "producer-a", "attempt-1", "result.json")
	if err != nil {
		t.Fatalf("get artifact: %v", err)
	}
	if !ok {
		t.Fatal("expected stored artifact")
	}
	if artifact.ArtifactID != "sample-http-legacy/producer-a/attempt-1/result.json" {
		t.Fatalf("artifactId = %q, want canonical artifact identity", artifact.ArtifactID)
	}
}

func TestHTTPGetArtifact(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := newTestService(t, store)
	handler := NewHTTPHandler(service)

	if _, err := service.RegisterArtifact(context.Background(), domain.Artifact{
		SampleRunID:       "sample-http-get",
		ProducerNodeID:    "producer-a",
		ProducerAttemptID: "attempt-1",
		OutputName:        "report",
		ArtifactID:        "sample-http-get/producer-a/attempt-1/report",
		Digest:            "sha256:abc123",
		NodeName:          "node-a",
		URI:               "jumi://runs/run-http-get/nodes/producer-a/outputs/report",
		SizeBytes:         4096,
	}); err != nil {
		t.Fatalf("register artifact: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/artifacts:get?sampleRunId=sample-http-get&producerNodeId=producer-a&attemptId=attempt-1&outputName=report", nil)
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
	service := newTestService(t, store)
	handler := NewHTTPHandler(service)

	req := httptest.NewRequest(http.MethodGet, "/v1/artifacts:get?sampleRunId=missing&producerNodeId=producer-a&attemptId=attempt-1&outputName=report", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("get artifact status = %d, want 404", resp.Code)
	}
}

func TestHTTPListArtifactsBySampleRun(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := newTestService(t, store)
	handler := NewHTTPHandler(service)

	for _, artifact := range []domain.Artifact{
		{
			SampleRunID:       "sample-http-list",
			ProducerNodeID:    "producer-a",
			ProducerAttemptID: "attempt-1",
			OutputName:        "report",
			ArtifactID:        "sample-http-list/producer-a/attempt-1/report",
			Digest:            "sha256:one",
			URI:               "jumi://runs/run-http-list/nodes/producer-a/outputs/report",
			SizeBytes:         512,
		},
		{
			SampleRunID:       "sample-http-list",
			ProducerNodeID:    "producer-b",
			ProducerAttemptID: "attempt-1",
			OutputName:        "metrics",
			ArtifactID:        "sample-http-list/producer-b/attempt-1/metrics",
			Digest:            "sha256:two",
			URI:               "jumi://runs/run-http-list/nodes/producer-b/outputs/metrics",
			SizeBytes:         128,
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

func TestHTTPListSources(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := newTestService(t, store)
	if _, err := service.RegisterArtifact(context.Background(), domain.Artifact{
		SampleRunID:       "sample-http-sources",
		ProducerNodeID:    "producer-a",
		ProducerAttemptID: "attempt-1",
		OutputName:        "dataset",
		ArtifactID:        "sample-http-sources/producer-a/attempt-1/dataset",
		Digest:            "sha256:abc123",
		URI:               "http://artifact-source.local/artifacts/abc123",
		Locations: []domain.Location{{
			NodeLocal: &domain.NodeLocalLocation{NodeName: "node-a", Path: "/var/lib/jumi-artifacts/cas/sha256/abc123"},
		}},
	}); err != nil {
		t.Fatalf("register artifact: %v", err)
	}

	handler := NewHTTPHandler(service)
	req := httptest.NewRequest(http.MethodGet, "/v1/sources:list?artifactId=sample-http-sources/producer-a/attempt-1/dataset", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Sources []domain.ArtifactSource `json:"sources"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode sources body: %v", err)
	}
	if len(body.Sources) != 2 {
		t.Fatalf("len(body.Sources) = %d, want 2", len(body.Sources))
	}
}

func TestResolveHandoffPlanningMode_IgnoresHTTPSourceOutsideAllowlist(t *testing.T) {
	t.Setenv("AH_ALLOWED_HTTP_SOURCE_HOSTS", "artifact-source.local")

	store := inventory.NewMemoryStore()
	service := newTestService(t, store)

	if _, err := service.RegisterArtifact(context.Background(), domain.Artifact{
		SampleRunID:       "sample-planning",
		ProducerNodeID:    "producer-a",
		ProducerAttemptID: "attempt-1",
		OutputName:        "dataset",
		ArtifactID:        "sample-planning/producer-a/attempt-1/dataset",
		Digest:            "sha256:abc123",
		URI:               "http://disallowed.example/artifacts/abc123",
	}); err != nil {
		t.Fatalf("register artifact: %v", err)
	}

	resolved, err := service.ResolveHandoff(context.Background(), domain.Binding{
		SampleRunID:        "sample-planning",
		BindingName:        "dataset",
		ChildNodeID:        "consumer-a",
		ProducerNodeID:     "producer-a",
		ProducerAttemptID:  "attempt-1",
		ProducerOutputName: "dataset",
		ConsumePolicy:      domain.ConsumePolicyRemoteOK,
		Required:           true,
	}, "")
	if err != nil {
		t.Fatalf("resolve handoff: %v", err)
	}
	if len(resolved.MaterializationCandidates) != 0 {
		t.Fatalf("materializationCandidates = %#v, want no candidates for disallowed host", resolved.MaterializationCandidates)
	}
}

func TestHTTPResolveHandoff(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := newTestService(t, store)
	handler := NewHTTPHandler(service)

	registerReq := httptest.NewRequest(http.MethodPost, "/v1/artifacts:register", strings.NewReader(`{
		"sampleRunId":"sample-resolve",
		"producerNodeId":"node-a",
		"producerAttemptId":"attempt-1",
		"outputName":"output",
		"nodeName":"node-a",
		"digest":"sha256:sample-resolve",
		"uri":"http://artifact-source.local/artifacts/sample-resolve",
		"sizeBytes":2048
	}`))
	registerReq.Header.Set("Content-Type", "application/json")
	registerResp := httptest.NewRecorder()
	handler.ServeHTTP(registerResp, registerReq)
	if registerResp.Code != http.StatusOK {
		t.Fatalf("register status = %d, want 200", registerResp.Code)
	}

	resolveReq := httptest.NewRequest(http.MethodPost, "/v1/handoffs:resolve", strings.NewReader(`{
		"binding": {
			"bindingName":"input",
			"sampleRunId":"sample-resolve",
			"producerNodeId":"node-a",
			"producerAttemptId":"attempt-1",
			"producerOutputName":"output",
			"consumePolicy":"RemoteOK",
			"required":true
		},
		"targetNodeName":"node-b"
	}`))
	resolveReq.Header.Set("Content-Type", "application/json")
	resolveResp := httptest.NewRecorder()
	handler.ServeHTTP(resolveResp, resolveReq)
	if resolveResp.Code != http.StatusOK {
		t.Fatalf("resolve status = %d, want 200: %s", resolveResp.Code, resolveResp.Body.String())
	}

	var body struct {
		Decision         string `json:"decision"`
		ResolutionStatus string `json:"resolutionStatus"`
		Retryable        bool   `json:"retryable"`
		PlacementIntent  struct {
			Mode     string `json:"mode"`
			NodeName string `json:"nodeName"`
		} `json:"placementIntent"`
		MaterializationPlan struct {
			Mode           string `json:"mode"`
			URI            string `json:"uri"`
			ExpectedDigest string `json:"expectedDigest"`
		} `json:"materializationPlan"`
	}
	if err := json.Unmarshal(resolveResp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal resolve response: %v", err)
	}
	if body.ResolutionStatus != string(domain.ResolutionStatusResolved) {
		t.Fatalf("resolutionStatus = %q, want %q", body.ResolutionStatus, domain.ResolutionStatusResolved)
	}
	if body.Decision != string(domain.ResolutionDecisionRemoteFetch) {
		t.Fatalf("decision = %q, want %q", body.Decision, domain.ResolutionDecisionRemoteFetch)
	}
	if body.PlacementIntent.Mode != string(domain.PlacementIntentModeNone) {
		t.Fatalf("placementIntent.mode = %q, want %q", body.PlacementIntent.Mode, domain.PlacementIntentModeNone)
	}
	if body.MaterializationPlan.Mode != string(domain.MaterializationModeRemoteFetch) {
		t.Fatalf("materializationPlan.mode = %q, want %q", body.MaterializationPlan.Mode, domain.MaterializationModeRemoteFetch)
	}
	if body.MaterializationPlan.URI == "" {
		t.Fatal("materializationPlan.uri must not be empty for remote_fetch")
	}
}

func TestHTTPResolveHandoff_LocalReuse(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := newTestService(t, store)
	handler := NewHTTPHandler(service)

	registerReq := httptest.NewRequest(http.MethodPost, "/v1/artifacts:register", strings.NewReader(`{
		"sampleRunId":"sample-local",
		"producerNodeId":"node-a",
		"producerAttemptId":"attempt-1",
		"outputName":"output",
		"nodeName":"node-a",
		"digest":"sha256:sample-local",
		"logicalUri":"jumi://runs/sample-local/nodes/node-a/outputs/output",
		"locations":[{"nodeLocal":{"nodeName":"node-a","path":"/var/lib/jumi-artifacts/cas/sha256/local"}}],
		"sizeBytes":1024
	}`))
	registerReq.Header.Set("Content-Type", "application/json")
	registerResp := httptest.NewRecorder()
	handler.ServeHTTP(registerResp, registerReq)
	if registerResp.Code != http.StatusOK {
		t.Fatalf("register status = %d, want 200", registerResp.Code)
	}

	resolveReq := httptest.NewRequest(http.MethodPost, "/v1/handoffs:resolve", strings.NewReader(`{
		"binding": {
			"bindingName":"input",
			"sampleRunId":"sample-local",
			"producerNodeId":"node-a",
			"producerAttemptId":"attempt-1",
			"producerOutputName":"output",
			"consumePolicy":"SameNodeOnly",
			"required":true
		},
		"targetNodeName":"node-a"
	}`))
	resolveReq.Header.Set("Content-Type", "application/json")
	resolveResp := httptest.NewRecorder()
	handler.ServeHTTP(resolveResp, resolveReq)
	if resolveResp.Code != http.StatusOK {
		t.Fatalf("resolve status = %d, want 200: %s", resolveResp.Code, resolveResp.Body.String())
	}

	var body struct {
		Decision        string `json:"decision"`
		PlacementIntent struct {
			Mode     string `json:"mode"`
			NodeName string `json:"nodeName"`
		} `json:"placementIntent"`
		MaterializationPlan struct {
			Mode           string `json:"mode"`
			LocalPath      string `json:"localPath"`
			SourceLocation struct {
				NodeLocal struct {
					NodeName string `json:"nodeName"`
					Path     string `json:"path"`
				} `json:"nodeLocal"`
			} `json:"sourceLocation"`
		} `json:"materializationPlan"`
	}
	if err := json.Unmarshal(resolveResp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal resolve response: %v", err)
	}
	if body.Decision != string(domain.ResolutionDecisionLocalReuse) {
		t.Fatalf("decision = %q, want %q", body.Decision, domain.ResolutionDecisionLocalReuse)
	}
	if body.PlacementIntent.Mode != string(domain.PlacementIntentModePreferredNode) {
		t.Fatalf("placementIntent.mode = %q, want preferred_node", body.PlacementIntent.Mode)
	}
	if body.PlacementIntent.NodeName != "node-a" {
		t.Fatalf("placementIntent.nodeName = %q, want node-a", body.PlacementIntent.NodeName)
	}
	if body.MaterializationPlan.Mode != string(domain.MaterializationModeLocalReuse) {
		t.Fatalf("materializationPlan.mode = %q, want local_reuse", body.MaterializationPlan.Mode)
	}
	if body.MaterializationPlan.SourceLocation.NodeLocal.Path != "/var/lib/jumi-artifacts/cas/sha256/local" {
		t.Fatalf("materializationPlan.sourceLocation.nodeLocal.path = %q, want node-local path", body.MaterializationPlan.SourceLocation.NodeLocal.Path)
	}
}

func TestHTTPNotifyTerminal(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := newTestService(t, store)
	handler := NewHTTPHandler(service)

	req := httptest.NewRequest(http.MethodPost, "/v1/nodes:notifyTerminal", strings.NewReader(`{
		"sampleRunId":"sample-notify",
		"nodeId":"node-a",
		"attemptId":"attempt-1",
		"terminalState":"Succeeded"
	}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("notifyTerminal status = %d, want 200: %s", resp.Code, resp.Body.String())
	}

	var body struct {
		Accepted bool `json:"accepted"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !body.Accepted {
		t.Fatal("accepted = false, want true")
	}

	rec, ok, err := store.GetNodeTerminal(context.Background(), "sample-notify", "node-a", "attempt-1")
	if err != nil {
		t.Fatalf("GetNodeTerminal: %v", err)
	}
	if !ok {
		t.Fatal("expected terminal record to be stored")
	}
	if rec.TerminalState != "Succeeded" {
		t.Fatalf("terminalState = %q, want Succeeded", rec.TerminalState)
	}
	if rec.AttemptID != "attempt-1" {
		t.Fatalf("attemptId = %q, want attempt-1", rec.AttemptID)
	}
}

func timePtr(v time.Time) *time.Time {
	return &v
}

func TestHTTPFinalizeAndEvaluateGC(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := newTestService(t, store)
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

func TestNotifyNodeTerminal_TerminalStateConflict(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := newTestService(t, store)
	ctx := context.Background()

	if err := service.NotifyNodeTerminal(ctx, "run-1", "node-a", "attempt-1", "Succeeded"); err != nil {
		t.Fatalf("first NotifyNodeTerminal: %v", err)
	}
	if err := service.NotifyNodeTerminal(ctx, "run-1", "node-a", "attempt-1", "Failed"); err == nil {
		t.Fatal("expected terminal state conflict error, got nil")
	}
}

func TestNotifyNodeTerminal_SameStateIsIdempotent(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := newTestService(t, store)
	ctx := context.Background()

	if err := service.NotifyNodeTerminal(ctx, "run-1", "node-a", "attempt-1", "Succeeded"); err != nil {
		t.Fatalf("first NotifyNodeTerminal: %v", err)
	}
	if err := service.NotifyNodeTerminal(ctx, "run-1", "node-a", "attempt-1", "Succeeded"); err != nil {
		t.Fatalf("second NotifyNodeTerminal (same state) must be idempotent: %v", err)
	}
}
