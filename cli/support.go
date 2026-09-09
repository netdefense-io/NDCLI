package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/netdefense-io/NDCLI/internal/helpers"
	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/output"
	"github.com/netdefense-io/NDCLI/internal/service"
)

// The responder console: the responder's side of the same tickets `ndcli
// support` works from the organization's side.
//
// Nothing here calls requireOrganization(). The support surface is cross-org
// by design — codes are global and a responder may read any ticket — so an
// org is neither needed nor meaningful; `respond list --org NAME` is a
// filter, not a scope.

var supportCmd = &cobra.Command{
	Use:   "respond",
	Short: "Responder console for support tickets",
	Long: `Work support tickets as a NetDefense responder.

These commands require an enabled support responder: your login email, or the
email of the account behind your token, must match a responder record. Reads
work with any responder; replying, closing and reopening need a login or a
read-write token.

The surface is cross-organization, so no --org is required. On 'respond list'
--org is a filter over an exact organization name.`,
}

var supportMeCmd = &cobra.Command{
	Use:   "me",
	Short: "Show your support responder profile",
	RunE:  runSupportMe,
}

var supportListCmd = &cobra.Command{
	Use:   "list",
	Short: "List tickets across every organization",
	RunE:  runSupportList,
}

var supportShowCmd = &cobra.Command{
	Use:   "show [code]",
	Short: "Show a ticket and its messages",
	Args:  cobra.ExactArgs(1),
	RunE:  runSupportShow,
}

var supportSetCmd = &cobra.Command{
	Use:   "set [code]",
	Short: "Set a ticket's priority and/or category",
	Long: `Set a ticket's priority and/or category.

Support owns triage, not the customer's words: the subject, related devices
and participants stay organization-owned and cannot be changed here.`,
	Args: cobra.ExactArgs(1),
	RunE: runSupportSet,
}

var supportCloseCmd = &cobra.Command{
	Use:   "close [code]",
	Short: "Close a ticket",
	Args:  cobra.ExactArgs(1),
	RunE:  runSupportClose,
}

var supportReopenCmd = &cobra.Command{
	Use:   "reopen [code]",
	Short: "Reopen a closed ticket",
	Args:  cobra.ExactArgs(1),
	RunE:  runSupportReopen,
}

var supportMessagesCmd = &cobra.Command{
	Use:   "messages [code]",
	Short: "List a ticket's messages",
	Args:  cobra.ExactArgs(1),
	RunE:  runSupportMessages,
}

var supportReplyCmd = &cobra.Command{
	Use:   "reply [code]",
	Short: "Post a reply to the organization",
	Long: `Post a reply visible to the organization.

A support reply moves the ticket to PENDING (waiting on the user) and emails
every participant.`,
	Args: cobra.ExactArgs(1),
	RunE: runSupportReply,
}

var supportNoteCmd = &cobra.Command{
	Use:   "note [code]",
	Short: "Post an internal note visible only to support",
	Args:  cobra.ExactArgs(1),
	RunE:  runSupportNote,
}

var supportAttachmentCmd = &cobra.Command{
	Use:   "attachment",
	Short: "Upload or delete support attachments",
}

var supportAttachmentUploadCmd = &cobra.Command{
	Use:   "upload [code] [path]",
	Short: "Upload a file into a ticket's scope and print its uuid",
	Args:  cobra.ExactArgs(2),
	RunE:  runSupportAttachmentUpload,
}

var supportAttachmentDeleteCmd = &cobra.Command{
	Use:   "delete [uuid]",
	Short: "Delete an attachment that is not yet bound to a ticket",
	Args:  cobra.ExactArgs(1),
	RunE:  runSupportAttachmentDelete,
}

var supportDownloadCmd = &cobra.Command{
	Use:   "download [code] [attachment-uuid]",
	Short: "Download a ticket attachment",
	Args:  cobra.ExactArgs(2),
	RunE:  runSupportDownload,
}

