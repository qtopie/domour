package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/qtopie/domour/internal/app/modelmanager"
)

func NewInternalBrainMux() (http.Handler, error) {
	brain, err := newLocalBrainClient()
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/internal/brain/chat-reply", decodeAndHandle(brain.ChatReply))
	mux.HandleFunc("/internal/brain/plan-diagram", decodeAndHandle(brain.PlanDiagram))
	mux.HandleFunc("/internal/brain/copilot", decodeAndHandle(brain.Copilot))
	mux.HandleFunc("/internal/brain/autopilot", decodeAndHandle(brain.Autopilot))
	mux.HandleFunc("/internal/brain/models/discover", decodeAndHandle(modelmanager.Discover))
	mux.HandleFunc("/internal/brain/models/set", decodeAndHandle(modelmanager.SetModel))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	return mux, nil
}

func decodeAndHandle[Req any, Resp any](handler func(rctx context.Context, req Req) (Resp, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req Req
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("decode request: %v", err), http.StatusBadRequest)
			return
		}

		resp, err := handler(r.Context(), req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			http.Error(w, fmt.Sprintf("encode response: %v", err), http.StatusInternalServerError)
		}
	}
}
