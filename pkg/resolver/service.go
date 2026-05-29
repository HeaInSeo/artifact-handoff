package resolver

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"path"
	"strings"
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
	httpAllowedHosts      map[string]struct{}
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
		httpAllowedHosts:      parseAllowedHTTPHosts(os.Getenv("AH_ALLOWED_HTTP_SOURCE_HOSTS")),
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

func (s *Service) ListSources(ctx context.Context, artifactID string) ([]domain.ArtifactSource, error) {
	return s.ListSourcesCore(ctx, artifactID)
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
	if err := s.store.PutArtifactSources(ctx, artifact.ArtifactID, initialSourcesForArtifact(artifact, s.now())); err != nil {
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

func (s *Service) ListSourcesCore(ctx context.Context, artifactID string) ([]domain.ArtifactSource, error) {
	if artifactID == "" {
		return nil, fmt.Errorf("artifactID is required")
	}
	sources, err := s.store.ListArtifactSources(ctx, artifactID)
	if err != nil {
		return nil, err
	}
	filtered := sources[:0]
	for _, source := range sources {
		if source.State == domain.SourceStateDeleted {
			continue
		}
		filtered = append(filtered, source)
	}
	return filtered, nil
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
	sources, err := s.store.ListArtifactSources(ctx, artifact.ArtifactID)
	if err != nil {
		return domain.ResolvedHandoff{}, err
	}
	sources = effectiveArtifactSources(artifact, sources, s.now())
	candidateSources := s.candidateEligibleArtifactSources(artifact, sources)
	nodeLocalSource := firstReadyNodeLocalSource(candidateSources)
	remoteSource := firstReadyHTTPSource(candidateSources)
	expectedDigest := artifact.Digest
	if binding.ExpectedDigest != "" {
		expectedDigest = binding.ExpectedDigest
	}
	localPath := ""
	if binding.ChildInputName != "" {
		localPath = path.Join("inputs", binding.ChildInputName)
	}
	localCandidate := func(source domain.ArtifactSource, includeScheduledCondition bool) (domain.MaterializationCandidate, bool) {
		if strings.TrimSpace(expectedDigest) == "" {
			return domain.MaterializationCandidate{}, false
		}
		conditions := []domain.MaterializationCondition{{
			Kind:      "source_state_ready",
			SourceRef: source.SourceID,
			State:     string(source.State),
		}}
		if includeScheduledCondition && source.Location.NodeLocal != nil && source.Location.NodeLocal.NodeName != "" {
			conditions = append(conditions, domain.MaterializationCondition{
				Kind:     "scheduled_on_node",
				NodeName: source.Location.NodeLocal.NodeName,
			})
		}
		return domain.MaterializationCandidate{
			Priority:       1,
			Mode:           domain.MaterializationModeLocalReuse,
			SourceRef:      source.SourceID,
			ExpectedDigest: expectedDigest,
			ExpectedSize:   artifact.SizeBytes,
			LocalPath:      localPath,
			SourceLocation: &source.Location,
			Conditions:     conditions,
		}, true
	}
	remoteCandidate := func(source domain.ArtifactSource, priority int) (domain.MaterializationCandidate, bool) {
		if strings.TrimSpace(expectedDigest) == "" {
			return domain.MaterializationCandidate{}, false
		}
		conditions := []domain.MaterializationCondition{{
			Kind:      "source_state_ready",
			SourceRef: source.SourceID,
			State:     string(source.State),
		}, {
			Kind:      "backend_available",
			BackendID: source.BackendID,
		}}
		uri := ""
		if source.Location.HTTP != nil {
			uri = source.Location.HTTP.URI
		}
		return domain.MaterializationCandidate{
			Priority:       priority,
			Mode:           domain.MaterializationModeRemoteFetch,
			SourceRef:      source.SourceID,
			ExpectedDigest: expectedDigest,
			ExpectedSize:   artifact.SizeBytes,
			LocalPath:      localPath,
			SourceLocation: &source.Location,
			URI:            uri,
			Conditions:     conditions,
		}, true
	}
	legacyPlanFromCandidate := func(candidate domain.MaterializationCandidate) domain.MaterializationPlan {
		return domain.MaterializationPlan{
			Mode:           candidate.Mode,
			URI:            candidate.URI,
			ExpectedDigest: candidate.ExpectedDigest,
			ExpectedSize:   candidate.ExpectedSize,
			SourceLocation: candidate.SourceLocation,
			LocalPath:      candidate.LocalPath,
		}
	}
	producerNodeName := artifact.NodeName
	if nodeLocalSource != nil && nodeLocalSource.Location.NodeLocal != nil && nodeLocalSource.Location.NodeLocal.NodeName != "" {
		producerNodeName = nodeLocalSource.Location.NodeLocal.NodeName
	}
	anyNodeLocalSource := hasAnyNodeLocalSource(sources)
	localCandidates := []domain.MaterializationCandidate{}
	if nodeLocalSource != nil {
		if candidate, ok := localCandidate(*nodeLocalSource, targetNodeName == ""); ok {
			localCandidates = append(localCandidates, candidate)
		}
	} else if !anyNodeLocalSource && producerNodeName != "" && artifact.URI != "" {
		if strings.TrimSpace(expectedDigest) != "" {
			conditions := []domain.MaterializationCondition{}
			if targetNodeName == "" {
				conditions = append(conditions, domain.MaterializationCondition{
					Kind:     "scheduled_on_node",
					NodeName: producerNodeName,
				})
			}
			localCandidates = append(localCandidates, domain.MaterializationCandidate{
				Priority:       1,
				Mode:           domain.MaterializationModeLocalReuse,
				ExpectedDigest: expectedDigest,
				LocalPath:      localPath,
				URI:            artifact.URI,
				Conditions:     conditions,
			})
		}
	}
	remoteCandidates := []domain.MaterializationCandidate{}
	if remoteSource != nil {
		priority := 1
		if len(localCandidates) > 0 {
			priority = 2
		}
		if candidate, ok := remoteCandidate(*remoteSource, priority); ok {
			remoteCandidates = append(remoteCandidates, candidate)
		}
	}

	// targetNodeName is known → post-scheduling check.
	if targetNodeName != "" {
		if producerNodeName != "" && targetNodeName == producerNodeName && len(localCandidates) > 0 {
			candidates := append([]domain.MaterializationCandidate{}, localCandidates...)
			candidates = append(candidates, remoteCandidates...)
			if len(candidates) == 0 {
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
					NodeName: producerNodeName,
				},
				MaterializationPlan:       legacyPlanFromCandidate(candidates[0]),
				MaterializationCandidates: candidates,
				Reason:                    "artifact available on target node",
				Retryable:                 false,
			}, nil
		}
		// Consumer landed on a different node.
		switch binding.ConsumePolicy {
		case domain.ConsumePolicySameNodeOnly:
			return domain.ResolvedHandoff{
				Status:              domain.ResolutionStatusPolicyBlocked,
				Decision:            domain.ResolutionDecisionUnavailable,
				PlacementIntent:     domain.PlacementIntent{Mode: domain.PlacementIntentModeRequiredNode, NodeName: producerNodeName},
				MaterializationPlan: domain.MaterializationPlan{Mode: domain.MaterializationModeNone},
				Reason:              "policy requires same node but consumer is on a different node",
				Retryable:           false,
			}, nil
		default:
			if len(remoteCandidates) == 0 {
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
				Status:                    domain.ResolutionStatusResolved,
				Decision:                  domain.ResolutionDecisionRemoteFetch,
				PlacementIntent:           domain.PlacementIntent{Mode: domain.PlacementIntentModeNone},
				MaterializationPlan:       legacyPlanFromCandidate(remoteCandidates[0]),
				MaterializationCandidates: remoteCandidates,
				Reason:                    "artifact available via remote fetch",
				Retryable:                 false,
			}, nil
		}
	}

	// targetNodeName is empty → pre-scheduling / planning mode.
	// IncFallback is NOT called here: this is a plan, not an executed fallback.
	switch binding.ConsumePolicy {
	case domain.ConsumePolicySameNodeOnly:
		if len(localCandidates) == 0 || producerNodeName == "" {
			return domain.ResolvedHandoff{
				Status:              domain.ResolutionStatusUnavailable,
				Decision:            domain.ResolutionDecisionUnavailable,
				PlacementIntent:     domain.PlacementIntent{Mode: domain.PlacementIntentModeNone},
				MaterializationPlan: domain.MaterializationPlan{Mode: domain.MaterializationModeNone},
				Reason:              "planning: artifact locality unknown; cannot satisfy SameNodeOnly",
				Retryable:           false,
			}, nil
		}
		return domain.ResolvedHandoff{
			Status:   domain.ResolutionStatusResolved,
			Decision: domain.ResolutionDecisionLocalReuse,
			PlacementIntent: domain.PlacementIntent{
				Mode:     domain.PlacementIntentModeRequiredNode,
				NodeName: producerNodeName,
			},
			MaterializationPlan:       legacyPlanFromCandidate(localCandidates[0]),
			MaterializationCandidates: localCandidates,
			Reason:                    "planning: schedule consumer on producer node for local reuse",
			Retryable:                 false,
		}, nil
	case domain.ConsumePolicySameNodeThenRemote:
		if len(localCandidates) == 0 && len(remoteCandidates) == 0 {
			return domain.ResolvedHandoff{
				Status:              domain.ResolutionStatusUnavailable,
				Decision:            domain.ResolutionDecisionUnavailable,
				PlacementIntent:     domain.PlacementIntent{Mode: domain.PlacementIntentModeNone},
				MaterializationPlan: domain.MaterializationPlan{Mode: domain.MaterializationModeNone},
				Reason:              "planning: artifact URI unknown; cannot provide remote fetch plan",
				Retryable:           false,
			}, nil
		}
		if len(localCandidates) == 0 || producerNodeName == "" {
			// No locality hint; degrade to remote fetch without placement preference.
			if len(remoteCandidates) == 0 {
				return domain.ResolvedHandoff{
					Status:              domain.ResolutionStatusUnavailable,
					Decision:            domain.ResolutionDecisionUnavailable,
					PlacementIntent:     domain.PlacementIntent{Mode: domain.PlacementIntentModeNone},
					MaterializationPlan: domain.MaterializationPlan{Mode: domain.MaterializationModeNone},
					Reason:              "planning: artifact locality unknown and no fallback source is ready",
					Retryable:           false,
				}, nil
			}
			return domain.ResolvedHandoff{
				Status:                    domain.ResolutionStatusResolved,
				Decision:                  domain.ResolutionDecisionRemoteFetch,
				PlacementIntent:           domain.PlacementIntent{Mode: domain.PlacementIntentModeNone},
				MaterializationPlan:       legacyPlanFromCandidate(remoteCandidates[0]),
				MaterializationCandidates: remoteCandidates,
				Reason:                    "planning: artifact locality unknown; remote fetch without placement hint",
				Retryable:                 false,
			}, nil
		}
		candidates := append([]domain.MaterializationCandidate{}, localCandidates...)
		candidates = append(candidates, remoteCandidates...)
		legacyPlan := domain.MaterializationPlan{Mode: domain.MaterializationModeNone}
		decision := domain.ResolutionDecisionLocalReuse
		reason := "planning: require producer node because no fallback source is ready"
		placementMode := domain.PlacementIntentModeRequiredNode
		if len(remoteCandidates) > 0 {
			legacyPlan = legacyPlanFromCandidate(remoteCandidates[0])
			decision = domain.ResolutionDecisionRemoteFetch
			reason = "planning: prefer producer node; remote fetch if scheduled elsewhere"
			placementMode = domain.PlacementIntentModePreferredNode
		} else if len(localCandidates) > 0 {
			legacyPlan = legacyPlanFromCandidate(localCandidates[0])
		}
		return domain.ResolvedHandoff{
			Status:   domain.ResolutionStatusResolved,
			Decision: decision,
			PlacementIntent: domain.PlacementIntent{
				Mode:     placementMode,
				NodeName: producerNodeName,
			},
			MaterializationPlan:       legacyPlan,
			MaterializationCandidates: candidates,
			Reason:                    reason,
			Retryable:                 false,
		}, nil
	default: // ConsumePolicyRemoteOK
		if len(remoteCandidates) == 0 && len(localCandidates) == 0 {
			return domain.ResolvedHandoff{
				Status:              domain.ResolutionStatusUnavailable,
				Decision:            domain.ResolutionDecisionUnavailable,
				PlacementIntent:     domain.PlacementIntent{Mode: domain.PlacementIntentModeNone},
				MaterializationPlan: domain.MaterializationPlan{Mode: domain.MaterializationModeNone},
				Reason:              "planning: artifact URI unknown; cannot provide remote fetch plan",
				Retryable:           false,
			}, nil
		}
		candidates := append([]domain.MaterializationCandidate{}, localCandidates...)
		candidates = append(candidates, remoteCandidates...)
		legacyPlan := domain.MaterializationPlan{Mode: domain.MaterializationModeNone}
		if len(remoteCandidates) > 0 {
			legacyPlan = legacyPlanFromCandidate(remoteCandidates[0])
		} else if len(localCandidates) > 0 {
			legacyPlan = legacyPlanFromCandidate(localCandidates[0])
		}
		return domain.ResolvedHandoff{
			Status:                    domain.ResolutionStatusResolved,
			Decision:                  domain.ResolutionDecisionRemoteFetch,
			PlacementIntent:           domain.PlacementIntent{Mode: domain.PlacementIntentModeNone},
			MaterializationPlan:       legacyPlan,
			MaterializationCandidates: candidates,
			Reason:                    "planning: remote fetch, no placement constraint",
			Retryable:                 false,
		}, nil
	}
}

func initialSourcesForArtifact(artifact domain.Artifact, now time.Time) []domain.ArtifactSource {
	var sources []domain.ArtifactSource
	appendSource := func(backendID string, location domain.Location) {
		fp := domain.LocationFingerprint(location)
		if fp == "" {
			return
		}
		source := domain.ArtifactSource{
			SourceID:            domain.SourceID(artifact.ArtifactID, backendID, fp),
			ArtifactID:          artifact.ArtifactID,
			BackendID:           backendID,
			Digest:              artifact.Digest,
			State:               domain.SourceStateReady,
			LocationFingerprint: fp,
			Location:            location,
			CreatedAt:           now,
			UpdatedAt:           now,
		}
		if err := domain.ValidateArtifactSourceForArtifact(artifact, source); err != nil {
			return
		}
		for _, existing := range sources {
			if existing.SourceID == source.SourceID {
				return
			}
		}
		sources = append(sources, source)
	}
	for _, location := range artifact.Locations {
		switch {
		case location.NodeLocal != nil:
			appendSource("node-local-default", location)
		case location.HTTP != nil:
			appendSource("legacy-http", location)
		}
	}
	if artifact.URI != "" {
		appendSource("legacy-http", domain.Location{HTTP: &domain.HTTPSource{URI: artifact.URI}})
	}
	return sources
}

func effectiveArtifactSources(artifact domain.Artifact, sources []domain.ArtifactSource, now time.Time) []domain.ArtifactSource {
	if len(sources) > 0 {
		return sources
	}
	return initialSourcesForArtifact(artifact, now)
}

func (s *Service) candidateEligibleArtifactSources(artifact domain.Artifact, sources []domain.ArtifactSource) []domain.ArtifactSource {
	filtered := make([]domain.ArtifactSource, 0, len(sources))
	for _, source := range sources {
		if source.State != domain.SourceStateReady {
			continue
		}
		if err := domain.ValidateArtifactSourceForArtifact(artifact, source); err != nil {
			continue
		}
		if err := validateCandidateSource(source, s.httpAllowedHosts); err != nil {
			continue
		}
		filtered = append(filtered, source)
	}
	return filtered
}

func validateCandidateSource(source domain.ArtifactSource, allowedHosts map[string]struct{}) error {
	if source.Location.HTTP == nil {
		return nil
	}
	parsed, err := url.Parse(source.Location.HTTP.URI)
	if err != nil {
		return fmt.Errorf("invalid http source uri: %w", err)
	}
	switch parsed.Scheme {
	case "http", "https":
	default:
		return fmt.Errorf("unsupported http source scheme %q", parsed.Scheme)
	}
	host := strings.TrimSpace(parsed.Hostname())
	if host == "" {
		return fmt.Errorf("http source host is required")
	}
	if len(allowedHosts) != 0 {
		if _, ok := allowedHosts[strings.ToLower(host)]; !ok {
			return fmt.Errorf("http source host %q is not in allowlist", host)
		}
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return fmt.Errorf("http source host %q is not allowed", host)
		}
	}
	return nil
}

func parseAllowedHTTPHosts(raw string) map[string]struct{} {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	out := make(map[string]struct{})
	for _, part := range strings.Split(raw, ",") {
		host := strings.ToLower(strings.TrimSpace(part))
		if host == "" {
			continue
		}
		out[host] = struct{}{}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func firstReadyNodeLocalSource(sources []domain.ArtifactSource) *domain.ArtifactSource {
	for i := range sources {
		if sources[i].State == domain.SourceStateReady &&
			sources[i].Location.NodeLocal != nil &&
			sources[i].Location.NodeLocal.Path != "" {
			return &sources[i]
		}
	}
	return nil
}

func firstReadyHTTPSource(sources []domain.ArtifactSource) *domain.ArtifactSource {
	for i := range sources {
		if sources[i].State == domain.SourceStateReady &&
			sources[i].Location.HTTP != nil &&
			sources[i].Location.HTTP.URI != "" {
			return &sources[i]
		}
	}
	return nil
}

func hasAnyNodeLocalSource(sources []domain.ArtifactSource) bool {
	for i := range sources {
		if sources[i].Location.NodeLocal != nil && sources[i].Location.NodeLocal.Path != "" {
			return true
		}
	}
	return false
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
