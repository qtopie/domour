package tool

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/cloudwego/eino/schema"
)

// --------------------------------------------------------------------------
// Agentic CLI definitions — each entry describes an external AI CLI binary
// that can be called as a tool by the main LLM.
// --------------------------------------------------------------------------

// agenticCLIDef describes a single external AI CLI tool.
type agenticCLIDef struct {
	name    string   // display name used in the LLM enum
	binary  string   // primary binary name (e.g. "gemini")
	aliases []string // fallback binary names to try in order
	desc    string   // description shown to LLM for choosing this CLI
	args    func(prompt, model, workspace string) []string
}

// agenticCLIDefs is the canonical registry of all supported external AI CLIs.
var agenticCLIDefs = []agenticCLIDef{
	{
		name:    "gemini",
		binary:  "gemini",
		aliases: nil,
		desc:    "Google Gemini CLI: complex reasoning, math, code generation, and analysis",
		args: func(prompt, model, workspace string) []string {
			args := []string{"--prompt", prompt, "--output-format", "text", "--yolo", "--skip-trust"}
			if model != "" {
				args = append([]string{"--model", model}, args...)
			}
			if workspace != "" {
				args = append(args, "--include-directories", workspace)
			}
			return args
		},
	},
	{
		name:    "copilot",
		binary:  "github-copilot-cli",
		aliases: []string{"copilot"},
		desc:    "GitHub Copilot CLI: code review, project understanding, code generation, and shell tasks",
		args: func(prompt, model, workspace string) []string {
			args := []string{"--prompt", prompt, "--allow-all", "--output-format", "text", "--silent"}
			if model != "" {
				args = append([]string{"--model", model}, args...)
			}
			return args
		},
	},
	{
		name:    "claude",
		binary:  "claude",
		aliases: []string{"claude-code"},
		desc:    "Anthropic Claude CLI: general-purpose AI assistant with strong reasoning and coding abilities",
		args: func(prompt, model, workspace string) []string {
			args := []string{"--print", "--dangerously-skip-permissions"}
			if model != "" {
				args = append(args, "--model", model)
			}
			if workspace != "" {
				args = append(args, "--add-dir", workspace)
			}
			args = append(args, prompt)
			return args
		},
	},
	{
		name:    "agy",
		binary:  "agy",
		aliases: nil,
		desc:    "Antigravity CLI: AGY ecosystem agent with skills support",
		args: func(prompt, model, workspace string) []string {
			args := []string{"--print", prompt, "--dangerously-skip-permissions"}
			if workspace != "" {
				args = append(args, "--add-dir", workspace)
			}
			return args
		},
	},
	{
		name:    "qoder",
		binary:  "qodercli",
		aliases: []string{"qoder"},
		desc:    "Qoder CLI: code generation and analysis agent",
		args: func(prompt, model, workspace string) []string {
			args := []string{"-p", prompt, "-f", "text", "-q"}
			if workspace != "" {
				args = append(args, "--workspace", workspace)
			}
			if model != "" {
				args = append(args, "--model", model)
			}
			return args
		},
	},
}

// --------------------------------------------------------------------------
// Binary resolution — finds the actual installed CLI binary, checking PATH
// and the Domour tools directory (~/.domour/tools/node_modules/.bin/).
// --------------------------------------------------------------------------

func resolveAgentBinary(def agenticCLIDef) (string, error) {
	candidates := []string{def.binary}
	candidates = append(candidates, def.aliases...)

	for _, name := range candidates {
		if path, err := exec.LookPath(name); err == nil && path != "" {
			return name, nil
		}
		// Also check the Domour tools directory (npm global install fallback)
		if homeDir, err := os.UserHomeDir(); err == nil {
			localPath := filepath.Join(homeDir, ".domour", "tools", "node_modules", ".bin", name)
			if _, err := os.Stat(localPath); err == nil {
				return localPath, nil
			}
		}
	}

	return "", fmt.Errorf("%s not found (tried: %s)", def.name, strings.Join(candidates, ", "))
}

// findInstalledCLIs returns only the CLIs that are actually installed.
var (
	installedCLIs     []agenticCLIDef
	installedCLIOnce  sync.Once
)

func findInstalledCLIs() []agenticCLIDef {
	installedCLIOnce.Do(func() {
		for _, def := range agenticCLIDefs {
			if _, err := resolveAgentBinary(def); err == nil {
				installedCLIs = append(installedCLIs, def)
			}
		}
	})
	return installedCLIs
}

// resetInstalledCLICache is exposed for testing.
func resetInstalledCLICache() {
	installedCLIOnce = sync.Once{}
	installedCLIs = nil
}

