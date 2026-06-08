package runtime

import (
	"context"
	"path/filepath"
	"testing"
)

func TestManagerPrepareCreatesStableRuntime(t *testing.T) {
	root := t.TempDir()
	manager := NewManager(root)

	first, err := manager.Prepare("github-copilot-cli", "session-a", "")
	if err != nil {
		t.Fatalf("prepare runtime: %v", err)
	}
	second, err := manager.Prepare("github-copilot-cli", "session-a", "")
	if err != nil {
		t.Fatalf("prepare runtime second time: %v", err)
	}

	if first != second {
		t.Fatalf("expected same runtime pointer for repeated session mapping")
	}
	if first.RuntimeDir != filepath.Join(root, "github-copilot-cli", "session-a") {
		t.Fatalf("unexpected runtime dir: %s", first.RuntimeDir)
	}
	if first.Workspace == "" {
		t.Fatalf("expected workspace to be populated")
	}
}

func TestRequestMetadataRoundTrip(t *testing.T) {
	ctx := WithRequestMetadata(context.Background(), RequestMetadata{
		SessionID: "s1",
		Workspace: "/tmp/work",
		Mode:      "stealth",
	})

	meta := RequestMetadataFromContext(ctx)
	if meta.SessionID != "s1" || meta.Workspace != "/tmp/work" || meta.Mode != "stealth" {
		t.Fatalf("unexpected metadata: %+v", meta)
	}
}
