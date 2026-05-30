package agent

import (
	"testing"

	appconfig "github.com/qtopie/domour/internal/app/config"
)

func TestEstimateTokenCount(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    int
	}{
		{"English", "Hello world", 3},
		{"Chinese", "你好世界", 3},
		{"Mixed", "Hello 你好", 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EstimateTokenCount(tt.content)
			if got != tt.want {
				t.Errorf("EstimateTokenCount(%q) = %d, want %d", tt.content, got, tt.want)
			}
		})
	}
}

func TestGetModelThresholds_Fallbacks(t *testing.T) {
	cfg := appconfig.DomourConfig{}

	maxAct, trig := GetModelThresholds(cfg, "gemini", "gemini-3.5-flash")
	if maxAct != 64000 || trig != 32000 {
		t.Errorf("expected 64000/32000 for Gemini fallback, got %d/%d", maxAct, trig)
	}

	maxAct, trig = GetModelThresholds(cfg, "openai", "gpt-4o")
	if maxAct != 24000 || trig != 16000 {
		t.Errorf("expected 24000/16000 for OpenAI fallback, got %d/%d", maxAct, trig)
	}

	maxAct, trig = GetModelThresholds(cfg, "ollama", "phi4")
	if maxAct != 4000 || trig != 3000 {
		t.Errorf("expected 4000/3000 for Ollama fallback, got %d/%d", maxAct, trig)
	}

	cfg.MaxActiveTokens = 9999
	cfg.CompressTriggerTokens = 8888
	maxAct, trig = GetModelThresholds(cfg, "openai", "gpt-4o")
	if maxAct != 9999 || trig != 8888 {
		t.Errorf("expected config overrides 9999/8888, got %d/%d", maxAct, trig)
	}
}
