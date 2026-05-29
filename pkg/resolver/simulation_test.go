package resolver

import (
	"context"
	"testing"
	"time"

	"github.com/HeaInSeo/artifact-handoff/pkg/domain"
	"github.com/HeaInSeo/artifact-handoff/pkg/inventory"
)

// Simulation tests exercise the full AH decision flow (A→B→C) without
// any Kubernetes or real executor involvement. Each test uses explicit
// attemptIDs and verifies PlacementIntent + MaterializationPlan.

const (
	simRun      = "sim-run-001"
	simAttempt  = "attempt-1"
	nodeWorker1 = "node-worker-1"
	nodeWorker2 = "node-worker-2"
	nodeWorker3 = "node-worker-3"
)

// registerArtifact is a helper that registers and fatals on error.
func registerArtifact(t *testing.T, svc *Service, a domain.Artifact) {
	t.Helper()
	if _, err := svc.RegisterArtifact(context.Background(), a); err != nil {
		t.Fatalf("RegisterArtifact(%s/%s/%s): %v", a.ProducerNodeID, a.ProducerAttemptID, a.OutputName, err)
	}
}

// notifyTerminal records a node completion and fatals on error.
func notifyTerminal(t *testing.T, svc *Service, sampleRunID, nodeID, attemptID, state string) {
	t.Helper()
	if err := svc.NotifyNodeTerminal(context.Background(), sampleRunID, nodeID, attemptID, state); err != nil {
		t.Fatalf("NotifyNodeTerminal(%s/%s): %v", nodeID, attemptID, err)
	}
}

// resolve calls ResolveHandoff and fatals on unexpected error.
func resolve(t *testing.T, svc *Service, b domain.Binding, targetNode string) domain.ResolvedHandoff {
	t.Helper()
	r, err := svc.ResolveHandoff(context.Background(), b, targetNode)
	if err != nil {
		t.Fatalf("ResolveHandoff binding=%s: %v", b.BindingName, err)
	}
	return r
}

// TestSimulateLinearABC_LocalReuse verifies that when A and B run on
// the same node, each downstream consumer gets a local_reuse decision
// with a preferred_node placement intent.
func TestSimulateLinearABC_LocalReuse(t *testing.T) {
	svc := newTestService(t, inventory.NewMemoryStore())
	ctx := context.Background()

	// A produces output-a on worker-1.
	registerArtifact(t, svc, domain.Artifact{
		SampleRunID:       simRun,
		ProducerNodeID:    "node-A",
		ProducerAttemptID: simAttempt,
		OutputName:        "output-a",
		NodeName:          nodeWorker1,
		URI:               "http://artifact-source.local/" + simRun + "/A/output-a",
		Digest:            "sha256:aaa",
	})
	notifyTerminal(t, svc, simRun, "node-A", simAttempt, "Succeeded")

	// B resolves A's output while running on the same node.
	bBinding := domain.Binding{
		BindingName:        "B-input-a",
		SampleRunID:        simRun,
		ChildNodeID:        "node-B",
		ChildAttemptID:     simAttempt,
		ProducerNodeID:     "node-A",
		ProducerAttemptID:  simAttempt,
		ProducerOutputName: "output-a",
		ConsumePolicy:      domain.ConsumePolicySameNodeThenRemote,
	}
	bResolved := resolve(t, svc, bBinding, nodeWorker1)

	if bResolved.Status != domain.ResolutionStatusResolved {
		t.Fatalf("B status = %s, want RESOLVED", bResolved.Status)
	}
	if bResolved.Decision != domain.ResolutionDecisionLocalReuse {
		t.Fatalf("B decision = %s, want local_reuse", bResolved.Decision)
	}
	if bResolved.PlacementIntent.Mode != domain.PlacementIntentModePreferredNode {
		t.Fatalf("B PlacementIntent.Mode = %s, want preferred_node", bResolved.PlacementIntent.Mode)
	}
	if bResolved.PlacementIntent.NodeName != nodeWorker1 {
		t.Fatalf("B PlacementIntent.NodeName = %s, want %s", bResolved.PlacementIntent.NodeName, nodeWorker1)
	}
	if bResolved.MaterializationPlan.Mode != domain.MaterializationModeLocalReuse {
		t.Fatalf("B MaterializationPlan.Mode = %s, want local_reuse", bResolved.MaterializationPlan.Mode)
	}
	if bResolved.MaterializationPlan.URI == "" {
		t.Fatal("B MaterializationPlan.URI must not be empty")
	}
	if bResolved.Retryable {
		t.Fatal("B local_reuse must not be retryable")
	}

	// B produces output-b on the same node.
	registerArtifact(t, svc, domain.Artifact{
		SampleRunID:       simRun,
		ProducerNodeID:    "node-B",
		ProducerAttemptID: simAttempt,
		OutputName:        "output-b",
		NodeName:          nodeWorker1,
		URI:               "node-local://" + nodeWorker1 + "/" + simRun + "/B/output-b",
		Digest:            "sha256:bbb",
	})
	notifyTerminal(t, svc, simRun, "node-B", simAttempt, "Succeeded")

	// C resolves B's output on the same node.
	cBinding := domain.Binding{
		BindingName:        "C-input-b",
		SampleRunID:        simRun,
		ChildNodeID:        "node-C",
		ChildAttemptID:     simAttempt,
		ProducerNodeID:     "node-B",
		ProducerAttemptID:  simAttempt,
		ProducerOutputName: "output-b",
		ConsumePolicy:      domain.ConsumePolicySameNodeThenRemote,
	}
	cResolved := resolve(t, svc, cBinding, nodeWorker1)

	if cResolved.Decision != domain.ResolutionDecisionLocalReuse {
		t.Fatalf("C decision = %s, want local_reuse", cResolved.Decision)
	}
	if cResolved.MaterializationPlan.Mode != domain.MaterializationModeLocalReuse {
		t.Fatalf("C MaterializationPlan.Mode = %s, want local_reuse", cResolved.MaterializationPlan.Mode)
	}

	_ = ctx
}

