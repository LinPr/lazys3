// Package fsstore implements the local-filesystem Storage backend.
package fsstore

import (
	"context"
	"os"
)

// FileStore is the local-filesystem counterpart of the S3 store: the small
// surface the sync engine needs from a destination that is a plain
// directory.
type FileStore struct{}

// NewFileStore builds a FileStore. ctx and LocalOption are accepted for
// symmetry with NewS3Client; neither affects a local store today.
func NewFileStore(ctx context.Context, option LocalOption) *FileStore {
	return &FileStore{}
}

// Create opens path for writing, truncating an existing file.
func (fs *FileStore) Create(path string) (*os.File, error) {
	return os.Create(path)
}
