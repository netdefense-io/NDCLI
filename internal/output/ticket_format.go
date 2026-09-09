package output

import (
	"fmt"
	"strings"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// Support-ticket rendering for all four formatters.
//
// Nothing here escapes or re-encodes text. Subjects, bodies, filenames and
// author names are plain text sanitized once at decode time
// (api.DecodeJSON → sanitize.Struct); a second pass here would only mangle
// legitimate characters — firewall configs and logs contain <, > and &.

const ticketSubjectWidth = 60

// ticketBodyWidth is the wrap width for message bodies in the human
// formats; wide enough for pasted config lines, narrow enough to read.
const ticketBodyWidth = 78

// ticketStatusDisplay colours a ticket status. PENDING means "waiting on
// the user", so it reads as an action item rather than a failure.
func ticketStatusDisplay(status string) string {
	switch status {
	case "OPEN":
		return ColorInProgress.Sprint(status)
	case "PENDING":
		return ColorPending.Sprint(status)
	case "CLOSED":
		return ColorDim.Sprint(status)
	default:
		return status
	}
}

// ticketPriorityDisplay colours a ticket priority.
func ticketPriorityDisplay(priority string) string {
	switch priority {
	case "URGENT":
		return ColorError.Sprint(priority)
	case "HIGH":
		return ColorWarning.Sprint(priority)
	case "LOW":
		return ColorDim.Sprint(priority)
	default:
		return priority
	}
}

// ticketCreatedBy renders the creating account, which is null once the
// account has been hard-deleted.
func ticketCreatedBy(t *models.Ticket) string {
	if t == nil || t.CreatedBy == nil {
		return "(deleted account)"
	}
	if t.CreatedBy.Name != "" {
		return t.CreatedBy.Name
	}
	return t.CreatedBy.Email
}

// ticketKindLabel names an interaction kind for a thread header.
func ticketKindLabel(kind string) string {
	if kind == models.TicketKindInternalNote {
		return "internal note"
	}
	return "response"
}

// ticketTime renders a timestamp, or "-" when unset.
func ticketTime(t *models.FlexibleTime) string {
	if t == nil || t.IsZero() {
		return "-"
	}
	return FormatTimestamp(t.Time)
}

// ticketAttachmentSize renders a byte count compactly.
func ticketAttachmentSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/float64(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// writeTicketThread renders a thread as indented, wrapped blocks. Internal
// notes are labelled and dimmed so an org-only note is never mistaken for
// something support can see (and vice versa on the support side).
func writeTicketThread(f BaseFormatter, messages []models.TicketInteraction, total int) {
	if len(messages) == 0 {
		fmt.Fprintln(f.Writer, "  (no messages)")
		return
	}
	for i := range messages {
		msg := &messages[i]
		if i > 0 {
			fmt.Fprintln(f.Writer)
		}
		header := fmt.Sprintf("#%d  %s  (%s)  %s",
			i+1,
			msg.Author.DisplayName(),
			ticketKindLabel(msg.Kind),
			FormatTimestamp(msg.CreatedAt.Time),
		)
		if msg.IsInternalNote() {
			ColorDim.Fprintln(f.Writer, "  "+header)
		} else {
			ColorHeader.Fprintln(f.Writer, "  "+header)
		}
		for _, line := range wrapMessageLines(msg.Body, ticketBodyWidth) {
			if msg.IsInternalNote() {
				ColorDim.Fprintf(f.Writer, "    %s\n", line)
			} else {
				fmt.Fprintf(f.Writer, "    %s\n", line)
			}
		}
		for _, att := range msg.Attachments {
			ColorDim.Fprintf(f.Writer, "    ↳ %s  %s  %s  %s\n",
				att.UUID, att.Filename, ticketAttachmentSize(att.SizeBytes), att.ContentType)
		}
	}
	if total > len(messages) {
		fmt.Fprintln(f.Writer)
		ColorDim.Fprintf(f.Writer, "  Showing %d of %d messages — use the 'messages' command with --page to read the rest.\n", len(messages), total)
	}
}

// --- table ---

// FormatTicketList renders tickets as a table. showOrg prepends the owning
// organization column, which only the support surface populates.
func (f *TableFormatter) FormatTicketList(tickets []models.Ticket, total int, showOrg bool) error {
	if len(tickets) == 0 {
		f.Info("No tickets found")
		return nil
	}
	headers := []string{"CODE", "SUBJECT", "STATUS", "PRIORITY", "CATEGORY", "CREATED BY", "LAST ACTIVITY"}
	if showOrg {
		headers = append([]string{"ORG"}, headers...)
	}
	table := NewStyledTable(headers)
	for i := range tickets {
		t := &tickets[i]
		row := []string{
			t.Code,
			truncate(t.Subject, ticketSubjectWidth),
			ticketStatusDisplay(t.Status),
			ticketPriorityDisplay(t.Priority),
			t.Category,
			ticketCreatedBy(t),
			RelativeTimeShort(t.LastActivityAt.Time),
		}
		if showOrg {
			row = append([]string{t.OrgName()}, row...)
		}
		table.Append(row)
	}
	table.Render()
	fmt.Fprintf(f.Writer, "\nTotal: %d ticket(s)\n", total)
	return nil
}

// FormatTicketDetail renders one ticket followed by its thread.
func (f *TableFormatter) FormatTicketDetail(ticket *models.Ticket, messages []models.TicketInteraction, messagesTotal int) error {
	if ticket == nil {
		f.Info("No ticket found")
		return nil
	}
	fmt.Fprintln(f.Writer)
	ColorHeader.Fprintf(f.Writer, "%s  %s\n", ticket.Code, ticket.Subject)
	fmt.Fprintln(f.Writer)
	writeTicketFields(f.BaseFormatter, ticket, "  ")
	if messages != nil || messagesTotal > 0 {
		fmt.Fprintln(f.Writer)
		ColorHeader.Fprintln(f.Writer, "Messages")
		fmt.Fprintln(f.Writer)
		writeTicketThread(f.BaseFormatter, messages, messagesTotal)
	}
	return nil
}

// writeTicketFields prints the ticket's own fields, shared by the table and
// simple renderers.
func writeTicketFields(f BaseFormatter, t *models.Ticket, indent string) {
	line := func(label, value string) {
		fmt.Fprintf(f.Writer, "%s%-16s %s\n", indent, label+":", value)
	}
	if org := t.OrgName(); org != "" {
		line("Organization", org)
	}
	line("Status", ticketStatusDisplay(t.Status))
	line("Priority", ticketPriorityDisplay(t.Priority))
	line("Category", t.Category)
	line("Created by", ticketCreatedBy(t))
	line("Created", FormatTimestamp(t.CreatedAt.Time))
	line("Last activity", FormatTimestamp(t.LastActivityAt.Time))
	if t.ClosedAt != nil && !t.ClosedAt.IsZero() {
		line("Closed", ticketTime(t.ClosedAt))
	}
	if t.WebURL != "" {
		line("Web", t.WebURL)
	}
	if len(t.Participants) > 0 {
		names := make([]string, 0, len(t.Participants))
		for _, p := range t.Participants {
			label := p.Email
			if p.IsCreator {
				label += " (creator)"
			}
			if !p.Enabled {
				label += " (disabled)"
			}
			names = append(names, label)
		}
		line("Participants", strings.Join(names, ", "))
	} else if t.ParticipantCount > 0 {
		line("Participants", fmt.Sprintf("%d", t.ParticipantCount))
	}
	if len(t.Devices) > 0 {
		devices := make([]string, 0, len(t.Devices))
		for _, d := range t.Devices {
			devices = append(devices, fmt.Sprintf("%s (%s)", d.Name, d.UUID))
		}
		line("Devices", strings.Join(devices, ", "))
	} else if t.DeviceCount > 0 {
		line("Devices", fmt.Sprintf("%d", t.DeviceCount))
	}
}

// FormatTicketThread renders a page of messages on their own.
func (f *TableFormatter) FormatTicketThread(messages []models.TicketInteraction, total int) error {
	fmt.Fprintln(f.Writer)
	writeTicketThread(f.BaseFormatter, messages, total)
	fmt.Fprintf(f.Writer, "\nTotal: %d message(s)\n", total)
	return nil
}

// FormatTicketAttachment renders one uploaded attachment.
func (f *TableFormatter) FormatTicketAttachment(att *models.TicketAttachment) error {
	if att == nil {
		return nil
	}
	ColorSuccess.Fprintln(f.Writer, "✓ Attachment uploaded")
	fmt.Fprintf(f.Writer, "  UUID:     %s\n", att.UUID)
	fmt.Fprintf(f.Writer, "  Filename: %s\n", att.Filename)
	fmt.Fprintf(f.Writer, "  Type:     %s\n", att.ContentType)
	fmt.Fprintf(f.Writer, "  Size:     %s\n", ticketAttachmentSize(att.SizeBytes))
	fmt.Fprintf(f.Writer, "  SHA-256:  %s\n", att.SHA256)
	if att.TicketCode == "" {
		ColorDim.Fprintln(f.Writer, "  Unbound — reference this uuid in a ticket create or reply within 24 hours.")
	}
	return nil
}

// FormatTicketDownload reports where a downloaded attachment landed.
func (f *TableFormatter) FormatTicketDownload(result *models.TicketDownloadResult) error {
	if result == nil {
		return nil
	}
	ColorSuccess.Fprintf(f.Writer, "✓ ")
	fmt.Fprintf(f.Writer, "Downloaded %s (%s) to %s\n", result.Filename, ticketAttachmentSize(result.Size), result.Path)
	ColorDim.Fprintf(f.Writer, "  SHA-256 verified: %s\n", result.SHA256)
	return nil
}

// FormatTicketParticipants renders the participant list after a change.
func (f *TableFormatter) FormatTicketParticipants(participants []models.TicketParticipant) error {
	if len(participants) == 0 {
		f.Info("No participants")
		return nil
	}
	table := NewStyledTable([]string{"EMAIL", "NAME", "CREATOR", "ENABLED", "ADDED"})
	for _, p := range participants {
		table.Append([]string{
			p.Email,
			p.Name,
			boolLabel(p.IsCreator),
			boolLabel(p.Enabled),
			FormatTimestamp(p.AddedAt.Time),
		})
	}
	table.Render()
	fmt.Fprintf(f.Writer, "\nTotal: %d participant(s)\n", len(participants))
	return nil
}

func boolLabel(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// --- simple ---

// FormatTicketList renders tickets as one line each.
func (f *SimpleFormatter) FormatTicketList(tickets []models.Ticket, total int, showOrg bool) error {
	if len(tickets) == 0 {
		f.Info("No tickets found")
		return nil
	}
	for i := range tickets {
		t := &tickets[i]
		prefix := ""
		if showOrg {
			prefix = t.OrgName() + " "
		}
		fmt.Fprintf(f.Writer, "• %s%s [%s/%s/%s] %s — %s\n",
			prefix, t.Code, t.Status, t.Priority, t.Category,
			truncate(t.Subject, ticketSubjectWidth), RelativeTimeShort(t.LastActivityAt.Time))
	}
	fmt.Fprintf(f.Writer, "Total: %d\n", total)
	return nil
}

// FormatTicketDetail renders one ticket and its thread compactly.
func (f *SimpleFormatter) FormatTicketDetail(ticket *models.Ticket, messages []models.TicketInteraction, messagesTotal int) error {
	if ticket == nil {
		f.Info("No ticket found")
		return nil
	}
	fmt.Fprintf(f.Writer, "%s  %s\n", ticket.Code, ticket.Subject)
	writeTicketFields(f.BaseFormatter, ticket, "  ")
	if messages != nil || messagesTotal > 0 {
		fmt.Fprintln(f.Writer)
		writeTicketThread(f.BaseFormatter, messages, messagesTotal)
	}
	return nil
}

// FormatTicketThread renders a page of messages.
func (f *SimpleFormatter) FormatTicketThread(messages []models.TicketInteraction, total int) error {
	writeTicketThread(f.BaseFormatter, messages, total)
	fmt.Fprintf(f.Writer, "Total: %d\n", total)
	return nil
}

// FormatTicketAttachment renders one uploaded attachment.
func (f *SimpleFormatter) FormatTicketAttachment(att *models.TicketAttachment) error {
	if att == nil {
		return nil
	}
	fmt.Fprintf(f.Writer, "%s %s %s %s\n", att.UUID, att.Filename, ticketAttachmentSize(att.SizeBytes), att.ContentType)
	return nil
}

// FormatTicketDownload reports where a downloaded attachment landed.
func (f *SimpleFormatter) FormatTicketDownload(result *models.TicketDownloadResult) error {
	if result == nil {
		return nil
	}
	fmt.Fprintf(f.Writer, "%s (%s, sha256 %s)\n", result.Path, ticketAttachmentSize(result.Size), result.SHA256)
	return nil
}

// FormatTicketParticipants renders participants as one line each.
func (f *SimpleFormatter) FormatTicketParticipants(participants []models.TicketParticipant) error {
	if len(participants) == 0 {
		f.Info("No participants")
		return nil
	}
	for _, p := range participants {
		suffix := ""
		if p.IsCreator {
			suffix = " (creator)"
		}
		if !p.Enabled {
			suffix += " (disabled)"
		}
		fmt.Fprintf(f.Writer, "• %s%s\n", p.Email, suffix)
	}
	return nil
}

// --- detailed ---

// FormatTicketList renders tickets as boxed entries.
func (f *DetailedFormatter) FormatTicketList(tickets []models.Ticket, total int, showOrg bool) error {
	if len(tickets) == 0 {
		f.Info("No tickets found")
		return nil
	}
	for i := range tickets {
		t := &tickets[i]
		if i > 0 {
			fmt.Fprintln(f.Writer)
		}
		box := &fieldBox{title: t.Code}
		box.field("Subject", t.Subject)
		if showOrg {
			box.field("Organization", t.OrgName())
		}
		box.field("Status", ticketStatusDisplay(t.Status))
		box.field("Priority", ticketPriorityDisplay(t.Priority))
		box.field("Category", t.Category)
		box.field("Created By", ticketCreatedBy(t))
		box.field("Last Activity", RelativeTime(t.LastActivityAt.Time))
		box.render(f.BaseFormatter)
	}
	fmt.Fprintf(f.Writer, "\nTotal: %d ticket(s)\n", total)
	return nil
}

// FormatTicketDetail renders one ticket with its thread.
func (f *DetailedFormatter) FormatTicketDetail(ticket *models.Ticket, messages []models.TicketInteraction, messagesTotal int) error {
	if ticket == nil {
		f.Info("No ticket found")
		return nil
	}
	box := &fieldBox{title: "Ticket " + ticket.Code}
	box.field("Subject", ticket.Subject)
	if org := ticket.OrgName(); org != "" {
		box.field("Organization", org)
	}
	box.field("Status", ticketStatusDisplay(ticket.Status))
	box.field("Priority", ticketPriorityDisplay(ticket.Priority))
	box.field("Category", ticket.Category)
	box.field("Created By", ticketCreatedBy(ticket))
	box.field("Created", FormatTimestamp(ticket.CreatedAt.Time))
	box.field("Last Activity", FormatTimestamp(ticket.LastActivityAt.Time))
	if ticket.ClosedAt != nil && !ticket.ClosedAt.IsZero() {
		box.field("Closed", ticketTime(ticket.ClosedAt))
	}
	if ticket.WebURL != "" {
		box.field("Web", ticket.WebURL)
	}
	for _, p := range ticket.Participants {
		label := p.Email
		if p.IsCreator {
			label += " (creator)"
		}
		if !p.Enabled {
			label += " (disabled)"
		}
		box.field("Participant", label)
	}
	for _, d := range ticket.Devices {
		box.field("Device", fmt.Sprintf("%s (%s)", d.Name, d.UUID))
	}
	box.render(f.BaseFormatter)
	if messages != nil || messagesTotal > 0 {
		fmt.Fprintln(f.Writer)
		ColorHeader.Fprintln(f.Writer, "Messages")
		fmt.Fprintln(f.Writer)
		writeTicketThread(f.BaseFormatter, messages, messagesTotal)
	}
	return nil
}

// FormatTicketThread renders a page of messages.
func (f *DetailedFormatter) FormatTicketThread(messages []models.TicketInteraction, total int) error {
	fmt.Fprintln(f.Writer)
	writeTicketThread(f.BaseFormatter, messages, total)
	fmt.Fprintf(f.Writer, "\nTotal: %d message(s)\n", total)
	return nil
}

// FormatTicketAttachment renders one uploaded attachment.
func (f *DetailedFormatter) FormatTicketAttachment(att *models.TicketAttachment) error {
	if att == nil {
		return nil
	}
	box := &fieldBox{title: "Attachment"}
	box.field("UUID", att.UUID)
	box.field("Filename", att.Filename)
	box.field("Content Type", att.ContentType)
	box.field("Size", ticketAttachmentSize(att.SizeBytes))
	box.field("SHA-256", att.SHA256)
	if att.TicketCode != "" {
		box.field("Ticket", att.TicketCode)
	} else {
		box.field("Expires", ticketTime(att.ExpiresAt))
	}
	box.render(f.BaseFormatter)
	return nil
}

// FormatTicketDownload reports where a downloaded attachment landed.
func (f *DetailedFormatter) FormatTicketDownload(result *models.TicketDownloadResult) error {
	if result == nil {
		return nil
	}
	ColorSuccess.Fprintln(f.Writer, "✓ Attachment downloaded")
	f.printLabelValue("Path", result.Path)
	f.printLabelValue("Filename", result.Filename)
	f.printLabelValue("Size", ticketAttachmentSize(result.Size))
	f.printLabelValue("SHA-256", result.SHA256)
	return nil
}

// FormatTicketParticipants renders participants with full detail.
func (f *DetailedFormatter) FormatTicketParticipants(participants []models.TicketParticipant) error {
	if len(participants) == 0 {
		f.Info("No participants")
		return nil
	}
	for _, p := range participants {
		f.printLabelValue("Email", p.Email)
		f.printLabelValue("Name", p.Name)
		f.printLabelValue("Creator", boolLabel(p.IsCreator))
		f.printLabelValue("Enabled", boolLabel(p.Enabled))
		f.printLabelValue("Added", FormatTimestamp(p.AddedAt.Time))
		fmt.Fprintln(f.Writer)
	}
	return nil
}

// --- json ---

// FormatTicketList renders a ticket page as JSON.
func (f *JSONFormatter) FormatTicketList(tickets []models.Ticket, total int, showOrg bool) error {
	if tickets == nil {
		tickets = []models.Ticket{}
	}
	return f.output(map[string]interface{}{
		"tickets": tickets,
		"total":   total,
	})
}

// FormatTicketDetail renders the ticket and its thread as JSON.
func (f *JSONFormatter) FormatTicketDetail(ticket *models.Ticket, messages []models.TicketInteraction, messagesTotal int) error {
	if messages == nil {
		messages = []models.TicketInteraction{}
	}
	return f.output(map[string]interface{}{
		"ticket":         ticket,
		"messages":       messages,
		"messages_total": messagesTotal,
	})
}

// FormatTicketThread renders a message page as JSON.
func (f *JSONFormatter) FormatTicketThread(messages []models.TicketInteraction, total int) error {
	if messages == nil {
		messages = []models.TicketInteraction{}
	}
	return f.output(map[string]interface{}{
		"messages": messages,
		"total":    total,
	})
}

// FormatTicketAttachment renders an attachment as JSON.
func (f *JSONFormatter) FormatTicketAttachment(att *models.TicketAttachment) error {
	return f.output(att)
}

// FormatTicketDownload renders a download result as JSON.
func (f *JSONFormatter) FormatTicketDownload(result *models.TicketDownloadResult) error {
	return f.output(result)
}

// FormatTicketParticipants renders the participant list as JSON.
func (f *JSONFormatter) FormatTicketParticipants(participants []models.TicketParticipant) error {
	if participants == nil {
		participants = []models.TicketParticipant{}
	}
	return f.output(map[string]interface{}{
		"participants": participants,
		"count":        len(participants),
	})
}

// --- write confirmations ---
//
// A ticket command that only changes state still has to produce output in
// every format: a bare success line printed straight to stdout leaves
// `-f json` with nothing to parse. These follow the FormatTokenRevoked
// precedent — a short sentence in the human formats, a structured object in
// JSON.

// ticketStatusSentence describes the outcome of a close or reopen.
func ticketStatusSentence(ticket *models.Ticket) string {
	return fmt.Sprintf("Ticket %s is now %s", ticket.Code, ticket.Status)
}

// ticketPostedSentence describes a posted message, including the status the
// server returned — a reply to a closed ticket reopens it.
func ticketPostedSentence(code string, msg *models.TicketInteraction) string {
	label := "Reply"
	if msg.IsInternalNote() {
		label = "Internal note"
	}
	if msg.TicketStatus == "" {
		return fmt.Sprintf("%s posted on ticket %s", label, code)
	}
	return fmt.Sprintf("%s posted on ticket %s; it is now %s", label, code, msg.TicketStatus)
}

// FormatTicketStatusChange confirms a close or reopen.
func (f *TableFormatter) FormatTicketStatusChange(ticket *models.Ticket) error {
	ColorSuccess.Fprintf(f.Writer, "✓ ")
	fmt.Fprintf(f.Writer, "%s\n", ticketStatusSentence(ticket))
	return nil
}

// FormatTicketMessagePosted confirms a reply or note.
func (f *TableFormatter) FormatTicketMessagePosted(code string, msg *models.TicketInteraction) error {
	ColorSuccess.Fprintf(f.Writer, "✓ ")
	fmt.Fprintf(f.Writer, "%s\n", ticketPostedSentence(code, msg))
	return nil
}

// FormatTicketAttachmentDeleted confirms an attachment deletion.
func (f *TableFormatter) FormatTicketAttachmentDeleted(uuid string) error {
	ColorSuccess.Fprintf(f.Writer, "✓ ")
	fmt.Fprintf(f.Writer, "Attachment %s deleted\n", uuid)
	return nil
}

// FormatTicketStatusChange confirms a close or reopen.
func (f *SimpleFormatter) FormatTicketStatusChange(ticket *models.Ticket) error {
	fmt.Fprintln(f.Writer, ticketStatusSentence(ticket))
	return nil
}

// FormatTicketMessagePosted confirms a reply or note.
func (f *SimpleFormatter) FormatTicketMessagePosted(code string, msg *models.TicketInteraction) error {
	fmt.Fprintln(f.Writer, ticketPostedSentence(code, msg))
	return nil
}

// FormatTicketAttachmentDeleted confirms an attachment deletion.
func (f *SimpleFormatter) FormatTicketAttachmentDeleted(uuid string) error {
	fmt.Fprintf(f.Writer, "Attachment %s deleted\n", uuid)
	return nil
}

// FormatTicketStatusChange confirms a close or reopen.
func (f *DetailedFormatter) FormatTicketStatusChange(ticket *models.Ticket) error {
	ColorSuccess.Fprintf(f.Writer, "✓ ")
	fmt.Fprintf(f.Writer, "%s\n", ticketStatusSentence(ticket))
	f.printLabelValue("Closed", ticketTime(ticket.ClosedAt))
	f.printLabelValue("Last Activity", FormatTimestamp(ticket.LastActivityAt.Time))
	return nil
}

// FormatTicketMessagePosted confirms a reply or note.
func (f *DetailedFormatter) FormatTicketMessagePosted(code string, msg *models.TicketInteraction) error {
	ColorSuccess.Fprintf(f.Writer, "✓ ")
	fmt.Fprintf(f.Writer, "%s\n", ticketPostedSentence(code, msg))
	f.printLabelValue("Message", msg.UUID)
	for _, att := range msg.Attachments {
		f.printLabelValue("Attachment", fmt.Sprintf("%s  %s", att.UUID, att.Filename))
	}
	return nil
}

// FormatTicketAttachmentDeleted confirms an attachment deletion.
func (f *DetailedFormatter) FormatTicketAttachmentDeleted(uuid string) error {
	ColorSuccess.Fprintf(f.Writer, "✓ ")
	fmt.Fprintf(f.Writer, "Attachment %s deleted\n", uuid)
	return nil
}

// FormatTicketStatusChange renders the ticket after a close or reopen.
func (f *JSONFormatter) FormatTicketStatusChange(ticket *models.Ticket) error {
	return f.output(ticket)
}

// FormatTicketMessagePosted renders the new message and the status the
// server returned.
func (f *JSONFormatter) FormatTicketMessagePosted(code string, msg *models.TicketInteraction) error {
	return f.output(map[string]interface{}{
		"ticket_code":   code,
		"interaction":   msg,
		"ticket_status": msg.TicketStatus,
	})
}

// FormatTicketAttachmentDeleted renders the deletion as JSON.
func (f *JSONFormatter) FormatTicketAttachmentDeleted(uuid string) error {
	return f.output(map[string]interface{}{
		"uuid":    uuid,
		"deleted": true,
	})
}

// --- support responder profile ---

// supportProfileWriteHint explains a read-only responder session in the one
// place every renderer shares.
func supportProfileWriteHint(profile *models.SupportResponderProfile) string {
	if profile.CanWrite {
		return "can reply, close and reopen tickets"
	}
	return "read-only: this principal cannot reply, close or reopen"
}

// FormatSupportProfile renders GET /support/me as a table.
func (f *TableFormatter) FormatSupportProfile(profile *models.SupportResponderProfile) error {
	if profile == nil {
		return nil
	}
	fmt.Fprintln(f.Writer)
	ColorHeader.Fprintf(f.Writer, "%s\n", profile.DisplayName)
	fmt.Fprintf(f.Writer, "  %-12s %s\n", "Email:", profile.Email)
	fmt.Fprintf(f.Writer, "  %-12s %s\n", "Status:", ColoredStatus(profile.Status))
	fmt.Fprintf(f.Writer, "  %-12s %s\n", "Principal:", profile.Principal)
	fmt.Fprintf(f.Writer, "  %-12s %s\n", "Access:", supportProfileWriteHint(profile))
	return nil
}

// FormatSupportProfile renders GET /support/me compactly.
func (f *SimpleFormatter) FormatSupportProfile(profile *models.SupportResponderProfile) error {
	if profile == nil {
		return nil
	}
	fmt.Fprintf(f.Writer, "%s <%s> [%s/%s] %s\n",
		profile.DisplayName, profile.Email, profile.Status, profile.Principal,
		supportProfileWriteHint(profile))
	return nil
}

// FormatSupportProfile renders GET /support/me with full detail.
func (f *DetailedFormatter) FormatSupportProfile(profile *models.SupportResponderProfile) error {
	if profile == nil {
		return nil
	}
	box := &fieldBox{title: "Support Responder"}
	box.field("Display Name", profile.DisplayName)
	box.field("Email", profile.Email)
	box.field("Status", ColoredStatus(profile.Status))
	box.field("Principal", profile.Principal)
	box.field("Access", supportProfileWriteHint(profile))
	box.render(f.BaseFormatter)
	return nil
}

// FormatSupportProfile renders GET /support/me as JSON.
func (f *JSONFormatter) FormatSupportProfile(profile *models.SupportResponderProfile) error {
	return f.output(profile)
}
