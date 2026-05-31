package inventory_test

import (
	"context"
	"testing"
	"time"

	"github.com/HeaInSeo/artifact-handoff/pkg/domain"
	"github.com/HeaInSeo/artifact-handoff/pkg/inventory"
)

// ---------------------------------------------------------------------------
// MemoryStore — GetArtifact / GetArtifactByID / ListArtifactsBySampleRun
// ---------------------------------------------------------------------------

func TestMemoryStore_GetArtifact_RoundTrip(t *testing.T) {
	s := inventory.NewMemoryStore()
	ctx := context.Background()

	a := domain.Artifact{
		SampleRunID:       "run-mem",
		ProducerNodeID:    "node-a",
		ProducerAttemptID: "att-1",
		OutputName:        "model",
		ArtifactID:        "art-mem-1",
		Digest:            "sha256:abc",
		CreatedAt:         time.Now().UTC(),
	}
	if err := s.PutArtifact(ctx, a); err != nil {
		t.Fatalf("PutArtifact: %v", err)
	}

	got, ok, err := s.GetArtifact(ctx, "run-mem", "node-a", "att-1", "model")
	if err != nil {
		t.Fatalf("GetArtifact: %v", err)
	}
	if !ok {
		t.Fatal("GetArtifact: not found")
	}
	if got.ArtifactID != a.ArtifactID {
		t.Fatalf("ArtifactID = %q, want %q", got.ArtifactID, a.ArtifactID)
	}
}

func TestMemoryStore_GetArtifact_NotFound(t *testing.T) {
	s := inventory.NewMemoryStore()
	_, ok, err := s.GetArtifact(context.Background(), "no-run", "no-node", "no-att", "no-output")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected not found")
	}
}

func TestMemoryStore_GetArtifactByID_RoundTrip(t *testing.T) {
	s := inventory.NewMemoryStore()
	ctx := context.Background()

	a := domain.Artifact{
		SampleRunID:       "run-byid",
		ProducerNodeID:    "node-a",
		ProducerAttemptID: "att-1",
		OutputName:        "out",
		ArtifactID:        "art-byid-99",
		CreatedAt:         time.Now().UTC(),
	}
	if err := s.PutArtifact(ctx, a); err != nil {
		t.Fatalf("PutArtifact: %v", err)
	}

	got, ok, err := s.GetArtifactByID(ctx, "art-byid-99")
	if err != nil {
		t.Fatalf("GetArtifactByID: %v", err)
	}
	if !ok {
		t.Fatal("GetArtifactByID: not found")
	}
	if got.SampleRunID != a.SampleRunID {
		t.Fatalf("SampleRunID = %q, want %q", got.SampleRunID, a.SampleRunID)
	}
}

func TestMemoryStore_GetArtifactByID_NotFound(t *testing.T) {
	s := inventory.NewMemoryStore()
	_, ok, err := s.GetArtifactByID(context.Background(), "does-not-exist")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected not found")
	}
}

