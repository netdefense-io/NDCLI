// Package resources implements registry.Resource for every NetDefense domain
// surfaced in the TUI. Each domain lives in its own file and is wired up by
// RegisterAll. The package depends on registry, service, models and uihelp —
// never on the tui package — so it can be registered from there without a
// cycle.
package resources

import (
	"time"

	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/service"
	"github.com/netdefense-io/NDCLI/internal/tui/registry"
	"github.com/netdefense-io/NDCLI/internal/tui/uihelp"
)

// RegisterAll registers every domain resource in display order. The order
// here drives the command palette and any resource list.
func RegisterAll(r *registry.Registry) {
	r.Register(deviceResource{})
	r.Register(taskResource{})
	r.Register(syncResource{})
	r.Register(orgResource{})
	r.Register(ouResource{})
	r.Register(templateResource{})
	r.Register(snippetResource{})
	r.Register(softwareResource{})
	r.Register(networkResource{})
	// Org-scope variables are editable here; device-scope overrides are managed
	// from the device page via ScopedVarResource{Scope: VarScopeDevice}.
	r.Register(ScopedVarResource{Scope: service.VarScopeOrg, Name: "Variables", KindID: "variable"})
	r.Register(scheduleResource{})
	r.Register(backupResource{})
	r.Register(accountResource{})
}

// relAge renders a FlexibleTime as a short "12s / 4m / 2h" age, or an em dash
// when zero/unset.
func relAge(t models.FlexibleTime) string {
	if t.IsZero() {
		return "—"
	}
	return uihelp.HumanDuration(int64(time.Since(t.Time).Seconds()))
}

// relAgePtr is the pointer variant used by nilable timestamp fields.
func relAgePtr(t *models.FlexibleTime) string {
	if t == nil || t.IsZero() {
		return "—"
	}
	return uihelp.HumanDuration(int64(time.Since(t.Time).Seconds()))
}

// fullTime renders an absolute timestamp for detail views.
func fullTime(t models.FlexibleTime) string {
	if t.IsZero() {
		return "—"
	}
	return t.Time.Format("2006-01-02 15:04:05")
}

// ago renders a past timestamp as "12s ago", or a bare "—" when the time is
// zero/unset or in the future (so labels never read "— ago").
func ago(t models.FlexibleTime) string {
	if t.IsZero() {
		return "—"
	}
	secs := int64(time.Since(t.Time).Seconds())
	if secs < 0 {
		return "—"
	}
	return uihelp.HumanDuration(secs) + " ago"
}

// agoPtr is the pointer variant of ago.
func agoPtr(t *models.FlexibleTime) string {
	if t == nil {
		return "—"
	}
	return ago(*t)
}
