package datanode

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/srujankothuri/SentinelFS/internal/common"
	pb "github.com/srujankothuri/SentinelFS/proto"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Server is the data node gRPC server
type Server struct {
	pb.UnimplementedDataNodeServiceServer
	store      *ChunkStore
	health     *HealthCollector
	grpcServer *grpc.Server

	nodeID    string
	address   string
	port      int
	adminPort int
	metaAddr  string
	capacity  int64
}

// NewServer creates a new data node server
func NewServer(port int, adminPort int, metaAddr, dataDir string, capacity int64, advertiseAddr string) (*Server, error) {
	store, err := NewChunkStore(dataDir)
	if err != nil {
		return nil, fmt.Errorf("create chunk store: %w", err)
	}

	addr := advertiseAddr
	if addr == "" {
		addr = fmt.Sprintf("localhost:%d", port)
	}

	return &Server{
		store:     store,
		health:    NewHealthCollector(),
		port:      port,
		adminPort: adminPort,
		metaAddr:  metaAddr,
		address:   addr,
		capacity:  capacity,
	}, nil
}

// Start registers with metadata server and starts serving
func (s *Server) Start() error {
	// Register with metadata server
	if err := s.register(); err != nil {
		return fmt.Errorf("register with metadata server: %w", err)
	}

	// Start gRPC server
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", s.port))
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	s.grpcServer = grpc.NewServer()
	pb.RegisterDataNodeServiceServer(s.grpcServer, s)

	// Start background routines
	go s.heartbeatLoop()
	go s.healthReportLoop()

	// Start admin HTTP server for chaos testing
	admin := NewAdminServer(s.health, s.adminPort, s.nodeID)
	go func() {
		if err := admin.Start(); err != nil {
			slog.Error("admin server failed", "error", err)
		}
	}()

	slog.Info("data node started",
		"node_id", s.nodeID,
		"port", s.port,
		"admin_port", s.adminPort,
		"meta_addr", s.metaAddr,
	)

	return s.grpcServer.Serve(lis)
}

// Stop gracefully stops the server
func (s *Server) Stop() {
	if s.grpcServer != nil {
		s.grpcServer.GracefulStop()
	}
}

// GetHealthCollector exposes health collector for degradation control
func (s *Server) GetHealthCollector() *HealthCollector {
	return s.health
}