func TestMemoryStore_ListArtifactsBySampleRun(t *testing.T) {
	s := inventory.NewMemoryStore()
	ctx := context.Background()

	artifacts := []domain.Artifact{
		{SampleRunID: "run-ls", ProducerNodeID: "a", ProducerAttemptID: "1", OutputName: "x", CreatedAt: time.Now().UTC()},
		{SampleRunID: "run-ls", ProducerNodeID: "b", ProducerAttemptID: "1", OutputName: "y", CreatedAt: time.Now().UTC()},
		{SampleRunID: "other-run", ProducerNodeID: "c", ProducerAttemptID: "1", OutputName: "z", CreatedAt: time.Now().UTC()},
	}
	for _, a := range artifacts {
		if err := s.PutArtifact(ctx, a); err != nil {
			t.Fatalf("PutArtifact: %v", err)
		}
	}

	list, err := s.ListArtifactsBySampleRun(ctx, "run-ls")
	if err != nil {
		t.Fatalf("ListArtifactsBySampleRun: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("len = %d, want 2", len(list))
	}

	emptyList, err := s.ListArtifactsBySampleRun(ctx, "no-such-run")
	if err != nil {
		t.Fatalf("ListArtifactsBySampleRun (empty): %v", err)
	}
	if len(emptyList) != 0 {
		t.Fatalf("expected 0, got %d", len(emptyList))
	}
}

// ---------------------------------------------------------------------------
// MemoryStore — PutArtifactSources / ListArtifactSources / GetArtifactSource
// ---------------------------------------------------------------------------

func TestMemoryStore_ArtifactSources_RoundTrip(t *testing.T) {
	s := inventory.NewMemoryStore()
	ctx := context.Background()

	sources := []domain.ArtifactSource{
		{
			SourceID:   "src-mem-1",
			ArtifactID: "art-mem-src",
			BackendID:  "node-local-default",
			State:      domain.SourceStateReady,
		},
		{
			SourceID:   "src-mem-2",
			ArtifactID: "art-mem-src",
			BackendID:  "legacy-http",
			State:      domain.SourceStatePending,
		},
	}
	if err := s.PutArtifactSources(ctx, "art-mem-src", sources); err != nil {
		t.Fatalf("PutArtifactSources: %v", err)
	}

	list, err := s.ListArtifactSources(ctx, "art-mem-src")
	if err != nil {
		t.Fatalf("ListArtifactSources: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("len = %d, want 2", len(list))
	}

	got, ok, err := s.GetArtifactSource(ctx, "src-mem-1")
	if err != nil {
		t.Fatalf("GetArtifactSource: %v", err)
	}
	if !ok {
		t.Fatal("GetArtifactSource: not found")
	}
	if got.BackendID != "node-local-default" {
		t.Fatalf("BackendID = %q, want node-local-default", got.BackendID)
	}
}

func TestMemoryStore_GetArtifactSource_NotFound(t *testing.T) {
	s := inventory.NewMemoryStore()
	_, ok, err := s.GetArtifactSource(context.Background(), "no-such-src")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected not found")
	}
}

func TestMemoryStore_PutArtifactSources_EmptySourceIDSkipped(t *testing.T) {
	s := inventory.NewMemoryStore()
	ctx := context.Background()

	// A source with no SourceID must be silently skipped (not panic, not error)
	sources := []domain.ArtifactSource{
		{SourceID: "", ArtifactID: "art-x", BackendID: "node-local-default", State: domain.SourceStateReady},
	}
	if err := s.PutArtifactSources(ctx, "art-x", sources); err != nil {
		t.Fatalf("PutArtifactSources with empty SourceID: %v", err)
	}
	list, err := s.ListArtifactSources(ctx, "art-x")
	if err != nil {
		t.Fatalf("ListArtifactSources: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 sources, got %d", len(list))
	}
}

func TestMemoryStore_PutArtifactSources_BackfillsArtifactID(t *testing.T) {
	s := inventory.NewMemoryStore()
	ctx := context.Background()

	// ArtifactID empty in source — should be filled from the artifactID argument
	sources := []domain.ArtifactSource{
		{SourceID: "src-fill", ArtifactID: "", BackendID: "node-local-default", State: domain.SourceStateReady},
	}
	if err := s.PutArtifactSources(ctx, "art-fill", sources); err != nil {
		t.Fatalf("PutArtifactSources: %v", err)
	}
	got, ok, err := s.GetArtifactSource(ctx, "src-fill")
	if err != nil {
		t.Fatalf("GetArtifactSource: %v", err)
	}
	if !ok {
		t.Fatal("source not found")
	}
	if got.ArtifactID != "art-fill" {
		t.Fatalf("ArtifactID = %q, want art-fill", got.ArtifactID)
	}
}

func TestMemoryStore_PutArtifactSources_Idempotent(t *testing.T) {
	s := inventory.NewMemoryStore()
	ctx := context.Background()

	src := domain.ArtifactSource{
		SourceID:   "src-idem",
		ArtifactID: "art-idem",
		BackendID:  "node-local-default",
		State:      domain.SourceStateReady,
	}
	for i := 0; i < 3; i++ {
		if err := s.PutArtifactSources(ctx, "art-idem", []domain.ArtifactSource{src}); err != nil {
			t.Fatalf("PutArtifactSources (iter %d): %v", i, err)
		}
	}
	list, err := s.ListArtifactSources(ctx, "art-idem")
	if err != nil {
		t.Fatalf("ListArtifactSources: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 source after 3 idempotent puts, got %d", len(list))
	}
}

// ---------------------------------------------------------------------------
// MemoryStore — ListNodeTerminalsBySampleRun / GetNodeTerminal
// ---------------------------------------------------------------------------

func TestMemoryStore_NodeTerminal_RoundTrip(t *testing.T) {
	s := inventory.NewMemoryStore()
	ctx := context.Background()

	r := domain.NodeTerminalRecord{
		SampleRunID:   "run-nt",
		NodeID:        "node-a",
		AttemptID:     "att-1",
		TerminalState: "Succeeded",
		RecordedAt:    time.Now().UTC(),
	}
	if err := s.RecordNodeTerminal(ctx, r); err != nil {
		t.Fatalf("RecordNodeTerminal: %v", err)
	}

	got, ok, err := s.GetNodeTerminal(ctx, "run-nt", "node-a", "att-1")
	if err != nil {
		t.Fatalf("GetNodeTerminal: %v", err)
	}
	if !ok {
		t.Fatal("GetNodeTerminal: not found")
	}
	if got.TerminalState != "Succeeded" {
		t.Fatalf("TerminalState = %q, want Succeeded", got.TerminalState)
	}
}

func TestMemoryStore_GetNodeTerminal_NotFound(t *testing.T) {
	s := inventory.NewMemoryStore()
	_, ok, err := s.GetNodeTerminal(context.Background(), "no-run", "no-node", "no-att")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected not found")
	}
}

