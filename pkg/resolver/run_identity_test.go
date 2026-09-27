package resolver

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/HeaInSeo/artifact-handoff/api/proto/ahv1"
	"github.com/HeaInSeo/artifact-handoff/pkg/domain"
	"github.com/HeaInSeo/artifact-handoff/pkg/inventory"
)

// F4 Mode B / FD-DATA-01 R10: RunID is the canonical execution identity. Two Runs of
// the same Sample never share a key, a terminal partition or a GC scope; SampleRunID
// is grouping metadata only; a missing RunID fails closed. Each test below is one R10
// acceptance item, driven through the Service (and gRPC where the wire matters).

const r10Sample = "sample-S"

func r10Artifact(runID, digest string) domain.Artifact {
	return domain.Artifact{
		RunID:             runID,
		SampleRunID:       r10Sample,
		ProducerNodeID:    "producer-a",
		ProducerAttemptID: "attempt-1",
		OutputName:        "dataset",
		Digest:            digest,
		NodeName:          "node-a",
		URI:               "http://artifact.local/" + runID + "/dataset",
	}
}

func r10Binding(runID string) domain.Binding {
	return domain.Binding{
		BindingName:        "dataset-input",
		RunID:              runID,
		SampleRunID:        r10Sample,
		ProducerNodeID:     "producer-a",
		ProducerAttemptID:  "attempt-1",
		ProducerOutputName: "dataset",
		ConsumePolicy:      domain.ConsumePolicyRemoteOK,
		Required:           true,
	}
}

// R10-1: the same Sample's R1 and R2 have distinct identities — the same producer
// node/attempt/output registers twice (even with different digests) without conflict.
func TestR10_SameSampleRunsHaveDistinctIdentity(t *testing.T) {
	ctx := context.Background()
	store := inventory.NewMemoryStore()
	svc := newTestService(t, store)

	if _, err := svc.RegisterArtifact(ctx, r10Artifact("R1", "sha256:one")); err != nil {
		t.Fatalf("register R1: %v", err)
	}
	if _, err := svc.RegisterArtifact(ctx, r10Artifact("R2", "sha256:two")); err != nil {
		t.Fatalf("register R2 (same sample/node/attempt/output, different run): %v", err)
	}
	a1, ok1, _ := svc.GetArtifact(ctx, "R1", "producer-a", "attempt-1", "dataset")
	a2, ok2, _ := svc.GetArtifact(ctx, "R2", "producer-a", "attempt-1", "dataset")
	if !ok1 || !ok2 {
		t.Fatalf("both runs' artifacts must be retrievable: R1=%v R2=%v", ok1, ok2)
	}
	if a1.ArtifactID == a2.ArtifactID || a1.Digest == a2.Digest {
		t.Fatalf("runs share identity: R1=%s(%s) R2=%s(%s)", a1.ArtifactID, a1.Digest, a2.ArtifactID, a2.Digest)
	}
}

// R10-2: R1's GC never touches R2 — R1 becoming GC-eligible does not expire a binding
// of R2, and R2's lifecycle stays independent (this closes the cross-run GC bug where
// the lifecycle was keyed by SampleRunID).
func TestR10_R1GCDoesNotTouchR2(t *testing.T) {
	ctx := context.Background()
	store := inventory.NewMemoryStore()
	svc := newTestService(t, store)
	base := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return base }

	for _, run := range []string{"R1", "R2"} {
		if _, err := svc.RegisterArtifact(ctx, r10Artifact(run, "sha256:"+run)); err != nil {
			t.Fatalf("register %s: %v", run, err)
		}
		if err := svc.NotifyNodeTerminal(ctx, run, "producer-a", "attempt-1", "Succeeded"); err != nil {
			t.Fatalf("notify %s: %v", run, err)
		}
	}
	if err := svc.FinalizeRun(ctx, "R1", r10Sample); err != nil {
		t.Fatalf("finalize R1: %v", err)
	}
	svc.now = func() time.Time { return base.Add(time.Hour) }
	if err := svc.EvaluateRunGC(ctx, "R1"); err != nil {
		t.Fatalf("evaluate R1 GC: %v", err)
	}
	r1, _, _ := svc.GetRunLifecycle(ctx, "R1")
	if !r1.GCEligible {
		t.Fatalf("precondition: R1 should be GC eligible, got %+v", r1)
	}

	if got, _ := svc.ResolveHandoff(ctx, r10Binding("R1"), "node-b"); got.Status != domain.ResolutionStatusGCExpired {
		t.Fatalf("R1 binding = %s, want GC_EXPIRED", got.Status)
	}
	got, err := svc.ResolveHandoff(ctx, r10Binding("R2"), "node-b")
	if err != nil {
		t.Fatalf("resolve R2: %v", err)
	}
	if got.Status != domain.ResolutionStatusResolved {
		t.Fatalf("R2 binding = %s (%s), want RESOLVED: R1's GC leaked into R2", got.Status, got.Reason)
	}
	if lc, ok, _ := svc.GetRunLifecycle(ctx, "R2"); ok && lc.GCEligible {
		t.Fatalf("R2 lifecycle became GC eligible from R1's evaluation: %+v", lc)
	}
	if r1.RetainedArtifactCount != 1 || r1.TerminalNodeCount != 1 {
		t.Fatalf("R1 GC scope counted another run: %+v", r1)
	}
}

