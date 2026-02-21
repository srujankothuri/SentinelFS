package common

import "time"

const (
	// Chunk size: 4MB
	ChunkSize = 4 * 1024 * 1024

	// Default replication factor
	DefaultReplicationFactor = 3

	// Heartbeat interval from data nodes
	HeartbeatInterval = 3 * time.Second

	// Health metrics reporting interval
	MetricsReportInterval = 5 * time.Second

	// Node considered dead after missing heartbeats
	DeadNodeTimeout = 15 * time.Second

	// Health risk thresholds
	RiskHealthy  = 0.3
	RiskWarning  = 0.6
	RiskAtRisk   = 0.8
	RiskCritical = 0.8

	// Sliding window durations
	WindowShort  = 5 * time.Minute
	WindowMedium = 15 * time.Minute
	WindowLong   = 1 * time.Hour
)

// NodeStatus represents the health status of a data node
type NodeStatus string

const (
	StatusHealthy  NodeStatus = "HEALTHY"
	StatusWarning  NodeStatus = "WARNING"
	StatusAtRisk   NodeStatus = "AT_RISK"
	StatusCritical NodeStatus = "CRITICAL"
	StatusDead     NodeStatus = "DEAD"
)

// RiskToStatus converts a risk score to a NodeStatus
func RiskToStatus(risk float64) NodeStatus {
	switch {
	case risk < RiskHealthy:
		return StatusHealthy
	case risk < RiskWarning:
		return StatusWarning
	case risk < RiskAtRisk:
		return StatusAtRisk
	default:
		return StatusCritical
	}
}