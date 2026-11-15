package output

import (
	"fmt"
	"io"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// WriteOrgConnectTransport is the human view of an organization's
// remote-session transport: the setting, the relay, and the relays offered.
func WriteOrgConnectTransport(w io.Writer, t *models.OrgConnectTransport) {
	fmt.Fprintf(w, "Organization: %s\n", t.OrganizationName)
	fmt.Fprintf(w, "Transport:    %s (%s)\n", models.ConnectTransportName(t.ConnectTransport), t.ConnectTransport)
	relay := "(none chosen)"
	if t.ConnectRelay != nil && *t.ConnectRelay != "" {
		relay = *t.ConnectRelay
	}
	fmt.Fprintf(w, "Relay:        %s\n", relay)
	if !t.DirectPeerAvailable {
		fmt.Fprintf(w, "%s is not available on this server.\n", models.ConnectTransportName(models.ConnectTransportDirectPeer))
	}
	if len(t.Relays) == 0 {
		return
	}
	fmt.Fprintln(w, "Relays offered:")
	for _, r := range t.Relays {
		line := "  " + r.ID
		if r.Label != "" {
			line += "  " + r.Label
		}
		fmt.Fprintf(w, "%s  %s:%d\n", line, r.Hostname, r.DERPPort)
	}
}

func (f *TableFormatter) FormatOrgConnectTransport(t *models.OrgConnectTransport) error {
	WriteOrgConnectTransport(f.Writer, t)
	return nil
}
func (f *SimpleFormatter) FormatOrgConnectTransport(t *models.OrgConnectTransport) error {
	WriteOrgConnectTransport(f.Writer, t)
	return nil
}
func (f *DetailedFormatter) FormatOrgConnectTransport(t *models.OrgConnectTransport) error {
	WriteOrgConnectTransport(f.Writer, t)
	return nil
}
func (f *JSONFormatter) FormatOrgConnectTransport(t *models.OrgConnectTransport) error {
	return f.output(t)
}
