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
			Digest:            "sha256:grpc-output",
			Uri:               "http://artifact.local/output",
			LogicalUri:        "jumi://runs/sample-1/nodes/parent-a/outputs/output",
			Locations: []*ahv1.ArtifactLocation{{
				Backend: &ahv1.ArtifactLocation_NodeLocal{
					NodeLocal: &ahv1.NodeLocalLocation{
						NodeName: "node-a",
						Path:     "/var/lib/jumi-artifacts/cas/sha256/grpc-output",
					},
				},
			}},
			SizeBytes: 4096,
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
	if resolveResp.GetMaterializationPlan().GetExpectedSizeBytes() != 4096 {
		t.Fatalf("expectedSizeBytes = %d, want 4096", resolveResp.GetMaterializationPlan().GetExpectedSizeBytes())
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

	addResp, err := client.AddSource(ctx, &ahv1.AddSourceRequest{
		ArtifactId: "sample-1/parent-a/attempt-1/output",
		Source: &ahv1.ArtifactSource{
			BackendId: "legacy-http",
			Location: &ahv1.ArtifactLocation{
				Backend: &ahv1.ArtifactLocation_Http{
					Http: &ahv1.HttpSource{Uri: "http://artifact-source.local/backup"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("AddSource() error = %v", err)
	}
	if addResp.GetSource().GetSourceId() == "" {
		t.Fatalf("AddSource() sourceId = empty")
	}

	listResp, err := client.ListSources(ctx, &ahv1.ListSourcesRequest{
		ArtifactId: "sample-1/parent-a/attempt-1/output",
	})
	if err != nil {
		t.Fatalf("ListSources() error = %v", err)
	}
	if len(listResp.GetSources()) < 2 {
		t.Fatalf("len(ListSources().sources) = %d, want >= 2", len(listResp.GetSources()))
	}

	updateResp, err := client.UpdateSourceState(ctx, &ahv1.UpdateSourceStateRequest{
		SourceId: addResp.GetSource().GetSourceId(),
		State:    string(domain.SourceStateDeleted),
	})
	if err != nil {
		t.Fatalf("UpdateSourceState() error = %v", err)
	}
	if updateResp.GetSource().GetState() != string(domain.SourceStateDeleted) {
		t.Fatalf("updated source state = %q, want deleted", updateResp.GetSource().GetState())
	}
}
