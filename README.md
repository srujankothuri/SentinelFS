# SentinelFS 🛡️

### A Predictive Self-Healing Distributed File System

SentinelFS is a distributed file system built in Go that goes beyond traditional reactive fault tolerance. It continuously monitors data node health metrics, uses **statistical trend analysis with linear regression** to predict node failures before they happen, and **proactively migrates data** to healthy nodes — achieving zero data loss without waiting for a crash.

> *"A DFS that predicts node failures and self-heals before data is lost."*

---

## ✨ Key Features

- **Distributed File Storage** — Files are chunked (4MB) and distributed across multiple data nodes with configurable replication (default: 3x)
- **Predictive Health Monitoring** — Sliding-window trend analysis with linear regression on 6 health metrics per node
- **Proactive Self-Healing** — Automatic chunk migration when a node is predicted to fail, before any data loss
- **Risk Scoring Engine** — Weighted multi-metric risk scores (0.0–1.0) with thresholds: HEALTHY → WARNING → AT_RISK → CRITICAL
- **gRPC Communication** — Type-safe, efficient inter-node communication using Protocol Buffers
- **Parallel Transfers** — Concurrent chunk uploads/downloads using goroutines
- **Fault Injection** — Built-in chaos testing via HTTP admin API to simulate disk degradation
- **Live Monitoring** — Real-time cluster dashboard with risk visualization
- **Docker Support** — One-command 5-node cluster deployment

---

## 🏗️ Architecture

```
                          ┌───────────────────────────────────┐
                          │        Metadata Server            │
                          │  ┌─────────────┬───────────────┐  │
    ┌──────────┐   gRPC   │  │  Namespace  │ Chunk Manager │  │
    │  Client   │◄────────►│  │  Manager    │ (Placement &  │  │
    │  (CLI)    │          │  │  (File Tree)│  Replication) │  │
    └──────────┘          │  └─────────────┴───────┬───────┘  │
                          │                        │          │
                          │  ┌─────────────────────▼───────┐  │
                          │  │   Health Monitor &           │  │
                          │  │   Prediction Engine          │  │
                          │  │   ├─ Sliding Windows (5m/15m)│  │
                          │  │   ├─ Linear Regression       │  │
                          │  │   ├─ Risk Scoring            │  │
                          │  │   └─ Migration Trigger       │  │
                          │  └─────────────────────────────┘  │
                          └───────────────┬───────────────────┘
                                          │ gRPC
                    ┌─────────────────────┼─────────────────────┐
                    │                     │                     │
             ┌──────▼──────┐       ┌──────▼──────┐      ┌──────▼──────┐
             │ Data Node 1 │       │ Data Node 2 │      │ Data Node N │
             │ ┌──────────┐│       │ ┌──────────┐│      │ ┌──────────┐│
             │ │  Chunk    ││       │ │  Chunk    ││      │ │  Chunk    ││
             │ │  Store    ││       │ │  Store    ││      │ │  Store    ││
             │ ├──────────┤│       │ ├──────────┤│      │ ├──────────┤│
             │ │  Health   ││       │ │  Health   ││      │ │  Health   ││
             │ │  Metrics  ││       │ │  Metrics  ││      │ │  Metrics  ││
             │ ├──────────┤│       │ ├──────────┤│      │ ├──────────┤│
             │ │  Admin    ││       │ │  Admin    ││      │ │  Admin    ││
             │ │  (Chaos)  ││       │ │  (Chaos)  ││      │ │  (Chaos)  ││
             │ └──────────┘│       │ └──────────┘│      │ └──────────┘│
             └─────────────┘       └─────────────┘      └─────────────┘
```

---

## 🧠 How Predictive Self-Healing Works

### 1. Continuous Monitoring
Every data node reports 6 health metrics to the metadata server every 5 seconds:
- Disk I/O latency, Disk utilization, Error rate
- gRPC response time, CPU usage, Memory usage

### 2. Trend Analysis
The prediction engine maintains **sliding windows** (5min, 15min) for each metric and computes:
- **Current Value Risk (40%)** — How close is the metric to its danger threshold?
- **Trend Risk (40%)** — Linear regression slope: is the metric getting worse over time?
- **Volatility Risk (20%)** — Standard deviation: is the metric unstable?

### 3. Risk Scoring
Each metric's risk is combined using domain-specific weights:

| Metric | Weight | Threshold |
|--------|--------|-----------|
| Disk I/O Latency | 25% | 50ms |
| Error Rate | 25% | 5/interval |
| Disk Utilization | 20% | 90% |
| Response Time | 15% | 30ms |
| Memory Usage | 10% | 90% |
| CPU Usage | 5% | 90% |

