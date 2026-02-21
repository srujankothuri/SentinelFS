package metaserver

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/srujankothuri/SentinelFS/internal/common"
	pb "github.com/srujankothuri/SentinelFS/proto"

	"google.golang.org/grpc"
)

// Server is the metadata gRPC server
type Server struct {
	pb.UnimplementedMetadataServiceServer
	namespace    *Namespace
	chunkMgr     *ChunkManager
	grpcServer   *grpc.Server
	port         int
}

// NewServer creates a new metadata server
func NewServer(port int) *Server {
	return &Server{
		namespace: NewNamespace(),
		chunkMgr:  NewChunkManager(),
		port:      port,
	}
}

// Start launches the gRPC server and background routines
func (s *Server) Start() error {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", s.port))
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	s.grpcServer = grpc.NewServer()
	pb.RegisterMetadataServiceServer(s.grpcServer, s)

	// Background: check for dead nodes
	go s.deadNodeChecker()

	slog.Info("metadata server started", "port", s.port)
	return s.grpcServer.Serve(lis)
}

// Stop gracefully stops the server
func (s *Server) Stop() {
	if s.grpcServer != nil {
		s.grpcServer.GracefulStop()
	}
}

// GetChunkManager exposes chunk manager for health monitor integration
func (s *Server) GetChunkManager() *ChunkManager {
	return s.chunkMgr
}

// GetNamespace exposes namespace for health monitor integration
func (s *Server) GetNamespace() *Namespace {
	return s.namespace
}

// ──────────────────────────────────────────────
// File Operations
// ──────────────────────────────────────────────

func (s *Server) PutFile(ctx context.Context, req *pb.PutFileRequest) (*pb.PutFileResponse, error) {
	slog.Info("PutFile request", "path", req.Path, "size", req.FileSize, "chunks", req.ChunkCount)

	// Allocate chunks across nodes
	placements, err := s.chunkMgr.AllocateChunks(req.Path, int(req.ChunkCount), req.FileSize)
	if err != nil {
		return &pb.PutFileResponse{Success: false, Message: err.Error()}, nil
	}

	// Build chunk IDs list
	chunkIDs := make([]string, len(placements))
	for i, p := range placements {
		chunkIDs[i] = p.ChunkID
	}

	// Register file in namespace
	meta := &FileMeta{
		Path:              req.Path,
		Size:              req.FileSize,
		ChunkCount:        int(req.ChunkCount),
		ReplicationFactor: common.DefaultReplicationFactor,
		Checksum:          req.Checksum,
		ChunkIDs:          chunkIDs,
		CreatedAt:         time.Now(),
	}

	if err := s.namespace.CreateFile(req.Path, meta); err != nil {
		return &pb.PutFileResponse{Success: false, Message: err.Error()}, nil
	}

	// Convert to proto placements
	pbPlacements := make([]*pb.ChunkPlacement, len(placements))
	for i, p := range placements {
		pbPlacements[i] = &pb.ChunkPlacement{
			ChunkId:       p.ChunkID,
			ChunkIndex:    int32(p.ChunkIndex),
			NodeAddresses: p.NodeAddresses,
			NodeIds:       p.NodeIDs,
		}
	}

	slog.Info("file registered", "path", req.Path, "chunks", len(placements))
	return &pb.PutFileResponse{
		Success:    true,
		Message:    "file registered",
		Placements: pbPlacements,
	}, nil
}

