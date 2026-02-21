package health

import (
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/srujankothuri/SentinelFS/internal/common"
)

// MetricWeight defines how much each metric contributes to risk score
var MetricWeight = map[MetricType]float64{
	MetricDiskIOLatency: 0.25,
	MetricDiskUtil:      0.20,
	MetricErrorRate:     0.25,
	MetricResponseTime:  0.15,
	MetricCPUUsage:      0.05,
	MetricMemoryUsage:   0.10,
}

// MetricThreshold defines the "danger zone" for each metric
// Values above these are considered concerning
var MetricThreshold = map[MetricType]float64{
	MetricDiskIOLatency: 50.0,  // 50ms disk latency is bad
	MetricDiskUtil:      0.90,  // 90% disk usage
	MetricErrorRate:     5.0,   // 5 errors per interval
	MetricResponseTime:  30.0,  // 30ms response time
	MetricCPUUsage:      0.90,  // 90% CPU
	MetricMemoryUsage:   0.90,  // 90% memory
}

// PredictionResult holds the risk assessment for a single node
type PredictionResult struct {
	NodeID       string
	OverallRisk  float64
	Status       common.NodeStatus
	MetricRisks  map[MetricType]*MetricRisk
	Reasons      []string
	PredictedAt  time.Time
}

// MetricRisk holds risk details for a single metric
type MetricRisk struct {
	CurrentValue float64
	MeanValue    float64
	TrendSlope   float64 // positive = worsening
	RiskScore    float64 // 0.0 - 1.0
	Window       string  // which window was used
}

// Predictor analyzes health metrics and predicts node failures
type Predictor struct {
	monitor *Monitor
}

// NewPredictor creates a new prediction engine
func NewPredictor(monitor *Monitor) *Predictor {
	return &Predictor{
		monitor: monitor,
	}
}

// PredictAll runs prediction on all monitored nodes
func (p *Predictor) PredictAll() []*PredictionResult {
	nodeIDs := p.monitor.GetAllNodeIDs()
	results := make([]*PredictionResult, 0, len(nodeIDs))

	for _, nodeID := range nodeIDs {
		result := p.PredictNode(nodeID)
		if result != nil {
			results = append(results, result)
		}
	}
	return results
}

// PredictNode analyzes a single node and returns its risk assessment
func (p *Predictor) PredictNode(nodeID string) *PredictionResult {
	nm, ok := p.monitor.GetNodeMetrics(nodeID)
	if !ok {
		return nil
	}

	result := &PredictionResult{
		NodeID:      nodeID,
		MetricRisks: make(map[MetricType]*MetricRisk),
		PredictedAt: time.Now(),
	}

	var weightedRiskSum float64
	var totalWeight float64

	for _, metricType := range AllMetricTypes {
		weight := MetricWeight[metricType]
		threshold := MetricThreshold[metricType]

		risk := p.analyzeMetric(nm, metricType, threshold)
		result.MetricRisks[metricType] = risk

		weightedRiskSum += risk.RiskScore * weight
		totalWeight += weight

		// Generate human-readable reasons for high risk metrics
		if risk.RiskScore >= 0.6 {
			result.Reasons = append(result.Reasons, formatReason(metricType, risk))
		}
	}

	// Calculate overall risk
	if totalWeight > 0 {
		result.OverallRisk = weightedRiskSum / totalWeight
	}

	// Clamp to 0-1
	result.OverallRisk = clampRisk(result.OverallRisk)
	result.Status = common.RiskToStatus(result.OverallRisk)

	if result.OverallRisk >= common.RiskWarning {
		slog.Warn("elevated risk detected",
			"node_id", nodeID,
			"risk", result.OverallRisk,
			"status", result.Status,
			"reasons", result.Reasons,
		)
	}

	return result
}

