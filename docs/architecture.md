# SentinelFS Architecture

## System Overview

SentinelFS follows a master-worker architecture with a single metadata server coordinating multiple data nodes. The unique addition is a health monitoring and prediction subsystem that runs on the metadata server.

## Components

### Metadata Server
The central coordinator responsible for:
- **Namespace Management**: Maintains an in-memory file/directory tree. Maps file paths to ordered lists of chunk IDs.
- **Chunk Management**: Tracks chunk→node mappings. Handles chunk placement using a load-balancing strategy that prefers nodes with fewer chunks and lower risk scores.
- **Node Registry**: Manages data node registration, heartbeat tracking, and dead node detection (15s timeout).
- **Prediction Loop**: Runs every 3 seconds, scoring all nodes and triggering migrations when risk exceeds threshold.

### Data Nodes
Stateless workers that store chunk data on local disk:
- **Chunk Store**: Writes chunks as individual files with `.chunk` extension. Verifies integrity via SHA-256 on every read.
- **Health Collector**: Gathers system metrics with realistic jitter. Supports degradation simulation for testing.
- **Heartbeat**: Reports to metadata server every 3 seconds with storage stats.
- **Health Reporter**: Sends 6 health metrics every 5 seconds.
- **Admin API**: HTTP endpoints for chaos testing (`/degrade`, `/recover`, `/status`).

### Client
CLI tool that communicates with the metadata server for file operations:
- **Upload**: Reads file → chunks into 4MB pieces → requests placement from metadata server → sends chunks in parallel to assigned nodes.
- **Download**: Requests chunk locations → fetches chunks in parallel from data nodes → reassembles in order → verifies checksum.

## Data Flow

### File Upload
```
Client                    MetaServer              DataNodes
  │                          │                       │
  │── PutFile(path, size) ──►│                       │
  │                          │── AllocateChunks() ──►│
  │◄── ChunkPlacements ─────│                       │
  │                          │                       │
  │── StoreChunk(data) ─────────────────────────────►│ (parallel to all replica nodes)
  │◄── Success ──────────────────────────────────────│
```

### Predictive Migration
```
DataNode                  MetaServer                  HealthyNode
  │                          │                            │
  │── ReportHealth(metrics)─►│                            │
  │                          │── PredictAll() ──┐         │
  │                          │   risk = 0.86    │         │
  │                          │◄─────────────────┘         │
  │                          │                            │
  │                          │── MigrateNode() ──┐        │
  │◄── TransferChunk() ─────│                   │        │
  │── StoreChunk(data) ─────────────────────────────────►│
  │                          │◄─ UpdateMappings ─┘        │
```

## Health Monitoring Pipeline

```
DataNode Health Collector
        │
        ▼ (every 5s via gRPC)
Health Monitor (IngestReport)
        │
        ▼ (stores in sliding windows)
MetricStore → NodeMetrics → MetricWindows
                              │
                              ├─ Short  (5 min)
                              ├─ Medium (15 min)
                              └─ Long   (1 hour)
        │
        ▼ (every 3s)
Prediction Engine
        │
        ├─ Per-metric analysis:
        │   ├─ Current Value Risk (40%)
        │   ├─ Trend Risk via Linear Regression (40%)
        │   └─ Volatility Risk via Std Dev (20%)
        │
        ├─ Weighted combination across all 6 metrics
        │
        └─ Overall Risk Score (0.0 - 1.0)
                │
                ▼
        Status Assignment
        ├─ < 0.25  → HEALTHY
        ├─ < 0.45  → WARNING
        ├─ < 0.55  → AT_RISK
        └─ ≥ 0.55  → CRITICAL → Trigger Migration
```

## Replication Strategy

- Default replication factor: 3
- Chunk placement prefers nodes with:
  1. Lower risk scores
  2. Fewer existing chunks (load balancing)
- During migration, target nodes are selected from healthy nodes that don't already have the chunk
- Replication factor is maintained even after migration

## Consistency Model

- **Metadata**: Strongly consistent (single metadata server, mutex-protected)
- **Data**: Eventually consistent during migration windows
- **Trade-off**: Optimizes for data availability over strict consistency