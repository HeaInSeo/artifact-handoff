package resolver

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

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
		if artifact == (domain.Artifact{}) {
			// Phase-1 backward compatibility: accept the older flat artifact body as well.
			if err := json.NewDecoder(bytes.NewReader(body)).Decode(&artifact); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
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
	mux.HandleFunc("/v1/handoffs:resolve", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Binding        domain.Binding `json:"binding"`
			TargetNodeName string         `json:"targetNodeName"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
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
			TerminalState string `json:"terminalState"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := service.NotifyNodeTerminalCore(r.Context(), req.SampleRunID, req.NodeID, req.TerminalState); err != nil {
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
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
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
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
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

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
