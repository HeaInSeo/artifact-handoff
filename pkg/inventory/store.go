package inventory

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/HeaInSeo/artifact-handoff/pkg/domain"
)

// ErrSourceOwnershipConflict is returned by PutArtifactSources when a source ID is
// already recorded for a different artifact (a legacy-unresolved artifact or another
// Run's artifact). Nothing from that call is written: a source row never changes owner
// and is never rewritten through another artifact.
var ErrSourceOwnershipConflict = errors.New("source id is owned by another artifact")

// Store is the backend-agnostic persistence contract.
// Implementations: MemoryStore (tests / ephemeral), SQLiteStore (single-node persistence),
// PostgresStore (J2 profile, selected only by OpenStoreProfile; its versionless
// UpsertRunLifecycle is unsupported, see CompareAndSetRunLifecycle).
//
// F4 Mode B: every identity lookup, terminal partition and lifecycle/GC scope is
// keyed by RunID. SampleRunID is grouping metadata: ListArtifactsBySampleRun and
// ListRunLifecyclesBySample group several Runs of one Sample without merging their
// identities. Pre-F4 rows that carry no RunID are legacy-unresolved: no method
// returns them and nothing attributes them to a Run (they are never GC-evaluated).
// Source reads (GetArtifactSource, ListArtifactSources) follow the same rule: a source
// is returned only while its artifact is a live Run-keyed row. Source writes
// (PutArtifactSources) only update a source ID already owned by the same artifact;
// otherwise the call fails with ErrSourceOwnershipConflict and writes nothing.
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
	case strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://"):
		return nil, nil, fmt.Errorf("a PostgreSQL DSN requires the explicit %q store profile", ProfileJ2Postgres)
	default:
		return nil, nil, fmt.Errorf("unsupported store DSN %q (supported: memory, sqlite:<path>)", dsn)
	}
}

// OpenStoreProfile selects the store by an explicit profile, independent of the DSN. An empty
// profile keeps the OpenStore DSN behaviour. ProfileJ2Postgres opens only the PostgreSQL
// store: a missing DSN, a non-PostgreSQL DSN or an unreachable database is an error, never a
// fallback to the memory or SQLite store (TX1-06, C-4).
func OpenStoreProfile(ctx context.Context, profile, dsn string) (Store, func(), error) {
	switch profile {
	case "":
		return OpenStore(dsn)
	case ProfileJ2Postgres:
		if !strings.HasPrefix(dsn, "postgres://") && !strings.HasPrefix(dsn, "postgresql://") {
			return nil, nil, fmt.Errorf("store profile %q requires a postgres:// DSN", ProfileJ2Postgres)
		}
		s, err := NewPostgresStore(ctx, dsn)
		if err != nil {
			return nil, nil, err
		}
		return s, func() { _ = s.Close() }, nil
	default:
		return nil, nil, fmt.Errorf("unsupported store profile %q (supported: %q)", profile, ProfileJ2Postgres)
	}
}
