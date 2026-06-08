package tool

import (
	"context"
	"fmt"
	"html"
	"strings"

	"oss.terrastruct.com/d2/d2graph"
	"oss.terrastruct.com/d2/d2layouts/d2dagrelayout"
	"oss.terrastruct.com/d2/d2lib"
	"oss.terrastruct.com/d2/d2renderers/d2svg"
	"oss.terrastruct.com/d2/d2themes/d2themescatalog"
	"oss.terrastruct.com/d2/lib/log"
	"oss.terrastruct.com/d2/lib/textmeasure"
	"oss.terrastruct.com/util-go/go2"
)

type D2Renderer struct{}

func NewD2Renderer() *D2Renderer {
	return &D2Renderer{}
}

func (m *D2Renderer) Name() string {
	return "motor"
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
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		return "", fmt.Errorf("create ruler failed: %w", err)
	}

	layoutResolver := func(engine string) (d2graph.LayoutGraph, error) {
		return d2dagrelayout.DefaultLayout, nil
	}

	renderOpts := &d2svg.RenderOpts{
		Pad:     go2.Pointer(int64(5)),
		ThemeID: &d2themescatalog.NeutralDefault.ID,
	}

	compileOpts := &d2lib.CompileOptions{
		LayoutResolver: layoutResolver,
		Ruler:          ruler,
	}

	lctx := log.WithDefault(ctx)
	diagram, _, err := d2lib.Compile(lctx, source, compileOpts, renderOpts)
	if err != nil {
		return "", fmt.Errorf("compile d2 source failed: %w", err)
	}

	svgBytes, err := d2svg.Render(diagram, renderOpts)
	if err != nil {
		return "", fmt.Errorf("render d2 svg failed: %w", err)
	}

	return string(svgBytes), nil
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
