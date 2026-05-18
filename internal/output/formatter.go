package output

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// sortedSnippetNames returns the snippet-name keys of an undefined-variables-by-snippet
// map in deterministic order, for consistent rendering across table/simple/detailed.
func sortedSnippetNames(m map[string][]string) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// leadingSpaces counts the number of leading ASCII space characters in s.
func leadingSpaces(s string) int {
	n := 0
	for n < len(s) && s[n] == ' ' {
		n++
	}
	return n
}

// wrapMessageLines wraps each \n-separated paragraph in s to fit width,
// preserving any internal whitespace that aligns columns. Lines that
// already fit pass through verbatim. Lines that exceed width are broken
// at the last space at or before the limit; if no such space exists the
// break is hard at the limit. Continuation lines are indented two
// spaces past the original line's leading whitespace so wrapped change
// rows still sit under their name column.
func wrapMessageLines(s string, width int) []string {
	if width < 1 {
		width = 1
	}
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if len(line) <= width {
			out = append(out, line)
			continue
		}
		hanging := strings.Repeat(" ", leadingSpaces(line)+2)
		for len(line) > width {
			// Find last space at or before width to break on.
			cut := strings.LastIndex(line[:width], " ")
			if cut <= leadingSpaces(line) {
				cut = width
			}
			out = append(out, strings.TrimRight(line[:cut], " "))
			line = hanging + strings.TrimLeft(line[cut:], " ")
		}
		out = append(out, line)
	}
	return out
}

// wrapToWidth splits s into lines no longer than width chars, preferring
// space boundaries. Embedded newlines are preserved; tokens longer than
// width are hard-split. Used to display multi-line messages inside fixed-width
// boxes without truncating (see detailed.FormatTask).
func wrapToWidth(s string, width int) []string {
	if width < 1 {
		width = 1
	}
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if len(line) <= width {
			out = append(out, line)
			continue
		}
		cur := ""
		for _, w := range strings.Fields(line) {
			if len(w) > width {
				if cur != "" {
					out = append(out, cur)
					cur = ""
				}
				for len(w) > width {
					out = append(out, w[:width])
					w = w[width:]
				}
				cur = w
				continue
			}
			switch {
			case cur == "":
				cur = w
			case len(cur)+1+len(w) <= width:
				cur = cur + " " + w
			default:
				out = append(out, cur)
				cur = w
			}
		}
		if cur != "" {
			out = append(out, cur)
		}
	}
	return out
}

// Format represents the output format type
type Format string

const (
	FormatSimple   Format = "simple"
	FormatDetailed Format = "detailed"
	FormatTable    Format = "table"
	FormatJSON     Format = "json"
)

// Formatter defines the interface for output formatters
type Formatter interface {
	// Devices
	FormatDevices(devices []models.Device, total int, quota *models.Quota) error
	FormatDevice(device *models.Device) error

	// Tasks
	FormatTasks(tasks []models.Task, total int) error
	FormatTask(task *models.Task) error
	FormatRunResult(result *models.RunResult) error

	// Organizations
	FormatOrganizations(orgs []models.Organization) error
	FormatOrganization(org *models.Organization) error
	FormatOrgQuota(quota *models.OrgQuota) error

	// Organizational Units
	FormatOUs(ous []models.OrganizationalUnit) error
	FormatOU(ou *models.OrganizationalUnit) error

	// Templates
	FormatTemplates(templates []models.Template) error
	FormatTemplate(template *models.Template) error

	// Snippets
	FormatSnippets(snippets []models.Snippet) error
	FormatSnippet(snippet *models.Snippet) error

	// Accounts
	FormatAccounts(accounts []models.Account, quota *models.Quota) error

	// Invitations
	FormatInvitations(invitations []models.Invitation) error
	FormatInvites(invites *models.InvitesResponse) error

	// Auth
	FormatAuthMe(authMe *models.AuthMe) error
	FormatAuthMeUpdate(resp *models.AuthMeUpdateResponse) error

	// Sync
	FormatSyncStatus(items []models.SyncStatusItem, total int) error
	FormatSyncApply(result *models.SyncApplyResponse) error

	// Variables
	FormatVariables(variables []models.Variable, total int) error
	FormatVariable(variable *models.Variable) error
	FormatVariableOverview(items []models.VariableOverview, total int) error

	// VPN Networks
	FormatVpnNetworks(networks []models.VpnNetwork, total int, quota *models.Quota) error
	FormatVpnNetwork(network *models.VpnNetwork) error

	// VPN Members
	FormatVpnMembers(members []models.VpnMember, total int) error
	FormatVpnMember(member *models.VpnMember) error

	// VPN Links
	FormatVpnLinks(links []models.VpnLink, total int) error
	FormatVpnLink(link *models.VpnLink) error

	// VPN Effective Connections (computed)
	FormatVpnConnections(connections []models.EffectiveConnection, total int) error
	FormatVpnConnection(connection *models.EffectiveConnection) error

	// VPN Prefixes
	FormatVpnPrefixes(prefixes []models.VpnMemberPrefix, total int) error
	FormatVpnPrefix(prefix *models.VpnMemberPrefix) error

	// Backup
	FormatBackupConfig(config *models.BackupConfig) error
	FormatBackupConfigTest(result *models.BackupConfigTestResponse) error
	FormatDeviceBackupStatuses(statuses []models.DeviceBackupStatus, total int, enabledCount int) error
	FormatDeviceBackupStatus(status *models.DeviceBackupStatus) error

	// Messages
	Success(message string)
	Error(message string)
	Warning(message string)
	Info(message string)
}

