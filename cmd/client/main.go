package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Println("SentinelFS CLI v0.1.0")

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	// TODO: Wire up cobra commands
	fmt.Println("Command:", os.Args[1])
}

func printUsage() {
	fmt.Println(`Usage: sentinelfs <command> [arguments]

Commands:
  put     <local_path> <remote_path>   Upload a file
  get     <remote_path> <local_path>   Download a file
  ls      <remote_path>                List files
  rm      <remote_path>                Delete a file
  info    <remote_path>                Show file chunk info
  cluster                              Show cluster health
  nodes                                Show node health details`)
}