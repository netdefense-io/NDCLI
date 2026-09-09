package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/netdefense-io/NDCLI/internal/helpers"
	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/output"
	"github.com/netdefense-io/NDCLI/internal/service"
)

// showThreadPerPage is the first page size `support show` / `respond show`
// pulls; anything beyond it is a pointer to the messages command rather
// than an unbounded fetch.
const showThreadPerPage = 100

var ticketCmd = &cobra.Command{
	Use:   "support",
	Short: "Support tickets (list, show, create, reply, close)",
	Long: `Open and work support tickets for your organization.

A ticket is a conversation with NetDefense Support. Messages are plain text;
files are attached by uploading them first and referencing the returned uuid,
which ` + "`--attach`" + ` does for you.

Replying to a closed ticket reopens it — every command prints the status the
server returned rather than the one it had before.`,
}

var ticketListCmd = &cobra.Command{
	Use:   "list",
	Short: "List the organization's tickets",
	RunE:  runTicketList,
}

var ticketShowCmd = &cobra.Command{
	Use:   "show [code]",
	Short: "Show a ticket and its messages",
	Args:  cobra.ExactArgs(1),
	RunE:  runTicketShow,
}

var ticketCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Open a new ticket",
	RunE:  runTicketCreate,
}

var ticketUpdateCmd = &cobra.Command{
	Use:   "update [code]",
	Short: "Change a ticket's subject, priority, category or devices",
	Args:  cobra.ExactArgs(1),
	RunE:  runTicketUpdate,
}

var ticketCloseCmd = &cobra.Command{
	Use:   "close [code]",
	Short: "Close a ticket",
	Args:  cobra.ExactArgs(1),
	RunE:  runTicketClose,
}

var ticketReopenCmd = &cobra.Command{
	Use:   "reopen [code]",
	Short: "Reopen a closed ticket",
	Args:  cobra.ExactArgs(1),
	RunE:  runTicketReopen,
}

var ticketMessagesCmd = &cobra.Command{
	Use:   "messages [code]",
	Short: "List a ticket's messages",
	Args:  cobra.ExactArgs(1),
	RunE:  runTicketMessages,
}

var ticketReplyCmd = &cobra.Command{
	Use:   "reply [code]",
	Short: "Post a reply visible to support",
	Long: `Post a reply on a ticket.

The reply is visible to NetDefense Support. Replying to a closed ticket
reopens it; the status printed is the one the server returned.`,
	Args: cobra.ExactArgs(1),
	RunE: runTicketReply,
}

var ticketNoteCmd = &cobra.Command{
	Use:   "note [code]",
	Short: "Post an internal note visible only to your organization",
	Args:  cobra.ExactArgs(1),
	RunE:  runTicketNote,
}

var ticketParticipantCmd = &cobra.Command{
	Use:   "participant",
	Short: "Manage ticket participants",
}

var ticketParticipantAddCmd = &cobra.Command{
	Use:   "add [code] [email]",
	Short: "Add an organization member to a ticket",
	Args:  cobra.ExactArgs(2),
	RunE:  runTicketParticipantAdd,
}

var ticketParticipantRemoveCmd = &cobra.Command{
	Use:   "remove [code] [email]",
	Short: "Remove a participant from a ticket",
	Args:  cobra.ExactArgs(2),
	RunE:  runTicketParticipantRemove,
}

var ticketAttachmentCmd = &cobra.Command{
	Use:   "attachment",
	Short: "Upload or delete ticket attachments",
}

var ticketAttachmentUploadCmd = &cobra.Command{
	Use:   "upload [path]",
	Short: "Upload a file and print its attachment uuid",
	Long: `Upload a file and print its attachment uuid.

The uuid is unbound until you pass it in the --attachment list of a create or
reply; unbound uploads are swept after 24 hours. Most of the time you want
--attach on create/reply/note instead, which does both steps.`,
	Args: cobra.ExactArgs(1),
	RunE: runTicketAttachmentUpload,
}

var ticketAttachmentDeleteCmd = &cobra.Command{
	Use:   "delete [uuid]",
	Short: "Delete an attachment that is not yet bound to a ticket",
	Args:  cobra.ExactArgs(1),
	RunE:  runTicketAttachmentDelete,
}

