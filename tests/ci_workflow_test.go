package tests_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// SPEC-CI-001: Workflow 配置文件完备性与语法合规
func TestCIWorkflowFile_StructureAndSteps(t *testing.T) {
	workflowPath := filepath.Join("..", ".github", "workflows", "ci.yml")
	data, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("Failed to read workflow file at %s: %v", workflowPath, err)
	}

	var parsed map[string]interface{}
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Workflow YAML is invalid: %v", err)
	}

	// Verify triggers
	rawOn, ok := parsed["on"]
	if !ok {
		t.Fatalf("Missing 'on' trigger in CI workflow")
	}

	// Triggers can be a map or list
	triggerStr := ""
	switch v := rawOn.(type) {
	case map[string]interface{}:
		for k := range v {
			triggerStr += k + " "
		}
	}

	if !strings.Contains(triggerStr, "push") || !strings.Contains(triggerStr, "pull_request") {
		t.Errorf("Expected push and pull_request triggers, got: %v", triggerStr)
	}

	// Verify jobs
	jobs, ok := parsed["jobs"].(map[string]interface{})
	if !ok || len(jobs) == 0 {
		t.Fatalf("Missing or invalid 'jobs' block in CI workflow")
	}

	// Check for verification and build jobs
	if _, ok := jobs["verify"]; !ok && jobs["verification"] == nil {
		t.Errorf("Expected 'verify' or 'verification' job in workflow, found: %v", jobs)
	}
	if _, ok := jobs["build"]; !ok {
		t.Errorf("Expected 'build' job in workflow, found: %v", jobs)
	}
}

// SPEC-CI-002: Harness 与全量测试脚本执行链路完整
func TestLocalVerificationChain(t *testing.T) {
	cmd := exec.Command("./scripts/check.sh")
	cmd.Dir = ".."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("scripts/check.sh failed: %v\nOutput:\n%s", err, string(out))
	}
}

// SPEC-CI-003: 二进制编译与入口可执行性
func TestBinaryBuildAndHelpExecution(t *testing.T) {
	tmpBin := filepath.Join(t.TempDir(), "domour")
	buildCmd := exec.Command("go", "build", "-o", tmpBin, "./cmd")
	buildCmd.Dir = ".."
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to compile cmd: %v\nOutput:\n%s", err, string(out))
	}

	runCmd := exec.Command(tmpBin, "--help")
	out, err := runCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to run binary --help: %v\nOutput:\n%s", err, string(out))
	}

	if !strings.Contains(string(out), "Welcome to Domour Local CLI!") {
		t.Errorf("Expected help banner, got:\n%s", string(out))
	}
}
