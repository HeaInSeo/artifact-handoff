package domain

import (
	"crypto/sha256"
	"encoding/hex"
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
	// Terminal success states.
	ResolutionStatusResolved ResolutionStatus = "RESOLVED"

	// Transient — caller should wait and retry.
	ResolutionStatusPending ResolutionStatus = "PENDING"

	// Artifact was expected but was not registered even though the producer
	// completed successfully (e.g. optional output skipped).
	ResolutionStatusMissing ResolutionStatus = "MISSING"

	// Producer node reached a Failed or Canceled terminal state; child should
	// be blocked, not retried.
	ResolutionStatusProducerFailed ResolutionStatus = "PRODUCER_FAILED"

	// ConsumePolicy forbids the requested placement (e.g. SameNodeOnly but
	// consumer landed on a different node). Fallback must not be attempted.
	ResolutionStatusPolicyBlocked ResolutionStatus = "POLICY_BLOCKED"

	// Binding specified an ExpectedDigest that does not match the registered
	// artifact digest, or the artifact has no digest at all. Indicates a
	// reproducibility / integrity violation.
	ResolutionStatusDigestMismatch ResolutionStatus = "DIGEST_MISMATCH"

	// The sample run has been marked GC-eligible; artifact data may no longer
	// be available. Caller should trigger a re-run or propagate failure.
	ResolutionStatusGCExpired ResolutionStatus = "GC_EXPIRED"

	// Artifact exists but cannot be materialised: URI is absent or the remote
	// source is unreachable.
	ResolutionStatusUnavailable ResolutionStatus = "UNAVAILABLE"
)

type ResolutionDecision string

const (
	ResolutionDecisionLocalReuse     ResolutionDecision = "local_reuse"
	ResolutionDecisionRemoteFetch    ResolutionDecision = "remote_fetch"
	ResolutionDecisionUnavailable    ResolutionDecision = "unavailable"
	ResolutionDecisionProducerFailed ResolutionDecision = "producer_failed"
)

type Artifact struct {
	SampleRunID       string     `json:"sampleRunId"`
	ProducerNodeID    string     `json:"producerNodeId"`
	ProducerAttemptID string     `json:"producerAttemptId"`
	OutputName        string     `json:"outputName"`
	ArtifactID        string     `json:"artifactId,omitempty"`
	Digest            string     `json:"digest,omitempty"`
	LogicalURI        string     `json:"logicalUri,omitempty"`
	NodeName          string     `json:"nodeName,omitempty"`
	URI               string     `json:"uri,omitempty"`
	Locations         []Location `json:"locations,omitempty"`
	SizeBytes         int64      `json:"sizeBytes,omitempty"`
	CreatedAt         time.Time  `json:"createdAt,omitempty"`
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

type Location struct {
	NodeLocal *NodeLocalLocation `json:"nodeLocal,omitempty"`
	HTTP      *HTTPSource        `json:"http,omitempty"`
}

type NodeLocalLocation struct {
	NodeName string `json:"nodeName,omitempty"`
	Path     string `json:"path,omitempty"`
}

type HTTPSource struct {
	URI     string            `json:"uri,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

type SourceState string

const (
	SourceStatePending     SourceState = "pending"
	SourceStateReady       SourceState = "ready"
	SourceStateStale       SourceState = "stale"
	SourceStateUnreachable SourceState = "unreachable"
	SourceStateDeleted     SourceState = "deleted"
)

type ArtifactSource struct {
	SourceID            string      `json:"sourceId"`
	ArtifactID          string      `json:"artifactId"`
	BackendID           string      `json:"backendId"`
	Digest              string      `json:"digest,omitempty"`
	State               SourceState `json:"state"`
	LocationFingerprint string      `json:"locationFingerprint,omitempty"`
	Location            Location    `json:"location"`
	CreatedAt           time.Time   `json:"createdAt,omitempty"`
	UpdatedAt           time.Time   `json:"updatedAt,omitempty"`
}

func SourceID(artifactID, backendID, fingerprint string) string {
	sum := sha256.Sum256([]byte(artifactID + "\x00" + backendID + "\x00" + fingerprint))
	return "src-" + hex.EncodeToString(sum[:8])
}

func LocationFingerprint(location Location) string {
	switch {
	case location.NodeLocal != nil:
		return fmt.Sprintf("node_local:%s:%s", location.NodeLocal.NodeName, location.NodeLocal.Path)
	case location.HTTP != nil:
		return fmt.Sprintf("http:%s", location.HTTP.URI)
	default:
		return ""
	}
}

type MaterializationPlan struct {
	Mode           MaterializationMode `json:"mode"`
	URI            string              `json:"uri,omitempty"`
	ExpectedDigest string              `json:"expectedDigest,omitempty"`
	SourceLocation *Location           `json:"sourceLocation,omitempty"`
	LocalPath      string              `json:"localPath,omitempty"`
}

type MaterializationCondition struct {
	Kind      string `json:"kind"`
	NodeName  string `json:"nodeName,omitempty"`
	BackendID string `json:"backendId,omitempty"`
	SourceRef string `json:"sourceRef,omitempty"`
	State     string `json:"state,omitempty"`
}

type MaterializationCandidate struct {
	Priority       int                        `json:"priority"`
	Mode           MaterializationMode        `json:"mode"`
	SourceRef      string                     `json:"sourceRef,omitempty"`
	ExpectedDigest string                     `json:"expectedDigest,omitempty"`
	LocalPath      string                     `json:"localPath,omitempty"`
	SourceLocation *Location                  `json:"sourceLocation,omitempty"`
	URI            string                     `json:"uri,omitempty"`
	Conditions     []MaterializationCondition `json:"conditions,omitempty"`
}

type ResolvedHandoff struct {
	Status   ResolutionStatus   `json:"resolutionStatus"`
	Decision ResolutionDecision `json:"decision"`

	PlacementIntent           PlacementIntent            `json:"placementIntent"`
	MaterializationPlan       MaterializationPlan        `json:"materializationPlan"`
	MaterializationCandidates []MaterializationCandidate `json:"materializationCandidates,omitempty"`

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