var ticketDownloadCmd = &cobra.Command{
	Use:   "download [code] [attachment-uuid]",
	Short: "Download a ticket attachment",
	Long: `Download an attachment from a ticket.

The file is written to --dest (or the attachment's filename in the working
directory), and its SHA-256 is verified against the ticket metadata before it
appears. An existing file is never replaced without --overwrite.`,
	Args: cobra.ExactArgs(2),
	RunE: runTicketDownload,
}

func init() {
	ticketCmd.AddCommand(ticketListCmd)
	ticketCmd.AddCommand(ticketShowCmd)
	ticketCmd.AddCommand(ticketCreateCmd)
	ticketCmd.AddCommand(ticketUpdateCmd)
	ticketCmd.AddCommand(ticketCloseCmd)
	ticketCmd.AddCommand(ticketReopenCmd)
	ticketCmd.AddCommand(ticketMessagesCmd)
	ticketCmd.AddCommand(ticketReplyCmd)
	ticketCmd.AddCommand(ticketNoteCmd)
	ticketCmd.AddCommand(ticketParticipantCmd)
	ticketCmd.AddCommand(ticketAttachmentCmd)
	ticketCmd.AddCommand(ticketDownloadCmd)

	ticketParticipantCmd.AddCommand(ticketParticipantAddCmd)
	ticketParticipantCmd.AddCommand(ticketParticipantRemoveCmd)
	ticketAttachmentCmd.AddCommand(ticketAttachmentUploadCmd)
	ticketAttachmentCmd.AddCommand(ticketAttachmentDeleteCmd)

	addTicketListFlags(ticketListCmd, false)

	ticketShowCmd.Flags().Bool("no-messages", false, "Print the ticket details only, without the message thread")

	ticketCreateCmd.Flags().String("subject", "", "Ticket subject (required)")
	addMessageFlags(ticketCreateCmd)
	ticketCreateCmd.Flags().String("priority", "", "Priority: LOW, NORMAL, HIGH, URGENT (default NORMAL)")
	ticketCreateCmd.Flags().String("category", "", "Category: BUG, QUESTION, FEATURE_REQUEST, BILLING, OTHER (default OTHER)")
	ticketCreateCmd.Flags().StringArray("device", nil, "Related device UUID (repeatable, max 20)")
	ticketCreateCmd.Flags().StringArray("participant", nil, "Email of an organization member to add as participant (repeatable)")
	ticketCreateCmd.Flags().StringArray("attach", nil, "Local file to upload and attach (repeatable, max 10)")
	ticketCreateCmd.Flags().StringArray("attachment", nil, "Already-uploaded attachment uuid (repeatable, max 10)")

	ticketUpdateCmd.Flags().String("subject", "", "New subject")
	ticketUpdateCmd.Flags().String("priority", "", "Priority: LOW, NORMAL, HIGH, URGENT")
	ticketUpdateCmd.Flags().String("category", "", "Category: BUG, QUESTION, FEATURE_REQUEST, BILLING, OTHER")
	ticketUpdateCmd.Flags().StringArray("device", nil, "Related device UUID; replaces the whole list (repeatable)")
	ticketUpdateCmd.Flags().Bool("clear-devices", false, "Remove every related device")

	addMessageFlags(ticketCloseCmd)
	addMessageFlags(ticketReopenCmd)

	addThreadFlags(ticketMessagesCmd)

	for _, c := range []*cobra.Command{ticketReplyCmd, ticketNoteCmd} {
		addMessageFlags(c)
		c.Flags().StringArray("attach", nil, "Local file to upload and attach (repeatable, max 10)")
		c.Flags().StringArray("attachment", nil, "Already-uploaded attachment uuid (repeatable, max 10)")
	}

	ticketParticipantRemoveCmd.Flags().Bool("force", false, "Skip the confirmation prompt")
	ticketAttachmentDeleteCmd.Flags().Bool("force", false, "Skip the confirmation prompt")

	for _, c := range []*cobra.Command{ticketListCmd, ticketCreateCmd, ticketUpdateCmd} {
		_ = c.RegisterFlagCompletionFunc("device", completeDeviceUUIDs)
	}

	ticketDownloadCmd.Flags().String("dest", "", "Destination file or directory (default: the attachment's filename here)")
	ticketDownloadCmd.Flags().Bool("overwrite", false, "Replace an existing file at the destination")
}

