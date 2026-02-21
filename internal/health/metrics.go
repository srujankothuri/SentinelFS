package health

import (
	"sync"
	"time"
)

// MetricType identifies a specific health metric
type MetricType string

const (
	MetricDiskIOLatency MetricType = "disk_io_latency"
	MetricDiskUtil      MetricType = "disk_utilization"
	MetricErrorRate     MetricType = "error_rate"
	MetricResponseTime  MetricType = "response_time"
	MetricCPUUsage      MetricType = "cpu_usage"
	MetricMemoryUsage   MetricType = "memory_usage"
)

// AllMetricTypes lists all tracked metrics
var AllMetricTypes = []MetricType{
	MetricDiskIOLatency,
	MetricDiskUtil,
	MetricErrorRate,
	MetricResponseTime,
	MetricCPUUsage,
	MetricMemoryUsage,
}

// MetricPoint is a single timestamped metric value
type MetricPoint struct {
	Value     float64
	Timestamp time.Time
}

// SlidingWindow maintains a time-bounded series of metric points
type SlidingWindow struct {
	mu       sync.RWMutex
	points   []MetricPoint
	duration time.Duration
}

// NewSlidingWindow creates a window that keeps points within the given duration
func NewSlidingWindow(duration time.Duration) *SlidingWindow {
	return &SlidingWindow{
		points:   make([]MetricPoint, 0, 256),
		duration: duration,
	}
}

// Add inserts a new metric point and evicts expired ones
func (sw *SlidingWindow) Add(value float64, ts time.Time) {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	sw.points = append(sw.points, MetricPoint{Value: value, Timestamp: ts})
	sw.evict(ts)
}

// Points returns a copy of all current points
func (sw *SlidingWindow) Points() []MetricPoint {
	sw.mu.RLock()
	defer sw.mu.RUnlock()

	result := make([]MetricPoint, len(sw.points))
	copy(result, sw.points)
	return result
}

// Values returns just the values (useful for stats)
func (sw *SlidingWindow) Values() []float64 {
	sw.mu.RLock()
	defer sw.mu.RUnlock()

	vals := make([]float64, len(sw.points))
	for i, p := range sw.points {
		vals[i] = p.Value
	}
	return vals
}

// Len returns the number of points in the window
func (sw *SlidingWindow) Len() int {
	sw.mu.RLock()
	defer sw.mu.RUnlock()
	return len(sw.points)
}

// Latest returns the most recent value, or 0 if empty
func (sw *SlidingWindow) Latest() float64 {
	sw.mu.RLock()
	defer sw.mu.RUnlock()

	if len(sw.points) == 0 {
		return 0
	}
	return sw.points[len(sw.points)-1].Value
}

// Mean returns the average value in the window
func (sw *SlidingWindow) Mean() float64 {
	sw.mu.RLock()
	defer sw.mu.RUnlock()

	if len(sw.points) == 0 {
		return 0
	}

	var sum float64
	for _, p := range sw.points {
		sum += p.Value
	}
	return sum / float64(len(sw.points))
}

// evict removes points older than the window duration (caller must hold lock)
func (sw *SlidingWindow) evict(now time.Time) {
	cutoff := now.Add(-sw.duration)
	idx := 0
	for idx < len(sw.points) && sw.points[idx].Timestamp.Before(cutoff) {
		idx++
	}
	if idx > 0 {
		sw.points = sw.points[idx:]
	}
}

// NodeMetrics stores all metric windows for a single node
type NodeMetrics struct {
	mu      sync.RWMutex
	NodeID  string
	Windows map[MetricType]*MetricWindows
}

// MetricWindows holds short/medium/long sliding windows for one metric
type MetricWindows struct {
	Short  *SlidingWindow // 5 min
	Medium *SlidingWindow // 15 min
	Long   *SlidingWindow // 1 hour
}

// NewNodeMetrics creates metric tracking for a node
func NewNodeMetrics(nodeID string) *NodeMetrics {
	nm := &NodeMetrics{
		NodeID:  nodeID,
		Windows: make(map[MetricType]*MetricWindows),
	}

	for _, mt := range AllMetricTypes {
		nm.Windows[mt] = &MetricWindows{
			Short:  NewSlidingWindow(5 * time.Minute),
			Medium: NewSlidingWindow(15 * time.Minute),
			Long:   NewSlidingWindow(1 * time.Hour),
		}
	}

	return nm
}

// Record adds a metric value to all windows
func (nm *NodeMetrics) Record(metricType MetricType, value float64, ts time.Time) {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	w, ok := nm.Windows[metricType]
	if !ok {
		return
	}

	w.Short.Add(value, ts)
	w.Medium.Add(value, ts)
	w.Long.Add(value, ts)
}

// GetWindow returns the sliding window for a metric at a given duration
func (nm *NodeMetrics) GetWindow(metricType MetricType, window string) *SlidingWindow {
	nm.mu.RLock()
	defer nm.mu.RUnlock()

	w, ok := nm.Windows[metricType]
	if !ok {
		return nil
	}

	switch window {
	case "short":
		return w.Short
	case "medium":
		return w.Medium
	case "long":
		return w.Long
	default:
		return w.Short
	}
}

// MetricStore manages metrics for all nodes in the cluster
type MetricStore struct {
	mu    sync.RWMutex
	nodes map[string]*NodeMetrics
}

// NewMetricStore creates a new metric store
func NewMetricStore() *MetricStore {
	return &MetricStore{
		nodes: make(map[string]*NodeMetrics),
	}
}

// GetOrCreate returns existing node metrics or creates new ones
func (ms *MetricStore) GetOrCreate(nodeID string) *NodeMetrics {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	nm, ok := ms.nodes[nodeID]
	if !ok {
		nm = NewNodeMetrics(nodeID)
		ms.nodes[nodeID] = nm
	}
	return nm
}

// Get returns node metrics if they exist
func (ms *MetricStore) Get(nodeID string) (*NodeMetrics, bool) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	nm, ok := ms.nodes[nodeID]
	return nm, ok
}

// AllNodeIDs returns all tracked node IDs
func (ms *MetricStore) AllNodeIDs() []string {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	ids := make([]string, 0, len(ms.nodes))
	for id := range ms.nodes {
		ids = append(ids, id)
	}
	return ids
}