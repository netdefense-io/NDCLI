package main

import (
	"fmt"
	"os"

	"github.com/netdefense-io/NDCLI/internal/config"
	"github.com/netdefense-io/NDCLI/internal/tui"
)

func main() {
	if err := tui.Run(os.Args[1:], config.Version); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