// addTicketListFlags installs the filters shared by `support list` and
// `respond list`; the responder side additionally filters by organization.
func addTicketListFlags(cmd *cobra.Command, support bool) {
	cmd.Flags().String("status", "", "Filter by status: OPEN, PENDING, CLOSED")
	cmd.Flags().String("priority", "", "Filter by priority: LOW, NORMAL, HIGH, URGENT")
	cmd.Flags().String("category", "", "Filter by category: BUG, QUESTION, FEATURE_REQUEST, BILLING, OTHER")
	if !support {
		cmd.Flags().String("participant", "", "Filter by participant: 'me' or an email address")
		cmd.Flags().String("device", "", "Filter by related device UUID")
	}
	cmd.Flags().StringP("q", "q", "", "Filter by subject substring (max 100 chars)")
	cmd.Flags().String("sort-by", "", "Sort as FIELD:asc|desc over last_activity_at, created_at, updated_at, priority, status, subject")
	cmd.Flags().Int("page", 1, "Page number")
	cmd.Flags().Int("per-page", 50, "Items per page (max 100)")
}

// addMessageFlags installs the message-body pair. Both commands that require
// a body and those where it is optional take the same two flags.
func addMessageFlags(cmd *cobra.Command) {
	cmd.Flags().String("message", "", "Message body")
	cmd.Flags().String("message-file", "", "Read the message body from a file, or '-' for stdin")
}

func addThreadFlags(cmd *cobra.Command) {
	cmd.Flags().String("order", "asc", "Chronological order: asc or desc")
	cmd.Flags().Int("page", 1, "Page number")
	cmd.Flags().Int("per-page", 50, "Items per page (max 100)")
}

// ticketListOptsFromFlags reads and validates the shared list filters.
func ticketListOptsFromFlags(cmd *cobra.Command, support bool) (service.TicketListOpts, error) {
	var opts service.TicketListOpts
	var err error

	status, _ := cmd.Flags().GetString("status")
	if opts.Status, err = service.ValidateTicketEnum("status", status, models.TicketStatuses); err != nil {
		return opts, err
	}
	priority, _ := cmd.Flags().GetString("priority")
	if opts.Priority, err = service.ValidateTicketEnum("priority", priority, models.TicketPriorities); err != nil {
		return opts, err
	}
	category, _ := cmd.Flags().GetString("category")
	if opts.Category, err = service.ValidateTicketEnum("category", category, models.TicketCategories); err != nil {
		return opts, err
	}
	if !support {
		opts.Participant, _ = cmd.Flags().GetString("participant")
		opts.Device, _ = cmd.Flags().GetString("device")
	}
	opts.Q, _ = cmd.Flags().GetString("q")
	sortBy, _ := cmd.Flags().GetString("sort-by")
	if opts.SortBy, err = service.ValidateTicketSort(sortBy); err != nil {
		return opts, err
	}
	opts.Page, _ = cmd.Flags().GetInt("page")
	opts.PerPage, _ = cmd.Flags().GetInt("per-page")
	return opts, nil
}

func threadOptsFromFlags(cmd *cobra.Command) service.TicketThreadOpts {
	opts := service.TicketThreadOpts{}
	opts.Order, _ = cmd.Flags().GetString("order")
	opts.Page, _ = cmd.Flags().GetInt("page")
	opts.PerPage, _ = cmd.Flags().GetInt("per-page")
	return opts
}

// messageFromFlags resolves the body from --message / --message-file.
func messageFromFlags(cmd *cobra.Command, required bool) (string, error) {
	message, _ := cmd.Flags().GetString("message")
	messageFile, _ := cmd.Flags().GetString("message-file")
	return helpers.ReadMessage(message, messageFile, required)
}

// attachmentsFromFlags resolves --attach (local files, uploaded now) and
// --attachment (uuids uploaded earlier) into one uuid list. Uploads happen
// first and are rolled back by the service if any of them fails, so a
// half-uploaded set never reaches a ticket.
// It returns the full list to post and, separately, the uuids this call
// created — the caller discards those if the write they were staged for
// fails, so a failed post leaves nothing bound to nothing.
func attachmentsFromFlags(ctx context.Context, cmd *cobra.Command, upload func(context.Context, []string) ([]string, error)) (all []string, uploaded []string, err error) {
	uuids, _ := cmd.Flags().GetStringArray("attachment")
	paths, _ := cmd.Flags().GetStringArray("attach")
	// Check the combined count first: the service caps each half on its
	// own, so an over-limit call would otherwise upload every file and then
	// abandon it at the post.
	if err := service.ValidateAttachmentCount(len(uuids), len(paths)); err != nil {
		return nil, nil, err
	}
	if len(paths) == 0 {
		return uuids, nil, nil
	}
	uploaded, err = upload(ctx, paths)
	if err != nil {
		return nil, nil, err
	}
	return append(append([]string{}, uuids...), uploaded...), uploaded, nil
}

