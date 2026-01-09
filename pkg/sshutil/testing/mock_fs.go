// Package testing provides SSH mock utilities for testing.
// This package simulates a remote machine with an in-memory filesystem.
package testing

import (
	"errors"
	"path/filepath"
	"strings"
	"sync"
)

// MockFS simulates an in-memory remote filesystem.
// It supports common filesystem operations used by rr: mkdir, cat, rm.
type MockFS struct {
	mu    sync.RWMutex
	files map[string][]byte   // path -> content
	dirs  map[string]struct{} // directories
}

// NewMockFS creates a new empty mock filesystem.
func NewMockFS() *MockFS {
	return &MockFS{
		files: make(map[string][]byte),
		dirs:  make(map[string]struct{}),
	}
}

// Mkdir creates a directory. Returns error if directory already exists.
// This mimics the behavior of `mkdir` (without -p flag).
func (fs *MockFS) Mkdir(path string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	path = filepath.Clean(path)

	// Check if already exists
	if _, exists := fs.dirs[path]; exists {
		return errors.New("directory already exists")
	}
	if _, exists := fs.files[path]; exists {
		return errors.New("file exists at path")
	}

	fs.dirs[path] = struct{}{}
	return nil
}

// MkdirAll creates a directory and all parent directories.
// This mimics the behavior of `mkdir -p`.
func (fs *MockFS) MkdirAll(path string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	path = filepath.Clean(path)

	// Create all parent directories
	parts := strings.Split(path, "/")
	current := ""
	for _, part := range parts {
		if part == "" {
			current = "/"
			continue
		}
		if current == "/" {
			current = "/" + part
		} else {
			current = current + "/" + part
		}
		fs.dirs[current] = struct{}{}
	}
	return nil
}

// WriteFile writes content to a file, creating parent directories as needed.
func (fs *MockFS) WriteFile(path string, content []byte) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	path = filepath.Clean(path)

	// Create parent directory
	dir := filepath.Dir(path)
	if dir != "." && dir != "/" {
		fs.dirs[dir] = struct{}{}
	}

	fs.files[path] = content
	return nil
}

// ReadFile reads the content of a file. Returns error if file doesn't exist.
func (fs *MockFS) ReadFile(path string) ([]byte, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	path = filepath.Clean(path)

	content, exists := fs.files[path]
	if !exists {
		return nil, errors.New("file not found")
	}
	return content, nil
}

// Remove removes a file or directory and all its contents.
// This mimics the behavior of `rm -rf`.
func (fs *MockFS) Remove(path string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	path = filepath.Clean(path)

	// Remove the path itself
	delete(fs.files, path)
	delete(fs.dirs, path)

	// Remove all children (for directories)
	prefix := path + "/"
	for p := range fs.files {
		if strings.HasPrefix(p, prefix) {
			delete(fs.files, p)
		}
	}
	for p := range fs.dirs {
		if strings.HasPrefix(p, prefix) {
			delete(fs.dirs, p)
		}
	}

	return nil
}

// Exists returns true if the path exists (file or directory).
func (fs *MockFS) Exists(path string) bool {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	path = filepath.Clean(path)

	if _, exists := fs.dirs[path]; exists {
		return true
	}
	if _, exists := fs.files[path]; exists {
		return true
	}
	return false
}

// IsDir returns true if the path exists and is a directory.
func (fs *MockFS) IsDir(path string) bool {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	path = filepath.Clean(path)
	_, exists := fs.dirs[path]
	return exists
}

// IsFile returns true if the path exists and is a file.
func (fs *MockFS) IsFile(path string) bool {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	path = filepath.Clean(path)
	_, exists := fs.files[path]
	return exists
}
