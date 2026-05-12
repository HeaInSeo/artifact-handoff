package domain

import (
	"fmt"
	"time"

	"github.com/HeaInSeo/artifact-handoff/internal/ids"
)

type ConsumePolicy string

const (
	ConsumePolicySameNodeOnly       ConsumePolicy = "SameNodeOnly"
	ConsumePolicySameNodeThenRemote ConsumePolicy = "SameNodeThenRemote"
	ConsumePolicyRemoteOK           ConsumePolicy = "RemoteOK"
)

func (p ConsumePolicy) Validate() error {
	switch p {
	case ConsumePolicySameNodeOnly, ConsumePolicySameNodeThenRemote, ConsumePolicyRemoteOK:
		return nil
	case "":
		return fmt.Errorf("consume policy is required")
	default:
		return fmt.Errorf("unknown consume policy %q", p)
	}
}

type AvailabilityState string

const (
	AvailabilityStateLocalOnly   AvailabilityState = "LOCAL_ONLY"
	AvailabilityStateRemoteOnly  AvailabilityState = "REMOTE_ONLY"
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
	SampleRunID       string    `json:"sampleRunId"`
	ProducerNodeID    string    `json:"producerNodeId"`
	ProducerAttemptID string    `json:"producerAttemptId"`
	OutputName        string    `json:"outputName"`
	ArtifactID        string    `json:"artifactId,omitempty"`
	Digest            string    `json:"digest,omitempty"`
	NodeName          string    `json:"nodeName,omitempty"`
	URI               string    `json:"uri,omitempty"`
	SizeBytes         int64     `json:"sizeBytes,omitempty"`
	CreatedAt         time.Time `json:"createdAt,omitempty"`
}

func (a Artifact) Key() string {
	return ids.ArtifactKey{
		SampleRunID:       a.SampleRunID,
		ProducerNodeID:    a.ProducerNodeID,
		ProducerAttemptID: a.ProducerAttemptID,
		OutputName:        a.OutputName,
	}.String()
}

// CanonicalID returns the product-owned artifact identity.
// Format: sampleRunId/producerNodeId/producerAttemptId/outputName
func (a Artifact) CanonicalID() string {
	return a.Key()
}

type Binding struct {
	BindingName        string        `json:"bindingName"`
	SampleRunID        string        `json:"sampleRunId"`
	ChildNodeID        string        `json:"childNodeId,omitempty"`
	ChildInputName     string        `json:"childInputName,omitempty"`
	ProducerNodeID     string        `json:"producerNodeId"`
	ProducerAttemptID  string        `json:"producerAttemptId"`
	ChildAttemptID     string        `json:"childAttemptId,omitempty"`
	ProducerOutputName string        `json:"producerOutputName"`
	ArtifactID         string        `json:"artifactId,omitempty"`
	ConsumePolicy      ConsumePolicy `json:"consumePolicy,omitempty"`
	ExpectedDigest     string        `json:"expectedDigest,omitempty"`
	Required           bool          `json:"required,omitempty"`
}

func (b Binding) Key() string {
	return ids.ArtifactKey{
		SampleRunID:       b.SampleRunID,
		ProducerNodeID:    b.ProducerNodeID,
		ProducerAttemptID: b.ProducerAttemptID,
		OutputName:        b.ProducerOutputName,
	}.String()
}

type PlacementIntentMode string

const (
	PlacementIntentModeNone          PlacementIntentMode = "none"
	PlacementIntentModePreferredNode PlacementIntentMode = "preferred_node"
	PlacementIntentModeRequiredNode  PlacementIntentMode = "required_node"
)

type MaterializationMode string

const (
	MaterializationModeNone        MaterializationMode = "none"
	MaterializationModeLocalReuse  MaterializationMode = "local_reuse"
	MaterializationModeRemoteFetch MaterializationMode = "remote_fetch"
)

type PlacementIntent struct {
	Mode     PlacementIntentMode `json:"mode"`
	NodeName string              `json:"nodeName,omitempty"`
}

type MaterializationPlan struct {
	Mode           MaterializationMode `json:"mode"`
	URI            string              `json:"uri,omitempty"`
	ExpectedDigest string              `json:"expectedDigest,omitempty"`
}

type ResolvedHandoff struct {
	Status   ResolutionStatus   `json:"resolutionStatus"`
	Decision ResolutionDecision `json:"decision"`

	PlacementIntent     PlacementIntent     `json:"placementIntent"`
	MaterializationPlan MaterializationPlan `json:"materializationPlan"`

	Reason    string `json:"reason,omitempty"`
	Retryable bool   `json:"retryable"`
}

type NodeTerminalRecord struct {
	SampleRunID   string    `json:"sampleRunId"`
	NodeID        string    `json:"nodeId"`
	AttemptID     string    `json:"attemptId"`
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