func init() {
	supportCmd.AddCommand(supportMeCmd)
	supportCmd.AddCommand(supportListCmd)
	supportCmd.AddCommand(supportShowCmd)
	supportCmd.AddCommand(supportSetCmd)
	supportCmd.AddCommand(supportCloseCmd)
	supportCmd.AddCommand(supportReopenCmd)
	supportCmd.AddCommand(supportMessagesCmd)
	supportCmd.AddCommand(supportReplyCmd)
	supportCmd.AddCommand(supportNoteCmd)
	supportCmd.AddCommand(supportAttachmentCmd)
	supportCmd.AddCommand(supportDownloadCmd)

	supportAttachmentCmd.AddCommand(supportAttachmentUploadCmd)
	supportAttachmentCmd.AddCommand(supportAttachmentDeleteCmd)

	addTicketListFlags(supportListCmd, true)
	// Declared with the -o shorthand on purpose. A local flag named "org"
	// suppresses inheritance of the root's persistent --org/-o entirely for
	// this command, shorthand included, so without this the reflex
	// `respond list -o acme` would fail to parse rather than filter.
	supportListCmd.Flags().StringP("org", "o", "", "Filter by exact organization name (this surface is cross-org; there is no org scope to set)")

	supportShowCmd.Flags().Bool("no-messages", false, "Print the ticket details only, without the message thread")

	supportSetCmd.Flags().String("priority", "", "Priority: LOW, NORMAL, HIGH, URGENT")
	supportSetCmd.Flags().String("category", "", "Category: BUG, QUESTION, FEATURE_REQUEST, BILLING, OTHER")

	addMessageFlags(supportCloseCmd)
	addMessageFlags(supportReopenCmd)
	addThreadFlags(supportMessagesCmd)

	for _, c := range []*cobra.Command{supportReplyCmd, supportNoteCmd} {
		addMessageFlags(c)
		c.Flags().StringArray("attach", nil, "Local file to upload and attach (repeatable, max 10)")
		c.Flags().StringArray("attachment", nil, "Already-uploaded attachment uuid (repeatable, max 10)")
	}

	supportAttachmentDeleteCmd.Flags().Bool("force", false, "Skip the confirmation prompt")

	supportDownloadCmd.Flags().String("dest", "", "Destination file or directory (default: the attachment's filename here)")
	supportDownloadCmd.Flags().Bool("overwrite", false, "Replace an existing file at the destination")
}

func runSupportMe(cmd *cobra.Command, args []string) error {
	requireAuth()
	profile, err := svc.SupportMe(context.Background())
	if err != nil {
		return err
	}
	return formatter.FormatSupportProfile(profile)
}

func runSupportList(cmd *cobra.Command, args []string) error {
	requireAuth()
	opts, err := ticketListOptsFromFlags(cmd, true)
	if err != nil {
		return err
	}
	// --org is an exact-name filter here, not the global -o/--org scope the
	// organization commands use.
	opts.Org, _ = cmd.Flags().GetString("org")

	result, err := svc.SupportList(context.Background(), opts)
	if err != nil {
		return err
	}
	if err := formatter.FormatTicketList(result.Tickets, result.Total, true); err != nil {
		return err
	}
	output.PrintPagination(result.Page, result.Total, result.PerPage)
	return nil
}

func runSupportShow(cmd *cobra.Command, args []string) error {
	requireAuth()
	ctx := context.Background()

	ticket, err := svc.SupportGet(ctx, args[0])
	if err != nil {
		return err
	}
	noMessages, _ := cmd.Flags().GetBool("no-messages")
	if noMessages {
		return formatter.FormatTicketDetail(ticket, nil, 0)
	}
	thread, err := svc.SupportThread(ctx, args[0], service.TicketThreadOpts{Order: "asc", Page: 1, PerPage: showThreadPerPage})
	if err != nil {
		return err
	}
	return formatter.FormatTicketDetail(ticket, thread.Messages, thread.Total)
}

