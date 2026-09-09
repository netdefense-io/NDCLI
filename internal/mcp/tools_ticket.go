package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/service"
)

// Support-ticket tools, the org side of the resource. Every ndcli support
// command has a tool here (MCP-parity policy); the responder side
// (ndcli respond) lives in tools_support.go.
//
// Two conventions matter for callers:
//   - Ticket text (subjects, message bodies, filenames) is returned in data
//     fields only, never folded into the human `message` string.
//   - attach_paths reads local files and uploads them before the write, so a
//     model can attach a capture without a separate upload round trip.

type ticketListInput struct {
	Organization string `json:"organization,omitempty"`
	Status       string `json:"status,omitempty"`
	Priority     string `json:"priority,omitempty"`
	Category     string `json:"category,omitempty"`
	Participant  string `json:"participant,omitempty"`
	Device       string `json:"device,omitempty"`
	Q            string `json:"q,omitempty"`
	SortBy       string `json:"sort_by,omitempty"`
	Page         int    `json:"page,omitempty"`
	PerPage      int    `json:"per_page,omitempty"`
}

type ticketGetInput struct {
	Organization    string `json:"organization,omitempty"`
	Code            string `json:"code"`
	IncludeMessages *bool  `json:"include_messages,omitempty"`
}

type ticketCreateInput struct {
	Organization string   `json:"organization,omitempty"`
	Subject      string   `json:"subject"`
	Message      string   `json:"message"`
	Priority     string   `json:"priority,omitempty"`
	Category     string   `json:"category,omitempty"`
	Devices      []string `json:"devices,omitempty"`
	Participants []string `json:"participants,omitempty"`
	Attachments  []string `json:"attachments,omitempty"`
	AttachPaths  []string `json:"attach_paths,omitempty"`
}

type ticketUpdateInput struct {
	Organization string    `json:"organization,omitempty"`
	Code         string    `json:"code"`
	Subject      *string   `json:"subject,omitempty"`
	Priority     string    `json:"priority,omitempty"`
	Category     string    `json:"category,omitempty"`
	Devices      *[]string `json:"devices,omitempty"`
	Confirm      bool      `json:"confirm,omitempty"`
}

type ticketTransitionInput struct {
	Organization string `json:"organization,omitempty"`
	Code         string `json:"code"`
	Message      string `json:"message,omitempty"`
}

type ticketMessagesInput struct {
	Organization string `json:"organization,omitempty"`
	Code         string `json:"code"`
	Order        string `json:"order,omitempty"`
	Page         int    `json:"page,omitempty"`
	PerPage      int    `json:"per_page,omitempty"`
}

type ticketPostInput struct {
	Organization string   `json:"organization,omitempty"`
	Code         string   `json:"code"`
	Message      string   `json:"message"`
	Attachments  []string `json:"attachments,omitempty"`
	AttachPaths  []string `json:"attach_paths,omitempty"`
}

type ticketParticipantInput struct {
	Organization string `json:"organization,omitempty"`
	Code         string `json:"code"`
	Email        string `json:"email"`
	Confirm      bool   `json:"confirm,omitempty"`
}

type ticketAttachmentUploadInput struct {
	Organization string `json:"organization,omitempty"`
	Path         string `json:"path"`
}

type ticketAttachmentDeleteInput struct {
	Organization string `json:"organization,omitempty"`
	UUID         string `json:"uuid"`
	Confirm      bool   `json:"confirm,omitempty"`
}

type ticketDownloadInput struct {
	Organization string `json:"organization,omitempty"`
	Code         string `json:"code"`
	UUID         string `json:"uuid"`
	Path         string `json:"path,omitempty"`
	Overwrite    bool   `json:"overwrite,omitempty"`
}

// ticketListProperties are the filter properties shared by the org and
// respond list tools.
func ticketListProperties() map[string]interface{} {
	return map[string]interface{}{
		"status":   stringEnumProperty("Filter by status", models.TicketStatuses),
		"priority": stringEnumProperty("Filter by priority", models.TicketPriorities),
		"category": stringEnumProperty("Filter by category", models.TicketCategories),
		"q":        stringProperty("Filter by subject substring (max 100 characters)"),
		"sort_by":  stringProperty("Sort as FIELD:asc|desc over last_activity_at, created_at, updated_at, priority, status, subject (default last_activity_at:desc)"),
		"page":     intProperty("Page number", 1),
		"per_page": intProperty("Items per page (max 100)", 50),
	}
}