// analyzeMetric computes the risk score for a single metric
func (p *Predictor) analyzeMetric(nm *NodeMetrics, metricType MetricType, threshold float64) *MetricRisk {
	risk := &MetricRisk{}

	// Use short window for current state, medium for trend
	shortWin := nm.GetWindow(metricType, "short")
	mediumWin := nm.GetWindow(metricType, "medium")

	if shortWin == nil || shortWin.Len() < 2 {
		return risk
	}

	risk.CurrentValue = shortWin.Latest()
	risk.MeanValue = shortWin.Mean()
	risk.Window = "short"

	// ── Component 1: Current value risk (40%) ──
	// How close is the current value to the danger threshold?
	valueRisk := risk.CurrentValue / threshold
	if valueRisk > 1.0 {
		valueRisk = 1.0
	}

	// ── Component 2: Trend risk (40%) ──
	// Is the metric getting worse over time?
	var trendRisk float64

	// Use medium window for more stable trend if available
	trendWin := shortWin
	if mediumWin != nil && mediumWin.Len() >= 3 {
		trendWin = mediumWin
		risk.Window = "medium"
	}

	slope := linearRegressionSlope(trendWin.Points())
	risk.TrendSlope = slope

	if slope > 0 {
		// Positive slope = worsening
		// Normalize: slope relative to threshold gives trend severity
		trendRisk = (slope * 60.0) / threshold // project 60 seconds ahead
		if trendRisk > 1.0 {
			trendRisk = 1.0
		}
	}

	// ── Component 3: Volatility risk (20%) ──
	// High variance suggests instability
	volatility := computeVolatility(shortWin.Values())
	volRisk := volatility / (threshold * 0.5)
	if volRisk > 1.0 {
		volRisk = 1.0
	}

	// ── Combine components ──
	risk.RiskScore = clampRisk(0.4*valueRisk + 0.4*trendRisk + 0.2*volRisk)

	return risk
}

// linearRegressionSlope computes the slope of a least-squares fit
// Positive slope = metric is increasing over time
func linearRegressionSlope(points []MetricPoint) float64 {
	n := len(points)
	if n < 2 {
		return 0
	}

	// Use time offset in seconds as x, metric value as y
	baseTime := points[0].Timestamp

	var sumX, sumY, sumXY, sumX2 float64
	for _, p := range points {
		x := p.Timestamp.Sub(baseTime).Seconds()
		y := p.Value
		sumX += x
		sumY += y
		sumXY += x * y
		sumX2 += x * x
	}

	fn := float64(n)
	denominator := fn*sumX2 - sumX*sumX
	if math.Abs(denominator) < 1e-10 {
		return 0
	}

	slope := (fn*sumXY - sumX*sumY) / denominator
	return slope
}

// computeVolatility calculates the standard deviation of values
func computeVolatility(values []float64) float64 {
	n := len(values)
	if n < 2 {
		return 0
	}

	var sum float64
	for _, v := range values {
		sum += v
	}
	mean := sum / float64(n)

	var variance float64
	for _, v := range values {
		diff := v - mean
		variance += diff * diff
	}
	variance /= float64(n - 1)

	return math.Sqrt(variance)
}

func clampRisk(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func formatReason(mt MetricType, risk *MetricRisk) string {
	switch mt {
	case MetricDiskIOLatency:
		return fmt.Sprintf("Disk I/O latency elevated: %.1fms (trend: %+.2f/s)", risk.CurrentValue, risk.TrendSlope)
	case MetricDiskUtil:
		return fmt.Sprintf("Disk utilization high: %.1f%% (trend: %+.4f/s)", risk.CurrentValue*100, risk.TrendSlope)
	case MetricErrorRate:
		return fmt.Sprintf("Error rate increasing: %.1f errors/interval (trend: %+.2f/s)", risk.CurrentValue, risk.TrendSlope)
	case MetricResponseTime:
		return fmt.Sprintf("Response time degraded: %.1fms (trend: %+.2f/s)", risk.CurrentValue, risk.TrendSlope)
	case MetricCPUUsage:
		return fmt.Sprintf("CPU usage high: %.1f%% (trend: %+.4f/s)", risk.CurrentValue*100, risk.TrendSlope)
	case MetricMemoryUsage:
		return fmt.Sprintf("Memory usage high: %.1f%% (trend: %+.4f/s)", risk.CurrentValue*100, risk.TrendSlope)
	default:
		return fmt.Sprintf("%s risk: %.2f", mt, risk.RiskScore)
	}
}