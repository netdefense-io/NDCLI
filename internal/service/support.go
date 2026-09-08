package service

import (
	"context"
	"errors"
	"net/http"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/models"
)

// The support side of the ticket resource: cross-org by design, reachable
// only by an enabled support responder. Every operation delegates to the
// shared implementation in ticket.go with the /api/v1/support route prefix,
// so the two surfaces cannot drift.
//
// The one behaviour that is specific to this side is the 403. Every refusal
// on a /support route returns the same body by design — a caller cannot
// tell "no such responder" from "disabled" from "read-only token" — so the
// client renders one fixed sentence rather than the server's generic
// "Access denied", which would send a responder hunting for a permissions
// screen that does not exist.

// SupportNotResponderMessage is the single rendering of a 403 from any
// /support route.
const SupportNotResponderMessage = "this login or token is not an enabled support responder; read-only and organization-scoped tokens cannot write"

// supportTicketRoutes builds the paths of the support surface. Uploads go
// into the target ticket, whose organization scopes them.
func supportTicketRoutes() ticketRoutes {
	const base = "/api/v1/support"
	return ticketRoutes{
		base:   base,
		upload: func(code string) string { return base + "/tickets/" + code + "/attachments" },
	}
}

// supportErr maps a 403 to the fixed responder message and passes every
// other error through untouched.
func supportErr(err error) error {
	if err == nil {
		return nil
	}
	var apiErr *api.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusForbidden {
		return &Error{Code: CodeAPIError, Message: SupportNotResponderMessage, Err: err}
	}
	return err
}

// SupportMe returns the caller's responder profile, or the fixed 403
// message when the caller is not an enabled responder. Clients probe this
// before showing any support surface.
func (s *Service) SupportMe(ctx context.Context) (*models.SupportResponderProfile, error) {
	resp, err := s.api.Get(ctx, "/api/v1/support/me", nil)
	if err != nil {
		return nil, supportErr(wrapAPI("%v", err))
	}
	var profile models.SupportResponderProfile
	if err := api.ParseResponse(resp, &profile); err != nil {
		return nil, supportErr(wrapAPI("%v", err))
	}
	return &profile, nil
}

// SupportList returns a cross-org page of tickets. Opts.Org is an exact
// organization-name filter, not a scope: an unknown name returns an empty
// page rather than an error.
func (s *Service) SupportList(ctx context.Context, opts TicketListOpts) (*TicketListResult, error) {
	res, err := s.ticketList(ctx, supportTicketRoutes(), opts)
	return res, supportErr(err)
}

// SupportGet reads one ticket from any organization.
func (s *Service) SupportGet(ctx context.Context, code string) (*models.Ticket, error) {
	ticket, err := s.ticketGet(ctx, supportTicketRoutes(), code)
	return ticket, supportErr(err)
}

// SupportThread returns a page of a ticket's messages as support sees them:
// support-authored internal notes are included, the organization's own
// notes never are.
func (s *Service) SupportThread(ctx context.Context, code string, opts TicketThreadOpts) (*TicketThreadResult, error) {
	res, err := s.ticketThread(ctx, supportTicketRoutes(), code, opts)
	return res, supportErr(err)
}

// SupportUpdate patches priority and category. Support owns triage, not the
// customer's words: subject, devices and participants stay org-owned, and
// the API has no field for them here.
func (s *Service) SupportUpdate(ctx context.Context, code, priority, category string) (*models.Ticket, error) {
	if err := validateTicketCode(code); err != nil {
		return nil, err
	}
	body := map[string]interface{}{}
	if priority != "" {
		body["priority"] = priority
	}
	if category != "" {
		body["category"] = category
	}
	if len(body) == 0 {
		return nil, &Error{Code: CodeInvalidInput, Message: "nothing to update: provide priority and/or category"}
	}
	r := supportTicketRoutes()
	resp, err := s.api.Patch(ctx, r.ticket(code), body)
	if err != nil {
		return nil, supportErr(wrapAPI("%v", err))
	}
	var ticket models.Ticket
	if err := api.ParseResponse(resp, &ticket); err != nil {
		return nil, supportErr(wrapAPI("%v", err))
	}
	return &ticket, nil
}

// SupportClose closes a ticket, optionally recording a support reply first.
func (s *Service) SupportClose(ctx context.Context, code, message string) (*models.Ticket, error) {
	ticket, err := s.ticketTransition(ctx, supportTicketRoutes(), code, "close", message)
	return ticket, supportErr(err)
}

// SupportReopen reopens a closed ticket.
func (s *Service) SupportReopen(ctx context.Context, code, message string) (*models.Ticket, error) {
	ticket, err := s.ticketTransition(ctx, supportTicketRoutes(), code, "reopen", message)
	return ticket, supportErr(err)
}

// SupportReply posts a support RESPONSE, which moves the ticket to PENDING
// and emails every participant.
func (s *Service) SupportReply(ctx context.Context, code, body string, attachments []string) (*models.TicketInteraction, error) {
	msg, err := s.ticketInteract(ctx, supportTicketRoutes(), code, models.TicketKindResponse, body, attachments)
	return msg, supportErr(err)
}

// SupportNote posts a support INTERNAL_NOTE. It changes nothing, emails
// nobody, and is never visible to the organization.
func (s *Service) SupportNote(ctx context.Context, code, body string, attachments []string) (*models.TicketInteraction, error) {
	msg, err := s.ticketInteract(ctx, supportTicketRoutes(), code, models.TicketKindInternalNote, body, attachments)
	return msg, supportErr(err)
}

// SupportAttachmentUpload uploads a file into the target ticket's scope.
// Unlike the org side, the upload path carries the ticket code.
func (s *Service) SupportAttachmentUpload(ctx context.Context, code, path string) (*models.TicketAttachment, error) {
	if err := validateTicketCode(code); err != nil {
		return nil, err
	}
	att, err := s.ticketUpload(ctx, supportTicketRoutes(), code, path)
	return att, supportErr(err)
}

// SupportAttachmentUploadAll uploads every path, cleaning up after itself
// if any single upload fails.
func (s *Service) SupportAttachmentUploadAll(ctx context.Context, code string, paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	if err := validateTicketCode(code); err != nil {
		return nil, err
	}
	uuids, err := s.ticketUploadAll(ctx, supportTicketRoutes(), code, paths)
	return uuids, supportErr(err)
}

// SupportDiscardAttachments discards uploads staged for a write that then
// failed. Best-effort: the caller is already returning the real failure.
func (s *Service) SupportDiscardAttachments(ctx context.Context, uuids []string) {
	if len(uuids) == 0 {
		return
	}
	s.ticketDiscardAttachments(ctx, supportTicketRoutes(), uuids)
}

// SupportAttachmentDelete deletes an attachment that is still unbound.
func (s *Service) SupportAttachmentDelete(ctx context.Context, uuid string) error {
	return supportErr(s.ticketDeleteAttachment(ctx, supportTicketRoutes(), uuid))
}

// SupportAttachmentDownload streams a bound attachment to disk, verifying
// its sha256 before the file appears. The support-side note filter gates
// which attachments resolve at all.
func (s *Service) SupportAttachmentDownload(ctx context.Context, code, uuid, dest string, overwrite bool) (*DownloadResult, error) {
	res, err := s.ticketDownload(ctx, supportTicketRoutes(), code, uuid, dest, overwrite)
	return res, supportErr(err)
}
