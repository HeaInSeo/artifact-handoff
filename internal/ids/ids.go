package ids

import (
	"fmt"
	"strings"
)

const sep = "/"

// runNamespace prefixes every RunID-scoped key (F4 Mode B). Pre-F4 keys were
// "sampleRunID/node/attempt/output" (4 segments) and "sampleRunID/node/attempt"
// (3 segments); a component can never contain the separator, so the extra
// leading "run" segment keeps the RunID key space disjoint from the legacy
// SampleRunID key space even when a RunID string equals an old SampleRunID.
const runNamespace = "run"

// ArtifactKey is the product-owned identity for a produced artifact.
// The string form is the canonical artifact ID and the persistence key.
// RunID is the canonical execution identity; SampleRunID is never part of it.
type ArtifactKey struct {
	RunID             string
	ProducerNodeID    string
	ProducerAttemptID string
	OutputName        string
}

func (k ArtifactKey) String() string {
	return runNamespace + sep + k.RunID + sep + k.ProducerNodeID + sep + k.ProducerAttemptID + sep + k.OutputName
}

// Validate returns an error if RunID is missing or any component contains the
// separator character, which would cause key collisions.
func (k ArtifactKey) Validate() error {
	if strings.TrimSpace(k.RunID) == "" {
		return fmt.Errorf("ids: runID is required")
	}
	for field, val := range map[string]string{
		"runID":             k.RunID,
		"producerNodeID":    k.ProducerNodeID,
		"producerAttemptID": k.ProducerAttemptID,
		"outputName":        k.OutputName,
	} {
		if strings.Contains(val, sep) {
			return fmt.Errorf("ids: %s %q must not contain separator %q", field, val, sep)
		}
	}
	return nil
}

// NodeAttemptKey is the product-owned identity for a node attempt terminal record.
type NodeAttemptKey struct {
	RunID     string
	NodeID    string
	AttemptID string
}

func (k NodeAttemptKey) String() string {
	return runNamespace + sep + k.RunID + sep + k.NodeID + sep + k.AttemptID
}

// Validate returns an error if RunID is missing or any component contains the
// separator character.
func (k NodeAttemptKey) Validate() error {
	if strings.TrimSpace(k.RunID) == "" {
		return fmt.Errorf("ids: runID is required")
	}
	for field, val := range map[string]string{
		"runID":     k.RunID,
		"nodeID":    k.NodeID,
		"attemptID": k.AttemptID,
	} {
		if strings.Contains(val, sep) {
			return fmt.Errorf("ids: %s %q must not contain separator %q", field, val, sep)
		}
	}
	return nil
}
