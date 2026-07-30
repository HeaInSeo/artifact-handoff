package resolver

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/HeaInSeo/artifact-handoff/pkg/domain"
	"github.com/HeaInSeo/artifact-handoff/pkg/inventory"
)

// errInjectedStoreFailure is returned by faultInjectingStore for whichever
// method(s) it's configured to fail.
var errInjectedStoreFailure = errors.New("injected store failure")

// faultInjectingStore wraps a real inventory.Store and lets tests force
// specific methods to fail, to exercise DB-write/read-failure error
// propagation that a real Store implementation would otherwise only hit
// under actual disk/network faults.
type faultInjectingStore struct {
	inventory.Store
	failPutArtifact           bool
	failUpsertLifecycle       bool
	failGetSampleRunLifecycle bool
}

func (f *faultInjectingStore) PutArtifact(ctx context.Context, artifact domain.Artifact) error {
	if f.failPutArtifact {
		return errInjectedStoreFailure
	}
	return f.Store.PutArtifact(ctx, artifact)
}

func (f *faultInjectingStore) UpsertSampleRunLifecycle(ctx context.Context, lifecycle domain.SampleRunLifecycle) error {
	if f.failUpsertLifecycle {
		return errInjectedStoreFailure
	}
	return f.Store.UpsertSampleRunLifecycle(ctx, lifecycle)
}

func (f *faultInjectingStore) GetSampleRunLifecycle(ctx context.Context, sampleRunID string) (domain.SampleRunLifecycle, bool, error) {
	if f.failGetSampleRunLifecycle {
		return domain.SampleRunLifecycle{}, false, errInjectedStoreFailure
	}
	return f.Store.GetSampleRunLifecycle(ctx, sampleRunID)
}

func TestRegisterArtifact_PropagatesStoreWriteFailure(t *testing.T) {
	store := &faultInjectingStore{Store: inventory.NewMemoryStore(), failPutArtifact: true}
	service := newTestService(t, store)

	_, err := service.RegisterArtifact(context.Background(), domain.Artifact{
		SampleRunID:       "sample-1",
		ProducerNodeID:    "parent-a",
		ProducerAttemptID: "attempt-1",
		OutputName:        "dataset",
	})
	if !errors.Is(err, errInjectedStoreFailure) {
		t.Fatalf("RegisterArtifact() error = %v, want errInjectedStoreFailure", err)
	}

	// The failure must not leave a phantom artifact visible through the
	// underlying store once writes work again.
	store.failPutArtifact = false
	_, found, getErr := store.Store.GetArtifact(context.Background(), "sample-1", "parent-a", "attempt-1", "dataset")
	if getErr != nil {
		t.Fatalf("GetArtifact() error = %v", getErr)
	}
	if found {
		t.Fatal("artifact should not exist after a failed PutArtifact")
	}
}

func TestEvaluateGC_PropagatesStoreWriteFailure(t *testing.T) {
	store := &faultInjectingStore{Store: inventory.NewMemoryStore()}
	service := newTestService(t, store)

	if _, err := service.RegisterArtifact(context.Background(), domain.Artifact{
		SampleRunID:       "sample-2",
		ProducerNodeID:    "parent-a",
		ProducerAttemptID: "attempt-1",
		OutputName:        "dataset",
		SizeBytes:         1024,
	}); err != nil {
		t.Fatalf("register artifact: %v", err)
	}
	if err := service.NotifyNodeTerminal(context.Background(), "sample-2", "parent-a", "attempt-1", "Succeeded"); err != nil {
		t.Fatalf("notify terminal: %v", err)
	}
	if err := service.FinalizeSampleRun(context.Background(), "sample-2"); err != nil {
		t.Fatalf("finalize sample run: %v", err)
	}

	// FinalizeSampleRun already wrote a lifecycle record (Finalized=true,
	// GCEligible=false, GCBlockedReason="gc_not_evaluated"). Capture it so we
	// can assert the failed EvaluateGC below leaves it untouched rather than
	// silently applying a partial update.
	before, ok, err := store.Store.GetSampleRunLifecycle(context.Background(), "sample-2")
	if err != nil || !ok {
		t.Fatalf("get lifecycle before failed GC: ok=%v err=%v", ok, err)
	}

	store.failUpsertLifecycle = true
	if err := service.EvaluateGC(context.Background(), "sample-2"); !errors.Is(err, errInjectedStoreFailure) {
		t.Fatalf("EvaluateGC() error = %v, want errInjectedStoreFailure", err)
	}

	// A failed Upsert must not leave a corrupted/partial lifecycle behind -
	// the pre-existing record should be byte-for-byte unchanged.
	store.failUpsertLifecycle = false
	after, found, err := store.Store.GetSampleRunLifecycle(context.Background(), "sample-2")
	if err != nil {
		t.Fatalf("GetSampleRunLifecycle() error = %v", err)
	}
	if !found {
		t.Fatal("lifecycle written by FinalizeSampleRun disappeared after a failed EvaluateGC upsert")
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("lifecycle changed despite failed upsert:\nbefore=%+v\nafter=%+v", before, after)
	}
}

