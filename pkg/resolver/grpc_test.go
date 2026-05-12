package resolver

import (
	"context"
	"net"
	"testing"

	ahv1 "github.com/HeaInSeo/artifact-handoff/api/proto/ahv1"
	"github.com/HeaInSeo/artifact-handoff/pkg/domain"
	"github.com/HeaInSeo/artifact-handoff/pkg/inventory"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

func TestGRPCRegisterResolveAndLifecycle(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := newTestService(t, store)

	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	RegisterGRPCService(server, service)
	go func() {
		_ = server.Serve(listener)
	}()
	defer server.Stop()

	ctx := context.Background()
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient() error = %v", err)
	}
	defer func() { _ = conn.Close() }()
	client := ahv1.NewArtifactHandoffResolverClient(conn)

	registerResp, err := client.RegisterArtifact(ctx, &ahv1.RegisterArtifactRequest{
		Artifact: &ahv1.ArtifactRef{
			SampleRunId:       "sample-1",
			ProducerNodeId:    "parent-a",
			ProducerAttemptId: "attempt-1",
			OutputName:        "output",
			NodeName:          "node-a",
			Uri:               "http://artifact.local/output",
			SizeBytes:         4096,
		},
	})
	if err != nil {
		t.Fatalf("RegisterArtifact() error = %v", err)
	}
	if registerResp.GetAvailabilityState() != string(domain.AvailabilityStateBoth) {
		t.Fatalf("availabilityState = %q, want %q", registerResp.GetAvailabilityState(), domain.AvailabilityStateBoth)
	}

	resolveResp, err := client.ResolveHandoff(ctx, &ahv1.ResolveHandoffRequest{
		Binding: &ahv1.ArtifactBinding{
			BindingName:        "dataset",
			SampleRunId:        "sample-1",
			ProducerNodeId:     "parent-a",
			ProducerAttemptId:  "attempt-1",
			ProducerOutputName: "output",
			ConsumePolicy:      string(domain.ConsumePolicyRemoteOK),
			Required:           true,
		},
		TargetNodeName: "node-b",
	})
	if err != nil {
		t.Fatalf("ResolveHandoff() error = %v", err)
	}
	if resolveResp.GetDecision() != string(domain.ResolutionDecisionRemoteFetch) {
		t.Fatalf("decision = %q, want %q", resolveResp.GetDecision(), domain.ResolutionDecisionRemoteFetch)
	}
	if resolveResp.GetMaterializationPlan().GetMode() != string(domain.MaterializationModeRemoteFetch) {
		t.Fatalf("materialization mode = %q, want remote_fetch", resolveResp.GetMaterializationPlan().GetMode())
	}

	if _, err := client.NotifyNodeTerminal(ctx, &ahv1.NotifyNodeTerminalRequest{
		SampleRunId:   "sample-1",
		NodeId:        "parent-a",
		AttemptId:     "attempt-1",
		TerminalState: "Succeeded",
	}); err != nil {
		t.Fatalf("NotifyNodeTerminal() error = %v", err)
	}

	if _, err := client.FinalizeSampleRun(ctx, &ahv1.FinalizeSampleRunRequest{
		SampleRunId: "sample-1",
	}); err != nil {
		t.Fatalf("FinalizeSampleRun() error = %v", err)
	}

	lifecycleResp, err := client.GetSampleRunLifecycle(ctx, &ahv1.GetSampleRunLifecycleRequest{
		SampleRunId: "sample-1",
	})
	if err != nil {
		t.Fatalf("GetSampleRunLifecycle() error = %v", err)
	}
	if !lifecycleResp.GetFinalized() {
		t.Fatal("lifecycle finalized = false, want true")
	}
	if lifecycleResp.GetRetainedArtifactBytes() != 4096 {
		t.Fatalf("retainedArtifactBytes = %d, want 4096", lifecycleResp.GetRetainedArtifactBytes())
	}
}
