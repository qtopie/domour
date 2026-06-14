package session

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/qtopie/domour/internal/app/assistant/shared"
)

type SessionInfo struct {
	SessionID   string           `json:"session_id"`
	Provider    string           `json:"provider"`
	Model       string           `json:"model"`
	LastMessage string           `json:"last_message"`
	UpdatedAt   time.Time        `json:"updated_at"`
	History     []shared.Message `json:"history,omitempty"`
}

type QueryFilter struct {
	Provider  string `json:"provider,omitempty"`
	SessionID string `json:"session_id,omitempty"`
}

// QuerySessions provides a unified query service to retrieve sessions across all LLM providers,
// combining SurrealDB/Memory store sessions and local CLI log files.
func QuerySessions(ctx context.Context, store Store, filter QueryFilter) ([]SessionInfo, error) {
	sessionMap := make(map[string]SessionInfo)

	// 1. Fetch sessions from the active session store (SurrealDB/Memory)
	if store != nil {
		dbSessions, err := store.ListSessions(ctx)
		if err == nil {
			for _, s := range dbSessions {
				prov := s.ActiveProvider
				if prov == "" {
					prov = "unknown"
				}
				model := s.ActiveModel
				if model == "" {
					model = "unknown"
				}

				var lastMsg string
				if len(s.History) > 0 {
					lastMsg = s.History[len(s.History)-1].Content
				}

				info := SessionInfo{
					SessionID:   s.ID,
					Provider:    prov,
					Model:       model,
					LastMessage: lastMsg,
					UpdatedAt:   s.UpdatedAt,
					History:     s.History,
				}
				sessionMap[s.ID] = info
			}
		}
	}

	// 2. Discover local CLI provider sessions from ~/.gemini and ~/.antigravity
	homeDir, err := os.UserHomeDir()
	if err == nil {
		var cliRoots []string
		if testRoots := os.Getenv("DOMOUR_TEST_CLI_ROOTS"); testRoots != "" {
			cliRoots = strings.Split(testRoots, string(os.PathListSeparator))
		} else {
			cliRoots = []string{
				filepath.Join(homeDir, ".gemini"),
				filepath.Join(homeDir, ".antigravity"),
			}
		}

		for _, root := range cliRoots {
			_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return nil
				}
				// Look for chat jsonl session files
				if !d.IsDir() && strings.HasSuffix(d.Name(), ".jsonl") && strings.Contains(path, "/chats/") {
					cliSess, parseErr := parseCliSession(path)
					if parseErr == nil && cliSess != nil {
						// Only overwrite or add if the CLI session is newer or doesn't exist in the active store
						existing, exists := sessionMap[cliSess.SessionID]
						if !exists || cliSess.UpdatedAt.After(existing.UpdatedAt) {
							sessionMap[cliSess.SessionID] = *cliSess
						}
					}
				}
				return nil
			})
		}
	}

	// 3. Filter and aggregate the results
	var results []SessionInfo
	filterProv := strings.ToLower(strings.TrimSpace(filter.Provider))
	filterSess := strings.TrimSpace(filter.SessionID)

	for _, info := range sessionMap {
		if filterProv != "" {
			if !strings.Contains(strings.ToLower(info.Provider), filterProv) {
				continue
			}
		}
		if filterSess != "" {
			if info.SessionID != filterSess {
				continue
			}
		}
		results = append(results, info)
	}

	// 4. Sort sessions by UpdatedAt descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].UpdatedAt.After(results[j].UpdatedAt)
	})

	return results, nil
}

func parseCliSession(filePath string) (*SessionInfo, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var info SessionInfo
	
	// Determine provider by path
	if strings.Contains(filePath, ".antigravity") {
		info.Provider = "agy-cli"
		info.Model = "gemini-3.1-pro"
	} else {
		info.Provider = "gemini-cli"
		info.Model = "gemini-3.1-flash"
	}

	var history []shared.Message
	var seq int32 = 1

	for scanner.Scan() {
		line := scanner.Text()
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			continue
		}

		if sessID, ok := m["sessionId"].(string); ok && sessID != "" {
			info.SessionID = sessID
		}
		if lastUp, ok := m["lastUpdated"].(string); ok {
			if t, err := time.Parse(time.RFC3339, lastUp); err == nil {
				info.UpdatedAt = t
			}
		} else if startT, ok := m["startTime"].(string); ok {
			if t, err := time.Parse(time.RFC3339, startT); err == nil {
				info.UpdatedAt = t
			}
		}

		msgType, _ := m["type"].(string)
		if msgType == "" {
			continue
		}

		timestampStr, _ := m["timestamp"].(string)
		var msgTime int64
		if t, err := time.Parse(time.RFC3339, timestampStr); err == nil {
			msgTime = t.Unix()
		}

		if msgType == "user" {
			var userText string
			if contentSlice, ok := m["content"].([]any); ok {
				for _, part := range contentSlice {
					if partMap, ok := part.(map[string]any); ok {
						if txt, ok := partMap["text"].(string); ok {
							// Strip system instructions and user headers to keep dialogue clean
							if idx := strings.Index(txt, "[USER]\nUser request:\n"); idx != -1 {
								txt = txt[idx+len("[USER]\nUser request:\n"):]
							} else if idx := strings.Index(txt, "[USER]\n"); idx != -1 {
								txt = txt[idx+len("[USER]\n"):]
							}
							userText += txt
						}
					}
				}
			}
			userText = strings.TrimSpace(userText)
			if userText != "" {
				history = append(history, shared.Message{
					Role:    "user",
					Content: userText,
					Time:    msgTime,
					Seq:     seq,
				})
				seq++
			}
		} else if msgType == "gemini" || msgType == "assistant" || msgType == "agy" {
			var assistantText string
			if contentStr, ok := m["content"].(string); ok {
				assistantText = contentStr
			}
			if modelStr, ok := m["model"].(string); ok && modelStr != "" {
				info.Model = modelStr
			}
			assistantText = strings.TrimSpace(assistantText)
			if assistantText != "" {
				history = append(history, shared.Message{
					Role:     "assistant",
					Content:  assistantText,
					Time:     msgTime,
					Seq:      seq,
					Provider: info.Provider,
					Model:    info.Model,
				})
				seq++
			}
		}
	}

	if len(history) == 0 {
		return nil, fmt.Errorf("empty history")
	}

	info.History = history
	info.LastMessage = history[len(history)-1].Content
	return &info, nil
}
