package llamacpp

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

// RunCLI handles the "domour llamacpp" command-line invocation.
func RunCLI(args []string) error {
	return RunCLIWithOutput(args, os.Stdout)
}

// RunCLIWithOutput allows capturing stdout for testing.
func RunCLIWithOutput(args []string, out io.Writer) error {
	if len(args) == 0 {
		printUsage(out)
		return nil
	}

	subcmd := args[0]
	subargs := args[1:]

	switch subcmd {
	case "smoke":
		return runSmokeTest(out)
	case "generate":
		return runGenerateCLI(subargs, out)
	case "chat":
		return runChatCLI(subargs, out)
	case "forward":
		return runForwardCLI(subargs, out)
	case "help", "-h", "--help":
		printUsage(out)
		return nil
	default:
		return fmt.Errorf("unknown subcommand %q. Run 'domour llamacpp help' for usage", subcmd)
	}
}

func printUsage(out io.Writer) {
	fmt.Fprintln(out, "Usage: domour llamacpp <command> [arguments]")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Commands:")
	fmt.Fprintln(out, "  smoke                Run self-contained regression smoke test")
	fmt.Fprintln(out, "  generate [flags]     Execute text generation")
	fmt.Fprintln(out, "  chat     [flags]     Execute conversational multi-turn generation")
	fmt.Fprintln(out, "  forward  [flags]     Execute single-forward logits extraction")
}

func runSmokeTest(out io.Writer) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	fmt.Fprintln(out, "[llamacpp-smoke] Initializing llama.cpp engine...")
	engine, err := NewEngine(Config{UseStub: true})
	if err != nil {
		return fmt.Errorf("smoke test failed on init: %w", err)
	}
	defer func() { _ = engine.Close() }()

	if !engine.IsReady() {
		return fmt.Errorf("smoke test failed: engine not ready")
	}

	// 1. Test ForwardLogits
	fmt.Fprintln(out, "[llamacpp-smoke] Testing ForwardLogits...")
	fwdResp, err := engine.ForwardLogits(ctx, ForwardRequest{
		Prompt:        "The quick brown fox jumps",
		MarkerIndices: []int{0, 2},
	})
	if err != nil {
		return fmt.Errorf("smoke test failed on ForwardLogits: %w", err)
	}
	if len(fwdResp.Logits) != 2 {
		return fmt.Errorf("smoke test failed on ForwardLogits: expected 2 logits entries, got %d", len(fwdResp.Logits))
	}

	// 2. Test Generate
	fmt.Fprintln(out, "[llamacpp-smoke] Testing Generate...")
	genResp, err := engine.Generate(ctx, GenerateRequest{
		Prompt:    "Hello world",
		MaxTokens: 8,
	})
	if err != nil {
		return fmt.Errorf("smoke test failed on Generate: %w", err)
	}
	if genResp.CompletionTokens <= 0 || genResp.Content == "" {
		return fmt.Errorf("smoke test failed on Generate: empty completion")
	}

	// 3. Test Stream
	fmt.Fprintln(out, "[llamacpp-smoke] Testing Stream...")
	streamCh, err := engine.Stream(ctx, GenerateRequest{
		Prompt:    "Streaming test",
		MaxTokens: 4,
	})
	if err != nil {
		return fmt.Errorf("smoke test failed on Stream: %w", err)
	}

	chunksCount := 0
	for chunk := range streamCh {
		if chunk.Err != nil {
			return fmt.Errorf("smoke test failed on Stream chunk: %w", chunk.Err)
		}
		chunksCount++
	}
	if chunksCount == 0 {
		return fmt.Errorf("smoke test failed on Stream: no chunks received")
	}

	fmt.Fprintln(out, "✅ [llamacpp-smoke] All regression checks passed successfully!")
	return nil
}

func runGenerateCLI(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("generate", flag.ContinueOnError)
	fs.SetOutput(out)

	prompt := fs.String("prompt", "", "Input prompt text")
	modelPath := fs.String("model", "", "Path to model GGUF (optional for stub)")
	maxTokens := fs.Int("tokens", 32, "Max tokens to generate")
	temp := fs.Float64("temp", 0.7, "Sampling temperature")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if strings.TrimSpace(*prompt) == "" {
		return fmt.Errorf("--prompt is required")
	}

	engine, err := NewEngine(Config{
		ModelPath: *modelPath,
		UseStub:   *modelPath == "",
	})
	if err != nil {
		return err
	}
	defer func() { _ = engine.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := engine.Generate(ctx, GenerateRequest{
		Prompt:      *prompt,
		MaxTokens:   *maxTokens,
		Temperature: float32(*temp),
	})
	if err != nil {
		return err
	}

	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(resp)
}

func runForwardCLI(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("forward", flag.ContinueOnError)
	fs.SetOutput(out)

	prompt := fs.String("prompt", "", "Input prompt text")
	markersStr := fs.String("markers", "0", "Comma-separated marker token indices (e.g. '0,2')")
	modelPath := fs.String("model", "", "Path to model GGUF (optional for stub)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if strings.TrimSpace(*prompt) == "" {
		return fmt.Errorf("--prompt is required")
	}

	var markerIndices []int
	for _, part := range strings.Split(*markersStr, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		idx, err := strconv.Atoi(part)
		if err != nil {
			return fmt.Errorf("invalid marker index %q: %w", part, err)
		}
		markerIndices = append(markerIndices, idx)
	}

	if len(markerIndices) == 0 {
		return fmt.Errorf("at least one marker index is required")
	}

	engine, err := NewEngine(Config{
		ModelPath: *modelPath,
		UseStub:   *modelPath == "",
	})
	if err != nil {
		return err
	}
	defer func() { _ = engine.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := engine.ForwardLogits(ctx, ForwardRequest{
		Prompt:        *prompt,
		MarkerIndices: markerIndices,
	})
	if err != nil {
		return err
	}

	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(resp)
}

func runChatCLI(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("chat", flag.ContinueOnError)
	fs.SetOutput(out)

	prompt := fs.String("prompt", "", "User message text")
	systemPrompt := fs.String("system", "You are a helpful assistant.", "System prompt")
	modelPath := fs.String("model", "", "Path to model GGUF (optional for stub)")
	maxTokens := fs.Int("tokens", 64, "Max tokens to generate")
	temp := fs.Float64("temp", 0.7, "Sampling temperature")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if strings.TrimSpace(*prompt) == "" {
		return fmt.Errorf("--prompt is required")
	}

	engine, err := NewEngine(Config{
		ModelPath: *modelPath,
		UseStub:   *modelPath == "",
	})
	if err != nil {
		return err
	}
	defer func() { _ = engine.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := engine.Chat(ctx, ChatRequest{
		Messages: []ChatMessage{
			{Role: "system", Content: *systemPrompt},
			{Role: "user", Content: *prompt},
		},
		MaxTokens:   *maxTokens,
		Temperature: float32(*temp),
	})
	if err != nil {
		return err
	}

	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(resp)
}