// TestSimulateLinearABC_RemoteFetch verifies that when consumers land on
// different nodes the decision is remote_fetch with no placement intent.
func TestSimulateLinearABC_RemoteFetch(t *testing.T) {
	svc := newTestService(t, inventory.NewMemoryStore())

	registerArtifact(t, svc, domain.Artifact{
		SampleRunID:       simRun,
		ProducerNodeID:    "node-A",
		ProducerAttemptID: simAttempt,
		OutputName:        "output-a",
		NodeName:          nodeWorker1,
		URI:               "http://artifact-source.local/" + simRun + "/A/output-a",
		Digest:            "sha256:aaa",
	})
	notifyTerminal(t, svc, simRun, "node-A", simAttempt, "Succeeded")

	bResolved := resolve(t, svc, domain.Binding{
		BindingName:        "B-input-a",
		SampleRunID:        simRun,
		ChildNodeID:        "node-B",
		ChildAttemptID:     simAttempt,
		ProducerNodeID:     "node-A",
		ProducerAttemptID:  simAttempt,
		ProducerOutputName: "output-a",
		ConsumePolicy:      domain.ConsumePolicyRemoteOK,
	}, nodeWorker2)

	if bResolved.Decision != domain.ResolutionDecisionRemoteFetch {
		t.Fatalf("B decision = %s, want remote_fetch", bResolved.Decision)
	}
	if bResolved.PlacementIntent.Mode != domain.PlacementIntentModeNone {
		t.Fatalf("B PlacementIntent.Mode = %s, want none", bResolved.PlacementIntent.Mode)
	}
	if bResolved.MaterializationPlan.Mode != domain.MaterializationModeRemoteFetch {
		t.Fatalf("B MaterializationPlan.Mode = %s, want remote_fetch", bResolved.MaterializationPlan.Mode)
	}
	if bResolved.MaterializationPlan.URI == "" {
		t.Fatal("B MaterializationPlan.URI must not be empty for remote_fetch")
	}

	registerArtifact(t, svc, domain.Artifact{
		SampleRunID:       simRun,
		ProducerNodeID:    "node-B",
		ProducerAttemptID: simAttempt,
		OutputName:        "output-b",
		NodeName:          nodeWorker2,
		URI:               "http://artifact-source.local/" + simRun + "/B/output-b",
		Digest:            "sha256:bbb",
	})
	notifyTerminal(t, svc, simRun, "node-B", simAttempt, "Succeeded")

	cResolved := resolve(t, svc, domain.Binding{
		BindingName:        "C-input-b",
		SampleRunID:        simRun,
		ChildNodeID:        "node-C",
		ChildAttemptID:     simAttempt,
		ProducerNodeID:     "node-B",
		ProducerAttemptID:  simAttempt,
		ProducerOutputName: "output-b",
		ConsumePolicy:      domain.ConsumePolicyRemoteOK,
	}, nodeWorker3)

	if cResolved.Decision != domain.ResolutionDecisionRemoteFetch {
		t.Fatalf("C decision = %s, want remote_fetch", cResolved.Decision)
	}
	if cResolved.MaterializationPlan.URI == "" {
		t.Fatal("C MaterializationPlan.URI must not be empty")
	}
}

