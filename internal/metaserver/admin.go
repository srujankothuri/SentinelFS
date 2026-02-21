package metaserver

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
)

// AdminServer provides HTTP endpoints for monitoring the metadata server
type AdminServer struct {
	srv  *Server
	port int
}

// NewAdminServer creates a new admin HTTP server
func NewAdminServer(srv *Server, port int) *AdminServer {
	return &AdminServer{srv: srv, port: port}
}

// Start launches the admin HTTP server
func (a *AdminServer) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/migrations", a.handleMigrations)
	mux.HandleFunc("/health", a.handleHealth)

	addr := fmt.Sprintf(":%d", a.port)
	slog.Info("metaserver admin started", "port", a.port)
	return http.ListenAndServe(addr, mux)
}

// GET /migrations — returns migration history
func (a *AdminServer) handleMigrations(w http.ResponseWriter, r *http.Request) {
	reports := a.srv.migrator.GetHistory()

	type migrationSummary struct {
		NodeID    string `json:"node_id"`
		Risk      float64 `json:"risk"`
		Total     int    `json:"total_chunks"`
		Migrated  int    `json:"migrated"`
		Failed    int    `json:"failed"`
		Duration  string `json:"duration"`
		StartedAt string `json:"started_at"`
	}

	summaries := make([]migrationSummary, 0, len(reports))
	for _, r := range reports {
		summaries = append(summaries, migrationSummary{
			NodeID:    r.NodeID,
			Risk:      r.RiskScore,
			Total:     r.TotalChunks,
			Migrated:  r.MigratedChunks,
			Failed:    r.FailedChunks,
			Duration:  r.CompletedAt.Sub(r.StartedAt).String(),
			StartedAt: r.StartedAt.Format("15:04:05"),
		})
	}

	resp := map[string]interface{}{
		"count":      len(summaries),
		"migrations": summaries,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// GET /health — returns cluster health summary
func (a *AdminServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	stats := a.srv.chunkMgr.GetClusterStats()
	nodes := a.srv.chunkMgr.GetAllNodes()

	type nodeSummary struct {
		NodeID string  `json:"node_id"`
		Status string  `json:"status"`
		Risk   float64 `json:"risk"`
		Chunks int     `json:"chunks"`
	}

	nodeList := make([]nodeSummary, 0, len(nodes))
	for _, n := range nodes {
		nodeList = append(nodeList, nodeSummary{
			NodeID: n.NodeID,
			Status: string(n.Status),
			Risk:   n.RiskScore,
			Chunks: int(n.ChunkCount),
		})
	}

	resp := map[string]interface{}{
		"total_nodes":   stats.TotalNodes,
		"healthy_nodes": stats.HealthyNodes,
		"total_chunks":  stats.TotalChunks,
		"nodes":         nodeList,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}