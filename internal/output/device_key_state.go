package output

import (
	"fmt"
	"io"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// keyMismatchBadge is a func so the color setting is read at render time.
func keyMismatchBadge() string { return ColorError.Sprint("[KEY MISMATCH]") }

// keyMismatchText is the one-line warning shared by every human format.
func keyMismatchText(d *models.Device) string {
	return ColorError.Sprint("KEY MISMATCH - rebind required:") + " " + ColorWarning.Sprint(d.RebindCommand())
}

// writeKeyMismatchDetail writes the evidence lines, when the control plane
// sent any.
func writeKeyMismatchDetail(w io.Writer, d *models.Device, indent string) {
	k := d.KeyStateDetail
	if k == nil {
		return
	}
	if k.BoundKID != "" || k.PresentedKID != "" {
		fmt.Fprintf(w, "%sbound kid: %s, presented kid: %s\n", indent, k.BoundKID, k.PresentedKID)
	}
	if k.Since != "" {
		fmt.Fprintf(w, "%sdetected via %s since %s\n", indent, k.Source, k.Since)
	}
}