func (s *Server) GetFile(ctx context.Context, req *pb.GetFileRequest) (*pb.GetFileResponse, error) {
	slog.Info("GetFile request", "path", req.Path)

	meta, err := s.namespace.GetFile(req.Path)
	if err != nil {
		return &pb.GetFileResponse{Success: false, Message: err.Error()}, nil
	}

	locs, err := s.chunkMgr.GetChunkLocations(meta.ChunkIDs)
	if err != nil {
		return &pb.GetFileResponse{Success: false, Message: err.Error()}, nil
	}

	// Build proto response
	chunkInfos := make([]*pb.ChunkInfo, len(meta.ChunkIDs))
	chunkLocs := make([]*pb.ChunkLocation, len(locs))

	for i, loc := range locs {
		cm, _ := s.chunkMgr.GetChunkMeta(loc.ChunkID)
		chunkInfos[i] = &pb.ChunkInfo{
			ChunkId:    cm.ChunkID,
			ChunkIndex: int32(cm.ChunkIndex),
			Size:       cm.Size,
			Checksum:   cm.Checksum,
			NodeIds:    cm.NodeIDs,
		}
		chunkLocs[i] = &pb.ChunkLocation{
			ChunkId:       loc.ChunkID,
			ChunkIndex:    int32(loc.ChunkIndex),
			NodeAddresses: loc.NodeAddresses,
			NodeIds:       loc.NodeIDs,
		}
	}

	return &pb.GetFileResponse{
		Success: true,
		FileInfo: &pb.FileInfo{
			Path:              meta.Path,
			Size:              meta.Size,
			ChunkCount:        int32(meta.ChunkCount),
			ReplicationFactor: int32(meta.ReplicationFactor),
			Chunks:            chunkInfos,
			CreatedAt:         meta.CreatedAt.Unix(),
		},
		ChunkLocations: chunkLocs,
	}, nil
}

func (s *Server) DeleteFile(ctx context.Context, req *pb.DeleteFileRequest) (*pb.DeleteFileResponse, error) {
	slog.Info("DeleteFile request", "path", req.Path)

	meta, err := s.namespace.DeleteFile(req.Path)
	if err != nil {
		return &pb.DeleteFileResponse{Success: false, Message: err.Error()}, nil
	}

	// Remove chunk metadata
	s.chunkMgr.RemoveChunks(meta.ChunkIDs)

	slog.Info("file deleted", "path", req.Path, "chunks_removed", len(meta.ChunkIDs))
	return &pb.DeleteFileResponse{Success: true, Message: "file deleted"}, nil
}

func (s *Server) ListFiles(ctx context.Context, req *pb.ListFilesRequest) (*pb.ListFilesResponse, error) {
	path := req.Path
	if path == "" {
		path = "/"
	}

	entries, err := s.namespace.ListDir(path)
	if err != nil {
		return &pb.ListFilesResponse{Success: false}, nil
	}

	files := make([]*pb.FileInfo, 0, len(entries))
	for _, e := range entries {
		if e.IsDir {
			files = append(files, &pb.FileInfo{
				Path: path + "/" + e.Name,
			})
		} else if e.File != nil {
			files = append(files, &pb.FileInfo{
				Path:              e.File.Path,
				Size:              e.File.Size,
				ChunkCount:        int32(e.File.ChunkCount),
				ReplicationFactor: int32(e.File.ReplicationFactor),
				CreatedAt:         e.File.CreatedAt.Unix(),
			})
		}
	}

	return &pb.ListFilesResponse{Success: true, Files: files}, nil
}

func (s *Server) GetFileInfo(ctx context.Context, req *pb.GetFileInfoRequest) (*pb.GetFileInfoResponse, error) {
	meta, err := s.namespace.GetFile(req.Path)
	if err != nil {
		return &pb.GetFileInfoResponse{Success: false}, nil
	}

	chunkInfos := make([]*pb.ChunkInfo, len(meta.ChunkIDs))
	for i, cid := range meta.ChunkIDs {
		cm, ok := s.chunkMgr.GetChunkMeta(cid)
		if !ok {
			continue
		}
		chunkInfos[i] = &pb.ChunkInfo{
			ChunkId:    cm.ChunkID,
			ChunkIndex: int32(cm.ChunkIndex),
			Size:       cm.Size,
			Checksum:   cm.Checksum,
			NodeIds:    cm.NodeIDs,
		}
	}

	return &pb.GetFileInfoResponse{
		Success: true,
		FileInfo: &pb.FileInfo{
			Path:              meta.Path,
			Size:              meta.Size,
			ChunkCount:        int32(meta.ChunkCount),
			ReplicationFactor: int32(meta.ReplicationFactor),
			Chunks:            chunkInfos,
			CreatedAt:         meta.CreatedAt.Unix(),
		},
	}, nil
}

// ──────────────────────────────────────────────
// Node Management
// ──────────────────────────────────────────────

