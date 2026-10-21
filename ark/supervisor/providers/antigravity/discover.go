package antigravity

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/qtopie/domour/ark/supervisor"
)

// ActiveSessionInfo 包含发现的活跃会话元数据
type ActiveSessionInfo struct {
	Process      *supervisor.AgentProcess
	LastContent  string
	LastStep     int
	LastStepType string
	LastUpdated  time.Time
}

// DiscoverRunningSessions 自动发现当前系统中所有正在运行的 Antigravity (agy) 实例与会话
func DiscoverRunningSessions() ([]*ActiveSessionInfo, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = os.Getenv("HOME")
	}

	results := make([]*ActiveSessionInfo, 0)

	// 1. 扫描 /proc 目录（Linux 标准环境）
	procEntries, err := os.ReadDir("/proc")
	if err != nil {
		// 非 Linux 环境（如 Windows / macOS）或 /proc 受限：基于 brain 会话目录发现活跃会话
		return discoverSessionsFromBrain(homeDir)
	}

	for _, entry := range procEntries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}

		pidPath := filepath.Join("/proc", entry.Name())

		// 读取进程名称
		commBytes, err := os.ReadFile(filepath.Join(pidPath, "comm"))
		if err != nil {
			continue
		}
		comm := strings.TrimSpace(string(commBytes))
		commLower := strings.ToLower(comm)
		isMatch := commLower == "agy" || commLower == "antigravity" || commLower == "language_server"
		if !isMatch {
			cmdlineBytes, err := os.ReadFile(filepath.Join(pidPath, "cmdline"))
			if err == nil {
				cmdline := strings.ToLower(string(cmdlineBytes))
				if strings.Contains(cmdline, "agy") || strings.Contains(cmdline, "antigravity") || strings.Contains(cmdline, "language_server") {
					isMatch = true
				}
			}
		}
		if !isMatch {
			continue
		}

		// 读取工作目录 (CWD)
		cwd, _ := os.Readlink(filepath.Join(pidPath, "cwd"))

		// 扫描 /proc/<pid>/fd 提取持有的 presence lock 获取 conversation_id
		fdDir := filepath.Join(pidPath, "fd")
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue
		}

		var conversationID string
		for _, fd := range fds {
			link, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
			if err != nil {
				continue
			}
			if strings.Contains(link, "/presence/") && strings.HasSuffix(link, ".lock") {
				base := filepath.Base(link)
				conversationID = strings.TrimSuffix(base, ".lock")
				break
			}
		}

		if conversationID == "" {
			continue
		}

		// 检查底层 transcript.jsonl（兼容 CLI 与 IDE 多路径）
		candidateDirs := []string{
			filepath.Join(homeDir, ".gemini", "antigravity-cli", "brain"),
			filepath.Join(homeDir, ".gemini", "antigravity", "brain"),
			filepath.Join(homeDir, ".gemini", "antigravity-ide", "brain"),
		}
		var transcriptPath string
		for _, bd := range candidateDirs {
			tp := filepath.Join(bd, conversationID, ".system_generated", "logs", "transcript.jsonl")
			if _, err := os.Stat(tp); err == nil {
				transcriptPath = tp
				break
			}
		}
		if transcriptPath == "" {
			transcriptPath = filepath.Join(candidateDirs[0], conversationID, ".system_generated", "logs", "transcript.jsonl")
		}

		lastContent, lastStep, lastType, status, currentTool, lastTime := inspectLatestTranscript(transcriptPath)

		proc := &supervisor.AgentProcess{
			ID:             conversationID,
			PID:            pid,
			Provider:       ProviderName,
			ClientType:     DetectClientType(transcriptPath),
			Workspace:      cwd,
			Status:         status,
			CurrentTool:    currentTool,
			TranscriptPath: transcriptPath,
			LastHeartbeat:  lastTime,
		}

		results = append(results, &ActiveSessionInfo{
			Process:      proc,
			LastContent:  lastContent,
			LastStep:     lastStep,
			LastStepType: lastType,
			LastUpdated:  lastTime,
		})
	}

	return results, nil
}