// R10-3: the presence of Sample metadata never changes the key.
func TestR10_SampleMetadataDoesNotChangeKey(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t, inventory.NewMemoryStore())

	with := r10Artifact("R1", "sha256:same")
	without := with
	without.SampleRunID = ""
	if with.CanonicalID() != without.CanonicalID() || r10Binding("R1").Key() != with.Key() {
		t.Fatalf("sample metadata changed the key: %s vs %s", with.CanonicalID(), without.CanonicalID())
	}
	if _, err := svc.RegisterArtifact(ctx, with); err != nil {
		t.Fatalf("register with sample: %v", err)
	}
	// Same Run, no sample metadata, same digest → the same artifact (idempotent).
	if _, err := svc.RegisterArtifact(ctx, without); err != nil {
		t.Fatalf("re-register without sample metadata: %v", err)
	}
	all, err := svc.ListArtifactsByRun(ctx, "R1")
	if err != nil || len(all) != 1 {
		t.Fatalf("run R1 artifacts = %d (err %v), want exactly 1", len(all), err)
	}
}

// R10-4: a missing RunID fails closed on every write and lookup — it is never derived
// from SampleRunID (no FirstNonEmpty fallback), at the Service and on the gRPC wire.
func TestR10_MissingRunIDFailsClosed(t *testing.T) {
	ctx := context.Background()
	store := inventory.NewMemoryStore()
	svc := newTestService(t, store)

	sampleOnly := r10Artifact("", "sha256:x")
	checks := map[string]error{}
	_, checks["register"] = svc.RegisterArtifact(ctx, sampleOnly)
	_, checks["resolve"] = svc.ResolveHandoff(ctx, r10Binding(""), "node-b")
	checks["notify"] = svc.NotifyNodeTerminal(ctx, "", "producer-a", "attempt-1", "Succeeded")
	checks["finalize"] = svc.FinalizeRun(ctx, "", r10Sample)
	checks["evaluateGC"] = svc.EvaluateRunGC(ctx, "")
	_, _, checks["lifecycle"] = svc.GetRunLifecycle(ctx, "")
	_, _, checks["get"] = svc.GetArtifact(ctx, "", "producer-a", "attempt-1", "dataset")
	for op, err := range checks {
		if !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("%s without runID: err = %v, want ErrInvalidArgument", op, err)
		}
	}
	if list, _ := svc.ListArtifactsBySampleRun(ctx, r10Sample); len(list) != 0 {
		t.Fatalf("a sample-only registration was stored: %+v", list)
	}

	// Wire: an old client that sends only sample_run_id is rejected, not reinterpreted.
	grpcSvc := &grpcResolverServer{service: svc}
	_, err := grpcSvc.RegisterArtifact(ctx, &ahv1.RegisterArtifactRequest{Artifact: &ahv1.ArtifactRef{
		SampleRunId: r10Sample, ProducerNodeId: "producer-a", ProducerAttemptId: "attempt-1", OutputName: "dataset",
	}})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("gRPC register with sample_run_id only: code = %v, want InvalidArgument", status.Code(err))
	}
	_, err = grpcSvc.FinalizeSampleRun(ctx, &ahv1.FinalizeSampleRunRequest{SampleRunId: r10Sample})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("gRPC finalize with sample_run_id only: code = %v, want InvalidArgument", status.Code(err))
	}
}

// R10-6: several Runs of one Sample can be listed, and each keeps its own identity.
func TestR10_SampleGroupingListsRunsWithoutMergingIdentity(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t, inventory.NewMemoryStore())
	for _, run := range []string{"R1", "R2"} {
		if _, err := svc.RegisterArtifact(ctx, r10Artifact(run, "sha256:"+run)); err != nil {
			t.Fatalf("register %s: %v", run, err)
		}
		if err := svc.FinalizeRun(ctx, run, r10Sample); err != nil {
			t.Fatalf("finalize %s: %v", run, err)
		}
	}
	runs, err := svc.ListRunsBySample(ctx, r10Sample)
	if err != nil || len(runs) != 2 || runs[0].RunID != "R1" || runs[1].RunID != "R2" {
		t.Fatalf("runs of sample = %+v (err %v), want R1 and R2 separately", runs, err)
	}
	for _, lc := range runs {
		if lc.SampleRunID != r10Sample || lc.RetainedArtifactCount != 1 {
			t.Fatalf("run %s lifecycle merged or lost metadata: %+v", lc.RunID, lc)
		}
	}
	arts, err := svc.ListArtifactsBySampleRun(ctx, r10Sample)
	if err != nil || len(arts) != 2 || arts[0].RunID == arts[1].RunID {
		t.Fatalf("sample grouping = %+v (err %v), want 2 artifacts with distinct runIDs", arts, err)
	}
}

// R10-7: concurrent registration and terminal notification across Runs of one Sample
// never collide on a key.
func TestR10_ConcurrentRunsDoNotCollide(t *testing.T) {
	ctx := context.Background()
	store := inventory.NewMemoryStore()
	svc := newTestService(t, store)
	const runs = 24
	var wg sync.WaitGroup
	errs := make(chan error, runs*2)
	for i := 0; i < runs; i++ {
		run := fmt.Sprintf("R%02d", i)
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := svc.RegisterArtifact(ctx, r10Artifact(run, "sha256:"+run)); err != nil {
				errs <- fmt.Errorf("register %s: %w", run, err)
			}
			if err := svc.NotifyNodeTerminal(ctx, run, "producer-a", "attempt-1", "Succeeded"); err != nil {
				errs <- fmt.Errorf("notify %s: %w", run, err)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	for i := 0; i < runs; i++ {
		run := fmt.Sprintf("R%02d", i)
		a, ok, _ := svc.GetArtifact(ctx, run, "producer-a", "attempt-1", "dataset")
		if !ok || a.Digest != "sha256:"+run {
			t.Fatalf("run %s artifact = %+v (ok=%v): cross-run collision", run, a, ok)
		}
		terms, _ := store.ListNodeTerminalsByRun(ctx, run)
		if len(terms) != 1 {
			t.Fatalf("run %s terminal partition = %d records, want 1", run, len(terms))
		}
	}
}
