package storetest

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/HeaInSeo/artifact-handoff/pkg/domain"
	"github.com/HeaInSeo/artifact-handoff/pkg/inventory"
)

// LegacyRows is one legacy-unresolved row set, as a pre-F4 binary left it: an
// artifact, its source and a node terminal, none of them carrying a RunID.
// Harness.SeedLegacy writes it below the Store API.
type LegacyRows struct {
	Artifact domain.Artifact
	Source   domain.ArtifactSource
	Terminal domain.NodeTerminalRecord
}

const (
	legacyArtifactID = "legacy-artifact"
	legacySourceID   = "src-legacy"
)

// legacyRows shares the Sample, producer, attempt and output of the live
// fixtures, so only the RunID guard keeps it out of every read.
func legacyRows() LegacyRows {
	a := artifact("", "sha256:legacy")
	a.ArtifactID = legacyArtifactID
	return LegacyRows{
		Artifact: a,
		Source:   source(legacySourceID, legacyArtifactID, "/legacy"),
		Terminal: terminal("", succeeded),
	}
}

// requireLegacy skips a legacy case for a backend that cannot hold legacy rows.
func requireLegacy(t *testing.T, h Harness, needReopen bool) {
	t.Helper()
	if h.SeedLegacy == nil {
		t.Skip("NOT IMPLEMENTED: backend holds no legacy-unresolved (pre-F4) rows")
	}
	if h.LegacySnapshot == nil {
		t.Fatal("storetest: Harness.LegacySnapshot is required with SeedLegacy")
	}
	if needReopen && h.Reopen == nil {
		t.Skip("NOT IMPLEMENTED: backend has no durable reopen")
	}
}

// seedLegacy seeds the legacy rows and returns the backend's snapshot of them.
func seedLegacy(t *testing.T, h Harness, s inventory.Store) (string, error) {
	t.Helper()
	h.SeedLegacy(t, s, legacyRows())
	snap := h.LegacySnapshot(t, s)
	if snap == "" {
		return "", errors.New("LegacySnapshot is empty right after SeedLegacy")
	}
	return snap, nil
}

func sameLegacy(t *testing.T, h Harness, s inventory.Store, want, phase string) error {
	t.Helper()
	if got := h.LegacySnapshot(t, s); got != want {
		return fmt.Errorf("%s: legacy rows changed\n got %q\nwant %q", phase, got, want)
	}
	return nil
}

// checkLegacyInvisible requires that no read returns or counts a legacy row and
// that the Sample grouping returns exactly the live artifacts.
func checkLegacyInvisible(s inventory.Store, live ...domain.Artifact) error {
	ctx := context.Background()
	if a, ok, err := s.GetArtifact(ctx, "", node, attempt, output); ok || err != nil {
		return fmt.Errorf("GetArtifact(RunID=\"\") = %+v ok=%v err=%v, want not found", a, ok, err)
	}
	if a, ok, err := s.GetArtifactByID(ctx, legacyArtifactID); ok || err != nil {
		return fmt.Errorf("GetArtifactByID(legacy) = %+v ok=%v err=%v, want not found", a, ok, err)
	}
	if list, err := s.ListArtifactsByRun(ctx, ""); err != nil || len(list) != 0 {
		return fmt.Errorf("ListArtifactsByRun(\"\") = %+v err=%v, want empty", list, err)
	}
	want := make([]string, 0, len(live))
	for i := range live {
		want = append(want, live[i].Key())
	}
	if list, err := s.ListArtifactsBySampleRun(ctx, sampleRun); err != nil || !reflect.DeepEqual(artifactKeys(list), want) {
		return fmt.Errorf("ListArtifactsBySampleRun = %v err=%v, want only live %v", artifactKeys(list), err, want)
	}
	if r, ok, err := s.GetNodeTerminal(ctx, "", node, attempt); ok || err != nil {
		return fmt.Errorf("GetNodeTerminal(RunID=\"\") = %+v ok=%v err=%v, want not found", r, ok, err)
	}
	if list, err := s.ListNodeTerminalsByRun(ctx, ""); err != nil || len(list) != 0 {
		return fmt.Errorf("ListNodeTerminalsByRun(\"\") = %+v err=%v, want empty", list, err)
	}
	if src, ok, err := s.GetArtifactSource(ctx, legacySourceID); ok || err != nil {
		return fmt.Errorf("GetArtifactSource(legacy) = %+v ok=%v err=%v, want not found", src, ok, err)
	}
	if list, err := s.ListArtifactSources(ctx, legacyArtifactID); err != nil || len(list) != 0 {
		return fmt.Errorf("ListArtifactSources(legacy) = %+v err=%v, want empty", list, err)
	}
	return nil
}

