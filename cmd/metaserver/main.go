package main

import (
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/srujankothuri/SentinelFS/internal/metaserver"
)

func main() {
	port := flag.Int("port", 9000, "metadata server gRPC port")
	adminPort := flag.Int("admin-port", 9600, "admin HTTP port")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	srv := metaserver.NewServer(*port, *adminPort)

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		slog.Info("shutting down metadata server")
		srv.Stop()
	}()

	if err := srv.Start(); err != nil {
		slog.Error("metadata server failed", "error", err)
		os.Exit(1)
	}
}