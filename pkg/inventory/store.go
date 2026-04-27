package inventory

import (
	"context"

	"github.com/HeaInSeo/artifact-handoff/pkg/domain"
)

type Store interface {
	PutArtifact(ctx context.Context, artifact domain.Artifact) error
	GetArtifact(ctx context.Context, sampleRunID, producerNodeID, outputName string) (domain.Artifact, bool, error)
	ListArtifactsBySampleRun(ctx context.Context, sampleRunID string) ([]domain.Artifact, error)
	ListNodeTerminalsBySampleRun(ctx context.Context, sampleRunID string) ([]domain.NodeTerminalRecord, error)
	RecordNodeTerminal(ctx context.Context, record domain.NodeTerminalRecord) error
	GetNodeTerminal(ctx context.Context, sampleRunID, nodeID string) (domain.NodeTerminalRecord, bool, error)
	UpsertSampleRunLifecycle(ctx context.Context, lifecycle domain.SampleRunLifecycle) error
	GetSampleRunLifecycle(ctx context.Context, sampleRunID string) (domain.SampleRunLifecycle, bool, error)
}