// TestSimulateProducerPending verifies that resolving before the producer
// is terminal returns PENDING / retryable, then succeeds after the producer
// completes and registers its artifact.
func TestSimulateProducerPending(t *testing.T) {
	svc := newTestService(t, inventory.NewMemoryStore())

	// B tries to resolve before A has finished.
	pendingResult := resolve(t, svc, domain.Binding{
		BindingName:        "B-input-a",
		SampleRunID:        simRun,
		ChildNodeID:        "node-B",
		ChildAttemptID:     simAttempt,
		ProducerNodeID:     "node-A",
		ProducerAttemptID:  simAttempt,
		ProducerOutputName: "output-a",
		ConsumePolicy:      domain.ConsumePolicyRemoteOK,
	}, nodeWorker2)

	if pendingResult.Status != domain.ResolutionStatusPending {
		t.Fatalf("status = %s, want PENDING", pendingResult.Status)
	}
	if !pendingResult.Retryable {
		t.Fatal("pending result must be retryable")
	}

	// A completes and registers its artifact.
	notifyTerminal(t, svc, simRun, "node-A", simAttempt, "Succeeded")
	registerArtifact(t, svc, domain.Artifact{
		SampleRunID:       simRun,
		ProducerNodeID:    "node-A",
		ProducerAttemptID: simAttempt,
		OutputName:        "output-a",
		NodeName:          nodeWorker1,
		URI:               "http://artifact-source.local/" + simRun + "/A/output-a",
		Digest:            "sha256:aaa",
	})

	// B retries and now gets RESOLVED.
	retryResult := resolve(t, svc, domain.Binding{
		BindingName:        "B-input-a",
		SampleRunID:        simRun,
		ChildNodeID:        "node-B",
		ChildAttemptID:     simAttempt,
		ProducerNodeID:     "node-A",
		ProducerAttemptID:  simAttempt,
		ProducerOutputName: "output-a",
		ConsumePolicy:      domain.ConsumePolicyRemoteOK,
	}, nodeWorker2)

	if retryResult.Status != domain.ResolutionStatusResolved {
		t.Fatalf("retry status = %s, want RESOLVED", retryResult.Status)
	}
	if retryResult.Retryable {
		t.Fatal("resolved result must not be retryable")
	}
}

// TestSimulateProducerFailed verifies that a Failed producer terminal
// results in producer_failed decision, not retryable.
func TestSimulateProducerFailed(t *testing.T) {
	svc := newTestService(t, inventory.NewMemoryStore())

	notifyTerminal(t, svc, simRun, "node-A", simAttempt, "Failed")

	result := resolve(t, svc, domain.Binding{
		BindingName:        "B-input-a",
		SampleRunID:        simRun,
		ChildNodeID:        "node-B",
		ChildAttemptID:     simAttempt,
		ProducerNodeID:     "node-A",
		ProducerAttemptID:  simAttempt,
		ProducerOutputName: "output-a",
		ConsumePolicy:      domain.ConsumePolicyRemoteOK,
	}, nodeWorker2)

	if result.Status != domain.ResolutionStatusProducerFailed {
		t.Fatalf("status = %s, want PRODUCER_FAILED", result.Status)
	}
	if result.Decision != domain.ResolutionDecisionProducerFailed {
		t.Fatalf("decision = %s, want producer_failed", result.Decision)
	}
	if result.Retryable {
		t.Fatal("producer_failed must not be retryable")
	}
}

// TestSimulateDigestMismatch verifies that a binding with the wrong
// expected digest returns an error.
func TestSimulateDigestMismatch(t *testing.T) {
	svc := newTestService(t, inventory.NewMemoryStore())

	registerArtifact(t, svc, domain.Artifact{
		SampleRunID:       simRun,
		ProducerNodeID:    "node-A",
		ProducerAttemptID: simAttempt,
		OutputName:        "output-a",
		NodeName:          nodeWorker1,
		URI:               "node-local://" + nodeWorker1 + "/A/output-a",
		Digest:            "sha256:real",
	})
	notifyTerminal(t, svc, simRun, "node-A", simAttempt, "Succeeded")

	result := resolve(t, svc, domain.Binding{
		BindingName:        "B-input-a",
		SampleRunID:        simRun,
		ChildNodeID:        "node-B",
		ChildAttemptID:     simAttempt,
		ProducerNodeID:     "node-A",
		ProducerAttemptID:  simAttempt,
		ProducerOutputName: "output-a",
		ConsumePolicy:      domain.ConsumePolicyRemoteOK,
		ExpectedDigest:     "sha256:wrong",
	}, nodeWorker2)
	if result.Status != domain.ResolutionStatusDigestMismatch {
		t.Fatalf("status = %s, want DIGEST_MISMATCH", result.Status)
	}
	if result.Retryable {
		t.Fatal("digest mismatch must not be retryable")
	}
}

