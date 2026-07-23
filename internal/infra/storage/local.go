package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// LocalStorage implements ObjectStorage for the local filesystem.
type LocalStorage struct {
	baseDir string
}

// NewLocalStorage creates a new LocalStorage storing files in baseDir.
func NewLocalStorage(baseDir string) (*LocalStorage, error) {
	if baseDir == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("get home dir: %w", err)
		}
		baseDir = filepath.Join(homeDir, ".domour", "data", "assets")
	}

	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return nil, fmt.Errorf("create local storage dir %s: %w", baseDir, err)
	}

	return &LocalStorage{baseDir: baseDir}, nil
}

func (s *LocalStorage) Put(_ context.Context, key string, r io.Reader, _ int64, _ string) (string, error) {
	targetPath := filepath.Join(s.baseDir, key)
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return "", fmt.Errorf("create parent dir for %s: %w", key, err)
	}

	f, err := os.Create(targetPath)
	if err != nil {
		return "", fmt.Errorf("create local file %s: %w", key, err)
	}
	defer f.Close()

	if _, err := io.Copy(f, r); err != nil {
		return "", fmt.Errorf("write local file %s: %w", key, err)
	}

	return s.GetURL(key), nil
}

func (s *LocalStorage) Get(_ context.Context, key string) (io.ReadCloser, error) {
	targetPath := filepath.Join(s.baseDir, key)
	f, err := os.Open(targetPath)
	if err != nil {
		return nil, fmt.Errorf("open local file %s: %w", key, err)
	}
	return f, nil
}

func (s *LocalStorage) Delete(_ context.Context, key string) error {
	targetPath := filepath.Join(s.baseDir, key)
	if err := os.Remove(targetPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete local file %s: %w", key, err)
	}
	return nil
}

func (s *LocalStorage) Exists(_ context.Context, key string) (bool, error) {
	targetPath := filepath.Join(s.baseDir, key)
	_, err := os.Stat(targetPath)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func (s *LocalStorage) GetURL(key string) string {
	return "file://" + filepath.Join(s.baseDir, key)
}
