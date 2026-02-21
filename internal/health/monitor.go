package health

import (
	"log/slog"
	"sync"
	"time"
)

// HealthReport represents a health metrics report from a data node
type HealthReport struct {
	NodeID        string
	DiskIOLatency float64
	DiskUtil      float64
	ErrorCount    int64
	ResponseTime  float64
	CPUUsage      float64
	MemoryUsage   float64
	Timestamp     time.Time
}

// Monitor collects and aggregates health metrics from all nodes
type Monitor struct {
	mu             sync.Mutex
	store          *MetricStore
	lastErrorCount map[string]int64
}

// NewMonitor creates a new health monitor
func NewMonitor() *Monitor {
	return &Monitor{
		store:          NewMetricStore(),
		lastErrorCount: make(map[string]int64),
	}
}

// GetStore returns the underlying metric store
func (m *Monitor) GetStore() *MetricStore {
	return m.store
}

// IngestReport processes a health report from a data node
func (m *Monitor) IngestReport(report *HealthReport) {
	m.mu.Lock()
	defer m.mu.Unlock()

	nm := m.store.GetOrCreate(report.NodeID)
	ts := report.Timestamp

	nm.Record(MetricDiskIOLatency, report.DiskIOLatency, ts)
	nm.Record(MetricDiskUtil, report.DiskUtil, ts)
	nm.Record(MetricResponseTime, report.ResponseTime, ts)
	nm.Record(MetricCPUUsage, report.CPUUsage, ts)
	nm.Record(MetricMemoryUsage, report.MemoryUsage, ts)

	lastCount, ok := m.lastErrorCount[report.NodeID]
	if ok {
		delta := report.ErrorCount - lastCount
		if delta < 0 {
			delta = 0
		}
		nm.Record(MetricErrorRate, float64(delta), ts)
	} else {
		nm.Record(MetricErrorRate, 0, ts)
	}
	m.lastErrorCount[report.NodeID] = report.ErrorCount

	slog.Debug("health report ingested",
		"node_id", report.NodeID,
		"disk_io", report.DiskIOLatency,
		"disk_util", report.DiskUtil,
		"errors", report.ErrorCount,
		"response_time", report.ResponseTime,
		"cpu", report.CPUUsage,
		"mem", report.MemoryUsage,
	)
}

// GetNodeMetrics returns metrics for a specific node
func (m *Monitor) GetNodeMetrics(nodeID string) (*NodeMetrics, bool) {
	return m.store.Get(nodeID)
}

// GetAllNodeIDs returns all nodes being monitored
func (m *Monitor) GetAllNodeIDs() []string {
	return m.store.AllNodeIDs()
}

// GetLatestSnapshot returns the latest values for all metrics of a node
func (m *Monitor) GetLatestSnapshot(nodeID string) map[MetricType]float64 {
	nm, ok := m.store.Get(nodeID)
	if !ok {
		return nil
	}

	snapshot := make(map[MetricType]float64)
	for _, mt := range AllMetricTypes {
		w := nm.GetWindow(mt, "short")
		if w != nil {
			snapshot[mt] = w.Latest()
		}
	}
	return snapshot
}