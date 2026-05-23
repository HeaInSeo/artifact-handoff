package resolver

import (
	"context"
	"fmt"
	"path"
	"time"

	"github.com/HeaInSeo/artifact-handoff/pkg/domain"
	"github.com/HeaInSeo/artifact-handoff/pkg/inventory"
	"github.com/HeaInSeo/artifact-handoff/pkg/metrics"
)

type Service struct {
	store                 inventory.Store
	now                   func() time.Time
	metrics               *metrics.Metrics
	minRetention          time.Duration
	retentionPolicySource string
}

func NewService(store inventory.Store) (*Service, error) {
	m, err := metrics.New()
	if err != nil {
		return nil, fmt.Errorf("artifact-handoff: metrics init failed: %w", err)
	}
	return &Service{
		store:                 store,
		now:                   func() time.Time { return time.Now().UTC() },
		metrics:               m,
		minRetention:          15 * time.Minute,
		retentionPolicySource: "service_default",
	}, nil
}

func (s *Service) Metrics() *metrics.Metrics {
	return s.metrics
}

func (s *Service) RegisterArtifact(ctx context.Context, artifact domain.Artifact) (domain.AvailabilityState, error) {
	return s.RegisterArtifactCore(ctx, artifact)
}

func (s *Service) ResolveHandoff(ctx context.Context, binding domain.Binding, targetNodeName string) (domain.ResolvedHandoff, error) {
	return s.ResolveHandoffCore(ctx, binding, targetNodeName)
}

