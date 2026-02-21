package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
)

func main() {
	port := flag.Int("port", 9001, "data node port")
	metaAddr := flag.String("meta-addr", "localhost:9000", "metadata server address")
	dataDir := flag.String("data-dir", "./data/node1", "directory for chunk storage")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	slog.Info("starting SentinelFS data node",
		"port", *port,
		"meta-addr", *metaAddr,
		"data-dir", *dataDir,
	)
	fmt.Printf("SentinelFS Data Node listening on :%d\n", *port)

	// TODO: Initialize and start gRPC server
}