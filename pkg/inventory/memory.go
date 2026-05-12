package inventory

import (
	"context"
	"sync"

	"github.com/HeaInSeo/artifact-handoff/internal/ids"
	"github.com/HeaInSeo/artifact-handoff/pkg/domain"
)

type MemoryStore struct {
	mu            sync.RWMutex
	artifacts     map[string]domain.Artifact
	nodeTerminals map[string]domain.NodeTerminalRecord
	sampleRuns    map[string]domain.SampleRunLifecycle
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		artifacts:     make(map[string]domain.Artifact),
		nodeTerminals: make(map[string]domain.NodeTerminalRecord),
		sampleRuns:    make(map[string]domain.SampleRunLifecycle),
	}
}

func (s *MemoryStore) PutArtifact(_ context.Context, artifact domain.Artifact) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.artifacts[artifact.Key()] = artifact
	return nil
}

func (s *MemoryStore) GetArtifact(_ context.Context, sampleRunID, producerNodeID, attemptID, outputName string) (domain.Artifact, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	artifact, ok := s.artifacts[ids.ArtifactKey{
		SampleRunID:       sampleRunID,
		ProducerNodeID:    producerNodeID,
		ProducerAttemptID: attemptID,
		OutputName:        outputName,
	}.String()]
	return artifact, ok, nil
}

func (s *MemoryStore) ListArtifactsBySampleRun(_ context.Context, sampleRunID string) ([]domain.Artifact, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]domain.Artifact, 0)
	for _, artifact := range s.artifacts {
		if artifact.SampleRunID == sampleRunID {
			out = append(out, artifact)
		}
	}
	return out, nil
}

func (s *MemoryStore) ListNodeTerminalsBySampleRun(_ context.Context, sampleRunID string) ([]domain.NodeTerminalRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]domain.NodeTerminalRecord, 0)
	for _, record := range s.nodeTerminals {
		if record.SampleRunID == sampleRunID {
			out = append(out, record)
		}
	}
	return out, nil
}

func (s *MemoryStore) RecordNodeTerminal(_ context.Context, record domain.NodeTerminalRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nodeTerminals[ids.NodeAttemptKey{
		SampleRunID: record.SampleRunID,
		NodeID:      record.NodeID,
		AttemptID:   record.AttemptID,
	}.String()] = record
	return nil
}

func (s *MemoryStore) GetNodeTerminal(_ context.Context, sampleRunID, nodeID, attemptID string) (domain.NodeTerminalRecord, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.nodeTerminals[ids.NodeAttemptKey{
		SampleRunID: sampleRunID,
		NodeID:      nodeID,
		AttemptID:   attemptID,
	}.String()]
	return record, ok, nil
}

func (s *MemoryStore) UpsertSampleRunLifecycle(_ context.Context, lifecycle domain.SampleRunLifecycle) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sampleRuns[lifecycle.SampleRunID] = lifecycle
	return nil
}

func (s *MemoryStore) GetSampleRunLifecycle(_ context.Context, sampleRunID string) (domain.SampleRunLifecycle, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	lifecycle, ok := s.sampleRuns[sampleRunID]
	return lifecycle, ok, nil
}
