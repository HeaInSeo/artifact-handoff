package inventory_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/HeaInSeo/artifact-handoff/pkg/domain"
	"github.com/HeaInSeo/artifact-handoff/pkg/inventory"
)

// openSQLite opens a SQLiteStore backed by a temp file and returns it with a cleanup func.
func openSQLite(t *testing.T) (*inventory.SQLiteStore, func()) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ah_test.db")
	s, err := inventory.NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	return s, func() { _ = s.Close() }
}

func TestSQLiteStore_ArtifactRoundTrip(t *testing.T) {
	s, cleanup := openSQLite(t)
	defer cleanup()

	ctx := context.Background()
	want := domain.Artifact{
		SampleRunID:       "run-1",
		ProducerNodeID:    "node-a",
		ProducerAttemptID: "attempt-1",
		OutputName:        "output",
		ArtifactID:        "run-1:node-a:output",
		Digest:            "sha256:deadbeef",
		NodeName:          "k8s-node-1",
		URI:               "jumi://runs/run-1/nodes/node-a/outputs/output",
		SizeBytes:         4096,
		CreatedAt:         time.Now().UTC().Truncate(time.Second),
	}

	if err := s.PutArtifact(ctx, want); err != nil {
		t.Fatalf("PutArtifact: %v", err)
	}

	got, ok, err := s.GetArtifact(ctx, "run-1", "node-a", "attempt-1", "output")
	if err != nil {
		t.Fatalf("GetArtifact: %v", err)
	}
	if !ok {
		t.Fatal("GetArtifact: not found")
	}
	if got.Digest != want.Digest {
		t.Fatalf("digest = %q, want %q", got.Digest, want.Digest)
	}
	if got.SizeBytes != want.SizeBytes {
		t.Fatalf("sizeBytes = %d, want %d", got.SizeBytes, want.SizeBytes)
	}
	if got.URI != want.URI {
		t.Fatalf("uri = %q, want %q", got.URI, want.URI)
	}
}

func TestSQLiteStore_ArtifactNotFound(t *testing.T) {
	s, cleanup := openSQLite(t)
	defer cleanup()

	_, ok, err := s.GetArtifact(context.Background(), "missing", "node", "attempt", "output")
	if err != nil {
		t.Fatalf("GetArtifact: %v", err)
	}
	if ok {
		t.Fatal("expected not found")
	}
}

