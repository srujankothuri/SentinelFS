package metaserver

import (
	"github.com/srujankothuri/SentinelFS/internal/common"
	"github.com/srujankothuri/SentinelFS/internal/health"
)

// ChunkManagerAdapter wraps ChunkManager to implement health.ChunkLocator
type ChunkManagerAdapter struct {
	cm *ChunkManager
}

// NewChunkManagerAdapter creates a new adapter
func NewChunkManagerAdapter(cm *ChunkManager) *ChunkManagerAdapter {
	return &ChunkManagerAdapter{cm: cm}
}

func (a *ChunkManagerAdapter) GetChunksOnNode(nodeID string) []string {
	return a.cm.GetChunksOnNode(nodeID)
}

func (a *ChunkManagerAdapter) GetChunkMeta(chunkID string) ([]string, bool) {
	meta, ok := a.cm.GetChunkMeta(chunkID)
	if !ok {
		return nil, false
	}
	return meta.NodeIDs, true
}

func (a *ChunkManagerAdapter) GetHealthyNodeAddresses(excludeNodeIDs []string) []health.NodeTarget {
	excludeSet := make(map[string]bool)
	for _, id := range excludeNodeIDs {
		excludeSet[id] = true
	}

	allNodes := a.cm.GetAllNodes()
	targets := make([]health.NodeTarget, 0)

	for _, n := range allNodes {
		if excludeSet[n.NodeID] {
			continue
		}
		if n.Status == common.StatusHealthy || n.Status == common.StatusWarning {
			targets = append(targets, health.NodeTarget{
				NodeID:  n.NodeID,
				Address: n.Address,
			})
		}
	}
	return targets
}

func (a *ChunkManagerAdapter) AddChunkToNode(chunkID, nodeID string) {
	a.cm.AddChunkToNode(chunkID, nodeID)
}

func (a *ChunkManagerAdapter) RemoveChunkFromNode(chunkID, nodeID string) {
	a.cm.RemoveChunkFromNode(chunkID, nodeID)
}

func (a *ChunkManagerAdapter) GetNodeAddress(nodeID string) (string, bool) {
	node, ok := a.cm.GetNode(nodeID)
	if !ok {
		return "", false
	}
	return node.Address, true
}