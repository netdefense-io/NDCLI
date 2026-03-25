package output

import (
	"fmt"
	"io"
	"os"

	"github.com/netdefense-io/NDCLI/internal/models"
)

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

// PrintPagination prints pagination info
func PrintPagination(current, total, perPage int) {
	if total > perPage {
		totalPages := (total + perPage - 1) / perPage
		ColorDim.Printf("\nPage %d of %d (showing %d of %d items)\n", current, totalPages, perPage, total)
	}
}