func ticketThreadProperties() map[string]interface{} {
	return map[string]interface{}{
		"order":    stringEnumProperty("Chronological order", []string{"asc", "desc"}),
		"page":     intProperty("Page number", 1),
		"per_page": intProperty("Items per page (max 100)", 50),
	}
}

func attachmentProperties() map[string]interface{} {
	return map[string]interface{}{
		"attachments":  stringArrayProperty("UUIDs of attachments already uploaded (max 10 per message)"),
		"attach_paths": stringArrayProperty("Local file paths to upload and attach now (max 10, each at most 25 MiB). Read from the machine running this server."),
	}
}

const ticketCodeDesc = "Ticket code (8 alphanumeric characters)"

// registerTicketTools registers the organization-side ticket tools.
func (s *Server) registerTicketTools() {
	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.support.list",
		Description: "List the organization's support tickets, with optional status/priority/category/participant/device/subject filters.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": mergeProps(ticketListProperties(), map[string]interface{}{
				"organization": organizationProperty(),
				"participant":  stringProperty("Filter by participant: 'me' or an email address"),
				"device":       stringProperty("Filter by related device UUID"),
			}),
		},
	}, s.handleTicketList)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.support.get",
		Description: "Read one support ticket, by default together with its message thread.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization":     organizationProperty(),
				"code":             stringProperty(ticketCodeDesc),
				"include_messages": boolProperty("Include the message thread (default true)"),
			},
			"required": []string{"code"},
		},
	}, s.handleTicketGet)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.support.create",
		Description: "Open a support ticket with its first message. Message text is plain text, never markup.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": mergeProps(attachmentProperties(), map[string]interface{}{
				"organization": organizationProperty(),
				"subject":      stringProperty("Ticket subject (plain text, max 255 characters)"),
				"message":      stringProperty("First message body (plain text, max 64 KiB)"),
				"priority":     stringEnumProperty("Priority (default NORMAL)", models.TicketPriorities),
				"category":     stringEnumProperty("Category (default OTHER)", models.TicketCategories),
				"devices":      stringArrayProperty("Related device UUIDs (max 20)"),
				"participants": stringArrayProperty("Emails of organization members to add as participants"),
			}),
			"required": []string{"subject", "message"},
		},
	}, s.handleTicketCreate)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.support.update",
		Description: "Change a ticket's subject, priority, category or related devices. At least one field is required. `devices` replaces the whole list, so an empty list clears every related device — which is why this tool requires confirm=true; without it, returns a preview.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"code":         stringProperty(ticketCodeDesc),
				"subject":      stringProperty("New subject (plain text, max 255 characters)"),
				"priority":     stringEnumProperty("New priority", models.TicketPriorities),
				"category":     stringEnumProperty("New category", models.TicketCategories),
				"devices":      stringArrayProperty("Full replacement list of related device UUIDs; an empty list clears them"),
				"confirm":      confirmProperty(),
			},
			"required": []string{"code"},
		},
	}, s.handleTicketUpdate)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.support.close",
		Description: "Close a ticket. An optional message is recorded as a reply before the status change.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"code":         stringProperty(ticketCodeDesc),
				"message":      stringProperty("Optional closing message (plain text)"),
			},
			"required": []string{"code"},
		},
	}, s.handleTicketClose)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.support.reopen",
		Description: "Reopen a closed ticket. An optional message is recorded as a reply before the status change.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"code":         stringProperty(ticketCodeDesc),
				"message":      stringProperty("Optional reopening message (plain text)"),
			},
			"required": []string{"code"},
		},
	}, s.handleTicketReopen)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.support.messages",
		Description: "List a ticket's messages, oldest first by default.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": mergeProps(ticketThreadProperties(), map[string]interface{}{
				"organization": organizationProperty(),
				"code":         stringProperty(ticketCodeDesc),
			}),
			"required": []string{"code"},
		},
	}, s.handleTicketMessages)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.support.reply",
		Description: "Post a reply visible to NetDefense Support. A reply to a closed ticket reopens it — read ticket_status from the result rather than assuming the previous status held.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": mergeProps(attachmentProperties(), map[string]interface{}{
				"organization": organizationProperty(),
				"code":         stringProperty(ticketCodeDesc),
				"message":      stringProperty("Message body (plain text, max 64 KiB)"),
			}),
			"required": []string{"code", "message"},
		},
	}, s.handleTicketReply)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.support.note",
		Description: "Post an internal note visible only to this organization. Support never sees it, and it does not change the ticket status.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": mergeProps(attachmentProperties(), map[string]interface{}{
				"organization": organizationProperty(),
				"code":         stringProperty(ticketCodeDesc),
				"message":      stringProperty("Note body (plain text, max 64 KiB)"),
			}),
			"required": []string{"code", "message"},
		},
	}, s.handleTicketNote)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.support.participant_add",
		Description: "Add an enabled organization member to a ticket as a participant.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"code":         stringProperty(ticketCodeDesc),
				"email":        stringProperty("Email of an enabled member of the same organization"),
			},
			"required": []string{"code", "email"},
		},
	}, s.handleTicketParticipantAdd)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.support.participant_remove",
		Description: "Remove a participant from a ticket. Requires confirm=true; without it, returns a preview. The ticket creator cannot be removed.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"code":         stringProperty(ticketCodeDesc),
				"email":        stringProperty("Participant email to remove"),
				"confirm":      confirmProperty(),
			},
			"required": []string{"code", "email"},
		},
	}, s.handleTicketParticipantRemove)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.support.attachment_upload",
		Description: "Upload a local file and return its attachment uuid. Reads the path on the machine running this server; the file must be a regular file of at most 25 MiB. The uuid is unbound until referenced in a create or reply, and is swept after 24 hours.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"path":         stringProperty("Local path of the file to upload"),
			},
			"required": []string{"path"},
		},
	}, s.handleTicketAttachmentUpload)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.support.attachment_delete",
		Description: "Delete an attachment that is not yet bound to a ticket. Requires confirm=true; without it, returns a preview.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"uuid":         stringProperty("Attachment uuid"),
				"confirm":      confirmProperty(),
			},
			"required": []string{"uuid"},
		},
	}, s.handleTicketAttachmentDelete)

	s.mcpServer.AddTool(&mcp.Tool{
		Name:        "ndcli.support.attachment_download",
		Description: "Download a ticket attachment to the machine running this server. Writes to path, or to the attachment's filename in the working directory; an existing file is never replaced unless overwrite is true. The SHA-256 is verified before the file appears.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"organization": organizationProperty(),
				"code":         stringProperty(ticketCodeDesc),
				"uuid":         stringProperty("Attachment uuid"),
				"path":         stringProperty("Destination file or directory (default: the attachment's filename in the working directory)"),
				"overwrite":    boolProperty("Replace an existing file at the destination (default false)"),
			},
			"required": []string{"code", "uuid"},
		},
	}, s.handleTicketAttachmentDownload)
}

