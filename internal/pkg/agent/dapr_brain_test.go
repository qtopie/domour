package agent

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	appconfig "github.com/qtopie/domour/internal/app/config"
)

func TestDaprBrainClientAutopilot(t *testing.T) {
	t.Parallel()

	brainMux, err := NewInternalBrainMux()
	if err != nil {
		t.Fatalf("NewInternalBrainMux() error = %v", err)
	}

	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		prefix := "/v1.0/invoke/domour-brain/method"
		if !strings.HasPrefix(r.URL.Path, prefix) {
			http.NotFound(w, r)
			return
		}

		targetReq := r.Clone(r.Context())
		targetReq.URL.Path = strings.TrimPrefix(r.URL.Path, prefix)
		targetReq.RequestURI = targetReq.URL.Path
		brainMux.ServeHTTP(w, targetReq)
	}))
	defer sidecar.Close()

	cfg := appconfig.DomourConfig{
		Services: map[string]appconfig.ServiceConfig{
			"brain": {
				Mode:  "dapr",
				AppID: "domour-brain",
			},
		},
		Dapr: appconfig.DaprConfig{
			HTTPAddress: strings.TrimPrefix(sidecar.URL, "http://"),
		},
	}

	client, err := newDaprBrainClient(cfg)
	if err != nil {
		t.Fatalf("newDaprBrainClient() error = %v", err)
	}

	resp, err := client.Autopilot(context.Background(), BrainAutopilotRequest{
		Workspace: ".",
		Goal:      "Design a safe rollout plan for a new Dapr brain adapter.",
		MaxSteps:  4,
	})
	if err != nil {
		t.Fatalf("Autopilot() error = %v", err)
	}
	if strings.TrimSpace(resp.Content) == "" {
		t.Fatal("Autopilot() returned empty content")
	}
}

func TestDaprBrainClientStreamAutopilot(t *testing.T) {
	t.Parallel()

	brainMux, err := NewInternalBrainMux()
	if err != nil {
		t.Fatalf("NewInternalBrainMux() error = %v", err)
	}

	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		prefix := "/v1.0/invoke/domour-brain/method"
		if !strings.HasPrefix(r.URL.Path, prefix) {
			http.NotFound(w, r)
			return
		}
		targetReq := r.Clone(r.Context())
		targetReq.URL.Path = strings.TrimPrefix(r.URL.Path, prefix)
		targetReq.RequestURI = targetReq.URL.Path
		brainMux.ServeHTTP(w, targetReq)
	}))
	defer sidecar.Close()

	client, err := newDaprBrainClient(appconfig.DomourConfig{
		Services: map[string]appconfig.ServiceConfig{
			"brain": {AppID: "domour-brain"},
		},
		Dapr: appconfig.DaprConfig{
			HTTPAddress: strings.TrimPrefix(sidecar.URL, "http://"),
		},
	})
	if err != nil {
		t.Fatalf("newDaprBrainClient() error = %v", err)
	}

	stream, err := client.StreamAutopilot(context.Background(), BrainAutopilotRequest{
		Workspace: ".",
		Goal:      "Break a complex rollout into a few safe steps.",
		MaxSteps:  3,
	})
	if err != nil {
		t.Fatalf("StreamAutopilot() error = %v", err)
	}

	var gotChunk, gotDone bool
	for event := range stream {
		if event.Err != nil && event.Err != io.EOF {
			t.Fatalf("stream event error = %v", event.Err)
		}
		if event.Type == "autopilot_chunk" && strings.TrimSpace(event.Content) != "" {
			gotChunk = true
		}
		if event.Type == "autopilot_done" {
			gotDone = true
		}
	}

	if !gotChunk {
		t.Fatal("expected at least one autopilot_chunk event")
	}
	if !gotDone {
		t.Fatal("expected autopilot_done event")
	}
}
