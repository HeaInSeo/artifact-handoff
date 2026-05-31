package resolver

import (
	"context"
	"errors"
	"time"

	ahv1 "github.com/HeaInSeo/artifact-handoff/api/proto/ahv1"
	"github.com/HeaInSeo/artifact-handoff/pkg/domain"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type grpcResolverServer struct {
	ahv1.UnimplementedArtifactHandoffResolverServer
	service *Service
}

func RegisterGRPCService(server grpc.ServiceRegistrar, service *Service) {
	ahv1.RegisterArtifactHandoffResolverServer(server, &grpcResolverServer{service: service})
}

func toGRPCError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, ErrNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, ErrInvalidArgument):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, ErrAlreadyExists):
		return status.Error(codes.AlreadyExists, err.Error())
	case errors.Is(err, ErrFailedPrecondition):
		return status.Error(codes.FailedPrecondition, err.Error())
	default:
		return status.Error(codes.Internal, err.Error())
	}
}

func (s *grpcResolverServer) RegisterArtifact(ctx context.Context, req *ahv1.RegisterArtifactRequest) (*ahv1.RegisterArtifactResponse, error) {
	s.service.Metrics().IncGRPCRegisterArtifact()
	artifact := req.GetArtifact()
	state, err := s.service.RegisterArtifactCore(ctx, domain.Artifact{
		SampleRunID:       artifact.GetSampleRunId(),
		ProducerNodeID:    artifact.GetProducerNodeId(),
		ProducerAttemptID: artifact.GetProducerAttemptId(),
		OutputName:        artifact.GetOutputName(),
		ArtifactID:        artifact.GetArtifactId(),
		Digest:            artifact.GetDigest(),
		NodeName:          artifact.GetNodeName(),
		URI:               artifact.GetUri(),
		LogicalURI:        artifact.GetLogicalUri(),
		Locations:         grpcLocationsToDomain(artifact.GetLocations()),
		SizeBytes:         artifact.GetSizeBytes(),
	})
	if err != nil {
		s.service.Metrics().IncGRPCRegisterArtifactErrors()
		return nil, toGRPCError(err)
	}
	return &ahv1.RegisterArtifactResponse{AvailabilityState: string(state)}, nil
}

func (s *grpcResolverServer) AddSource(ctx context.Context, req *ahv1.AddSourceRequest) (*ahv1.AddSourceResponse, error) {
	source, err := s.service.AddSourceCore(ctx, req.GetArtifactId(), grpcSourceToDomain(req.GetSource()))
	if err != nil {
		return nil, toGRPCError(err)
	}
	return &ahv1.AddSourceResponse{Source: grpcSourceFromDomain(source)}, nil
}

func (s *grpcResolverServer) UpdateSourceState(ctx context.Context, req *ahv1.UpdateSourceStateRequest) (*ahv1.UpdateSourceStateResponse, error) {
	source, err := s.service.UpdateSourceStateCore(ctx, req.GetSourceId(), domain.SourceState(req.GetState()))
	if err != nil {
		return nil, toGRPCError(err)
	}
	return &ahv1.UpdateSourceStateResponse{Source: grpcSourceFromDomain(source)}, nil
}

func (s *grpcResolverServer) ListSources(ctx context.Context, req *ahv1.ListSourcesRequest) (*ahv1.ListSourcesResponse, error) {
	sources, err := s.service.ListSourcesCore(ctx, req.GetArtifactId())
	if err != nil {
		return nil, toGRPCError(err)
	}
	return &ahv1.ListSourcesResponse{Sources: grpcSourcesFromDomain(sources)}, nil
}

func (s *grpcResolverServer) VerifySource(ctx context.Context, req *ahv1.VerifySourceRequest) (*ahv1.VerifySourceResponse, error) {
	source, verified, reason, err := s.service.VerifySourceCore(ctx, req.GetSourceId())
	if err != nil {
		return nil, toGRPCError(err)
	}
	return &ahv1.VerifySourceResponse{
		Source:   grpcSourceFromDomain(source),
		Verified: verified,
		Reason:   reason,
	}, nil
}

