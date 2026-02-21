package health

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/srujankothuri/SentinelFS/internal/common"
	pb "github.com/srujankothuri/SentinelFS/proto"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// ChunkLocator provides chunk and node information to the migrator
// Implemented by the metadata server's chunk manager
type ChunkLocator interface {
	GetChunksOnNode(nodeID string) []string
	GetChunkMeta(chunkID string) (nodeIDs []string, found bool)
	GetHealthyNodeAddresses(excludeNodeIDs []string) []NodeTarget
	AddChunkToNode(chunkID, nodeID string)
	RemoveChunkFromNode(chunkID, nodeID string)
	GetNodeAddress(nodeID string) (string, bool)
}

// NodeTarget represents a healthy node that can receive chunks
type NodeTarget struct {
	NodeID  string
	Address string
}

// MigrationEvent records a single chunk migration
type MigrationEvent struct {
	ChunkID    string
	FromNodeID string
	ToNodeID   string
	ToAddress  string
	Reason     string
	Success    bool
	Error      string
	StartedAt  time.Time
	Duration   time.Duration
}

// MigrationReport summarizes a migration run
type MigrationReport struct {
	NodeID          string
	RiskScore       float64
	Status          common.NodeStatus
	TotalChunks     int
	MigratedChunks  int
	FailedChunks    int
	Events          []*MigrationEvent
	StartedAt       time.Time
	CompletedAt     time.Time
	Reasons         []string
}

// Migrator handles proactive chunk migration from at-risk nodes
type Migrator struct {
	mu        sync.Mutex
	locator   ChunkLocator
	migrating map[string]bool // nodeIDs currently being migrated
	history   []*MigrationReport
}

// NewMigrator creates a new migration engine
func NewMigrator(locator ChunkLocator) *Migrator {
	return &Migrator{
		locator:   locator,
		migrating: make(map[string]bool),
		history:   make([]*MigrationReport, 0),
	}
}

// MigrateNode moves all chunks off an at-risk node to healthy nodes
func (m *Migrator) MigrateNode(prediction *PredictionResult) *MigrationReport {
	m.mu.Lock()
	if m.migrating[prediction.NodeID] {
		m.mu.Unlock()
		slog.Info("migration already in progress", "node_id", prediction.NodeID)
		return nil
	}
	m.migrating[prediction.NodeID] = true
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		delete(m.migrating, prediction.NodeID)
		m.mu.Unlock()
	}()

	report := &MigrationReport{
		NodeID:    prediction.NodeID,
		RiskScore: prediction.OverallRisk,
		Status:    prediction.Status,
		Reasons:   prediction.Reasons,
		StartedAt: time.Now(),
	}

	slog.Warn("🚨 PROACTIVE MIGRATION STARTED",
		"node_id", prediction.NodeID,
		"risk", fmt.Sprintf("%.2f", prediction.OverallRisk),
		"status", prediction.Status,
		"reasons", prediction.Reasons,
	)

	// Get all chunks on the at-risk node
	chunkIDs := m.locator.GetChunksOnNode(prediction.NodeID)
	report.TotalChunks = len(chunkIDs)

	if len(chunkIDs) == 0 {
		slog.Info("no chunks to migrate", "node_id", prediction.NodeID)
		report.CompletedAt = time.Now()
		return report
	}

	slog.Info("chunks to migrate",
		"node_id", prediction.NodeID,
		"count", len(chunkIDs),
	)

	// Get source node address
	sourceAddr, ok := m.locator.GetNodeAddress(prediction.NodeID)
	if !ok {
		slog.Error("source node address not found", "node_id", prediction.NodeID)
		report.CompletedAt = time.Now()
		return report
	}

	// Migrate each chunk
	for _, chunkID := range chunkIDs {
		event := m.migrateChunk(chunkID, prediction.NodeID, sourceAddr)
		report.Events = append(report.Events, event)

		if event.Success {
			report.MigratedChunks++
		} else {
			report.FailedChunks++
		}
	}

	report.CompletedAt = time.Now()

	slog.Warn("🏁 PROACTIVE MIGRATION COMPLETED",
		"node_id", prediction.NodeID,
		"total", report.TotalChunks,
		"migrated", report.MigratedChunks,
		"failed", report.FailedChunks,
		"duration", report.CompletedAt.Sub(report.StartedAt),
	)

	// Store in history
	m.mu.Lock()
	m.history = append(m.history, report)
	m.mu.Unlock()

	return report
}

