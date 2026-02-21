package datanode

import (
	"math/rand"
	"runtime"
	"sync"
	"time"
)

// HealthCollector gathers system health metrics from the data node
type HealthCollector struct {
	mu sync.RWMutex

	// Current metrics
	diskIOLatency  float64 // ms
	diskUtil       float64 // 0.0-1.0
	errorCount     int64
	responseTime   float64 // ms
	cpuUsage       float64 // 0.0-1.0
	memoryUsage    float64 // 0.0-1.0

	// Degradation simulation
	degrading      bool
	degradeSpeed   float64 // how fast metrics worsen per tick
	degradeStarted time.Time
}

// NewHealthCollector creates a new health collector
func NewHealthCollector() *HealthCollector {
	return &HealthCollector{
		diskIOLatency: 1.0 + rand.Float64()*2.0, // 1-3ms baseline
		diskUtil:      0.1 + rand.Float64()*0.2,  // 10-30% baseline
		errorCount:    0,
		responseTime:  0.5 + rand.Float64()*1.0,  // 0.5-1.5ms baseline
		cpuUsage:      0.05 + rand.Float64()*0.15, // 5-20% baseline
		memoryUsage:   0.2 + rand.Float64()*0.1,   // 20-30% baseline
	}
}

// Collect gathers current health metrics with realistic variation
func (hc *HealthCollector) Collect() Metrics {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	// Add natural jitter to metrics
	jitter := func(base, amount float64) float64 {
		v := base + (rand.Float64()-0.5)*amount
		if v < 0 {
			v = 0
		}
		return v
	}

	// If degrading, worsen metrics over time
	if hc.degrading {
		elapsed := time.Since(hc.degradeStarted).Seconds()
		factor := elapsed * hc.degradeSpeed

		hc.diskIOLatency += factor * 0.8
		hc.diskUtil += factor * 0.005
		hc.responseTime += factor * 0.5
		hc.cpuUsage += factor * 0.005
		hc.memoryUsage += factor * 0.003
		hc.errorCount += int64(factor * 1.0)

		// Clamp to valid ranges
		if hc.diskUtil > 0.99 {
			hc.diskUtil = 0.99
		}
		if hc.cpuUsage > 0.99 {
			hc.cpuUsage = 0.99
		}
		if hc.memoryUsage > 0.99 {
			hc.memoryUsage = 0.99
		}
	}

	// Get real memory stats
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	realMemUsage := float64(memStats.Alloc) / float64(memStats.Sys)
	if realMemUsage > 1.0 {
		realMemUsage = 1.0
	}

	return Metrics{
		DiskIOLatency: jitter(hc.diskIOLatency, 1.0),
		DiskUtil:      clamp(jitter(hc.diskUtil, 0.05), 0, 1),
		ErrorCount:    hc.errorCount,
		ResponseTime:  jitter(hc.responseTime, 0.5),
		CPUUsage:      clamp(jitter(hc.cpuUsage, 0.05), 0, 1),
		MemoryUsage:   clamp(jitter(hc.memoryUsage, 0.03)+realMemUsage*0.1, 0, 1),
		Timestamp:     time.Now(),
	}
}

// StartDegradation begins simulating disk/node degradation
func (hc *HealthCollector) StartDegradation(speed float64) {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	hc.degrading = true
	hc.degradeSpeed = speed
	hc.degradeStarted = time.Now()
}

// StopDegradation stops the degradation simulation
func (hc *HealthCollector) StopDegradation() {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	hc.degrading = false
}

// IsDegrading returns whether degradation is active
func (hc *HealthCollector) IsDegrading() bool {
	hc.mu.RLock()
	defer hc.mu.RUnlock()
	return hc.degrading
}

// Metrics represents a snapshot of node health
type Metrics struct {
	DiskIOLatency float64   // ms
	DiskUtil      float64   // 0.0-1.0
	ErrorCount    int64
	ResponseTime  float64   // ms
	CPUUsage      float64   // 0.0-1.0
	MemoryUsage   float64   // 0.0-1.0
	Timestamp     time.Time
}

func clamp(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}