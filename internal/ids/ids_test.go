package ids

import "testing"

func TestArtifactKeyString(t *testing.T) {
	key := ArtifactKey{
		SampleRunID:       "run-1",
		ProducerNodeID:    "node-a",
		ProducerAttemptID: "attempt-1",
		OutputName:        "result.json",
	}

	if got := key.String(); got != "run-1/node-a/attempt-1/result.json" {
		t.Fatalf("ArtifactKey.String() = %q, want %q", got, "run-1/node-a/attempt-1/result.json")
	}
}

func TestNodeAttemptKeyString(t *testing.T) {
	key := NodeAttemptKey{
		SampleRunID: "run-1",
		NodeID:      "node-a",
		AttemptID:   "attempt-1",
	}

	if got := key.String(); got != "run-1/node-a/attempt-1" {
		t.Fatalf("NodeAttemptKey.String() = %q, want %q", got, "run-1/node-a/attempt-1")
	}
}
