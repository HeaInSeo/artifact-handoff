package ids

import (
	"fmt"
	"strings"
)

const sep = "/"

// ArtifactKey is the product-owned identity for a produced artifact.
// The string form is the canonical artifact ID and the persistence key.
type ArtifactKey struct {
	SampleRunID       string
	ProducerNodeID    string
	ProducerAttemptID string
	OutputName        string
}

func (k ArtifactKey) String() string {
	return k.SampleRunID + sep + k.ProducerNodeID + sep + k.ProducerAttemptID + sep + k.OutputName
}

// Validate returns an error if any component contains the separator character,
// which would cause key collisions.
func (k ArtifactKey) Validate() error {
	for field, val := range map[string]string{
		"sampleRunID":       k.SampleRunID,
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
	SampleRunID string
	NodeID      string
	AttemptID   string
}

func (k NodeAttemptKey) String() string {
	return k.SampleRunID + sep + k.NodeID + sep + k.AttemptID
}

// Validate returns an error if any component contains the separator character.
func (k NodeAttemptKey) Validate() error {
	for field, val := range map[string]string{
		"sampleRunID": k.SampleRunID,
		"nodeID":      k.NodeID,
		"attemptID":   k.AttemptID,
	} {
		if strings.Contains(val, sep) {
			return fmt.Errorf("ids: %s %q must not contain separator %q", field, val, sep)
		}
	}
	return nil
}