// register connects to metadata server and registers this node
func (s *Server) register() error {
	conn, err := grpc.NewClient(s.metaAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("connect to metadata server: %w", err)
	}
	defer conn.Close()

	client := pb.NewMetadataServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := client.RegisterNode(ctx, &pb.RegisterNodeRequest{
		Address:       s.address,
		CapacityBytes: s.capacity,
	})
	if err != nil {
		return fmt.Errorf("register RPC: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("registration failed: %s", resp.Message)
	}

	s.nodeID = resp.NodeId
	slog.Info("registered with metadata server", "node_id", s.nodeID)
	return nil
}

// heartbeatLoop sends periodic heartbeats to metadata server
func (s *Server) heartbeatLoop() {
	ticker := time.NewTicker(common.HeartbeatInterval)
	defer ticker.Stop()

	for range ticker.C {
		if err := s.sendHeartbeat(); err != nil {
			slog.Error("heartbeat failed", "error", err)
		}
	}
}

func (s *Server) sendHeartbeat() error {
	conn, err := grpc.NewClient(s.metaAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer conn.Close()

	client := pb.NewMetadataServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err = client.Heartbeat(ctx, &pb.HeartbeatRequest{
		NodeId:     s.nodeID,
		UsedBytes:  s.store.UsedBytes(),
		ChunkCount: s.store.ChunkCount(),
	})
	return err
}

// healthReportLoop sends periodic health metrics to metadata server
func (s *Server) healthReportLoop() {
	ticker := time.NewTicker(common.MetricsReportInterval)
	defer ticker.Stop()

	for range ticker.C {
		if err := s.sendHealthReport(); err != nil {
			slog.Error("health report failed", "error", err)
		}
	}
}

func (s *Server) sendHealthReport() error {
	metrics := s.health.Collect()

	conn, err := grpc.NewClient(s.metaAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer conn.Close()

	client := pb.NewMetadataServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err = client.ReportHealth(ctx, &pb.ReportHealthRequest{
		Metrics: &pb.HealthMetrics{
			NodeId:           s.nodeID,
			DiskIoLatencyMs:  metrics.DiskIOLatency,
			DiskUtilization:  metrics.DiskUtil,
			ErrorCount:       metrics.ErrorCount,
			ResponseTimeMs:   metrics.ResponseTime,
			CpuUsage:         metrics.CPUUsage,
			MemoryUsage:      metrics.MemoryUsage,
			Timestamp:        metrics.Timestamp.UnixMilli(),
		},
	})
	return err
}

// ──────────────────────────────────────────────
// gRPC Service Implementation
// ──────────────────────────────────────────────

func (s *Server) StoreChunk(ctx context.Context, req *pb.StoreChunkRequest) (*pb.StoreChunkResponse, error) {
	slog.Info("StoreChunk", "chunk_id", req.ChunkId, "size", len(req.Data))

	if err := s.store.StoreChunk(req.ChunkId, req.Data, req.Checksum); err != nil {
		return &pb.StoreChunkResponse{Success: false, Message: err.Error()}, nil
	}

	return &pb.StoreChunkResponse{Success: true, Message: "chunk stored"}, nil
}

func (s *Server) GetChunk(ctx context.Context, req *pb.GetChunkRequest) (*pb.GetChunkResponse, error) {
	slog.Debug("GetChunk", "chunk_id", req.ChunkId)

	data, checksum, err := s.store.GetChunk(req.ChunkId)
	if err != nil {
		return &pb.GetChunkResponse{Success: false}, nil
	}

	return &pb.GetChunkResponse{
		Success:  true,
		Data:     data,
		Checksum: checksum,
	}, nil
}

func (s *Server) DeleteChunk(ctx context.Context, req *pb.DeleteChunkRequest) (*pb.DeleteChunkResponse, error) {
	slog.Info("DeleteChunk", "chunk_id", req.ChunkId)

	if err := s.store.DeleteChunk(req.ChunkId); err != nil {
		return &pb.DeleteChunkResponse{Success: false, Message: err.Error()}, nil
	}

	return &pb.DeleteChunkResponse{Success: true, Message: "chunk deleted"}, nil
}

func (s *Server) TransferChunk(ctx context.Context, req *pb.TransferChunkRequest) (*pb.TransferChunkResponse, error) {
	slog.Info("TransferChunk", "chunk_id", req.ChunkId, "target", req.TargetAddress)

	// Read chunk locally
	data, checksum, err := s.store.GetChunk(req.ChunkId)
	if err != nil {
		return &pb.TransferChunkResponse{Success: false, Message: err.Error()}, nil
	}

	// Connect to target node and send chunk
	conn, err := grpc.NewClient(req.TargetAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return &pb.TransferChunkResponse{Success: false, Message: err.Error()}, nil
	}
	defer conn.Close()

	targetClient := pb.NewDataNodeServiceClient(conn)
	storeCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := targetClient.StoreChunk(storeCtx, &pb.StoreChunkRequest{
		ChunkId:  req.ChunkId,
		Data:     data,
		Checksum: checksum,
	})
	if err != nil {
		return &pb.TransferChunkResponse{Success: false, Message: err.Error()}, nil
	}

	if !resp.Success {
		return &pb.TransferChunkResponse{Success: false, Message: resp.Message}, nil
	}

	slog.Info("chunk transferred", "chunk_id", req.ChunkId, "target", req.TargetAddress)
	return &pb.TransferChunkResponse{Success: true, Message: "chunk transferred"}, nil
}

func (s *Server) GetHealth(ctx context.Context, req *pb.GetHealthRequest) (*pb.GetHealthResponse, error) {
	metrics := s.health.Collect()

	return &pb.GetHealthResponse{
		Metrics: &pb.HealthMetrics{
			NodeId:           s.nodeID,
			DiskIoLatencyMs:  metrics.DiskIOLatency,
			DiskUtilization:  metrics.DiskUtil,
			ErrorCount:       metrics.ErrorCount,
			ResponseTimeMs:   metrics.ResponseTime,
			CpuUsage:         metrics.CPUUsage,
			MemoryUsage:      metrics.MemoryUsage,
			Timestamp:        metrics.Timestamp.UnixMilli(),
		},
	}, nil
}