// --- handlers ---
//
// Each handler is the thin auth+parse shell; the work sits in a *Core method
// that takes an already-parsed input. Tests drive the Core methods directly,
// because the test Service cannot satisfy RequireAuth (see testutil_test.go).

func decodeToolInput[T any](req *mcp.CallToolRequest) (*T, error) {
	argsJSON, _ := json.Marshal(req.Params.Arguments)
	return parseInput[T](argsJSON)
}

func (s *Server) handleTicketList(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	input, err := decodeToolInput[ticketListInput](req)
	if err != nil {
		return s.errorResult(err)
	}
	return s.ticketListCore(ctx, input)
}

func (s *Server) ticketListCore(_ context.Context, input *ticketListInput) (*mcp.CallToolResult, error) {
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	opts, err := ticketListOpts(input.Status, input.Priority, input.Category, input.SortBy)
	if err != nil {
		return s.errorResult(err)
	}
	opts.Participant = input.Participant
	opts.Device = input.Device
	opts.Q = input.Q
	opts.Page = input.Page
	opts.PerPage = input.PerPage

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	result, err := s.svc.TicketList(apiCtx, org, opts)
	if err != nil {
		return s.errorResult(err)
	}
	return s.successResultWithPagination(map[string]interface{}{
		"tickets": nonNilTickets(result.Tickets),
	}, result.Page, result.PerPage, result.Total)
}

