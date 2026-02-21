.PHONY: proto build clean run-meta run-datanode run-client test fmt lint

BINARY_DIR=bin
PROTO_DIR=proto
GO=go

# Build all binaries
build: proto
	$(GO) build -o $(BINARY_DIR)/metaserver ./cmd/metaserver
	$(GO) build -o $(BINARY_DIR)/datanode ./cmd/datanode
	$(GO) build -o $(BINARY_DIR)/sentinelfs ./cmd/client

# Generate protobuf code
proto:
	protoc --go_out=. --go-grpc_out=. $(PROTO_DIR)/sentinelfs.proto

# Run components
run-meta:
	$(GO) run ./cmd/metaserver --port 9000

run-datanode:
	$(GO) run ./cmd/datanode --meta-addr localhost:9000 --port 9001 --data-dir ./data/node1

run-client:
	$(GO) run ./cmd/client

# Testing
test:
	$(GO) test ./... -v -count=1

test-cover:
	$(GO) test ./... -coverprofile=coverage.out
	$(GO) tool cover -html=coverage.out

# Code quality
fmt:
	$(GO) fmt ./...

lint:
	golangci-lint run ./...

# Cleanup
clean:
	rm -rf $(BINARY_DIR) data/ coverage.out