func (s *Server) RegisterNode(ctx context.Context, req *pb.RegisterNodeRequest) (*pb.RegisterNodeResponse, error) {
	slog.Info("RegisterNode request", "address", req.Address, "capacity", req.CapacityBytes)

	nodeID, err := s.chunkMgr.RegisterNode(req.Address, req.CapacityBytes)
	if err != nil {
		return &pb.RegisterNodeResponse{Success: false, Message: err.Error()}, nil
	}

	return &pb.RegisterNodeResponse{
		Success: true,
		NodeId:  nodeID,
		Message: "node registered",
	}, nil
}

func (s *Server) Heartbeat(ctx context.Context, req *pb.HeartbeatRequest) (*pb.HeartbeatResponse, error) {
	err := s.chunkMgr.UpdateHeartbeat(req.NodeId, req.UsedBytes, req.ChunkCount)
	if err != nil {
		return &pb.HeartbeatResponse{Success: false}, nil
	}
	return &pb.HeartbeatResponse{Success: true}, nil
}

func (s *Server) ReportHealth(ctx context.Context, req *pb.ReportHealthRequest) (*pb.ReportHealthResponse, error) {
	if req.Metrics == nil {
		return &pb.ReportHealthResponse{Success: false, Status: "no metrics"}, nil
	}

	node, ok := s.chunkMgr.GetNode(req.Metrics.NodeId)
	if !ok {
		return &pb.ReportHealthResponse{Success: false, Status: "unknown node"}, nil
	}

	// Health monitor will process these metrics (wired in commit #14)
	// For now, just acknowledge
	return &pb.ReportHealthResponse{
		Success: true,
		Status:  string(node.Status),
	}, nil
}

// ──────────────────────────────────────────────
// Cluster Info
// ──────────────────────────────────────────────

func (s *Server) GetClusterInfo(ctx context.Context, req *pb.GetClusterInfoRequest) (*pb.GetClusterInfoResponse, error) {
	stats := s.chunkMgr.GetClusterStats()
	allFiles := s.namespace.GetAllFiles()
	nodes := s.chunkMgr.GetAllNodes()

	pbNodes := make([]*pb.NodeInfo, len(nodes))
	for i, n := range nodes {
		pbNodes[i] = &pb.NodeInfo{
			NodeId:        n.NodeID,
			Address:       n.Address,
			CapacityBytes: n.CapacityBytes,
			UsedBytes:     n.UsedBytes,
			ChunkCount:    n.ChunkCount,
			Status:        string(n.Status),
			RiskScore:     n.RiskScore,
		}
	}

	return &pb.GetClusterInfoResponse{
		TotalNodes:    int32(stats.TotalNodes),
		HealthyNodes:  int32(stats.HealthyNodes),
		WarningNodes:  int32(stats.WarningNodes),
		AtRiskNodes:   int32(stats.AtRiskNodes),
		DeadNodes:     int32(stats.DeadNodes),
		TotalCapacity: stats.TotalCap,
		TotalUsed:     stats.TotalUsed,
		TotalFiles:    int32(len(allFiles)),
		TotalChunks:   int32(stats.TotalChunks),
		Nodes:         pbNodes,
	}, nil
}

func (s *Server) GetNodeInfo(ctx context.Context, req *pb.GetNodeInfoRequest) (*pb.GetNodeInfoResponse, error) {
	var nodes []*DataNodeInfo

	if req.NodeId != "" {
		n, ok := s.chunkMgr.GetNode(req.NodeId)
		if !ok {
			return &pb.GetNodeInfoResponse{}, nil
		}
		nodes = []*DataNodeInfo{n}
	} else {
		nodes = s.chunkMgr.GetAllNodes()
	}

	pbNodes := make([]*pb.NodeInfo, len(nodes))
	for i, n := range nodes {
		pbNodes[i] = &pb.NodeInfo{
			NodeId:        n.NodeID,
			Address:       n.Address,
			CapacityBytes: n.CapacityBytes,
			UsedBytes:     n.UsedBytes,
			ChunkCount:    n.ChunkCount,
			Status:        string(n.Status),
			RiskScore:     n.RiskScore,
		}
	}

	return &pb.GetNodeInfoResponse{Nodes: pbNodes}, nil
}

// ──────────────────────────────────────────────
// Background Routines
// ──────────────────────────────────────────────

func (s *Server) deadNodeChecker() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		deadIDs := s.chunkMgr.CheckDeadNodes()
		for _, id := range deadIDs {
			slog.Warn("detected dead node", "node_id", id)
		}
	}
}