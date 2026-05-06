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
	store                 inventory.Store
	now                   func() time.Time
	metrics               *metrics.Registry
	minRetention          time.Duration
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
	return s.RegisterArtifactCore(ctx, artifact)
}

func (s *Service) ResolveHandoff(ctx context.Context, binding domain.Binding, targetNodeName string) (domain.ResolvedHandoff, error) {
	return s.ResolveHandoffCore(ctx, binding, targetNodeName)
}

func (s *Service) NotifyNodeTerminal(ctx context.Context, sampleRunID, nodeID, terminalState string) error {
	return s.NotifyNodeTerminalCore(ctx, sampleRunID, nodeID, terminalState)
}

func (s *Service) FinalizeSampleRun(ctx context.Context, sampleRunID string) error {
	return s.FinalizeSampleRunCore(ctx, sampleRunID)
}

func (s *Service) EvaluateGC(ctx context.Context, sampleRunID string) error {
	return s.EvaluateGCCore(ctx, sampleRunID)
}

func (s *Service) GetSampleRunLifecycle(ctx context.Context, sampleRunID string) (domain.SampleRunLifecycle, bool, error) {
	return s.GetSampleRunLifecycleCore(ctx, sampleRunID)
}

func (s *Service) GetArtifact(ctx context.Context, sampleRunID, producerNodeID, outputName string) (domain.Artifact, bool, error) {
	return s.GetArtifactCore(ctx, sampleRunID, producerNodeID, outputName)
}

func (s *Service) ListArtifactsBySampleRun(ctx context.Context, sampleRunID string) ([]domain.Artifact, error) {
	return s.ListArtifactsBySampleRunCore(ctx, sampleRunID)
}