func TestResolveHandoff_PropagatesStoreReadFailure(t *testing.T) {
	store := &faultInjectingStore{Store: inventory.NewMemoryStore(), failGetSampleRunLifecycle: true}
	service := newTestService(t, store)

	_, err := service.ResolveHandoff(context.Background(), domain.Binding{
		SampleRunID:        "sample-3",
		ProducerNodeID:     "parent-a",
		ProducerAttemptID:  "attempt-1",
		ProducerOutputName: "dataset",
		ConsumePolicy:      domain.ConsumePolicyRemoteOK,
	}, "consumer-node")
	if !errors.Is(err, errInjectedStoreFailure) {
		t.Fatalf("ResolveHandoff() error = %v, want errInjectedStoreFailure", err)
	}
}

// TestConcurrentGCAndResolve_RaceSafeAndConsistent fires EvaluateGC and
// ResolveHandoff concurrently against the same sampleRunID, past the point
// where the sample run is GC-eligible. This is the TOCTOU window flagged in
// https://github.com/HeaInSeo/artifact-handoff/issues/12: ResolveHandoffCore
// reads GCEligible once and acts on it, with no lock held across the
// read-then-decide sequence, so a concurrent EvaluateGC can flip eligibility
// in between.
//
// Run with -race, this proves the storage layer itself has no data race
// under concurrent access (MemoryStore is mutex-protected per-call). It does
// NOT prove the TOCTOU window is closed - it isn't, by design, since there's
// no cross-call locking in Service. What it asserts is the weaker but
// load-bearing invariant: every concurrent ResolveHandoff call returns
// either a normal RESOLVED result or a GC_EXPIRED result, never a panic,
// never any other error, and never a torn/inconsistent read.
func TestConcurrentGCAndResolve_RaceSafeAndConsistent(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := newTestService(t, store)
	baseNow := time.Date(2026, 4, 21, 10, 0, 0, 0, time.UTC)
	var nowMu sync.RWMutex
	service.now = func() time.Time {
		nowMu.RLock()
		defer nowMu.RUnlock()
		return baseNow
	}

	// The resolve goroutine below calls ResolveHandoff with a non-empty
	// targetNodeName (post-scheduling mode) from "consumer-node", which is
	// not the producer's node - so an HTTP source is required for RemoteOK
	// to have a remote-fetch candidate available; a NodeLocal-only artifact
	// would deterministically resolve UNAVAILABLE once scheduled off-node,
	// which is a fixture bug, not the race this test targets.
	if _, err := service.RegisterArtifact(context.Background(), domain.Artifact{
		SampleRunID:       "sample-race",
		ProducerNodeID:    "parent-a",
		ProducerAttemptID: "attempt-1",
		OutputName:        "dataset",
		NodeName:          "node-a",
		Digest:            "sha256:race",
		SizeBytes:         512,
		URI:               "http://artifact.local/sample-race-dataset",
	}); err != nil {
		t.Fatalf("register artifact: %v", err)
	}
	if err := service.NotifyNodeTerminal(context.Background(), "sample-race", "parent-a", "attempt-1", "Succeeded"); err != nil {
		t.Fatalf("notify terminal: %v", err)
	}
	if err := service.FinalizeSampleRun(context.Background(), "sample-race"); err != nil {
		t.Fatalf("finalize sample run: %v", err)
	}

	// Advance past the retention window so EvaluateGC will actually flip
	// GCEligible=true once it runs, rather than staying blocked forever.
	nowMu.Lock()
	baseNow = baseNow.Add(16 * time.Minute)
	nowMu.Unlock()

	binding := domain.Binding{
		SampleRunID:        "sample-race",
		ProducerNodeID:     "parent-a",
		ProducerAttemptID:  "attempt-1",
		ProducerOutputName: "dataset",
		ConsumePolicy:      domain.ConsumePolicyRemoteOK,
	}

	const iterations = 200
	var wg sync.WaitGroup
	errCh := make(chan error, iterations*2)
	statusCh := make(chan domain.ResolutionStatus, iterations)

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			if err := service.EvaluateGC(context.Background(), "sample-race"); err != nil {
				errCh <- err
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			resolved, err := service.ResolveHandoff(context.Background(), binding, "consumer-node")
			if err != nil {
				errCh <- err
				continue
			}
			statusCh <- resolved.Status
		}
	}()

	wg.Wait()
	close(errCh)
	close(statusCh)

	for err := range errCh {
		t.Fatalf("unexpected error during concurrent GC/resolve: %v", err)
	}

	seen := map[domain.ResolutionStatus]int{}
	for status := range statusCh {
		seen[status]++
		if status != domain.ResolutionStatusResolved && status != domain.ResolutionStatusGCExpired {
			t.Fatalf("ResolveHandoff returned unexpected status %q under concurrent GC", status)
		}
	}
	if seen[domain.ResolutionStatusResolved]+seen[domain.ResolutionStatusGCExpired] != iterations {
		t.Fatalf("status counts = %+v, want %d total results", seen, iterations)
	}

	// After both goroutines finish, GC has definitely run at least once past
	// the retention window, so the final state must be GC-eligible - proving
	// the eventual state converges correctly even though individual
	// concurrent resolves may have observed either status along the way.
	lifecycle, ok, err := service.GetSampleRunLifecycle(context.Background(), "sample-race")
	if err != nil {
		t.Fatalf("get lifecycle: %v", err)
	}
	if !ok || !lifecycle.GCEligible {
		t.Fatalf("expected sample run to converge to GC-eligible, got %+v (ok=%v)", lifecycle, ok)
	}
}