// ticketListOpts validates the enum filters shared by both list tools.
func ticketListOpts(status, priority, category, sortBy string) (service.TicketListOpts, error) {
	var opts service.TicketListOpts
	var err error
	if opts.Status, err = service.ValidateTicketEnum("status", status, models.TicketStatuses); err != nil {
		return opts, err
	}
	if opts.Priority, err = service.ValidateTicketEnum("priority", priority, models.TicketPriorities); err != nil {
		return opts, err
	}
	if opts.Category, err = service.ValidateTicketEnum("category", category, models.TicketCategories); err != nil {
		return opts, err
	}
	if opts.SortBy, err = service.ValidateTicketSort(sortBy); err != nil {
		return opts, err
	}
	return opts, nil
}

func nonNilTickets(t []models.Ticket) []models.Ticket {
	if t == nil {
		return []models.Ticket{}
	}
	return t
}

func nonNilMessages(m []models.TicketInteraction) []models.TicketInteraction {
	if m == nil {
		return []models.TicketInteraction{}
	}
	return m
}

func (s *Server) handleTicketGet(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	input, err := decodeToolInput[ticketGetInput](req)
	if err != nil {
		return s.errorResult(err)
	}
	return s.ticketGetCore(ctx, input)
}

func (s *Server) ticketGetCore(_ context.Context, input *ticketGetInput) (*mcp.CallToolResult, error) {
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	ticket, err := s.svc.TicketGet(apiCtx, org, input.Code)
	if err != nil {
		return s.errorResult(err)
	}
	data := map[string]interface{}{"ticket": ticket}
	if input.IncludeMessages == nil || *input.IncludeMessages {
		thread, err := s.svc.TicketThread(apiCtx, org, input.Code, service.TicketThreadOpts{Order: "asc", Page: 1, PerPage: 100})
		if err != nil {
			return s.errorResult(err)
		}
		data["messages"] = nonNilMessages(thread.Messages)
		data["messages_total"] = thread.Total
	}
	return s.successResult(data, "")
}

func (s *Server) handleTicketCreate(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	input, err := decodeToolInput[ticketCreateInput](req)
	if err != nil {
		return s.errorResult(err)
	}
	return s.ticketCreateCore(ctx, input)
}

