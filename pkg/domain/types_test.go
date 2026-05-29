package domain

import "testing"

func TestValidateArtifactSourceRejectsMixedLocation(t *testing.T) {
	source := ArtifactSource{
		SourceID:   "src-1",
		ArtifactID: "art-1",
		BackendID:  "node-local-default",
		Location: Location{
			NodeLocal: &NodeLocalLocation{NodeName: "worker-2", Path: "/var/lib/jumi-artifacts/cas/sha256/abc123"},
			HTTP:      &HTTPSource{URI: "http://artifact-source.local/artifacts/abc123"},
		},
	}

	if err := ValidateArtifactSource(source); err == nil {
		t.Fatal("expected mixed location payload to be rejected")
	}
}

func TestValidateArtifactSourceRejectsBackendLocationMismatch(t *testing.T) {
	source := ArtifactSource{
		SourceID:   "src-1",
		ArtifactID: "art-1",
		BackendID:  "node-local-default",
		Location: Location{
			HTTP: &HTTPSource{URI: "http://artifact-source.local/artifacts/abc123"},
		},
	}

	if err := ValidateArtifactSource(source); err == nil {
		t.Fatal("expected backend/location mismatch to be rejected")
	}
}
