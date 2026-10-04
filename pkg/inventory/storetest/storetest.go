// Package storetest is the reusable, backend-neutral inventory.Store contract
// suite. MemoryStore, SQLiteStore and any future backend run the same cases
// through Run with their own Harness, so Run separation, ACK-loss convergence,
// source ownership and RunID fail-closed semantics are proven per backend
// instead of being assumed. It selects no storage product or topology.
package storetest

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/HeaInSeo/artifact-handoff/pkg/domain"
	"github.com/HeaInSeo/artifact-handoff/pkg/inventory"
)

// Harness adapts one Store backend to the suite.
type Harness struct {
	// New returns an empty Store. Required.
	New func(t *testing.T) inventory.Store
	// Reopen returns a NEW Store over the same durable state as s, as a restarted
	// process would see it. A backend without durable state leaves it nil and the
	// Reopen case is skipped as NOT IMPLEMENTED rather than passed.
	Reopen func(t *testing.T, s inventory.Store) inventory.Store
	// SeedLegacy writes rows into s's durable state below the Store API, exactly
	// as a pre-F4 binary would have left them (no RunID). A backend that cannot
	// hold legacy rows leaves it nil and the Legacy cases are skipped as NOT
	// IMPLEMENTED.
	SeedLegacy func(t *testing.T, s inventory.Store, rows LegacyRows)
	// LegacySnapshot returns a canonical dump of every legacy-unresolved row in
	// s's durable state. Required with SeedLegacy; it proves legacy rows are
	// retained and unmutated, since no Store read may return them.
	LegacySnapshot func(t *testing.T, s inventory.Store) string
	// SetLifecycle writes a run lifecycle the way this backend supports it. nil means
	// s.UpsertRunLifecycle. A backend that refuses the versionless upsert (the J2 store,
	// TX1-05) supplies its fenced write here so the lifecycle contract still runs.
	SetLifecycle func(ctx context.Context, s inventory.Store, lc domain.RunLifecycle) error
}

func (h Harness) setLifecycle(ctx context.Context, s inventory.Store, lc domain.RunLifecycle) error {
	if h.SetLifecycle != nil {
		return h.SetLifecycle(ctx, s, lc)
	}
	return s.UpsertRunLifecycle(ctx, lc)
}

