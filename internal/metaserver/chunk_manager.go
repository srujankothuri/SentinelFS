package metaserver

import (
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/srujankothuri/SentinelFS/internal/common"
)

// DataNodeInfo tracks a registered data node
type DataNodeInfo struct {
	NodeID        string
	Address       string
	CapacityBytes int64
	UsedBytes     int64
	ChunkCount    int32
	Status        common.NodeStatus
	RiskScore     float64
	LastHeartbeat time.Time
	Chunks        map[string]bool // chunk IDs stored on this node
}

// ChunkMeta tracks where a chunk is stored
type ChunkMeta struct {
	ChunkID    string
	ChunkIndex int
	Size       int64
	Checksum   string
	NodeIDs    []string // nodes that have this chunk
	FilePath   string   // which file this chunk belongs to
}

// ChunkManager handles chunk placement and tracking
type ChunkManager struct {
	mu     sync.RWMutex
	nodes  map[string]*DataNodeInfo // nodeID -> info
	chunks map[string]*ChunkMeta   // chunkID -> meta
}

// NewChunkManager creates a new chunk manager
func NewChunkManager() *ChunkManager {
	return &ChunkManager{
		nodes:  make(map[string]*DataNodeInfo),
		chunks: make(map[string]*ChunkMeta),
	}
}

// RegisterNode adds a new data node to the cluster
func (cm *ChunkManager) RegisterNode(address string, capacity int64) (string, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Check if node already registered by address
	for _, n := range cm.nodes {
		if n.Address == address {
			n.LastHeartbeat = time.Now()
			slog.Info("node re-registered", "node_id", n.NodeID, "address", address)
			return n.NodeID, nil
		}
	}

	nodeID := fmt.Sprintf("node-%d", len(cm.nodes)+1)
	cm.nodes[nodeID] = &DataNodeInfo{
		NodeID:        nodeID,
		Address:       address,
		CapacityBytes: capacity,
		Status:        common.StatusHealthy,
		LastHeartbeat: time.Now(),
		Chunks:        make(map[string]bool),
	}

	slog.Info("node registered", "node_id", nodeID, "address", address, "capacity", capacity)
	return nodeID, nil
}

// UpdateHeartbeat updates a node's heartbeat and stats
func (cm *ChunkManager) UpdateHeartbeat(nodeID string, usedBytes int64, chunkCount int32) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	node, ok := cm.nodes[nodeID]
	if !ok {
		return fmt.Errorf("unknown node: %s", nodeID)
	}

	node.LastHeartbeat = time.Now()
	node.UsedBytes = usedBytes
	node.ChunkCount = chunkCount

	// Revive dead node if heartbeat received
	if node.Status == common.StatusDead {
		node.Status = common.StatusHealthy
		node.RiskScore = 0.0
		slog.Info("node revived", "node_id", nodeID)
	}
	return nil
}

// UpdateNodeHealth updates a node's health status and risk score
func (cm *ChunkManager) UpdateNodeHealth(nodeID string, status common.NodeStatus, risk float64) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	node, ok := cm.nodes[nodeID]
	if !ok {
		return
	}
	node.Status = status
	node.RiskScore = risk
}

// AllocateChunks decides where to place chunks for a new file
func (cm *ChunkManager) AllocateChunks(filePath string, chunkCount int, fileSize int64) ([]*ChunkPlacementInfo, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	healthy := cm.healthyNodes()
	if len(healthy) == 0 {
		return nil, fmt.Errorf("no healthy nodes available")
	}

	replFactor := common.DefaultReplicationFactor
	if len(healthy) < replFactor {
		replFactor = len(healthy)
		slog.Warn("replication factor reduced", "requested", common.DefaultReplicationFactor, "available", replFactor)
	}

	chunkSize := common.ChunkSize
	placements := make([]*ChunkPlacementInfo, chunkCount)

	for i := 0; i < chunkCount; i++ {
		chunkID := common.GenerateChunkID(filePath, i)

		// Calculate chunk size (last chunk may be smaller)
		size := int64(chunkSize)
		remaining := fileSize - int64(i)*int64(chunkSize)
		if remaining < size {
			size = remaining
		}

		// Pick nodes with least chunks, spreading data evenly
		targets := cm.selectNodes(healthy, replFactor, chunkID)

		nodeIDs := make([]string, len(targets))
		nodeAddrs := make([]string, len(targets))
		for j, n := range targets {
			nodeIDs[j] = n.NodeID
			nodeAddrs[j] = n.Address
			n.Chunks[chunkID] = true
		}

		cm.chunks[chunkID] = &ChunkMeta{
			ChunkID:    chunkID,
			ChunkIndex: i,
			Size:       size,
			NodeIDs:    nodeIDs,
			FilePath:   filePath,
		}

		placements[i] = &ChunkPlacementInfo{
			ChunkID:       chunkID,
			ChunkIndex:    i,
			NodeIDs:       nodeIDs,
			NodeAddresses: nodeAddrs,
		}
	}

	return placements, nil
}

