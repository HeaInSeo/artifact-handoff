package resolver

import (
	"context"
	"fmt"
	"time"

	"github.com/HeaInSeo/artifact-handoff/pkg/domain"
	"github.com/HeaInSeo/artifact-handoff/pkg/inventory"
	"github.com/HeaInSeo/artifact-handoff/pkg/metrics"
)

type Service struct {
	store                inventory.Store
	now                  func() time.Time
	metrics              *metrics.Registry
	minRetention         time.Duration
	retentionPolicySource string
}

func NewService(store inventory.Store) *Service {
	reg := metrics.NewRegistry()
	for _, name := range []string{
		"ah_artifacts_registered_total",
		"ah_resolve_requests_total",
		"ah_fallback_total",
	} {
		reg.EnsureCounter(name)
	}
	reg.EnsureGauge("ah_gc_backlog_bytes")
	return &Service{
		store:                 store,
		now:                   func() time.Time { return time.Now().UTC() },
		metrics:               reg,
		minRetention:          15 * time.Minute,
		retentionPolicySource: "service_default",
	}
}

func (s *Service) Metrics() *metrics.Registry {
	return s.metrics
}

func (s *Service) RegisterArtifact(ctx context.Context, artifact domain.Artifact) (domain.AvailabilityState, error) {
	if artifact.SampleRunID == "" || artifact.ProducerNodeID == "" || artifact.OutputName == "" {
		return "", fmt.Errorf("sampleRunID, producerNodeID, outputName are required")
	}
	if artifact.CreatedAt.IsZero() {
		artifact.CreatedAt = s.now()
	}
	if err := s.store.PutArtifact(ctx, artifact); err != nil {
		return "", err
	}
	s.metrics.IncCounter("ah_artifacts_registered_total")
	if artifact.NodeName != "" {
		return domain.AvailabilityStateLocalOnly, nil
	}
	return domain.AvailabilityStateBoth, nil
}

func (s *Service) ResolveHandoff(ctx context.Context, binding domain.Binding, targetNodeName string) (domain.ResolvedHandoff, error) {
	s.metrics.IncCounter("ah_resolve_requests_total")
	s.metrics.SetGauge("ah_gc_backlog_bytes", 0)
	if binding.SampleRunID == "" || binding.ProducerNodeID == "" || binding.ProducerOutputName == "" {
		return domain.ResolvedHandoff{}, fmt.Errorf("binding sampleRunID, producerNodeID, producerOutputName are required")
	}
	artifact, ok, err := s.store.GetArtifact(ctx, binding.SampleRunID, binding.ProducerNodeID, binding.ProducerOutputName)
	if err != nil {
		return domain.ResolvedHandoff{}, err
	}
	if !ok {
		if binding.Required {
			return domain.ResolvedHandoff{
				Status:   domain.ResolutionStatusMissing,
				Decision: domain.ResolutionDecisionUnavailable,
			}, nil
		}
		return domain.ResolvedHandoff{
			Status:   domain.ResolutionStatusPending,
			Decision: domain.ResolutionDecisionUnavailable,
		}, nil
	}
	if binding.ExpectedDigest != "" && artifact.Digest != "" && binding.ExpectedDigest != artifact.Digest {
		return domain.ResolvedHandoff{}, fmt.Errorf("artifact digest mismatch for binding %s", binding.BindingName)
	}
	if targetNodeName != "" && artifact.NodeName != "" && targetNodeName == artifact.NodeName {
		return domain.ResolvedHandoff{
			Status:                  domain.ResolutionStatusResolved,
			Decision:                domain.ResolutionDecisionLocalReuse,
			SourceNodeName:          artifact.NodeName,
			ArtifactURI:             artifact.URI,
			RequiresMaterialization: false,
		}, nil
	}
	switch binding.ConsumePolicy {
	case domain.ConsumePolicySameNodeOnly:
		return domain.ResolvedHandoff{
			Status:   domain.ResolutionStatusMissing,
			Decision: domain.ResolutionDecisionUnavailable,
		}, nil
	default:
		s.metrics.IncCounter("ah_fallback_total")
		return domain.ResolvedHandoff{
			Status:                  domain.ResolutionStatusResolved,
			Decision:                domain.ResolutionDecisionRemoteFetch,
			SourceNodeName:          artifact.NodeName,
			ArtifactURI:             artifact.URI,
			RequiresMaterialization: true,
		}, nil
	}
}

