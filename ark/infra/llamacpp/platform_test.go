package llamacpp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// SPEC-LLAMA-008: Darwin / macOS CGo Build & Linking Isolation
func TestCGo_PlatformBuildDirectives(t *testing.T) {
	darwinFile := "runner_cgo_darwin.go"
	linuxFile := "runner_cgo_linux.go"

	// 1. Verify runner_cgo_darwin.go
	darwinData, err := os.ReadFile(darwinFile)
	if err != nil {
		t.Fatalf("Expected %s to exist for macOS support: %v", darwinFile, err)
	}
	darwinContent := string(darwinData)

	if !strings.Contains(darwinContent, "//go:build llamacpp && darwin") {
		t.Errorf("runner_cgo_darwin.go must contain '//go:build llamacpp && darwin'")
	}
	if !strings.Contains(darwinContent, "-framework Metal") {
		t.Errorf("runner_cgo_darwin.go must link with Apple Metal framework (-framework Metal)")
	}
	if !strings.Contains(darwinContent, "-framework Accelerate") {
		t.Errorf("runner_cgo_darwin.go must link with Apple Accelerate framework (-framework Accelerate)")
	}
	if !strings.Contains(darwinContent, "/opt/homebrew") {
		t.Errorf("runner_cgo_darwin.go should include Homebrew paths (/opt/homebrew)")
	}
	if strings.Contains(darwinContent, "/home/qtopierw") || strings.Contains(darwinContent, "/root/llama") {
		t.Errorf("runner_cgo_darwin.go must NOT contain hardcoded Linux user paths")
	}

	// 2. Verify runner_cgo_linux.go
	linuxData, err := os.ReadFile(linuxFile)
	if err != nil {
		t.Fatalf("Expected %s to exist for Linux support: %v", linuxFile, err)
	}
	linuxContent := string(linuxData)

	if !strings.Contains(linuxContent, "//go:build llamacpp && linux") {
		t.Errorf("runner_cgo_linux.go must contain '//go:build llamacpp && linux'")
	}

	// 3. Verify no lingering ambiguous runner_cgo.go
	if _, err := os.Stat("runner_cgo.go"); err == nil {
		t.Errorf("runner_cgo.go should be separated into runner_cgo_linux.go and runner_cgo_darwin.go to prevent platform collision")
	}

	// 4. Verify pure-Go stub exists for default builds
	stubData, err := os.ReadFile(filepath.Join(".", "runner_stub.go"))
	if err != nil {
		t.Fatalf("runner_stub.go should exist: %v", err)
	}
	if !strings.Contains(string(stubData), "//go:build !llamacpp") {
		t.Errorf("runner_stub.go must contain '//go:build !llamacpp'")
	}
}
