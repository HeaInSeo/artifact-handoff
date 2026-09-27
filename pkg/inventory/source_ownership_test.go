package inventory_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/HeaInSeo/artifact-handoff/pkg/domain"
	"github.com/HeaInSeo/artifact-handoff/pkg/inventory"
)

func ownershipSource(sourceID, artifactID, path string) domain.ArtifactSource {
	now := time.Now().UTC()
	return domain.ArtifactSource{
		SourceID:            sourceID,
		ArtifactID:          artifactID,
		BackendID:           "node-local-default",
		Digest:              "sha256:abc",
		State:               domain.SourceStateReady,
		LocationFingerprint: "node_local:worker-1:" + path,
		Location: domain.Location{
			NodeLocal: &domain.NodeLocalLocation{NodeName: "worker-1", Path: path},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// N1: a source ID owned by one artifact is never rewritten through another artifact,
// and a call that hits such a conflict writes none of its sources.
func TestStore_PutArtifactSources_RejectsForeignSourceID(t *testing.T) {
	stores := map[string]func(t *testing.T) inventory.Store{
		"memory": func(*testing.T) inventory.Store { return inventory.NewMemoryStore() },
		"sqlite": func(t *testing.T) inventory.Store {
			s, cleanup := openSQLite(t)
			t.Cleanup(cleanup)
			return s
		},
	}
	for name, open := range stores {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			s := open(t)
			putLiveArtifact(t, s, "art-owner")
			putLiveArtifact(t, s, "art-other")
			if err := s.PutArtifactSources(ctx, "art-owner", []domain.ArtifactSource{
				ownershipSource("src-owned", "art-owner", "/owner"),
			}); err != nil {
				t.Fatalf("seed owner source: %v", err)
			}

			err := s.PutArtifactSources(ctx, "art-other", []domain.ArtifactSource{
				ownershipSource("src-new", "art-other", "/new"),
				ownershipSource("src-owned", "art-other", "/hijack"),
			})
			if !errors.Is(err, inventory.ErrSourceOwnershipConflict) {
				t.Fatalf("PutArtifactSources(foreign source id) err = %v, want ErrSourceOwnershipConflict", err)
			}

			got, ok, err := s.GetArtifactSource(ctx, "src-owned")
			if err != nil || !ok {
				t.Fatalf("GetArtifactSource(src-owned): ok=%v err=%v", ok, err)
			}
			if got.ArtifactID != "art-owner" || got.Location.NodeLocal == nil || got.Location.NodeLocal.Path != "/owner" {
				t.Fatalf("owned source rewritten: %+v", got)
			}
			if _, ok, _ := s.GetArtifactSource(ctx, "src-new"); ok {
				t.Fatal("conflicting call partially wrote src-new")
			}
			if list, _ := s.ListArtifactSources(ctx, "art-other"); len(list) != 0 {
				t.Fatalf("conflicting call left sources on art-other: %+v", list)
			}

			// The owner can still update its own source.
			if err := s.PutArtifactSources(ctx, "art-owner", []domain.ArtifactSource{
				ownershipSource("src-owned", "art-owner", "/owner-moved"),
			}); err != nil {
				t.Fatalf("owner update: %v", err)
			}
		})
	}
}