func runTicketList(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	opts, err := ticketListOptsFromFlags(cmd, false)
	if err != nil {
		return err
	}
	result, err := svc.TicketList(context.Background(), org, opts)
	if err != nil {
		return err
	}
	if err := formatter.FormatTicketList(result.Tickets, result.Total, false); err != nil {
		return err
	}
	output.PrintPagination(result.Page, result.Total, result.PerPage)
	return nil
}

func runTicketShow(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()
	ctx := context.Background()

	ticket, err := svc.TicketGet(ctx, org, args[0])
	if err != nil {
		return err
	}
	noMessages, _ := cmd.Flags().GetBool("no-messages")
	if noMessages {
		return formatter.FormatTicketDetail(ticket, nil, 0)
	}
	thread, err := svc.TicketThread(ctx, org, args[0], service.TicketThreadOpts{Order: "asc", Page: 1, PerPage: showThreadPerPage})
	if err != nil {
		return err
	}
	return formatter.FormatTicketDetail(ticket, thread.Messages, thread.Total)
}

func runTicketCreate(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()
	ctx := context.Background()

	subject, _ := cmd.Flags().GetString("subject")
	if subject == "" {
		return fmt.Errorf("--subject is required")
	}
	message, err := messageFromFlags(cmd, true)
	if err != nil {
		return err
	}
	priority, err := service.ValidateTicketEnum("priority", mustString(cmd, "priority"), models.TicketPriorities)
	if err != nil {
		return err
	}
	category, err := service.ValidateTicketEnum("category", mustString(cmd, "category"), models.TicketCategories)
	if err != nil {
		return err
	}
	attachments, uploaded, err := attachmentsFromFlags(ctx, cmd, func(c context.Context, paths []string) ([]string, error) {
		return svc.TicketAttachmentUploadAll(c, org, paths)
	})
	if err != nil {
		return err
	}

	devices, _ := cmd.Flags().GetStringArray("device")
	participants, _ := cmd.Flags().GetStringArray("participant")

	ticket, err := svc.TicketCreate(ctx, org, service.TicketCreateInput{
		Subject:      subject,
		Message:      message,
		Priority:     priority,
		Category:     category,
		Devices:      devices,
		Participants: participants,
		Attachments:  attachments,
	})
	if err != nil {
		svc.TicketDiscardAttachments(ctx, org, uploaded)
		return err
	}
	// No success banner here: this command's output is the ticket itself,
	// and a decorated line ahead of the formatter would make -f json
	// unparseable.
	return formatter.FormatTicketDetail(ticket, nil, 0)
}

func runTicketUpdate(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	var in service.TicketUpdateInput
	if cmd.Flags().Changed("subject") {
		subject, _ := cmd.Flags().GetString("subject")
		in.Subject = &subject
	}
	var err error
	if in.Priority, err = service.ValidateTicketEnum("priority", mustString(cmd, "priority"), models.TicketPriorities); err != nil {
		return err
	}
	if in.Category, err = service.ValidateTicketEnum("category", mustString(cmd, "category"), models.TicketCategories); err != nil {
		return err
	}
	clear, _ := cmd.Flags().GetBool("clear-devices")
	devices, _ := cmd.Flags().GetStringArray("device")
	switch {
	case clear && len(devices) > 0:
		return fmt.Errorf("--clear-devices cannot be combined with --device")
	case clear:
		empty := []string{}
		in.Devices = &empty
	case cmd.Flags().Changed("device"):
		in.Devices = &devices
	}

	ticket, err := svc.TicketUpdate(context.Background(), org, args[0], in)
	if err != nil {
		return err
	}
	return formatter.FormatTicketDetail(ticket, nil, 0)
}

func runTicketClose(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()
	message, err := messageFromFlags(cmd, false)
	if err != nil {
		return err
	}
	ticket, err := svc.TicketClose(context.Background(), org, args[0], message)
	if err != nil {
		return err
	}
	return formatter.FormatTicketStatusChange(ticket)
}