// Run executes every contract case against h.
func Run(t *testing.T, h Harness) {
	t.Helper()
	if h.New == nil {
		t.Fatal("storetest: Harness.New is required")
	}
	t.Run("RunSeparation", func(t *testing.T) { testRunSeparation(t, h) })
	t.Run("PutArtifactConvergence", func(t *testing.T) { testPutArtifactConvergence(t, h) })
	t.Run("RecordNodeTerminalConvergence", func(t *testing.T) { testRecordNodeTerminalConvergence(t, h) })
	t.Run("UpsertRunLifecycleConvergence", func(t *testing.T) { testUpsertRunLifecycleConvergence(t, h) })
	t.Run("SourceOwnershipConflictZeroMutation", func(t *testing.T) { testSourceOwnershipConflict(t, h) })
	t.Run("SourceRequiresLiveArtifact", func(t *testing.T) { testSourceRequiresLiveArtifact(t, h) })
	t.Run("EmptyRunIDFailsClosed", func(t *testing.T) { testEmptyRunIDFailsClosed(t, h) })
	t.Run("Reopen", func(t *testing.T) { testReopen(t, h) })
	for _, c := range legacyCases {
		t.Run(c.name, func(t *testing.T) {
			if err := c.run(t, h); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// legacyCases return an error instead of failing t, so the suite can be
// mutation-tested against deliberately broken stores.
var legacyCases = []struct {
	name string
	run  func(t *testing.T, h Harness) error
}{
	{"Legacy/InvisibleAcrossReopen", legacyInvisible},
	{"Legacy/RetainedIdempotentReopen", legacyRetainedAcrossReopen},
	{"Legacy/SourceNotClaimableByLive", legacySourceNotClaimable},
}

// The same producer/attempt/output in two Runs of one Sample are two identities:
// separate artifacts, terminals and lifecycles. SampleRunID only groups them.
func testRunSeparation(t *testing.T, h Harness) {
	ctx := context.Background()
	s := h.New(t)
	a, b := artifact(runA, digestOne), artifact(runB, digestTwo)
	mustPutArtifact(t, s, a)
	mustPutArtifact(t, s, b)
	mustRecordTerminal(t, s, terminal(runA, succeeded))
	mustRecordTerminal(t, s, terminal(runB, failed))
	mustUpsertLifecycle(t, h, s, lifecycle(runB, true))
	mustUpsertLifecycle(t, h, s, lifecycle(runA, false))

	assertArtifact(t, s, a)
	assertArtifact(t, s, b)
	assertTerminal(t, s, terminal(runA, succeeded))
	assertTerminal(t, s, terminal(runB, failed))
	assertLifecycle(t, s, lifecycle(runA, false))
	assertLifecycle(t, s, lifecycle(runB, true))

	for run, want := range map[string]domain.Artifact{runA: a, runB: b} {
		list, err := s.ListArtifactsByRun(ctx, run)
		if err != nil || len(list) != 1 || !reflect.DeepEqual(list[0], want) {
			t.Fatalf("ListArtifactsByRun(%s) = %+v err=%v, want only %s", run, list, err, want.Key())
		}
		terms, err := s.ListNodeTerminalsByRun(ctx, run)
		if err != nil || len(terms) != 1 || terms[0].RunID != run {
			t.Fatalf("ListNodeTerminalsByRun(%s) = %+v err=%v", run, terms, err)
		}
	}
	grouped, err := s.ListArtifactsBySampleRun(ctx, sampleRun)
	if got, want := artifactKeys(grouped), []string{a.Key(), b.Key()}; err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("ListArtifactsBySampleRun = %v err=%v, want %v", got, err, want)
	}
	lcs, err := s.ListRunLifecyclesBySample(ctx, sampleRun)
	if got := lifecycleRuns(lcs); err != nil || !reflect.DeepEqual(got, []string{runA, runB}) {
		t.Fatalf("ListRunLifecyclesBySample = %v err=%v, want [%s %s] in RunID order", got, err, runA, runB)
	}
}

// A committed PutArtifact whose ACK was lost converges on retry; a different or
// cleared digest is rejected and leaves the stored artifact untouched.
func testPutArtifactConvergence(t *testing.T, h Harness) {
	ctx := context.Background()
	s := h.New(t)
	a := artifact(runA, digestOne)
	mustPutArtifact(t, s, a)
	mustPutArtifact(t, s, a)
	conflicting := artifact(runA, digestTwo)
	if err := s.PutArtifact(ctx, conflicting); err == nil {
		t.Fatal("PutArtifact with a different digest succeeded")
	}
	cleared := artifact(runA, "")
	if err := s.PutArtifact(ctx, cleared); err == nil {
		t.Fatal("PutArtifact clearing the digest succeeded")
	}
	assertArtifact(t, s, a)
	if list, err := s.ListArtifactsByRun(ctx, runA); err != nil || len(list) != 1 {
		t.Fatalf("ListArtifactsByRun after retries = %d artifacts err=%v, want 1", len(list), err)
	}
}

// A committed terminal whose ACK was lost converges on retry; a different terminal
// state for the same Run/node/attempt is rejected and changes nothing.
func testRecordNodeTerminalConvergence(t *testing.T, h Harness) {
	ctx := context.Background()
	s := h.New(t)
	mustRecordTerminal(t, s, terminal(runA, succeeded))
	mustRecordTerminal(t, s, terminal(runA, succeeded))
	if err := s.RecordNodeTerminal(ctx, terminal(runA, failed)); err == nil {
		t.Fatal("RecordNodeTerminal with a different state succeeded")
	}
	assertTerminal(t, s, terminal(runA, succeeded))
	if terms, err := s.ListNodeTerminalsByRun(ctx, runA); err != nil || len(terms) != 1 {
		t.Fatalf("ListNodeTerminalsByRun after retries = %d err=%v, want 1", len(terms), err)
	}
}

// A replayed lifecycle upsert converges to one row per Run; a later upsert
// replaces that Run's lifecycle only.
func testUpsertRunLifecycleConvergence(t *testing.T, h Harness) {
	ctx := context.Background()
	s := h.New(t)
	mustUpsertLifecycle(t, h, s, lifecycle(runA, false))
	mustUpsertLifecycle(t, h, s, lifecycle(runA, false))
	mustUpsertLifecycle(t, h, s, lifecycle(runB, false))
	mustUpsertLifecycle(t, h, s, lifecycle(runA, true))
	assertLifecycle(t, s, lifecycle(runA, true))
	assertLifecycle(t, s, lifecycle(runB, false))
	lcs, err := s.ListRunLifecyclesBySample(ctx, sampleRun)
	if got := lifecycleRuns(lcs); err != nil || !reflect.DeepEqual(got, []string{runA, runB}) {
		t.Fatalf("ListRunLifecyclesBySample = %v err=%v, want one row per Run", got, err)
	}
}

// A source ID owned by one artifact is never rewritten through another artifact
// (including another Run's artifact), and a conflicting call writes none of its
// sources.
func testSourceOwnershipConflict(t *testing.T, h Harness) {
	ctx := context.Background()
	s := h.New(t)
	owner, other := artifact(runA, digestOne), artifact(runB, digestOne)
	mustPutArtifact(t, s, owner)
	mustPutArtifact(t, s, other)
	owned := source("src-owned", owner.ArtifactID, "/owner")
	mustPutSources(t, s, owner.ArtifactID, owned)
	mustPutSources(t, s, owner.ArtifactID, owned) // ACK-loss replay

	err := s.PutArtifactSources(ctx, other.ArtifactID, []domain.ArtifactSource{
		source("src-new", other.ArtifactID, "/new"),
		source("src-owned", other.ArtifactID, "/hijack"),
	})
	if !errors.Is(err, inventory.ErrSourceOwnershipConflict) {
		t.Fatalf("foreign source id: err=%v, want ErrSourceOwnershipConflict", err)
	}
	assertSource(t, s, owned)
	if _, ok, err := s.GetArtifactSource(ctx, "src-new"); ok || err != nil {
		t.Fatalf("conflicting call partially wrote src-new: ok=%v err=%v", ok, err)
	}
	if list, err := s.ListArtifactSources(ctx, other.ArtifactID); err != nil || len(list) != 0 {
		t.Fatalf("conflicting call left sources on the other artifact: %+v err=%v", list, err)
	}
	if list, err := s.ListArtifactSources(ctx, owner.ArtifactID); err != nil || len(list) != 1 || !reflect.DeepEqual(list[0], owned) {
		t.Fatalf("owner sources = %+v err=%v, want exactly the owned source", list, err)
	}
}

// A source is returned only while its artifact is a live Run-keyed row. A source
// stored before its artifact is invisible through both reads yet still owns its ID
// (another artifact cannot claim it); once the artifact is stored, the same source
// becomes visible unchanged.
func testSourceRequiresLiveArtifact(t *testing.T, h Harness) {
	ctx := context.Background()
	s := h.New(t)
	a, other := artifact(runA, digestOne), artifact(runB, digestOne)
	pending := source("src-pending", a.ArtifactID, "/pending")
	mustPutSources(t, s, a.ArtifactID, pending)
	if got, ok, err := s.GetArtifactSource(ctx, pending.SourceID); ok || err != nil {
		t.Fatalf("GetArtifactSource without artifact = %+v ok=%v err=%v, want not found", got, ok, err)
	}
	if list, err := s.ListArtifactSources(ctx, a.ArtifactID); err != nil || len(list) != 0 {
		t.Fatalf("ListArtifactSources without artifact = %+v err=%v, want empty", list, err)
	}

	mustPutArtifact(t, s, other)
	err := s.PutArtifactSources(ctx, other.ArtifactID, []domain.ArtifactSource{source(pending.SourceID, other.ArtifactID, "/hijack")})
	if !errors.Is(err, inventory.ErrSourceOwnershipConflict) {
		t.Fatalf("claiming an invisible source id: err=%v, want ErrSourceOwnershipConflict", err)
	}
	if list, err := s.ListArtifactSources(ctx, other.ArtifactID); err != nil || len(list) != 0 {
		t.Fatalf("conflicting claim left sources on the other artifact: %+v err=%v", list, err)
	}
	// Liveness is judged per referenced artifact: a different live artifact must not
	// make the pending source visible.
	if got, ok, err := s.GetArtifactSource(ctx, pending.SourceID); ok || err != nil {
		t.Fatalf("GetArtifactSource with only another artifact live = %+v ok=%v err=%v, want not found", got, ok, err)
	}
	if list, err := s.ListArtifactSources(ctx, a.ArtifactID); err != nil || len(list) != 0 {
		t.Fatalf("ListArtifactSources with only another artifact live = %+v err=%v, want empty", list, err)
	}

	mustPutArtifact(t, s, a)
	assertSource(t, s, pending)
	if list, err := s.ListArtifactSources(ctx, a.ArtifactID); err != nil || len(list) != 1 || !reflect.DeepEqual(list[0], pending) {
		t.Fatalf("ListArtifactSources once live = %+v err=%v, want exactly the pending source", list, err)
	}
}

// Nothing is persisted or attributed without a RunID: blank-RunID writes fail and
// leave no row visible through any Run- or Sample-scoped read.
func testEmptyRunIDFailsClosed(t *testing.T, h Harness) {
	ctx := context.Background()
	s := h.New(t)
	for _, run := range []string{"", "  "} {
		if err := s.PutArtifact(ctx, artifact(run, digestOne)); err == nil {
			t.Fatalf("PutArtifact(RunID=%q) succeeded", run)
		}
		if err := s.RecordNodeTerminal(ctx, terminal(run, succeeded)); err == nil {
			t.Fatalf("RecordNodeTerminal(RunID=%q) succeeded", run)
		}
		if err := h.setLifecycle(ctx, s, lifecycle(run, true)); err == nil {
			t.Fatalf("UpsertRunLifecycle(RunID=%q) succeeded", run)
		}
		if _, ok, err := s.GetArtifact(ctx, run, node, attempt, output); ok || err != nil {
			t.Fatalf("GetArtifact(RunID=%q): ok=%v err=%v", run, ok, err)
		}
		if _, ok, err := s.GetNodeTerminal(ctx, run, node, attempt); ok || err != nil {
			t.Fatalf("GetNodeTerminal(RunID=%q): ok=%v err=%v", run, ok, err)
		}
		if _, ok, err := s.GetRunLifecycle(ctx, run); ok || err != nil {
			t.Fatalf("GetRunLifecycle(RunID=%q): ok=%v err=%v", run, ok, err)
		}
	}
	if list, err := s.ListArtifactsBySampleRun(ctx, sampleRun); err != nil || len(list) != 0 {
		t.Fatalf("blank-RunID artifact visible by Sample: %+v err=%v", list, err)
	}
	if list, err := s.ListRunLifecyclesBySample(ctx, sampleRun); err != nil || len(list) != 0 {
		t.Fatalf("blank-RunID lifecycle visible by Sample: %+v err=%v", list, err)
	}
	if list, err := s.ListNodeTerminalsByRun(ctx, ""); err != nil || len(list) != 0 {
		t.Fatalf("blank-RunID terminal visible: %+v err=%v", list, err)
	}
}

// Everything committed before a restart is read back identically afterwards, and
// replay/conflict verdicts are the same across the restart.
func testReopen(t *testing.T, h Harness) {
	if h.Reopen == nil {
		t.Skip("NOT IMPLEMENTED: backend has no durable reopen; not faked with an identity reopen")
	}
	ctx := context.Background()
	s := h.New(t)
	a, b := artifact(runA, digestOne), artifact(runB, digestTwo)
	owned := source("src-owned", a.ArtifactID, "/owner")
	mustPutArtifact(t, s, a)
	mustPutArtifact(t, s, b)
	mustPutSources(t, s, a.ArtifactID, owned)
	mustRecordTerminal(t, s, terminal(runA, succeeded))
	mustUpsertLifecycle(t, h, s, lifecycle(runA, true))

	r := h.Reopen(t, s)
	if r == s {
		t.Fatal("Reopen returned the same Store instance; that is not a reopen")
	}
	assertArtifact(t, r, a)
	assertArtifact(t, r, b)
	assertSource(t, r, owned)
	assertTerminal(t, r, terminal(runA, succeeded))
	assertLifecycle(t, r, lifecycle(runA, true))

	mustPutArtifact(t, r, a)
	mustRecordTerminal(t, r, terminal(runA, succeeded))
	if err := r.PutArtifact(ctx, artifact(runA, digestTwo)); err == nil {
		t.Fatal("digest conflict accepted after reopen")
	}
	if err := r.RecordNodeTerminal(ctx, terminal(runA, failed)); err == nil {
		t.Fatal("terminal conflict accepted after reopen")
	}
	err := r.PutArtifactSources(ctx, b.ArtifactID, []domain.ArtifactSource{source("src-owned", b.ArtifactID, "/hijack")})
	if !errors.Is(err, inventory.ErrSourceOwnershipConflict) {
		t.Fatalf("source ownership after reopen: err=%v, want ErrSourceOwnershipConflict", err)
	}
	assertArtifact(t, r, a)
	assertTerminal(t, r, terminal(runA, succeeded))
	assertSource(t, r, owned)
}
