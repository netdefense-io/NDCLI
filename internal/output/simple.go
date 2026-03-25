package output

import (
	"fmt"
	"strings"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// SimpleFormatter formats output as simple bullet points
type SimpleFormatter struct {
	BaseFormatter
}

// NewSimpleFormatter creates a new simple formatter
func NewSimpleFormatter() *SimpleFormatter {
	return &SimpleFormatter{BaseFormatter: NewBaseFormatter()}
}

// FormatDevices formats a list of devices
func (f *SimpleFormatter) FormatDevices(devices []models.Device, total int, quota *models.Quota) error {
	if len(devices) == 0 {
		f.Info("No devices found")
		return nil
	}

	for _, d := range devices {
		status := ColoredStatus(d.Status)
		synced := SyncIndicator(d.IsSynced())
		fmt.Fprintf(f.Writer, "• %s [%s] %s - %s\n", d.Name, status, synced, d.GetOUsDisplay())
	}
	if quota != nil {
		fmt.Fprintf(f.Writer, "\n%s\n", formatQuotaFooterSimple("enabled devices", quota))
	}
	return nil
}

// FormatDevice formats a single device
func (f *SimpleFormatter) FormatDevice(device *models.Device) error {
	fmt.Fprintf(f.Writer, "Device: %s\n", device.Name)
	fmt.Fprintf(f.Writer, "  Status: %s\n", ColoredStatus(device.Status))
	fmt.Fprintf(f.Writer, "  Organization: %s\n", device.Organization)
	if len(device.OrganizationalUnits) > 0 {
		fmt.Fprintf(f.Writer, "  OUs: %s\n", device.GetOUsDisplay())
	}
	if device.Version != "" {
		fmt.Fprintf(f.Writer, "  Version: %s\n", device.Version)
	}
	fmt.Fprintf(f.Writer, "  Synced: %s\n", SyncIndicator(device.IsSynced()))
	return nil
}

// FormatTasks formats a list of tasks
func (f *SimpleFormatter) FormatTasks(tasks []models.Task, total int) error {
	if len(tasks) == 0 {
		f.Info("No tasks found")
		return nil
	}

	for _, t := range tasks {
		status := ColoredStatus(t.Status)
		fmt.Fprintf(f.Writer, "• %s - %s [%s]\n", t.ID[:8], t.Type, status)
	}
	return nil
}

// FormatTask formats a single task with full details
func (f *SimpleFormatter) FormatTask(task *models.Task) error {
	fmt.Fprintf(f.Writer, "Task: %s\n", task.ID)
	fmt.Fprintf(f.Writer, "  Type: %s\n", task.Type)
	fmt.Fprintf(f.Writer, "  Status: %s\n", ColoredStatus(task.Status))
	fmt.Fprintf(f.Writer, "  Device: %s\n", task.DeviceName)
	fmt.Fprintf(f.Writer, "  Organization: %s\n", task.Organization)
	fmt.Fprintf(f.Writer, "  Created: %s\n", FormatTimestamp(task.CreatedAt.Time))
	if !task.ExpiresAt.IsZero() {
		fmt.Fprintf(f.Writer, "  Expires: %s\n", FormatTimestamp(task.ExpiresAt.Time))
	}
	if !task.StartedAt.IsZero() {
		fmt.Fprintf(f.Writer, "  Started: %s\n", FormatTimestamp(task.StartedAt.Time))
	}
	if !task.CompletedAt.IsZero() {
		fmt.Fprintf(f.Writer, "  Completed: %s\n", FormatTimestamp(task.CompletedAt.Time))
	}
	if task.Message != "" {
		fmt.Fprintf(f.Writer, "\nMessage:\n%s\n", task.Message)
	}
	if task.ErrorMessage != "" {
		fmt.Fprintf(f.Writer, "\nError: %s\n", task.ErrorMessage)
	}
	return nil
}

// FormatOrganizations formats a list of organizations
func (f *SimpleFormatter) FormatOrganizations(orgs []models.Organization) error {
	if len(orgs) == 0 {
		f.Info("No organizations found")
		return nil
	}

	for _, o := range orgs {
		role := o.GetRole()
		if role == "" {
			role = "-"
		}
		status := o.Status
		if status == "" {
			status = "ENABLED"
		}
		fmt.Fprintf(f.Writer, "• %s - %s - %s\n", o.Name, ColoredRole(role), ColoredStatus(status))
	}
	return nil
}

// FormatOrganization formats a single organization
func (f *SimpleFormatter) FormatOrganization(org *models.Organization) error {
	fmt.Fprintf(f.Writer, "Organization: %s\n", org.Name)
	fmt.Fprintf(f.Writer, "  Status: %s\n", ColoredStatus(org.Status))
	if defaultOU := org.GetDefaultOU(); defaultOU != "" {
		fmt.Fprintf(f.Writer, "  Default OU: %s\n", defaultOU)
	}
	if org.DeviceCount > 0 {
		fmt.Fprintf(f.Writer, "  Devices: %d\n", org.DeviceCount)
	}
	if org.MemberCount > 0 {
		fmt.Fprintf(f.Writer, "  Members: %d\n", org.MemberCount)
	}
	if len(org.Owners) > 0 {
		fmt.Fprintf(f.Writer, "  Owners: %s\n", strings.Join(org.Owners, ", "))
	}
	if org.Token != "" {
		fmt.Fprintf(f.Writer, "  Token: %s\n", org.Token)
	}
	return nil
}

// FormatOUs formats a list of organizational units
func (f *SimpleFormatter) FormatOUs(ous []models.OrganizationalUnit) error {
	if len(ous) == 0 {
		f.Info("No organizational units found")
		return nil
	}

	for _, ou := range ous {
		fmt.Fprintf(f.Writer, "• %s [%s] - %d devices, %d templates\n", ou.Name, ou.Organization, ou.DeviceCount, ou.TemplateCount)
	}
	return nil
}

// FormatOU formats a single organizational unit
func (f *SimpleFormatter) FormatOU(ou *models.OrganizationalUnit) error {
	fmt.Fprintf(f.Writer, "OU: %s\n", ou.Name)
	fmt.Fprintf(f.Writer, "  Organization: %s\n", ou.Organization)
	fmt.Fprintf(f.Writer, "  Status: %s\n", ou.Status)
	if ou.Description != "" {
		fmt.Fprintf(f.Writer, "  Description: %s\n", ou.Description)
	}

	fmt.Fprintf(f.Writer, "\nDevices (%d):\n", len(ou.Devices))
	for _, d := range ou.Devices {
		fmt.Fprintf(f.Writer, "  • %s\n", d.Name)
	}

	fmt.Fprintf(f.Writer, "\nTemplates (%d):\n", len(ou.Templates))
	for _, t := range ou.Templates {
		fmt.Fprintf(f.Writer, "  • %s\n", t.Name)
	}

	return nil
}

// FormatTemplates formats a list of templates
func (f *SimpleFormatter) FormatTemplates(templates []models.Template) error {
	if len(templates) == 0 {
		f.Info("No templates found")
		return nil
	}

	for _, t := range templates {
		fmt.Fprintf(f.Writer, "• %s [%s] (%d snippets)\n", t.Name, t.Position, t.SnippetCount)
	}
	return nil
}

// FormatTemplate formats a single template with its snippets
func (f *SimpleFormatter) FormatTemplate(template *models.Template) error {
	fmt.Fprintf(f.Writer, "Template: %s\n", template.Name)
	if template.Description != "" {
		fmt.Fprintf(f.Writer, "  Description: %s\n", template.Description)
	}
	fmt.Fprintf(f.Writer, "  Position: %s\n", template.Position)
	fmt.Fprintf(f.Writer, "  Created: %s\n", FormatTimestamp(template.CreatedAt.Time))
	fmt.Fprintf(f.Writer, "  Updated: %s\n", FormatTimestamp(template.UpdatedAt.Time))

	if len(template.Snippets) == 0 {
		fmt.Fprintf(f.Writer, "\nNo snippets in this template\n")
		return nil
	}

	fmt.Fprintf(f.Writer, "\nSnippets (%d):\n", len(template.Snippets))
	for _, s := range template.Snippets {
		fmt.Fprintf(f.Writer, "  • %s (priority: %d, type: %s)\n", s.Name, s.Priority, s.Type)
	}
	return nil
}

// FormatSnippets formats a list of snippets
func (f *SimpleFormatter) FormatSnippets(snippets []models.Snippet) error {
	if len(snippets) == 0 {
		f.Info("No snippets found")
		return nil
	}

	for _, s := range snippets {
		fmt.Fprintf(f.Writer, "• %s (priority: %d, type: %s)\n", s.Name, s.Priority, s.Type)
	}
	return nil
}

// FormatSnippet formats a single snippet
func (f *SimpleFormatter) FormatSnippet(snippet *models.Snippet) error {
	fmt.Fprintf(f.Writer, "Snippet: %s\n", snippet.Name)
	fmt.Fprintf(f.Writer, "  Type: %s\n", snippet.Type)
	fmt.Fprintf(f.Writer, "  Priority: %d\n", snippet.Priority)
	return nil
}

// FormatAccounts formats a list of accounts
func (f *SimpleFormatter) FormatAccounts(accounts []models.Account, quota *models.Quota) error {
	if len(accounts) == 0 {
		f.Info("No accounts found")
		return nil
	}

	for _, a := range accounts {
		role := ColoredRole(a.Role)
		status := ColoredStatus(a.Status)
		fmt.Fprintf(f.Writer, "• %s [%s] %s\n", a.Email, role, status)
	}
	if quota != nil {
		fmt.Fprintf(f.Writer, "\n%s\n", formatQuotaFooterSimple("users", quota))
	}
	return nil
}

// FormatInvitations formats a list of invitations
func (f *SimpleFormatter) FormatInvitations(invitations []models.Invitation) error {
	if len(invitations) == 0 {
		f.Info("No invitations found")
		return nil
	}

	for _, i := range invitations {
		status := ColoredStatus(i.Status)
		fmt.Fprintf(f.Writer, "• %s → %s [%s]\n", i.Email, i.Organization, status)
	}
	return nil
}

// FormatInvites formats the invites response (received and sent)
func (f *SimpleFormatter) FormatInvites(invites *models.InvitesResponse) error {
	if len(invites.Received) == 0 && len(invites.Sent) == 0 {
		f.Info("No invitations found")
		return nil
	}

	if len(invites.Received) > 0 {
		fmt.Fprintf(f.Writer, "Received:\n")
		for _, i := range invites.Received {
			fmt.Fprintf(f.Writer, "• %s [%s] from %s\n", i.Organization, ColoredRole(i.Role), i.InvitedBy)
		}
	}

	if len(invites.Sent) > 0 {
		if len(invites.Received) > 0 {
			fmt.Fprintln(f.Writer)
		}
		fmt.Fprintf(f.Writer, "Sent:\n")
		for _, i := range invites.Sent {
			fmt.Fprintf(f.Writer, "• %s → %s [%s]\n", i.Email, i.Organization, ColoredRole(i.Role))
		}
	}
	return nil
}

// FormatAuthMe formats the authenticated user's profile (simple view)
func (f *SimpleFormatter) FormatAuthMe(authMe *models.AuthMe) error {
	name := authMe.GetName()
	if name != "" {
		fmt.Fprintf(f.Writer, "Name: %s\n", name)
	}
	fmt.Fprintf(f.Writer, "Email: %s\n", authMe.Email)
	fmt.Fprintf(f.Writer, "Status: %s\n", ColoredStatus(authMe.Status))
	fmt.Fprintf(f.Writer, "Updated: %s\n", FormatTimestamp(authMe.UpdatedAt.Time))
	return nil
}

// FormatAuthMeUpdate formats the auth me update response (simple view)
func (f *SimpleFormatter) FormatAuthMeUpdate(resp *models.AuthMeUpdateResponse) error {
	fmt.Fprintf(f.Writer, "%s\n", resp.Message)
	if len(resp.PendingInvites) > 0 {
		fmt.Fprintf(f.Writer, "\nPending Invites: %d\n", len(resp.PendingInvites))
		for _, inv := range resp.PendingInvites {
			fmt.Fprintf(f.Writer, "• %s [%s] from %s\n", inv.Organization, ColoredRole(inv.Role), inv.InvitedBy)
		}
	}
	return nil
}

// FormatSyncStatus formats sync status (simple view)
func (f *SimpleFormatter) FormatSyncStatus(items []models.SyncStatusItem, total int) error {
	if len(items) == 0 {
		f.Info("No devices found")
		return nil
	}

	for _, item := range items {
		var status string
		if item.Error != nil && *item.Error != "" {
			status = ColorError.Sprint("ERROR")
		} else if item.IsSynced() {
			status = ColorSuccess.Sprint("SYNCED")
		} else {
			status = ColorWarning.Sprint("NOT SYNCED")
		}
		ous := ""
		if len(item.OUs) > 0 {
			ous = fmt.Sprintf(" (%s)", item.GetOUsDisplay())
		}
		fmt.Fprintf(f.Writer, "• %s%s [%s]\n", item.DeviceName, ous, status)
	}
	fmt.Fprintf(f.Writer, "\nTotal: %d devices\n", total)
	return nil
}

// FormatSyncApply formats sync apply result (simple view)
func (f *SimpleFormatter) FormatSyncApply(result *models.SyncApplyResponse) error {
	fmt.Fprintf(f.Writer, "%s\n", result.Message)
	fmt.Fprintf(f.Writer, "Affected: %d, Skipped: %d\n", result.DevicesAffected, result.Skipped)

	if len(result.Tasks) > 0 {
		fmt.Fprintf(f.Writer, "\nTasks created:\n")
		for _, t := range result.Tasks {
			fmt.Fprintf(f.Writer, "• %s → %s (%d snippets, %d vpn networks)\n", t.Task, t.DeviceName, t.SnippetCount, t.VpnNetworkCount)
		}
	}

	if len(result.Errors) > 0 {
		fmt.Fprintf(f.Writer, "\nErrors:\n")
		for _, e := range result.Errors {
			fmt.Fprintf(f.Writer, "• %s: %s\n", e.DeviceName, e.Error)
			for _, c := range e.Conflicts {
				fmt.Fprintf(f.Writer, "  %s\n", c.Message)
			}
			if len(e.UndefinedVariables) > 0 {
				fmt.Fprintf(f.Writer, "  Undefined: %s\n", formatVarListSimple(e.UndefinedVariables))
			}
		}
	}

	return nil
}

// formatVarListSimple formats variable names for simple output
func formatVarListSimple(vars []string) string {
	formatted := make([]string, len(vars))
	for i, v := range vars {
		formatted[i] = "${" + v + "}"
	}
	return strings.Join(formatted, ", ")
}

// FormatVariables formats a list of variables (simple view)
func (f *SimpleFormatter) FormatVariables(variables []models.Variable, total int) error {
	if len(variables) == 0 {
		f.Info("No variables found")
		return nil
	}

	for _, v := range variables {
		value := v.Value
		if len(value) > 30 {
			value = value[:27] + "..."
		}
		scopeInfo := v.Scope
		if v.ScopeName != nil {
			scopeInfo = fmt.Sprintf("%s:%s", v.Scope, *v.ScopeName)
		}
		secretIndicator := ""
		if v.Secret {
			secretIndicator = " [SECRET]"
		}
		fmt.Fprintf(f.Writer, "• %s = %s [%s]%s\n", v.Name, value, scopeInfo, secretIndicator)
	}
	return nil
}

// FormatVariable formats a single variable (simple view)
func (f *SimpleFormatter) FormatVariable(variable *models.Variable) error {
	fmt.Fprintf(f.Writer, "Variable: %s\n", variable.Name)
	fmt.Fprintf(f.Writer, "  Value: %s\n", variable.Value)
	if variable.Description != "" {
		fmt.Fprintf(f.Writer, "  Description: %s\n", variable.Description)
	}
	fmt.Fprintf(f.Writer, "  Scope: %s\n", variable.Scope)
	if variable.ScopeName != nil {
		fmt.Fprintf(f.Writer, "  Scope Name: %s\n", *variable.ScopeName)
	}
	if variable.Secret {
		fmt.Fprintf(f.Writer, "  Secret: Yes\n")
	}
	fmt.Fprintf(f.Writer, "  Created: %s\n", FormatTimestamp(variable.CreatedAt.Time))
	if variable.UpdatedAt != nil && !variable.UpdatedAt.IsZero() {
		fmt.Fprintf(f.Writer, "  Updated: %s\n", FormatTimestamp(variable.UpdatedAt.Time))
	}
	return nil
}

// FormatVariableOverview formats a consolidated view of variables (simple view)
func (f *SimpleFormatter) FormatVariableOverview(items []models.VariableOverview, total int) error {
	if len(items) == 0 {
		f.Info("No variables found")
		return nil
	}

	for _, item := range items {
		fmt.Fprintf(f.Writer, "• %s\n", item.Name)
		for _, def := range item.Definitions {
			scopeInfo := def.Scope
			if def.ScopeName != nil {
				scopeInfo = fmt.Sprintf("%s:%s", def.Scope, *def.ScopeName)
			}
			desc := ""
			if def.Description != "" {
				desc = fmt.Sprintf(" (%s)", def.Description)
			}
			secretIndicator := ""
			if def.Secret {
				secretIndicator = " [SECRET]"
			}
			fmt.Fprintf(f.Writer, "    %s: %s%s%s\n", scopeInfo, def.Value, desc, secretIndicator)
		}
	}
	fmt.Fprintf(f.Writer, "\nTotal: %d variables\n", total)
	return nil
}

// FormatBackupConfig formats a backup configuration (simple view)
func (f *SimpleFormatter) FormatBackupConfig(config *models.BackupConfig) error {
	fmt.Fprintf(f.Writer, "Backup Config: %s\n", config.Organization)
	fmt.Fprintf(f.Writer, "  Status: %s\n", ColoredStatus(config.Status))
	fmt.Fprintf(f.Writer, "  S3 Endpoint: %s\n", config.S3Endpoint)
	fmt.Fprintf(f.Writer, "  S3 Bucket: %s\n", config.S3Bucket)
	if config.S3Prefix != nil && *config.S3Prefix != "" {
		fmt.Fprintf(f.Writer, "  S3 Folder: %s\n", *config.S3Prefix)
	}
	fmt.Fprintf(f.Writer, "  Schedule: %s\n", config.Schedule)
	encKey := "Not configured"
	if config.HasEncryptionKey {
		encKey = "Configured"
	}
	fmt.Fprintf(f.Writer, "  Encryption Key: %s\n", encKey)
	return nil
}

// FormatBackupConfigTest formats a backup config test result (simple view)
func (f *SimpleFormatter) FormatBackupConfigTest(result *models.BackupConfigTestResponse) error {
	if result.Success {
		fmt.Fprintf(f.Writer, "✓ %s\n", result.Message)
	} else {
		fmt.Fprintf(f.Writer, "✗ %s\n", result.Message)
	}
	return nil
}

// FormatDeviceBackupStatuses formats a list of device backup statuses (simple view)
func (f *SimpleFormatter) FormatDeviceBackupStatuses(statuses []models.DeviceBackupStatus, total int, enabledCount int) error {
	if len(statuses) == 0 {
		f.Info("No devices found")
		return nil
	}

	for _, s := range statuses {
		enabled := "disabled"
		if s.Enabled {
			enabled = ColorEnabled.Sprint("enabled")
		}

		keyInfo := ""
		if s.HasEncryptionKeyOverride {
			keyInfo = " [custom key]"
		}

		lastBackup := "never"
		if s.LastBackupAt != nil && !s.LastBackupAt.IsZero() {
			lastBackup = RelativeTimeShort(s.LastBackupAt.Time)
		}

		status := ""
		if s.LastBackupStatus != "" {
			status = fmt.Sprintf(" [%s]", ColoredStatus(s.LastBackupStatus))
		}

		fmt.Fprintf(f.Writer, "• %s (%s%s) - last: %s%s\n", s.DeviceName, enabled, keyInfo, lastBackup, status)
	}
	fmt.Fprintf(f.Writer, "\nTotal: %d devices (%d enabled)\n", total, enabledCount)
	return nil
}

// FormatDeviceBackupStatus formats a single device backup status (simple view)
func (f *SimpleFormatter) FormatDeviceBackupStatus(status *models.DeviceBackupStatus) error {
	enabled := "disabled"
	if status.Enabled {
		enabled = ColorEnabled.Sprint("enabled")
	}
	fmt.Fprintf(f.Writer, "Device: %s (%s)\n", status.DeviceName, enabled)

	keyOverride := "org default"
	if status.HasEncryptionKeyOverride {
		keyOverride = "custom key"
	}
	fmt.Fprintf(f.Writer, "  Key: %s\n", keyOverride)

	if status.LastBackupAt != nil && !status.LastBackupAt.IsZero() {
		fmt.Fprintf(f.Writer, "  Last Backup: %s [%s]\n", FormatTimestamp(status.LastBackupAt.Time), ColoredStatus(status.LastBackupStatus))
		if status.LastBackupMessage != "" {
			fmt.Fprintf(f.Writer, "  Message: %s\n", status.LastBackupMessage)
		}
	} else {
		fmt.Fprintf(f.Writer, "  Last Backup: never\n")
	}
	return nil
}

// FormatVpnNetworks formats a list of VPN networks (simple view)
func (f *SimpleFormatter) FormatVpnNetworks(networks []models.VpnNetwork, total int, quota *models.Quota) error {
	if len(networks) == 0 {
		f.Info("No VPN networks found")
		return nil
	}

	for _, n := range networks {
		autoHubs := ""
		if n.AutoConnectHubs {
			autoHubs = " [auto-hubs]"
		}
		fmt.Fprintf(f.Writer, "• %s (%s) - %d members, %d overrides%s\n", n.Name, n.OverlayCIDRv4, n.MemberCount, n.LinkCount, autoHubs)
	}
	if quota != nil {
		fmt.Fprintf(f.Writer, "\n%s\n", formatQuotaFooterSimple("VPN networks", quota))
	}
	return nil
}

// FormatOrgQuota formats organization quota (simple view)
func (f *SimpleFormatter) FormatOrgQuota(quota *models.OrgQuota) error {
	if quota.Plan != nil {
		fmt.Fprintf(f.Writer, "Plan: %s\n\n", quota.Plan.DisplayName)
	}
	fmt.Fprintf(f.Writer, "• Devices: %s\n", simpleQuotaLine(quota.Devices))
	fmt.Fprintf(f.Writer, "• Users: %s\n", simpleQuotaLine(quota.Users))
	fmt.Fprintf(f.Writer, "• VPN Networks: %s\n", simpleQuotaLine(quota.VpnNetworks))
	fmt.Fprintf(f.Writer, "• Snippets: %s\n", simpleQuotaLine(quota.Snippets))
	fmt.Fprintf(f.Writer, "+ Backup: %s\n", enabledDisabledText(quota.BackupEnabled))
	fmt.Fprintf(f.Writer, "+ Remote Admin: %s\n", enabledDisabledText(quota.RemoteAdminEnabled))
	return nil
}

func simpleQuotaLine(q models.Quota) string {
	if q.Unlimited {
		return fmt.Sprintf("%d/∞ (Unlimited)", q.Used)
	}
	return fmt.Sprintf("%d/%d (%d available)", q.Used, q.Limit, q.Available)
}

func enabledDisabledText(v bool) string {
	if v {
		return "Enabled"
	}
	return "Disabled"
}

func formatQuotaFooterSimple(resourceName string, q *models.Quota) string {
	if q.Unlimited {
		return fmt.Sprintf("Quota: %s (Unlimited)", resourceName)
	}
	return fmt.Sprintf("Quota: %d/%d %s (%d available)", q.Used, q.Limit, resourceName, q.Available)
}

// FormatVpnNetwork formats a single VPN network (simple view)
func (f *SimpleFormatter) FormatVpnNetwork(network *models.VpnNetwork) error {
	fmt.Fprintf(f.Writer, "VPN Network: %s\n", network.Name)
	fmt.Fprintf(f.Writer, "  Overlay CIDR: %s\n", network.OverlayCIDRv4)
	autoHubs := "No"
	if network.AutoConnectHubs {
		autoHubs = "Yes"
	}
	fmt.Fprintf(f.Writer, "  Auto-Hubs: %s\n", autoHubs)
	fmt.Fprintf(f.Writer, "  Listen Port: %d\n", network.ListenPortDefault)
	if network.MTUDefault != nil {
		fmt.Fprintf(f.Writer, "  MTU: %d\n", *network.MTUDefault)
	}
	if network.KeepaliveDefault != nil {
		fmt.Fprintf(f.Writer, "  Keepalive: %d\n", *network.KeepaliveDefault)
	}
	fmt.Fprintf(f.Writer, "  Members: %d, Overrides: %d\n", network.MemberCount, network.LinkCount)
	fmt.Fprintf(f.Writer, "  Created: %s\n", FormatTimestamp(network.CreatedAt.Time))
	fmt.Fprintf(f.Writer, "  Updated: %s\n", FormatTimestamp(network.UpdatedAt.Time))
	return nil
}

// FormatVpnMembers formats a list of VPN members (simple view)
func (f *SimpleFormatter) FormatVpnMembers(members []models.VpnMember, total int) error {
	if len(members) == 0 {
		f.Info("No VPN members found")
		return nil
	}

	for _, m := range members {
		enabled := ""
		if !m.Enabled {
			enabled = " [disabled]"
		}
		endpoint := ""
		if m.EndpointHost != nil && *m.EndpointHost != "" {
			endpoint = fmt.Sprintf(" endpoint=%s", *m.EndpointHost)
			if m.EndpointPort != nil {
				endpoint = fmt.Sprintf(" endpoint=%s:%d", *m.EndpointHost, *m.EndpointPort)
			}
		}
		fmt.Fprintf(f.Writer, "• %s [%s] %s%s%s\n", m.DeviceName, m.Role, m.OverlayIPv4, endpoint, enabled)
	}
	return nil
}

// FormatVpnMember formats a single VPN member (simple view)
func (f *SimpleFormatter) FormatVpnMember(member *models.VpnMember) error {
	fmt.Fprintf(f.Writer, "Member: %s\n", member.DeviceName)
	fmt.Fprintf(f.Writer, "  VPN Network: %s\n", member.VpnNetwork)
	fmt.Fprintf(f.Writer, "  Role: %s\n", member.Role)
	enabled := "Yes"
	if !member.Enabled {
		enabled = "No"
	}
	fmt.Fprintf(f.Writer, "  Enabled: %s\n", enabled)
	fmt.Fprintf(f.Writer, "  Overlay IP: %s\n", member.OverlayIPv4)
	fmt.Fprintf(f.Writer, "  Public Key: %s\n", member.WgPublicKey)
	if member.EndpointHost != nil && *member.EndpointHost != "" {
		fmt.Fprintf(f.Writer, "  Endpoint: %s", *member.EndpointHost)
		if member.EndpointPort != nil {
			fmt.Fprintf(f.Writer, ":%d", *member.EndpointPort)
		}
		fmt.Fprintln(f.Writer)
	}
	if member.TransitViaHub != nil && *member.TransitViaHub != "" {
		fmt.Fprintf(f.Writer, "  Transit Hub: %s\n", *member.TransitViaHub)
	}
	fmt.Fprintf(f.Writer, "  Created: %s\n", FormatTimestamp(member.CreatedAt.Time))
	fmt.Fprintf(f.Writer, "  Updated: %s\n", FormatTimestamp(member.UpdatedAt.Time))
	return nil
}

// FormatVpnLinks formats a list of VPN links (simple view)
func (f *SimpleFormatter) FormatVpnLinks(links []models.VpnLink, total int) error {
	if len(links) == 0 {
		f.Info("No VPN links found")
		return nil
	}

	for _, l := range links {
		enabled := ""
		if !l.Enabled {
			enabled = " [disabled]"
		}
		psk := ""
		if l.HasPSK {
			psk = " [psk]"
		}
		fmt.Fprintf(f.Writer, "• %s <-> %s%s%s\n", l.DeviceAName, l.DeviceBName, psk, enabled)
	}
	return nil
}

// FormatVpnLink formats a single VPN link (simple view)
func (f *SimpleFormatter) FormatVpnLink(link *models.VpnLink) error {
	fmt.Fprintf(f.Writer, "Link: %s <-> %s\n", link.DeviceAName, link.DeviceBName)
	fmt.Fprintf(f.Writer, "  VPN Network: %s\n", link.VpnNetwork)
	enabled := "Yes"
	if !link.Enabled {
		enabled = "No"
	}
	fmt.Fprintf(f.Writer, "  Enabled: %s\n", enabled)
	psk := "No"
	if link.HasPSK {
		psk = "Yes"
	}
	fmt.Fprintf(f.Writer, "  PSK: %s\n", psk)
	fmt.Fprintf(f.Writer, "  Created: %s\n", FormatTimestamp(link.CreatedAt.Time))
	fmt.Fprintf(f.Writer, "  Updated: %s\n", FormatTimestamp(link.UpdatedAt.Time))
	return nil
}

// FormatVpnPrefixes formats a list of VPN member prefixes (simple view)
func (f *SimpleFormatter) FormatVpnPrefixes(prefixes []models.VpnMemberPrefix, total int) error {
	if len(prefixes) == 0 {
		f.Info("No VPN prefixes found")
		return nil
	}

	for _, p := range prefixes {
		publish := "publish"
		if !p.Publish {
			publish = "no-publish"
		}
		fmt.Fprintf(f.Writer, "• %s [%s]\n", p.VariableName, publish)
	}
	return nil
}

// FormatVpnPrefix formats a single VPN member prefix (simple view)
func (f *SimpleFormatter) FormatVpnPrefix(prefix *models.VpnMemberPrefix) error {
	fmt.Fprintf(f.Writer, "Prefix: %s\n", prefix.VariableName)
	fmt.Fprintf(f.Writer, "  VPN Network: %s\n", prefix.VpnNetwork)
	fmt.Fprintf(f.Writer, "  Device: %s\n", prefix.DeviceName)
	publish := "Yes"
	if !prefix.Publish {
		publish = "No"
	}
	fmt.Fprintf(f.Writer, "  Publish: %s\n", publish)
	fmt.Fprintf(f.Writer, "  Created: %s\n", FormatTimestamp(prefix.CreatedAt.Time))
	fmt.Fprintf(f.Writer, "  Updated: %s\n", FormatTimestamp(prefix.UpdatedAt.Time))
	return nil
}

// FormatVpnConnections formats a list of effective VPN connections (simple view)
func (f *SimpleFormatter) FormatVpnConnections(connections []models.EffectiveConnection, total int) error {
	if len(connections) == 0 {
		f.Info("No VPN connections found")
		return nil
	}

	for _, c := range connections {
		tags := ""
		if !c.Active {
			tags += " [inactive]"
		}
		if c.HasPSK {
			tags += " [psk]"
		}
		note := VpnConnectionNote(&c)
		if note != "-" {
			tags += " [" + note + "]"
		}
		fmt.Fprintf(f.Writer, "• %s ↔ %s  (%s)%s\n", c.DeviceA, c.DeviceB, VpnPairTypeDisplay(c.PairType), tags)
	}
	return nil
}

// FormatVpnConnection formats a single effective VPN connection (simple view)
func (f *SimpleFormatter) FormatVpnConnection(connection *models.EffectiveConnection) error {
	fmt.Fprintf(f.Writer, "Connection: %s\n", VpnConnectionLine(connection))
	fmt.Fprintf(f.Writer, "  Type: %s\n", VpnTypeValue(connection.PairType, connection.Source))
	active := "Yes"
	if !connection.Active {
		active = "No"
	}
	fmt.Fprintf(f.Writer, "  Active: %s\n", active)
	psk := "No"
	if connection.HasPSK {
		psk = "Yes"
	}
	fmt.Fprintf(f.Writer, "  PSK: %s\n", psk)
	fmt.Fprintln(f.Writer)
	explanation := VpnConnectionExplanation(connection)
	for _, line := range strings.Split(explanation, "\n") {
		fmt.Fprintf(f.Writer, "  %s\n", line)
	}
	return nil
}
