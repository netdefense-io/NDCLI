package main

import (
	"fmt"
	"os"

	"github.com/netdefense-io/NDCLI/internal/mcp"
)

func main() {
	// Create and start the MCP server
	server, err := mcp.NewServer()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize MCP server: %v\n", err)
		os.Exit(1)
	}

	// Start serving on stdio
	if err := server.Serve(); err != nil {
		fmt.Fprintf(os.Stderr, "MCP server error: %v\n", err)
		os.Exit(1)
	}
}
