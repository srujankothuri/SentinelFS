package datanode

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/srujankothuri/SentinelFS/internal/common"
)

// ChunkStore manages chunk files on local disk
type ChunkStore struct {
	mu       sync.RWMutex
	dataDir  string
	chunks   map[string]*StoredChunk // chunkID -> metadata
	usedBytes atomic.Int64
}

// StoredChunk tracks a locally stored chunk
type StoredChunk struct {
	ChunkID  string
	Size     int64
	Checksum string
	FilePath string // path on local disk
}

// NewChunkStore creates a new chunk store at the given directory
func NewChunkStore(dataDir string) (*ChunkStore, error) {
	// Create the data directory if it doesn't exist
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	cs := &ChunkStore{
		dataDir: dataDir,
		chunks:  make(map[string]*StoredChunk),
	}

	// Load existing chunks from disk
	if err := cs.loadExisting(); err != nil {
		slog.Warn("failed to load existing chunks", "error", err)
	}

	return cs, nil
}

// StoreChunk writes a chunk to disk with checksum verification
func (cs *ChunkStore) StoreChunk(chunkID string, data []byte, expectedChecksum string) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	// Verify checksum if provided
	actualChecksum := common.DataChecksum(data)
	if expectedChecksum != "" && actualChecksum != expectedChecksum {
		return fmt.Errorf("checksum mismatch for chunk %s: expected %s, got %s",
			chunkID, expectedChecksum, actualChecksum)
	}

	// Write to disk
	chunkPath := cs.chunkPath(chunkID)
	if err := os.WriteFile(chunkPath, data, 0644); err != nil {
		return fmt.Errorf("write chunk %s: %w", chunkID, err)
	}

	size := int64(len(data))
	cs.chunks[chunkID] = &StoredChunk{
		ChunkID:  chunkID,
		Size:     size,
		Checksum: actualChecksum,
		FilePath: chunkPath,
	}
	cs.usedBytes.Add(size)

	slog.Debug("chunk stored", "chunk_id", chunkID, "size", size, "checksum", actualChecksum)
	return nil
}

// GetChunk reads a chunk from disk and verifies its integrity
func (cs *ChunkStore) GetChunk(chunkID string) ([]byte, string, error) {
	cs.mu.RLock()
	meta, ok := cs.chunks[chunkID]
	cs.mu.RUnlock()

	if !ok {
		return nil, "", fmt.Errorf("chunk not found: %s", chunkID)
	}

	data, err := os.ReadFile(meta.FilePath)
	if err != nil {
		return nil, "", fmt.Errorf("read chunk %s: %w", chunkID, err)
	}

	// Verify integrity
	checksum := common.DataChecksum(data)
	if checksum != meta.Checksum {
		slog.Error("chunk corruption detected",
			"chunk_id", chunkID,
			"expected", meta.Checksum,
			"actual", checksum,
		)
		return nil, "", fmt.Errorf("chunk %s corrupted on disk", chunkID)
	}

	return data, checksum, nil
}

// DeleteChunk removes a chunk from disk
func (cs *ChunkStore) DeleteChunk(chunkID string) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	meta, ok := cs.chunks[chunkID]
	if !ok {
		return fmt.Errorf("chunk not found: %s", chunkID)
	}

	if err := os.Remove(meta.FilePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete chunk %s: %w", chunkID, err)
	}

	cs.usedBytes.Add(-meta.Size)
	delete(cs.chunks, chunkID)

	slog.Debug("chunk deleted", "chunk_id", chunkID)
	return nil
}

// HasChunk checks if a chunk exists locally
func (cs *ChunkStore) HasChunk(chunkID string) bool {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	_, ok := cs.chunks[chunkID]
	return ok
}

// ChunkCount returns the number of stored chunks
func (cs *ChunkStore) ChunkCount() int32 {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return int32(len(cs.chunks))
}

// UsedBytes returns total bytes used
func (cs *ChunkStore) UsedBytes() int64 {
	return cs.usedBytes.Load()
}

// GetAllChunkIDs returns all stored chunk IDs
func (cs *ChunkStore) GetAllChunkIDs() []string {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	ids := make([]string, 0, len(cs.chunks))
	for id := range cs.chunks {
		ids = append(ids, id)
	}
	return ids
}

// chunkPath returns the filesystem path for a chunk
func (cs *ChunkStore) chunkPath(chunkID string) string {
	return filepath.Join(cs.dataDir, chunkID+".chunk")
}

// loadExisting scans the data directory for existing chunks
func (cs *ChunkStore) loadExisting() error {
	entries, err := os.ReadDir(cs.dataDir)
	if err != nil {
		return err
	}

	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".chunk" {
			continue
		}

		chunkID := e.Name()[:len(e.Name())-6] // strip .chunk
		chunkPath := filepath.Join(cs.dataDir, e.Name())

		data, err := os.ReadFile(chunkPath)
		if err != nil {
			slog.Warn("failed to read existing chunk", "chunk_id", chunkID, "error", err)
			continue
		}

		size := int64(len(data))
		cs.chunks[chunkID] = &StoredChunk{
			ChunkID:  chunkID,
			Size:     size,
			Checksum: common.DataChecksum(data),
			FilePath: chunkPath,
		}
		cs.usedBytes.Add(size)
	}

	slog.Info("loaded existing chunks", "count", len(cs.chunks), "bytes", cs.usedBytes.Load())
	return nil
}