func (s *Service) RegisterArtifactCore(ctx context.Context, artifact domain.Artifact) (domain.AvailabilityState, error) {
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

func (s *Service) GetArtifactCore(ctx context.Context, sampleRunID, producerNodeID, outputName string) (domain.Artifact, bool, error) {
	if sampleRunID == "" || producerNodeID == "" || outputName == "" {
		return domain.Artifact{}, false, fmt.Errorf("sampleRunID, producerNodeID, outputName are required")
	}
	return s.store.GetArtifact(ctx, sampleRunID, producerNodeID, outputName)
}

func (s *Service) ListArtifactsBySampleRunCore(ctx context.Context, sampleRunID string) ([]domain.Artifact, error) {
	if sampleRunID == "" {
		return nil, fmt.Errorf("sampleRunID is required")
	}
	return s.store.ListArtifactsBySampleRun(ctx, sampleRunID)
}

func (s *Service) ResolveHandoffCore(ctx context.Context, binding domain.Binding, targetNodeName string) (domain.ResolvedHandoff, error) {
	s.metrics.IncCounter("ah_resolve_requests_total")
	s.metrics.SetGauge("ah_gc_backlog_bytes", 0)
	if binding.SampleRunID == "" || binding.ProducerNodeID == "" || binding.ProducerOutputName == "" {
		return domain.ResolvedHandoff{}, fmt.Errorf("binding sampleRunID, producerNodeID, producerOutputName are required")
	}
	lifecycle, lifecycleFound, err := s.store.GetSampleRunLifecycle(ctx, binding.SampleRunID)
	if err != nil {
		return domain.ResolvedHandoff{}, err
	}
	if lifecycleFound && lifecycle.GCEligible {
		return domain.ResolvedHandoff{
			Status:   domain.ResolutionStatusMissing,
			Decision: domain.ResolutionDecisionUnavailable,
		}, nil
	}
	artifact, ok, err := s.store.GetArtifact(ctx, binding.SampleRunID, binding.ProducerNodeID, binding.ProducerOutputName)
	if err != nil {
		return domain.ResolvedHandoff{}, err
	}
	if !ok {
		terminal, terminalFound, err := s.store.GetNodeTerminal(ctx, binding.SampleRunID, binding.ProducerNodeID)
		if err != nil {
			return domain.ResolvedHandoff{}, err
		}
		if !terminalFound {
			return domain.ResolvedHandoff{
				Status:   domain.ResolutionStatusPending,
				Decision: domain.ResolutionDecisionUnavailable,
			}, nil
		}
		if terminal.TerminalState == "Failed" || terminal.TerminalState == "Canceled" {
			return domain.ResolvedHandoff{
				Status:   domain.ResolutionStatusMissing,
				Decision: domain.ResolutionDecisionProducerFailed,
			}, nil
		}
		if binding.Required {
			return domain.ResolvedHandoff{
				Status:   domain.ResolutionStatusMissing,
				Decision: domain.ResolutionDecisionUnavailable,
			}, nil
		}
		if terminal.TerminalState == "Succeeded" || terminal.TerminalState == "Failed" || terminal.TerminalState == "Canceled" {
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

func (s *Service) NotifyNodeTerminalCore(ctx context.Context, sampleRunID, nodeID, terminalState string) error {
	if sampleRunID == "" || nodeID == "" || terminalState == "" {
		return fmt.Errorf("sampleRunID, nodeID, terminalState are required")
	}
	switch terminalState {
	case "Succeeded", "Failed", "Canceled":
	default:
		return fmt.Errorf("unsupported terminalState %q", terminalState)
	}
	return s.store.RecordNodeTerminal(ctx, domain.NodeTerminalRecord{
		SampleRunID:   sampleRunID,
		NodeID:        nodeID,
		TerminalState: terminalState,
		RecordedAt:    s.now(),
	})
}

func (s *Service) FinalizeSampleRunCore(ctx context.Context, sampleRunID string) error {
	if sampleRunID == "" {
		return fmt.Errorf("sampleRunID is required")
	}
	now := s.now()
	lifecycle, ok, err := s.store.GetSampleRunLifecycle(ctx, sampleRunID)
	if err != nil {
		return err
	}
	if !ok {
		lifecycle = domain.SampleRunLifecycle{SampleRunID: sampleRunID}
	}
	if err := s.refreshLifecycleSnapshot(ctx, &lifecycle); err != nil {
		return err
	}
	lifecycle.Finalized = true
	lifecycle.FinalizedAt = &now
	lifecycle.RetentionPolicySource = s.retentionPolicySource
	lifecycle.RetentionDuration = s.minRetention
	retentionUntil := now.Add(s.minRetention)
	lifecycle.RetentionUntil = &retentionUntil
	lifecycle.GCEligible = false
	lifecycle.GCEligibleAt = nil
	lifecycle.GCBlockedReason = "gc_not_evaluated"
	s.metrics.SetGauge("ah_gc_backlog_bytes", 0)
	return s.store.UpsertSampleRunLifecycle(ctx, lifecycle)
}

func (s *Service) EvaluateGCCore(ctx context.Context, sampleRunID string) error {
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
	if err := s.refreshLifecycleSnapshot(ctx, &lifecycle); err != nil {
		return err
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
	s.metrics.SetGauge("ah_gc_backlog_bytes", float64(estimateGCBacklogBytes(lifecycle)))
	return s.store.UpsertSampleRunLifecycle(ctx, lifecycle)
}

func (s *Service) GetSampleRunLifecycleCore(ctx context.Context, sampleRunID string) (domain.SampleRunLifecycle, bool, error) {
	if sampleRunID == "" {
		return domain.SampleRunLifecycle{}, false, fmt.Errorf("sampleRunID is required")
	}
	return s.store.GetSampleRunLifecycle(ctx, sampleRunID)
}

func (s *Service) refreshLifecycleSnapshot(ctx context.Context, lifecycle *domain.SampleRunLifecycle) error {
	if lifecycle == nil {
		return fmt.Errorf("lifecycle is required")
	}
	artifacts, err := s.store.ListArtifactsBySampleRun(ctx, lifecycle.SampleRunID)
	if err != nil {
		return err
	}
	terminals, err := s.store.ListNodeTerminalsBySampleRun(ctx, lifecycle.SampleRunID)
	if err != nil {
		return err
	}
	lifecycle.RetainedArtifactCount = len(artifacts)
	lifecycle.RetainedArtifactBytes = 0
	lifecycle.TerminalNodeCount = len(terminals)
	lifecycle.SucceededNodeCount = 0
	lifecycle.FailedNodeCount = 0
	lifecycle.CanceledNodeCount = 0
	for _, artifact := range artifacts {
		if artifact.SizeBytes > 0 {
			lifecycle.RetainedArtifactBytes += artifact.SizeBytes
		}
	}
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
	return nil
}

func estimateGCBacklogBytes(lifecycle domain.SampleRunLifecycle) int {
	if !lifecycle.GCEligible || lifecycle.RetainedArtifactCount == 0 {
		return 0
	}
	if lifecycle.RetainedArtifactBytes <= 0 {
		return 0
	}
	return int(lifecycle.RetainedArtifactBytes)
}
