package supervisor

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/fsnotify/fsnotify"
)

// TranscriptWatcher 负责监听 Agent 的 transcript.jsonl 并将新行反序列化为 TranscriptRecord
type TranscriptWatcher struct {
	fsWatcher *fsnotify.Watcher
	mu        sync.RWMutex
	files     map[string]*fileTrack
	stopCh    chan struct{}
}

type fileTrack struct {
	path      string
	provider  AgentProvider
	offset    int64
	listeners []chan *TranscriptRecord
}

// NewTranscriptWatcher 创建日志监听器
func NewTranscriptWatcher() (*TranscriptWatcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create fsnotify watcher: %w", err)
	}

	tw := &TranscriptWatcher{
		fsWatcher: watcher,
		files:     make(map[string]*fileTrack),
		stopCh:    make(chan struct{}),
	}

	go tw.run()
	return tw, nil
}

// WatchFile 监听指定文件并将记录写入给定的 channel
func (tw *TranscriptWatcher) WatchFile(ctx context.Context, path string, provider AgentProvider, out chan *TranscriptRecord) error {
	tw.mu.Lock()
	track, exists := tw.files[path]
	if !exists {
		// 初始获取当前文件大小
		var initialOffset int64
		if fi, err := os.Stat(path); err == nil {
			initialOffset = fi.Size()
		}

		track = &fileTrack{
			path:      path,
			provider:  provider,
			offset:    initialOffset,
			listeners: make([]chan *TranscriptRecord, 0),
		}
		tw.files[path] = track
		_ = tw.fsWatcher.Add(path)
	}

	track.listeners = append(track.listeners, out)
	tw.mu.Unlock()

	// 监听 ctx 取消以清理订阅
	go func() {
		<-ctx.Done()
		tw.mu.Lock()
		defer tw.mu.Unlock()
		if t, ok := tw.files[path]; ok {
			for i, ch := range t.listeners {
				if ch == out {
					t.listeners = append(t.listeners[:i], t.listeners[i+1:]...)
					break
				}
			}
			if len(t.listeners) == 0 {
				_ = tw.fsWatcher.Remove(path)
				delete(tw.files, path)
			}
		}
	}()

	return nil
}

func (tw *TranscriptWatcher) run() {
	for {
		select {
		case <-tw.stopCh:
			return
		case event, ok := <-tw.fsWatcher.Events:
			if !ok {
				return
			}
			if event.Has(fsnotify.Write) {
				tw.readAppendedLines(event.Name)
			}
		case <-tw.fsWatcher.Errors:
			// 忽略静默错误
		}
	}
}

func (tw *TranscriptWatcher) readAppendedLines(path string) {
	tw.mu.Lock()
	track, ok := tw.files[path]
	if !ok {
		tw.mu.Unlock()
		return
	}

	f, err := os.Open(path)
	if err != nil {
		tw.mu.Unlock()
		return
	}
	defer f.Close()

	if _, err := f.Seek(track.offset, io.SeekStart); err != nil {
		tw.mu.Unlock()
		return
	}

	scanner := bufio.NewScanner(f)
	var records []*TranscriptRecord

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		if rec, err := track.provider.ParseTranscriptLine(line); err == nil && rec != nil {
			records = append(records, rec)
		}
	}

	newOffset, err := f.Seek(0, io.SeekCurrent)
	if err == nil {
		track.offset = newOffset
	}
	listeners := make([]chan *TranscriptRecord, len(track.listeners))
	copy(listeners, track.listeners)
	tw.mu.Unlock()

	for _, rec := range records {
		for _, ch := range listeners {
			select {
			case ch <- rec:
			default:
			}
		}
	}
}

// Close 关闭监听器
func (tw *TranscriptWatcher) Close() error {
	close(tw.stopCh)
	return tw.fsWatcher.Close()
}