func TestMemoryStore_ListNodeTerminalsBySampleRun(t *testing.T) {
	s := inventory.NewMemoryStore()
	ctx := context.Background()

	records := []domain.NodeTerminalRecord{
		{SampleRunID: "run-list-nt", NodeID: "node-a", AttemptID: "1", TerminalState: "Succeeded", RecordedAt: time.Now().UTC()},
		{SampleRunID: "run-list-nt", NodeID: "node-b", AttemptID: "1", TerminalState: "Failed", RecordedAt: time.Now().UTC()},
		{SampleRunID: "other-run-nt", NodeID: "node-c", AttemptID: "1", TerminalState: "Succeeded", RecordedAt: time.Now().UTC()},
	}
	for _, r := range records {
		if err := s.RecordNodeTerminal(ctx, r); err != nil {
			t.Fatalf("RecordNodeTerminal: %v", err)
		}
	}

	list, err := s.ListNodeTerminalsBySampleRun(ctx, "run-list-nt")
	if err != nil {
		t.Fatalf("ListNodeTerminalsBySampleRun: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("len = %d, want 2", len(list))
	}
}

func TestMemoryStore_RecordNodeTerminal_SameStateIdempotent(t *testing.T) {
	s := inventory.NewMemoryStore()
	ctx := context.Background()

	r := domain.NodeTerminalRecord{
		SampleRunID:   "run-idem-nt",
		NodeID:        "node-a",
		AttemptID:     "att-1",
		TerminalState: "Succeeded",
		RecordedAt:    time.Now().UTC(),
	}
	if err := s.RecordNodeTerminal(ctx, r); err != nil {
		t.Fatalf("first RecordNodeTerminal: %v", err)
	}
	if err := s.RecordNodeTerminal(ctx, r); err != nil {
		t.Fatalf("second RecordNodeTerminal (same state) should be idempotent: %v", err)
	}
}

// ---------------------------------------------------------------------------
// MemoryStore — UpsertSampleRunLifecycle / GetSampleRunLifecycle
// ---------------------------------------------------------------------------

func TestMemoryStore_SampleRunLifecycle_RoundTrip(t *testing.T) {
	s := inventory.NewMemoryStore()
	ctx := context.Background()

	now := time.Now().UTC()
	lc := domain.SampleRunLifecycle{
		SampleRunID:           "run-lc-mem",
		Finalized:             true,
		FinalizedAt:           &now,
		RetentionPolicySource: "default",
		RetentionDuration:     24 * time.Hour,
		GCEligible:            false,
		TerminalNodeCount:     5,
		SucceededNodeCount:    4,
		FailedNodeCount:       1,
		RetainedArtifactCount: 3,
		RetainedArtifactBytes: 16384,
	}
	if err := s.UpsertSampleRunLifecycle(ctx, lc); err != nil {
		t.Fatalf("UpsertSampleRunLifecycle: %v", err)
	}

	got, ok, err := s.GetSampleRunLifecycle(ctx, "run-lc-mem")
	if err != nil {
		t.Fatalf("GetSampleRunLifecycle: %v", err)
	}
	if !ok {
		t.Fatal("GetSampleRunLifecycle: not found")
	}
	if !got.Finalized {
		t.Fatal("Finalized = false, want true")
	}
	if got.RetainedArtifactBytes != 16384 {
		t.Fatalf("RetainedArtifactBytes = %d, want 16384", got.RetainedArtifactBytes)
	}
	if got.RetentionDuration != 24*time.Hour {
		t.Fatalf("RetentionDuration = %v, want 24h", got.RetentionDuration)
	}
}

func TestMemoryStore_GetSampleRunLifecycle_NotFound(t *testing.T) {
	s := inventory.NewMemoryStore()
	_, ok, err := s.GetSampleRunLifecycle(context.Background(), "does-not-exist")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected not found")
	}
}

