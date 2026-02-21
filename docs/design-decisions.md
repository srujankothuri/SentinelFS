# Design Decisions

This document explains the key technical decisions in SentinelFS and the reasoning behind them.

## 1. Why Go?

Go is the industry standard for distributed systems — Docker, Kubernetes, etcd, CockroachDB, and Consul are all written in Go. Specific advantages for SentinelFS:

- **Goroutines**: Lightweight concurrency for parallel chunk transfers, heartbeat loops, and health monitoring — all running simultaneously without complex thread management.
- **Strong standard library**: `net`, `crypto/sha256`, `sync`, `log/slog` cover most needs without external dependencies.
- **Static binaries**: Single binary deployment, perfect for Docker containers.
- **gRPC ecosystem**: First-class protobuf and gRPC support.

## 2. Why gRPC over REST/raw sockets?

- **Type safety**: Protocol Buffers enforce contracts between services at compile time. Changes to the wire format are caught immediately.
- **Efficient serialization**: Binary protobuf is 3-10x smaller and faster than JSON for chunk data transfer.
- **Streaming**: gRPC supports bidirectional streaming, useful for future chunk streaming optimizations.
- **Code generation**: `protoc` generates both client and server stubs, reducing boilerplate.

## 3. Why statistical prediction over deep learning?

This is perhaps the most important design decision. Many portfolio projects would reach for TensorFlow or PyTorch here. We deliberately chose simple statistics:

- **Interpretability**: Linear regression slopes and standard deviations produce human-readable explanations ("Disk I/O latency increasing at +2.5ms/s"). Black-box models can't do this.
- **Lightweight**: Runs in-process in Go with ~100 lines of code. No Python runtime, no model files, no inference latency.
- **Production reality**: Prometheus alerting rules, Datadog monitors, and AWS CloudWatch alarms all use threshold-based statistical methods — not neural networks. Our approach mirrors what actually runs in production.
- **Sufficient accuracy**: For detecting disk degradation trends, linear regression on a 5-minute window is more than adequate. The failure mode is gradual, not sudden.
- **No training data needed**: Deep learning requires labeled failure datasets we don't have. Statistical methods work from first principles.

## 4. Why proactive migration over increased replication?

When a node is predicted to fail, we could either:
- **Option A**: Create additional replicas of its chunks on other nodes (increase replication factor temporarily)
- **Option B**: Move chunks off the risky node entirely (proactive migration)

We chose Option B because:
- **Budget neutral**: Doesn't consume extra storage. The replication factor stays at 3.
- **Clean semantics**: After migration, the risky node is no longer responsible for any data. If it dies, nothing is lost.
- **Scalable**: Increasing replication factor under pressure doesn't scale — every new failure would compound storage usage.

## 5. Why sliding windows over full metric history?

- **Bounded memory**: A 1-hour window with 5-second intervals = 720 points per metric per node. Predictable memory usage regardless of uptime.
- **Recency weighting**: Recent metrics matter more than hour-old data for failure prediction. Sliding windows naturally emphasize recent trends.
- **Natural expiration**: Old metrics automatically fall out of the window. No garbage collection needed.

## 6. Why 4MB chunk size?

- **Balance**: Small enough for parallelism (multiple chunks per file = multiple goroutines), large enough to avoid excessive metadata overhead.
- **Industry alignment**: GFS uses 64MB, HDFS uses 128MB — but those are for petabyte-scale clusters. For a portfolio-scale project, 4MB provides meaningful chunking for smaller test files.

## 7. Why single metadata server?

- **Simplicity**: A distributed metadata layer (Raft consensus, sharded metadata) is a project unto itself. Single server keeps the focus on the predictive self-healing innovation.
- **Strong consistency**: Single server means no split-brain, no consensus delays, no distributed transaction complexity.
- **Known limitation**: Single point of failure for metadata. Documented as a future improvement — could add Raft-based replication for the metadata server.

## 8. Why HTTP admin API for chaos testing?

- **Simplicity**: HTTP is universal — `curl` commands work everywhere, no special tooling needed.
- **Separation**: Chaos controls are isolated from the gRPC data path. No risk of test endpoints being called in production.
- **Scriptable**: Easy to integrate into shell scripts for automated chaos testing.

## Future Improvements

- Raft consensus for metadata server high availability
- Erasure coding as alternative to full replication
- Chunk-level streaming for large file transfers
- Persistent metadata (currently in-memory only)
- Web-based monitoring dashboard
- Cross-datacenter replication simulation