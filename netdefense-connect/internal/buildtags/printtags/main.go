// printtags prints the release build tags, to regenerate build-tags.txt:
//
//	go run ./internal/buildtags/printtags > build-tags.txt
package main

import (
	"fmt"

	"github.com/netdefense-io/NDCLI/netdefense-connect/internal/buildtags"
)

func main() {
	fmt.Println(buildtags.Release())
}
