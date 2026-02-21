package main

import (
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/srujankothuri/SentinelFS/internal/datanode"
)

func main() {
	port := flag.Int("port", 9001, "data node port")
	metaAddr := flag.String("meta-addr", "localhost:9000", "metadata server address")
	dataDir := flag.String("data-dir", "./data/node1", "directory for chunk storage")
	capacity := flag.Int64("capacity", 10*1024*1024*1024, "storage capacity in bytes") // 10GB default
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	srv, err := datanode.NewServer(*port, *metaAddr, *dataDir, *capacity)
	if err != nil {
		slog.Error("failed to create data node", "error", err)
		os.Exit(1)
	}

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		slog.Info("shutting down data node")
		srv.Stop()
	}()

	if err := srv.Start(); err != nil {
		slog.Error("data node failed", "error", err)
		os.Exit(1)
	}
}