func (s *grpcResolverServer) ResolveHandoff(ctx context.Context, req *ahv1.ResolveHandoffRequest) (*ahv1.ResolveHandoffResponse, error) {
	s.service.Metrics().IncGRPCResolveHandoff()
	binding := req.GetBinding()
	resolved, err := s.service.ResolveHandoffCore(ctx, domain.Binding{
		BindingName:        binding.GetBindingName(),
		SampleRunID:        binding.GetSampleRunId(),
		ChildNodeID:        binding.GetChildNodeId(),
		ChildInputName:     binding.GetChildInputName(),
		ProducerNodeID:     binding.GetProducerNodeId(),
		ProducerAttemptID:  binding.GetProducerAttemptId(),
		ChildAttemptID:     binding.GetChildAttemptId(),
		ProducerOutputName: binding.GetProducerOutputName(),
		ArtifactID:         binding.GetArtifactId(),
		ConsumePolicy:      domain.ConsumePolicy(binding.GetConsumePolicy()),
		ExpectedDigest:     binding.GetExpectedDigest(),
		Required:           binding.GetRequired(),
	}, req.GetTargetNodeName())
	if err != nil {
		s.service.Metrics().IncGRPCResolveHandoffErrors()
		return nil, toGRPCError(err)
	}
	return &ahv1.ResolveHandoffResponse{
		ResolutionStatus: string(resolved.Status),
		Decision:         string(resolved.Decision),
		PlacementIntent: &ahv1.PlacementIntent{
			Mode:     string(resolved.PlacementIntent.Mode),
			NodeName: resolved.PlacementIntent.NodeName,
		},
		MaterializationPlan: &ahv1.MaterializationPlan{
			Mode:              string(resolved.MaterializationPlan.Mode),
			Uri:               resolved.MaterializationPlan.URI,
			ExpectedDigest:    resolved.MaterializationPlan.ExpectedDigest,
			ExpectedSizeBytes: resolved.MaterializationPlan.ExpectedSize,
			SourceLocation:    grpcLocationFromDomain(resolved.MaterializationPlan.SourceLocation),
			LocalPath:         resolved.MaterializationPlan.LocalPath,
		},
		Reason:                    resolved.Reason,
		Retryable:                 resolved.Retryable,
		MaterializationCandidates: grpcCandidatesFromDomain(resolved.MaterializationCandidates),
	}, nil
}

func (s *grpcResolverServer) NotifyNodeTerminal(ctx context.Context, req *ahv1.NotifyNodeTerminalRequest) (*ahv1.NotifyNodeTerminalResponse, error) {
	s.service.Metrics().IncGRPCNotifyNodeTerminal()
	if err := s.service.NotifyNodeTerminalCore(ctx, req.GetSampleRunId(), req.GetNodeId(), req.GetAttemptId(), req.GetTerminalState()); err != nil {
		return nil, toGRPCError(err)
	}
	return &ahv1.NotifyNodeTerminalResponse{Accepted: true}, nil
}

func (s *grpcResolverServer) FinalizeSampleRun(ctx context.Context, req *ahv1.FinalizeSampleRunRequest) (*ahv1.FinalizeSampleRunResponse, error) {
	s.service.Metrics().IncGRPCFinalizeSampleRun()
	if err := s.service.FinalizeSampleRunCore(ctx, req.GetSampleRunId()); err != nil {
		return nil, toGRPCError(err)
	}
	return &ahv1.FinalizeSampleRunResponse{Accepted: true}, nil
}