func TestSQLiteStore_ListArtifactsBySampleRun(t *testing.T) {
	s, cleanup := openSQLite(t)
	defer cleanup()
	ctx := context.Background()

	for _, a := range []domain.Artifact{
		{SampleRunID: "run-list", ProducerNodeID: "a", ProducerAttemptID: "1", OutputName: "x", URI: "u1", CreatedAt: time.Now().UTC()},
		{SampleRunID: "run-list", ProducerNodeID: "b", ProducerAttemptID: "1", OutputName: "y", URI: "u2", CreatedAt: time.Now().UTC()},
		{SampleRunID: "other-run", ProducerNodeID: "c", ProducerAttemptID: "1", OutputName: "z", URI: "u3", CreatedAt: time.Now().UTC()},
	} {
		if err := s.PutArtifact(ctx, a); err != nil {
			t.Fatalf("PutArtifact: %v", err)
		}
	}

	list, err := s.ListArtifactsBySampleRun(ctx, "run-list")
	if err != nil {
		t.Fatalf("ListArtifactsBySampleRun: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("len = %d, want 2", len(list))
	}
}

func TestSQLiteStore_NodeTerminalRoundTrip(t *testing.T) {
	s, cleanup := openSQLite(t)
	defer cleanup()
	ctx := context.Background()

	want := domain.NodeTerminalRecord{
		SampleRunID:   "run-term",
		NodeID:        "node-a",
		AttemptID:     "attempt-1",
		TerminalState: "Succeeded",
		RecordedAt:    time.Now().UTC().Truncate(time.Second),
	}

	if err := s.RecordNodeTerminal(ctx, want); err != nil {
		t.Fatalf("RecordNodeTerminal: %v", err)
	}

	got, ok, err := s.GetNodeTerminal(ctx, "run-term", "node-a", "attempt-1")
	if err != nil {
		t.Fatalf("GetNodeTerminal: %v", err)
	}
	if !ok {
		t.Fatal("GetNodeTerminal: not found")
	}
	if got.TerminalState != want.TerminalState {
		t.Fatalf("terminalState = %q, want %q", got.TerminalState, want.TerminalState)
	}
	if got.AttemptID != want.AttemptID {
		t.Fatalf("attemptId = %q, want %q", got.AttemptID, want.AttemptID)
	}
}

func TestSQLiteStore_SampleRunLifecycleRoundTrip(t *testing.T) {
	s, cleanup := openSQLite(t)
	defer cleanup()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	want := domain.SampleRunLifecycle{
		SampleRunID:           "run-lc",
		Finalized:             true,
		FinalizedAt:           &now,
		RetentionPolicySource: "default",
		RetentionDuration:     48 * time.Hour,
		GCEligible:            false,
		TerminalNodeCount:     3,
		SucceededNodeCount:    2,
		FailedNodeCount:       1,
		RetainedArtifactCount: 2,
		RetainedArtifactBytes: 8192,
	}

	if err := s.UpsertSampleRunLifecycle(ctx, want); err != nil {
		t.Fatalf("UpsertSampleRunLifecycle: %v", err)
	}

	got, ok, err := s.GetSampleRunLifecycle(ctx, "run-lc")
	if err != nil {
		t.Fatalf("GetSampleRunLifecycle: %v", err)
	}
	if !ok {
		t.Fatal("GetSampleRunLifecycle: not found")
	}
	if !got.Finalized {
		t.Fatal("finalized = false, want true")
	}
	if got.RetentionDuration != want.RetentionDuration {
		t.Fatalf("retentionDuration = %v, want %v", got.RetentionDuration, want.RetentionDuration)
	}
	if got.RetainedArtifactBytes != want.RetainedArtifactBytes {
		t.Fatalf("retainedArtifactBytes = %d, want %d", got.RetainedArtifactBytes, want.RetainedArtifactBytes)
	}
	if got.FinalizedAt == nil || !got.FinalizedAt.Equal(*want.FinalizedAt) {
		t.Fatalf("finalizedAt = %v, want %v", got.FinalizedAt, want.FinalizedAt)
	}
}

// TestSQLiteStore_Persistence is the core Step 5 test: write data, close the store,
// reopen the same file, and confirm data survives the restart.
func TestSQLiteStore_Persistence(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "ah.db")
	ctx := context.Background()

	// --- first "process": write data ---
	s1, err := inventory.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore (open): %v", err)
	}

	artifact := domain.Artifact{
		SampleRunID:       "run-persist",
		ProducerNodeID:    "node-a",
		ProducerAttemptID: "attempt-1",
		OutputName:        "result",
		Digest:            "sha256:persist",
		NodeName:          "k8s-node-1",
		URI:               "jumi://runs/run-persist/nodes/node-a/outputs/result",
		SizeBytes:         1024,
		CreatedAt:         time.Now().UTC(),
	}
	if err := s1.PutArtifact(ctx, artifact); err != nil {
		t.Fatalf("PutArtifact: %v", err)
	}

	termRec := domain.NodeTerminalRecord{
		SampleRunID:   "run-persist",
		NodeID:        "node-a",
		AttemptID:     "attempt-1",
		TerminalState: "Succeeded",
		RecordedAt:    time.Now().UTC(),
	}
	if err := s1.RecordNodeTerminal(ctx, termRec); err != nil {
		t.Fatalf("RecordNodeTerminal: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	lc := domain.SampleRunLifecycle{
		SampleRunID:           "run-persist",
		Finalized:             true,
		FinalizedAt:           &now,
		RetainedArtifactCount: 1,
		RetainedArtifactBytes: 1024,
	}
	if err := s1.UpsertSampleRunLifecycle(ctx, lc); err != nil {
		t.Fatalf("UpsertSampleRunLifecycle: %v", err)
	}

	// simulate process shutdown
	if err := s1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// --- second "process": reopen and verify ---
	s2, err := inventory.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore (reopen): %v", err)
	}
	defer s2.Close()

	gotArtifact, ok, err := s2.GetArtifact(ctx, "run-persist", "node-a", "attempt-1", "result")
	if err != nil {
		t.Fatalf("GetArtifact after restart: %v", err)
	}
	if !ok {
		t.Fatal("artifact not found after restart")
	}
	if gotArtifact.Digest != "sha256:persist" {
		t.Fatalf("digest = %q, want sha256:persist", gotArtifact.Digest)
	}

	gotTerm, ok, err := s2.GetNodeTerminal(ctx, "run-persist", "node-a", "attempt-1")
	if err != nil {
		t.Fatalf("GetNodeTerminal after restart: %v", err)
	}
	if !ok {
		t.Fatal("terminal record not found after restart")
	}
	if gotTerm.TerminalState != "Succeeded" {
		t.Fatalf("terminalState = %q, want Succeeded", gotTerm.TerminalState)
	}

	gotLC, ok, err := s2.GetSampleRunLifecycle(ctx, "run-persist")
	if err != nil {
		t.Fatalf("GetSampleRunLifecycle after restart: %v", err)
	}
	if !ok {
		t.Fatal("lifecycle not found after restart")
	}
	if !gotLC.Finalized {
		t.Fatal("lifecycle.finalized = false after restart")
	}
	if gotLC.RetainedArtifactBytes != 1024 {
		t.Fatalf("retainedArtifactBytes = %d, want 1024", gotLC.RetainedArtifactBytes)
	}
}

// TestOpenStore_DSN verifies the OpenStore factory routes DSNs correctly.
func TestOpenStore_DSN(t *testing.T) {
	t.Run("memory", func(t *testing.T) {
		store, shutdown, err := inventory.OpenStore("memory")
		if err != nil {
			t.Fatalf("OpenStore(memory): %v", err)
		}
		defer shutdown()
		if store == nil {
			t.Fatal("store is nil")
		}
	})

	t.Run("empty_defaults_to_memory", func(t *testing.T) {
		store, shutdown, err := inventory.OpenStore("")
		if err != nil {
			t.Fatalf("OpenStore(''): %v", err)
		}
		defer shutdown()
		if store == nil {
			t.Fatal("store is nil")
		}
	})

	t.Run("sqlite", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "test.db")
		store, shutdown, err := inventory.OpenStore("sqlite:" + dbPath)
		if err != nil {
			t.Fatalf("OpenStore(sqlite): %v", err)
		}
		defer shutdown()
		if store == nil {
			t.Fatal("store is nil")
		}
	})

	t.Run("unknown_dsn_returns_error", func(t *testing.T) {
		_, _, err := inventory.OpenStore("postgres://localhost/ah")
		if err == nil {
			t.Fatal("expected error for unknown DSN")
		}
	})
}
