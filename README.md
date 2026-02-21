# SentinelFS 🛡️

A **Predictive Self-Healing Distributed File System** built in Go.

SentinelFS goes beyond traditional reactive fault tolerance. It continuously monitors data node health metrics, uses statistical trend analysis to **predict node failures before they happen**, and **proactively migrates data** to healthy nodes — achieving zero data loss without waiting for a crash.

## Features

- **Distributed File Storage** — Files are chunked and distributed across multiple data nodes with configurable replication
- **gRPC Communication** — Type-safe, efficient inter-node communication using Protocol Buffers
- **Predictive Health Monitoring** — Sliding-window trend analysis with linear regression on node health metrics
- **Proactive Self-Healing** — Automatic chunk migration when a node is predicted to fail, before any data loss occurs
- **Parallel Transfers** — Concurrent chunk uploads and downloads using goroutines
- **Fault Injection** — Built-in chaos testing to simulate disk degradation and validate self-healing

## Architecture

```
  Client (CLI)
       │
       ▼ gRPC
  Metadata Server ◄── Health Monitor & Prediction Engine
       │                        ▲
       ▼ gRPC                   │ Metrics Stream
  ┌────┴────┬────────┐          │
  ▼         ▼        ▼          │
Node 1   Node 2   Node 3 ──────┘
```

## Quick Start

```bash
# Build
make build

# Start metadata server
make run-meta

# Start data nodes (in separate terminals)
make run-datanode

# Use the CLI
./bin/sentinelfs put ./myfile.txt /data/myfile.txt
./bin/sentinelfs get /data/myfile.txt ./downloaded.txt
./bin/sentinelfs cluster
```

## Tech Stack

| Component | Technology |
|-----------|-----------|
| Language | Go 1.22+ |
| Communication | gRPC + Protocol Buffers |
| Prediction | Linear regression (gonum) |
| Containerization | Docker + Docker Compose |
| CLI Framework | Cobra |

## Project Status

🚧 Under active development

## License

MIT