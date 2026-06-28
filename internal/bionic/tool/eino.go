package tool

import (
	"strings"

	"github.com/cloudwego/eino/schema"
)

// SanitizeToolName maps any tool name to comply with ^[a-zA-Z0-9_-]+$ by replacing invalid characters with underscores.
func SanitizeToolName(name string) string {
	var sb strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			sb.WriteRune(r)
		} else {
			sb.WriteRune('_')
		}
	}
	return sb.String()
}

// GetEinoToolSchemas returns Eino schema definitions for all registered tools.
func GetEinoToolSchemas(tools []ToolInfo) []*schema.ToolInfo {
	var schemas []*schema.ToolInfo
	for _, t := range tools {
		var s *schema.ToolInfo
		if staticSchema := GetEinoToolSchema(t.Name); staticSchema != nil {
			// Create a copy or rebuild to avoid mutating static schema definition structures
			s = &schema.ToolInfo{
				Name:        staticSchema.Name,
				Desc:        staticSchema.Desc,
				ParamsOneOf: staticSchema.ParamsOneOf,
			}
		} else if t.Params != nil {
			s = &schema.ToolInfo{
				Name:        t.Name,
				Desc:        t.Description,
				ParamsOneOf: t.Params,
			}
		} else {
			// Fallback schema for custom/unknown tools
			s = &schema.ToolInfo{
				Name: t.Name,
				Desc: t.Description,
			}
		}
		s.Name = SanitizeToolName(s.Name)
		schemas = append(schemas, s)
	}
	return schemas
}


// GetEinoToolSchema returns a specific tool's Eino schema with detailed parameter info.
func GetEinoToolSchema(name string) *schema.ToolInfo {
	switch name {
	case "render_d2":
		return &schema.ToolInfo{
			Name: "render_d2",
			Desc: "Render D2 diagrams locally with the built-in renderer",
			ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
				"source": {
					Type:     schema.String,
					Desc:     "D2 diagram source code",
					Required: true,
				},
				"format": {
					Type:     schema.String,
					Desc:     "Output format: 'svg', 'png', or 'html'",
					Required: false,
					Enum:     []string{"svg", "png", "html"},
				},
				"title": {
					Type:     schema.String,
					Desc:     "Optional diagram title",
					Required: false,
				},
			}),
		}

	case "shell.exec":
		return &schema.ToolInfo{
			Name: "shell.exec",
			Desc: "Execute a local shell command through the motor-managed CLI runtime",
			ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
				"command": {
					Type:     schema.String,
					Desc:     "The local shell command line to execute",
					Required: true,
				},
				"dir": {
					Type:     schema.String,
					Desc:     "Optional directory to run the command in",
					Required: false,
				},
			}),
		}

	case "search.grep":
		return &schema.ToolInfo{
			Name: "search.grep",
			Desc: "Search file contents using sniphunt (regex supported)",
			ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
				"pattern": {
					Type:     schema.String,
					Desc:     "Regex/substring pattern to search for",
					Required: true,
				},
				"dir": {
					Type:     schema.String,
					Desc:     "Search root directory (defaults to workspace)",
					Required: false,
				},
				"extensions": {
					Type:     schema.String,
					Desc:     "Optional comma-separated extensions (e.g. 'go,md')",
					Required: false,
				},
			}),
		}

	case "file.edit_lines":
		return &schema.ToolInfo{
			Name: "file.edit_lines",
			Desc: "Surgically replace a range of lines in a file (1-based index)",
			ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
				"path": {
					Type:     schema.String,
					Desc:     "Path to the target file",
					Required: true,
				},
				"start_line": {
					Type:     schema.Integer,
					Desc:     "Starting line number (1-based, inclusive)",
					Required: true,
				},
				"end_line": {
					Type:     schema.Integer,
					Desc:     "Ending line number (1-based, inclusive)",
					Required: true,
				},
				"content": {
					Type:     schema.String,
					Desc:     "The new replacement content",
					Required: true,
				},
			}),
		}

	case "file.replace":
		return &schema.ToolInfo{
			Name: "file.replace",
			Desc: "Replace an exact string block in a file (fails if ambiguous)",
			ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
				"path": {
					Type:     schema.String,
					Desc:     "Path to the target file",
					Required: true,
				},
				"old": {
					Type:     schema.String,
					Desc:     "The exact string block to replace",
					Required: true,
				},
				"new": {
					Type:     schema.String,
					Desc:     "The new replacement string",
					Required: true,
				},
			}),
		}

	case "runtime.info":
		return &schema.ToolInfo{
			Name: "runtime.info",
			Desc: "Retrieve system runtime details (OS type, Architecture, CPU count, Go version, Hostname, Working directory)",
		}

	default:
		return nil
	}
}