func TestMemoryStore_UpsertSampleRunLifecycle_Overwrites(t *testing.T) {
	s := inventory.NewMemoryStore()
	ctx := context.Background()

	lc := domain.SampleRunLifecycle{
		SampleRunID:           "run-upsert",
		Finalized:             false,
		RetainedArtifactCount: 1,
	}
	if err := s.UpsertSampleRunLifecycle(ctx, lc); err != nil {
		t.Fatalf("first Upsert: %v", err)
	}

	lc.Finalized = true
	lc.RetainedArtifactCount = 5
	if err := s.UpsertSampleRunLifecycle(ctx, lc); err != nil {
		t.Fatalf("second Upsert: %v", err)
	}

	got, ok, err := s.GetSampleRunLifecycle(ctx, "run-upsert")
	if err != nil {
		t.Fatalf("GetSampleRunLifecycle: %v", err)
	}
	if !ok {
		t.Fatal("not found after upsert")
	}
	if !got.Finalized {
		t.Fatal("expected Finalized = true after second upsert")
	}
	if got.RetainedArtifactCount != 5 {
		t.Fatalf("RetainedArtifactCount = %d, want 5", got.RetainedArtifactCount)
	}
}

// ---------------------------------------------------------------------------
// SQLiteStore — GetArtifactByID / ListNodeTerminalsBySampleRun / PutArtifactSources
// ---------------------------------------------------------------------------

func TestSQLiteStore_GetArtifactByID(t *testing.T) {
	s, cleanup := openSQLite(t)
	defer cleanup()
	ctx := context.Background()

	a := domain.Artifact{
		SampleRunID:       "run-byid",
		ProducerNodeID:    "node-a",
		ProducerAttemptID: "att-1",
		OutputName:        "out",
		ArtifactID:        "art-sqlite-byid",
		Digest:            "sha256:x",
		CreatedAt:         time.Now().UTC(),
	}
	if err := s.PutArtifact(ctx, a); err != nil {
		t.Fatalf("PutArtifact: %v", err)
	}

	got, ok, err := s.GetArtifactByID(ctx, "art-sqlite-byid")
	if err != nil {
		t.Fatalf("GetArtifactByID: %v", err)
	}
	if !ok {
		t.Fatal("GetArtifactByID: not found")
	}
	if got.SampleRunID != a.SampleRunID {
		t.Fatalf("SampleRunID = %q, want %q", got.SampleRunID, a.SampleRunID)
	}

	_, ok2, err2 := s.GetArtifactByID(ctx, "no-such-id")
	if err2 != nil {
		t.Fatalf("GetArtifactByID (missing): %v", err2)
	}
	if ok2 {
		t.Fatal("expected not found for missing artifact")
	}
}