func runTicketReopen(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()
	message, err := messageFromFlags(cmd, false)
	if err != nil {
		return err
	}
	ticket, err := svc.TicketReopen(context.Background(), org, args[0], message)
	if err != nil {
		return err
	}
	return formatter.FormatTicketStatusChange(ticket)
}

func runTicketMessages(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()
	result, err := svc.TicketThread(context.Background(), org, args[0], threadOptsFromFlags(cmd))
	if err != nil {
		return err
	}
	if err := formatter.FormatTicketThread(result.Messages, result.Total); err != nil {
		return err
	}
	output.PrintPagination(result.Page, result.Total, result.PerPage)
	return nil
}

func runTicketReply(cmd *cobra.Command, args []string) error {
	return ticketPostMessage(cmd, args[0], false)
}

func runTicketNote(cmd *cobra.Command, args []string) error {
	return ticketPostMessage(cmd, args[0], true)
}

func ticketPostMessage(cmd *cobra.Command, code string, internal bool) error {
	requireAuth()
	org := requireOrganization()
	ctx := context.Background()

	body, err := messageFromFlags(cmd, true)
	if err != nil {
		return err
	}
	attachments, uploaded, err := attachmentsFromFlags(ctx, cmd, func(c context.Context, paths []string) ([]string, error) {
		return svc.TicketAttachmentUploadAll(c, org, paths)
	})
	if err != nil {
		return err
	}

	post := svc.TicketReply
	if internal {
		post = svc.TicketNote
	}
	msg, err := post(ctx, org, code, body, attachments)
	if err != nil {
		svc.TicketDiscardAttachments(ctx, org, uploaded)
		return err
	}
	// The server decides the resulting status — a reply to a closed ticket
	// reopens it — so the formatter renders what came back, never the
	// status we saw before.
	return formatter.FormatTicketMessagePosted(code, msg)
}

func runTicketParticipantAdd(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()
	participants, err := svc.TicketParticipantAdd(context.Background(), org, args[0], args[1])
	if err != nil {
		return err
	}
	return formatter.FormatTicketParticipants(participants)
}

func runTicketParticipantRemove(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()
	force, _ := cmd.Flags().GetBool("force")
	ok, err := helpers.ConfirmOrForce(fmt.Sprintf("Remove %s from ticket %s?", args[1], args[0]), force)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(os.Stderr, "Cancelled")
		return nil
	}
	participants, err := svc.TicketParticipantRemove(context.Background(), org, args[0], args[1])
	if err != nil {
		return err
	}
	return formatter.FormatTicketParticipants(participants)
}

func runTicketAttachmentUpload(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()
	att, err := svc.TicketAttachmentUpload(context.Background(), org, args[0])
	if err != nil {
		return err
	}
	return formatter.FormatTicketAttachment(att)
}

func runTicketAttachmentDelete(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()
	force, _ := cmd.Flags().GetBool("force")
	ok, err := helpers.ConfirmOrForce(fmt.Sprintf("Delete attachment %s?", args[0]), force)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(os.Stderr, "Cancelled")
		return nil
	}
	if err := svc.TicketAttachmentDelete(context.Background(), org, args[0]); err != nil {
		return err
	}
	return formatter.FormatTicketAttachmentDeleted(args[0])
}

func runTicketDownload(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()
	dest, _ := cmd.Flags().GetString("dest")
	overwrite, _ := cmd.Flags().GetBool("overwrite")
	result, err := svc.TicketAttachmentDownload(context.Background(), org, args[0], args[1], dest, overwrite)
	if err != nil {
		return namedOverwriteFlag(err)
	}
	return formatter.FormatTicketDownload(result)
}

// namedOverwriteFlag restores the CLI's own wording on a
// destination-exists error. The service message is surface-neutral because
// MCP callers have no flags; here, naming the flag is the useful advice.
func namedOverwriteFlag(err error) error {
	var svcErr *service.Error
	if errors.As(err, &svcErr) && svcErr.Code == service.CodeDestinationExists {
		return fmt.Errorf("%s", strings.Replace(svcErr.Message,
			service.OverwriteHint, "pass --overwrite to replace it", 1))
	}
	return err
}

// mustString reads a string flag, ignoring the "flag not defined" error that
// cannot happen for the flags these commands declare.
func mustString(cmd *cobra.Command, name string) string {
	v, _ := cmd.Flags().GetString(name)
	return v
}
