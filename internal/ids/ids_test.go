package ids

import "testing"

func TestArtifactKeyString(t *testing.T) {
	key := ArtifactKey{
		RunID:             "run-1",
		ProducerNodeID:    "node-a",
		ProducerAttemptID: "attempt-1",
		OutputName:        "result.json",
	}

	if got := key.String(); got != "run/run-1/node-a/attempt-1/result.json" {
		t.Fatalf("ArtifactKey.String() = %q, want %q", got, "run/run-1/node-a/attempt-1/result.json")
	}
}

func TestNodeAttemptKeyString(t *testing.T) {
	key := NodeAttemptKey{
		RunID:     "run-1",
		NodeID:    "node-a",
		AttemptID: "attempt-1",
	}

	if got := key.String(); got != "run/run-1/node-a/attempt-1" {
		t.Fatalf("NodeAttemptKey.String() = %q, want %q", got, "run/run-1/node-a/attempt-1")
	}
}

// F4 Mode B: a RunID key can never equal a pre-F4 SampleRunID key, even when the
// RunID string equals the old SampleRunID (separate key namespace).
func TestRunKeysDisjointFromLegacySampleKeys(t *testing.T) {
	legacyArtifact := "run-1/node-a/attempt-1/result.json"
	legacyTerminal := "run-1/node-a/attempt-1"
	if got := (ArtifactKey{RunID: "run-1", ProducerNodeID: "node-a", ProducerAttemptID: "attempt-1", OutputName: "result.json"}).String(); got == legacyArtifact {
		t.Fatalf("run-scoped artifact key collides with legacy key %q", legacyArtifact)
	}
	if got := (NodeAttemptKey{RunID: "run-1", NodeID: "node-a", AttemptID: "attempt-1"}).String(); got == legacyTerminal {
		t.Fatalf("run-scoped terminal key collides with legacy key %q", legacyTerminal)
	}
}

// F4 Mode B: a missing RunID fails closed.
func TestKeysRequireRunID(t *testing.T) {
	if err := (ArtifactKey{ProducerNodeID: "n", ProducerAttemptID: "a", OutputName: "o"}).Validate(); err == nil {
		t.Fatal("ArtifactKey without RunID validated")
	}
	if err := (NodeAttemptKey{NodeID: "n", AttemptID: "a"}).Validate(); err == nil {
		t.Fatal("NodeAttemptKey without RunID validated")
	}
	if err := (ArtifactKey{RunID: "r/1", ProducerNodeID: "n", ProducerAttemptID: "a", OutputName: "o"}).Validate(); err == nil {
		t.Fatal("RunID containing the separator validated")
	}
}