// BaseFormatter provides common functionality for all formatters
type BaseFormatter struct {
	Writer io.Writer
}

// NewBaseFormatter creates a new base formatter
func NewBaseFormatter() BaseFormatter {
	return BaseFormatter{Writer: os.Stdout}
}

// Success prints a success message
func (f *BaseFormatter) Success(message string) {
	ColorSuccess.Fprint(f.Writer, "✓ ")
	fmt.Fprintln(f.Writer, message)
}

// Error prints an error message
func (f *BaseFormatter) Error(message string) {
	ColorError.Fprint(f.Writer, "✗ ")
	fmt.Fprintln(f.Writer, message)
}

// Warning prints a warning message
func (f *BaseFormatter) Warning(message string) {
	ColorWarning.Fprint(f.Writer, "⚠ ")
	fmt.Fprintln(f.Writer, message)
}

// Info prints an info message
func (f *BaseFormatter) Info(message string) {
	ColorInfo.Fprint(f.Writer, "ℹ ")
	fmt.Fprintln(f.Writer, message)
}

// GetFormatter returns the appropriate formatter for the given format
func GetFormatter(format string) Formatter {
	switch Format(format) {
	case FormatSimple:
		return NewSimpleFormatter()
	case FormatDetailed:
		return NewDetailedFormatter()
	case FormatTable:
		return NewTableFormatter()
	case FormatJSON:
		return NewJSONFormatter()
	default:
		return NewTableFormatter()
	}
}

// runResultHeader builds the one-line summary used by every text formatter
// for `ndcli run` output. JSON serializes the model directly. Plural is
// real (not the parenthesized "task(s)"), and the scheduled-fire timestamp
// is rendered in the user's display tz.
func runResultHeader(result *models.RunResult) string {
	noun := "task"
	if result.Total != 1 {
		noun = "tasks"
	}
	if result.ScheduledAt != "" {
		return fmt.Sprintf("Scheduled %d %s %s in org %q — fires at %s",
			result.Total, result.Type, noun, result.Organization,
			FormatTimestamp(parseRunTime(result.ScheduledAt)))
	}
	return fmt.Sprintf("Created %d %s %s in org %q",
		result.Total, result.Type, noun, result.Organization)
}

// parseRunTime parses an RFC3339 timestamp into time.Time. Returns the
// zero value (which FormatTimestamp renders as "-") on parse failure or
// empty input — never panics, since render-path failures shouldn't break
// a successful task creation.
func parseRunTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	return time.Time{}
}

// PrintPagination prints pagination info
func PrintPagination(current, total, perPage int) {
	if total > perPage {
		totalPages := (total + perPage - 1) / perPage
		ColorDim.Printf("\nPage %d of %d (showing %d of %d items)\n", current, totalPages, perPage, total)
	}
}
