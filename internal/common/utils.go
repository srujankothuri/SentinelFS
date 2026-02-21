package common

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
)

// GenerateChunkID creates a unique chunk ID from file path and chunk index
func GenerateChunkID(filePath string, chunkIndex int) string {
	data := fmt.Sprintf("%s:chunk:%d", filePath, chunkIndex)
	hash := sha256.Sum256([]byte(data))
	return fmt.Sprintf("%x", hash[:8])
}

// FileChecksum computes SHA-256 checksum of a file
func FileChecksum(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("compute checksum: %w", err)
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// DataChecksum computes SHA-256 checksum of raw bytes
func DataChecksum(data []byte) string {
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h)
}

// ChunkFile splits a file into chunks and returns them as byte slices
func ChunkFile(path string) ([][]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	var chunks [][]byte
	buf := make([]byte, ChunkSize)

	for {
		n, err := f.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			chunks = append(chunks, chunk)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read file: %w", err)
		}
	}
	return chunks, nil
}

// ReassembleChunks writes ordered chunks to a file
func ReassembleChunks(chunks [][]byte, destPath string) error {
	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer f.Close()

	for i, chunk := range chunks {
		if _, err := f.Write(chunk); err != nil {
			return fmt.Errorf("write chunk %d: %w", i, err)
		}
	}
	return nil
}