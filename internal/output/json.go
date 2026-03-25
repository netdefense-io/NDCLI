package output

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// JSONFormatter formats output as JSON
type JSONFormatter struct {
	BaseFormatter
	Indent bool
}

// NewJSONFormatter creates a new JSON formatter
func NewJSONFormatter() *JSONFormatter {
	return &JSONFormatter{
		BaseFormatter: NewBaseFormatter(),
		Indent:        true,
	}
}

func (f *JSONFormatter) output(data interface{}) error {
	var out []byte
	var err error

	if f.Indent {
		out, err = json.MarshalIndent(data, "", "  ")
	} else {
		out, err = json.Marshal(data)
	}

	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	fmt.Fprintln(f.Writer, string(out))
	return nil
}

// FormatDevices formats devices as JSON
func (f *JSONFormatter) FormatDevices(devices []models.Device, total int, quota *models.Quota) error {
	out := map[string]interface{}{
		"devices": devices,
		"total":   total,
	}
	if quota != nil {
		out["quota"] = quota
	}
	return f.output(out)
}

// FormatDevice formats a single device as JSON
func (f *JSONFormatter) FormatDevice(device *models.Device) error {
	return f.output(device)
}

// FormatTasks formats tasks as JSON
func (f *JSONFormatter) FormatTasks(tasks []models.Task, total int) error {
	return f.output(map[string]interface{}{
		"tasks": tasks,
		"total": total,
	})
}

// FormatTask formats a single task as JSON
func (f *JSONFormatter) FormatTask(task *models.Task) error {
	return f.output(task)
}

// FormatOrganizations formats organizations as JSON
func (f *JSONFormatter) FormatOrganizations(orgs []models.Organization) error {
	return f.output(map[string]interface{}{
		"organizations": orgs,
		"total":         len(orgs),
	})
}

// FormatOrganization formats a single organization as JSON
func (f *JSONFormatter) FormatOrganization(org *models.Organization) error {
	return f.output(org)
}

// FormatOUs formats organizational units as JSON
func (f *JSONFormatter) FormatOUs(ous []models.OrganizationalUnit) error {
	return f.output(map[string]interface{}{
		"organizational_units": ous,
		"total":                len(ous),
	})
}

// FormatOU formats a single organizational unit as JSON
func (f *JSONFormatter) FormatOU(ou *models.OrganizationalUnit) error {
	return f.output(ou)
}

// FormatTemplates formats templates as JSON
func (f *JSONFormatter) FormatTemplates(templates []models.Template) error {
	return f.output(map[string]interface{}{
		"templates": templates,
		"total":     len(templates),
	})
}

// FormatTemplate formats a single template as JSON
func (f *JSONFormatter) FormatTemplate(template *models.Template) error {
	return f.output(template)
}

// FormatSnippets formats snippets as JSON
func (f *JSONFormatter) FormatSnippets(snippets []models.Snippet) error {
	return f.output(map[string]interface{}{
		"snippets": snippets,
		"total":    len(snippets),
	})
}

// FormatSnippet formats a single snippet as JSON
func (f *JSONFormatter) FormatSnippet(snippet *models.Snippet) error {
	return f.output(snippet)
}

// FormatAccounts formats accounts as JSON
func (f *JSONFormatter) FormatAccounts(accounts []models.Account, quota *models.Quota) error {
	out := map[string]interface{}{
		"accounts": accounts,
		"total":    len(accounts),
	}
	if quota != nil {
		out["quota"] = quota
	}
	return f.output(out)
}

// FormatOrgQuota formats organization quota as JSON
func (f *JSONFormatter) FormatOrgQuota(quota *models.OrgQuota) error {
	return f.output(quota)
}

// FormatInvitations formats invitations as JSON
func (f *JSONFormatter) FormatInvitations(invitations []models.Invitation) error {
	return f.output(map[string]interface{}{
		"invitations": invitations,
		"total":       len(invitations),
	})
}

// FormatInvites formats the invites response as JSON
func (f *JSONFormatter) FormatInvites(invites *models.InvitesResponse) error {
	return f.output(invites)
}

// FormatAuthMe formats the authenticated user's profile as JSON
func (f *JSONFormatter) FormatAuthMe(authMe *models.AuthMe) error {
	return f.output(authMe)
}

// FormatAuthMeUpdate formats the auth me update response as JSON
func (f *JSONFormatter) FormatAuthMeUpdate(resp *models.AuthMeUpdateResponse) error {
	return f.output(resp)
}

// FormatSyncStatus formats sync status as JSON
func (f *JSONFormatter) FormatSyncStatus(items []models.SyncStatusItem, total int) error {
	return f.output(map[string]interface{}{
		"items": items,
		"total": total,
	})
}

