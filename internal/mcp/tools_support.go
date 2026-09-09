package mcp

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/service"
)

// Responder-console tools (ndcli respond): the responder's side of the same
// tickets the ndcli.support.* tools work from the organization's side.
//
// None of these take an organization. The surface is cross-org by design, so
// `organization` on the list tool is an exact-name filter, not a scope. Every
// tool requires an enabled support responder; a refusal renders as one fixed
// sentence because the API returns an identical body for every reason.

type supportEmptyInput struct{}

type supportListInput struct {
	Organization string `json:"organization,omitempty"`
	Status       string `json:"status,omitempty"`
	Priority     string `json:"priority,omitempty"`
	Category     string `json:"category,omitempty"`
	Q            string `json:"q,omitempty"`
	SortBy       string `json:"sort_by,omitempty"`
	Page         int    `json:"page,omitempty"`
	PerPage      int    `json:"per_page,omitempty"`
}

type supportGetInput struct {
	Code            string `json:"code"`
	IncludeMessages *bool  `json:"include_messages,omitempty"`
}

type supportUpdateInput struct {
	Code     string `json:"code"`
	Priority string `json:"priority,omitempty"`
	Category string `json:"category,omitempty"`
	Confirm  bool   `json:"confirm,omitempty"`
}

type supportTransitionInput struct {
	Code    string `json:"code"`
	Message string `json:"message,omitempty"`
}

type supportMessagesInput struct {
	Code    string `json:"code"`
	Order   string `json:"order,omitempty"`
	Page    int    `json:"page,omitempty"`
	PerPage int    `json:"per_page,omitempty"`
}

type supportPostInput struct {
	Code        string   `json:"code"`
	Message     string   `json:"message"`
	Attachments []string `json:"attachments,omitempty"`
	AttachPaths []string `json:"attach_paths,omitempty"`
}

type supportAttachmentUploadInput struct {
	Code string `json:"code"`
	Path string `json:"path"`
}

type supportAttachmentDeleteInput struct {
	UUID    string `json:"uuid"`
	Confirm bool   `json:"confirm,omitempty"`
}

type supportDownloadInput struct {
	Code      string `json:"code"`
	UUID      string `json:"uuid"`
	Path      string `json:"path,omitempty"`
	Overwrite bool   `json:"overwrite,omitempty"`
}

// Every support tool description ends with one of these, so a model knows
// before it calls why the call might be refused. They are split because the
// two halves of the rule are not the same: any enabled responder may read,
// while writing additionally needs a login or a read-write token. A single
// note mentioning read-only tokens on a read tool reads as if reads were
// blocked.
const (
	responderReadNote  = " Requires an enabled NetDefense support responder; any responder, including one using a read-only token, may read."
	responderWriteNote = " Requires an enabled NetDefense support responder with write access: a login or a read-write token. Read-only tokens are refused, and an organization-scoped token is refused outright because this surface is cross-organization."
)

