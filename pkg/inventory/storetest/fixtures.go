package storetest

import (
	"context"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/HeaInSeo/artifact-handoff/pkg/domain"
	"github.com/HeaInSeo/artifact-handoff/pkg/inventory"
)

const (
	runA      = "run-a"
	runB      = "run-b"
	sampleRun = "sample-1"
	node      = "producer"
	attempt   = "attempt-1"
	output    = "dataset"
	digestOne = "sha256:one"
	digestTwo = "sha256:two"
	succeeded = "Succeeded"
	failed    = "Failed"
)

// at is a fixed UTC instant, so durable round-trips compare with DeepEqual.
var at = time.Date(2026, 10, 1, 8, 0, 0, 123456789, time.UTC)

// artifact returns a fully populated Run-keyed artifact whose ArtifactID is its
// canonical key.
func artifact(runID, digest string) domain.Artifact {
	a := domain.Artifact{
		RunID: runID, SampleRunID: sampleRun, ProducerNodeID: node, ProducerAttemptID: attempt, OutputName: output,
		Digest: digest, LogicalURI: "ah://" + runID + "/" + output, NodeName: "worker-1",
		Locations: []domain.Location{{NodeLocal: &domain.NodeLocalLocation{NodeName: "worker-1", Path: "/data/" + runID}}},
		SizeBytes: 42, CreatedAt: at,
	}
	a.ArtifactID = a.Key()
	return a
}

func terminal(runID, state string) domain.NodeTerminalRecord {
	return domain.NodeTerminalRecord{RunID: runID, NodeID: node, AttemptID: attempt, TerminalState: state, RecordedAt: at}
}

func lifecycle(runID string, finalized bool) domain.RunLifecycle {
	until := at.Add(24 * time.Hour)
	lc := domain.RunLifecycle{
		RunID: runID, SampleRunID: sampleRun, RetentionPolicySource: "default", RetentionDuration: 24 * time.Hour,
		RetentionUntil: &until, TerminalNodeCount: 1, SucceededNodeCount: 1, RetainedArtifactCount: 1, RetainedArtifactBytes: 42,
	}
	if finalized {
		lc.Finalized, lc.FinalizedAt = true, &at
	}
	return lc
}

func source(sourceID, artifactID, path string) domain.ArtifactSource {
	return domain.ArtifactSource{
		SourceID: sourceID, ArtifactID: artifactID, BackendID: "node-local-default", Digest: digestOne,
		State: domain.SourceStateReady, LocationFingerprint: "node_local:worker-1:" + path,
		Location:  domain.Location{NodeLocal: &domain.NodeLocalLocation{NodeName: "worker-1", Path: path}},
		CreatedAt: at, UpdatedAt: at,
	}
}

func mustPutArtifact(t *testing.T, s inventory.Store, a domain.Artifact) {
	t.Helper()
	if err := s.PutArtifact(context.Background(), a); err != nil {
		t.Fatalf("PutArtifact(%s): %v", a.Key(), err)
	}
}

func mustRecordTerminal(t *testing.T, s inventory.Store, r domain.NodeTerminalRecord) {
	t.Helper()
	if err := s.RecordNodeTerminal(context.Background(), r); err != nil {
		t.Fatalf("RecordNodeTerminal(%s, %s): %v", r.RunID, r.TerminalState, err)
	}
}

func mustUpsertLifecycle(t *testing.T, s inventory.Store, lc domain.RunLifecycle) {
	t.Helper()
	if err := s.UpsertRunLifecycle(context.Background(), lc); err != nil {
		t.Fatalf("UpsertRunLifecycle(%s): %v", lc.RunID, err)
	}
}

func mustPutSources(t *testing.T, s inventory.Store, artifactID string, sources ...domain.ArtifactSource) {
	t.Helper()
	if err := s.PutArtifactSources(context.Background(), artifactID, sources); err != nil {
		t.Fatalf("PutArtifactSources(%s): %v", artifactID, err)
	}
}

func assertArtifact(t *testing.T, s inventory.Store, want domain.Artifact) {
	t.Helper()
	got, ok, err := s.GetArtifact(context.Background(), want.RunID, want.ProducerNodeID, want.ProducerAttemptID, want.OutputName)
	if err != nil || !ok {
		t.Fatalf("GetArtifact(%s): ok=%v err=%v", want.Key(), ok, err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("GetArtifact(%s)\n got %+v\nwant %+v", want.Key(), got, want)
	}
	byID, ok, err := s.GetArtifactByID(context.Background(), want.ArtifactID)
	if err != nil || !ok || !reflect.DeepEqual(byID, want) {
		t.Fatalf("GetArtifactByID(%s): ok=%v err=%v got %+v", want.ArtifactID, ok, err, byID)
	}
}

func assertTerminal(t *testing.T, s inventory.Store, want domain.NodeTerminalRecord) {
	t.Helper()
	got, ok, err := s.GetNodeTerminal(context.Background(), want.RunID, want.NodeID, want.AttemptID)
	if err != nil || !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("GetNodeTerminal(%s): ok=%v err=%v\n got %+v\nwant %+v", want.RunID, ok, err, got, want)
	}
}

func assertLifecycle(t *testing.T, s inventory.Store, want domain.RunLifecycle) {
	t.Helper()
	got, ok, err := s.GetRunLifecycle(context.Background(), want.RunID)
	if err != nil || !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("GetRunLifecycle(%s): ok=%v err=%v\n got %+v\nwant %+v", want.RunID, ok, err, got, want)
	}
}

func assertSource(t *testing.T, s inventory.Store, want domain.ArtifactSource) {
	t.Helper()
	got, ok, err := s.GetArtifactSource(context.Background(), want.SourceID)
	if err != nil || !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("GetArtifactSource(%s): ok=%v err=%v\n got %+v\nwant %+v", want.SourceID, ok, err, got, want)
	}
}

func artifactKeys(list []domain.Artifact) []string {
	keys := make([]string, 0, len(list))
	for i := range list {
		keys = append(keys, list[i].Key())
	}
	sort.Strings(keys)
	return keys
}

func lifecycleRuns(list []domain.RunLifecycle) []string {
	runs := make([]string, 0, len(list))
	for i := range list {
		runs = append(runs, list[i].RunID)
	}
	return runs
}