func (s *Service) NotifyNodeTerminal(ctx context.Context, sampleRunID, nodeID, terminalState string) error {
	if sampleRunID == "" || nodeID == "" || terminalState == "" {
		return fmt.Errorf("sampleRunID, nodeID, terminalState are required")
	}
	return s.store.RecordNodeTerminal(ctx, domain.NodeTerminalRecord{
		SampleRunID:   sampleRunID,
		NodeID:        nodeID,
		TerminalState: terminalState,
		RecordedAt:    s.now(),
	})
}

func (s *Service) FinalizeSampleRun(ctx context.Context, sampleRunID string) error {
	if sampleRunID == "" {
		return fmt.Errorf("sampleRunID is required")
	}
	now := s.now()
	artifacts, err := s.store.ListArtifactsBySampleRun(ctx, sampleRunID)
	if err != nil {
		return err
	}
	terminals, err := s.store.ListNodeTerminalsBySampleRun(ctx, sampleRunID)
	if err != nil {
		return err
	}
	lifecycle, ok, err := s.store.GetSampleRunLifecycle(ctx, sampleRunID)
	if err != nil {
		return err
	}
	if !ok {
		lifecycle = domain.SampleRunLifecycle{SampleRunID: sampleRunID}
	}
	lifecycle.Finalized = true
	lifecycle.FinalizedAt = &now
	lifecycle.RetentionPolicySource = s.retentionPolicySource
	lifecycle.RetentionDuration = s.minRetention
	retentionUntil := now.Add(s.minRetention)
	lifecycle.RetentionUntil = &retentionUntil
	lifecycle.RetainedArtifactCount = len(artifacts)
	lifecycle.TerminalNodeCount = len(terminals)
	lifecycle.SucceededNodeCount = 0
	lifecycle.FailedNodeCount = 0
	lifecycle.CanceledNodeCount = 0
	for _, record := range terminals {
		switch record.TerminalState {
		case "Succeeded":
			lifecycle.SucceededNodeCount++
		case "Failed":
			lifecycle.FailedNodeCount++
		case "Canceled":
			lifecycle.CanceledNodeCount++
		}
	}
	lifecycle.GCEligible = false
	lifecycle.GCEligibleAt = nil
	lifecycle.GCBlockedReason = "gc_not_evaluated"
	return s.store.UpsertSampleRunLifecycle(ctx, lifecycle)
}

func (s *Service) EvaluateGC(ctx context.Context, sampleRunID string) error {
	if sampleRunID == "" {
		return fmt.Errorf("sampleRunID is required")
	}
	lifecycle, ok, err := s.store.GetSampleRunLifecycle(ctx, sampleRunID)
	if err != nil {
		return err
	}
	if !ok {
		lifecycle = domain.SampleRunLifecycle{SampleRunID: sampleRunID}
	}
	switch {
	case !lifecycle.Finalized:
		lifecycle.GCEligible = false
		lifecycle.GCEligibleAt = nil
		lifecycle.GCBlockedReason = "sample_run_not_finalized"
	case lifecycle.TerminalNodeCount == 0:
		lifecycle.GCEligible = false
		lifecycle.GCEligibleAt = nil
		lifecycle.GCBlockedReason = "terminal_nodes_missing"
	case lifecycle.RetainedArtifactCount == 0:
		lifecycle.GCEligible = false
		lifecycle.GCEligibleAt = nil
		lifecycle.GCBlockedReason = "no_retained_artifacts"
	case lifecycle.RetentionUntil != nil && s.now().Before(*lifecycle.RetentionUntil):
		lifecycle.GCEligible = false
		lifecycle.GCEligibleAt = nil
		lifecycle.GCBlockedReason = "retention_window_active"
	default:
		now := s.now()
		lifecycle.GCEligible = true
		lifecycle.GCEligibleAt = &now
		lifecycle.GCBlockedReason = ""
	}
	s.metrics.SetGauge("ah_gc_backlog_bytes", float64(lifecycle.RetainedArtifactCount*1024))
	return s.store.UpsertSampleRunLifecycle(ctx, lifecycle)
}

func (s *Service) GetSampleRunLifecycle(ctx context.Context, sampleRunID string) (domain.SampleRunLifecycle, bool, error) {
	if sampleRunID == "" {
		return domain.SampleRunLifecycle{}, false, fmt.Errorf("sampleRunID is required")
	}
	return s.store.GetSampleRunLifecycle(ctx, sampleRunID)
}