// ChunkPlacementInfo describes where a chunk should be stored
type ChunkPlacementInfo struct {
	ChunkID       string
	ChunkIndex    int
	NodeIDs       []string
	NodeAddresses []string
}

// selectNodes picks the best nodes for a chunk placement
// Strategy: prefer nodes with fewer chunks (load balancing) and lower risk
func (cm *ChunkManager) selectNodes(candidates []*DataNodeInfo, count int, chunkID string) []*DataNodeInfo {
	// Sort by chunk count (ascending) then risk score (ascending)
	sorted := make([]*DataNodeInfo, len(candidates))
	copy(sorted, candidates)

	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].RiskScore != sorted[j].RiskScore {
			return sorted[i].RiskScore < sorted[j].RiskScore
		}
		return len(sorted[i].Chunks) < len(sorted[j].Chunks)
	})

	if count > len(sorted) {
		count = len(sorted)
	}
	return sorted[:count]
}

// GetChunkLocations returns node addresses for each chunk of a file
func (cm *ChunkManager) GetChunkLocations(chunkIDs []string) ([]*ChunkLocationInfo, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	locs := make([]*ChunkLocationInfo, len(chunkIDs))
	for i, cid := range chunkIDs {
		meta, ok := cm.chunks[cid]
		if !ok {
			return nil, fmt.Errorf("chunk not found: %s", cid)
		}

		addrs := make([]string, 0, len(meta.NodeIDs))
		nodeIDs := make([]string, 0, len(meta.NodeIDs))
		for _, nid := range meta.NodeIDs {
			node, ok := cm.nodes[nid]
			if !ok || node.Status == common.StatusDead {
				continue
			}
			addrs = append(addrs, node.Address)
			nodeIDs = append(nodeIDs, nid)
		}

		if len(addrs) == 0 {
			return nil, fmt.Errorf("no live nodes for chunk %s", cid)
		}

		locs[i] = &ChunkLocationInfo{
			ChunkID:       meta.ChunkID,
			ChunkIndex:    meta.ChunkIndex,
			NodeAddresses: addrs,
			NodeIDs:       nodeIDs,
		}
	}
	return locs, nil
}

// ChunkLocationInfo describes where to find a chunk
type ChunkLocationInfo struct {
	ChunkID       string
	ChunkIndex    int
	NodeAddresses []string
	NodeIDs       []string
}

// RemoveChunks removes chunk metadata (used on file delete)
func (cm *ChunkManager) RemoveChunks(chunkIDs []string) []string {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Collect affected node addresses for cleanup
	nodeAddrs := make(map[string]bool)
	for _, cid := range chunkIDs {
		meta, ok := cm.chunks[cid]
		if !ok {
			continue
		}
		for _, nid := range meta.NodeIDs {
			if node, ok := cm.nodes[nid]; ok {
				delete(node.Chunks, cid)
				nodeAddrs[node.Address] = true
			}
		}
		delete(cm.chunks, cid)
	}

	addrs := make([]string, 0, len(nodeAddrs))
	for a := range nodeAddrs {
		addrs = append(addrs, a)
	}
	return addrs
}

// UpdateChunkChecksum sets the checksum after a chunk is stored
func (cm *ChunkManager) UpdateChunkChecksum(chunkID, checksum string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if meta, ok := cm.chunks[chunkID]; ok {
		meta.Checksum = checksum
	}
}

// GetChunkMeta returns metadata for a specific chunk
func (cm *ChunkManager) GetChunkMeta(chunkID string) (*ChunkMeta, bool) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	meta, ok := cm.chunks[chunkID]
	return meta, ok
}

// GetNode returns info for a specific node
func (cm *ChunkManager) GetNode(nodeID string) (*DataNodeInfo, bool) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	node, ok := cm.nodes[nodeID]
	return node, ok
}