func (s *grpcResolverServer) EvaluateGC(ctx context.Context, req *ahv1.EvaluateGCRequest) (*ahv1.EvaluateGCResponse, error) {
	s.service.Metrics().IncGRPCEvaluateGC()
	if err := s.service.EvaluateGCCore(ctx, req.GetSampleRunId()); err != nil {
		return nil, toGRPCError(err)
	}
	return &ahv1.EvaluateGCResponse{Accepted: true}, nil
}

func (s *grpcResolverServer) GetSampleRunLifecycle(ctx context.Context, req *ahv1.GetSampleRunLifecycleRequest) (*ahv1.GetSampleRunLifecycleResponse, error) {
	s.service.Metrics().IncGRPCGetLifecycle()
	lifecycle, ok, err := s.service.GetSampleRunLifecycleCore(ctx, req.GetSampleRunId())
	if err != nil {
		return nil, toGRPCError(err)
	}
	if !ok {
		return nil, status.Error(codes.NotFound, "sample run lifecycle not found")
	}
	return lifecycleToGRPC(lifecycle), nil
}

func lifecycleToGRPC(lifecycle domain.SampleRunLifecycle) *ahv1.GetSampleRunLifecycleResponse {
	resp := &ahv1.GetSampleRunLifecycleResponse{
		SampleRunId:           lifecycle.SampleRunID,
		Finalized:             lifecycle.Finalized,
		RetentionPolicySource: lifecycle.RetentionPolicySource,
		RetentionDuration:     lifecycle.RetentionDuration.String(),
		GcEligible:            lifecycle.GCEligible,
		GcBlockedReason:       lifecycle.GCBlockedReason,
		TerminalNodeCount:     int32(lifecycle.TerminalNodeCount),     //nolint:gosec
		SucceededNodeCount:    int32(lifecycle.SucceededNodeCount),    //nolint:gosec
		FailedNodeCount:       int32(lifecycle.FailedNodeCount),       //nolint:gosec
		CanceledNodeCount:     int32(lifecycle.CanceledNodeCount),     //nolint:gosec
		RetainedArtifactCount: int32(lifecycle.RetainedArtifactCount), //nolint:gosec
		RetainedArtifactBytes: lifecycle.RetainedArtifactBytes,
	}
	if lifecycle.FinalizedAt != nil {
		resp.FinalizedAt = lifecycle.FinalizedAt.UTC().Format(time.RFC3339Nano)
	}
	if lifecycle.RetentionUntil != nil {
		resp.RetentionUntil = lifecycle.RetentionUntil.UTC().Format(time.RFC3339Nano)
	}
	if lifecycle.GCEligibleAt != nil {
		resp.GcEligibleAt = lifecycle.GCEligibleAt.UTC().Format(time.RFC3339Nano)
	}
	return resp
}

func grpcLocationsToDomain(locations []*ahv1.ArtifactLocation) []domain.Location {
	if len(locations) == 0 {
		return nil
	}
	out := make([]domain.Location, 0, len(locations))
	for _, location := range locations {
		if location == nil {
			continue
		}
		var converted domain.Location
		switch backend := location.GetBackend().(type) {
		case *ahv1.ArtifactLocation_NodeLocal:
			converted.NodeLocal = &domain.NodeLocalLocation{
				NodeName: backend.NodeLocal.GetNodeName(),
				Path:     backend.NodeLocal.GetPath(),
			}
		case *ahv1.ArtifactLocation_Http:
			converted.HTTP = &domain.HTTPSource{
				URI: backend.Http.GetUri(),
			}
		}
		out = append(out, converted)
	}
	return out
}

func grpcLocationFromDomain(location *domain.Location) *ahv1.ArtifactLocation {
	if location == nil {
		return nil
	}
	switch {
	case location.NodeLocal != nil:
		return &ahv1.ArtifactLocation{
			Backend: &ahv1.ArtifactLocation_NodeLocal{
				NodeLocal: &ahv1.NodeLocalLocation{
					NodeName: location.NodeLocal.NodeName,
					Path:     location.NodeLocal.Path,
				},
			},
		}
	case location.HTTP != nil:
		return &ahv1.ArtifactLocation{
			Backend: &ahv1.ArtifactLocation_Http{
				Http: &ahv1.HttpSource{
					Uri: location.HTTP.URI,
				},
			},
		}
	default:
		return nil
	}
}