// migrateChunk moves a single chunk from source to a healthy target
func (m *Migrator) migrateChunk(chunkID, fromNodeID, sourceAddr string) *MigrationEvent {
	event := &MigrationEvent{
		ChunkID:    chunkID,
		FromNodeID: fromNodeID,
		StartedAt:  time.Now(),
	}

	// Find which nodes already have this chunk
	existingNodeIDs, found := m.locator.GetChunkMeta(chunkID)
	if !found {
		event.Error = "chunk metadata not found"
		event.Duration = time.Since(event.StartedAt)
		return event
	}

	// Get a healthy target that doesn't already have this chunk
	targets := m.locator.GetHealthyNodeAddresses(existingNodeIDs)
	if len(targets) == 0 {
		event.Error = "no healthy target nodes available"
		event.Duration = time.Since(event.StartedAt)
		slog.Warn("no targets for chunk migration",
			"chunk_id", chunkID,
			"existing_nodes", existingNodeIDs,
		)
		return event
	}

	target := targets[0] // pick the first healthy target
	event.ToNodeID = target.NodeID
	event.ToAddress = target.Address
	event.Reason = fmt.Sprintf("proactive migration from at-risk node %s (risk: %.2f)",
		fromNodeID, 0.0) // risk passed via report

	slog.Info("migrating chunk",
		"chunk_id", chunkID,
		"from", fromNodeID,
		"to", target.NodeID,
		"target_addr", target.Address,
	)

	// Trigger node-to-node transfer via gRPC
	err := m.transferChunk(sourceAddr, chunkID, target.Address)
	if err != nil {
		event.Error = err.Error()
		event.Duration = time.Since(event.StartedAt)
		slog.Error("chunk migration failed",
			"chunk_id", chunkID,
			"error", err,
		)
		return event
	}

	// Update metadata: add new node, remove old node
	m.locator.AddChunkToNode(chunkID, target.NodeID)
	m.locator.RemoveChunkFromNode(chunkID, fromNodeID)

	event.Success = true
	event.Duration = time.Since(event.StartedAt)

	slog.Info("✓ chunk migrated",
		"chunk_id", chunkID,
		"from", fromNodeID,
		"to", target.NodeID,
		"duration", event.Duration,
	)

	return event
}

// transferChunk tells the source node to send a chunk to the target
func (m *Migrator) transferChunk(sourceAddr, chunkID, targetAddr string) error {
	conn, err := grpc.NewClient(sourceAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("connect to source %s: %w", sourceAddr, err)
	}
	defer conn.Close()

	client := pb.NewDataNodeServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := client.TransferChunk(ctx, &pb.TransferChunkRequest{
		ChunkId:       chunkID,
		TargetAddress: targetAddr,
	})
	if err != nil {
		return fmt.Errorf("TransferChunk RPC: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("transfer failed: %s", resp.Message)
	}

	return nil
}

// IsMigrating checks if a node is currently being migrated
func (m *Migrator) IsMigrating(nodeID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.migrating[nodeID]
}

// GetHistory returns all migration reports
func (m *Migrator) GetHistory() []*MigrationReport {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make([]*MigrationReport, len(m.history))
	copy(result, m.history)
	return result
}

// GetLastReport returns the most recent migration report for a node
func (m *Migrator) GetLastReport(nodeID string) *MigrationReport {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i := len(m.history) - 1; i >= 0; i-- {
		if m.history[i].NodeID == nodeID {
			return m.history[i]
		}
	}
	return nil
}