// registerSupportTools registers the responder-console tools (ndcli respond).
func (s *Server) registerSupportTools() {
	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.respond.me",
		Description: "Show the caller's support responder profile, including whether this principal may write." + responderReadNote,
		InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
	}, s.handleSupportMe)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.respond.list",
		Description: "List support tickets across every organization. `organization` is an exact-name filter, not a scope; an unknown name returns an empty page." + responderReadNote,
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": mergeProps(ticketListProperties(), map[string]interface{}{
				"organization": stringProperty("Filter by exact organization name"),
			}),
		},
	}, s.handleSupportList)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.respond.get",
		Description: "Read one ticket from any organization, by default with its message thread. Support-authored internal notes are visible here; the organization's own notes never are." + responderReadNote,
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"code":             stringProperty(ticketCodeDesc),
				"include_messages": boolProperty("Include the message thread (default true)"),
			},
			"required": []string{"code"},
		},
	}, s.handleSupportGet)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.respond.update",
		Description: "Set a ticket's priority and/or category. Support owns triage only: subject, devices and participants stay organization-owned. Requires confirm=true; without it, returns a preview." + responderWriteNote,
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"code":     stringProperty(ticketCodeDesc),
				"priority": stringEnumProperty("New priority", models.TicketPriorities),
				"category": stringEnumProperty("New category", models.TicketCategories),
				"confirm":  confirmProperty(),
			},
			"required": []string{"code"},
		},
	}, s.handleSupportUpdate)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.respond.close",
		Description: "Close a ticket. An optional message is recorded as a support reply before the status change." + responderWriteNote,
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"code":    stringProperty(ticketCodeDesc),
				"message": stringProperty("Optional closing message (plain text)"),
			},
			"required": []string{"code"},
		},
	}, s.handleSupportClose)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.respond.reopen",
		Description: "Reopen a closed ticket. An optional message is recorded as a support reply before the status change." + responderWriteNote,
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"code":    stringProperty(ticketCodeDesc),
				"message": stringProperty("Optional reopening message (plain text)"),
			},
			"required": []string{"code"},
		},
	}, s.handleSupportReopen)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.respond.messages",
		Description: "List a ticket's messages as support sees them, oldest first by default." + responderReadNote,
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": mergeProps(ticketThreadProperties(), map[string]interface{}{
				"code": stringProperty(ticketCodeDesc),
			}),
			"required": []string{"code"},
		},
	}, s.handleSupportMessages)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.respond.reply",
		Description: "Post a reply visible to the organization. A support reply moves the ticket to PENDING and emails every participant; a reply to a closed ticket reopens it, so read ticket_status from the result." + responderWriteNote,
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": mergeProps(attachmentProperties(), map[string]interface{}{
				"code":    stringProperty(ticketCodeDesc),
				"message": stringProperty("Message body (plain text, max 64 KiB)"),
			}),
			"required": []string{"code", "message"},
		},
	}, s.handleSupportReply)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.respond.note",
		Description: "Post an internal note visible only to support. It changes no status and emails nobody." + responderWriteNote,
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": mergeProps(attachmentProperties(), map[string]interface{}{
				"code":    stringProperty(ticketCodeDesc),
				"message": stringProperty("Note body (plain text, max 64 KiB)"),
			}),
			"required": []string{"code", "message"},
		},
	}, s.handleSupportNote)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.respond.attachment_upload",
		Description: "Upload a local file into a ticket's scope and return its attachment uuid. Reads the path on the machine running this server; the file must be a regular file of at most 25 MiB." + responderWriteNote,
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"code": stringProperty(ticketCodeDesc),
				"path": stringProperty("Local path of the file to upload"),
			},
			"required": []string{"code", "path"},
		},
	}, s.handleSupportAttachmentUpload)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.respond.attachment_delete",
		Description: "Delete an attachment that is not yet bound to a ticket. Requires confirm=true; without it, returns a preview." + responderWriteNote,
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"uuid":    stringProperty("Attachment uuid"),
				"confirm": confirmProperty(),
			},
			"required": []string{"uuid"},
		},
	}, s.handleSupportAttachmentDelete)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.respond.attachment_download",
		Description: "Download a ticket attachment to the machine running this server. Writes to path, or to the attachment's filename in the working directory; an existing file is never replaced unless overwrite is true. The SHA-256 is verified before the file appears." + responderWriteNote,
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"code":      stringProperty(ticketCodeDesc),
				"uuid":      stringProperty("Attachment uuid"),
				"path":      stringProperty("Destination file or directory (default: the attachment's filename in the working directory)"),
				"overwrite": boolProperty("Replace an existing file at the destination (default false)"),
			},
			"required": []string{"code", "uuid"},
		},
	}, s.handleSupportAttachmentDownload)
}

// --- handlers ---

func (s *Server) handleSupportMe(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	if _, err := decodeToolInput[supportEmptyInput](req); err != nil {
		return s.errorResult(err)
	}
	return s.supportMeCore(ctx)
}

func (s *Server) supportMeCore(_ context.Context) (*mcp.CallToolResult, error) {
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	profile, err := s.svc.SupportMe(apiCtx)
	if err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{"profile": profile}, "")
}

func (s *Server) handleSupportList(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	input, err := decodeToolInput[supportListInput](req)
	if err != nil {
		return s.errorResult(err)
	}
	return s.supportListCore(ctx, input)
}

func (s *Server) supportListCore(_ context.Context, input *supportListInput) (*mcp.CallToolResult, error) {
	opts, err := ticketListOpts(input.Status, input.Priority, input.Category, input.SortBy)
	if err != nil {
		return s.errorResult(err)
	}
	opts.Org = input.Organization
	opts.Q = input.Q
	opts.Page = input.Page
	opts.PerPage = input.PerPage

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	result, err := s.svc.SupportList(apiCtx, opts)
	if err != nil {
		return s.errorResult(err)
	}
	return s.successResultWithPagination(map[string]interface{}{
		"tickets": nonNilTickets(result.Tickets),
	}, result.Page, result.PerPage, result.Total)
}

