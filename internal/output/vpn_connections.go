package output

import (
	"fmt"
	"io"
	"strings"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// writeConnectionsSummary prints the line that ends the human views of a
// connection list: the total, then how many are automatic, manual links and
// overrides. The JSON formatter carries the total as a field instead.
func writeConnectionsSummary(w io.Writer, connections []models.EffectiveConnection) {
	automatic, manualLinks, overrides := 0, 0, 0
	for _, c := range connections {
		if c.Source == "implicit" {
			automatic++
			if c.HasOverride {
				overrides++
			}
		} else {
			manualLinks++
		}
	}
	connWord := "connections"
	if len(connections) == 1 {
		connWord = "connection"
	}
	var parts []string
	if automatic > 0 {
		parts = append(parts, fmt.Sprintf("%d automatic", automatic))
	}
	if manualLinks > 0 {
		word := "manual links"
		if manualLinks == 1 {
			word = "manual link"
		}
		parts = append(parts, fmt.Sprintf("%d %s", manualLinks, word))
	}
	if overrides > 0 {
		word := "overrides"
		if overrides == 1 {
			word = "override"
		}
		parts = append(parts, fmt.Sprintf("%d %s", overrides, word))
	}
	ColorDim.Fprintf(w, "\n%d %s (%s)\n", len(connections), connWord, strings.Join(parts, ", "))
}
