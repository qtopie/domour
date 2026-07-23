package storage

import (
	"context"
	"io"
)

// ObjectStorage defines the interface for storing unstructured binary/blob files
// such as images, audio, video, attachments, and generated code artifacts.
type ObjectStorage interface {
	// Put writes a file to object storage and returns its access URL/key.
	Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) (url string, err error)

	// Get retrieves a reader for the specified file key.
	Get(ctx context.Context, key string) (io.ReadCloser, error)

	// Delete removes the specified file.
	Delete(ctx context.Context, key string) error

	// Exists checks whether the specified file exists.
	Exists(ctx context.Context, key string) (bool, error)

	// GetURL returns a public or absolute file URL for the given key.
	GetURL(key string) string
}