func discoverSessionsFromBrain(homeDir string) ([]*ActiveSessionInfo, error) {
	if homeDir == "" {
		homeDir, _ = os.UserHomeDir()
		if homeDir == "" {
			homeDir = os.Getenv("HOME")
			if homeDir == "" {
				homeDir = os.Getenv("USERPROFILE")
			}
		}
	}

	results := make([]*ActiveSessionInfo, 0)
	candidateDirs := []string{
		filepath.Join(homeDir, ".gemini", "antigravity-cli", "brain"),
		filepath.Join(homeDir, ".gemini", "antigravity", "brain"),
		filepath.Join(homeDir, ".gemini", "antigravity-ide", "brain"),
	}

	seen := make(map[string]bool)
	for _, brainDir := range candidateDirs {
		entries, err := os.ReadDir(brainDir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() || seen[e.Name()] {
				continue
			}
			conversationID := e.Name()
			transcriptPath := filepath.Join(brainDir, conversationID, ".system_generated", "logs", "transcript.jsonl")
			if _, err := os.Stat(transcriptPath); err != nil {
				continue
			}

			seen[conversationID] = true
			lastContent, lastStep, lastType, status, currentTool, lastTime := inspectLatestTranscript(transcriptPath)

			proc := &supervisor.AgentProcess{
				ID:             conversationID,
				PID:            0,
				Provider:       ProviderName,
				ClientType:     DetectClientType(transcriptPath),
				Workspace:      filepath.Dir(filepath.Dir(filepath.Dir(transcriptPath))),
				Status:         status,
				CurrentTool:    currentTool,
				TranscriptPath: transcriptPath,
				LastHeartbeat:  lastTime,
			}

			results = append(results, &ActiveSessionInfo{
				Process:      proc,
				LastContent:  lastContent,
				LastStep:     lastStep,
				LastStepType: lastType,
				LastUpdated:  lastTime,
			})
		}
	}

	return results, nil
}

func inspectLatestTranscript(path string) (lastContent string, lastStep int, lastType string, status supervisor.ProcessStatus, currentTool string, lastTime time.Time) {
	status = supervisor.StatusWorking
	lastTime = time.Now()

	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	fi, err := f.Stat()
	if err == nil {
		lastTime = fi.ModTime()
	}

	// 快速读取文件最后一行
	lastLine := readLastLine(f)
	if len(lastLine) == 0 {
		return
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(lastLine, &raw); err != nil {
		return
	}

	if stepIdx, ok := raw["step_index"].(float64); ok {
		lastStep = int(stepIdx)
	}
	if t, ok := raw["type"].(string); ok {
		lastType = t
	}
	if c, ok := raw["content"].(string); ok {
		lastContent = c
	}

	// 检查是否有 tool 调用
	if toolCalls, ok := raw["tool_calls"].([]interface{}); ok && len(toolCalls) > 0 {
		if firstTool, ok := toolCalls[0].(map[string]interface{}); ok {
			if name, ok := firstTool["tool_name"].(string); ok {
				currentTool = name
			}
		}
	}

	// 状态推断
	if raw["source"] == "MODEL" && raw["status"] == "DONE" {
		status = supervisor.StatusIdle
	} else if raw["type"] == "USER_INPUT" {
		status = supervisor.StatusThinking
	} else if currentTool != "" {
		status = supervisor.StatusToolUse
	}

	return
}

func readLastLine(f *os.File) []byte {
	stat, err := f.Stat()
	if err != nil || stat.Size() == 0 {
		return nil
	}

	size := stat.Size()
	bufSize := int64(8192)
	if bufSize > size {
		bufSize = size
	}

	buf := make([]byte, bufSize)
	_, err = f.ReadAt(buf, size-bufSize)
	if err != nil && err != io.EOF {
		return nil
	}

	lines := strings.Split(strings.TrimSpace(string(buf)), "\n")
	if len(lines) == 0 {
		return nil
	}
	return []byte(lines[len(lines)-1])
}

// ReadSessionRecentTurns 读取某个会话最近的若干轮对话记录
func ReadSessionRecentTurns(transcriptPath string, maxTurns int) ([]*supervisor.TranscriptRecord, error) {
	if transcriptPath == "" {
		return nil, os.ErrNotExist
	}

	f, err := os.Open(transcriptPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	provider := NewProvider()
	scanner := bufio.NewScanner(f)
	records := make([]*supervisor.TranscriptRecord, 0)

	for scanner.Scan() {
		line := scanner.Bytes()
		rec, err := provider.ParseTranscriptLine(line)
		if err == nil && rec != nil {
			records = append(records, rec)
		}
	}

	if len(records) > maxTurns {
		records = records[len(records)-maxTurns:]
	}

	return records, nil
}
