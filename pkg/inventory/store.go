package inventory

import (
	"context"
	"fmt"
	"strings"

	"github.com/HeaInSeo/artifact-handoff/pkg/domain"
)

// Store is the backend-agnostic persistence contract.
// Implementations: MemoryStore (tests / ephemeral), SQLiteStore (single-node persistence).
// Future backends (PostgreSQL, etcd) implement this interface without touching callers.
//
// F4 Mode B: every identity lookup, terminal partition and lifecycle/GC scope is
// keyed by RunID. SampleRunID is grouping metadata: ListArtifactsBySampleRun and
// ListRunLifecyclesBySample group several Runs of one Sample without merging their
// identities. Pre-F4 rows that carry no RunID are legacy-unresolved: no method
// returns them and nothing attributes them to a Run (they are never GC-evaluated).
type Store interface {
	PutArtifact(ctx context.Context, artifact domain.Artifact) error
	GetArtifact(ctx context.Context, runID, producerNodeID, attemptID, outputName string) (domain.Artifact, bool, error)
	GetArtifactByID(ctx context.Context, artifactID string) (domain.Artifact, bool, error)
	ListArtifactsByRun(ctx context.Context, runID string) ([]domain.Artifact, error)
	ListArtifactsBySampleRun(ctx context.Context, sampleRunID string) ([]domain.Artifact, error)
	PutArtifactSources(ctx context.Context, artifactID string, sources []domain.ArtifactSource) error
	GetArtifactSource(ctx context.Context, sourceID string) (domain.ArtifactSource, bool, error)
	ListArtifactSources(ctx context.Context, artifactID string) ([]domain.ArtifactSource, error)
	ListNodeTerminalsByRun(ctx context.Context, runID string) ([]domain.NodeTerminalRecord, error)
	RecordNodeTerminal(ctx context.Context, record domain.NodeTerminalRecord) error
	GetNodeTerminal(ctx context.Context, runID, nodeID, attemptID string) (domain.NodeTerminalRecord, bool, error)
	UpsertRunLifecycle(ctx context.Context, lifecycle domain.RunLifecycle) error
	GetRunLifecycle(ctx context.Context, runID string) (domain.RunLifecycle, bool, error)
	ListRunLifecyclesBySample(ctx context.Context, sampleRunID string) ([]domain.RunLifecycle, error)
}

// OpenStore constructs a Store from a DSN string and returns a shutdown function.
//
// Supported DSN formats:
//
//	""          → in-memory (same as "memory")
//	"memory"    → in-memory, no persistence
//	"sqlite:<path>"  → SQLite file at <path>  (e.g. "sqlite:/data/ah.db")
//
// The caller must invoke the returned shutdown function when done to release resources.
// For MemoryStore the shutdown function is a no-op.
func OpenStore(dsn string) (Store, func(), error) {
	switch {
	case dsn == "" || dsn == "memory":
		return NewMemoryStore(), func() {}, nil
	case strings.HasPrefix(dsn, "sqlite:"):
		path := strings.TrimPrefix(dsn, "sqlite:")
		s, err := NewSQLiteStore(path)
		if err != nil {
			return nil, nil, fmt.Errorf("open sqlite store %q: %w", path, err)
		}
		return s, func() { _ = s.Close() }, nil
	default:
		return nil, nil, fmt.Errorf("unsupported store DSN %q (supported: memory, sqlite:<path>)", dsn)
	}
}
