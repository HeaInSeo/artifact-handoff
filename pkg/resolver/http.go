package resolver

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/HeaInSeo/artifact-handoff/pkg/domain"
)

func NewHTTPHandler(service *Service) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/v1/artifacts:register", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MiB
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var req struct {
			Artifact domain.Artifact `json:"artifact"`
		}
		if err := json.NewDecoder(bytes.NewReader(body)).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		artifact := req.Artifact
		if artifact.SampleRunID == "" &&
			artifact.ProducerNodeID == "" &&
			artifact.ProducerAttemptID == "" &&
			artifact.OutputName == "" &&
			artifact.ArtifactID == "" &&
			artifact.Digest == "" &&
			artifact.LogicalURI == "" &&
			artifact.NodeName == "" &&
			artifact.URI == "" &&
			len(artifact.Locations) == 0 &&
			artifact.SizeBytes == 0 &&
			artifact.CreatedAt.IsZero() {
			// Phase-1 backward compatibility: accept the older flat artifact body as well.
			if err := json.NewDecoder(bytes.NewReader(body)).Decode(&artifact); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
		canonicalizeLegacyHTTPArtifactID(&artifact)
		state, err := service.RegisterArtifactCore(r.Context(), artifact)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]string{"availabilityState": string(state)})
	})
	mux.HandleFunc("/v1/artifacts:get", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		artifact, ok, err := service.GetArtifactCore(
			r.Context(),
			r.URL.Query().Get("sampleRunId"),
			r.URL.Query().Get("producerNodeId"),
			r.URL.Query().Get("attemptId"),
			r.URL.Query().Get("outputName"),
		)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if !ok {
			http.Error(w, "artifact not found", http.StatusNotFound)
			return
		}
		writeJSON(w, map[string]domain.Artifact{"artifact": artifact})
	})
	mux.HandleFunc("/v1/artifacts:list", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		artifacts, err := service.ListArtifactsBySampleRunCore(r.Context(), r.URL.Query().Get("sampleRunId"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string][]domain.Artifact{"artifacts": artifacts})
	})
	mux.HandleFunc("/v1/sources:list", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		sources, err := service.ListSourcesCore(r.Context(), r.URL.Query().Get("artifactId"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string][]domain.ArtifactSource{"sources": sources})
	})
	mux.HandleFunc("/v1/sources:add", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			ArtifactID string                `json:"artifactId"`
			Source     domain.ArtifactSource `json:"source"`
		}
		if err := decodeJSON(w, r, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		source, err := service.AddSourceCore(r.Context(), req.ArtifactID, req.Source)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]domain.ArtifactSource{"source": source})
	})
	mux.HandleFunc("/v1/sources:updateState", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			SourceID string             `json:"sourceId"`
			State    domain.SourceState `json:"state"`
		}
		if err := decodeJSON(w, r, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		source, err := service.UpdateSourceStateCore(r.Context(), req.SourceID, req.State)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]domain.ArtifactSource{"source": source})
	})
	mux.HandleFunc("/v1/sources:verify", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			SourceID string `json:"sourceId"`
		}
		if err := decodeJSON(w, r, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		source, verified, reason, err := service.VerifySourceCore(r.Context(), req.SourceID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]any{
			"source":   source,
			"verified": verified,
			"reason":   reason,
		})
	})
	mux.HandleFunc("/v1/handoffs:resolve", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Binding        domain.Binding `json:"binding"`
			TargetNodeName string         `json:"targetNodeName"`
		}
		if err := decodeJSON(w, r, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		resolved, err := service.ResolveHandoffCore(r.Context(), req.Binding, req.TargetNodeName)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, resolved)
	})
	mux.HandleFunc("/v1/nodes:notifyTerminal", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			SampleRunID   string `json:"sampleRunId"`
			NodeID        string `json:"nodeId"`
			AttemptID     string `json:"attemptId"`
			TerminalState string `json:"terminalState"`
		}
		if err := decodeJSON(w, r, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := service.NotifyNodeTerminalCore(r.Context(), req.SampleRunID, req.NodeID, req.AttemptID, req.TerminalState); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]bool{"accepted": true})
	})
	mux.HandleFunc("/v1/sampleRuns:finalize", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			SampleRunID string `json:"sampleRunId"`
		}
		if err := decodeJSON(w, r, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := service.FinalizeSampleRunCore(r.Context(), req.SampleRunID); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]bool{"accepted": true})
	})
	mux.HandleFunc("/v1/sampleRuns:evaluateGC", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			SampleRunID string `json:"sampleRunId"`
		}
		if err := decodeJSON(w, r, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := service.EvaluateGCCore(r.Context(), req.SampleRunID); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]bool{"accepted": true})
	})
	mux.HandleFunc("/v1/sampleRuns:lifecycle", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		sampleRunID := r.URL.Query().Get("sampleRunId")
		lifecycle, ok, err := service.GetSampleRunLifecycleCore(r.Context(), sampleRunID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if !ok {
			http.Error(w, "sample run lifecycle not found", http.StatusNotFound)
			return
		}
		writeJSON(w, lifecycle)
	})
	mux.Handle("/metrics", service.Metrics().Handler())
	return mux
}

func decodeJSON(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	return json.NewDecoder(r.Body).Decode(v)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func canonicalizeLegacyHTTPArtifactID(artifact *domain.Artifact) {
	if artifact == nil || artifact.ArtifactID == "" {
		return
	}
	if artifact.ArtifactID == artifact.CanonicalID() {
		return
	}

	legacyID := strings.Join([]string{
		artifact.SampleRunID,
		artifact.ProducerNodeID,
		artifact.OutputName,
	}, ":")
	if artifact.ArtifactID == legacyID {
		artifact.ArtifactID = artifact.CanonicalID()
	}
}
