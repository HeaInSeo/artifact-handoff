package resolver

import (
	"context"
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

func (s *grpcResolverServer) RegisterArtifact(ctx context.Context, req *ahv1.RegisterArtifactRequest) (*ahv1.RegisterArtifactResponse, error) {
	artifact := req.GetArtifact()
	state, err := s.service.RegisterArtifactCore(ctx, domain.Artifact{
		SampleRunID:    artifact.GetSampleRunId(),
		ProducerNodeID: artifact.GetProducerNodeId(),
		OutputName:     artifact.GetOutputName(),
		ArtifactID:     artifact.GetArtifactId(),
		Digest:         artifact.GetDigest(),
		NodeName:       artifact.GetNodeName(),
		URI:            artifact.GetUri(),
		SizeBytes:      artifact.GetSizeBytes(),
	})
	if err != nil {
		return nil, err
	}
	return &ahv1.RegisterArtifactResponse{AvailabilityState: string(state)}, nil
}

func (s *grpcResolverServer) ResolveHandoff(ctx context.Context, req *ahv1.ResolveHandoffRequest) (*ahv1.ResolveHandoffResponse, error) {
	binding := req.GetBinding()
	resolved, err := s.service.ResolveHandoffCore(ctx, domain.Binding{
		BindingName:        binding.GetBindingName(),
		SampleRunID:        binding.GetSampleRunId(),
		ChildNodeID:        binding.GetChildNodeId(),
		ChildInputName:     binding.GetChildInputName(),
		ProducerNodeID:     binding.GetProducerNodeId(),
		ProducerOutputName: binding.GetProducerOutputName(),
		ArtifactID:         binding.GetArtifactId(),
		ConsumePolicy:      domain.ConsumePolicy(binding.GetConsumePolicy()),
		ExpectedDigest:     binding.GetExpectedDigest(),
		Required:           binding.GetRequired(),
	}, req.GetTargetNodeName())
	if err != nil {
		return nil, err
	}
	return &ahv1.ResolveHandoffResponse{
		ResolutionStatus:        string(resolved.Status),
		Decision:                string(resolved.Decision),
		SourceNodeName:          resolved.SourceNodeName,
		ArtifactUri:             resolved.ArtifactURI,
		RequiresMaterialization: resolved.RequiresMaterialization,
	}, nil
}

func (s *grpcResolverServer) NotifyNodeTerminal(ctx context.Context, req *ahv1.NotifyNodeTerminalRequest) (*ahv1.NotifyNodeTerminalResponse, error) {
	if err := s.service.NotifyNodeTerminalCore(ctx, req.GetSampleRunId(), req.GetNodeId(), req.GetTerminalState()); err != nil {
		return nil, err
	}
	return &ahv1.NotifyNodeTerminalResponse{Accepted: true}, nil
}

func (s *grpcResolverServer) FinalizeSampleRun(ctx context.Context, req *ahv1.FinalizeSampleRunRequest) (*ahv1.FinalizeSampleRunResponse, error) {
	if err := s.service.FinalizeSampleRunCore(ctx, req.GetSampleRunId()); err != nil {
		return nil, err
	}
	return &ahv1.FinalizeSampleRunResponse{Accepted: true}, nil
}

func (s *grpcResolverServer) EvaluateGC(ctx context.Context, req *ahv1.EvaluateGCRequest) (*ahv1.EvaluateGCResponse, error) {
	if err := s.service.EvaluateGCCore(ctx, req.GetSampleRunId()); err != nil {
		return nil, err
	}
	return &ahv1.EvaluateGCResponse{Accepted: true}, nil
}

func (s *grpcResolverServer) GetSampleRunLifecycle(ctx context.Context, req *ahv1.GetSampleRunLifecycleRequest) (*ahv1.GetSampleRunLifecycleResponse, error) {
	lifecycle, ok, err := s.service.GetSampleRunLifecycleCore(ctx, req.GetSampleRunId())
	if err != nil {
		return nil, err
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
		TerminalNodeCount:     int32(lifecycle.TerminalNodeCount),
		SucceededNodeCount:    int32(lifecycle.SucceededNodeCount),
		FailedNodeCount:       int32(lifecycle.FailedNodeCount),
		CanceledNodeCount:     int32(lifecycle.CanceledNodeCount),
		RetainedArtifactCount: int32(lifecycle.RetainedArtifactCount),
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
