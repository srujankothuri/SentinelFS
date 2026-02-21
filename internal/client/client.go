package client

import (
	"context"
	"fmt"
	"time"

	pb "github.com/srujankothuri/SentinelFS/proto"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Client connects to the SentinelFS cluster
type Client struct {
	metaAddr string
	conn     *grpc.ClientConn
	meta     pb.MetadataServiceClient
}

// New creates a new SentinelFS client
func New(metaAddr string) (*Client, error) {
	conn, err := grpc.NewClient(metaAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("connect to metadata server: %w", err)
	}

	return &Client{
		metaAddr: metaAddr,
		conn:     conn,
		meta:     pb.NewMetadataServiceClient(conn),
	}, nil
}

// Close closes the client connection
func (c *Client) Close() {
	if c.conn != nil {
		c.conn.Close()
	}
}

// PutFile uploads a local file to the distributed file system
func (c *Client) PutFile(localPath, remotePath string) error {
	// Read and chunk the file
	chunks, fileSize, checksum, err := readAndChunkFile(localPath)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	fmt.Printf("Uploading %s (%d bytes, %d chunks)\n", localPath, fileSize, len(chunks))

	// Request chunk placements from metadata server
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := c.meta.PutFile(ctx, &pb.PutFileRequest{
		Path:       remotePath,
		FileSize:   fileSize,
		ChunkCount: int32(len(chunks)),
		Checksum:   checksum,
	})
	if err != nil {
		return fmt.Errorf("PutFile RPC: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("PutFile failed: %s", resp.Message)
	}

	// Upload chunks in parallel to assigned nodes
	if err := uploadChunks(chunks, resp.Placements); err != nil {
		return fmt.Errorf("upload chunks: %w", err)
	}

	fmt.Printf("✓ Uploaded %s → %s (%d chunks across cluster)\n", localPath, remotePath, len(chunks))
	return nil
}

// GetFile downloads a file from the distributed file system
func (c *Client) GetFile(remotePath, localPath string) error {
	// Get chunk locations from metadata server
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := c.meta.GetFile(ctx, &pb.GetFileRequest{Path: remotePath})
	if err != nil {
		return fmt.Errorf("GetFile RPC: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("GetFile failed: %s", resp.Message)
	}

	fmt.Printf("Downloading %s (%d bytes, %d chunks)\n",
		remotePath, resp.FileInfo.Size, resp.FileInfo.ChunkCount)

	// Download chunks in parallel
	data, err := downloadChunks(resp.ChunkLocations)
	if err != nil {
		return fmt.Errorf("download chunks: %w", err)
	}

	// Write to local file
	if err := writeFile(localPath, data); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	fmt.Printf("✓ Downloaded %s → %s\n", remotePath, localPath)
	return nil
}

// ListFiles lists files at a remote path
func (c *Client) ListFiles(remotePath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := c.meta.ListFiles(ctx, &pb.ListFilesRequest{Path: remotePath})
	if err != nil {
		return fmt.Errorf("ListFiles RPC: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("ListFiles failed")
	}

	if len(resp.Files) == 0 {
		fmt.Println("(empty)")
		return nil
	}

	fmt.Printf("%-40s %10s %8s %s\n", "PATH", "SIZE", "CHUNKS", "CREATED")
	fmt.Println("─────────────────────────────────────────────────────────────────────")
	for _, f := range resp.Files {
		created := ""
		if f.CreatedAt > 0 {
			created = time.Unix(f.CreatedAt, 0).Format("2006-01-02 15:04")
		}
		if f.ChunkCount == 0 {
			// Directory
			fmt.Printf("%-40s %10s %8s %s\n", f.Path+"/", "DIR", "-", "")
		} else {
			fmt.Printf("%-40s %10s %8d %s\n", f.Path, formatSize(f.Size), f.ChunkCount, created)
		}
	}
	return nil
}

// DeleteFile removes a file from the cluster
func (c *Client) DeleteFile(remotePath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := c.meta.DeleteFile(ctx, &pb.DeleteFileRequest{Path: remotePath})
	if err != nil {
		return fmt.Errorf("DeleteFile RPC: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("DeleteFile failed: %s", resp.Message)
	}

	fmt.Printf("✓ Deleted %s\n", remotePath)
	return nil
}

// FileInfo shows detailed info about a file
func (c *Client) FileInfo(remotePath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := c.meta.GetFileInfo(ctx, &pb.GetFileInfoRequest{Path: remotePath})
	if err != nil {
		return fmt.Errorf("GetFileInfo RPC: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("file not found: %s", remotePath)
	}

	f := resp.FileInfo
	fmt.Printf("File:        %s\n", f.Path)
	fmt.Printf("Size:        %s (%d bytes)\n", formatSize(f.Size), f.Size)
	fmt.Printf("Chunks:      %d\n", f.ChunkCount)
	fmt.Printf("Replication: %d\n", f.ReplicationFactor)
	fmt.Printf("Created:     %s\n", time.Unix(f.CreatedAt, 0).Format("2006-01-02 15:04:05"))
	fmt.Println()

	fmt.Printf("  %-18s %10s %s\n", "CHUNK ID", "SIZE", "NODES")
	fmt.Println("  " + "──────────────────────────────────────────────────")
	for _, ch := range f.Chunks {
		if ch == nil {
			continue
		}
		fmt.Printf("  %-18s %10s %v\n", ch.ChunkId[:16], formatSize(ch.Size), ch.NodeIds)
	}
	return nil
}

// ClusterInfo shows cluster health overview
func (c *Client) ClusterInfo() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := c.meta.GetClusterInfo(ctx, &pb.GetClusterInfoRequest{})
	if err != nil {
		return fmt.Errorf("GetClusterInfo RPC: %w", err)
	}

	fmt.Println("═══════════════════════════════════════")
	fmt.Println("       SentinelFS Cluster Status       ")
	fmt.Println("═══════════════════════════════════════")
	fmt.Printf("  Nodes:    %d total (%d healthy, %d warning, %d at-risk, %d dead)\n",
		resp.TotalNodes, resp.HealthyNodes, resp.WarningNodes, resp.AtRiskNodes, resp.DeadNodes)
	fmt.Printf("  Storage:  %s / %s used\n", formatSize(resp.TotalUsed), formatSize(resp.TotalCapacity))
	fmt.Printf("  Files:    %d\n", resp.TotalFiles)
	fmt.Printf("  Chunks:   %d\n", resp.TotalChunks)
	fmt.Println()
	return nil
}

// NodeInfo shows per-node health details
func (c *Client) NodeInfo() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := c.meta.GetNodeInfo(ctx, &pb.GetNodeInfoRequest{})
	if err != nil {
		return fmt.Errorf("GetNodeInfo RPC: %w", err)
	}

	fmt.Printf("%-10s %-20s %-10s %8s %12s %8s\n",
		"NODE", "ADDRESS", "STATUS", "RISK", "STORAGE", "CHUNKS")
	fmt.Println("──────────────────────────────────────────────────────────────────────")

	for _, n := range resp.Nodes {
		statusIcon := statusColor(n.Status)
		fmt.Printf("%-10s %-20s %s %-10s %6.2f   %12s %8d\n",
			n.NodeId, n.Address, statusIcon, n.Status, n.RiskScore,
			fmt.Sprintf("%s/%s", formatSize(n.UsedBytes), formatSize(n.CapacityBytes)),
			n.ChunkCount)
	}
	return nil
}

func statusColor(status string) string {
	switch status {
	case "HEALTHY":
		return "🟢"
	case "WARNING":
		return "🟡"
	case "AT_RISK":
		return "🟠"
	case "CRITICAL":
		return "🔴"
	case "DEAD":
		return "💀"
	default:
		return "⚪"
	}
}

func formatSize(bytes int64) string {
	switch {
	case bytes >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(bytes)/(1<<30))
	case bytes >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(bytes)/(1<<20))
	case bytes >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(bytes)/(1<<10))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}