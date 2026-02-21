package client

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/srujankothuri/SentinelFS/internal/common"
	pb "github.com/srujankothuri/SentinelFS/proto"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// readAndChunkFile reads a file and splits it into chunks
func readAndChunkFile(path string) ([][]byte, int64, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, "", fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return nil, 0, "", fmt.Errorf("stat file: %w", err)
	}

	// Compute file checksum
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, 0, "", fmt.Errorf("checksum: %w", err)
	}
	checksum := fmt.Sprintf("%x", h.Sum(nil))

	// Reset and read chunks
	f.Seek(0, 0)
	var chunks [][]byte
	buf := make([]byte, common.ChunkSize)

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
			return nil, 0, "", fmt.Errorf("read: %w", err)
		}
	}

	return chunks, stat.Size(), checksum, nil
}

// uploadChunks sends chunks to assigned data nodes in parallel
func uploadChunks(chunks [][]byte, placements []*pb.ChunkPlacement) error {
	var wg sync.WaitGroup
	errCh := make(chan error, len(placements))

	for i, p := range placements {
		if i >= len(chunks) {
			break
		}

		wg.Add(1)
		go func(idx int, placement *pb.ChunkPlacement, data []byte) {
			defer wg.Done()

			checksum := common.DataChecksum(data)

			// Send to all replica nodes
			for _, addr := range placement.NodeAddresses {
				if err := sendChunkToNode(addr, placement.ChunkId, data, checksum); err != nil {
					errCh <- fmt.Errorf("chunk %s to %s: %w", placement.ChunkId, addr, err)
					return
				}
			}

			fmt.Printf("  ✓ Chunk %d/%d uploaded (%s)\n", idx+1, len(chunks), placement.ChunkId[:12])
		}(i, p, chunks[i])
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		return err
	}
	return nil
}

// sendChunkToNode sends a single chunk to a data node
func sendChunkToNode(addr, chunkID string, data []byte, checksum string) error {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("connect to %s: %w", addr, err)
	}
	defer conn.Close()

	client := pb.NewDataNodeServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := client.StoreChunk(ctx, &pb.StoreChunkRequest{
		ChunkId:  chunkID,
		Data:     data,
		Checksum: checksum,
	})
	if err != nil {
		return fmt.Errorf("StoreChunk RPC: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("store failed: %s", resp.Message)
	}
	return nil
}

// chunkResult holds a downloaded chunk with its index
type chunkResult struct {
	Index int
	Data  []byte
}

// downloadChunks fetches chunks in parallel from data nodes
func downloadChunks(locations []*pb.ChunkLocation) ([]byte, error) {
	var mu sync.Mutex
	var wg sync.WaitGroup
	results := make([]chunkResult, 0, len(locations))
	errCh := make(chan error, len(locations))

	for _, loc := range locations {
		wg.Add(1)
		go func(l *pb.ChunkLocation) {
			defer wg.Done()

			data, err := fetchChunkFromNodes(l.ChunkId, l.NodeAddresses)
			if err != nil {
				errCh <- fmt.Errorf("chunk %s: %w", l.ChunkId, err)
				return
			}

			mu.Lock()
			results = append(results, chunkResult{
				Index: int(l.ChunkIndex),
				Data:  data,
			})
			mu.Unlock()

			fmt.Printf("  ✓ Chunk %d/%d downloaded (%s)\n", l.ChunkIndex+1, len(locations), l.ChunkId[:12])
		}(loc)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		return nil, err
	}

	// Sort by index and concatenate
	sort.Slice(results, func(i, j int) bool {
		return results[i].Index < results[j].Index
	})

	var total []byte
	for _, r := range results {
		total = append(total, r.Data...)
	}
	return total, nil
}

// fetchChunkFromNodes tries to get a chunk from any available node
func fetchChunkFromNodes(chunkID string, addresses []string) ([]byte, error) {
	var lastErr error

	for _, addr := range addresses {
		data, err := fetchChunkFromNode(addr, chunkID)
		if err != nil {
			lastErr = err
			continue
		}
		return data, nil
	}

	return nil, fmt.Errorf("all nodes failed for chunk %s: %w", chunkID, lastErr)
}

// fetchChunkFromNode downloads a chunk from a single node
func fetchChunkFromNode(addr, chunkID string) ([]byte, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	client := pb.NewDataNodeServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := client.GetChunk(ctx, &pb.GetChunkRequest{ChunkId: chunkID})
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("GetChunk failed")
	}

	// Verify checksum
	actual := common.DataChecksum(resp.Data)
	if resp.Checksum != "" && actual != resp.Checksum {
		return nil, fmt.Errorf("checksum mismatch: expected %s, got %s", resp.Checksum, actual)
	}

	return resp.Data, nil
}

// writeFile writes data to a local file
func writeFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0644)
}