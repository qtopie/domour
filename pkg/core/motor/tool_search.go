package motor

import (
	"context"
	"fmt"
	"strings"

	"github.com/qtopie/sniphunt/pkg/search"
)

// NewSearchGrepTool returns a ToolSpec for the 'search.grep' internal tool.
func NewSearchGrepTool() ToolSpec {
	return NewInternalTool("search.grep", "Search file contents using sniphunt (regex supported)", func(ctx context.Context, command Command) (Result, error) {
		pattern, ok := command.Input["pattern"].(string)
		if !ok || strings.TrimSpace(pattern) == "" {
			return Result{}, fmt.Errorf("pattern is required for search.grep")
		}

		dir, _ := command.Input["dir"].(string)
		dir = strings.TrimSpace(dir)
		if dir == "" {
			dir = strings.TrimSpace(command.Meta["workspace"])
		}
		if dir == "" {
			dir = "."
		}

		s := search.NewSearcher()
		// Optional: parse extensions from input if needed
		if extRaw, ok := command.Input["extensions"]; ok {
			if exts, ok := extRaw.([]string); ok {
				s.Extensions = exts
			} else if extStr, ok := extRaw.(string); ok {
				s.Extensions = strings.Split(extStr, ",")
			}
		}

		matchChan, errChan := s.Search(ctx, dir, pattern)

		var results []string
		count := 0
		const maxResults = 100

		for {
			select {
			case <-ctx.Done():
				return Result{}, ctx.Err()
			case err, ok := <-errChan:
				if ok && err != nil {
					return Result{}, fmt.Errorf("search failed: %w", err)
				}
				if !ok {
					errChan = nil
				}
			case match, ok := <-matchChan:
				if !ok {
					matchChan = nil
				} else {
					if count < maxResults {
						results = append(results, fmt.Sprintf("%s:%d: %s", match.Path, match.LineNum, strings.TrimSpace(string(match.Text))))
						count++
					}
				}
			}

			if matchChan == nil && errChan == nil {
				break
			}
		}

		observation := strings.Join(results, "\n")
		if observation == "" {
			observation = "No matches found."
		} else if count >= maxResults {
			observation += fmt.Sprintf("\n... (showing first %d results)", maxResults)
		}

		return Result{
			Observation: observation,
			Done:        true,
			Meta: map[string]string{
				"dir":     dir,
				"pattern": pattern,
				"count":   fmt.Sprintf("%d", count),
			},
		}, nil
	})
}
