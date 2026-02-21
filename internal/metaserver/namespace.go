package metaserver

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// FileMeta holds metadata about a stored file
type FileMeta struct {
	Path              string
	Size              int64
	ChunkCount        int
	ReplicationFactor int
	Checksum          string
	ChunkIDs          []string // ordered chunk IDs
	CreatedAt         time.Time
}

// DirEntry represents a node in the filesystem tree
type DirEntry struct {
	Name     string
	IsDir    bool
	Children map[string]*DirEntry // dirname/filename -> entry
	File     *FileMeta            // non-nil if this is a file
}

// Namespace manages the in-memory file/directory tree
type Namespace struct {
	mu   sync.RWMutex
	root *DirEntry
}

// NewNamespace creates a new empty namespace with root directory
func NewNamespace() *Namespace {
	return &Namespace{
		root: &DirEntry{
			Name:     "/",
			IsDir:    true,
			Children: make(map[string]*DirEntry),
		},
	}
}

// CreateFile registers a new file in the namespace
func (ns *Namespace) CreateFile(path string, meta *FileMeta) error {
	ns.mu.Lock()
	defer ns.mu.Unlock()

	dir, name, err := ns.splitPath(path)
	if err != nil {
		return err
	}

	parent, err := ns.mkdirAll(dir)
	if err != nil {
		return fmt.Errorf("create parent dirs: %w", err)
	}

	if existing, ok := parent.Children[name]; ok {
		if existing.IsDir {
			return fmt.Errorf("path %s is a directory", path)
		}
		// Overwrite existing file
	}

	parent.Children[name] = &DirEntry{
		Name:  name,
		IsDir: false,
		File:  meta,
	}
	return nil
}

// GetFile retrieves file metadata by path
func (ns *Namespace) GetFile(path string) (*FileMeta, error) {
	ns.mu.RLock()
	defer ns.mu.RUnlock()

	entry, err := ns.resolve(path)
	if err != nil {
		return nil, err
	}
	if entry.IsDir {
		return nil, fmt.Errorf("path %s is a directory", path)
	}
	return entry.File, nil
}

// DeleteFile removes a file from the namespace
func (ns *Namespace) DeleteFile(path string) (*FileMeta, error) {
	ns.mu.Lock()
	defer ns.mu.Unlock()

	dir, name, err := ns.splitPath(path)
	if err != nil {
		return nil, err
	}

	parent, err := ns.resolveDir(dir)
	if err != nil {
		return nil, fmt.Errorf("parent directory not found: %w", err)
	}

	entry, ok := parent.Children[name]
	if !ok {
		return nil, fmt.Errorf("file not found: %s", path)
	}
	if entry.IsDir {
		return nil, fmt.Errorf("path %s is a directory, use rmdir", path)
	}

	meta := entry.File
	delete(parent.Children, name)
	return meta, nil
}

// ListDir lists all entries in a directory
func (ns *Namespace) ListDir(path string) ([]*DirEntry, error) {
	ns.mu.RLock()
	defer ns.mu.RUnlock()

	var dir *DirEntry
	var err error

	if path == "/" || path == "" {
		dir = ns.root
	} else {
		dir, err = ns.resolve(path)
		if err != nil {
			return nil, err
		}
	}

	if !dir.IsDir {
		return nil, fmt.Errorf("path %s is not a directory", path)
	}

	entries := make([]*DirEntry, 0, len(dir.Children))
	for _, child := range dir.Children {
		entries = append(entries, child)
	}
	return entries, nil
}

// FileExists checks if a file exists at the given path
func (ns *Namespace) FileExists(path string) bool {
	ns.mu.RLock()
	defer ns.mu.RUnlock()

	entry, err := ns.resolve(path)
	if err != nil {
		return false
	}
	return !entry.IsDir
}

// GetAllFiles returns all files in the namespace (for replication checks)
func (ns *Namespace) GetAllFiles() []*FileMeta {
	ns.mu.RLock()
	defer ns.mu.RUnlock()

	var files []*FileMeta
	ns.walkFiles(ns.root, &files)
	return files
}

// walkFiles recursively collects all files
func (ns *Namespace) walkFiles(entry *DirEntry, files *[]*FileMeta) {
	if !entry.IsDir && entry.File != nil {
		*files = append(*files, entry.File)
		return
	}
	for _, child := range entry.Children {
		ns.walkFiles(child, files)
	}
}

// resolve traverses the tree to find an entry by path
func (ns *Namespace) resolve(path string) (*DirEntry, error) {
	parts := ns.cleanPath(path)
	current := ns.root

	for _, part := range parts {
		child, ok := current.Children[part]
		if !ok {
			return nil, fmt.Errorf("path not found: %s", path)
		}
		current = child
	}
	return current, nil
}

// resolveDir resolves a path that must be a directory
func (ns *Namespace) resolveDir(path string) (*DirEntry, error) {
	if path == "/" || path == "" {
		return ns.root, nil
	}
	entry, err := ns.resolve(path)
	if err != nil {
		return nil, err
	}
	if !entry.IsDir {
		return nil, fmt.Errorf("path %s is not a directory", path)
	}
	return entry, nil
}

// mkdirAll creates all directories along a path
func (ns *Namespace) mkdirAll(path string) (*DirEntry, error) {
	if path == "/" || path == "" {
		return ns.root, nil
	}

	parts := ns.cleanPath(path)
	current := ns.root

	for _, part := range parts {
		child, ok := current.Children[part]
		if !ok {
			child = &DirEntry{
				Name:     part,
				IsDir:    true,
				Children: make(map[string]*DirEntry),
			}
			current.Children[part] = child
		} else if !child.IsDir {
			return nil, fmt.Errorf("path component %s is a file", part)
		}
		current = child
	}
	return current, nil
}

// splitPath splits "/a/b/c.txt" into dir="/a/b" and name="c.txt"
func (ns *Namespace) splitPath(path string) (string, string, error) {
	path = strings.TrimPrefix(path, "/")
	if path == "" {
		return "", "", fmt.Errorf("empty path")
	}

	parts := strings.Split(path, "/")
	name := parts[len(parts)-1]
	dir := "/" + strings.Join(parts[:len(parts)-1], "/")
	return dir, name, nil
}

// cleanPath splits a path into its components
func (ns *Namespace) cleanPath(path string) []string {
	path = strings.TrimPrefix(path, "/")
	path = strings.TrimSuffix(path, "/")
	if path == "" {
		return nil
	}
	return strings.Split(path, "/")
}