package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
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

func ValidateArtifactForRegistration(artifact Artifact) error {
	if strings.TrimSpace(artifact.URI) != "" {
		if err := validateLegacyArtifactURI(artifact.URI); err != nil {
			return err
		}
	}
	for _, location := range artifact.Locations {
		if err := ValidateArtifactLocation(location); err != nil {
			return err
		}
	}
	return nil
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

type SourceBackendType string

const (
	SourceBackendTypeUnknown   SourceBackendType = ""
	SourceBackendTypeNodeLocal SourceBackendType = "node_local"
	SourceBackendTypeHTTP      SourceBackendType = "http"
)

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

func BackendTypeForID(backendID string) SourceBackendType {
	switch strings.TrimSpace(backendID) {
	case "node-local-default":
		return SourceBackendTypeNodeLocal
	case "legacy-http", "lab-http-cache":
		return SourceBackendTypeHTTP
	default:
		return SourceBackendTypeUnknown
	}
}

func (l Location) BackendType() SourceBackendType {
	switch count := l.backendCount(); {
	case count != 1:
		return SourceBackendTypeUnknown
	case l.NodeLocal != nil:
		return SourceBackendTypeNodeLocal
	case l.HTTP != nil:
		return SourceBackendTypeHTTP
	default:
		return SourceBackendTypeUnknown
	}
}

func (l Location) ValidateTypedUnion() error {
	switch count := l.backendCount(); {
	case count == 0:
		return fmt.Errorf("location must declare exactly one backend payload")
	case count > 1:
		return fmt.Errorf("location must not mix multiple backend payloads")
	default:
		return nil
	}
}

func ValidateArtifactLocation(location Location) error {
	if err := location.ValidateTypedUnion(); err != nil {
		return err
	}
	if location.HTTP != nil {
		if err := validateHTTPSource(*location.HTTP); err != nil {
			return err
		}
	}
	return nil
}

func (l Location) backendCount() int {
	count := 0
	if l.NodeLocal != nil {
		count++
	}
	if l.HTTP != nil {
		count++
	}
	return count
}

func ValidateArtifactSource(source ArtifactSource) error {
	if strings.TrimSpace(source.SourceID) == "" {
		return fmt.Errorf("sourceID is required")
	}
	if strings.TrimSpace(source.ArtifactID) == "" {
		return fmt.Errorf("artifactID is required")
	}
	if strings.TrimSpace(source.BackendID) == "" {
		return fmt.Errorf("backendID is required")
	}
	if err := source.Location.ValidateTypedUnion(); err != nil {
		return err
	}
	expected := BackendTypeForID(source.BackendID)
	if expected == SourceBackendTypeUnknown {
		return fmt.Errorf("unknown backendID %q", source.BackendID)
	}
	if actual := source.Location.BackendType(); actual != expected {
		return fmt.Errorf("backend %q expects %s location, got %s", source.BackendID, expected, actual)
	}
	if source.Location.HTTP != nil {
		if err := validateHTTPSource(*source.Location.HTTP); err != nil {
			return err
		}
	}
	return nil
}

func ValidateArtifactSourceForArtifact(artifact Artifact, source ArtifactSource) error {
	if err := ValidateArtifactSource(source); err != nil {
		return err
	}
	if artifact.ArtifactID != "" && source.ArtifactID != artifact.ArtifactID {
		return fmt.Errorf("source artifactID %q does not match artifactID %q", source.ArtifactID, artifact.ArtifactID)
	}
	if strings.TrimSpace(source.Digest) == "" {
		return fmt.Errorf("source digest is required")
	}
	if strings.TrimSpace(artifact.Digest) == "" {
		return fmt.Errorf("artifact digest is required")
	}
	if source.Digest != artifact.Digest {
		return fmt.Errorf("source digest %q does not match artifact digest %q", source.Digest, artifact.Digest)
	}
	return nil
}

func validateHTTPSource(source HTTPSource) error {
	if strings.TrimSpace(source.URI) == "" {
		return fmt.Errorf("http source uri is required")
	}
	if len(source.Headers) != 0 {
		return fmt.Errorf("http source headers must be empty; credential embedding is not allowed")
	}
	parsed, err := url.Parse(source.URI)
	if err != nil {
		return fmt.Errorf("invalid http source uri: %w", err)
	}
	switch parsed.Scheme {
	case "http", "https":
	default:
		return fmt.Errorf("unsupported http source scheme %q", parsed.Scheme)
	}
	if parsed.User != nil {
		return fmt.Errorf("http source uri must not contain userinfo")
	}
	if strings.TrimSpace(parsed.Host) == "" {
		return fmt.Errorf("http source host is required")
	}
	if parsed.RawQuery != "" {
		return fmt.Errorf("http source uri must not contain query string")
	}
	return nil
}

func validateLegacyArtifactURI(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("invalid artifact uri: %w", err)
	}
	switch parsed.Scheme {
	case "http", "https":
	default:
		return fmt.Errorf("artifact uri must be fetchable http(s), got scheme %q", parsed.Scheme)
	}
	if parsed.User != nil {
		return fmt.Errorf("artifact uri must not contain userinfo")
	}
	if strings.TrimSpace(parsed.Host) == "" {
		return fmt.Errorf("artifact uri host is required")
	}
	if parsed.RawQuery != "" {
		return fmt.Errorf("artifact uri must not contain query string")
	}
	return nil
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
	ExpectedSize   int64               `json:"expectedSizeBytes,omitempty"`
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
	ExpectedSize   int64                      `json:"expectedSizeBytes,omitempty"`
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