// --------------------------------------------------------------------------
// Schema — builds the LLM tool-call schema with a dynamic enum of only
// the CLIs that are actually installed on the current machine.
// --------------------------------------------------------------------------

func buildAgenticCLISchema() *schema.ParamsOneOf {
	available := findInstalledCLIs()
	names := make([]string, len(available))
	descParts := make([]string, 0, len(available)+2)
	descParts = append(descParts, "Delegate a task to an external AI CLI tool. Choose the right CLI based on the task type:")
	for i, cli := range available {
		names[i] = cli.name
		descParts = append(descParts, fmt.Sprintf("  - %s: %s", cli.name, cli.desc))
	}
	if len(available) == 0 {
		descParts = append(descParts, "  (no external AI CLI tools detected on this machine)")
	}

	enumDesc := strings.Join(descParts, "\n")

	return schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
		"cli": {
			Type:     schema.String,
			Desc:     enumDesc,
			Required: true,
			Enum:     names,
		},
		"prompt": {
			Type:     schema.String,
			Desc:     "The task or question to delegate to the chosen AI CLI",
			Required: true,
		},
		"model": {
			Type:     schema.String,
			Desc:     "Optional: specific model to use (e.g. 'gemini-2.5-pro', 'claude-sonnet-4-20250514')",
			Required: false,
		},
		"workspace": {
			Type:     schema.String,
			Desc:     "Optional: workspace or project directory to provide context to the CLI",
			Required: false,
		},
	})
}

// --------------------------------------------------------------------------
// Tool registration
// --------------------------------------------------------------------------

// NewAgenticCLITool creates a ToolSpec that lets the main LLM delegate tasks
// to any installed external AI CLI (gemini, copilot, claude, agy, qoder).
// The available CLIs are detected dynamically at registration time, so the
// LLM only sees CLIs that are actually installed on the machine.
func NewAgenticCLITool() ToolSpec {
	available := findInstalledCLIs()
	names := make([]string, len(available))
	for i, cli := range available {
		names[i] = cli.name
	}

	availableDesc := "none"
	if len(names) > 0 {
		availableDesc = strings.Join(names, ", ")
	}

	desc := fmt.Sprintf(
		"Delegate a task to an installed external AI CLI tool. Available: %s. Each CLI has different strengths — choose wisely based on the task.",
		availableDesc,
	)

	return ToolSpec{
		Name:        "agentic_cli",
		Kind:        ToolKindInternal,
		Description: desc,
		Params:      buildAgenticCLISchema(),
		Load: func(ctx context.Context) (ToolRuntime, error) {
			return &agenticCLIRuntime{}, nil
		},
	}
}

// --------------------------------------------------------------------------
// Runtime — executes the chosen CLI with the given prompt
// --------------------------------------------------------------------------

type agenticCLIRuntime struct{}

func (r *agenticCLIRuntime) Invoke(ctx context.Context, command Command) (Result, error) {
	cliName, _ := command.Input["cli"].(string)
	if cliName == "" {
		return Result{}, fmt.Errorf("agentic_cli: 'cli' input is required")
	}

	prompt, _ := command.Input["prompt"].(string)
	if prompt == "" {
		return Result{}, fmt.Errorf("agentic_cli: 'prompt' input is required")
	}

	model, _ := command.Input["model"].(string)
	workspace, _ := command.Input["workspace"].(string)

	// Look up the CLI definition
	var def *agenticCLIDef
	for i := range agenticCLIDefs {
		if agenticCLIDefs[i].name == cliName {
			def = &agenticCLIDefs[i]
			break
		}
	}
	if def == nil {
		return Result{}, fmt.Errorf("agentic_cli: unknown CLI %q", cliName)
	}

	// Resolve the binary path (resolve again at runtime — the binary may have
	// been installed after the tool was registered)
	binary, err := resolveAgentBinary(*def)
	if err != nil {
		return Result{}, fmt.Errorf("agentic_cli: %s is not installed: %w", cliName, err)
	}

	// Build arguments using the CLI's own argument builder
	args := def.args(prompt, model, workspace)

	cmd := exec.CommandContext(ctx, binary, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return Result{}, fmt.Errorf("agentic_cli(%s): %w: %s", cliName, err, strings.TrimSpace(string(output)))
	}

	return Result{
		CommandID:   firstNonEmpty(strings.TrimSpace(command.ID), command.Action),
		Observation: string(output),
		Done:        true,
		Meta: map[string]string{
			"agent":          cliName,
			"agentic_cli":    cliName,
			"agentic_binary": binary,
		},
	}, nil
}

func (r *agenticCLIRuntime) Close(ctx context.Context) error {
	return nil
}
