package domain

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// ConsumePolicy.Validate
// ---------------------------------------------------------------------------

func TestConsumePolicy_Validate(t *testing.T) {
	tests := []struct {
		name    string
		policy  ConsumePolicy
		wantErr bool
	}{
		{"SameNodeOnly", ConsumePolicySameNodeOnly, false},
		{"SameNodeThenRemote", ConsumePolicySameNodeThenRemote, false},
		{"RemoteOK", ConsumePolicyRemoteOK, false},
		{"empty", "", true},
		{"unknown", ConsumePolicy("Bogus"), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.policy.Validate()
			if (err != nil) != tc.wantErr {
				t.Fatalf("Validate() error = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// SourceState.Validate
// ---------------------------------------------------------------------------

func TestSourceState_Validate(t *testing.T) {
	tests := []struct {
		name    string
		state   SourceState
		wantErr bool
	}{
		{"pending", SourceStatePending, false},
		{"ready", SourceStateReady, false},
		{"stale", SourceStateStale, false},
		{"unreachable", SourceStateUnreachable, false},
		{"deleted", SourceStateDeleted, false},
		{"empty", "", true},
		{"unknown", SourceState("flying"), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.state.Validate()
			if (err != nil) != tc.wantErr {
				t.Fatalf("Validate() error = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Artifact.Key / CanonicalID
// ---------------------------------------------------------------------------

func TestArtifactKey(t *testing.T) {
	a := Artifact{
		SampleRunID:       "run-1",
		ProducerNodeID:    "node-a",
		ProducerAttemptID: "att-1",
		OutputName:        "model",
	}
	key := a.Key()
	if key == "" {
		t.Fatal("Key() returned empty string")
	}
	// Must contain all four components separated by "/"
	for _, part := range []string{"run-1", "node-a", "att-1", "model"} {
		if !strings.Contains(key, part) {
			t.Errorf("Key() %q does not contain %q", key, part)
		}
	}
	if a.CanonicalID() != key {
		t.Errorf("CanonicalID() = %q, want %q", a.CanonicalID(), key)
	}
}

// ---------------------------------------------------------------------------
// Binding.Key
// ---------------------------------------------------------------------------

func TestBindingKey(t *testing.T) {
	b := Binding{
		SampleRunID:        "run-1",
		ProducerNodeID:     "node-a",
		ProducerAttemptID:  "att-1",
		ProducerOutputName: "model",
	}
	key := b.Key()
	if key == "" {
		t.Fatal("Binding.Key() returned empty string")
	}
	for _, part := range []string{"run-1", "node-a", "att-1", "model"} {
		if !strings.Contains(key, part) {
			t.Errorf("Key() %q does not contain %q", key, part)
		}
	}
}

// ---------------------------------------------------------------------------
// ValidateArtifactForRegistration — legacy URI branch
// ---------------------------------------------------------------------------

func TestValidateArtifactForRegistration(t *testing.T) {
	tests := []struct {
		name    string
		a       Artifact
		wantErr bool
	}{
		{
			name: "no_uri_ok",
			a: Artifact{
				SampleRunID: "run-1", ProducerNodeID: "node-a",
				ProducerAttemptID: "att-1", OutputName: "x",
			},
			wantErr: false,
		},
		{
			name: "valid_https_uri",
			a: Artifact{
				SampleRunID: "run-1", ProducerNodeID: "node-a",
				ProducerAttemptID: "att-1", OutputName: "x",
				URI: "https://storage.example.com/bucket/artifact.bin",
			},
			wantErr: false,
		},
		{
			name: "uri_with_userinfo_rejected",
			a: Artifact{
				SampleRunID: "run-1", ProducerNodeID: "node-a",
				ProducerAttemptID: "att-1", OutputName: "x",
				URI: "http://user:pass@host/path", // #nosec G101 -- test-only credential in URL string
			},
			wantErr: true,
		},
		{
			name: "uri_with_query_rejected",
			a: Artifact{
				SampleRunID: "run-1", ProducerNodeID: "node-a",
				ProducerAttemptID: "att-1", OutputName: "x",
				URI: "http://host/path?key=value",
			},
			wantErr: true,
		},
		{
			name: "non_http_scheme_rejected",
			a: Artifact{
				SampleRunID: "run-1", ProducerNodeID: "node-a",
				ProducerAttemptID: "att-1", OutputName: "x",
				URI: "ftp://host/file",
			},
			wantErr: true,
		},
		{
			name: "empty_host_rejected",
			a: Artifact{
				SampleRunID: "run-1", ProducerNodeID: "node-a",
				ProducerAttemptID: "att-1", OutputName: "x",
				URI: "http:///path",
			},
			wantErr: true,
		},
		{
			name: "valid_location_node_local",
			a: Artifact{
				SampleRunID: "run-1", ProducerNodeID: "node-a",
				ProducerAttemptID: "att-1", OutputName: "x",
				Locations: []Location{
					{NodeLocal: &NodeLocalLocation{NodeName: "worker-1", Path: "/data/file"}},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid_location_empty",
			a: Artifact{
				SampleRunID: "run-1", ProducerNodeID: "node-a",
				ProducerAttemptID: "att-1", OutputName: "x",
				Locations: []Location{{}},
			},
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateArtifactForRegistration(tc.a)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ValidateArtifactForRegistration() error = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ValidateArtifactLocation
// ---------------------------------------------------------------------------

func TestValidateArtifactLocation(t *testing.T) {
	tests := []struct {
		name    string
		loc     Location
		wantErr bool
	}{
		{
			name:    "empty_location",
			loc:     Location{},
			wantErr: true,
		},
		{
			name: "node_local_ok",
			loc: Location{
				NodeLocal: &NodeLocalLocation{NodeName: "node-1", Path: "/data"},
			},
			wantErr: false,
		},
		{
			name: "http_ok",
			loc: Location{
				HTTP: &HTTPSource{URI: "https://example.com/file"},
			},
			wantErr: false,
		},
		{
			name: "mixed_backends_rejected",
			loc: Location{
				NodeLocal: &NodeLocalLocation{NodeName: "n", Path: "/p"},
				HTTP:      &HTTPSource{URI: "https://example.com/file"},
			},
			wantErr: true,
		},
		{
			name: "http_missing_uri_rejected",
			loc: Location{
				HTTP: &HTTPSource{},
			},
			wantErr: true,
		},
		{
			name: "http_with_headers_rejected",
			loc: Location{
				HTTP: &HTTPSource{
					URI:     "https://example.com/file",
					Headers: map[string]string{"X-Token": "secret"},
				},
			},
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateArtifactLocation(tc.loc)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ValidateArtifactLocation() error = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Location.BackendType
// ---------------------------------------------------------------------------

func TestLocationBackendType(t *testing.T) {
	tests := []struct {
		name string
		loc  Location
		want SourceBackendType
	}{
		{"empty", Location{}, SourceBackendTypeUnknown},
		{"node_local", Location{NodeLocal: &NodeLocalLocation{}}, SourceBackendTypeNodeLocal},
		{"http", Location{HTTP: &HTTPSource{}}, SourceBackendTypeHTTP},
		{
			"mixed",
			Location{NodeLocal: &NodeLocalLocation{}, HTTP: &HTTPSource{}},
			SourceBackendTypeUnknown,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.loc.BackendType()
			if got != tc.want {
				t.Fatalf("BackendType() = %q, want %q", got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// BackendTypeForID
// ---------------------------------------------------------------------------

func TestBackendTypeForID(t *testing.T) {
	tests := []struct {
		id   string
		want SourceBackendType
	}{
		{"node-local-default", SourceBackendTypeNodeLocal},
		{"legacy-http", SourceBackendTypeHTTP},
		{"lab-http-cache", SourceBackendTypeHTTP},
		{"unknown-backend", SourceBackendTypeUnknown},
		{"", SourceBackendTypeUnknown},
		{"  ", SourceBackendTypeUnknown},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			got := BackendTypeForID(tc.id)
			if got != tc.want {
				t.Fatalf("BackendTypeForID(%q) = %q, want %q", tc.id, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ValidateArtifactSource — success path + missing-field paths
// ---------------------------------------------------------------------------

func TestValidateArtifactSource_Valid(t *testing.T) {
	source := ArtifactSource{
		SourceID:   "src-1",
		ArtifactID: "art-1",
		BackendID:  "node-local-default",
		Location: Location{
			NodeLocal: &NodeLocalLocation{NodeName: "worker-1", Path: "/data/file"},
		},
	}
	if err := ValidateArtifactSource(source); err != nil {
		t.Fatalf("ValidateArtifactSource() unexpected error: %v", err)
	}
}

func TestValidateArtifactSource_MissingFields(t *testing.T) {
	base := ArtifactSource{
		SourceID:   "src-1",
		ArtifactID: "art-1",
		BackendID:  "node-local-default",
		Location: Location{
			NodeLocal: &NodeLocalLocation{NodeName: "n", Path: "/p"},
		},
	}
	tests := []struct {
		name   string
		mutate func(*ArtifactSource)
	}{
		{"missing_source_id", func(s *ArtifactSource) { s.SourceID = "" }},
		{"missing_artifact_id", func(s *ArtifactSource) { s.ArtifactID = "" }},
		{"missing_backend_id", func(s *ArtifactSource) { s.BackendID = "" }},
		{"unknown_backend_id", func(s *ArtifactSource) { s.BackendID = "not-a-real-backend" }},
		{"wrong_location_type", func(s *ArtifactSource) {
			s.BackendID = "legacy-http"
			// location is still NodeLocal — mismatch
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := base
			tc.mutate(&s)
			if err := ValidateArtifactSource(s); err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ValidateArtifactSourceForArtifact
// ---------------------------------------------------------------------------

func TestValidateArtifactSourceForArtifact(t *testing.T) {
	validArtifact := Artifact{
		ArtifactID: "art-1",
		Digest:     "sha256:abc",
	}
	validSource := ArtifactSource{
		SourceID:   "src-1",
		ArtifactID: "art-1",
		BackendID:  "node-local-default",
		Digest:     "sha256:abc",
		Location: Location{
			NodeLocal: &NodeLocalLocation{NodeName: "n", Path: "/p"},
		},
	}

	tests := []struct {
		name     string
		artifact Artifact
		source   ArtifactSource
		wantErr  bool
	}{
		{
			name:     "valid",
			artifact: validArtifact,
			source:   validSource,
			wantErr:  false,
		},
		{
			name:     "artifact_id_mismatch",
			artifact: validArtifact,
			source:   func() ArtifactSource { s := validSource; s.ArtifactID = "wrong"; return s }(),
			wantErr:  true,
		},
		{
			name:     "missing_source_digest",
			artifact: validArtifact,
			source:   func() ArtifactSource { s := validSource; s.Digest = ""; return s }(),
			wantErr:  true,
		},
		{
			name:     "missing_artifact_digest",
			artifact: func() Artifact { a := validArtifact; a.Digest = ""; return a }(),
			source:   validSource,
			wantErr:  true,
		},
		{
			name:     "digest_mismatch",
			artifact: validArtifact,
			source:   func() ArtifactSource { s := validSource; s.Digest = "sha256:different"; return s }(),
			wantErr:  true,
		},
		{
			name:     "artifact_id_empty_skips_id_check",
			artifact: func() Artifact { a := validArtifact; a.ArtifactID = ""; return a }(),
			source:   validSource,
			wantErr:  false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateArtifactSourceForArtifact(tc.artifact, tc.source)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ValidateArtifactSourceForArtifact() error = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// SourceID — deterministic hash
// ---------------------------------------------------------------------------

func TestSourceID(t *testing.T) {
	id1 := SourceID("art-1", "node-local-default", "node_local:worker-1:/data")
	id2 := SourceID("art-1", "node-local-default", "node_local:worker-1:/data")
	id3 := SourceID("art-1", "node-local-default", "node_local:worker-2:/data")

	if id1 == "" {
		t.Fatal("SourceID returned empty string")
	}
	if !strings.HasPrefix(id1, "src-") {
		t.Errorf("SourceID %q does not start with 'src-'", id1)
	}
	if id1 != id2 {
		t.Errorf("SourceID not deterministic: %q vs %q", id1, id2)
	}
	if id1 == id3 {
		t.Errorf("expected different IDs for different fingerprints")
	}
}

// ---------------------------------------------------------------------------
// LocationFingerprint
// ---------------------------------------------------------------------------

func TestLocationFingerprint(t *testing.T) {
	tests := []struct {
		name   string
		loc    Location
		prefix string
		empty  bool
	}{
		{
			name:   "node_local",
			loc:    Location{NodeLocal: &NodeLocalLocation{NodeName: "node-1", Path: "/var/lib/jumi"}},
			prefix: "node_local:",
		},
		{
			name:   "http",
			loc:    Location{HTTP: &HTTPSource{URI: "https://storage.example.com/artifact"}},
			prefix: "http:",
		},
		{
			name:  "empty_returns_empty_string",
			loc:   Location{},
			empty: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fp := LocationFingerprint(tc.loc)
			if tc.empty {
				if fp != "" {
					t.Fatalf("expected empty fingerprint, got %q", fp)
				}
				return
			}
			if !strings.HasPrefix(fp, tc.prefix) {
				t.Fatalf("LocationFingerprint() = %q, want prefix %q", fp, tc.prefix)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// validateHTTPSource (indirectly via ValidateArtifactSource)
// ---------------------------------------------------------------------------

func TestValidateHTTPSource_EdgeCases(t *testing.T) {
	httpBackend := func(uri string, headers map[string]string) ArtifactSource {
		return ArtifactSource{
			SourceID:   "src-1",
			ArtifactID: "art-1",
			BackendID:  "legacy-http",
			Location:   Location{HTTP: &HTTPSource{URI: uri, Headers: headers}},
		}
	}

	tests := []struct {
		name    string
		source  ArtifactSource
		wantErr bool
	}{
		{"valid_http", httpBackend("http://host/path", nil), false},
		{"valid_https", httpBackend("https://host/path", nil), false},
		{"empty_uri", httpBackend("", nil), true},
		{"whitespace_uri", httpBackend("   ", nil), true},
		{"ftp_scheme", httpBackend("ftp://host/path", nil), true},
		{"userinfo_in_uri", httpBackend("http://user:pass@host/path", nil), true},
		{"query_string", httpBackend("http://host/path?q=1", nil), true},
		{"empty_host", httpBackend("http:///path", nil), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateArtifactSource(tc.source)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ValidateArtifactSource() error = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}
