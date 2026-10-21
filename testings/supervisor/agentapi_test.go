package supervisor_test

import (
	"context"
	"testing"
	"time"

	"github.com/qtopie/domour/ark/supervisor/providers/antigravity"
)

// TestAgentAPIClient_TypedMethods [SPEC-SUPV-015]
func TestAgentAPIClient_TypedMethods(t *testing.T) {
	client := antigravity.NewAgentAPIClient()
	if !client.IsAvailable() {
		t.Skip("AgentAPI client not available in this test environment, skipping live execution test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 1. 测试元数据读取
	// 使用一个固定测试 ID 进行探测，若会话不存在应返回明确的 JSON error 而非崩溃
	meta, err := client.GetMetadata(ctx, "test-probe-session-id")
	t.Logf("GetMetadata returned meta=%+v, err=%v", meta, err)

	// 2. 验证参数校验门禁
	if _, err := client.NewConversation(ctx, "", "", ""); err == nil {
		t.Errorf("expected error when prompt is empty")
	}
	if _, err := client.SendMessage(ctx, "", "hello", ""); err == nil {
		t.Errorf("expected error when recipientID is empty")
	}
	if _, err := client.SendMessage(ctx, "valid-id", "", ""); err == nil {
		t.Errorf("expected error when content is empty")
	}
	if _, err := client.GetMetadata(ctx, ""); err == nil {
		t.Errorf("expected error when conversationID is empty")
	}
}
