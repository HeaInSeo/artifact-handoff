package resolver

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
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
	_, found, getErr := store.GetArtifact(context.Background(), "sample-1", "parent-a", "attempt-1", "dataset")
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
	before, ok, err := store.GetSampleRunLifecycle(context.Background(), "sample-2")
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
	after, found, err := store.GetSampleRunLifecycle(context.Background(), "sample-2")
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
// where the sample run is GC-eligible, for many iterations under -race.
//
// This is a stress/smoke test, not a guaranteed-interleaving test: the Go
// scheduler could in principle run every EvaluateGC iteration to completion
// before any ResolveHandoff call gets scheduled (or vice versa), in which
// case this test would pass without ever actually exercising the read/write
// interleaving it's aimed at. What it reliably proves is narrower but still
// load-bearing: the storage layer has no data race under concurrent access
// (MemoryStore is mutex-protected per-call, confirmed under -race across
// many iterations), and every concurrent ResolveHandoff call returns a
// sane, fully-formed result (RESOLVED or GC_EXPIRED, never a panic, never
// any other error, never a torn/inconsistent read) regardless of ordering.
//
// TestConcurrentGCAndResolve_ForcedInterleaving below uses a gated store to
// deterministically force the actual TOCTOU window this test can only
// probabilistically stumble into.
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

// gatedUpsertStore wraps a real inventory.Store and, once armed, makes the
// next UpsertSampleRunLifecycle call pause between EvaluateGCCore's read of
// the current lifecycle and its write of the recomputed one: as soon as the
// wrapped Upsert is entered, upsertEntered is closed (signaling the test
// that GC's read-then-decide phase is done and the pre-flip state is still
// what's stored), then it blocks on upsertGate until the test closes that
// channel to let the write through. This deterministically forces the
// TOCTOU interleaving instead of hoping the Go scheduler produces it.
//
// armed starts false so setup calls (e.g. FinalizeSampleRun's own internal
// UpsertSampleRunLifecycle) pass straight through and don't deadlock on a
// gate nothing has opened yet; the test arms it right before triggering the
// one Upsert call it actually wants to intercept.
type gatedUpsertStore struct {
	inventory.Store
	armed         atomic.Bool
	upsertEntered chan struct{}
	upsertGate    chan struct{}
}

func (s *gatedUpsertStore) UpsertSampleRunLifecycle(ctx context.Context, lifecycle domain.SampleRunLifecycle) error {
	if s.armed.Load() {
		close(s.upsertEntered)
		<-s.upsertGate
	}
	return s.Store.UpsertSampleRunLifecycle(ctx, lifecycle)
}

// TestConcurrentGCAndResolve_ForcedInterleaving deterministically exercises
// the TOCTOU window flagged in
// https://github.com/HeaInSeo/artifact-handoff/issues/12:
// ResolveHandoffCore reads GCEligible once and acts on it, with no lock held
// across EvaluateGCCore's own read-then-decide-then-write sequence, so a
// resolve that lands in the middle of a concurrent EvaluateGC call can (and,
// forced here, does) observe the pre-flip state. Unlike
// TestConcurrentGCAndResolve_RaceSafeAndConsistent above, this test does not
// rely on scheduler luck: gatedUpsertStore blocks GC's write until the test
// has already run a resolve in that exact window and asserted what it saw.
func TestConcurrentGCAndResolve_ForcedInterleaving(t *testing.T) {
	store := &gatedUpsertStore{
		Store:         inventory.NewMemoryStore(),
		upsertEntered: make(chan struct{}),
		upsertGate:    make(chan struct{}),
	}
	service := newTestService(t, store)
	baseNow := time.Date(2026, 4, 21, 10, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return baseNow }

	if _, err := service.RegisterArtifact(context.Background(), domain.Artifact{
		SampleRunID:       "sample-forced-race",
		ProducerNodeID:    "parent-a",
		ProducerAttemptID: "attempt-1",
		OutputName:        "dataset",
		NodeName:          "node-a",
		Digest:            "sha256:forced-race",
		SizeBytes:         512,
		URI:               "http://artifact.local/sample-forced-race-dataset",
	}); err != nil {
		t.Fatalf("register artifact: %v", err)
	}
	if err := service.NotifyNodeTerminal(context.Background(), "sample-forced-race", "parent-a", "attempt-1", "Succeeded"); err != nil {
		t.Fatalf("notify terminal: %v", err)
	}
	if err := service.FinalizeSampleRun(context.Background(), "sample-forced-race"); err != nil {
		t.Fatalf("finalize sample run: %v", err)
	}
	baseNow = baseNow.Add(16 * time.Minute) // past the retention window

	binding := domain.Binding{
		SampleRunID:        "sample-forced-race",
		ProducerNodeID:     "parent-a",
		ProducerAttemptID:  "attempt-1",
		ProducerOutputName: "dataset",
		ConsumePolicy:      domain.ConsumePolicyRemoteOK,
	}

	store.armed.Store(true)
	gcErrCh := make(chan error, 1)
	go func() {
		gcErrCh <- service.EvaluateGC(context.Background(), "sample-forced-race")
	}()

	select {
	case <-store.upsertEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("EvaluateGC never reached its UpsertSampleRunLifecycle call")
	}

	// GC has read the not-yet-eligible lifecycle and is blocked before
	// writing the flip. A resolve right now must observe the pre-flip state.
	beforeFlip, err := service.ResolveHandoff(context.Background(), binding, "consumer-node")
	if err != nil {
		t.Fatalf("ResolveHandoff during GC's pending write: %v", err)
	}
	if beforeFlip.Status != domain.ResolutionStatusResolved {
		t.Fatalf("ResolveHandoff status during GC's pending write = %q, want RESOLVED (pre-flip state)", beforeFlip.Status)
	}

	close(store.upsertGate) // let GC's write through
	if err := <-gcErrCh; err != nil {
		t.Fatalf("EvaluateGC: %v", err)
	}

	// GC has now committed the flip. A resolve after this point must
	// observe GC_EXPIRED.
	afterFlip, err := service.ResolveHandoff(context.Background(), binding, "consumer-node")
	if err != nil {
		t.Fatalf("ResolveHandoff after GC's write: %v", err)
	}
	if afterFlip.Status != domain.ResolutionStatusGCExpired {
		t.Fatalf("ResolveHandoff status after GC's write = %q, want GC_EXPIRED (post-flip state)", afterFlip.Status)
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
