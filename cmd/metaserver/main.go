package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
)

func main() {
	port := flag.Int("port", 9000, "metadata server port")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	slog.Info("starting SentinelFS metadata server", "port", *port)
	fmt.Printf("SentinelFS Metadata Server listening on :%d\n", *port)

	// TODO: Initialize and start gRPC server
}