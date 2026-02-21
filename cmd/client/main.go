package main

import (
	"fmt"
	"os"

	"github.com/srujankothuri/SentinelFS/internal/client"
)

const defaultMetaAddr = "localhost:9000"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	metaAddr := os.Getenv("SENTINEL_META_ADDR")
	if metaAddr == "" {
		metaAddr = defaultMetaAddr
	}

	c, err := client.New(metaAddr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer c.Close()

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "put":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: sentinelfs put <local_path> <remote_path>")
			os.Exit(1)
		}
		if err := c.PutFile(args[0], args[1]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	case "get":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: sentinelfs get <remote_path> <local_path>")
			os.Exit(1)
		}
		if err := c.GetFile(args[0], args[1]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	case "ls":
		path := "/"
		if len(args) > 0 {
			path = args[0]
		}
		if err := c.ListFiles(path); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	case "rm":
		if len(args) < 1 {
			fmt.Fprintln(os.Stderr, "Usage: sentinelfs rm <remote_path>")
			os.Exit(1)
		}
		if err := c.DeleteFile(args[0]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	case "info":
		if len(args) < 1 {
			fmt.Fprintln(os.Stderr, "Usage: sentinelfs info <remote_path>")
			os.Exit(1)
		}
		if err := c.FileInfo(args[0]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	case "cluster":
		if err := c.ClusterInfo(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	case "nodes":
		if err := c.NodeInfo(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`SentinelFS CLI v0.1.0 🛡️

Usage: sentinelfs <command> [arguments]

Commands:
  put     <local_path> <remote_path>   Upload a file
  get     <remote_path> <local_path>   Download a file
  ls      [remote_path]                List files (default: /)
  rm      <remote_path>                Delete a file
  info    <remote_path>                Show file chunk details
  cluster                              Show cluster health overview
  nodes                                Show per-node health details

Environment:
  SENTINEL_META_ADDR   Metadata server address (default: localhost:9000)`)
}