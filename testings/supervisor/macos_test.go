package supervisor_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/qtopie/domour/ark/supervisor/providers/antigravity"
	"github.com/qtopie/domour/ark/supervisor/providers/chatgpt"
)

// SPEC-SUPV-022: macOS Application Bundle 与语言服务器探测
func TestAntigravity_MacOSPathsDiscovery(t *testing.T) {
	// Create a temporary mock macOS home and Application directory structure
	tmpDir := t.TempDir()
	mockAppDir := filepath.Join(tmpDir, "Applications", "Antigravity.app")
	mockMacOSBin := filepath.Join(mockAppDir, "Contents", "MacOS", "Antigravity")
	mockLSBin := filepath.Join(mockAppDir, "Contents", "Resources", "app", "resources", "bin", "language_server")

	if err := os.MkdirAll(filepath.Dir(mockMacOSBin), 0755); err != nil {
		t.Fatalf("failed to create mock MacOS dir: %v", err)
	}
	if err := os.WriteFile(mockMacOSBin, []byte("#!/bin/sh\nexit 0"), 0755); err != nil {
		t.Fatalf("failed to write mock MacOS bin: %v", err)
	}

	if err := os.MkdirAll(filepath.Dir(mockLSBin), 0755); err != nil {
		t.Fatalf("failed to create mock LS dir: %v", err)
	}
	if err := os.WriteFile(mockLSBin, []byte("#!/bin/sh\nexit 0"), 0755); err != nil {
		t.Fatalf("failed to write mock LS bin: %v", err)
	}

	// Override HOME to tmpDir to simulate user ~/Applications/...
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	origAgentAPI := os.Getenv("ANTIGRAVITY_AGENTAPI_EXE")
	os.Unsetenv("ANTIGRAVITY_AGENTAPI_EXE")
	defer func() {
		if origAgentAPI != "" {
			os.Setenv("ANTIGRAVITY_AGENTAPI_EXE", origAgentAPI)
		}
	}()

	// Test FindIDEExecutable discovers the macOS binary
	idePath, ok := antigravity.FindIDEExecutableForOS("darwin")
	if !ok || idePath != mockMacOSBin {
		t.Errorf("FindIDEExecutableForOS(\"darwin\") on macOS = (%q, %v), want (%q, true)", idePath, ok, mockMacOSBin)
	}

	// Test FindAgentAPI discovers the language_server in the app bundle
	lsPath, isSubcmd, ok := antigravity.FindAgentAPIForOS("darwin")
	if !ok || !isSubcmd || lsPath != mockLSBin {
		t.Errorf("FindAgentAPIForOS(\"darwin\") on macOS = (%q, isSubcmd=%v, ok=%v), want (%q, isSubcmd=true, ok=true)", lsPath, isSubcmd, ok, mockLSBin)
	}
}

// SPEC-SUPV-023: Darwin 无 /proc 环境下的活跃进程与 PID 自动发现
func TestDarwin_ProcessDiscoveryFallback(t *testing.T) {
	// 1. Verify ParsePOSIXProcessOutput parses standard POSIX `ps -eo pid,command` lines correctly
	psOutput := `  PID COMMAND
    1 /sbin/init
 1234 /Applications/Codex.app/Contents/MacOS/codex --app-server
 5678 /usr/local/bin/agy run
 9999 /opt/Antigravity/resources/bin/language_server
`
	codexPIDs := chatgpt.ParsePOSIXProcesses(psOutput, "codex")
	if len(codexPIDs) != 1 || codexPIDs[0] != 1234 {
		t.Errorf("ParsePOSIXProcesses for codex: got %v, want [1234]", codexPIDs)
	}

	agyPIDs := antigravity.ParsePOSIXProcesses(psOutput)
	if len(agyPIDs) != 2 {
		t.Errorf("ParsePOSIXProcesses for antigravity: got %v (len %d), want 2 entries (5678, 9999)", agyPIDs, len(agyPIDs))
	}

	// 2. Call FindRunningPIDsFallback directly and ensure it does not panic or error
	results := chatgpt.FindRunningPIDs()
	// Must return a non-nil map (even if empty in isolated sandbox test environment)
	if results == nil {
		t.Errorf("FindRunningPIDs() returned nil map")
	}

	// 3. Call DiscoverRunningSessions and ensure no error or panic in non-/proc environment
	sessions, err := antigravity.DiscoverRunningSessions()
	if err != nil {
		t.Errorf("DiscoverRunningSessions() returned unexpected error: %v", err)
	}
	if sessions == nil {
		t.Errorf("DiscoverRunningSessions() returned nil slice")
	}
}