func (s *Server) handleSupportGet(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	input, err := decodeToolInput[supportGetInput](req)
	if err != nil {
		return s.errorResult(err)
	}
	return s.supportGetCore(ctx, input)
}

func (s *Server) supportGetCore(_ context.Context, input *supportGetInput) (*mcp.CallToolResult, error) {
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	ticket, err := s.svc.SupportGet(apiCtx, input.Code)
	if err != nil {
		return s.errorResult(err)
	}
	data := map[string]interface{}{"ticket": ticket}
	if input.IncludeMessages == nil || *input.IncludeMessages {
		thread, err := s.svc.SupportThread(apiCtx, input.Code, service.TicketThreadOpts{Order: "asc", Page: 1, PerPage: 100})
		if err != nil {
			return s.errorResult(err)
		}
		data["messages"] = nonNilMessages(thread.Messages)
		data["messages_total"] = thread.Total
	}
	return s.successResult(data, "")
}

func (s *Server) handleSupportUpdate(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	input, err := decodeToolInput[supportUpdateInput](req)
	if err != nil {
		return s.errorResult(err)
	}
	return s.supportUpdateCore(ctx, input)
}

func (s *Server) supportUpdateCore(_ context.Context, input *supportUpdateInput) (*mcp.CallToolResult, error) {
	// Gated like the org-side ndcli.support.update. This responder patch
	// (ndcli.respond.update) cannot clear a list the way the org-side one
	// can, but a pair of twin surfaces that gates one and not the other
	// would be the surprising arrangement.
	if !input.Confirm {
		return s.previewResult("update ticket", input.Code)
	}
	priority, err := service.ValidateTicketEnum("priority", input.Priority, models.TicketPriorities)
	if err != nil {
		return s.errorResult(err)
	}
	category, err := service.ValidateTicketEnum("category", input.Category, models.TicketCategories)
	if err != nil {
		return s.errorResult(err)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	ticket, err := s.svc.SupportUpdate(apiCtx, input.Code, priority, category)
	if err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{"ticket": ticket}, fmt.Sprintf("Ticket %s updated.", ticket.Code))
}

func (s *Server) handleSupportClose(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return s.supportTransition(ctx, req, true)
}

func (s *Server) handleSupportReopen(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return s.supportTransition(ctx, req, false)
}

func (s *Server) supportTransition(ctx context.Context, req *mcp.CallToolRequest, close bool) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	input, err := decodeToolInput[supportTransitionInput](req)
	if err != nil {
		return s.errorResult(err)
	}
	return s.supportTransitionCore(ctx, input, close)
}

func (s *Server) supportTransitionCore(_ context.Context, input *supportTransitionInput, close bool) (*mcp.CallToolResult, error) {
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	fn := s.svc.SupportReopen
	if close {
		fn = s.svc.SupportClose
	}
	ticket, err := fn(apiCtx, input.Code, input.Message)
	if err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{"ticket": ticket},
		fmt.Sprintf("Ticket %s is now %s.", ticket.Code, ticket.Status))
}

func (s *Server) handleSupportMessages(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	input, err := decodeToolInput[supportMessagesInput](req)
	if err != nil {
		return s.errorResult(err)
	}
	return s.supportMessagesCore(ctx, input)
}

func (s *Server) supportMessagesCore(_ context.Context, input *supportMessagesInput) (*mcp.CallToolResult, error) {
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	result, err := s.svc.SupportThread(apiCtx, input.Code, service.TicketThreadOpts{
		Order: input.Order, Page: input.Page, PerPage: input.PerPage,
	})
	if err != nil {
		return s.errorResult(err)
	}
	return s.successResultWithPagination(map[string]interface{}{
		"messages": nonNilMessages(result.Messages),
	}, result.Page, result.PerPage, result.Total)
}

func (s *Server) handleSupportReply(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return s.supportPost(ctx, req, models.TicketKindResponse)
}

func (s *Server) handleSupportNote(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return s.supportPost(ctx, req, models.TicketKindInternalNote)
}

func (s *Server) supportPost(ctx context.Context, req *mcp.CallToolRequest, kind string) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	input, err := decodeToolInput[supportPostInput](req)
	if err != nil {
		return s.errorResult(err)
	}
	return s.supportPostCore(ctx, input, kind)
}