// TestResolveHandoffTwiceReturnsIdenticalResult makes explicit what was
// previously only an inference from "ResolveHandoffCore writes nothing":
// repeated resolves of the same binding, with no state change in between,
// return byte-for-byte identical results.
func TestResolveHandoffTwiceReturnsIdenticalResult(t *testing.T) {
	store := inventory.NewMemoryStore()
	service := newTestService(t, store)

	if _, err := service.RegisterArtifact(context.Background(), domain.Artifact{
		SampleRunID:       "sample-repeat",
		ProducerNodeID:    "parent-a",
		ProducerAttemptID: "attempt-1",
		OutputName:        "dataset",
		NodeName:          "node-a",
		Digest:            "sha256:repeat",
		URI:               "http://artifact.local/sample-repeat-dataset",
	}); err != nil {
		t.Fatalf("register artifact: %v", err)
	}

	binding := domain.Binding{
		SampleRunID:        "sample-repeat",
		ProducerNodeID:     "parent-a",
		ProducerAttemptID:  "attempt-1",
		ProducerOutputName: "dataset",
		ConsumePolicy:      domain.ConsumePolicyRemoteOK,
	}

	first, err := service.ResolveHandoff(context.Background(), binding, "")
	if err != nil {
		t.Fatalf("first ResolveHandoff: %v", err)
	}
	second, err := service.ResolveHandoff(context.Background(), binding, "")
	if err != nil {
		t.Fatalf("second ResolveHandoff: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("repeated ResolveHandoff calls diverged:\nfirst=%+v\nsecond=%+v", first, second)
	}
	if first.Status != domain.ResolutionStatusResolved {
		t.Fatalf("first.Status = %q, want RESOLVED (fixture sanity check)", first.Status)
	}
}
