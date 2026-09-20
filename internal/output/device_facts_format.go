package output

import (
	"fmt"
	"io"
	"strings"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// Device facts rendering.
//
// One ordered list of label/value pairs feeds every human-facing formatter,
// so `device describe` names the same facts in the same order whichever
// format the reader picked. The JSON formatter is not involved: it marshals
// models.Device and gets the raw map, unknown keys included.
//
// Nothing here ever prints a placeholder. A device whose agent does not
// report facts — or reports only some of them — renders exactly the lines it
// has and no others, matching how every other optional device field in this
// package behaves.

// deviceFactLines returns the facts worth showing a human, in display order,
// as label/value pairs. An empty result means "print no Facts section".
func deviceFactLines(d *models.Device) [][2]string {
	if d == nil || !d.HasFacts() {
		return nil
	}
	var lines [][2]string
	if tz, ok := d.TimezoneFact(); ok {
		lines = append(lines, [2]string{"Timezone", tz.Display()})
	}
	if ifaces := d.InterfacesSummary(); ifaces != "" {
		lines = append(lines, [2]string{"Interfaces", ifaces})
	}
	if opn, ok := d.OPNsenseFact(); ok {
		value := opn.Version
		if value == "" {
			value = opn.Series
		}
		lines = append(lines, [2]string{"OPNsense", value})
	}
	if os, ok := d.OSFact(); ok {
		lines = append(lines, [2]string{"OS", os.Display()})
	}
	if host, ok := d.HostnameFact(); ok {
		lines = append(lines, [2]string{"Hostname", host})
	}
	// A newer agent may report facts this binary has no layout for. Naming
	// the keys beats dropping them silently: the reader learns the data is
	// there and that `-f json` will show it.
	if unknown := d.UnknownFactKeys(); len(unknown) > 0 {
		lines = append(lines, [2]string{"Other", strings.Join(unknown, ", ") + " (see -f json)"})
	}
	return lines
}

// writeDeviceFactsBox renders the facts of one device as a titled "Facts"
// box under the device block, or writes nothing when there are none.
func writeDeviceFactsBox(f BaseFormatter, d *models.Device) {
	lines := deviceFactLines(d)
	if len(lines) == 0 {
		return
	}
	box := &fieldBox{title: "Facts"}
	for _, l := range lines {
		box.field(l[0], l[1])
	}
	fmt.Fprintln(f.Writer)
	box.render(f)
}

// writeDeviceFactLines prints already-collected facts as plain "Label: value"
// lines, the shape the table and simple formatters already use for every
// optional device field. It takes the lines rather than the device because
// both callers must test them for emptiness first — a heading with nothing
// under it is worse than no section. indent is the leading whitespace and
// labelWidth pads the label so the values line up with the block above.
func writeDeviceFactLines(w io.Writer, lines [][2]string, indent string, labelWidth int) {
	for _, l := range lines {
		fmt.Fprintf(w, "%s%-*s %s\n", indent, labelWidth, l[0]+":", l[1])
	}
}
