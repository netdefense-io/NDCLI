package cli

import (
	"context"

	"github.com/spf13/cobra"
)

// `ndcli dashboard` — org-level roll-up backed by
// GET /api/v1/organizations/{org}/dashboard. One round trip; all four
// output formats render the same data.
var dashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Fleet dashboard roll-up for the current organization",
	Long: `Show counters and the compact fleet table for the current organization.

Surfaces what a tech needs at-a-glance: how many devices are online,
which ones have failed their last sync, what tasks ran in the last 24h,
and the per-device attention badges (services down, pending updates,
certs expiring soon). The compact rows are sorted by attention rank so
the row most likely to be on fire is on top.

Render with --format table (default), simple, detailed, or json.`,
	RunE: runDashboard,
}

func runDashboard(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	result, err := svc.Dashboard(context.Background(), org)
	if err != nil {
		return err
	}
	return formatter.FormatDashboard(result)
}
