package datanode

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
)

// AdminServer provides HTTP endpoints for controlling the data node
// Used for chaos testing and degradation simulation
type AdminServer struct {
	health *HealthCollector
	port   int
	nodeID string
}

// NewAdminServer creates a new admin HTTP server
func NewAdminServer(health *HealthCollector, port int, nodeID string) *AdminServer {
	return &AdminServer{
		health: health,
		port:   port,
		nodeID: nodeID,
	}
}

// Start launches the admin HTTP server
func (a *AdminServer) Start() error {
	mux := http.NewServeMux()

	mux.HandleFunc("/degrade", a.handleDegrade)
	mux.HandleFunc("/recover", a.handleRecover)
	mux.HandleFunc("/status", a.handleStatus)

	addr := fmt.Sprintf(":%d", a.port)
	slog.Info("admin server started", "port", a.port, "node_id", a.nodeID)

	return http.ListenAndServe(addr, mux)
}

// POST /degrade?speed=0.1
// Starts simulating disk degradation
// speed: how fast metrics worsen (0.01=slow, 0.1=medium, 0.5=fast)
func (a *AdminServer) handleDegrade(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}

	speedStr := r.URL.Query().Get("speed")
	speed := 0.1 // default medium speed
	if speedStr != "" {
		var err error
		speed, err = strconv.ParseFloat(speedStr, 64)
		if err != nil {
			http.Error(w, "invalid speed parameter", http.StatusBadRequest)
			return
		}
	}

	a.health.StartDegradation(speed)

	slog.Warn("⚠️  DEGRADATION STARTED",
		"node_id", a.nodeID,
		"speed", speed,
	)

	resp := map[string]interface{}{
		"status":  "degrading",
		"node_id": a.nodeID,
		"speed":   speed,
		"message": "Disk degradation simulation started",
	}
	writeJSON(w, resp)
}

// POST /recover
// Stops degradation simulation
func (a *AdminServer) handleRecover(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}

	a.health.StopDegradation()

	slog.Info("✓ DEGRADATION STOPPED", "node_id", a.nodeID)

	resp := map[string]interface{}{
		"status":  "recovered",
		"node_id": a.nodeID,
		"message": "Degradation simulation stopped",
	}
	writeJSON(w, resp)
}

// GET /status
// Returns current health collector state
func (a *AdminServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	metrics := a.health.Collect()

	resp := map[string]interface{}{
		"node_id":          a.nodeID,
		"degrading":        a.health.IsDegrading(),
		"disk_io_latency":  fmt.Sprintf("%.2f ms", metrics.DiskIOLatency),
		"disk_utilization": fmt.Sprintf("%.1f%%", metrics.DiskUtil*100),
		"error_count":      metrics.ErrorCount,
		"response_time":    fmt.Sprintf("%.2f ms", metrics.ResponseTime),
		"cpu_usage":        fmt.Sprintf("%.1f%%", metrics.CPUUsage*100),
		"memory_usage":     fmt.Sprintf("%.1f%%", metrics.MemoryUsage*100),
	}
	writeJSON(w, resp)
}

func writeJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}