func grpcCandidatesFromDomain(candidates []domain.MaterializationCandidate) []*ahv1.MaterializationCandidate {
	if len(candidates) == 0 {
		return nil
	}
	out := make([]*ahv1.MaterializationCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		item := &ahv1.MaterializationCandidate{
			Priority:          int32(candidate.Priority), //nolint:gosec
			Mode:              string(candidate.Mode),
			SourceRef:         candidate.SourceRef,
			ExpectedDigest:    candidate.ExpectedDigest,
			ExpectedSizeBytes: candidate.ExpectedSize,
			LocalPath:         candidate.LocalPath,
			SourceLocation:    grpcLocationFromDomain(candidate.SourceLocation),
			Uri:               candidate.URI,
		}
		if len(candidate.Conditions) != 0 {
			item.Conditions = make([]*ahv1.MaterializationCondition, 0, len(candidate.Conditions))
			for _, condition := range candidate.Conditions {
				item.Conditions = append(item.Conditions, &ahv1.MaterializationCondition{
					Kind:      condition.Kind,
					NodeName:  condition.NodeName,
					BackendId: condition.BackendID,
					SourceRef: condition.SourceRef,
					State:     condition.State,
				})
			}
		}
		out = append(out, item)
	}
	return out
}

func grpcSourceToDomain(source *ahv1.ArtifactSource) domain.ArtifactSource {
	if source == nil {
		return domain.ArtifactSource{}
	}
	return domain.ArtifactSource{
		SourceID:            source.GetSourceId(),
		ArtifactID:          source.GetArtifactId(),
		BackendID:           source.GetBackendId(),
		Digest:              source.GetDigest(),
		State:               domain.SourceState(source.GetState()),
		LocationFingerprint: source.GetLocationFingerprint(),
		Location:            grpcLocationToDomain(source.GetLocation()),
	}
}

func grpcSourceFromDomain(source domain.ArtifactSource) *ahv1.ArtifactSource {
	lastVerifiedAt := ""
	if !source.LastVerifiedAt.IsZero() {
		lastVerifiedAt = source.LastVerifiedAt.UTC().Format(time.RFC3339Nano)
	}
	return &ahv1.ArtifactSource{
		SourceId:            source.SourceID,
		ArtifactId:          source.ArtifactID,
		BackendId:           source.BackendID,
		Digest:              source.Digest,
		State:               string(source.State),
		LocationFingerprint: source.LocationFingerprint,
		Location:            grpcLocationFromDomain(&source.Location),
		LastVerifiedAt:      lastVerifiedAt,
		LastError:           source.LastError,
	}
}

func grpcSourcesFromDomain(sources []domain.ArtifactSource) []*ahv1.ArtifactSource {
	if len(sources) == 0 {
		return nil
	}
	out := make([]*ahv1.ArtifactSource, 0, len(sources))
	for _, source := range sources {
		out = append(out, grpcSourceFromDomain(source))
	}
	return out
}

func grpcLocationToDomain(location *ahv1.ArtifactLocation) domain.Location {
	if location == nil {
		return domain.Location{}
	}
	switch backend := location.GetBackend().(type) {
	case *ahv1.ArtifactLocation_NodeLocal:
		return domain.Location{
			NodeLocal: &domain.NodeLocalLocation{
				NodeName: backend.NodeLocal.GetNodeName(),
				Path:     backend.NodeLocal.GetPath(),
			},
		}
	case *ahv1.ArtifactLocation_Http:
		return domain.Location{
			HTTP: &domain.HTTPSource{
				URI: backend.Http.GetUri(),
			},
		}
	default:
		return domain.Location{}
	}
}
