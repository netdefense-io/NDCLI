package main

import (
	"os"

	"github.com/netdefense-io/NDCLI/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