// checkLive requires the live artifact, its source and terminal to read back
// exactly.
func checkLive(s inventory.Store, a domain.Artifact, src domain.ArtifactSource, term domain.NodeTerminalRecord) error {
	ctx := context.Background()
	if got, ok, err := s.GetArtifact(ctx, a.RunID, a.ProducerNodeID, a.ProducerAttemptID, a.OutputName); err != nil || !ok || !reflect.DeepEqual(got, a) {
		return fmt.Errorf("live artifact: ok=%v err=%v\n got %+v\nwant %+v", ok, err, got, a)
	}
	if got, ok, err := s.GetArtifactSource(ctx, src.SourceID); err != nil || !ok || !reflect.DeepEqual(got, src) {
		return fmt.Errorf("live source: ok=%v err=%v\n got %+v\nwant %+v", ok, err, got, src)
	}
	if got, ok, err := s.GetNodeTerminal(ctx, term.RunID, term.NodeID, term.AttemptID); err != nil || !ok || !reflect.DeepEqual(got, term) {
		return fmt.Errorf("live terminal: ok=%v err=%v\n got %+v\nwant %+v", ok, err, got, term)
	}
	return nil
}

// L1: legacy-unresolved rows are invisible to every read, before and after a
// reopen, while a live artifact of the same Sample/producer/attempt/output is
// returned normally.
func legacyInvisible(t *testing.T, h Harness) error {
	requireLegacy(t, h, true)
	s := h.New(t)
	live := artifact(runA, digestOne)
	if err := s.PutArtifact(context.Background(), live); err != nil {
		return fmt.Errorf("PutArtifact(live): %w", err)
	}
	if _, err := seedLegacy(t, h, s); err != nil {
		return err
	}
	if err := checkLegacyInvisible(s, live); err != nil {
		return fmt.Errorf("before reopen: %w", err)
	}
	if err := checkLegacyInvisible(h.Reopen(t, s), live); err != nil {
		return fmt.Errorf("after reopen: %w", err)
	}
	return nil
}

// L2: reopening (and so re-running migration) is idempotent: legacy rows are
// retained byte-for-byte, never deleted, backfilled or attributed, and live
// data reads back unchanged, across repeated reopens.
func legacyRetainedAcrossReopen(t *testing.T, h Harness) error {
	requireLegacy(t, h, true)
	ctx := context.Background()
	s := h.New(t)
	live := artifact(runA, digestOne)
	owned := source("src-owned", live.ArtifactID, "/owner")
	term := terminal(runA, succeeded)
	if err := s.PutArtifact(ctx, live); err != nil {
		return fmt.Errorf("PutArtifact(live): %w", err)
	}
	if err := s.PutArtifactSources(ctx, live.ArtifactID, []domain.ArtifactSource{owned}); err != nil {
		return fmt.Errorf("PutArtifactSources(live): %w", err)
	}
	if err := s.RecordNodeTerminal(ctx, term); err != nil {
		return fmt.Errorf("RecordNodeTerminal(live): %w", err)
	}
	snap, err := seedLegacy(t, h, s)
	if err != nil {
		return err
	}
	for i := 1; i <= 2; i++ {
		s = h.Reopen(t, s)
		phase := fmt.Sprintf("reopen %d", i)
		if err := sameLegacy(t, h, s, snap, phase); err != nil {
			return err
		}
		if err := checkLive(s, live, owned, term); err != nil {
			return fmt.Errorf("%s: %w", phase, err)
		}
		if err := checkLegacyInvisible(s, live); err != nil {
			return fmt.Errorf("%s: %w", phase, err)
		}
	}
	return nil
}

// O2: a live artifact cannot claim a source ID owned by a legacy artifact. The
// call fails with ErrSourceOwnershipConflict and writes nothing: no other source
// of the call, no live source, and the legacy rows are unchanged.
func legacySourceNotClaimable(t *testing.T, h Harness) error {
	requireLegacy(t, h, false)
	ctx := context.Background()
	s := h.New(t)
	live := artifact(runA, digestOne)
	if err := s.PutArtifact(ctx, live); err != nil {
		return fmt.Errorf("PutArtifact(live): %w", err)
	}
	snap, err := seedLegacy(t, h, s)
	if err != nil {
		return err
	}
	err = s.PutArtifactSources(ctx, live.ArtifactID, []domain.ArtifactSource{
		source("src-new", live.ArtifactID, "/new"),
		source(legacySourceID, live.ArtifactID, "/hijack"),
	})
	if !errors.Is(err, inventory.ErrSourceOwnershipConflict) {
		return fmt.Errorf("claiming a legacy-owned source: err=%v, want ErrSourceOwnershipConflict", err)
	}
	if src, ok, err := s.GetArtifactSource(ctx, "src-new"); ok || err != nil {
		return fmt.Errorf("conflicting call partially wrote src-new: %+v ok=%v err=%v", src, ok, err)
	}
	if list, err := s.ListArtifactSources(ctx, live.ArtifactID); err != nil || len(list) != 0 {
		return fmt.Errorf("conflicting call left sources on the live artifact: %+v err=%v", list, err)
	}
	if err := sameLegacy(t, h, s, snap, "after conflict"); err != nil {
		return err
	}
	return checkLegacyInvisible(s, live)
}