func (s *Server) ticketCreateCore(_ context.Context, input *ticketCreateInput) (*mcp.CallToolResult, error) {
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	priority, err := service.ValidateTicketEnum("priority", input.Priority, models.TicketPriorities)
	if err != nil {
		return s.errorResult(err)
	}
	category, err := service.ValidateTicketEnum("category", input.Category, models.TicketCategories)
	if err != nil {
		return s.errorResult(err)
	}

	attachments, uploaded, errResult := s.uploadAttachPaths(org, input.Attachments, input.AttachPaths)
	if errResult != nil {
		return errResult, nil
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	ticket, err := s.svc.TicketCreate(apiCtx, org, service.TicketCreateInput{
		Subject:      input.Subject,
		Message:      input.Message,
		Priority:     priority,
		Category:     category,
		Devices:      input.Devices,
		Participants: input.Participants,
		Attachments:  attachments,
	})
	if err != nil {
		// The uploads were staged for this write; with the write gone they
		// are bound to nothing.
		cleanupCtx, cleanupCancel := contextForCleanup()
		s.svc.TicketDiscardAttachments(cleanupCtx, org, uploaded)
		cleanupCancel()
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{"ticket": ticket},
		fmt.Sprintf("Ticket %s opened (%s).", ticket.Code, ticket.Status))
}

// uploadAttachPaths uploads the local files named by attach_paths and
// returns the full attachment list to post, plus the uuids this call
// created. The caller deletes those if the write they were staged for
// fails; a failure during the uploads themselves is already cleaned up
// inside the service.
func (s *Server) uploadAttachPaths(org string, existing, paths []string) (all []string, uploaded []string, errResult *mcp.CallToolResult) {
	// Checked before the first byte moves: uploading and then failing the
	// post would leave unbound attachments waiting on the sweep.
	if err := service.ValidateAttachmentCount(len(existing), len(paths)); err != nil {
		res, _ := s.errorResult(err)
		return nil, nil, res
	}
	if len(paths) == 0 {
		return existing, nil, nil
	}
	rawCtx, cancel := contextForRawTransfer()
	defer cancel()

	uploaded, err := s.svc.TicketAttachmentUploadAll(rawCtx, org, paths)
	if err != nil {
		res, _ := s.errorResult(err)
		return nil, nil, res
	}
	return append(append([]string{}, existing...), uploaded...), uploaded, nil
}

func (s *Server) handleTicketUpdate(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	input, err := decodeToolInput[ticketUpdateInput](req)
	if err != nil {
		return s.errorResult(err)
	}
	return s.ticketUpdateCore(ctx, input)
}

func (s *Server) ticketUpdateCore(_ context.Context, input *ticketUpdateInput) (*mcp.CallToolResult, error) {
	// Gated like most "update a named resource" tools here (device.rename,
	// ou.rename, template.update; network.update is an exception). It
	// matters more on this one than on most: the device list is a wholesale
	// replacement, so an uncontested call with devices: [] would clear every
	// related device.
	if !input.Confirm {
		return s.previewResult("update ticket", input.Code)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	in := service.TicketUpdateInput{Subject: input.Subject, Devices: input.Devices}
	if in.Priority, err = service.ValidateTicketEnum("priority", input.Priority, models.TicketPriorities); err != nil {
		return s.errorResult(err)
	}
	if in.Category, err = service.ValidateTicketEnum("category", input.Category, models.TicketCategories); err != nil {
		return s.errorResult(err)
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	ticket, err := s.svc.TicketUpdate(apiCtx, org, input.Code, in)
	if err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{"ticket": ticket}, fmt.Sprintf("Ticket %s updated.", ticket.Code))
}

func (s *Server) handleTicketClose(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return s.ticketTransition(ctx, req, true)
}

func (s *Server) handleTicketReopen(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return s.ticketTransition(ctx, req, false)
}

func (s *Server) ticketTransition(ctx context.Context, req *mcp.CallToolRequest, close bool) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	input, err := decodeToolInput[ticketTransitionInput](req)
	if err != nil {
		return s.errorResult(err)
	}
	return s.ticketTransitionCore(ctx, input, close)
}

func (s *Server) ticketTransitionCore(_ context.Context, input *ticketTransitionInput, close bool) (*mcp.CallToolResult, error) {
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	fn := s.svc.TicketReopen
	if close {
		fn = s.svc.TicketClose
	}
	ticket, err := fn(apiCtx, org, input.Code, input.Message)
	if err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{"ticket": ticket},
		fmt.Sprintf("Ticket %s is now %s.", ticket.Code, ticket.Status))
}

func (s *Server) handleTicketMessages(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	input, err := decodeToolInput[ticketMessagesInput](req)
	if err != nil {
		return s.errorResult(err)
	}
	return s.ticketMessagesCore(ctx, input)
}

func (s *Server) ticketMessagesCore(_ context.Context, input *ticketMessagesInput) (*mcp.CallToolResult, error) {
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	result, err := s.svc.TicketThread(apiCtx, org, input.Code, service.TicketThreadOpts{
		Order: input.Order, Page: input.Page, PerPage: input.PerPage,
	})
	if err != nil {
		return s.errorResult(err)
	}
	return s.successResultWithPagination(map[string]interface{}{
		"messages": nonNilMessages(result.Messages),
	}, result.Page, result.PerPage, result.Total)
}

func (s *Server) handleTicketReply(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return s.ticketPost(ctx, req, models.TicketKindResponse)
}

func (s *Server) handleTicketNote(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return s.ticketPost(ctx, req, models.TicketKindInternalNote)
}

func (s *Server) ticketPost(ctx context.Context, req *mcp.CallToolRequest, kind string) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	input, err := decodeToolInput[ticketPostInput](req)
	if err != nil {
		return s.errorResult(err)
	}
	return s.ticketPostCore(ctx, input, kind)
}

func (s *Server) ticketPostCore(_ context.Context, input *ticketPostInput, kind string) (*mcp.CallToolResult, error) {
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	attachments, uploaded, errResult := s.uploadAttachPaths(org, input.Attachments, input.AttachPaths)
	if errResult != nil {
		return errResult, nil
	}

	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	post := s.svc.TicketReply
	if kind == models.TicketKindInternalNote {
		post = s.svc.TicketNote
	}
	msg, err := post(apiCtx, org, input.Code, input.Message, attachments)
	if err != nil {
		cleanupCtx, cleanupCancel := contextForCleanup()
		s.svc.TicketDiscardAttachments(cleanupCtx, org, uploaded)
		cleanupCancel()
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{
		"interaction":   msg,
		"ticket_status": msg.TicketStatus,
	}, ticketPostMessageText(input.Code, kind, msg.TicketStatus))
}