func runSupportSet(cmd *cobra.Command, args []string) error {
	requireAuth()
	priority, err := service.ValidateTicketEnum("priority", mustString(cmd, "priority"), models.TicketPriorities)
	if err != nil {
		return err
	}
	category, err := service.ValidateTicketEnum("category", mustString(cmd, "category"), models.TicketCategories)
	if err != nil {
		return err
	}
	ticket, err := svc.SupportUpdate(context.Background(), args[0], priority, category)
	if err != nil {
		return err
	}
	// No success banner ahead of the formatter: it would make -f json
	// unparseable, and the ticket it prints already shows the change.
	return formatter.FormatTicketDetail(ticket, nil, 0)
}

func runSupportClose(cmd *cobra.Command, args []string) error {
	requireAuth()
	message, err := messageFromFlags(cmd, false)
	if err != nil {
		return err
	}
	ticket, err := svc.SupportClose(context.Background(), args[0], message)
	if err != nil {
		return err
	}
	return formatter.FormatTicketStatusChange(ticket)
}

func runSupportReopen(cmd *cobra.Command, args []string) error {
	requireAuth()
	message, err := messageFromFlags(cmd, false)
	if err != nil {
		return err
	}
	ticket, err := svc.SupportReopen(context.Background(), args[0], message)
	if err != nil {
		return err
	}
	return formatter.FormatTicketStatusChange(ticket)
}

func runSupportMessages(cmd *cobra.Command, args []string) error {
	requireAuth()
	result, err := svc.SupportThread(context.Background(), args[0], threadOptsFromFlags(cmd))
	if err != nil {
		return err
	}
	if err := formatter.FormatTicketThread(result.Messages, result.Total); err != nil {
		return err
	}
	output.PrintPagination(result.Page, result.Total, result.PerPage)
	return nil
}

func runSupportReply(cmd *cobra.Command, args []string) error {
	return supportPostMessage(cmd, args[0], false)
}

func runSupportNote(cmd *cobra.Command, args []string) error {
	return supportPostMessage(cmd, args[0], true)
}

func supportPostMessage(cmd *cobra.Command, code string, internal bool) error {
	requireAuth()
	ctx := context.Background()

	body, err := messageFromFlags(cmd, true)
	if err != nil {
		return err
	}
	attachments, uploaded, err := attachmentsFromFlags(ctx, cmd, func(c context.Context, paths []string) ([]string, error) {
		return svc.SupportAttachmentUploadAll(c, code, paths)
	})
	if err != nil {
		return err
	}

	post := svc.SupportReply
	if internal {
		post = svc.SupportNote
	}
	msg, err := post(ctx, code, body, attachments)
	if err != nil {
		// The uploads were staged for this write; with the write gone they
		// are bound to nothing.
		svc.SupportDiscardAttachments(ctx, uploaded)
		return err
	}
	return formatter.FormatTicketMessagePosted(code, msg)
}

func runSupportAttachmentUpload(cmd *cobra.Command, args []string) error {
	requireAuth()
	att, err := svc.SupportAttachmentUpload(context.Background(), args[0], args[1])
	if err != nil {
		return err
	}
	return formatter.FormatTicketAttachment(att)
}

func runSupportAttachmentDelete(cmd *cobra.Command, args []string) error {
	requireAuth()
	force, _ := cmd.Flags().GetBool("force")
	ok, err := helpers.ConfirmOrForce(fmt.Sprintf("Delete attachment %s?", args[0]), force)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(os.Stderr, "Cancelled")
		return nil
	}
	if err := svc.SupportAttachmentDelete(context.Background(), args[0]); err != nil {
		return err
	}
	return formatter.FormatTicketAttachmentDeleted(args[0])
}

func runSupportDownload(cmd *cobra.Command, args []string) error {
	requireAuth()
	dest, _ := cmd.Flags().GetString("dest")
	overwrite, _ := cmd.Flags().GetBool("overwrite")
	result, err := svc.SupportAttachmentDownload(context.Background(), args[0], args[1], dest, overwrite)
	if err != nil {
		return namedOverwriteFlag(err)
	}
	return formatter.FormatTicketDownload(result)
}
