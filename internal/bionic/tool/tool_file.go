package tool

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// NewFileEditLinesTool returns a ToolSpec for the 'file.edit_lines' internal tool.
func NewFileEditLinesTool() ToolSpec {
	return NewInternalTool("file.edit_lines", "Surgically replace a range of lines in a file (1-based index)", func(ctx context.Context, command Command) (Result, error) {
		path, ok := command.Input["path"].(string)
		if !ok || strings.TrimSpace(path) == "" {
			return Result{}, fmt.Errorf("path is required for file.edit_lines")
		}

		startLineRaw, _ := command.Input["start_line"]
		endLineRaw, _ := command.Input["end_line"]
		newContent, ok := command.Input["content"].(string)
		if !ok {
			return Result{}, fmt.Errorf("content is required for file.edit_lines")
		}

		startLine := toInt(startLineRaw)
		endLine := toInt(endLineRaw)

		if startLine < 1 || endLine < 1 || startLine > endLine {
			return Result{}, fmt.Errorf("invalid line range: %v-%v", startLineRaw, endLineRaw)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return Result{}, fmt.Errorf("failed to read file: %w", err)
		}

		lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
		if startLine > len(lines) {
			return Result{}, fmt.Errorf("start_line %d is beyond file length %d", startLine, len(lines))
		}
		if endLine > len(lines) {
			endLine = len(lines)
		}

		newLines := strings.Split(strings.ReplaceAll(newContent, "\r\n", "\n"), "\n")

		// splice: lines[:start-1] + newLines + lines[end:]
		finalLines := make([]string, 0, len(lines)-(endLine-startLine+1)+len(newLines))
		finalLines = append(finalLines, lines[:startLine-1]...)
		finalLines = append(finalLines, newLines...)
		finalLines = append(finalLines, lines[endLine:]...)

		err = os.WriteFile(path, []byte(strings.Join(finalLines, "\n")), 0644)
		if err != nil {
			return Result{}, fmt.Errorf("failed to write file: %w", err)
		}

		return Result{
			Observation: fmt.Sprintf("Successfully replaced lines %d to %d in %s.", startLine, endLine, path),
			Done:        true,
			Meta: map[string]string{
				"path":       path,
				"start_line": fmt.Sprintf("%d", startLine),
				"end_line":   fmt.Sprintf("%d", endLine),
			},
		}, nil
	})
}

// NewFileReplaceTool returns a ToolSpec for the 'file.replace' internal tool.
func NewFileReplaceTool() ToolSpec {
	return NewInternalTool("file.replace", "Replace an exact string block in a file (fails if ambiguous)", func(ctx context.Context, command Command) (Result, error) {
		path, ok := command.Input["path"].(string)
		if !ok || strings.TrimSpace(path) == "" {
			return Result{}, fmt.Errorf("path is required for file.replace")
		}

		oldStr, ok := command.Input["old"].(string)
		if !ok || oldStr == "" {
			return Result{}, fmt.Errorf("old string is required for file.replace")
		}

		newStr, ok := command.Input["new"].(string)
		if !ok {
			return Result{}, fmt.Errorf("new string is required for file.replace")
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return Result{}, fmt.Errorf("failed to read file: %w", err)
		}

		content := string(data)
		count := strings.Count(content, oldStr)
		if count == 0 {
			return Result{}, fmt.Errorf("string to replace not found in file")
		}
		if count > 1 {
			return Result{}, fmt.Errorf("string to replace found %d times; please provide more context to be unique", count)
		}

		newContent := strings.Replace(content, oldStr, newStr, 1)
		err = os.WriteFile(path, []byte(newContent), 0644)
		if err != nil {
			return Result{}, fmt.Errorf("failed to write file: %w", err)
		}

		return Result{
			Observation: fmt.Sprintf("Successfully replaced exact string block in %s.", path),
			Done:        true,
			Meta: map[string]string{
				"path": path,
			},
		}, nil
	})
}

func toInt(v interface{}) int {
	switch val := v.(type) {
	case int:
		return val
	case int32:
		return int(val)
	case int64:
		return int(val)
	case float32:
		return int(val)
	case float64:
		return int(val)
	default:
		return 0
	}
}