func ticketPostMessageText(code, kind, status string) string {
	label := "Reply"
	if kind == models.TicketKindInternalNote {
		label = "Internal note"
	}
	if status == "" {
		return fmt.Sprintf("%s posted on %s.", label, code)
	}
	return fmt.Sprintf("%s posted on %s; ticket is now %s.", label, code, status)
}

func (s *Server) handleTicketParticipantAdd(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	input, err := decodeToolInput[ticketParticipantInput](req)
	if err != nil {
		return s.errorResult(err)
	}
	return s.ticketParticipantAddCore(ctx, input)
}

func (s *Server) ticketParticipantAddCore(_ context.Context, input *ticketParticipantInput) (*mcp.CallToolResult, error) {
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	participants, err := s.svc.TicketParticipantAdd(apiCtx, org, input.Code, input.Email)
	if err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{"participants": participants},
		fmt.Sprintf("Participant added to %s.", input.Code))
}

func (s *Server) handleTicketParticipantRemove(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	input, err := decodeToolInput[ticketParticipantInput](req)
	if err != nil {
		return s.errorResult(err)
	}
	return s.ticketParticipantRemoveCore(ctx, input)
}

func (s *Server) ticketParticipantRemoveCore(_ context.Context, input *ticketParticipantInput) (*mcp.CallToolResult, error) {
	if !input.Confirm {
		return s.previewResult("remove participant "+input.Email+" from ticket", input.Code)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	participants, err := s.svc.TicketParticipantRemove(apiCtx, org, input.Code, input.Email)
	if err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{"participants": participants},
		fmt.Sprintf("Participant removed from %s.", input.Code))
}

func (s *Server) handleTicketAttachmentUpload(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	input, err := decodeToolInput[ticketAttachmentUploadInput](req)
	if err != nil {
		return s.errorResult(err)
	}
	return s.ticketAttachmentUploadCore(ctx, input)
}

func (s *Server) ticketAttachmentUploadCore(_ context.Context, input *ticketAttachmentUploadInput) (*mcp.CallToolResult, error) {
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	rawCtx, cancel := contextForRawTransfer()
	defer cancel()

	att, err := s.svc.TicketAttachmentUpload(rawCtx, org, input.Path)
	if err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{"attachment": att},
		fmt.Sprintf("Uploaded attachment %s.", att.UUID))
}

func (s *Server) handleTicketAttachmentDelete(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	input, err := decodeToolInput[ticketAttachmentDeleteInput](req)
	if err != nil {
		return s.errorResult(err)
	}
	return s.ticketAttachmentDeleteCore(ctx, input)
}

func (s *Server) ticketAttachmentDeleteCore(_ context.Context, input *ticketAttachmentDeleteInput) (*mcp.CallToolResult, error) {
	if !input.Confirm {
		return s.previewResult("delete unbound attachment", input.UUID)
	}
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	apiCtx, cancel := contextWithTimeout()
	defer cancel()

	if err := s.svc.TicketAttachmentDelete(apiCtx, org, input.UUID); err != nil {
		return s.errorResult(err)
	}
	return s.successResult(map[string]interface{}{"uuid": input.UUID},
		fmt.Sprintf("Attachment %s deleted.", input.UUID))
}

func (s *Server) handleTicketAttachmentDownload(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.svc.RequireAuth(); err != nil {
		return s.errorResult(err)
	}
	input, err := decodeToolInput[ticketDownloadInput](req)
	if err != nil {
		return s.errorResult(err)
	}
	return s.ticketAttachmentDownloadCore(ctx, input)
}

func (s *Server) ticketAttachmentDownloadCore(_ context.Context, input *ticketDownloadInput) (*mcp.CallToolResult, error) {
	org, err := s.svc.ResolveOrg(input.Organization)
	if err != nil {
		return s.errorResult(err)
	}
	rawCtx, cancel := contextForRawTransfer()
	defer cancel()

	result, err := s.svc.TicketAttachmentDownload(rawCtx, org, input.Code, input.UUID, input.Path, input.Overwrite)
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
