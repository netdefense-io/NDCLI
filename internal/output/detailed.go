package output

import (
	"fmt"
	"strings"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// DetailedFormatter formats output with full details
type DetailedFormatter struct {
	BaseFormatter
}

// NewDetailedFormatter creates a new detailed formatter
func NewDetailedFormatter() *DetailedFormatter {
	return &DetailedFormatter{BaseFormatter: NewBaseFormatter()}
}

func (f *DetailedFormatter) printLabel(label string) {
	ColorLabel.Fprintf(f.Writer, "  %s: ", label)
}

func (f *DetailedFormatter) printValue(value string) {
	fmt.Fprintln(f.Writer, value)
}

func (f *DetailedFormatter) printLabelValue(label, value string) {
	f.printLabel(label)
	f.printValue(value)
}

// FormatDevices formats a list of devices with full details
func (f *DetailedFormatter) FormatDevices(devices []models.Device, total int, quota *models.Quota) error {
	if len(devices) == 0 {
		f.Info("No devices found")
		return nil
	}

	if quota != nil {
		if quota.Unlimited {
			fmt.Fprintf(f.Writer, "Quota: enabled devices (Unlimited)\n\n")
		} else {
			fmt.Fprintf(f.Writer, "Quota: %d/%d enabled devices (%d available)\n\n", quota.Used, quota.Limit, quota.Available)
		}
	}

	for i, d := range devices {
		if i > 0 {
			fmt.Fprintln(f.Writer)
		}
		f.formatDeviceRich(&d)
	}
	return nil
}

// formatDeviceRich formats a single device with rich box drawing
func (f *DetailedFormatter) formatDeviceRich(d *models.Device) {
	const width = 55

	// Header box
	box := NewBox(width)
	fmt.Fprintln(f.Writer, box.TopLineWithTitle("Device"))
	ColorHeader.Fprintf(f.Writer, "%s  %s", BoxVertical, d.Name)
	// padding = width - 1(│) - 2(spaces) - name_len - 1(space) - 1(│)
	padding := width - 5 - len(d.Name)
	fmt.Fprintf(f.Writer, "%s %s\n", strings.Repeat(" ", padding), BoxVertical)
	if d.UUID != "" {
		ColorDim.Fprintf(f.Writer, "%s  UUID: %s", BoxVertical, d.UUID)
		// "UUID: " is 6 chars, so: width - 1 - 2 - 6 - uuid_len - 1 - 1 = width - 11 - uuid_len
		padding = width - 11 - len(d.UUID)
		fmt.Fprintf(f.Writer, "%s %s\n", strings.Repeat(" ", padding), BoxVertical)
	}
	fmt.Fprintln(f.Writer, box.BottomLine())
	fmt.Fprintln(f.Writer)

	// Status row
	fmt.Fprintf(f.Writer, "  %-12s %s", "Status", StatusIndicator(d.Status)+" "+ColoredStatus(d.Status))
	fmt.Fprintf(f.Writer, "%s", strings.Repeat(" ", 10-len(d.Status)))
	autoSyncStr := "No"
	if d.AutoSync {
		autoSyncStr = "Yes"
	}
	fmt.Fprintf(f.Writer, "%-12s %s\n", "Auto-Sync", autoSyncStr)

	// Details
	fmt.Fprintf(f.Writer, "  %-12s %s\n", "Organization", d.Organization)
	if len(d.OrganizationalUnits) > 0 {
		fmt.Fprintf(f.Writer, "  %-12s %s\n", "OUs", d.GetOUsDisplay())
	}
	if d.Version != "" {
		fmt.Fprintf(f.Writer, "  %-12s %s\n", "Version", d.Version)
	}
	fmt.Fprintln(f.Writer)

	// Sync info
	if d.SyncedAt != nil && !d.SyncedAt.Time.IsZero() {
		fmt.Fprintf(f.Writer, "  %-12s %s (%s)\n", "Synced At", FormatTimestamp(d.SyncedAt.Time), RelativeTime(d.SyncedAt.Time))
	} else {
		fmt.Fprintf(f.Writer, "  %-12s %s\n", "Synced At", "Never")
	}

	// Timing info
	if !d.Heartbeat.IsZero() {
		fmt.Fprintf(f.Writer, "  %-12s %s (%s)\n", "Heartbeat", FormatTimestamp(d.Heartbeat.Time), RelativeTime(d.Heartbeat.Time))
	}
	fmt.Fprintf(f.Writer, "  %-12s %s\n", "Created", FormatTimestamp(d.CreatedAt.Time))
	if !d.UpdatedAt.IsZero() {
		fmt.Fprintf(f.Writer, "  %-12s %s\n", "Updated", FormatTimestamp(d.UpdatedAt.Time))
	}
}

// FormatDevice formats a single device with full details
func (f *DetailedFormatter) FormatDevice(device *models.Device) error {
	return f.FormatDevices([]models.Device{*device}, 1, nil)
}

// FormatTasks formats a list of tasks with full details
func (f *DetailedFormatter) FormatTasks(tasks []models.Task, total int) error {
	if len(tasks) == 0 {
		f.Info("No tasks found")
		return nil
	}

	for i, t := range tasks {
		if i > 0 {
			fmt.Fprintln(f.Writer)
		}
		ColorHeader.Fprintf(f.Writer, "─── Task %s ───\n", t.ID[:8])
		f.printLabelValue("ID", t.ID)
		f.printLabelValue("Type", t.Type)
		f.printLabel("Status")
		fmt.Fprintln(f.Writer, ColoredStatus(t.Status))
		f.printLabelValue("Device", t.DeviceName)
		f.printLabelValue("Created", FormatTimestamp(t.CreatedAt.Time))
		if !t.StartedAt.IsZero() {
			f.printLabelValue("Started", FormatTimestamp(t.StartedAt.Time))
		}
		if !t.CompletedAt.IsZero() {
			f.printLabelValue("Completed", FormatTimestamp(t.CompletedAt.Time))
		}
		if t.ErrorMessage != "" {
			ColorError.Fprintf(f.Writer, "  Error: %s\n", t.ErrorMessage)
		}
	}
	return nil
}

// FormatTask formats a single task with full details
func (f *DetailedFormatter) FormatTask(task *models.Task) error {
	const width = 55

	// Header box
	box := NewBox(width)
	fmt.Fprintln(f.Writer, box.TopLineWithTitle("Task"))
	ColorHeader.Fprintf(f.Writer, "%s  %s", BoxVertical, task.ID)
	padding := width - 5 - len(task.ID)
	fmt.Fprintf(f.Writer, "%s %s\n", strings.Repeat(" ", padding), BoxVertical)
	ColorDim.Fprintf(f.Writer, "%s  %s", BoxVertical, task.Type)
	padding = width - 5 - len(task.Type)
	fmt.Fprintf(f.Writer, "%s %s\n", strings.Repeat(" ", padding), BoxVertical)
	fmt.Fprintln(f.Writer, box.BottomLine())
	fmt.Fprintln(f.Writer)

	// Status row
	fmt.Fprintf(f.Writer, "  %-12s %s %s\n", "Status", StatusIndicator(task.Status), ColoredStatus(task.Status))
	fmt.Fprintf(f.Writer, "  %-12s %s\n", "Device", task.DeviceName)
	fmt.Fprintf(f.Writer, "  %-12s %s\n", "Organization", task.Organization)
	fmt.Fprintln(f.Writer)

	// Timestamps
	fmt.Fprintf(f.Writer, "  %-12s %s\n", "Created", FormatTimestamp(task.CreatedAt.Time))
	if !task.ExpiresAt.IsZero() {
		fmt.Fprintf(f.Writer, "  %-12s %s\n", "Expires", FormatTimestamp(task.ExpiresAt.Time))
	}
	if !task.StartedAt.IsZero() {
		fmt.Fprintf(f.Writer, "  %-12s %s\n", "Started", FormatTimestamp(task.StartedAt.Time))
	}
	if !task.CompletedAt.IsZero() {
		fmt.Fprintf(f.Writer, "  %-12s %s\n", "Completed", FormatTimestamp(task.CompletedAt.Time))
	}

	// Message
	if task.Message != "" {
		fmt.Fprintln(f.Writer)
		sectionBox := NewSharpBox(width)
		fmt.Fprintln(f.Writer, sectionBox.TopLineWithTitle("Message"))
		// Split message into lines and display each
		lines := strings.Split(task.Message, "\n")
		for _, line := range lines {
			if len(line) > width-4 {
				// Truncate long lines
				fmt.Fprintln(f.Writer, sectionBox.ContentLine(line[:width-7]+"..."))
			} else {
				fmt.Fprintln(f.Writer, sectionBox.ContentLine(line))
			}
		}
		fmt.Fprintln(f.Writer, sectionBox.BottomLine())
	}

	// Error message
	if task.ErrorMessage != "" {
		fmt.Fprintln(f.Writer)
		ColorError.Fprintf(f.Writer, "  Error: %s\n", task.ErrorMessage)
	}

	return nil
}

// FormatOrganizations formats a list of organizations with full details
func (f *DetailedFormatter) FormatOrganizations(orgs []models.Organization) error {
	if len(orgs) == 0 {
		f.Info("No organizations found")
		return nil
	}

	for i, o := range orgs {
		if i > 0 {
			fmt.Fprintln(f.Writer)
		}
		f.formatOrganizationRich(&o)
	}
	return nil
}

// formatOrganizationRich formats a single organization with rich box drawing
func (f *DetailedFormatter) formatOrganizationRich(o *models.Organization) {
	const width = 55

	// Header box
	box := NewBox(width)
	fmt.Fprintln(f.Writer, box.TopLineWithTitle("Organization"))
	ColorHeader.Fprintf(f.Writer, "%s  %s", BoxVertical, o.Name)
	// padding = width - 1(│) - 2(spaces) - name_len - 1(space) - 1(│)
	padding := width - 5 - len(o.Name)
	fmt.Fprintf(f.Writer, "%s %s\n", strings.Repeat(" ", padding), BoxVertical)
	fmt.Fprintln(f.Writer, box.BottomLine())
	fmt.Fprintln(f.Writer)

	// Status row
	role := o.GetRole()
	fmt.Fprintf(f.Writer, "  %-12s %s", "Status", ColoredStatus(o.Status))
	fmt.Fprintf(f.Writer, "%s", strings.Repeat(" ", 14-len(o.Status)))
	fmt.Fprintf(f.Writer, "%-12s %s\n", "Your Role", ColoredRole(role))

	// Default OU
	if defaultOU := o.GetDefaultOU(); defaultOU != "" {
		fmt.Fprintf(f.Writer, "  %-12s %s\n", "Default OU", defaultOU)
	}
	fmt.Fprintln(f.Writer)

	// Members section (if we have member count data)
	if o.MemberCount > 0 || len(o.MemberCountsByRole) > 0 {
		sectionBox := NewSharpBox(width)
		fmt.Fprintln(f.Writer, sectionBox.TopLineWithTitle("Members"))

		if len(o.MemberCountsByRole) > 0 {
			su := o.MemberCountsByRole["SU"]
			rw := o.MemberCountsByRole["RW"]
			ro := o.MemberCountsByRole["RO"]
			fmt.Fprintln(f.Writer, sectionBox.ContentLine(fmt.Sprintf("Total: %d", o.MemberCount)))
			fmt.Fprintln(f.Writer, sectionBox.ContentLine(fmt.Sprintf("%s Superusers (SU): %d", Bullet(""), su)))
			fmt.Fprintln(f.Writer, sectionBox.ContentLine(fmt.Sprintf("%s Read-Write (RW): %d", Bullet(""), rw)))
			fmt.Fprintln(f.Writer, sectionBox.ContentLine(fmt.Sprintf("%s Read-Only (RO):  %d", Bullet(""), ro)))
		} else {
			fmt.Fprintln(f.Writer, sectionBox.ContentLine(fmt.Sprintf("Total: %d", o.MemberCount)))
		}

		if len(o.MemberCountsByStatus) > 0 {
			fmt.Fprintln(f.Writer, sectionBox.EmptyLine())
			enabled := o.MemberCountsByStatus["ENABLED"]
			disabled := o.MemberCountsByStatus["DISABLED"]
			invited := o.MemberCountsByStatus["INVITED"]
			declined := o.MemberCountsByStatus["DECLINED"]
			fmt.Fprintln(f.Writer, sectionBox.ContentLine("By Status:"))
			line := fmt.Sprintf("%s Enabled: %-3d    %s Disabled: %d", Bullet(""), enabled, Bullet(""), disabled)
			fmt.Fprintln(f.Writer, sectionBox.ContentLine(line))
			line = fmt.Sprintf("%s Invited: %-3d    %s Declined: %d", Bullet(""), invited, Bullet(""), declined)
			fmt.Fprintln(f.Writer, sectionBox.ContentLine(line))
		}
		fmt.Fprintln(f.Writer, sectionBox.BottomLine())
		fmt.Fprintln(f.Writer)
	}

	// Resources section
	if o.DeviceCount > 0 {
		sectionBox := NewSharpBox(width)
		fmt.Fprintln(f.Writer, sectionBox.TopLineWithTitle("Resources"))
		fmt.Fprintln(f.Writer, sectionBox.ContentLine(fmt.Sprintf("Devices: %d", o.DeviceCount)))
		fmt.Fprintln(f.Writer, sectionBox.BottomLine())
		fmt.Fprintln(f.Writer)
	}

	// Footer info
	if len(o.Owners) > 0 {
		fmt.Fprintf(f.Writer, "  %-12s %s\n", "Owners", strings.Join(o.Owners, ", "))
	}
	if o.Token != "" {
		fmt.Fprintf(f.Writer, "  %-12s %s\n", "Token", o.Token)
	}
	fmt.Fprintf(f.Writer, "  %-12s %s\n", "Created", FormatTimestamp(o.CreatedAt.Time))
	if !o.UpdatedAt.IsZero() {
		fmt.Fprintf(f.Writer, "  %-12s %s\n", "Updated", FormatTimestamp(o.UpdatedAt.Time))
	}
}

// FormatOrganization formats a single organization with full details
func (f *DetailedFormatter) FormatOrganization(org *models.Organization) error {
	return f.FormatOrganizations([]models.Organization{*org})
}

// FormatOUs formats a list of organizational units with full details
func (f *DetailedFormatter) FormatOUs(ous []models.OrganizationalUnit) error {
	if len(ous) == 0 {
		f.Info("No organizational units found")
		return nil
	}

	for i, ou := range ous {
		if i > 0 {
			fmt.Fprintln(f.Writer)
		}
		ColorHeader.Fprintf(f.Writer, "─── %s ───\n", ou.Name)
		f.printLabelValue("Organization", ou.Organization)
		f.printLabelValue("Status", ColoredStatus(ou.Status))
		if ou.Description != "" {
			f.printLabelValue("Description", ou.Description)
		}
		f.printLabelValue("Devices", fmt.Sprintf("%d", ou.DeviceCount))
		f.printLabelValue("Templates", fmt.Sprintf("%d", ou.TemplateCount))
		f.printLabelValue("Created", FormatTimestamp(ou.CreatedAt.Time))
	}
	return nil
}

// FormatOU formats a single organizational unit with full details
func (f *DetailedFormatter) FormatOU(ou *models.OrganizationalUnit) error {
	ColorHeader.Fprintf(f.Writer, "─── %s ───\n", ou.Name)
	f.printLabelValue("Organization", ou.Organization)
	f.printLabelValue("Status", ColoredStatus(ou.Status))
	if ou.Description != "" {
		f.printLabelValue("Description", ou.Description)
	}
	f.printLabelValue("Created", FormatTimestamp(ou.CreatedAt.Time))
	f.printLabelValue("Updated", FormatTimestamp(ou.UpdatedAt.Time))

	// Devices section
	fmt.Fprintf(f.Writer, "\n")
	ColorHeader.Fprintf(f.Writer, "Devices (%d)\n", len(ou.Devices))
	if len(ou.Devices) == 0 {
		fmt.Fprintf(f.Writer, "  (none)\n")
	} else {
		for _, d := range ou.Devices {
			fmt.Fprintf(f.Writer, "  • %s\n", d.Name)
		}
	}

	// Templates section
	fmt.Fprintf(f.Writer, "\n")
	ColorHeader.Fprintf(f.Writer, "Templates (%d)\n", len(ou.Templates))
	if len(ou.Templates) == 0 {
		fmt.Fprintf(f.Writer, "  (none)\n")
	} else {
		for _, t := range ou.Templates {
			fmt.Fprintf(f.Writer, "  • %s (%d snippets)\n", t.Name, t.SnippetCount)
		}
	}

	return nil
}

// FormatTemplates formats a list of templates with full details
func (f *DetailedFormatter) FormatTemplates(templates []models.Template) error {
	if len(templates) == 0 {
		f.Info("No templates found")
		return nil
	}

	for i, t := range templates {
		if i > 0 {
			fmt.Fprintln(f.Writer)
		}
		ColorHeader.Fprintf(f.Writer, "─── %s ───\n", t.Name)
		if t.Description != "" {
			f.printLabelValue("Description", t.Description)
		}
		f.printLabelValue("Position", t.Position)
		f.printLabelValue("Snippets", fmt.Sprintf("%d", t.SnippetCount))
		f.printLabelValue("Created", FormatTimestamp(t.CreatedAt.Time))
	}
	return nil
}

// FormatTemplate formats a single template with full details and snippets
func (f *DetailedFormatter) FormatTemplate(template *models.Template) error {
	const width = 55

	// Header box
	box := NewBox(width)
	fmt.Fprintln(f.Writer, box.TopLineWithTitle("Template"))
	ColorHeader.Fprintf(f.Writer, "%s  %s", BoxVertical, template.Name)
	padding := width - 5 - len(template.Name)
	fmt.Fprintf(f.Writer, "%s %s\n", strings.Repeat(" ", padding), BoxVertical)
	fmt.Fprintln(f.Writer, box.BottomLine())
	fmt.Fprintln(f.Writer)

	// Details
	if template.Description != "" {
		fmt.Fprintf(f.Writer, "  %-12s %s\n", "Description", template.Description)
	}
	fmt.Fprintf(f.Writer, "  %-12s %s\n", "Position", template.Position)
	fmt.Fprintf(f.Writer, "  %-12s %s\n", "Created", FormatTimestamp(template.CreatedAt.Time))
	fmt.Fprintf(f.Writer, "  %-12s %s\n", "Updated", FormatTimestamp(template.UpdatedAt.Time))
	fmt.Fprintln(f.Writer)

	// Snippets section
	if len(template.Snippets) == 0 {
		f.Info("No snippets in this template")
		return nil
	}

	sectionBox := NewSharpBox(width)
	fmt.Fprintln(f.Writer, sectionBox.TopLineWithTitle(fmt.Sprintf("Snippets (%d)", len(template.Snippets))))

	for _, s := range template.Snippets {
		line := fmt.Sprintf("%s %s (priority: %d, type: %s)", Bullet(""), s.Name, s.Priority, s.Type)
		fmt.Fprintln(f.Writer, sectionBox.ContentLine(line))
	}

	fmt.Fprintln(f.Writer, sectionBox.BottomLine())
	return nil
}

// FormatSnippets formats a list of snippets with full details
func (f *DetailedFormatter) FormatSnippets(snippets []models.Snippet) error {
	if len(snippets) == 0 {
		f.Info("No snippets found")
		return nil
	}

	for i, s := range snippets {
		if i > 0 {
			fmt.Fprintln(f.Writer)
		}
		ColorHeader.Fprintf(f.Writer, "─── %s ───\n", s.Name)
		f.printLabelValue("Type", s.Type)
		f.printLabelValue("Priority", fmt.Sprintf("%d", s.Priority))
		f.printLabelValue("Updated", FormatTimestamp(s.UpdatedAt.Time))
	}
	return nil
}

// FormatSnippet formats a single snippet with full details
func (f *DetailedFormatter) FormatSnippet(snippet *models.Snippet) error {
	return f.FormatSnippets([]models.Snippet{*snippet})
}

// FormatAccounts formats a list of accounts with full details
func (f *DetailedFormatter) FormatAccounts(accounts []models.Account, quota *models.Quota) error {
	if len(accounts) == 0 {
		f.Info("No accounts found")
		return nil
	}

	if quota != nil {
		if quota.Unlimited {
			fmt.Fprintf(f.Writer, "Quota: users (Unlimited)\n\n")
		} else {
			fmt.Fprintf(f.Writer, "Quota: %d/%d users (%d available)\n\n", quota.Used, quota.Limit, quota.Available)
		}
	}

	for i, a := range accounts {
		if i > 0 {
			fmt.Fprintln(f.Writer)
		}
		ColorHeader.Fprintf(f.Writer, "─── %s ───\n", a.Email)
		f.printLabel("Role")
		fmt.Fprintln(f.Writer, ColoredRole(a.Role))
		f.printLabel("Status")
		fmt.Fprintln(f.Writer, ColoredStatus(a.Status))
		if !a.LastLogin.IsZero() {
			f.printLabelValue("Last Login", FormatTimestamp(a.LastLogin.Time))
		}
	}
	return nil
}

// FormatInvitations formats a list of invitations with full details
func (f *DetailedFormatter) FormatInvitations(invitations []models.Invitation) error {
	if len(invitations) == 0 {
		f.Info("No invitations found")
		return nil
	}

	for i, inv := range invitations {
		if i > 0 {
			fmt.Fprintln(f.Writer)
		}
		ColorHeader.Fprintf(f.Writer, "─── Invitation ───\n")
		f.printLabelValue("Email", inv.Email)
		f.printLabelValue("Organization", inv.Organization)
		f.printLabel("Role")
		fmt.Fprintln(f.Writer, ColoredRole(inv.Role))
		f.printLabel("Status")
		fmt.Fprintln(f.Writer, ColoredStatus(inv.Status))
		f.printLabelValue("Expires", FormatTimestamp(inv.ExpiresAt.Time))
	}
	return nil
}

// FormatInvites formats the invites response (received and sent) with full details
func (f *DetailedFormatter) FormatInvites(invites *models.InvitesResponse) error {
	if len(invites.Received) == 0 && len(invites.Sent) == 0 {
		f.Info("No invitations found")
		return nil
	}

	const width = 55

	if len(invites.Received) > 0 {
		sectionBox := NewSharpBox(width)
		fmt.Fprintln(f.Writer, sectionBox.TopLineWithTitle("Received Invitations"))

		for _, inv := range invites.Received {
			line := fmt.Sprintf("%s %s [%s] %s", Bullet(""), inv.Organization, ColoredRole(inv.Role), ColoredStatus(inv.Status))
			fmt.Fprintln(f.Writer, sectionBox.ContentLine(line))
			fromLine := fmt.Sprintf("    From: %s (%s)", inv.InvitedBy, FormatTimestamp(inv.CreatedAt.Time))
			fmt.Fprintln(f.Writer, sectionBox.ContentLine(fromLine))
		}

		fmt.Fprintln(f.Writer, sectionBox.BottomLine())
	}

	if len(invites.Sent) > 0 {
		if len(invites.Received) > 0 {
			fmt.Fprintln(f.Writer)
		}
		sectionBox := NewSharpBox(width)
		fmt.Fprintln(f.Writer, sectionBox.TopLineWithTitle("Sent Invitations"))

		for _, inv := range invites.Sent {
			line := fmt.Sprintf("%s %s → %s [%s] %s", Bullet(""), inv.Email, inv.Organization, ColoredRole(inv.Role), ColoredStatus(inv.Status))
			fmt.Fprintln(f.Writer, sectionBox.ContentLine(line))
			dateLine := fmt.Sprintf("    Sent: %s", FormatTimestamp(inv.CreatedAt.Time))
			fmt.Fprintln(f.Writer, sectionBox.ContentLine(dateLine))
		}

		fmt.Fprintln(f.Writer, sectionBox.BottomLine())
	}

	return nil
}

// FormatAuthMe formats the authenticated user's profile with full details
func (f *DetailedFormatter) FormatAuthMe(authMe *models.AuthMe) error {
	const width = 55

	// Header box
	box := NewBox(width)
	fmt.Fprintln(f.Writer, box.TopLineWithTitle("User Profile"))

	// Email as main identifier
	ColorHeader.Fprintf(f.Writer, "%s  %s", BoxVertical, authMe.Email)
	padding := width - 5 - len(authMe.Email)
	fmt.Fprintf(f.Writer, "%s %s\n", strings.Repeat(" ", padding), BoxVertical)

	// Name if available
	name := authMe.GetName()
	if name != "" {
		ColorDim.Fprintf(f.Writer, "%s  %s", BoxVertical, name)
		padding = width - 5 - len(name)
		fmt.Fprintf(f.Writer, "%s %s\n", strings.Repeat(" ", padding), BoxVertical)
	}
	fmt.Fprintln(f.Writer, box.BottomLine())
	fmt.Fprintln(f.Writer)

	// Status row
	fmt.Fprintf(f.Writer, "  %-12s %s\n", "Status", ColoredStatus(authMe.Status))

	// Timestamps
	fmt.Fprintf(f.Writer, "  %-12s %s\n", "Created", FormatTimestamp(authMe.CreatedAt.Time))
	fmt.Fprintf(f.Writer, "  %-12s %s\n", "Updated", FormatTimestamp(authMe.UpdatedAt.Time))
	fmt.Fprintln(f.Writer)

	// Organizations section
	if len(authMe.Organizations) > 0 {
		sectionBox := NewSharpBox(width)
		fmt.Fprintln(f.Writer, sectionBox.TopLineWithTitle("Organizations"))

		for _, org := range authMe.Organizations {
			line := fmt.Sprintf("%s %s [%s] %s", Bullet(""), org.Name, ColoredRole(org.Role), ColoredStatus(org.Status))
			fmt.Fprintln(f.Writer, sectionBox.ContentLine(line))
		}

		fmt.Fprintln(f.Writer, sectionBox.BottomLine())
	}

	return nil
}

// FormatAuthMeUpdate formats the auth me update response with full details
func (f *DetailedFormatter) FormatAuthMeUpdate(resp *models.AuthMeUpdateResponse) error {
	const width = 55

	ColorSuccess.Fprintf(f.Writer, "✓ %s\n", resp.Message)

	if len(resp.PendingInvites) > 0 {
		fmt.Fprintln(f.Writer)
		sectionBox := NewSharpBox(width)
		fmt.Fprintln(f.Writer, sectionBox.TopLineWithTitle("Pending Invites"))

		for _, inv := range resp.PendingInvites {
			line := fmt.Sprintf("%s %s [%s]", Bullet(""), inv.Organization, ColoredRole(inv.Role))
			fmt.Fprintln(f.Writer, sectionBox.ContentLine(line))
			invitedLine := fmt.Sprintf("    Invited by: %s (%s)", inv.InvitedBy, RelativeTime(inv.CreatedAt.Time))
			fmt.Fprintln(f.Writer, sectionBox.ContentLine(invitedLine))
		}

		fmt.Fprintln(f.Writer, sectionBox.BottomLine())
	}

	return nil
}

// FormatSyncStatus formats sync status with full details
func (f *DetailedFormatter) FormatSyncStatus(items []models.SyncStatusItem, total int) error {
	if len(items) == 0 {
		f.Info("No devices found")
		return nil
	}

	const width = 55

	for i, item := range items {
		if i > 0 {
			fmt.Fprintln(f.Writer)
		}

		// Header
		box := NewBox(width)
		fmt.Fprintln(f.Writer, box.TopLineWithTitle("Device"))
		ColorHeader.Fprintf(f.Writer, "%s  %s", BoxVertical, item.DeviceName)
		padding := width - 5 - len(item.DeviceName)
		fmt.Fprintf(f.Writer, "%s %s\n", strings.Repeat(" ", padding), BoxVertical)
		fmt.Fprintln(f.Writer, box.BottomLine())
		fmt.Fprintln(f.Writer)

		// Status row
		var status string
		if item.Error != nil && *item.Error != "" {
			status = "ERROR"
		} else if item.IsSynced() {
			status = "SYNCED"
		} else {
			status = "NOT SYNCED"
		}
		autoSync := "No"
		if item.AutoSync {
			autoSync = "Yes"
		}
		fmt.Fprintf(f.Writer, "  %-12s %s %s", "Status", StatusIndicator(status), ColoredStatus(status))
		statusPadding := 12 - len(status)
		if statusPadding < 2 {
			statusPadding = 2
		}
		fmt.Fprintf(f.Writer, "%s", strings.Repeat(" ", statusPadding))
		fmt.Fprintf(f.Writer, "%-12s %s\n", "Auto-Sync", autoSync)

		// Error
		if item.Error != nil && *item.Error != "" {
			ColorError.Fprintf(f.Writer, "  %-12s %s\n", "Error", *item.Error)
		}

		// OUs
		if len(item.OUs) > 0 {
			fmt.Fprintf(f.Writer, "  %-12s %s\n", "OUs", item.GetOUsDisplay())
		}

		// Sync time
		if item.SyncedAt != nil && !item.SyncedAt.Time.IsZero() {
			fmt.Fprintf(f.Writer, "  %-12s %s (%s)\n", "Synced At", FormatTimestamp(item.SyncedAt.Time), RelativeTime(item.SyncedAt.Time))
		} else {
			fmt.Fprintf(f.Writer, "  %-12s %s\n", "Synced At", "Never")
		}

		// Current hash
		if item.CurrentHash != nil && *item.CurrentHash != "" {
			fmt.Fprintf(f.Writer, "  %-12s %s\n", "Current Hash", *item.CurrentHash)
		}
	}

	fmt.Fprintf(f.Writer, "\nTotal: %d devices\n", total)
	return nil
}

// FormatSyncApply formats sync apply result with full details
func (f *DetailedFormatter) FormatSyncApply(result *models.SyncApplyResponse) error {
	const width = 55

	ColorSuccess.Fprintf(f.Writer, "✓ %s\n", result.Message)
	fmt.Fprintln(f.Writer)
	fmt.Fprintf(f.Writer, "  %-20s %d\n", "Devices affected", result.DevicesAffected)
	fmt.Fprintf(f.Writer, "  %-20s %d\n", "Skipped", result.Skipped)

	if len(result.Tasks) > 0 {
		fmt.Fprintln(f.Writer)
		sectionBox := NewSharpBox(width)
		fmt.Fprintln(f.Writer, sectionBox.TopLineWithTitle("Created Tasks"))

		for _, t := range result.Tasks {
			line := fmt.Sprintf("%s %s → %s (%d snippets, %d vpn networks)", Bullet(""), t.Task, t.DeviceName, t.SnippetCount, t.VpnNetworkCount)
			fmt.Fprintln(f.Writer, sectionBox.ContentLine(line))
		}

		fmt.Fprintln(f.Writer, sectionBox.BottomLine())
	}

	if len(result.Errors) > 0 {
		fmt.Fprintln(f.Writer)
		ColorError.Fprintln(f.Writer, "Errors:")
		for _, e := range result.Errors {
			fmt.Fprintf(f.Writer, "  • %s: %s\n", e.DeviceName, e.Error)
			for _, c := range e.Conflicts {
				fmt.Fprintf(f.Writer, "      %s\n", c.Message)
			}
			if len(e.UndefinedVariables) > 0 {
				fmt.Fprintf(f.Writer, "      Undefined: %s\n", formatVarListDetailed(e.UndefinedVariables))
			}
		}
	}

	return nil
}

// formatVarListDetailed formats variable names for detailed output
func formatVarListDetailed(vars []string) string {
	formatted := make([]string, len(vars))
	for i, v := range vars {
		formatted[i] = "${" + v + "}"
	}
	return strings.Join(formatted, ", ")
}

// FormatVariables formats a list of variables with full details
func (f *DetailedFormatter) FormatVariables(variables []models.Variable, total int) error {
	if len(variables) == 0 {
		f.Info("No variables found")
		return nil
	}

	const width = 60

	for i, v := range variables {
		if i > 0 {
			fmt.Fprintln(f.Writer)
		}

		box := NewBox(width)
		fmt.Fprintln(f.Writer, box.TopLineWithTitle(v.Name))

		// Value (may be multiline)
		valueLines := strings.Split(v.Value, "\n")
		if len(valueLines) == 1 && len(v.Value) <= 45 {
			fmt.Fprintln(f.Writer, box.ContentLine(fmt.Sprintf("  %-10s %s", "Value", v.Value)))
		} else {
			fmt.Fprintln(f.Writer, box.ContentLine(fmt.Sprintf("  %-10s", "Value")))
			for _, line := range valueLines {
				if len(line) > 50 {
					line = line[:47] + "..."
				}
				fmt.Fprintln(f.Writer, box.ContentLine(fmt.Sprintf("    %s", line)))
			}
		}

		if v.Description != "" {
			fmt.Fprintln(f.Writer, box.ContentLine(fmt.Sprintf("  %-10s %s", "Desc", v.Description)))
		}

		scopeInfo := v.Scope
		if v.ScopeName != nil {
			scopeInfo = fmt.Sprintf("%s: %s", v.Scope, *v.ScopeName)
		}
		fmt.Fprintln(f.Writer, box.ContentLine(fmt.Sprintf("  %-10s %s", "Scope", scopeInfo)))

		if v.Secret {
			fmt.Fprintln(f.Writer, box.ContentLine(fmt.Sprintf("  %-10s %s", "Secret", "Yes")))
		}

		fmt.Fprintln(f.Writer, box.ContentLine(fmt.Sprintf("  %-10s %s", "Created", FormatTimestamp(v.CreatedAt.Time))))
		if v.UpdatedAt != nil && !v.UpdatedAt.IsZero() {
			fmt.Fprintln(f.Writer, box.ContentLine(fmt.Sprintf("  %-10s %s", "Updated", FormatTimestamp(v.UpdatedAt.Time))))
		}

		fmt.Fprintln(f.Writer, box.BottomLine())
	}

	return nil
}

// FormatVariable formats a single variable with full details
func (f *DetailedFormatter) FormatVariable(variable *models.Variable) error {
	const width = 60

	box := NewBox(width)
	fmt.Fprintln(f.Writer, box.TopLineWithTitle(variable.Name))

	// Value (may be multiline, show full value for single variable view)
	valueLines := strings.Split(variable.Value, "\n")
	fmt.Fprintln(f.Writer, box.ContentLine(fmt.Sprintf("  %-12s", "Value")))
	for _, line := range valueLines {
		fmt.Fprintln(f.Writer, box.ContentLine(fmt.Sprintf("    %s", line)))
	}

	if variable.Description != "" {
		fmt.Fprintln(f.Writer, box.ContentLine(""))
		fmt.Fprintln(f.Writer, box.ContentLine(fmt.Sprintf("  %-12s %s", "Description", variable.Description)))
	}

	fmt.Fprintln(f.Writer, box.ContentLine(""))
	scopeInfo := variable.Scope
	if variable.ScopeName != nil {
		scopeInfo = fmt.Sprintf("%s: %s", variable.Scope, *variable.ScopeName)
	}
	fmt.Fprintln(f.Writer, box.ContentLine(fmt.Sprintf("  %-12s %s", "Scope", scopeInfo)))

	if variable.Secret {
		fmt.Fprintln(f.Writer, box.ContentLine(fmt.Sprintf("  %-12s %s", "Secret", "Yes")))
	}

	fmt.Fprintln(f.Writer, box.ContentLine(fmt.Sprintf("  %-12s %s (%s)", "Created", FormatTimestamp(variable.CreatedAt.Time), RelativeTime(variable.CreatedAt.Time))))
	if variable.UpdatedAt != nil && !variable.UpdatedAt.IsZero() {
		fmt.Fprintln(f.Writer, box.ContentLine(fmt.Sprintf("  %-12s %s (%s)", "Updated", FormatTimestamp(variable.UpdatedAt.Time), RelativeTime(variable.UpdatedAt.Time))))
	}

	fmt.Fprintln(f.Writer, box.BottomLine())
	return nil
}

// FormatVariableOverview formats a consolidated view of variables (detailed view)
func (f *DetailedFormatter) FormatVariableOverview(items []models.VariableOverview, total int) error {
	if len(items) == 0 {
		f.Info("No variables found")
		return nil
	}

	const width = 60

	for i, item := range items {
		if i > 0 {
			fmt.Fprintln(f.Writer)
		}

		box := NewBox(width)
		fmt.Fprintln(f.Writer, box.TopLineWithTitle(item.Name))

		for _, def := range item.Definitions {
			scopeInfo := def.Scope
			if def.ScopeName != nil {
				scopeInfo = fmt.Sprintf("%s:%s", def.Scope, *def.ScopeName)
			}

			value := def.Value
			if len(value) > 35 {
				value = value[:32] + "..."
			}

			secretIndicator := ""
			if def.Secret {
				secretIndicator = " [SECRET]"
			}

			line := fmt.Sprintf("  %-20s %s%s", scopeInfo, value, secretIndicator)
			fmt.Fprintln(f.Writer, box.ContentLine(line))

			if def.Description != "" {
				desc := def.Description
				if len(desc) > 45 {
					desc = desc[:42] + "..."
				}
				fmt.Fprintln(f.Writer, box.ContentLine(fmt.Sprintf("  %s%s", strings.Repeat(" ", 20), ColorDim.Sprint(desc))))
			}
		}

		fmt.Fprintln(f.Writer, box.BottomLine())
	}

	fmt.Fprintf(f.Writer, "\nTotal: %d variables\n", total)
	return nil
}

// FormatBackupConfig formats a backup configuration (detailed view)
func (f *DetailedFormatter) FormatBackupConfig(config *models.BackupConfig) error {
	const width = 55

	// Header box
	box := NewBox(width)
	fmt.Fprintln(f.Writer, box.TopLineWithTitle("Backup Configuration"))
	ColorHeader.Fprintf(f.Writer, "%s  %s", BoxVertical, config.Organization)
	padding := width - 5 - len(config.Organization)
	fmt.Fprintf(f.Writer, "%s %s\n", strings.Repeat(" ", padding), BoxVertical)
	fmt.Fprintln(f.Writer, box.BottomLine())
	fmt.Fprintln(f.Writer)

	// Status row
	fmt.Fprintf(f.Writer, "  %-14s %s\n", "Status", StatusWithIcon(config.Status))
	fmt.Fprintln(f.Writer)

	// S3 Configuration
	sectionBox := NewSharpBox(width)
	fmt.Fprintln(f.Writer, sectionBox.TopLineWithTitle("S3 Configuration"))
	fmt.Fprintln(f.Writer, sectionBox.ContentLine(fmt.Sprintf("  %-12s %s", "Endpoint", config.S3Endpoint)))
	fmt.Fprintln(f.Writer, sectionBox.ContentLine(fmt.Sprintf("  %-12s %s", "Bucket", config.S3Bucket)))
	if config.S3Prefix != nil && *config.S3Prefix != "" {
		fmt.Fprintln(f.Writer, sectionBox.ContentLine(fmt.Sprintf("  %-12s %s", "Folder", *config.S3Prefix)))
	}
	fmt.Fprintln(f.Writer, sectionBox.ContentLine(fmt.Sprintf("  %-12s %s", "Key ID", config.S3KeyID)))
	fmt.Fprintln(f.Writer, sectionBox.BottomLine())
	fmt.Fprintln(f.Writer)

	// Schedule and encryption
	fmt.Fprintf(f.Writer, "  %-14s %s\n", "Schedule", config.Schedule)
	fmt.Fprintf(f.Writer, "  %-14s %s\n", "Encryption Key", EncryptionKeyWithIcon(config.HasEncryptionKey))
	fmt.Fprintln(f.Writer)

	// Timestamps
	fmt.Fprintf(f.Writer, "  %-14s %s\n", "Created", FormatTimestamp(config.CreatedAt.Time))
	fmt.Fprintf(f.Writer, "  %-14s %s\n", "Updated", FormatTimestamp(config.UpdatedAt.Time))

	return nil
}

// FormatBackupConfigTest formats a backup config test result (detailed view)
func (f *DetailedFormatter) FormatBackupConfigTest(result *models.BackupConfigTestResponse) error {
	if result.Success {
		ColorSuccess.Fprintf(f.Writer, "✓ %s\n", result.Message)
	} else {
		ColorError.Fprintf(f.Writer, "✗ %s\n", result.Message)
	}
	return nil
}

// FormatDeviceBackupStatuses formats a list of device backup statuses (detailed view)
func (f *DetailedFormatter) FormatDeviceBackupStatuses(statuses []models.DeviceBackupStatus, total int, enabledCount int) error {
	if len(statuses) == 0 {
		f.Info("No devices found")
		return nil
	}

	const width = 55

	for i, s := range statuses {
		if i > 0 {
			fmt.Fprintln(f.Writer)
		}

		// Header box
		box := NewBox(width)
		fmt.Fprintln(f.Writer, box.TopLineWithTitle("Device Backup"))
		ColorHeader.Fprintf(f.Writer, "%s  %s", BoxVertical, s.DeviceName)
		padding := width - 5 - len(s.DeviceName)
		fmt.Fprintf(f.Writer, "%s %s\n", strings.Repeat(" ", padding), BoxVertical)
		fmt.Fprintln(f.Writer, box.BottomLine())
		fmt.Fprintln(f.Writer)

		// Status row
		fmt.Fprintf(f.Writer, "  %-14s %s\n", "Backup", EnabledDisabledWithIcon(s.Enabled))
		fmt.Fprintf(f.Writer, "  %-14s %s\n", "Key Override", KeyOverrideWithIcon(s.HasEncryptionKeyOverride))

		// Last backup
		if s.LastBackupAt != nil && !s.LastBackupAt.IsZero() {
			fmt.Fprintf(f.Writer, "  %-14s %s (%s)\n", "Last Backup", FormatTimestamp(s.LastBackupAt.Time), RelativeTime(s.LastBackupAt.Time))
			fmt.Fprintf(f.Writer, "  %-14s %s\n", "Status", BackupStatusWithIcon(s.LastBackupStatus))
			if s.LastBackupMessage != "" {
				fmt.Fprintf(f.Writer, "  %-14s %s\n", "Message", s.LastBackupMessage)
			}
		} else {
			fmt.Fprintf(f.Writer, "  %-14s %s\n", "Last Backup", "Never")
		}
	}

	fmt.Fprintf(f.Writer, "\nTotal: %d devices (%d with backup enabled)\n", total, enabledCount)
	return nil
}

// FormatDeviceBackupStatus formats a single device backup status (detailed view)
func (f *DetailedFormatter) FormatDeviceBackupStatus(status *models.DeviceBackupStatus) error {
	const width = 55

	// Header box
	box := NewBox(width)
	fmt.Fprintln(f.Writer, box.TopLineWithTitle("Device Backup Status"))
	ColorHeader.Fprintf(f.Writer, "%s  %s", BoxVertical, status.DeviceName)
	padding := width - 5 - len(status.DeviceName)
	fmt.Fprintf(f.Writer, "%s %s\n", strings.Repeat(" ", padding), BoxVertical)
	ColorDim.Fprintf(f.Writer, "%s  %s", BoxVertical, status.Organization)
	padding = width - 5 - len(status.Organization)
	fmt.Fprintf(f.Writer, "%s %s\n", strings.Repeat(" ", padding), BoxVertical)
	fmt.Fprintln(f.Writer, box.BottomLine())
	fmt.Fprintln(f.Writer)

	// Status row
	fmt.Fprintf(f.Writer, "  %-14s %s\n", "Backup", EnabledDisabledWithIcon(status.Enabled))
	fmt.Fprintf(f.Writer, "  %-14s %s\n", "Key Override", KeyOverrideWithIcon(status.HasEncryptionKeyOverride))
	fmt.Fprintln(f.Writer)

	// Last backup section
	if status.LastBackupAt != nil && !status.LastBackupAt.IsZero() {
		sectionBox := NewSharpBox(width)
		fmt.Fprintln(f.Writer, sectionBox.TopLineWithTitle("Last Backup"))
		fmt.Fprintln(f.Writer, sectionBox.ContentLine(fmt.Sprintf("  %-12s %s", "Time", FormatTimestamp(status.LastBackupAt.Time))))
		fmt.Fprintln(f.Writer, sectionBox.ContentLine(fmt.Sprintf("  %-12s %s", "Relative", RelativeTime(status.LastBackupAt.Time))))
		fmt.Fprintln(f.Writer, sectionBox.ContentLine(fmt.Sprintf("  %-12s %s", "Status", BackupStatusWithIcon(status.LastBackupStatus))))
		if status.LastBackupMessage != "" {
			fmt.Fprintln(f.Writer, sectionBox.ContentLine(fmt.Sprintf("  %-12s %s", "Message", status.LastBackupMessage)))
		}
		fmt.Fprintln(f.Writer, sectionBox.BottomLine())
	} else {
		fmt.Fprintf(f.Writer, "  %-14s %s\n", "Last Backup", "Never")
	}

	return nil
}

// FormatVpnNetworks formats a list of VPN networks with full details
func (f *DetailedFormatter) FormatVpnNetworks(networks []models.VpnNetwork, total int, quota *models.Quota) error {
	if len(networks) == 0 {
		f.Info("No VPN networks found")
		return nil
	}

	if quota != nil {
		if quota.Unlimited {
			fmt.Fprintf(f.Writer, "Quota: VPN networks (Unlimited)\n\n")
		} else {
			fmt.Fprintf(f.Writer, "Quota: %d/%d VPN networks (%d available)\n\n", quota.Used, quota.Limit, quota.Available)
		}
	}

	for i, n := range networks {
		if i > 0 {
			fmt.Fprintln(f.Writer)
		}
		autoHubs := "No"
		if n.AutoConnectHubs {
			autoHubs = "Yes"
		}
		ColorHeader.Fprintf(f.Writer, "--- %s ---\n", n.Name)
		f.printLabelValue("CIDR", n.OverlayCIDRv4)
		f.printLabelValue("Auto-Hubs", autoHubs)
		f.printLabelValue("Listen Port", fmt.Sprintf("%d", n.ListenPortDefault))
		f.printLabelValue("Members", fmt.Sprintf("%d", n.MemberCount))
		f.printLabelValue("Overrides", fmt.Sprintf("%d", n.LinkCount))
		f.printLabelValue("Created", FormatTimestamp(n.CreatedAt.Time))
	}
	return nil
}

// FormatOrgQuota formats organization quota (detailed view)
func (f *DetailedFormatter) FormatOrgQuota(quota *models.OrgQuota) error {
	const width = 55
	box := NewBox(width)
	fmt.Fprintln(f.Writer, box.TopLineWithTitle("Organization Quota"))
	titleLine := fmt.Sprintf("%s  %s", BoxVertical, quota.Organization)
	padding := width - 5 - len(quota.Organization)
	fmt.Fprintf(f.Writer, "%s%s %s\n", titleLine, strings.Repeat(" ", padding), BoxVertical)
	fmt.Fprintln(f.Writer, box.BottomLine())
	fmt.Fprintln(f.Writer)

	if quota.Plan != nil {
		f.printLabelValue("Plan", quota.Plan.DisplayName)
		if quota.Plan.PricePerDeviceCents > 0 {
			f.printLabelValue("Price/Device", fmt.Sprintf("$%.2f/mo", float64(quota.Plan.PricePerDeviceCents)/100))
		}
		fmt.Fprintln(f.Writer)
	}

	fmt.Fprintf(f.Writer, "  %-16s %-10s %-10s %s\n", "Resource", "Limit", "Used", "Available")
	fmt.Fprintf(f.Writer, "  %s\n", strings.Repeat("─", 50))
	f.printQuotaRow("Devices", quota.Devices)
	f.printQuotaRow("Users", quota.Users)
	f.printQuotaRow("VPN Networks", quota.VpnNetworks)
	f.printQuotaRow("Snippets", quota.Snippets)

	fmt.Fprintln(f.Writer)
	fmt.Fprintf(f.Writer, "  Features:\n")
	f.printLabelValue("  Backup", EnabledDisabledWithIcon(quota.BackupEnabled))
	f.printLabelValue("  Remote Admin", EnabledDisabledWithIcon(quota.RemoteAdminEnabled))
	return nil
}

func (f *DetailedFormatter) printQuotaRow(name string, q models.Quota) {
	limit := fmt.Sprintf("%d", q.Limit)
	available := fmt.Sprintf("%d", q.Available)
	if q.Unlimited {
		limit = "∞"
		available = "∞"
	}
	fmt.Fprintf(f.Writer, "  %-16s %-10s %-10d %s\n", name, limit, q.Used, available)
}

// FormatVpnNetwork formats a single VPN network with full details
func (f *DetailedFormatter) FormatVpnNetwork(network *models.VpnNetwork) error {
	const width = 55

	box := NewBox(width)
	fmt.Fprintln(f.Writer, box.TopLineWithTitle("VPN Network"))
	ColorHeader.Fprintf(f.Writer, "%s  %s", BoxVertical, network.Name)
	padding := width - 5 - len(network.Name)
	fmt.Fprintf(f.Writer, "%s %s\n", strings.Repeat(" ", padding), BoxVertical)
	ColorDim.Fprintf(f.Writer, "%s  CIDR: %s", BoxVertical, network.OverlayCIDRv4)
	padding = width - 11 - len(network.OverlayCIDRv4)
	fmt.Fprintf(f.Writer, "%s %s\n", strings.Repeat(" ", padding), BoxVertical)
	fmt.Fprintln(f.Writer, box.BottomLine())
	fmt.Fprintln(f.Writer)

	autoHubs := "No"
	if network.AutoConnectHubs {
		autoHubs = "Yes"
	}
	fmt.Fprintf(f.Writer, "  %-14s %s", "Auto-Hubs", autoHubs)
	fmt.Fprintf(f.Writer, "%s", strings.Repeat(" ", 10-len(autoHubs)))
	fmt.Fprintf(f.Writer, "%-14s %d\n", "Listen Port", network.ListenPortDefault)

	mtu := "-"
	if network.MTUDefault != nil {
		mtu = fmt.Sprintf("%d", *network.MTUDefault)
	}
	keepalive := "-"
	if network.KeepaliveDefault != nil {
		keepalive = fmt.Sprintf("%d", *network.KeepaliveDefault)
	}
	fmt.Fprintf(f.Writer, "  %-14s %s", "MTU", mtu)
	fmt.Fprintf(f.Writer, "%s", strings.Repeat(" ", 10-len(mtu)))
	fmt.Fprintf(f.Writer, "%-14s %s\n", "Keepalive", keepalive)

	if network.Organization != "" {
		fmt.Fprintf(f.Writer, "  %-14s %s\n", "Organization", network.Organization)
	}
	fmt.Fprintln(f.Writer)

	sectionBox := NewSharpBox(width)
	fmt.Fprintln(f.Writer, sectionBox.TopLineWithTitle("Statistics"))
	fmt.Fprintln(f.Writer, sectionBox.ContentLine(fmt.Sprintf("  Members: %d", network.MemberCount)))
	fmt.Fprintln(f.Writer, sectionBox.ContentLine(fmt.Sprintf("  Overrides: %d", network.LinkCount)))
	fmt.Fprintln(f.Writer, sectionBox.BottomLine())
	fmt.Fprintln(f.Writer)

	fmt.Fprintf(f.Writer, "  %-14s %s\n", "Created", FormatTimestamp(network.CreatedAt.Time))
	fmt.Fprintf(f.Writer, "  %-14s %s\n", "Updated", FormatTimestamp(network.UpdatedAt.Time))

	return nil
}

// FormatVpnMembers formats a list of VPN members with full details
func (f *DetailedFormatter) FormatVpnMembers(members []models.VpnMember, total int) error {
	if len(members) == 0 {
		f.Info("No VPN members found")
		return nil
	}

	for i, m := range members {
		if i > 0 {
			fmt.Fprintln(f.Writer)
		}
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
		ColorHeader.Fprintf(f.Writer, "--- %s ---\n", m.DeviceName)
		f.printLabelValue("Role", m.Role)
		f.printLabelValue("Enabled", enabled)
		f.printLabelValue("Overlay IP", m.OverlayIPv4)
		f.printLabelValue("Endpoint", endpoint)
		if m.TransitViaHub != nil && *m.TransitViaHub != "" {
			f.printLabelValue("Transit Hub", *m.TransitViaHub)
		}
		f.printLabelValue("Created", FormatTimestamp(m.CreatedAt.Time))
	}
	return nil
}

// FormatVpnMember formats a single VPN member with full details
func (f *DetailedFormatter) FormatVpnMember(member *models.VpnMember) error {
	const width = 55

	box := NewBox(width)
	fmt.Fprintln(f.Writer, box.TopLineWithTitle("VPN Member"))
	ColorHeader.Fprintf(f.Writer, "%s  %s", BoxVertical, member.DeviceName)
	padding := width - 5 - len(member.DeviceName)
	fmt.Fprintf(f.Writer, "%s %s\n", strings.Repeat(" ", padding), BoxVertical)
	ColorDim.Fprintf(f.Writer, "%s  %s / %s", BoxVertical, member.VpnNetwork, member.Role)
	infoStr := member.VpnNetwork + " / " + member.Role
	padding = width - 5 - len(infoStr)
	fmt.Fprintf(f.Writer, "%s %s\n", strings.Repeat(" ", padding), BoxVertical)
	fmt.Fprintln(f.Writer, box.BottomLine())
	fmt.Fprintln(f.Writer)

	enabled := "Yes"
	if !member.Enabled {
		enabled = "No"
	}
	fmt.Fprintf(f.Writer, "  %-14s %s", "Enabled", enabled)
	fmt.Fprintf(f.Writer, "%s", strings.Repeat(" ", 10-len(enabled)))
	fmt.Fprintf(f.Writer, "%-14s %s\n", "Overlay IP", member.OverlayIPv4)

	fmt.Fprintf(f.Writer, "  %-14s %s\n", "Public Key", member.WgPublicKey)

	if member.EndpointHost != nil && *member.EndpointHost != "" {
		fmt.Fprintf(f.Writer, "  %-14s %s", "Endpoint Host", *member.EndpointHost)
		if member.EndpointPort != nil {
			fmt.Fprintf(f.Writer, ":%d", *member.EndpointPort)
		}
		fmt.Fprintln(f.Writer)
	}
	if member.ListenPort != nil {
		fmt.Fprintf(f.Writer, "  %-14s %d\n", "Listen Port", *member.ListenPort)
	}
	if member.MTU != nil {
		fmt.Fprintf(f.Writer, "  %-14s %d\n", "MTU", *member.MTU)
	}
	if member.Keepalive != nil {
		fmt.Fprintf(f.Writer, "  %-14s %d\n", "Keepalive", *member.Keepalive)
	}
	if member.TransitViaHub != nil && *member.TransitViaHub != "" {
		fmt.Fprintf(f.Writer, "  %-14s %s\n", "Transit Hub", *member.TransitViaHub)
	}
	fmt.Fprintln(f.Writer)

	fmt.Fprintf(f.Writer, "  %-14s %s\n", "Created", FormatTimestamp(member.CreatedAt.Time))
	fmt.Fprintf(f.Writer, "  %-14s %s\n", "Updated", FormatTimestamp(member.UpdatedAt.Time))

	return nil
}

// FormatVpnLinks formats a list of VPN links with full details
func (f *DetailedFormatter) FormatVpnLinks(links []models.VpnLink, total int) error {
	if len(links) == 0 {
		f.Info("No VPN links found")
		return nil
	}

	for i, l := range links {
		if i > 0 {
			fmt.Fprintln(f.Writer)
		}
		enabled := "Yes"
		if !l.Enabled {
			enabled = "No"
		}
		psk := "No"
		if l.HasPSK {
			psk = "Yes"
		}
		ColorHeader.Fprintf(f.Writer, "--- %s <-> %s ---\n", l.DeviceAName, l.DeviceBName)
		f.printLabelValue("Enabled", enabled)
		f.printLabelValue("PSK", psk)
		f.printLabelValue("Created", FormatTimestamp(l.CreatedAt.Time))
	}
	return nil
}

// FormatVpnLink formats a single VPN link with full details
func (f *DetailedFormatter) FormatVpnLink(link *models.VpnLink) error {
	const width = 55

	box := NewBox(width)
	fmt.Fprintln(f.Writer, box.TopLineWithTitle("VPN Link"))
	title := link.DeviceAName + " <-> " + link.DeviceBName
	ColorHeader.Fprintf(f.Writer, "%s  %s", BoxVertical, title)
	padding := width - 5 - len(title)
	fmt.Fprintf(f.Writer, "%s %s\n", strings.Repeat(" ", padding), BoxVertical)
	ColorDim.Fprintf(f.Writer, "%s  %s", BoxVertical, link.VpnNetwork)
	padding = width - 5 - len(link.VpnNetwork)
	fmt.Fprintf(f.Writer, "%s %s\n", strings.Repeat(" ", padding), BoxVertical)
	fmt.Fprintln(f.Writer, box.BottomLine())
	fmt.Fprintln(f.Writer)

	enabled := "Yes"
	if !link.Enabled {
		enabled = "No"
	}
	psk := "No"
	if link.HasPSK {
		psk = "Yes"
	}
	fmt.Fprintf(f.Writer, "  %-14s %s", "Enabled", enabled)
	fmt.Fprintf(f.Writer, "%s", strings.Repeat(" ", 10-len(enabled)))
	fmt.Fprintf(f.Writer, "%-14s %s\n", "PSK", psk)
	fmt.Fprintln(f.Writer)

	fmt.Fprintf(f.Writer, "  %-14s %s\n", "Created", FormatTimestamp(link.CreatedAt.Time))
	fmt.Fprintf(f.Writer, "  %-14s %s\n", "Updated", FormatTimestamp(link.UpdatedAt.Time))

	return nil
}

// FormatVpnPrefixes formats a list of VPN member prefixes with full details
func (f *DetailedFormatter) FormatVpnPrefixes(prefixes []models.VpnMemberPrefix, total int) error {
	if len(prefixes) == 0 {
		f.Info("No VPN prefixes found")
		return nil
	}

	for i, p := range prefixes {
		if i > 0 {
			fmt.Fprintln(f.Writer)
		}
		publish := "Yes"
		if !p.Publish {
			publish = "No"
		}
		ColorHeader.Fprintf(f.Writer, "--- %s ---\n", p.VariableName)
		f.printLabelValue("Publish", publish)
		f.printLabelValue("Created", FormatTimestamp(p.CreatedAt.Time))
	}
	return nil
}

// FormatVpnPrefix formats a single VPN member prefix with full details
func (f *DetailedFormatter) FormatVpnPrefix(prefix *models.VpnMemberPrefix) error {
	const width = 55

	box := NewBox(width)
	fmt.Fprintln(f.Writer, box.TopLineWithTitle("VPN Prefix"))
	ColorHeader.Fprintf(f.Writer, "%s  %s", BoxVertical, prefix.VariableName)
	padding := width - 5 - len(prefix.VariableName)
	fmt.Fprintf(f.Writer, "%s %s\n", strings.Repeat(" ", padding), BoxVertical)
	info := prefix.DeviceName + " @ " + prefix.VpnNetwork
	ColorDim.Fprintf(f.Writer, "%s  %s", BoxVertical, info)
	padding = width - 5 - len(info)
	fmt.Fprintf(f.Writer, "%s %s\n", strings.Repeat(" ", padding), BoxVertical)
	fmt.Fprintln(f.Writer, box.BottomLine())
	fmt.Fprintln(f.Writer)

	publish := "Yes"
	if !prefix.Publish {
		publish = "No"
	}
	fmt.Fprintf(f.Writer, "  %-14s %s\n", "Publish", publish)
	fmt.Fprintln(f.Writer)
	fmt.Fprintf(f.Writer, "  %-14s %s\n", "Created", FormatTimestamp(prefix.CreatedAt.Time))
	fmt.Fprintf(f.Writer, "  %-14s %s\n", "Updated", FormatTimestamp(prefix.UpdatedAt.Time))

	return nil
}

// FormatVpnConnections formats a list of effective VPN connections with full details
func (f *DetailedFormatter) FormatVpnConnections(connections []models.EffectiveConnection, total int) error {
	if len(connections) == 0 {
		f.Info("No VPN connections found")
		return nil
	}

	for i, c := range connections {
		if i > 0 {
			fmt.Fprintln(f.Writer)
		}
		active := "Yes"
		if !c.Active {
			active = "No"
		}
		psk := "No"
		if c.HasPSK {
			psk = "Yes"
		}
		ColorHeader.Fprintf(f.Writer, "--- %s ↔ %s ---\n", c.DeviceA, c.DeviceB)
		f.printLabelValue("Pair", VpnPairType(c.PairType))
		f.printLabelValue("Active", active)
		f.printLabelValue("PSK", psk)
		note := VpnConnectionNote(&c)
		if note != "-" {
			f.printLabelValue("Notes", note)
		}
	}
	return nil
}

// FormatVpnConnection formats a single effective VPN connection with full details
func (f *DetailedFormatter) FormatVpnConnection(connection *models.EffectiveConnection) error {
	const width = 55

	box := NewBox(width)
	fmt.Fprintln(f.Writer, box.TopLineWithTitle("VPN Connection"))
	title := VpnConnectionLine(connection)
	ColorHeader.Fprintf(f.Writer, "%s  %s", BoxVertical, title)
	padding := width - 5 - visibleLength(title)
	if padding < 0 {
		padding = 0
	}
	fmt.Fprintf(f.Writer, "%s %s\n", strings.Repeat(" ", padding), BoxVertical)
	subtitle := connection.VpnNetwork
	ColorDim.Fprintf(f.Writer, "%s  %s", BoxVertical, subtitle)
	padding = width - 5 - len(subtitle)
	if padding < 0 {
		padding = 0
	}
	fmt.Fprintf(f.Writer, "%s %s\n", strings.Repeat(" ", padding), BoxVertical)
	fmt.Fprintln(f.Writer, box.BottomLine())
	fmt.Fprintln(f.Writer)

	typeVal := VpnTypeValue(connection.PairType, connection.Source)
	active := "Yes"
	if !connection.Active {
		active = "No"
	}
	psk := "No"
	if connection.HasPSK {
		psk = "Yes"
	}

	fmt.Fprintf(f.Writer, "  %-14s %s\n", "Type", typeVal)
	fmt.Fprintf(f.Writer, "  %-14s %s", "Active", active)
	fmt.Fprintf(f.Writer, "%s", strings.Repeat(" ", 10-len(active)))
	fmt.Fprintf(f.Writer, "%-14s %s\n", "PSK", psk)
	fmt.Fprintln(f.Writer)

	explanation := VpnConnectionExplanation(connection)
	for _, line := range strings.Split(explanation, "\n") {
		ColorDim.Fprintf(f.Writer, "  %s\n", line)
	}

	return nil
}