// FormatSyncApply formats sync apply result as JSON
func (f *JSONFormatter) FormatSyncApply(result *models.SyncApplyResponse) error {
	return f.output(result)
}

// FormatVariables formats variables as JSON
func (f *JSONFormatter) FormatVariables(variables []models.Variable, total int) error {
	return f.output(map[string]interface{}{
		"variables": variables,
		"total":     total,
	})
}

// FormatVariable formats a single variable as JSON
func (f *JSONFormatter) FormatVariable(variable *models.Variable) error {
	return f.output(variable)
}

// FormatVariableOverview formats variable overview as JSON
func (f *JSONFormatter) FormatVariableOverview(items []models.VariableOverview, total int) error {
	return f.output(map[string]interface{}{
		"items": items,
		"total": total,
	})
}

// FormatBackupConfig formats a backup configuration as JSON
func (f *JSONFormatter) FormatBackupConfig(config *models.BackupConfig) error {
	return f.output(config)
}

// FormatBackupConfigTest formats a backup config test result as JSON
func (f *JSONFormatter) FormatBackupConfigTest(result *models.BackupConfigTestResponse) error {
	return f.output(result)
}

// FormatDeviceBackupStatuses formats a list of device backup statuses as JSON
func (f *JSONFormatter) FormatDeviceBackupStatuses(statuses []models.DeviceBackupStatus, total int, enabledCount int) error {
	return f.output(map[string]interface{}{
		"items":         statuses,
		"total":         total,
		"enabled_count": enabledCount,
	})
}

// FormatDeviceBackupStatus formats a single device backup status as JSON
func (f *JSONFormatter) FormatDeviceBackupStatus(status *models.DeviceBackupStatus) error {
	return f.output(status)
}

// FormatVpnNetworks formats VPN networks as JSON
func (f *JSONFormatter) FormatVpnNetworks(networks []models.VpnNetwork, total int, quota *models.Quota) error {
	out := map[string]interface{}{
		"vpn_networks": networks,
		"total":        total,
	}
	if quota != nil {
		out["quota"] = quota
	}
	return f.output(out)
}

// FormatVpnNetwork formats a single VPN network as JSON
func (f *JSONFormatter) FormatVpnNetwork(network *models.VpnNetwork) error {
	return f.output(network)
}

// FormatVpnMembers formats VPN members as JSON
func (f *JSONFormatter) FormatVpnMembers(members []models.VpnMember, total int) error {
	return f.output(map[string]interface{}{
		"members": members,
		"total":   total,
	})
}

// FormatVpnMember formats a single VPN member as JSON
func (f *JSONFormatter) FormatVpnMember(member *models.VpnMember) error {
	return f.output(member)
}

// FormatVpnLinks formats VPN links as JSON
func (f *JSONFormatter) FormatVpnLinks(links []models.VpnLink, total int) error {
	return f.output(map[string]interface{}{
		"links": links,
		"total": total,
	})
}

// FormatVpnLink formats a single VPN link as JSON
func (f *JSONFormatter) FormatVpnLink(link *models.VpnLink) error {
	return f.output(link)
}

// FormatVpnPrefixes formats VPN member prefixes as JSON
func (f *JSONFormatter) FormatVpnPrefixes(prefixes []models.VpnMemberPrefix, total int) error {
	return f.output(map[string]interface{}{
		"prefixes": prefixes,
		"total":    total,
	})
}

// FormatVpnPrefix formats a single VPN member prefix as JSON
func (f *JSONFormatter) FormatVpnPrefix(prefix *models.VpnMemberPrefix) error {
	return f.output(prefix)
}

// FormatVpnConnections formats effective VPN connections as JSON
func (f *JSONFormatter) FormatVpnConnections(connections []models.EffectiveConnection, total int) error {
	return f.output(map[string]interface{}{
		"connections": connections,
		"total":       total,
	})
}

// FormatVpnConnection formats a single effective VPN connection as JSON
func (f *JSONFormatter) FormatVpnConnection(connection *models.EffectiveConnection) error {
	return f.output(connection)
}

// Success prints nothing for JSON (no messages)
func (f *JSONFormatter) Success(message string) {
	// JSON formatter suppresses messages to keep output clean for parsing
	// Only write to stderr if needed
	fmt.Fprintln(os.Stderr, message)
}

// Error prints nothing for JSON
func (f *JSONFormatter) Error(message string) {
	fmt.Fprintln(os.Stderr, "Error: "+message)
}

// Warning prints nothing for JSON
func (f *JSONFormatter) Warning(message string) {
	fmt.Fprintln(os.Stderr, "Warning: "+message)
}

// Info prints nothing for JSON
func (f *JSONFormatter) Info(message string) {
	fmt.Fprintln(os.Stderr, message)
}