func (s *Service) NotifyNodeTerminal(ctx context.Context, sampleRunID, nodeID, attemptID, terminalState string) error {
	return s.NotifyNodeTerminalCore(ctx, sampleRunID, nodeID, attemptID, terminalState)
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

func (s *Service) GetArtifact(ctx context.Context, sampleRunID, producerNodeID, attemptID, outputName string) (domain.Artifact, bool, error) {
	return s.GetArtifactCore(ctx, sampleRunID, producerNodeID, attemptID, outputName)
}

func (s *Service) ListArtifactsBySampleRun(ctx context.Context, sampleRunID string) ([]domain.Artifact, error) {
	return s.ListArtifactsBySampleRunCore(ctx, sampleRunID)
}

func (s *Service) RegisterArtifactCore(ctx context.Context, artifact domain.Artifact) (domain.AvailabilityState, error) {
	if artifact.SampleRunID == "" || artifact.ProducerNodeID == "" || artifact.OutputName == "" {
		return "", fmt.Errorf("sampleRunID, producerNodeID, outputName are required")
	}
	if artifact.ProducerAttemptID == "" {
		return "", fmt.Errorf("producerAttemptID is required")
	}
	if artifact.CreatedAt.IsZero() {
		artifact.CreatedAt = s.now()
	}
	// ArtifactID must equal the canonical identity string.
	canonical := artifact.CanonicalID()
	if artifact.ArtifactID == "" {
		artifact.ArtifactID = canonical
	} else if artifact.ArtifactID != canonical {
		return "", fmt.Errorf("artifactID %q does not match canonical ID %q", artifact.ArtifactID, canonical)
	}
	// Enforce artifact immutability: same key + same digest = idempotent OK;
	// same key + different digest, or clearing an existing digest = conflict error.
	existing, exists, err := s.store.GetArtifact(ctx, artifact.SampleRunID, artifact.ProducerNodeID, artifact.ProducerAttemptID, artifact.OutputName)
	if err != nil {
		return "", err
	}
	if exists && existing.Digest != "" {
		if artifact.Digest == "" {
			return "", fmt.Errorf("artifact %s already registered with digest %s; refusing to clear digest",
				artifact.Key(), existing.Digest)
		}
		if artifact.Digest != existing.Digest {
			return "", fmt.Errorf("artifact %s already registered with digest %s; rejecting re-registration with digest %s",
				artifact.Key(), existing.Digest, artifact.Digest)
		}
	}
	if err := s.store.PutArtifact(ctx, artifact); err != nil {
		return "", err
	}
	s.metrics.IncArtifactsRegistered()
	hasNodeLocal := false
	for _, loc := range artifact.Locations {
		if loc.NodeLocal != nil && loc.NodeLocal.Path != "" {
			hasNodeLocal = true
			break
		}
	}
	switch {
	case artifact.NodeName != "" && artifact.URI != "":
		return domain.AvailabilityStateBoth, nil
	case hasNodeLocal && artifact.URI != "":
		return domain.AvailabilityStateBoth, nil
	case hasNodeLocal || artifact.NodeName != "":
		return domain.AvailabilityStateLocalOnly, nil
	case artifact.URI != "":
		return domain.AvailabilityStateRemoteOnly, nil
	default:
		return domain.AvailabilityStateUnavailable, nil
	}
}

func (s *Service) GetArtifactCore(ctx context.Context, sampleRunID, producerNodeID, attemptID, outputName string) (domain.Artifact, bool, error) {
	if sampleRunID == "" || producerNodeID == "" || outputName == "" {
		return domain.Artifact{}, false, fmt.Errorf("sampleRunID, producerNodeID, outputName are required")
	}
	if attemptID == "" {
		return domain.Artifact{}, false, fmt.Errorf("attemptID is required")
	}
	return s.store.GetArtifact(ctx, sampleRunID, producerNodeID, attemptID, outputName)
}

func (s *Service) ListArtifactsBySampleRunCore(ctx context.Context, sampleRunID string) ([]domain.Artifact, error) {
	if sampleRunID == "" {
		return nil, fmt.Errorf("sampleRunID is required")
	}
	return s.store.ListArtifactsBySampleRun(ctx, sampleRunID)
}

func (s *Service) ResolveHandoffCore(ctx context.Context, binding domain.Binding, targetNodeName string) (domain.ResolvedHandoff, error) {
	s.metrics.IncResolveRequests()
	if binding.SampleRunID == "" || binding.ProducerNodeID == "" || binding.ProducerOutputName == "" {
		return domain.ResolvedHandoff{}, fmt.Errorf("binding sampleRunID, producerNodeID, producerOutputName are required")
	}
	if binding.ProducerAttemptID == "" {
		return domain.ResolvedHandoff{}, fmt.Errorf("binding producerAttemptID is required")
	}
	if err := binding.ConsumePolicy.Validate(); err != nil {
		return domain.ResolvedHandoff{}, fmt.Errorf("binding %s: %w", binding.BindingName, err)
	}
	lifecycle, lifecycleFound, err := s.store.GetSampleRunLifecycle(ctx, binding.SampleRunID)
	if err != nil {
		return domain.ResolvedHandoff{}, err
	}
	if lifecycleFound && lifecycle.GCEligible {
		return domain.ResolvedHandoff{
			Status:              domain.ResolutionStatusGCExpired,
			Decision:            domain.ResolutionDecisionUnavailable,
			PlacementIntent:     domain.PlacementIntent{Mode: domain.PlacementIntentModeNone},
			MaterializationPlan: domain.MaterializationPlan{Mode: domain.MaterializationModeNone},
			Reason:              "sample run is GC eligible",
			Retryable:           false,
		}, nil
	}
	artifact, ok, err := s.store.GetArtifact(ctx, binding.SampleRunID, binding.ProducerNodeID, binding.ProducerAttemptID, binding.ProducerOutputName)
	if err != nil {
		return domain.ResolvedHandoff{}, err
	}
	if !ok {
		terminal, terminalFound, err := s.store.GetNodeTerminal(ctx, binding.SampleRunID, binding.ProducerNodeID, binding.ProducerAttemptID)
		if err != nil {
			return domain.ResolvedHandoff{}, err
		}
		if !terminalFound {
			return domain.ResolvedHandoff{
				Status:              domain.ResolutionStatusPending,
				Decision:            domain.ResolutionDecisionUnavailable,
				PlacementIntent:     domain.PlacementIntent{Mode: domain.PlacementIntentModeNone},
				MaterializationPlan: domain.MaterializationPlan{Mode: domain.MaterializationModeNone},
				Reason:              "producer not yet terminal",
				Retryable:           true,
			}, nil
		}
		if terminal.TerminalState == "Failed" || terminal.TerminalState == "Canceled" {
			return domain.ResolvedHandoff{
				Status:              domain.ResolutionStatusProducerFailed,
				Decision:            domain.ResolutionDecisionProducerFailed,
				PlacementIntent:     domain.PlacementIntent{Mode: domain.PlacementIntentModeNone},
				MaterializationPlan: domain.MaterializationPlan{Mode: domain.MaterializationModeNone},
				Reason:              "producer failed or canceled",
				Retryable:           false,
			}, nil
		}
		if binding.Required {
			return domain.ResolvedHandoff{
				Status:              domain.ResolutionStatusMissing,
				Decision:            domain.ResolutionDecisionUnavailable,
				PlacementIntent:     domain.PlacementIntent{Mode: domain.PlacementIntentModeNone},
				MaterializationPlan: domain.MaterializationPlan{Mode: domain.MaterializationModeNone},
				Reason:              "artifact not registered by producer",
				Retryable:           false,
			}, nil
		}
		if terminal.TerminalState == "Succeeded" || terminal.TerminalState == "Failed" || terminal.TerminalState == "Canceled" {
			return domain.ResolvedHandoff{
				Status:              domain.ResolutionStatusMissing,
				Decision:            domain.ResolutionDecisionUnavailable,
				PlacementIntent:     domain.PlacementIntent{Mode: domain.PlacementIntentModeNone},
				MaterializationPlan: domain.MaterializationPlan{Mode: domain.MaterializationModeNone},
				Reason:              "artifact not registered by producer",
				Retryable:           false,
			}, nil
		}
		return domain.ResolvedHandoff{
			Status:              domain.ResolutionStatusPending,
			Decision:            domain.ResolutionDecisionUnavailable,
			PlacementIntent:     domain.PlacementIntent{Mode: domain.PlacementIntentModeNone},
			MaterializationPlan: domain.MaterializationPlan{Mode: domain.MaterializationModeNone},
			Reason:              "producer not yet terminal",
			Retryable:           true,
		}, nil
	}
	if binding.ExpectedDigest != "" {
		if artifact.Digest == "" {
			return domain.ResolvedHandoff{
				Status:              domain.ResolutionStatusDigestMismatch,
				Decision:            domain.ResolutionDecisionUnavailable,
				PlacementIntent:     domain.PlacementIntent{Mode: domain.PlacementIntentModeNone},
				MaterializationPlan: domain.MaterializationPlan{Mode: domain.MaterializationModeNone},
				Reason:              "artifact has no digest; expected digest was specified",
				Retryable:           false,
			}, nil
		}
		if binding.ExpectedDigest != artifact.Digest {
			return domain.ResolvedHandoff{
				Status:              domain.ResolutionStatusDigestMismatch,
				Decision:            domain.ResolutionDecisionUnavailable,
				PlacementIntent:     domain.PlacementIntent{Mode: domain.PlacementIntentModeNone},
				MaterializationPlan: domain.MaterializationPlan{Mode: domain.MaterializationModeNone},
				Reason:              "artifact digest mismatch",
				Retryable:           false,
			}, nil
		}
	}
	// Build the materialization plan helpers up front — both branches may need them.
	nodeLocalLocation := firstNodeLocalLocation(artifact)
	localMatPlan := func() domain.MaterializationPlan {
		mp := domain.MaterializationPlan{Mode: domain.MaterializationModeLocalReuse, URI: artifact.URI}
		if artifact.Digest != "" {
			mp.ExpectedDigest = artifact.Digest
		}
		if binding.ExpectedDigest != "" {
			mp.ExpectedDigest = binding.ExpectedDigest
		}
		if nodeLocalLocation != nil {
			mp.SourceLocation = &domain.Location{NodeLocal: nodeLocalLocation}
		}
		if binding.ChildInputName != "" {
			mp.LocalPath = path.Join("/work/inputs", binding.ChildInputName)
		}
		return mp
	}
	remoteMatPlan := func() domain.MaterializationPlan {
		mp := domain.MaterializationPlan{Mode: domain.MaterializationModeRemoteFetch, URI: artifact.URI}
		if artifact.Digest != "" {
			mp.ExpectedDigest = artifact.Digest
		}
		if binding.ExpectedDigest != "" {
			mp.ExpectedDigest = binding.ExpectedDigest
		}
		return mp
	}

	// targetNodeName is known → post-scheduling check.
	if targetNodeName != "" {
		if artifact.NodeName != "" && targetNodeName == artifact.NodeName {
			if nodeLocalLocation == nil && artifact.URI == "" {
				return domain.ResolvedHandoff{
					Status:              domain.ResolutionStatusUnavailable,
					Decision:            domain.ResolutionDecisionUnavailable,
					PlacementIntent:     domain.PlacementIntent{Mode: domain.PlacementIntentModeNone},
					MaterializationPlan: domain.MaterializationPlan{Mode: domain.MaterializationModeNone},
					Reason:              "artifact local location unknown; cannot provide local reuse plan",
					Retryable:           false,
				}, nil
			}
			return domain.ResolvedHandoff{
				Status:   domain.ResolutionStatusResolved,
				Decision: domain.ResolutionDecisionLocalReuse,
				PlacementIntent: domain.PlacementIntent{
					Mode:     domain.PlacementIntentModePreferredNode,
					NodeName: artifact.NodeName,
				},
				MaterializationPlan: localMatPlan(),
				Reason:              "artifact available on target node",
				Retryable:           false,
			}, nil
		}
		// Consumer landed on a different node.
		switch binding.ConsumePolicy {
		case domain.ConsumePolicySameNodeOnly:
			return domain.ResolvedHandoff{
				Status:              domain.ResolutionStatusPolicyBlocked,
				Decision:            domain.ResolutionDecisionUnavailable,
				PlacementIntent:     domain.PlacementIntent{Mode: domain.PlacementIntentModeRequiredNode, NodeName: artifact.NodeName},
				MaterializationPlan: domain.MaterializationPlan{Mode: domain.MaterializationModeNone},
				Reason:              "policy requires same node but consumer is on a different node",
				Retryable:           false,
			}, nil
		default:
			if artifact.URI == "" {
				return domain.ResolvedHandoff{
					Status:              domain.ResolutionStatusUnavailable,
					Decision:            domain.ResolutionDecisionUnavailable,
					PlacementIntent:     domain.PlacementIntent{Mode: domain.PlacementIntentModeNone},
					MaterializationPlan: domain.MaterializationPlan{Mode: domain.MaterializationModeNone},
					Reason:              "artifact URI unknown; cannot provide remote fetch plan",
					Retryable:           false,
				}, nil
			}
			s.metrics.IncFallback()
			return domain.ResolvedHandoff{
				Status:              domain.ResolutionStatusResolved,
				Decision:            domain.ResolutionDecisionRemoteFetch,
				PlacementIntent:     domain.PlacementIntent{Mode: domain.PlacementIntentModeNone},
				MaterializationPlan: remoteMatPlan(),
				Reason:              "artifact available via remote fetch",
				Retryable:           false,
			}, nil
		}
	}

	// targetNodeName is empty → pre-scheduling / planning mode.
	// IncFallback is NOT called here: this is a plan, not an executed fallback.
	switch binding.ConsumePolicy {
	case domain.ConsumePolicySameNodeOnly:
		if artifact.NodeName == "" {
			return domain.ResolvedHandoff{
				Status:              domain.ResolutionStatusUnavailable,
				Decision:            domain.ResolutionDecisionUnavailable,
				PlacementIntent:     domain.PlacementIntent{Mode: domain.PlacementIntentModeNone},
				MaterializationPlan: domain.MaterializationPlan{Mode: domain.MaterializationModeNone},
				Reason:              "planning: artifact locality unknown; cannot satisfy SameNodeOnly",
				Retryable:           false,
			}, nil
		}
		if nodeLocalLocation == nil && artifact.URI == "" {
			return domain.ResolvedHandoff{
				Status:              domain.ResolutionStatusUnavailable,
				Decision:            domain.ResolutionDecisionUnavailable,
				PlacementIntent:     domain.PlacementIntent{Mode: domain.PlacementIntentModeNone},
				MaterializationPlan: domain.MaterializationPlan{Mode: domain.MaterializationModeNone},
				Reason:              "planning: artifact local location unknown; cannot provide materialization plan",
				Retryable:           false,
			}, nil
		}
		return domain.ResolvedHandoff{
			Status:   domain.ResolutionStatusResolved,
			Decision: domain.ResolutionDecisionLocalReuse,
			PlacementIntent: domain.PlacementIntent{
				Mode:     domain.PlacementIntentModeRequiredNode,
				NodeName: artifact.NodeName,
			},
			MaterializationPlan: localMatPlan(),
			Reason:              "planning: schedule consumer on producer node for local reuse",
			Retryable:           false,
		}, nil
	case domain.ConsumePolicySameNodeThenRemote:
		if artifact.URI == "" {
			return domain.ResolvedHandoff{
				Status:              domain.ResolutionStatusUnavailable,
				Decision:            domain.ResolutionDecisionUnavailable,
				PlacementIntent:     domain.PlacementIntent{Mode: domain.PlacementIntentModeNone},
				MaterializationPlan: domain.MaterializationPlan{Mode: domain.MaterializationModeNone},
				Reason:              "planning: artifact URI unknown; cannot provide remote fetch plan",
				Retryable:           false,
			}, nil
		}
		if artifact.NodeName == "" {
			// No locality hint; degrade to remote fetch without placement preference.
			return domain.ResolvedHandoff{
				Status:              domain.ResolutionStatusResolved,
				Decision:            domain.ResolutionDecisionRemoteFetch,
				PlacementIntent:     domain.PlacementIntent{Mode: domain.PlacementIntentModeNone},
				MaterializationPlan: remoteMatPlan(),
				Reason:              "planning: artifact locality unknown; remote fetch without placement hint",
				Retryable:           false,
			}, nil
		}
		return domain.ResolvedHandoff{
			Status:   domain.ResolutionStatusResolved,
			Decision: domain.ResolutionDecisionRemoteFetch,
			PlacementIntent: domain.PlacementIntent{
				Mode:     domain.PlacementIntentModePreferredNode,
				NodeName: artifact.NodeName,
			},
			MaterializationPlan: remoteMatPlan(),
			Reason:              "planning: prefer producer node; remote fetch if scheduled elsewhere",
			Retryable:           false,
		}, nil
	default: // ConsumePolicyRemoteOK
		if artifact.URI == "" {
			return domain.ResolvedHandoff{
				Status:              domain.ResolutionStatusUnavailable,
				Decision:            domain.ResolutionDecisionUnavailable,
				PlacementIntent:     domain.PlacementIntent{Mode: domain.PlacementIntentModeNone},
				MaterializationPlan: domain.MaterializationPlan{Mode: domain.MaterializationModeNone},
				Reason:              "planning: artifact URI unknown; cannot provide remote fetch plan",
				Retryable:           false,
			}, nil
		}
		return domain.ResolvedHandoff{
			Status:              domain.ResolutionStatusResolved,
			Decision:            domain.ResolutionDecisionRemoteFetch,
			PlacementIntent:     domain.PlacementIntent{Mode: domain.PlacementIntentModeNone},
			MaterializationPlan: remoteMatPlan(),
			Reason:              "planning: remote fetch, no placement constraint",
			Retryable:           false,
		}, nil
	}
}

func firstNodeLocalLocation(artifact domain.Artifact) *domain.NodeLocalLocation {
	for _, loc := range artifact.Locations {
		if loc.NodeLocal != nil && loc.NodeLocal.Path != "" {
			return loc.NodeLocal
		}
	}
	return nil
}

func (s *Service) NotifyNodeTerminalCore(ctx context.Context, sampleRunID, nodeID, attemptID, terminalState string) error {
	if sampleRunID == "" || nodeID == "" || terminalState == "" {
		return fmt.Errorf("sampleRunID, nodeID, terminalState are required")
	}
	if attemptID == "" {
		return fmt.Errorf("attemptID is required")
	}
	switch terminalState {
	case "Succeeded", "Failed", "Canceled":
	default:
		return fmt.Errorf("unsupported terminalState %q", terminalState)
	}
	return s.store.RecordNodeTerminal(ctx, domain.NodeTerminalRecord{
		SampleRunID:   sampleRunID,
		NodeID:        nodeID,
		AttemptID:     attemptID,
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
	s.metrics.SetGCBacklogBytes(0)
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
	s.metrics.SetGCBacklogBytes(float64(estimateGCBacklogBytes(lifecycle)))
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
