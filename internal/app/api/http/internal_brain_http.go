package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/cloudwego/eino/schema"
	"github.com/qtopie/domour/internal/config/modelmanager"
	"github.com/qtopie/domour/internal/engine"
)

type DaprGenerateTextRequest struct {
	Entry    string            `json:"entry"`
	Provider string            `json:"provider,omitempty"`
	Model    string            `json:"model,omitempty"`
	Messages []*schema.Message `json:"messages"`
}

type DaprGenerateMessageRequest struct {
	Entry    string            `json:"entry"`
	Provider string            `json:"provider,omitempty"`
	Model    string            `json:"model,omitempty"`
	Messages []*schema.Message `json:"messages"`
}

type DaprBindToolsRequest struct {
	Entry string             `json:"entry"`
	Tools []*schema.ToolInfo `json:"tools"`
}

func NewInternalBrainMux(brain engine.CognitorClient) (http.Handler, error) {
	if brain == nil {
		return nil, fmt.Errorf("brain client is required")
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/internal/brain/generate-text", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req DaprGenerateTextRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("decode request: %v", err), http.StatusBadRequest)
			return
		}
		client, err := brain.GetClientWithOverride(r.Context(), req.Entry, req.Provider, req.Model)
		if err != nil {
			http.Error(w, fmt.Sprintf("resolve client: %v", err), http.StatusInternalServerError)
			return
		}
		resp, err := client.GenerateText(r.Context(), req.Messages)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("/internal/brain/generate-message", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req DaprGenerateMessageRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("decode request: %v", err), http.StatusBadRequest)
			return
		}
		client, err := brain.GetClientWithOverride(r.Context(), req.Entry, req.Provider, req.Model)
		if err != nil {
			http.Error(w, fmt.Sprintf("resolve client: %v", err), http.StatusInternalServerError)
			return
		}
		resp, err := client.GenerateMessage(r.Context(), req.Messages)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("/internal/brain/bind-tools", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req DaprBindToolsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("decode request: %v", err), http.StatusBadRequest)
			return
		}
		client, err := brain.GetClient(r.Context(), req.Entry)
		if err != nil {
			http.Error(w, fmt.Sprintf("resolve client: %v", err), http.StatusInternalServerError)
			return
		}
		if err := client.BindTools(req.Tools); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	})

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
