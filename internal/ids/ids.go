package ids

// ArtifactKey is the product-owned identity for a produced artifact.
// The string form is the canonical artifact ID and the persistence key.
type ArtifactKey struct {
	SampleRunID       string
	ProducerNodeID    string
	ProducerAttemptID string
	OutputName        string
}

func (k ArtifactKey) String() string {
	return k.SampleRunID + "/" + k.ProducerNodeID + "/" + k.ProducerAttemptID + "/" + k.OutputName
}

// NodeAttemptKey is the product-owned identity for a node attempt terminal record.
type NodeAttemptKey struct {
	SampleRunID string
	NodeID      string
	AttemptID   string
}

func (k NodeAttemptKey) String() string {
	return k.SampleRunID + "/" + k.NodeID + "/" + k.AttemptID
}
