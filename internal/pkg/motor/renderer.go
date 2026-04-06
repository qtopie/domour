package motor

import (
	"context"
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type D2Renderer struct{}

func NewD2Renderer() *D2Renderer {
	return &D2Renderer{}
}

func (m *D2Renderer) Name() string {
	return LayerName
}

func (m *D2Renderer) Act(ctx context.Context, command Command) (Result, error) {
	if command.Action != "render_d2" {
		return Result{}, fmt.Errorf("unsupported motor action %q", command.Action)
	}

	source, ok := command.Input["source"].(string)
	if !ok || strings.TrimSpace(source) == "" {
		return Result{}, fmt.Errorf("render_d2 requires non-empty source")
	}

	format, _ := command.Input["format"].(string)
	format = normalizeFormat(format)

	svg, err := renderD2ToSVG(ctx, source)
	if err != nil {
		return Result{}, err
	}

	observation := svg
	if format == "html" {
		observation = wrapHTML(svg, command)
	}

	return Result{
		CommandID:   command.ID,
		Observation: observation,
		Done:        true,
		Meta: map[string]string{
			"format": format,
		},
	}, nil
}

func normalizeFormat(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "html", "web":
		return "html"
	default:
		return "svg"
	}
}

func renderD2ToSVG(ctx context.Context, source string) (string, error) {
	dir, err := os.MkdirTemp("", "domour-d2-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)

	inputPath := filepath.Join(dir, "diagram.d2")
	outputPath := filepath.Join(dir, "diagram.svg")
	if err := os.WriteFile(inputPath, []byte(source), 0o600); err != nil {
		return "", err
	}

	cmd := exec.CommandContext(ctx, "d2", inputPath, outputPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("d2 render failed: %w: %s", err, strings.TrimSpace(string(output)))
	}

	rendered, err := os.ReadFile(outputPath)
	if err != nil {
		return "", err
	}
	return string(rendered), nil
}

func wrapHTML(svg string, command Command) string {
	title, _ := command.Input["title"].(string)
	if strings.TrimSpace(title) == "" {
		title = "Domour Diagram"
	}
	d2Source, _ := command.Input["source"].(string)

	return fmt.Sprintf(`<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>%s</title>
    <style>
      body { font-family: sans-serif; margin: 24px; background: #0f172a; color: #e2e8f0; }
      .card { background: #111827; border-radius: 16px; padding: 20px; margin-bottom: 20px; }
      pre { white-space: pre-wrap; overflow-wrap: anywhere; background: #020617; padding: 16px; border-radius: 12px; }
      .diagram { background: white; border-radius: 12px; padding: 12px; overflow: auto; }
    </style>
  </head>
  <body>
    <div class="card">
      <h1>%s</h1>
      <p>Rendered by Domour Motor using the local d2 tool.</p>
    </div>
    <div class="card">
      <h2>Diagram</h2>
      <div class="diagram">%s</div>
    </div>
    <div class="card">
      <h2>D2 Source</h2>
      <pre>%s</pre>
    </div>
  </body>
</html>`, html.EscapeString(title), html.EscapeString(title), svg, html.EscapeString(d2Source))
}
