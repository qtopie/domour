package llm

import (
	"strings"
	"testing"
)

func TestApplyProxyEnv(t *testing.T) {
	t.Parallel()

	env := applyProxyEnv([]string{"PATH=/bin"}, "http://192.168.50.31:1080")
	joined := strings.Join(env, "\n")

	for _, expected := range []string{
		"HTTPS_PROXY=http://192.168.50.31:1080",
		"https_proxy=http://192.168.50.31:1080",
		"HTTP_PROXY=http://192.168.50.31:1080",
		"http_proxy=http://192.168.50.31:1080",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("applyProxyEnv() missing %q in %q", expected, joined)
		}
	}
}
