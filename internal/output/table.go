package output

import (
	"fmt"
	"os"
	"strings"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// TableFormatter formats output as tables
type TableFormatter struct {
	BaseFormatter
}

// NewTableFormatter creates a new table formatter
func NewTableFormatter() *TableFormatter {
	return &TableFormatter{BaseFormatter: NewBaseFormatter()}
}

// StyledTable renders tables with unicode box-drawing characters
type StyledTable struct {
	headers []string
	rows    [][]string
	widths  []int
}

// NewStyledTable creates a new styled table with the given headers
func NewStyledTable(headers []string) *StyledTable {
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	return &StyledTable{
		headers: headers,
		widths:  widths,
	}
}

// Append adds a row to the table
func (t *StyledTable) Append(row []string) {
	t.rows = append(t.rows, row)
	for i, cell := range row {
		if i < len(t.widths) {
			cellLen := visibleLength(cell)
			if cellLen > t.widths[i] {
				t.widths[i] = cellLen
			}
		}
	}
}

// Render outputs the styled table to stdout
func (t *StyledTable) Render() {
	// Top border: ╭──────┬──────┬──────╮
	t.renderTop()
	// Header row with bold text
	t.renderHeader()
	// Header separator: ├──────┼──────┼──────┤
	t.renderSeparator()
	// Data rows
	for _, row := range t.rows {
		t.renderRow(row)
	}
	// Bottom border: ╰──────┴──────┴──────╯
	t.renderBottom()
}

func (t *StyledTable) renderTop() {
	var sb strings.Builder
	sb.WriteString(BoxTopLeft)
	for i, w := range t.widths {
		sb.WriteString(strings.Repeat(BoxHorizontal, w+2))
		if i < len(t.widths)-1 {
			sb.WriteString(BoxTeeTop)
		}
	}
	sb.WriteString(BoxTopRight)
	fmt.Fprintln(os.Stdout, sb.String())
}

func (t *StyledTable) renderHeader() {
	var sb strings.Builder
	sb.WriteString(BoxVertical)
	for i, h := range t.headers {
		// Bold header text
		boldHeader := ColorHeader.Sprint(h)
		padding := t.widths[i] - len(h)
		sb.WriteString(" ")
		sb.WriteString(boldHeader)
		sb.WriteString(strings.Repeat(" ", padding+1))
		sb.WriteString(BoxVertical)
	}
	fmt.Fprintln(os.Stdout, sb.String())
}

func (t *StyledTable) renderSeparator() {
	var sb strings.Builder
	sb.WriteString(BoxTeeLeft)
	for i, w := range t.widths {
		sb.WriteString(strings.Repeat(BoxHorizontal, w+2))
		if i < len(t.widths)-1 {
			sb.WriteString(BoxCross)
		}
	}
	sb.WriteString(BoxTeeRight)
	fmt.Fprintln(os.Stdout, sb.String())
}

func (t *StyledTable) renderRow(row []string) {
	var sb strings.Builder
	sb.WriteString(BoxVertical)
	for i, cell := range row {
		if i >= len(t.widths) {
			break
		}
		cellLen := visibleLength(cell)
		padding := t.widths[i] - cellLen
		sb.WriteString(" ")
		sb.WriteString(cell)
		sb.WriteString(strings.Repeat(" ", padding+1))
		sb.WriteString(BoxVertical)
	}
	// Handle case where row has fewer columns than headers
	for i := len(row); i < len(t.widths); i++ {
		sb.WriteString(" ")
		sb.WriteString(strings.Repeat(" ", t.widths[i]+1))
		sb.WriteString(BoxVertical)
	}
	fmt.Fprintln(os.Stdout, sb.String())
}

func (t *StyledTable) renderBottom() {
	var sb strings.Builder
	sb.WriteString(BoxBottomLeft)
	for i, w := range t.widths {
		sb.WriteString(strings.Repeat(BoxHorizontal, w+2))
		if i < len(t.widths)-1 {
			sb.WriteString(BoxTeeBottom)
		}
	}
	sb.WriteString(BoxBottomRight)
	fmt.Fprintln(os.Stdout, sb.String())
}

// FormatDevices formats a list of devices as a table
func (f *TableFormatter) FormatDevices(devices []models.Device, total int, quota *models.Quota) error {
	if len(devices) == 0 {
		f.Info("No devices found")
		return nil
	}

	table := NewStyledTable([]string{"Name", "Status", "OU", "Version", "Heartbeat", "Synced At"})

	for _, d := range devices {
		ou := d.GetOUsDisplay()
		version := d.Version
		if version == "" {
			version = "-"
		}
		heartbeat := RelativeTimeShort(d.Heartbeat.Time)

		// Show synced_at time or "Never" if not synced
		syncedAt := "Never"
		if d.SyncedAt != nil && !d.SyncedAt.Time.IsZero() {
			syncedAt = RelativeTimeShort(d.SyncedAt.Time)
		}

		table.Append([]string{
			d.Name,
			StatusWithIcon(d.Status),
			ou,
			version,
			heartbeat,
			syncedAt,
		})
	}

	table.Render()
	if quota != nil {
		fmt.Fprintf(os.Stdout, "\n%s\n", formatQuotaFooter("enabled devices", quota))
	}
	return nil
}

// FormatDevice formats a single device as a table
func (f *TableFormatter) FormatDevice(device *models.Device) error {
	fmt.Printf("Name:         %s\n", device.Name)
	fmt.Printf("UUID:         %s\n", device.UUID)
	fmt.Printf("Status:       %s\n", ColoredStatus(device.Status))
	fmt.Printf("Organization: %s\n", device.Organization)

	// OUs
	if len(device.OrganizationalUnits) > 0 {
		fmt.Printf("OUs:          %s\n", strings.Join(device.OrganizationalUnits, ", "))
	}

	fmt.Printf("Version:      %s\n", device.Version)
	fmt.Printf("Auto-Sync:    %v\n", device.AutoSync)
	fmt.Println()

	// Timestamps
	fmt.Printf("Heartbeat:    %s (%s)\n", FormatTimestamp(device.Heartbeat.Time), RelativeTimeShort(device.Heartbeat.Time))
	if device.SyncedAt != nil && !device.SyncedAt.IsZero() {
		fmt.Printf("Synced At:    %s (%s)\n", FormatTimestamp(device.SyncedAt.Time), RelativeTimeShort(device.SyncedAt.Time))
	}
	if device.SyncedHash != nil && *device.SyncedHash != "" {
		fmt.Printf("Synced Hash:  %s\n", *device.SyncedHash)
	}
	fmt.Printf("Created:      %s\n", FormatTimestamp(device.CreatedAt.Time))
	fmt.Printf("Updated:      %s\n", FormatTimestamp(device.UpdatedAt.Time))

	return nil
}

// FormatTasks formats a list of tasks as a table
func (f *TableFormatter) FormatTasks(tasks []models.Task, total int) error {
	if len(tasks) == 0 {
		f.Info("No tasks found")
		return nil
	}

	table := NewStyledTable([]string{"ID", "Type", "Status", "Device", "Created"})

	for _, t := range tasks {
		id := t.ID
		if len(id) > 8 {
			id = id[:8]
		}
		table.Append([]string{
			id,
			t.Type,
			StatusWithIcon(t.Status),
			t.DeviceName,
			FormatTimestampShort(t.CreatedAt.Time),
		})
	}

	table.Render()
	return nil
}

// FormatTask formats a single task with full details
func (f *TableFormatter) FormatTask(task *models.Task) error {
	fmt.Printf("Task ID:      %s\n", task.ID)
	fmt.Printf("Type:         %s\n", task.Type)
	fmt.Printf("Status:       %s\n", ColoredStatus(task.Status))
	fmt.Printf("Device:       %s\n", task.DeviceName)
	fmt.Printf("Organization: %s\n", task.Organization)
	fmt.Printf("Created:      %s\n", FormatTimestamp(task.CreatedAt.Time))
	if !task.ExpiresAt.IsZero() {
		fmt.Printf("Expires:      %s\n", FormatTimestamp(task.ExpiresAt.Time))
	}
	if !task.StartedAt.IsZero() {
		fmt.Printf("Started:      %s\n", FormatTimestamp(task.StartedAt.Time))
	}
	if !task.CompletedAt.IsZero() {
		fmt.Printf("Completed:    %s\n", FormatTimestamp(task.CompletedAt.Time))
	}
	if task.Message != "" {
		fmt.Printf("\nMessage:\n%s\n", task.Message)
	}
	if task.ErrorMessage != "" {
		fmt.Printf("\nError: %s\n", task.ErrorMessage)
	}
	return nil
}

// FormatOrganizations formats a list of organizations as a table
func (f *TableFormatter) FormatOrganizations(orgs []models.Organization) error {
	if len(orgs) == 0 {
		f.Info("No organizations found")
		return nil
	}

	table := NewStyledTable([]string{"Name", "Role", "Status", "Default OU", "Created"})

	for _, o := range orgs {
		role := o.GetRole()
		if role == "" {
			role = "-"
		}
		status := o.Status
		if status == "" {
			status = "ENABLED"
		}
		defaultOU := o.GetDefaultOU()
		if defaultOU == "" {
			defaultOU = "-"
		}
		table.Append([]string{
			o.Name,
			ColoredRole(role),
			StatusWithIcon(status),
			defaultOU,
			FormatTimestampShort(o.CreatedAt.Time),
		})
	}

	table.Render()
	return nil
}

// FormatOrganization formats a single organization with details
func (f *TableFormatter) FormatOrganization(org *models.Organization) error {
	fmt.Printf("Name:         %s\n", org.Name)
	if org.DisplayName != "" {
		fmt.Printf("Display Name: %s\n", org.DisplayName)
	}
	fmt.Printf("Status:       %s\n", ColoredStatus(org.Status))
	defaultOU := org.GetDefaultOU()
	if defaultOU != "" {
		fmt.Printf("Default OU:   %s\n", defaultOU)
	}
	fmt.Printf("Created:      %s\n", FormatTimestamp(org.CreatedAt.Time))
	fmt.Printf("Updated:      %s\n", FormatTimestamp(org.UpdatedAt.Time))

	// Stats from describe endpoint
	if org.DeviceCount > 0 || org.MemberCount > 0 {
		fmt.Printf("\nStatistics:\n")
		fmt.Printf("  Devices: %d\n", org.DeviceCount)
		fmt.Printf("  Members: %d\n", org.MemberCount)
		if len(org.MemberCountsByRole) > 0 {
			fmt.Printf("    By Role: SU=%d, RW=%d, RO=%d\n",
				org.MemberCountsByRole["SU"],
				org.MemberCountsByRole["RW"],
				org.MemberCountsByRole["RO"])
		}
	}

	// Owners section
	if len(org.Owners) > 0 {
		fmt.Printf("\nOwners:\n")
		for _, owner := range org.Owners {
			fmt.Printf("  • %s\n", owner)
		}
	}

	// Token (for device registration)
	if org.Token != "" {
		fmt.Printf("\nRegistration Token: %s\n", org.Token)
	}

	return nil
}

// FormatOUs formats a list of organizational units as a table
func (f *TableFormatter) FormatOUs(ous []models.OrganizationalUnit) error {
	if len(ous) == 0 {
		f.Info("No organizational units found")
		return nil
	}

	table := NewStyledTable([]string{"Name", "Organization", "Devices", "Templates", "Status", "Created"})

	for _, ou := range ous {
		table.Append([]string{
			ou.Name,
			ou.Organization,
			fmt.Sprintf("%d", ou.DeviceCount),
			fmt.Sprintf("%d", ou.TemplateCount),
			StatusWithIcon(ou.Status),
			FormatTimestampShort(ou.CreatedAt.Time),
		})
	}

	table.Render()
	return nil
}

// FormatOU formats a single organizational unit with details
func (f *TableFormatter) FormatOU(ou *models.OrganizationalUnit) error {
	fmt.Printf("Name:         %s\n", ou.Name)
	fmt.Printf("Organization: %s\n", ou.Organization)
	fmt.Printf("Status:       %s\n", ColoredStatus(ou.Status))
	if ou.Description != "" {
		fmt.Printf("Description:  %s\n", ou.Description)
	}
	fmt.Printf("Created:      %s\n", FormatTimestamp(ou.CreatedAt.Time))
	fmt.Printf("Updated:      %s\n", FormatTimestamp(ou.UpdatedAt.Time))

	// Devices section
	fmt.Printf("\nDevices (%d):\n", len(ou.Devices))
	if len(ou.Devices) == 0 {
		fmt.Printf("  (none)\n")
	} else {
		for _, d := range ou.Devices {
			fmt.Printf("  • %s\n", d.Name)
		}
	}

	// Templates section
	fmt.Printf("\nTemplates (%d):\n", len(ou.Templates))
	if len(ou.Templates) == 0 {
		fmt.Printf("  (none)\n")
	} else {
		for _, t := range ou.Templates {
			fmt.Printf("  • %s (%d snippets)\n", t.Name, t.SnippetCount)
		}
	}

	return nil
}

// FormatTemplates formats a list of templates as a table
func (f *TableFormatter) FormatTemplates(templates []models.Template) error {
	if len(templates) == 0 {
		f.Info("No templates found")
		return nil
	}

	table := NewStyledTable([]string{"Name", "Position", "Description", "Snippets", "Created"})

	for _, t := range templates {
		desc := t.Description
		if len(desc) > 40 {
			desc = desc[:37] + "..."
		}
		if desc == "" {
			desc = "-"
		}
		table.Append([]string{
			t.Name,
			t.Position,
			desc,
			fmt.Sprintf("%d", t.SnippetCount),
			FormatTimestampShort(t.CreatedAt.Time),
		})
	}

	table.Render()
	return nil
}

// FormatTemplate formats a single template with its snippets
func (f *TableFormatter) FormatTemplate(template *models.Template) error {
	desc := template.Description
	if desc == "" {
		desc = "-"
	}

	fmt.Printf("Name:        %s\n", template.Name)
	fmt.Printf("Description: %s\n", desc)
	fmt.Printf("Position:    %s\n", template.Position)
	fmt.Printf("Created:     %s\n", FormatTimestamp(template.CreatedAt.Time))
	fmt.Printf("Updated:     %s\n", FormatTimestamp(template.UpdatedAt.Time))
	fmt.Println()

	if len(template.Snippets) == 0 {
		f.Info("No snippets in this template")
		return nil
	}

	fmt.Printf("Snippets (%d):\n", len(template.Snippets))
	return f.FormatSnippets(template.Snippets)
}

// FormatSnippets formats a list of snippets as a table
func (f *TableFormatter) FormatSnippets(snippets []models.Snippet) error {
	if len(snippets) == 0 {
		f.Info("No snippets found")
		return nil
	}

	table := NewStyledTable([]string{"Name", "Priority", "Type", "Updated"})

	for _, s := range snippets {
		snippetType := s.Type
		if snippetType == "" {
			snippetType = "-"
		}
		table.Append([]string{
			s.Name,
			fmt.Sprintf("%d", s.Priority),
			snippetType,
			FormatTimestampShort(s.UpdatedAt.Time),
		})
	}

	table.Render()
	return nil
}

// FormatSnippet formats a single snippet as a table
func (f *TableFormatter) FormatSnippet(snippet *models.Snippet) error {
	return f.FormatSnippets([]models.Snippet{*snippet})
}

// FormatAccounts formats a list of accounts as a table
func (f *TableFormatter) FormatAccounts(accounts []models.Account, quota *models.Quota) error {
	if len(accounts) == 0 {
		f.Info("No accounts found")
		return nil
	}

	table := NewStyledTable([]string{"Email", "Name", "Role", "Status", "Created"})

	for _, a := range accounts {
		name := a.Name
		if name == "" {
			name = "-"
		}
		table.Append([]string{
			a.Email,
			name,
			ColoredRole(a.Role),
			StatusWithIcon(a.Status),
			FormatTimestampShort(a.CreatedAt.Time),
		})
	}

	table.Render()
	if quota != nil {
		fmt.Fprintf(os.Stdout, "\n%s\n", formatQuotaFooter("users", quota))
	}
	return nil
}

// FormatInvitations formats a list of invitations as a table
func (f *TableFormatter) FormatInvitations(invitations []models.Invitation) error {
	if len(invitations) == 0 {
		f.Info("No invitations found")
		return nil
	}

	table := NewStyledTable([]string{"Email", "Organization", "Role", "Status", "Expires"})

	for _, i := range invitations {
		table.Append([]string{
			i.Email,
			i.Organization,
			ColoredRole(i.Role),
			StatusWithIcon(i.Status),
			FormatTimestampShort(i.ExpiresAt.Time),
		})
	}

	table.Render()
	return nil
}

// FormatInvites formats the invites response (received and sent) as tables
func (f *TableFormatter) FormatInvites(invites *models.InvitesResponse) error {
	if len(invites.Received) == 0 && len(invites.Sent) == 0 {
		f.Info("No invitations found")
		return nil
	}

	if len(invites.Received) > 0 {
		fmt.Println("Received Invitations:")
		table := NewStyledTable([]string{"Organization", "Role", "Status", "From", "Created"})
		for _, i := range invites.Received {
			table.Append([]string{
				i.Organization,
				ColoredRole(i.Role),
				StatusWithIcon(i.Status),
				i.InvitedBy,
				FormatTimestamp(i.CreatedAt.Time),
			})
		}
		table.Render()
	}

	if len(invites.Sent) > 0 {
		if len(invites.Received) > 0 {
			fmt.Println()
		}
		fmt.Println("Sent Invitations:")
		table := NewStyledTable([]string{"Email", "Organization", "Role", "Status", "Created"})
		for _, i := range invites.Sent {
			table.Append([]string{
				i.Email,
				i.Organization,
				ColoredRole(i.Role),
				StatusWithIcon(i.Status),
				FormatTimestamp(i.CreatedAt.Time),
			})
		}
		table.Render()
	}

	return nil
}

// FormatAuthMe formats the authenticated user's profile as a table
func (f *TableFormatter) FormatAuthMe(authMe *models.AuthMe) error {
	// User info section
	name := authMe.GetName()
	if name == "" {
		name = "-"
	}
	fmt.Printf("Email:   %s\n", authMe.Email)
	fmt.Printf("Name:    %s\n", name)
	fmt.Printf("Status:  %s\n", StatusWithIcon(authMe.Status))
	fmt.Printf("Created: %s\n", FormatTimestamp(authMe.CreatedAt.Time))
	fmt.Printf("Updated: %s\n", FormatTimestamp(authMe.UpdatedAt.Time))
	fmt.Println()

	// Organizations table
	if len(authMe.Organizations) > 0 {
		fmt.Println("Organizations:")
		table := NewStyledTable([]string{"Name", "Role", "Status", "Joined"})

		for _, org := range authMe.Organizations {
			table.Append([]string{
				org.Name,
				ColoredRole(org.Role),
				StatusWithIcon(org.Status),
				FormatTimestamp(org.CreatedAt.Time),
			})
		}

		table.Render()
	}

	return nil
}

// FormatAuthMeUpdate formats the auth me update response as a table
func (f *TableFormatter) FormatAuthMeUpdate(resp *models.AuthMeUpdateResponse) error {
	ColorSuccess.Printf("✓ %s\n", resp.Message)

	if len(resp.PendingInvites) > 0 {
		fmt.Println()
		fmt.Println("Pending Invites:")
		table := NewStyledTable([]string{"Organization", "Role", "Invited By", "Created"})

		for _, inv := range resp.PendingInvites {
			table.Append([]string{
				inv.Organization,
				ColoredRole(inv.Role),
				inv.InvitedBy,
				FormatTimestamp(inv.CreatedAt.Time),
			})
		}

		table.Render()
	}

	return nil
}

// FormatSyncStatus formats sync status as a table
func (f *TableFormatter) FormatSyncStatus(items []models.SyncStatusItem, total int) error {
	if len(items) == 0 {
		f.Info("No devices found")
		return nil
	}

	table := NewStyledTable([]string{"Device", "OU", "Auto-Sync", "Synced At", "Status"})

	for _, item := range items {
		ou := item.GetOUsDisplay()

		autoSync := "No"
		if item.AutoSync {
			autoSync = "Yes"
		}

		syncedAt := "Never"
		if item.SyncedAt != nil && !item.SyncedAt.Time.IsZero() {
			syncedAt = RelativeTimeShort(item.SyncedAt.Time)
		}

		var statusDisplay string
		if item.Error != nil && *item.Error != "" {
			statusDisplay = ColorError.Sprint("●") + " " + ColorError.Sprint("ERROR")
		} else if item.IsSynced() {
			statusDisplay = ColorSuccess.Sprint("●") + " " + ColorSuccess.Sprint("SYNCED")
		} else {
			statusDisplay = ColorWarning.Sprint("○") + " " + ColorWarning.Sprint("NOT SYNCED")
		}

		table.Append([]string{
			item.DeviceName,
			ou,
			autoSync,
			syncedAt,
			statusDisplay,
		})
	}

	table.Render()
	fmt.Printf("\nTotal: %d devices\n", total)
	return nil
}

// FormatSyncApply formats sync apply result
func (f *TableFormatter) FormatSyncApply(result *models.SyncApplyResponse) error {
	ColorSuccess.Printf("✓ %s\n", result.Message)
	fmt.Printf("  Devices affected: %d\n", result.DevicesAffected)
	fmt.Printf("  Skipped (already synced): %d\n", result.Skipped)

	if len(result.Tasks) > 0 {
		fmt.Println()
		table := NewStyledTable([]string{"Task", "Device", "Snippets", "VPN Networks"})
		for _, t := range result.Tasks {
			table.Append([]string{
				t.Task,
				t.DeviceName,
				fmt.Sprintf("%d", t.SnippetCount),
				fmt.Sprintf("%d", t.VpnNetworkCount),
			})
		}
		table.Render()
	}

	if len(result.Errors) > 0 {
		fmt.Println()
		ColorError.Println("Errors:")
		for _, e := range result.Errors {
			fmt.Printf("  • %s: %s\n", e.DeviceName, e.Error)
			// Show conflict details
			for _, c := range e.Conflicts {
				fmt.Printf("      %s\n", c.Message)
			}
			// Show undefined variables, grouped by snippet when the server tells us which
			// snippet each variable came from (newer NDManager); fall back to a flat list.
			if len(e.UndefinedVariablesBySnippet) > 0 {
				for _, snippetName := range sortedSnippetNames(e.UndefinedVariablesBySnippet) {
					fmt.Printf("      Snippet %q: %s\n", snippetName, formatVarList(e.UndefinedVariablesBySnippet[snippetName]))
				}
			} else if len(e.UndefinedVariables) > 0 {
				fmt.Printf("      Undefined: %s\n", formatVarList(e.UndefinedVariables))
			}
		}
	}

	return nil
}

// formatVarList formats a list of variable names for display
func formatVarList(vars []string) string {
	formatted := make([]string, len(vars))
	for i, v := range vars {
		formatted[i] = "${" + v + "}"
	}
	return strings.Join(formatted, ", ")
}

// FormatVariables formats a list of variables as a table
func (f *TableFormatter) FormatVariables(variables []models.Variable, total int) error {
	if len(variables) == 0 {
		f.Info("No variables found")
		return nil
	}

	table := NewStyledTable([]string{"Name", "Value", "Scope", "Scope Name", "Secret", "Updated"})

	for _, v := range variables {
		scopeName := "-"
		if v.ScopeName != nil {
			scopeName = *v.ScopeName
		}

		value := v.Value
		if len(value) > 30 {
			value = value[:27] + "..."
		}

		updated := "-"
		if v.UpdatedAt != nil && !v.UpdatedAt.IsZero() {
			updated = RelativeTimeShort(v.UpdatedAt.Time)
		}

		secret := "-"
		if v.Secret {
			secret = "Yes"
		}

		table.Append([]string{
			v.Name,
			value,
			v.Scope,
			scopeName,
			secret,
			updated,
		})
	}

	table.Render()
	return nil
}

// FormatVariable formats a single variable with full details
func (f *TableFormatter) FormatVariable(variable *models.Variable) error {
	fmt.Printf("Name:        %s\n", variable.Name)
	fmt.Printf("Value:       %s\n", variable.Value)
	if variable.Description != "" {
		fmt.Printf("Description: %s\n", variable.Description)
	}
	fmt.Printf("Scope:       %s\n", variable.Scope)
	if variable.ScopeName != nil {
		fmt.Printf("Scope Name:  %s\n", *variable.ScopeName)
	}
	if variable.Secret {
		fmt.Printf("Secret:      Yes\n")
	}
	fmt.Printf("Created:     %s\n", FormatTimestamp(variable.CreatedAt.Time))
	if variable.UpdatedAt != nil && !variable.UpdatedAt.IsZero() {
		fmt.Printf("Updated:     %s\n", FormatTimestamp(variable.UpdatedAt.Time))
	}
	return nil
}

// FormatVariableOverview formats a consolidated view of variables across scopes
func (f *TableFormatter) FormatVariableOverview(items []models.VariableOverview, total int) error {
	if len(items) == 0 {
		f.Info("No variables found")
		return nil
	}

	table := NewStyledTable([]string{"Variable", "Scope", "Scope Name", "Value", "Secret"})

	for _, item := range items {
		for i, def := range item.Definitions {
			varName := ""
			if i == 0 {
				varName = item.Name
			}

			scopeName := "-"
			if def.ScopeName != nil {
				scopeName = *def.ScopeName
			}

			value := def.Value
			if len(value) > 40 {
				value = value[:37] + "..."
			}

			secret := "-"
			if def.Secret {
				secret = "Yes"
			}

			table.Append([]string{
				varName,
				def.Scope,
				scopeName,
				value,
				secret,
			})
		}
	}

	table.Render()
	fmt.Printf("\nTotal: %d variables\n", total)
	return nil
}

// FormatBackupConfig formats a backup configuration
func (f *TableFormatter) FormatBackupConfig(config *models.BackupConfig) error {
	fmt.Printf("Organization:   %s\n", config.Organization)
	fmt.Printf("Status:         %s\n", StatusWithIcon(config.Status))
	fmt.Println()
	fmt.Printf("S3 Endpoint:    %s\n", config.S3Endpoint)
	fmt.Printf("S3 Bucket:      %s\n", config.S3Bucket)
	if config.S3Prefix != nil && *config.S3Prefix != "" {
		fmt.Printf("S3 Folder:      %s\n", *config.S3Prefix)
	}
	fmt.Printf("S3 Key ID:      %s\n", config.S3KeyID)
	fmt.Printf("Schedule:       %s\n", config.Schedule)
	fmt.Printf("Encryption Key: %s\n", EncryptionKeyWithIcon(config.HasEncryptionKey))
	fmt.Println()
	fmt.Printf("Created:        %s\n", FormatTimestamp(config.CreatedAt.Time))
	fmt.Printf("Updated:        %s\n", FormatTimestamp(config.UpdatedAt.Time))
	return nil
}

// FormatBackupConfigTest formats a backup config test result
func (f *TableFormatter) FormatBackupConfigTest(result *models.BackupConfigTestResponse) error {
	if result.Success {
		ColorSuccess.Printf("✓ %s\n", result.Message)
	} else {
		ColorError.Printf("✗ %s\n", result.Message)
	}
	return nil
}

// FormatDeviceBackupStatuses formats a list of device backup statuses as a table
func (f *TableFormatter) FormatDeviceBackupStatuses(statuses []models.DeviceBackupStatus, total int, enabledCount int) error {
	if len(statuses) == 0 {
		f.Info("No devices found")
		return nil
	}

	table := NewStyledTable([]string{"Device", "Backup", "Key", "Last Backup", "Status"})

	for _, s := range statuses {
		enabled := "Disabled"
		if s.Enabled {
			enabled = ColorEnabled.Sprint("Enabled")
		} else {
			enabled = ColorDisabled.Sprint("Disabled")
		}

		keyStatus := "org"
		if s.HasEncryptionKeyOverride {
			keyStatus = ColorInfo.Sprint("custom")
		} else if !s.Enabled {
			keyStatus = "-"
		}

		lastBackup := "Never"
		if s.LastBackupAt != nil && !s.LastBackupAt.IsZero() {
			lastBackup = RelativeTimeShort(s.LastBackupAt.Time)
		}

		backupStatus := "-"
		if s.LastBackupStatus != "" {
			backupStatus = BackupStatusWithIcon(s.LastBackupStatus)
		}

		table.Append([]string{
			s.DeviceName,
			enabled,
			keyStatus,
			lastBackup,
			backupStatus,
		})
	}

	table.Render()
	fmt.Printf("\nTotal: %d devices (%d with backup enabled)\n", total, enabledCount)
	return nil
}

// FormatDeviceBackupStatus formats a single device backup status
func (f *TableFormatter) FormatDeviceBackupStatus(status *models.DeviceBackupStatus) error {
	fmt.Printf("Device:       %s\n", status.DeviceName)
	fmt.Printf("Organization: %s\n", status.Organization)
	fmt.Printf("Backup:       %s\n", EnabledDisabledWithIcon(status.Enabled))
	fmt.Printf("Key Override: %s\n", KeyOverrideWithIcon(status.HasEncryptionKeyOverride))
	fmt.Println()

	if status.LastBackupAt != nil && !status.LastBackupAt.IsZero() {
		fmt.Printf("Last Backup:  %s (%s)\n", FormatTimestamp(status.LastBackupAt.Time), RelativeTime(status.LastBackupAt.Time))
		fmt.Printf("Status:       %s\n", BackupStatusWithIcon(status.LastBackupStatus))
		if status.LastBackupMessage != "" {
			fmt.Printf("Message:      %s\n", status.LastBackupMessage)
		}
	} else {
		fmt.Printf("Last Backup:  Never\n")
	}

	return nil
}

// FormatVpnNetworks formats a list of VPN networks as a table
func (f *TableFormatter) FormatVpnNetworks(networks []models.VpnNetwork, total int, quota *models.Quota) error {
	if len(networks) == 0 {
		f.Info("No VPN networks found")
		return nil
	}

	table := NewStyledTable([]string{"NAME", "CIDR", "AUTO-HUBS", "PORT", "MEMBERS", "OVERRIDES"})

	for _, n := range networks {
		autoHubs := "No"
		if n.AutoConnectHubs {
			autoHubs = "Yes"
		}
		table.Append([]string{
			n.Name,
			n.OverlayCIDRv4,
			autoHubs,
			fmt.Sprintf("%d", n.ListenPortDefault),
			fmt.Sprintf("%d", n.MemberCount),
			fmt.Sprintf("%d", n.LinkCount),
		})
	}

	table.Render()
	if quota != nil {
		fmt.Fprintf(os.Stdout, "\n%s\n", formatQuotaFooter("VPN networks", quota))
	}
	return nil
}

// FormatOrgQuota formats organization quota as a table
func (f *TableFormatter) FormatOrgQuota(quota *models.OrgQuota) error {
	if quota.Plan != nil {
		fmt.Fprintf(os.Stdout, "Plan: %s\n\n", quota.Plan.DisplayName)
	}

	table := NewStyledTable([]string{"Resource", "Limit", "Used", "Available"})
	table.Append([]string{"Devices", formatQuotaLimit(quota.Devices), fmt.Sprintf("%d", quota.Devices.Used), formatQuotaAvailable(quota.Devices)})
	table.Append([]string{"Users", formatQuotaLimit(quota.Users), fmt.Sprintf("%d", quota.Users.Used), formatQuotaAvailable(quota.Users)})
	table.Append([]string{"VPN Networks", formatQuotaLimit(quota.VpnNetworks), fmt.Sprintf("%d", quota.VpnNetworks.Used), formatQuotaAvailable(quota.VpnNetworks)})
	table.Append([]string{"Snippets", formatQuotaLimit(quota.Snippets), fmt.Sprintf("%d", quota.Snippets.Used), formatQuotaAvailable(quota.Snippets)})
	table.Render()

	fmt.Fprintln(os.Stdout)
	fmt.Fprintf(os.Stdout, "Features:\n")
	fmt.Fprintf(os.Stdout, "  Backup:       %s\n", EnabledDisabledWithIcon(quota.BackupEnabled))
	fmt.Fprintf(os.Stdout, "  Remote Admin: %s\n", EnabledDisabledWithIcon(quota.RemoteAdminEnabled))
	return nil
}

// formatQuotaLimit returns "∞" for unlimited or the numeric limit as string
func formatQuotaLimit(q models.Quota) string {
	if q.Unlimited {
		return "∞"
	}
	return fmt.Sprintf("%d", q.Limit)
}

// formatQuotaAvailable returns "∞" for unlimited or the available count as string
func formatQuotaAvailable(q models.Quota) string {
	if q.Unlimited {
		return "∞"
	}
	return fmt.Sprintf("%d", q.Available)
}

// formatQuotaFooter formats a one-line quota summary for list footers
func formatQuotaFooter(resourceName string, q *models.Quota) string {
	if q.Unlimited {
		return fmt.Sprintf("Quota: %s (Unlimited)", resourceName)
	}
	return fmt.Sprintf("Quota: %d/%d %s (%d available)", q.Used, q.Limit, resourceName, q.Available)
}

// FormatVpnNetwork formats a single VPN network with details
func (f *TableFormatter) FormatVpnNetwork(network *models.VpnNetwork) error {
	autoHubs := "No"
	if network.AutoConnectHubs {
		autoHubs = "Yes"
	}
	autoFW := "No"
	if network.AutoFirewallRules {
		autoFW = "Yes"
	}

	fmt.Printf("Name:           %s\n", network.Name)
	fmt.Printf("Overlay CIDR:   %s\n", network.OverlayCIDRv4)
	fmt.Printf("Auto-Hubs:      %s\n", autoHubs)
	fmt.Printf("Auto-FW Rules:  %s\n", autoFW)
	fmt.Printf("Listen Port:    %d\n", network.ListenPortDefault)
	if network.MTUDefault != nil {
		fmt.Printf("MTU:            %d\n", *network.MTUDefault)
	} else {
		fmt.Printf("MTU:            -\n")
	}
	if network.KeepaliveDefault != nil {
		fmt.Printf("Keepalive:      %d\n", *network.KeepaliveDefault)
	} else {
		fmt.Printf("Keepalive:      -\n")
	}
	if network.Organization != "" {
		fmt.Printf("Organization:   %s\n", network.Organization)
	}
	fmt.Printf("Members:        %d\n", network.MemberCount)
	fmt.Printf("Overrides:      %d\n", network.LinkCount)
	fmt.Println()
	fmt.Printf("Created:        %s\n", FormatTimestamp(network.CreatedAt.Time))
	fmt.Printf("Updated:        %s\n", FormatTimestamp(network.UpdatedAt.Time))

	return nil
}

// FormatVpnMembers formats a list of VPN members as a table
func (f *TableFormatter) FormatVpnMembers(members []models.VpnMember, total int) error {
	if len(members) == 0 {
		f.Info("No VPN members found")
		return nil
	}

	table := NewStyledTable([]string{"DEVICE", "ROLE", "ENABLED", "OVERLAY IP", "ENDPOINT", "TRANSIT"})

	for _, m := range members {
		enabled := "Yes"
		if !m.Enabled {
			enabled = "No"
		}

		endpoint := "-"
		if m.EndpointHost != nil && *m.EndpointHost != "" {
			endpoint = *m.EndpointHost
			if m.EndpointPort != nil {
				endpoint = fmt.Sprintf("%s:%d", endpoint, *m.EndpointPort)
			}
		}

		transit := "-"
		if m.TransitViaHub != nil && *m.TransitViaHub != "" {
			transit = *m.TransitViaHub
		}

		table.Append([]string{
			m.DeviceName,
			m.Role,
			enabled,
			m.OverlayIPv4,
			endpoint,
			transit,
		})
	}

	table.Render()
	return nil
}

// FormatVpnMember formats a single VPN member with details
func (f *TableFormatter) FormatVpnMember(member *models.VpnMember) error {
	enabled := "Yes"
	if !member.Enabled {
		enabled = "No"
	}

	fmt.Printf("VPN Network:    %s\n", member.VpnNetwork)
	fmt.Printf("Device:         %s\n", member.DeviceName)
	fmt.Printf("Role:           %s\n", member.Role)
	fmt.Printf("Enabled:        %s\n", enabled)
	fmt.Printf("Overlay IP:     %s\n", member.OverlayIPv4)
	fmt.Printf("Public Key:     %s\n", member.WgPublicKey)

	if member.EndpointHost != nil && *member.EndpointHost != "" {
		fmt.Printf("Endpoint Host:  %s\n", *member.EndpointHost)
	}
	if member.EndpointPort != nil {
		fmt.Printf("Endpoint Port:  %d\n", *member.EndpointPort)
	}
	if member.ListenPort != nil {
		fmt.Printf("Listen Port:    %d\n", *member.ListenPort)
	}
	if member.MTU != nil {
		fmt.Printf("MTU:            %d\n", *member.MTU)
	}
	if member.Keepalive != nil {
		fmt.Printf("Keepalive:      %d\n", *member.Keepalive)
	}
	if member.TransitViaHub != nil && *member.TransitViaHub != "" {
		fmt.Printf("Transit Hub:    %s\n", *member.TransitViaHub)
	}

	fmt.Println()
	fmt.Printf("Created:        %s\n", FormatTimestamp(member.CreatedAt.Time))
	fmt.Printf("Updated:        %s\n", FormatTimestamp(member.UpdatedAt.Time))

	return nil
}

// FormatVpnLinks formats a list of VPN links as a table
func (f *TableFormatter) FormatVpnLinks(links []models.VpnLink, total int) error {
	if len(links) == 0 {
		f.Info("No VPN links found")
		return nil
	}

	table := NewStyledTable([]string{"DEVICE A", "DEVICE B", "ENABLED", "PSK"})

	for _, l := range links {
		enabled := "Yes"
		if !l.Enabled {
			enabled = "No"
		}
		psk := "No"
		if l.HasPSK {
			psk = "Yes"
		}

		table.Append([]string{
			l.DeviceAName,
			l.DeviceBName,
			enabled,
			psk,
		})
	}

	table.Render()
	return nil
}

// FormatVpnLink formats a single VPN link with details
func (f *TableFormatter) FormatVpnLink(link *models.VpnLink) error {
	enabled := "Yes"
	if !link.Enabled {
		enabled = "No"
	}
	psk := "No"
	if link.HasPSK {
		psk = "Yes"
	}

	fmt.Printf("VPN Network:  %s\n", link.VpnNetwork)
	fmt.Printf("Device A:     %s\n", link.DeviceAName)
	fmt.Printf("Device B:     %s\n", link.DeviceBName)
	fmt.Printf("Enabled:      %s\n", enabled)
	fmt.Printf("PSK:          %s\n", psk)
	fmt.Println()
	fmt.Printf("Created:      %s\n", FormatTimestamp(link.CreatedAt.Time))
	fmt.Printf("Updated:      %s\n", FormatTimestamp(link.UpdatedAt.Time))

	return nil
}

// FormatVpnPrefixes formats a list of VPN member prefixes as a table
func (f *TableFormatter) FormatVpnPrefixes(prefixes []models.VpnMemberPrefix, total int) error {
	if len(prefixes) == 0 {
		f.Info("No VPN prefixes found")
		return nil
	}

	table := NewStyledTable([]string{"VARIABLE", "PUBLISH"})

	for _, p := range prefixes {
		publish := "Yes"
		if !p.Publish {
			publish = "No"
		}
		table.Append([]string{
			p.VariableName,
			publish,
		})
	}

	table.Render()
	return nil
}

// FormatVpnPrefix formats a single VPN member prefix with details
func (f *TableFormatter) FormatVpnPrefix(prefix *models.VpnMemberPrefix) error {
	publish := "Yes"
	if !prefix.Publish {
		publish = "No"
	}

	fmt.Printf("VPN Network:  %s\n", prefix.VpnNetwork)
	fmt.Printf("Device:       %s\n", prefix.DeviceName)
	fmt.Printf("Variable:     %s\n", prefix.VariableName)
	fmt.Printf("Publish:      %s\n", publish)
	fmt.Println()
	fmt.Printf("Created:      %s\n", FormatTimestamp(prefix.CreatedAt.Time))
	fmt.Printf("Updated:      %s\n", FormatTimestamp(prefix.UpdatedAt.Time))

	return nil
}

// FormatVpnConnections formats a list of effective VPN connections as a table
func (f *TableFormatter) FormatVpnConnections(connections []models.EffectiveConnection, total int) error {
	if len(connections) == 0 {
		f.Info("No VPN connections found")
		return nil
	}

	table := NewStyledTable([]string{"DEVICE A", "DEVICE B", "PAIR", "ACTIVE", "PSK", "NOTES"})

	for _, c := range connections {
		active := ColorEnabled.Sprint("Yes")
		if !c.Active {
			active = ColorDisabled.Sprint("No")
		}

		psk := "No"
		if c.HasPSK {
			psk = "Yes"
		}

		table.Append([]string{
			c.DeviceA,
			c.DeviceB,
			VpnPairTypeDisplay(c.PairType),
			active,
			psk,
			VpnConnectionNote(&c),
		})
	}

	table.Render()
	return nil
}

// FormatVpnConnection formats a single effective VPN connection with details
func (f *TableFormatter) FormatVpnConnection(connection *models.EffectiveConnection) error {
	fmt.Printf("Connection:   %s\n", VpnConnectionLine(connection))
	fmt.Printf("Type:         %s\n", VpnTypeValue(connection.PairType, connection.Source))
	fmt.Printf("Active:       %s\n", activeYesNo(connection.Active))
	fmt.Printf("PSK:          %s\n", yesNo(connection.HasPSK))
	fmt.Println()
	explanation := VpnConnectionExplanation(connection)
	for _, line := range strings.Split(explanation, "\n") {
		ColorDim.Printf("  %s\n", line)
	}
	return nil
}

func activeYesNo(v bool) string {
	if v {
		return ColorEnabled.Sprint("Yes")
	}
	return ColorDisabled.Sprint("No")
}

func yesNo(v bool) string {
	if v {
		return "Yes"
	}
	return "No"
}
