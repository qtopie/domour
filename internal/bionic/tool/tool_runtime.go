package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
)

// NewRuntimeInfoTool returns a ToolSpec for the 'runtime.info' internal tool.
func NewRuntimeInfoTool() ToolSpec {
	return NewInternalTool("runtime.info", "Retrieve system runtime details (OS type, Architecture, CPU count, Go version, Hostname, Working directory)", func(ctx context.Context, command Command) (Result, error) {
		hostname, _ := os.Hostname()
		cwd, _ := os.Getwd()

		info := map[string]interface{}{
			"os":          runtime.GOOS,
			"arch":        runtime.GOARCH,
			"cpus":        runtime.NumCPU(),
			"go_version":  runtime.Version(),
			"hostname":    hostname,
			"working_dir": cwd,
		}

		outputBytes, err := json.MarshalIndent(info, "", "  ")
		if err != nil {
			return Result{}, fmt.Errorf("failed to marshal system info: %w", err)
		}

		return Result{
			Observation: string(outputBytes),
			Done:        true,
			Meta: map[string]string{
				"os":   runtime.GOOS,
				"arch": runtime.GOARCH,
			},
		}, nil
	})
}
