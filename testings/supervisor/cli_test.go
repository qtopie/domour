package supervisor_test

import (
	"strings"
	"testing"

	"github.com/qtopie/domour/ark/supervisor"
	"github.com/qtopie/domour/ark/supervisor/providers/antigravity"
	"github.com/qtopie/domour/ark/supervisor/providers/generic"
)

// TestSupervisor_CLI [SPEC-SUPV-007]
func TestSupervisor_CLI(t *testing.T) {
	agyProvider := antigravity.NewProvider()
	genProvider := generic.NewProvider()

	// 1. 验证 Antigravity FormatInstruction 格式
	agyData, err := agyProvider.FormatInstruction("test prompt")
	if err != nil {
		t.Fatalf("failed to format agy instruction: %v", err)
	}
	agyStr := string(agyData)
	if !strings.Contains(agyStr, `"event":"user"`) || !strings.Contains(agyStr, `"content":"test prompt"`) {
		t.Errorf("unexpected agy instruction format: %s", agyStr)
	}

	// 2. 验证 Generic FormatInstruction 格式
	genData, err := genProvider.FormatInstruction("test prompt")
	if err != nil {
		t.Fatalf("failed to format generic instruction: %v", err)
	}
	genStr := string(genData)
	if !strings.Contains(genStr, `"role":"user"`) || !strings.Contains(genStr, `"content":"test prompt"`) {
		t.Errorf("unexpected generic instruction format: %s", genStr)
	}

	// 3. 验证 Headless 参数构造
	headlessArgs, err := agyProvider.BuildHeadlessArgs(supervisor.LaunchOptions{
		Workspace:                  "/test/dir",
		DangerouslySkipPermissions: true,
	})
	if err != nil {
		t.Fatalf("failed to build headless args: %v", err)
	}
	joined := strings.Join(headlessArgs, " ")
	if !strings.Contains(joined, "stream-json") || !strings.Contains(joined, "--dangerously-skip-permissions") {
		t.Errorf("unexpected headless args: %s", joined)
	}

	// 4. 验证 Resume 参数构造
	resumeArgs, err := agyProvider.BuildResumeArgs("sess-456", "继续下一步", supervisor.LaunchOptions{
		Workspace: "/test/dir",
	})
	if err != nil {
		t.Fatalf("failed to build resume args: %v", err)
	}
	if resumeArgs[0] != "--conversation" || resumeArgs[1] != "sess-456" {
		t.Errorf("unexpected resume args: %v", resumeArgs)
	}
}
