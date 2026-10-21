package chatgpt

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/qtopie/domour/ark/supervisor"
)

// DefaultDBPath 返回系统默认的 Codex / ChatGPT SQLite 数据库路径
func DefaultDBPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.Getenv("HOME")
	}
	return filepath.Join(home, ".codex", "state_5.sqlite")
}

// QueryThreads 从指定 SQLite 数据库中查询 threads 记录
func QueryThreads(dbPath string) ([]ThreadRecord, error) {
	if dbPath == "" {
		dbPath = DefaultDBPath()
	}

	if _, err := os.Stat(dbPath); err != nil {
		return nil, fmt.Errorf("sqlite database not found at %s: %w", dbPath, err)
	}

	// 优先使用系统 sqlite3 CLI 读取 JSON
	sqliteBin, err := exec.LookPath("sqlite3")
	if err != nil {
		return nil, fmt.Errorf("sqlite3 executable not found in PATH")
	}

	query := `SELECT id, rollout_path, created_at, updated_at, source, model_provider, cwd, title, sandbox_policy, approval_mode, tokens_used, model, first_user_message FROM threads ORDER BY updated_at DESC LIMIT 50;`
	cmd := exec.Command(sqliteBin, "-json", dbPath, query)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to query sqlite database: %w", err)
	}

	var rawList []struct {
		ID               string `json:"id"`
		RolloutPath      string `json:"rollout_path"`
		CreatedAt        int64  `json:"created_at"`
		UpdatedAt        int64  `json:"updated_at"`
		Source           string `json:"source"`
		ModelProvider    string `json:"model_provider"`
		CWD              string `json:"cwd"`
		Title            string `json:"title"`
		SandboxPolicy    string `json:"sandbox_policy"`
		ApprovalMode     string `json:"approval_mode"`
		TokensUsed       int    `json:"tokens_used"`
		Model            string `json:"model"`
		FirstUserMessage string `json:"first_user_message"`
	}

	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" || trimmed == "[]" {
		return nil, nil
	}

	if err := json.Unmarshal([]byte(trimmed), &rawList); err != nil {
		return nil, fmt.Errorf("failed to unmarshal sqlite json result: %w", err)
	}

	results := make([]ThreadRecord, 0, len(rawList))
	for _, r := range rawList {
		results = append(results, ThreadRecord{
			ID:               r.ID,
			RolloutPath:      r.RolloutPath,
			CreatedAt:        r.CreatedAt,
			UpdatedAt:        r.UpdatedAt,
			Source:           r.Source,
			ModelProvider:    r.ModelProvider,
			CWD:              r.CWD,
			Title:            r.Title,
			SandboxPolicy:    r.SandboxPolicy,
			ApprovalMode:     r.ApprovalMode,
			TokensUsed:       r.TokensUsed,
			Model:            r.Model,
			FirstUserMessage: r.FirstUserMessage,
			LastUpdated:      time.Unix(r.UpdatedAt, 0),
		})
	}

	return results, nil
}

// FindRunningPIDs 扫描 /proc 查找当前运行的 codex 进程
func FindRunningPIDs() map[string]int {
	pidsByCWD := make(map[string]int)

	entries, err := os.ReadDir("/proc")
	if err != nil {
		return pidsByCWD
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}

		pidPath := filepath.Join("/proc", entry.Name())
		commBytes, err := os.ReadFile(filepath.Join(pidPath, "comm"))
		if err != nil {
			continue
		}
		comm := strings.ToLower(strings.TrimSpace(string(commBytes)))
		isMatch := comm == "codex" || strings.Contains(comm, "codex")

		if !isMatch {
			cmdlineBytes, err := os.ReadFile(filepath.Join(pidPath, "cmdline"))
			if err == nil {
				cmdline := strings.ToLower(string(cmdlineBytes))
				if strings.Contains(cmdline, "codex") {
					isMatch = true
				}
			}
		}

		if isMatch {
			cwd, _ := os.Readlink(filepath.Join(pidPath, "cwd"))
			if cwd != "" {
				pidsByCWD[cwd] = pid
			}
		}
	}

	return pidsByCWD
}

// DiscoverRunningSessions 发现系统归属 ChatGPT/Codex 的活跃与历史会话
func DiscoverRunningSessions(dbPath string) ([]*supervisor.AgentProcess, error) {
	threads, err := QueryThreads(dbPath)
	if err != nil {
		// 数据库不存在或无法读取时返回空切片
		return []*supervisor.AgentProcess{}, nil
	}

	runningPIDs := FindRunningPIDs()
	processes := make([]*supervisor.AgentProcess, 0, len(threads))

	for _, thr := range threads {
		pid := runningPIDs[thr.CWD]
		status := supervisor.StatusIdle
		if pid > 0 {
			status = supervisor.StatusWorking
		}

		proc := &supervisor.AgentProcess{
			ID:             thr.ID,
			PID:            pid,
			Provider:       "chatgpt",
			ClientType:     "cli",
			Workspace:      thr.CWD,
			Status:         status,
			TranscriptPath: thr.RolloutPath,
			QuotaRemaining: 100, // 默认额度充足
			LastHeartbeat:  thr.LastUpdated,
		}

		processes = append(processes, proc)
	}

	return processes, nil
}