func (s *Server) supportPostCore(_ context.Context, input *supportPostInput, kind string) (*mcp.CallToolResult, error) {
	attachments, uploaded, errResult := s.uploadSupportAttachPaths(input.Code, input.Attachments, input.AttachPaths)
	if errResult != nil {
		return errResult, nil
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	post := s.svc.SupportReply
	if kind == models.TicketKindInternalNote {
		post = s.svc.SupportNote
	}
	msg, err := post(apiCtx, input.Code, input.Message, attachments)
	if err != nil {
		// The uploads were staged for this write; with the write gone they
		// are bound to nothing.
		cleanupCtx, cleanupCancel := contextForCleanup()
		s.svc.SupportDiscardAttachments(cleanupCtx, uploaded)
		cleanupCancel()
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"interaction":   msg,
		"ticket_status": msg.TicketStatus,
	}, ticketPostMessageText(input.Code, kind, msg.TicketStatus))
}

// uploadSupportAttachPaths is the support twin of uploadAttachPaths: it
// uploads into the target ticket's scope and reports which uuids this call
// created, so a failed write can discard exactly those.
func (s *Server) uploadSupportAttachPaths(code string, existing, paths []string) (all []string, uploaded []string, errResult *mcp.CallToolResult) {
	// Checked before the first byte moves, from the same rule the org side
	// and the CLI use.
	if err := service.ValidateAttachmentCount(len(existing), len(paths)); err != nil {
		res, _ := s.errorResult(err)
		return nil, nil, res
	}
	if len(paths) == 0 {
		return existing, nil, nil
	}
	rawCtx, cancel := contextForRawTransfer()
	defer cancel()

	uploaded, err := s.svc.SupportAttachmentUploadAll(rawCtx, code, paths)
	if err != nil {
		res, _ := s.errorResult(err)
		return nil, nil, res
	}
	return append(append([]string{}, existing...), uploaded...), uploaded, nil
}

func (s *Server) handleSupportAttachmentUpload(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	input, err := decodeToolInput[supportAttachmentUploadInput](req)
	if err != nil {
		return s.errorResult(err)
	}
	return s.supportAttachmentUploadCore(ctx, input)
}

func (s *Server) supportAttachmentUploadCore(_ context.Context, input *supportAttachmentUploadInput) (*mcp.CallToolResult, error) {
	rawCtx, cancel := contextForRawTransfer()
	defer cancel()

	att, err := s.svc.SupportAttachmentUpload(rawCtx, input.Code, input.Path)
	if err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{"attachment": att},
		fmt.Sprintf("Uploaded attachment %s.", att.UUID))
}

func (s *Server) handleSupportAttachmentDelete(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	input, err := decodeToolInput[supportAttachmentDeleteInput](req)
	if err != nil {
		return s.errorResult(err)
	}
	return s.supportAttachmentDeleteCore(ctx, input)
}

func (s *Server) supportAttachmentDeleteCore(_ context.Context, input *supportAttachmentDeleteInput) (*mcp.CallToolResult, error) {
	if !input.Confirm {
		return s.previewResult("delete unbound attachment", input.UUID)
	}
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	if err := s.svc.SupportAttachmentDelete(apiCtx, input.UUID); err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{"uuid": input.UUID},
		fmt.Sprintf("Attachment %s deleted.", input.UUID))
}

func (s *Server) handleSupportAttachmentDownload(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	input, err := decodeToolInput[supportDownloadInput](req)
	if err != nil {
		return s.errorResult(err)
	}
	return s.supportAttachmentDownloadCore(ctx, input)
}

func (s *Server) supportAttachmentDownloadCore(_ context.Context, input *supportDownloadInput) (*mcp.CallToolResult, error) {
	rawCtx, cancel := contextForRawTransfer()
	defer cancel()

	result, err := s.svc.SupportAttachmentDownload(rawCtx, input.Code, input.UUID, input.Path, input.Overwrite)
	if err != nil {
		return s.errorResult(err)
	}
	// Flat, named fields rather than a sentence: the caller needs the path
	// it can now read and the sha256 this client already verified, and
	// neither should have to be parsed back out of prose.
	return s.successResult(map[string]interface{}{
		"path":       result.Path,
		"filename":   result.Filename,
		"size_bytes": result.Size,
		"sha256":     result.SHA256,
	}, "Attachment downloaded and its checksum verified.")
}
