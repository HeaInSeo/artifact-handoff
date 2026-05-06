package domain

import "time"

type ConsumePolicy string

const (
	ConsumePolicySameNodeOnly       ConsumePolicy = "SameNodeOnly"
	ConsumePolicySameNodeThenRemote ConsumePolicy = "SameNodeThenRemote"
	ConsumePolicyRemoteOK           ConsumePolicy = "RemoteOK"
)

type AvailabilityState string

const (
	AvailabilityStateLocalOnly   AvailabilityState = "LOCAL_ONLY"
	AvailabilityStateBoth        AvailabilityState = "BOTH"
	AvailabilityStateDeleted     AvailabilityState = "DELETED"
	AvailabilityStateUnavailable AvailabilityState = "UNAVAILABLE"
)

type ResolutionStatus string

const (
	ResolutionStatusResolved ResolutionStatus = "RESOLVED"
	ResolutionStatusPending  ResolutionStatus = "PENDING"
	ResolutionStatusMissing  ResolutionStatus = "MISSING"
)

type ResolutionDecision string

const (
	ResolutionDecisionLocalReuse     ResolutionDecision = "local_reuse"
	ResolutionDecisionRemoteFetch    ResolutionDecision = "remote_fetch"
	ResolutionDecisionUnavailable    ResolutionDecision = "unavailable"
	ResolutionDecisionProducerFailed ResolutionDecision = "producer_failed"
)

type Artifact struct {
	SampleRunID    string    `json:"sampleRunId"`
	ProducerNodeID string    `json:"producerNodeId"`
	OutputName     string    `json:"outputName"`
	ArtifactID     string    `json:"artifactId,omitempty"`
	Digest         string    `json:"digest,omitempty"`
	NodeName       string    `json:"nodeName,omitempty"`
	URI            string    `json:"uri,omitempty"`
	SizeBytes      int64     `json:"sizeBytes,omitempty"`
	CreatedAt      time.Time `json:"createdAt,omitempty"`
}

func (a Artifact) Key() string {
	return artifactKey(a.SampleRunID, a.ProducerNodeID, a.OutputName)
}

type Binding struct {
	BindingName        string        `json:"bindingName"`
	SampleRunID        string        `json:"sampleRunId"`
	ChildNodeID        string        `json:"childNodeId,omitempty"`
	ChildInputName     string        `json:"childInputName,omitempty"`
	ProducerNodeID     string        `json:"producerNodeId"`
	ProducerOutputName string        `json:"producerOutputName"`
	ArtifactID         string        `json:"artifactId,omitempty"`
	ConsumePolicy      ConsumePolicy `json:"consumePolicy,omitempty"`
	ExpectedDigest     string        `json:"expectedDigest,omitempty"`
	Required           bool          `json:"required,omitempty"`
}

func (b Binding) Key() string {
	return artifactKey(b.SampleRunID, b.ProducerNodeID, b.ProducerOutputName)
}

type ResolvedHandoff struct {
	Status                  ResolutionStatus   `json:"resolutionStatus"`
	Decision                ResolutionDecision `json:"decision"`
	SourceNodeName          string             `json:"sourceNodeName,omitempty"`
	ArtifactURI             string             `json:"artifactURI,omitempty"`
	RequiresMaterialization bool               `json:"requiresMaterialization"`
}

type NodeTerminalRecord struct {
	SampleRunID   string    `json:"sampleRunId"`
	NodeID        string    `json:"nodeId"`
	TerminalState string    `json:"terminalState"`
	RecordedAt    time.Time `json:"recordedAt"`
}

type SampleRunLifecycle struct {
	SampleRunID           string        `json:"sampleRunId"`
	Finalized             bool          `json:"finalized"`
	FinalizedAt           *time.Time    `json:"finalizedAt,omitempty"`
	RetentionPolicySource string        `json:"retentionPolicySource,omitempty"`
	RetentionDuration     time.Duration `json:"retentionDuration,omitempty"`
	RetentionUntil        *time.Time    `json:"retentionUntil,omitempty"`
	GCEligible            bool          `json:"gcEligible"`
	GCEligibleAt          *time.Time    `json:"gcEligibleAt,omitempty"`
	GCBlockedReason       string        `json:"gcBlockedReason,omitempty"`
	TerminalNodeCount     int           `json:"terminalNodeCount,omitempty"`
	SucceededNodeCount    int           `json:"succeededNodeCount,omitempty"`
	FailedNodeCount       int           `json:"failedNodeCount,omitempty"`
	CanceledNodeCount     int           `json:"canceledNodeCount,omitempty"`
	RetainedArtifactCount int           `json:"retainedArtifactCount,omitempty"`
	RetainedArtifactBytes int64         `json:"retainedArtifactBytes,omitempty"`
}

func artifactKey(sampleRunID, producerNodeID, outputName string) string {
	return sampleRunID + "::" + producerNodeID + "::" + outputName
}