### 4. Proactive Migration
When a node's overall risk exceeds the critical threshold:
1. All chunks on the at-risk node are identified
2. For each chunk, a healthy target node (that doesn't already have a copy) is selected
3. Node-to-node chunk transfer is triggered via gRPC
4. Metadata mappings are updated atomically
5. Complete migration logged with timing and reasoning

**Result:** Data is safely relocated *before* the node fails. Zero data loss.

---

## 🚀 Quick Start

### Local Development

```bash
# Clone
git clone https://github.com/srujankothuri/SentinelFS.git
cd SentinelFS

# Build
go build ./...

# Terminal 1: Start metadata server
go run ./cmd/metaserver --port 9000

# Terminal 2-4: Start data nodes
go run ./cmd/datanode --meta-addr localhost:9000 --port 9001 --admin-port 9501 --data-dir ./data/node1
go run ./cmd/datanode --meta-addr localhost:9000 --port 9002 --admin-port 9502 --data-dir ./data/node2
go run ./cmd/datanode --meta-addr localhost:9000 --port 9003 --admin-port 9503 --data-dir ./data/node3

# Terminal 5: Use the CLI
go run ./cmd/client put ./myfile.txt /data/myfile.txt
go run ./cmd/client get /data/myfile.txt ./downloaded.txt
go run ./cmd/client cluster
go run ./cmd/client watch
```

### Docker (One Command)

```bash
docker-compose up --build -d

# Test with client container
docker exec sentinel-client sentinelfs cluster
docker exec sentinel-client sh -c "echo 'test' > /tmp/t.txt && sentinelfs put /tmp/t.txt /data/t.txt"
docker exec sentinel-client sentinelfs nodes

# Trigger chaos
curl -X POST "http://localhost:9503/degrade?speed=2.0"

# Cleanup
docker-compose down -v
```

---

## 🔥 Chaos Testing Demo

SentinelFS includes built-in fault injection for demonstrating predictive self-healing:

```bash
# 1. Start cluster and upload files

# 2. Trigger disk degradation on a node
curl -X POST "http://localhost:9503/degrade?speed=2.0"

# 3. Watch the live dashboard
go run ./cmd/client watch

# You'll see:
#   🟢 HEALTHY → 🟡 WARNING → 🟠 AT_RISK → 🔴 CRITICAL
#   Then: "PROACTIVE MIGRATION STARTED"
#   Chunks migrate to healthy nodes automatically

# 4. Verify data integrity — files still downloadable
go run ./cmd/client get /data/myfile.txt ./verify.txt

# 5. Stop degradation
curl -X POST "http://localhost:9503/recover"
```

**Automated test:**
```bash
./scripts/chaos_test.sh
```

---

## 📋 CLI Reference

| Command | Description |
|---------|-------------|
| `sentinelfs put <local> <remote>` | Upload a file to the cluster |
| `sentinelfs get <remote> <local>` | Download a file from the cluster |
| `sentinelfs ls [path]` | List files (default: /) |
| `sentinelfs rm <remote>` | Delete a file |
| `sentinelfs info <remote>` | Show chunk locations and replication |
| `sentinelfs cluster` | Cluster health overview |
| `sentinelfs nodes` | Per-node health and risk scores |
| `sentinelfs watch [interval]` | Live monitoring dashboard |

---

## ⚙️ Tech Stack

| Component | Technology |
|-----------|-----------|
| Language | Go 1.22+ |
| Communication | gRPC + Protocol Buffers |
| Prediction | Linear regression, sliding windows |
| Containerization | Docker + Docker Compose |
| Logging | Go `slog` (structured) |
| Testing | Shell-based integration + chaos tests |

---

## 📁 Project Structure

```
sentinelfs/
├── proto/                          # Protocol Buffer definitions
│   └── sentinelfs.proto
├── cmd/
│   ├── metaserver/main.go          # Metadata server entry point
│   ├── datanode/main.go            # Data node entry point
│   └── client/main.go              # CLI client
├── internal/
│   ├── metaserver/                 # Metadata server logic
│   │   ├── server.go               #   gRPC server + prediction loop
│   │   ├── namespace.go            #   File/directory tree
│   │   ├── chunk_manager.go        #   Chunk placement & tracking
│   │   └── locator_adapter.go      #   Bridge to health subsystem
│   ├── datanode/                   # Data node logic
│   │   ├── server.go               #   gRPC server + heartbeat
│   │   ├── store.go                #   Chunk storage engine
│   │   ├── health.go               #   Health metrics collector
│   │   └── admin.go                #   Chaos testing HTTP API
│   ├── health/                     # Prediction engine
│   │   ├── monitor.go              #   Metrics aggregator
│   │   ├── predictor.go            #   Risk scoring + trend analysis
│   │   ├── migrator.go             #   Proactive migration
│   │   └── metrics.go              #   Sliding window data structures
│   └── client/                     # Client library
│       ├── client.go               #   Core operations
│       ├── transfer.go             #   Parallel chunk transfer
│       └── watch.go                #   Live monitoring
├── scripts/
│   ├── demo.sh                     # Full demo script
│   ├── chaos.sh                    # Fault injection helper
│   ├── chaos_test.sh               # Automated chaos test
│   └── integration_test.sh         # End-to-end integration test
├── docker-compose.yml
├── Dockerfile.metaserver
├── Dockerfile.datanode
├── Dockerfile.client
└── Makefile
```

---

## 💡 Design Decisions

| Decision | Rationale |
|----------|-----------|
| **Go** | Industry standard for distributed systems (Docker, K8s, etcd). Goroutines ideal for concurrent chunk transfers and health monitoring. |
| **gRPC** | Type-safe contracts, efficient binary serialization, streaming support for chunk transfers. |
| **Statistical prediction over ML/DL** | Lightweight, interpretable, runs in-process with no Python dependency. Linear regression on time-series is what production monitoring systems (Prometheus, Datadog) actually use. Overcomplexity signals poor engineering judgment. |
| **Proactive migration over increased replication** | Migration preserves the replication factor budget. Adding replicas is wasteful and doesn't scale. |
| **Sliding windows over full history** | Bounded memory usage, recent data weighted more heavily, natural expiration of stale metrics. |
| **Strong metadata consistency** | Single metadata server ensures consistent file→chunk mappings. Acceptable trade-off for a system optimizing for data availability. |

---

## 📊 Performance

Tested with 5 data nodes managing files up to 1MB:

| Metric | Result |
|--------|--------|
| Prediction Detection | Risk escalation detected within 5s of degradation |
| Migration Speed | 4 chunks migrated in ~340ms (node-to-node gRPC) |
| Data Integrity | 100% — all files verified via SHA-256 checksums after migration |
| Cluster Scale | Tested with 5 data nodes, 6+ files, 18+ chunk replicas |

---

## License

MIT