func TestSQLiteStore_ArtifactSources_RoundTrip(t *testing.T) {
	s, cleanup := openSQLite(t)
	defer cleanup()
	ctx := context.Background()

	sources := []domain.ArtifactSource{
		{
			SourceID:            "src-sqlite-1",
			ArtifactID:          "art-sqlite-src",
			BackendID:           "node-local-default",
			Digest:              "sha256:abc",
			State:               domain.SourceStateReady,
			LocationFingerprint: "node_local:worker-1:/data",
			Location: domain.Location{
				NodeLocal: &domain.NodeLocalLocation{NodeName: "worker-1", Path: "/data"},
			},
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		},
	}
	if err := s.PutArtifactSources(ctx, "art-sqlite-src", sources); err != nil {
		t.Fatalf("PutArtifactSources: %v", err)
	}

	list, err := s.ListArtifactSources(ctx, "art-sqlite-src")
	if err != nil {
		t.Fatalf("ListArtifactSources: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("len = %d, want 1", len(list))
	}
	if list[0].BackendID != "node-local-default" {
		t.Fatalf("BackendID = %q, want node-local-default", list[0].BackendID)
	}

	got, ok, err := s.GetArtifactSource(ctx, "src-sqlite-1")
	if err != nil {
		t.Fatalf("GetArtifactSource: %v", err)
	}
	if !ok {
		t.Fatal("GetArtifactSource: not found")
	}
	if got.State != domain.SourceStateReady {
		t.Fatalf("State = %q, want %q", got.State, domain.SourceStateReady)
	}

	_, ok2, err2 := s.GetArtifactSource(ctx, "no-such-src")
	if err2 != nil {
		t.Fatalf("GetArtifactSource (missing): %v", err2)
	}
	if ok2 {
		t.Fatal("expected not found for missing source")
	}
}

func TestSQLiteStore_ListNodeTerminalsBySampleRun(t *testing.T) {
	s, cleanup := openSQLite(t)
	defer cleanup()
	ctx := context.Background()

	for _, r := range []domain.NodeTerminalRecord{
		{SampleRunID: "run-list-term", NodeID: "node-a", AttemptID: "1", TerminalState: "Succeeded", RecordedAt: time.Now().UTC()},
		{SampleRunID: "run-list-term", NodeID: "node-b", AttemptID: "1", TerminalState: "Failed", RecordedAt: time.Now().UTC()},
		{SampleRunID: "other-run-term", NodeID: "node-c", AttemptID: "1", TerminalState: "Succeeded", RecordedAt: time.Now().UTC()},
	} {
		if err := s.RecordNodeTerminal(ctx, r); err != nil {
			t.Fatalf("RecordNodeTerminal: %v", err)
		}
	}

	list, err := s.ListNodeTerminalsBySampleRun(ctx, "run-list-term")
	if err != nil {
		t.Fatalf("ListNodeTerminalsBySampleRun: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("len = %d, want 2", len(list))
	}

	empty, err := s.ListNodeTerminalsBySampleRun(ctx, "no-such-run")
	if err != nil {
		t.Fatalf("ListNodeTerminalsBySampleRun (empty): %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("expected 0, got %d", len(empty))
	}
}

func TestSQLiteStore_PutArtifact_ClearDigestRejected(t *testing.T) {
	s, cleanup := openSQLite(t)
	defer cleanup()
	ctx := context.Background()

	a := domain.Artifact{
		SampleRunID:       "run-clrdig",
		ProducerNodeID:    "node-a",
		ProducerAttemptID: "att-1",
		OutputName:        "out",
		Digest:            "sha256:original",
		CreatedAt:         time.Now().UTC(),
	}
	if err := s.PutArtifact(ctx, a); err != nil {
		t.Fatalf("first PutArtifact: %v", err)
	}

	// Attempt to overwrite with empty digest — must be rejected
	a.Digest = ""
	if err := s.PutArtifact(ctx, a); err == nil {
		t.Fatal("expected error when clearing digest, got nil")
	}
}

func TestSQLiteStore_ArtifactSources_BackfillsArtifactID(t *testing.T) {
	s, cleanup := openSQLite(t)
	defer cleanup()
	ctx := context.Background()

	sources := []domain.ArtifactSource{
		{
			SourceID:  "src-bfill",
			BackendID: "node-local-default",
			State:     domain.SourceStateReady,
			Location: domain.Location{
				NodeLocal: &domain.NodeLocalLocation{NodeName: "n", Path: "/p"},
			},
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		},
	}
	if err := s.PutArtifactSources(ctx, "art-bfill", sources); err != nil {
		t.Fatalf("PutArtifactSources: %v", err)
	}
	got, ok, err := s.GetArtifactSource(ctx, "src-bfill")
	if err != nil {
		t.Fatalf("GetArtifactSource: %v", err)
	}
	if !ok {
		t.Fatal("not found")
	}
	if got.ArtifactID != "art-bfill" {
		t.Fatalf("ArtifactID = %q, want art-bfill", got.ArtifactID)
	}
}