// TestSimulateSameNodeOnlyViolation verifies that SameNodeOnly policy
// when the consumer lands on a different node returns unavailable with
// a required_node placement hint pointing at the artifact's node.
func TestSimulateSameNodeOnlyViolation(t *testing.T) {
	svc := newTestService(t, inventory.NewMemoryStore())

	registerArtifact(t, svc, domain.Artifact{
		SampleRunID:       simRun,
		ProducerNodeID:    "node-A",
		ProducerAttemptID: simAttempt,
		OutputName:        "output-a",
		NodeName:          nodeWorker1,
		URI:               "node-local://" + nodeWorker1 + "/A/output-a",
	})
	notifyTerminal(t, svc, simRun, "node-A", simAttempt, "Succeeded")

	result := resolve(t, svc, domain.Binding{
		BindingName:        "B-input-a",
		SampleRunID:        simRun,
		ChildNodeID:        "node-B",
		ChildAttemptID:     simAttempt,
		ProducerNodeID:     "node-A",
		ProducerAttemptID:  simAttempt,
		ProducerOutputName: "output-a",
		ConsumePolicy:      domain.ConsumePolicySameNodeOnly,
	}, nodeWorker2) // different node

	if result.Status != domain.ResolutionStatusPolicyBlocked {
		t.Fatalf("status = %s, want POLICY_BLOCKED", result.Status)
	}
	if result.Decision != domain.ResolutionDecisionUnavailable {
		t.Fatalf("decision = %s, want unavailable", result.Decision)
	}
	if result.PlacementIntent.Mode != domain.PlacementIntentModeRequiredNode {
		t.Fatalf("PlacementIntent.Mode = %s, want required_node", result.PlacementIntent.Mode)
	}
	if result.PlacementIntent.NodeName != nodeWorker1 {
		t.Fatalf("PlacementIntent.NodeName = %s, want %s", result.PlacementIntent.NodeName, nodeWorker1)
	}
}

// TestSimulateGCExpiredRun verifies that after a run becomes GC-eligible
// any further resolve returns MISSING / unavailable and is not retryable.
func TestSimulateGCExpiredRun(t *testing.T) {
	store := inventory.NewMemoryStore()
	svc := newTestService(t, store)

	baseNow := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return baseNow }

	registerArtifact(t, svc, domain.Artifact{
		SampleRunID:       simRun,
		ProducerNodeID:    "node-A",
		ProducerAttemptID: simAttempt,
		OutputName:        "output-a",
		NodeName:          nodeWorker1,
		URI:               "node-local://" + nodeWorker1 + "/A/output-a",
		SizeBytes:         1024,
	})
	notifyTerminal(t, svc, simRun, "node-A", simAttempt, "Succeeded")

	if err := svc.FinalizeSampleRun(context.Background(), simRun); err != nil {
		t.Fatalf("FinalizeSampleRun: %v", err)
	}

	// Advance past retention window (15 min) and mark GC eligible.
	svc.now = func() time.Time { return baseNow.Add(20 * time.Minute) }
	if err := svc.EvaluateGC(context.Background(), simRun); err != nil {
		t.Fatalf("EvaluateGC: %v", err)
	}

	// C tries to resolve after the run is GC eligible.
	result := resolve(t, svc, domain.Binding{
		BindingName:        "C-input-a",
		SampleRunID:        simRun,
		ChildNodeID:        "node-C",
		ChildAttemptID:     simAttempt,
		ProducerNodeID:     "node-A",
		ProducerAttemptID:  simAttempt,
		ProducerOutputName: "output-a",
		ConsumePolicy:      domain.ConsumePolicyRemoteOK,
	}, nodeWorker2)

	if result.Status != domain.ResolutionStatusGCExpired {
		t.Fatalf("status = %s, want GC_EXPIRED", result.Status)
	}
	if result.Decision != domain.ResolutionDecisionUnavailable {
		t.Fatalf("decision = %s, want unavailable", result.Decision)
	}
	if result.Retryable {
		t.Fatal("GC-expired run must not be retryable")
	}
}