// GetAllNodes returns all registered nodes
func (cm *ChunkManager) GetAllNodes() []*DataNodeInfo {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	nodes := make([]*DataNodeInfo, 0, len(cm.nodes))
	for _, n := range cm.nodes {
		nodes = append(nodes, n)
	}
	return nodes
}

// GetNodeByAddress finds a node by its address
func (cm *ChunkManager) GetNodeByAddress(addr string) (*DataNodeInfo, bool) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	for _, n := range cm.nodes {
		if n.Address == addr {
			return n, true
		}
	}
	return nil, false
}

// healthyNodes returns nodes that are safe to receive new chunks
func (cm *ChunkManager) healthyNodes() []*DataNodeInfo {
	nodes := make([]*DataNodeInfo, 0)
	for _, n := range cm.nodes {
		if n.Status == common.StatusHealthy || n.Status == common.StatusWarning {
			if time.Since(n.LastHeartbeat) < common.DeadNodeTimeout {
				nodes = append(nodes, n)
			}
		}
	}
	return nodes
}

// CheckDeadNodes marks nodes as dead if heartbeat is stale
func (cm *ChunkManager) CheckDeadNodes() []string {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	var deadNodeIDs []string
	for _, n := range cm.nodes {
		if n.Status != common.StatusDead && time.Since(n.LastHeartbeat) > common.DeadNodeTimeout {
			n.Status = common.StatusDead
			slog.Warn("node marked dead", "node_id", n.NodeID, "last_heartbeat", n.LastHeartbeat)
			deadNodeIDs = append(deadNodeIDs, n.NodeID)
		}
	}
	return deadNodeIDs
}

// GetChunksOnNode returns all chunk IDs stored on a given node
func (cm *ChunkManager) GetChunksOnNode(nodeID string) []string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	node, ok := cm.nodes[nodeID]
	if !ok {
		return nil
	}

	chunks := make([]string, 0, len(node.Chunks))
	for cid := range node.Chunks {
		chunks = append(chunks, cid)
	}
	return chunks
}

// AddChunkToNode records that a chunk is now on a specific node
func (cm *ChunkManager) AddChunkToNode(chunkID, nodeID string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if node, ok := cm.nodes[nodeID]; ok {
		node.Chunks[chunkID] = true
	}
	if meta, ok := cm.chunks[chunkID]; ok {
		// Avoid duplicates
		for _, nid := range meta.NodeIDs {
			if nid == nodeID {
				return
			}
		}
		meta.NodeIDs = append(meta.NodeIDs, nodeID)
	}
}

// RemoveChunkFromNode removes a chunk record from a node
func (cm *ChunkManager) RemoveChunkFromNode(chunkID, nodeID string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if node, ok := cm.nodes[nodeID]; ok {
		delete(node.Chunks, chunkID)
	}
	if meta, ok := cm.chunks[chunkID]; ok {
		for i, nid := range meta.NodeIDs {
			if nid == nodeID {
				meta.NodeIDs = append(meta.NodeIDs[:i], meta.NodeIDs[i+1:]...)
				break
			}
		}
	}
}

// GetUnderReplicatedChunks finds chunks that have fewer replicas than desired
func (cm *ChunkManager) GetUnderReplicatedChunks() []*ChunkMeta {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	var under []*ChunkMeta
	for _, meta := range cm.chunks {
		liveCount := 0
		for _, nid := range meta.NodeIDs {
			if node, ok := cm.nodes[nid]; ok && node.Status != common.StatusDead {
				liveCount++
			}
		}
		if liveCount < common.DefaultReplicationFactor {
			under = append(under, meta)
		}
	}
	return under
}

// ClusterStats returns summary statistics
type ClusterStats struct {
	TotalNodes   int
	HealthyNodes int
	WarningNodes int
	AtRiskNodes  int
	DeadNodes    int
	TotalCap     int64
	TotalUsed    int64
	TotalChunks  int
}

// GetClusterStats returns current cluster statistics
func (cm *ChunkManager) GetClusterStats() ClusterStats {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	stats := ClusterStats{TotalChunks: len(cm.chunks)}
	for _, n := range cm.nodes {
		stats.TotalNodes++
		stats.TotalCap += n.CapacityBytes
		stats.TotalUsed += n.UsedBytes
		switch n.Status {
		case common.StatusHealthy:
			stats.HealthyNodes++
		case common.StatusWarning:
			stats.WarningNodes++
		case common.StatusAtRisk, common.StatusCritical:
			stats.AtRiskNodes++
		case common.StatusDead:
			stats.DeadNodes++
		}
	}
	return stats
}