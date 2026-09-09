package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/models"
)

// Support tickets have two REST surfaces with identical shapes: the org side
// under /api/v1/organizations/{org} and the cross-org support side under
// /api/v1/support. Every operation below is implemented once against a
// ticketRoutes value and exposed twice — Ticket* here, Support* in
// support.go — so the two sides can never drift.

// DownloadResult is the outcome of an attachment download.
type DownloadResult = models.TicketDownloadResult

// OverwriteHint is the surface-neutral half of the destination-exists
// message. It is exported and shared rather than repeated as a literal:
// the CLI rewrites this phrase into its own flag name, and a silent
// no-op rewrite is exactly what a drifting copy would produce.
const OverwriteHint = "set overwrite to replace it"

// MaxTicketAttachments is the server's per-interaction attachment count cap.
const MaxTicketAttachments = 10

// MaxTicketDevices is the server's per-ticket related-device cap. Checked
// client-side for the same reason the attachment cap is: the flag help
// documents the number, so exceeding it should be named rather than
// round-tripped into a generic server error.
const MaxTicketDevices = 20

// ticketRoutes builds the paths of one ticket surface.
type ticketRoutes struct {
	base string
	// upload differs between the surfaces: the org side uploads to a
	// ticket-independent staging path, the support side uploads into the
	// target ticket (its org scope is derived from the ticket).
	upload func(code string) string
}

func orgTicketRoutes(org string) ticketRoutes {
	base := "/api/v1/organizations/" + url.PathEscape(org)
	return ticketRoutes{
		base:   base,
		upload: func(string) string { return base + "/ticket-attachments" },
	}
}

func (r ticketRoutes) tickets() string             { return r.base + "/tickets" }
func (r ticketRoutes) ticket(code string) string   { return r.base + "/tickets/" + code }
func (r ticketRoutes) attachment(id string) string { return r.base + "/ticket-attachments/" + id }
func (r ticketRoutes) download(code, id string) string {
	return r.base + "/tickets/" + code + "/attachments/" + id
}

// --- shared validation ---

// validTicketCode mirrors the server's ^[A-Za-z0-9]{8}$ pattern. Checking it
// client-side keeps an arbitrary string out of a URL path.
func validateTicketCode(code string) error {
	if len(code) != 8 {
		return &Error{Code: CodeInvalidInput, Message: fmt.Sprintf("invalid ticket code %q: expected 8 alphanumeric characters", code)}
	}
	for _, r := range code {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			continue
		}
		return &Error{Code: CodeInvalidInput, Message: fmt.Sprintf("invalid ticket code %q: expected 8 alphanumeric characters", code)}
	}
	return nil
}

// validateUUID keeps a caller-supplied attachment id to the shape the server
// mints, for the same reason validateTicketCode exists.
func validateAttachmentUUID(id string) error {
	if id == "" {
		return &Error{Code: CodeInvalidInput, Message: "attachment uuid is required"}
	}
	for _, r := range id {
		if (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return &Error{Code: CodeInvalidInput, Message: fmt.Sprintf("invalid attachment uuid %q", id)}
	}
	return nil
}

// ValidateAttachmentCount rejects a message whose attachments would exceed
// the per-message cap, counting the uuids the caller already holds together
// with the local files it wants uploaded.
//
// It lives here, not in a front-end, because the check has to happen before
// any upload starts: the alternative is uploading every file and then
// failing the post, leaving unbound attachments for the 24-hour sweep. Both
// the CLI and the MCP handlers call this one definition so the two cannot
// drift apart.
func ValidateAttachmentCount(existing, toUpload int) error {
	total := existing + toUpload
	if total <= MaxTicketAttachments {
		return nil
	}
	return &Error{
		Code:    CodeInvalidInput,
		Message: fmt.Sprintf("at most %d attachments per message; got %d", MaxTicketAttachments, total),
	}
}

// ValidateTicketEnum upper-cases value and checks it against allowed,
// returning a message that lists the accepted values. An empty value passes
// through unchanged so "flag not set" needs no special case at call sites.
func ValidateTicketEnum(flag, value string, allowed []string) (string, error) {
	if value == "" {
		return "", nil
	}
	upper := strings.ToUpper(strings.TrimSpace(value))
	for _, a := range allowed {
		if upper == a {
			return upper, nil
		}
	}
	return "", &Error{
		Code:    CodeInvalidInput,
		Message: fmt.Sprintf("invalid %s %q: expected one of %s", flag, value, strings.Join(allowed, ", ")),
	}
}

// ValidateTicketSort checks a `field` or `field:direction` sort expression
// against the server's field set, validating the direction client-side.
func ValidateTicketSort(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	field, dir, hasDir := strings.Cut(strings.TrimSpace(value), ":")
	field = strings.ToLower(field)
	ok := false
	for _, f := range models.TicketSortFields {
		if field == f {
			ok = true
			break
		}
	}
	if !ok {
		return "", &Error{
			Code:    CodeInvalidInput,
			Message: fmt.Sprintf("invalid sort field %q: expected one of %s", field, strings.Join(models.TicketSortFields, ", ")),
		}
	}
	if !hasDir {
		return field + ":desc", nil
	}
	dir = strings.ToLower(dir)
	if dir != "asc" && dir != "desc" {
		return "", &Error{Code: CodeInvalidInput, Message: fmt.Sprintf("invalid sort direction %q: expected asc or desc", dir)}
	}
	return field + ":" + dir, nil
}

// --- typed opts and results ---

// TicketListOpts collects the filters both list endpoints accept. Org is
// support-side only (an exact organization-name filter).
type TicketListOpts struct {
	Org         string
	Status      string
	Priority    string
	Category    string
	Participant string
	Device      string
	Q           string
	SortBy      string
	Page        int
	PerPage     int
}

// TicketListResult is a page of tickets plus its pagination.
type TicketListResult struct {
	Tickets []models.Ticket
	Total   int
	Page    int
	PerPage int
	Pages   int
}

// TicketThreadOpts pages and orders a ticket's interactions.
type TicketThreadOpts struct {
	Order   string
	Page    int
	PerPage int
}

// TicketThreadResult is a page of interactions plus its pagination.
type TicketThreadResult struct {
	Messages []models.TicketInteraction
	Total    int
	Page     int
	PerPage  int
	Pages    int
}

// TicketCreateInput is the body of a ticket creation. Attachments are uuids
// of files already uploaded (see TicketAttachmentUpload).
type TicketCreateInput struct {
	Subject      string
	Message      string
	Priority     string
	Category     string
	Devices      []string
	Participants []string
	Attachments  []string
}

// TicketUpdateInput is a partial ticket patch. A nil pointer means "leave
// alone"; a non-nil Devices pointer replaces the device list wholesale (an
// empty slice clears it).
type TicketUpdateInput struct {
	Subject  *string
	Priority string
	Category string
	Devices  *[]string
}

// --- shared implementation ---

func (s *Service) ticketList(ctx context.Context, r ticketRoutes, opts TicketListOpts) (*TicketListResult, error) {
	page := opts.Page
	if page < 1 {
		page = 1
	}
	perPage := opts.PerPage
	if perPage < 1 {
		perPage = 50
	}
	params := map[string]string{
		"page":     strconv.Itoa(page),
		"per_page": strconv.Itoa(perPage),
	}
	for k, v := range map[string]string{
		"org":         opts.Org,
		"status":      opts.Status,
		"priority":    opts.Priority,
		"category":    opts.Category,
		"participant": opts.Participant,
		"device":      opts.Device,
		"q":           opts.Q,
		"sort_by":     opts.SortBy,
	} {
		if v != "" {
			params[k] = v
		}
	}

	resp, err := s.api.Get(ctx, r.tickets(), params)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var out models.TicketListResponse
	if err := api.ParseResponse(resp, &out); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &TicketListResult{
		Tickets: out.GetItems(),
		Total:   out.Total,
		Page:    out.Page,
		PerPage: out.PerPage,
		Pages:   out.Pages,
	}, nil
}

func (s *Service) ticketGet(ctx context.Context, r ticketRoutes, code string) (*models.Ticket, error) {
	if err := validateTicketCode(code); err != nil {
		return nil, err
	}
	resp, err := s.api.Get(ctx, r.ticket(code), nil)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var ticket models.Ticket
	if err := api.ParseResponse(resp, &ticket); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &ticket, nil
}

func (s *Service) ticketThread(ctx context.Context, r ticketRoutes, code string, opts TicketThreadOpts) (*TicketThreadResult, error) {
	if err := validateTicketCode(code); err != nil {
		return nil, err
	}
	order := strings.ToLower(strings.TrimSpace(opts.Order))
	if order == "" {
		order = "asc"
	}
	if order != "asc" && order != "desc" {
		return nil, &Error{Code: CodeInvalidInput, Message: fmt.Sprintf("invalid order %q: expected asc or desc", opts.Order)}
	}
	page := opts.Page
	if page < 1 {
		page = 1
	}
	perPage := opts.PerPage
	if perPage < 1 {
		perPage = 50
	}
	params := map[string]string{
		"order":    order,
		"page":     strconv.Itoa(page),
		"per_page": strconv.Itoa(perPage),
	}
	resp, err := s.api.Get(ctx, r.ticket(code)+"/interactions", params)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var out models.TicketInteractionListResponse
	if err := api.ParseResponse(resp, &out); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &TicketThreadResult{
		Messages: out.GetItems(),
		Total:    out.Total,
		Page:     out.Page,
		PerPage:  out.PerPage,
		Pages:    out.Pages,
	}, nil
}

func (s *Service) ticketInteract(ctx context.Context, r ticketRoutes, code, kind, body string, attachments []string) (*models.TicketInteraction, error) {
	if err := validateTicketCode(code); err != nil {
		return nil, err
	}
	if strings.TrimSpace(body) == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "message body is required"}
	}
	if len(attachments) > MaxTicketAttachments {
		return nil, &Error{Code: CodeInvalidInput, Message: fmt.Sprintf("at most %d attachments per message", MaxTicketAttachments)}
	}
	payload := map[string]interface{}{
		"kind":        kind,
		"body":        body,
		"attachments": nonNilStrings(attachments),
	}
	resp, err := s.api.Post(ctx, r.ticket(code)+"/interactions", payload)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var out models.TicketInteraction
	if err := api.ParseResponse(resp, &out); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &out, nil
}

func (s *Service) ticketTransition(ctx context.Context, r ticketRoutes, code, action, message string) (*models.Ticket, error) {
	if err := validateTicketCode(code); err != nil {
		return nil, err
	}
	var payload interface{}
	if message != "" {
		payload = map[string]interface{}{"message": message}
	} else {
		payload = map[string]interface{}{}
	}
	resp, err := s.api.Post(ctx, r.ticket(code)+"/"+action, payload)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var ticket models.Ticket
	if err := api.ParseResponse(resp, &ticket); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &ticket, nil
}

// ticketUpload sends one local file as a raw request body. The content type
// comes from the extension (parameters such as "; charset=utf-8" stripped,
// since the server stores the type verbatim), and the filename travels as a
// query parameter because the body is the bytes themselves.
func (s *Service) ticketUpload(ctx context.Context, r ticketRoutes, code, path string) (*models.TicketAttachment, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, &Error{Code: CodeInvalidInput, Message: fmt.Sprintf("cannot read %s: %v", path, err), Err: err}
	}
	if info.IsDir() || !info.Mode().IsRegular() {
		return nil, &Error{Code: CodeInvalidInput, Message: fmt.Sprintf("%s is not a regular file", path)}
	}
	if info.Size() > api.MaxAttachmentBytes {
		return nil, &Error{
			Code:    CodeInvalidInput,
			Message: fmt.Sprintf("%s is %d bytes; the attachment limit is %d bytes (25 MiB)", path, info.Size(), api.MaxAttachmentBytes),
		}
	}
	if info.Size() == 0 {
		return nil, &Error{Code: CodeInvalidInput, Message: fmt.Sprintf("%s is empty", path)}
	}

	size := info.Size()
	open := func() (io.ReadCloser, int64, error) {
		f, err := os.Open(path)
		if err != nil {
			return nil, 0, &Error{Code: CodeInvalidInput, Message: fmt.Sprintf("cannot read %s: %v", path, err), Err: err}
		}
		return f, size, nil
	}

	params := map[string]string{"filename": filepath.Base(path)}
	resp, err := s.api.PostRaw(ctx, r.upload(code), params, open, attachmentContentType(path))
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var att models.TicketAttachment
	if err := api.ParseResponse(resp, &att); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &att, nil
}

// attachmentContentType maps a filename to a content type from its
// extension, dropping any parameters, and falls back to
// application/octet-stream for anything unrecognised.
func attachmentContentType(path string) string {
	ct := mime.TypeByExtension(filepath.Ext(path))
	if ct == "" {
		return "application/octet-stream"
	}
	if media, _, err := mime.ParseMediaType(ct); err == nil && media != "" {
		return media
	}
	if base, _, found := strings.Cut(ct, ";"); found {
		return strings.TrimSpace(base)
	}
	return ct
}

func (s *Service) ticketDeleteAttachment(ctx context.Context, r ticketRoutes, uuid string) error {
	if err := validateAttachmentUUID(uuid); err != nil {
		return err
	}
	resp, err := s.api.Delete(ctx, r.attachment(uuid))
	if err != nil {
		return wrapAPI("%v", err)
	}
	if err := api.ParseResponse(resp, nil); err != nil {
		return wrapAPI("%v", err)
	}
	return nil
}

// ticketUploadAll uploads every path and returns the resulting uuids. If any
// upload fails, nothing is left bound: the uuids already created are deleted
// best-effort (they are unbound, so a missed delete is swept by
// housekeeping) and the original failure is returned.
func (s *Service) ticketUploadAll(ctx context.Context, r ticketRoutes, code string, paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	if err := ValidateAttachmentCount(0, len(paths)); err != nil {
		return nil, err
	}
	uuids := make([]string, 0, len(paths))
	for _, p := range paths {
		att, err := s.ticketUpload(ctx, r, code, p)
		if err != nil {
			for _, id := range uuids {
				_ = s.ticketDeleteAttachment(ctx, r, id)
			}
			return nil, err
		}
		uuids = append(uuids, att.UUID)
	}
	return uuids, nil
}

// ticketDiscardAttachments best-effort deletes unbound uploads. It is what
// a caller runs when the write those uploads were staged for fails: the
// files are bound to nothing, and leaving them costs a 24-hour wait for the
// housekeeping sweep. Errors are ignored on purpose — the caller is already
// returning the real failure, and a missed delete is swept anyway.
func (s *Service) ticketDiscardAttachments(ctx context.Context, r ticketRoutes, uuids []string) {
	for _, id := range uuids {
		_ = s.ticketDeleteAttachment(ctx, r, id)
	}
}

// TicketDiscardAttachments discards org-side uploads staged for a write
// that then failed.
func (s *Service) TicketDiscardAttachments(ctx context.Context, org string, uuids []string) {
	if org == "" || len(uuids) == 0 {
		return
	}
	s.ticketDiscardAttachments(ctx, orgTicketRoutes(org), uuids)
}

// ticketFindAttachment locates an attachment in a ticket's thread so the
// download knows the filename, size and digest to verify against. The
// thread is the only place that metadata is exposed once an attachment is
// bound.
func (s *Service) ticketFindAttachment(ctx context.Context, r ticketRoutes, code, uuid string) (*models.TicketAttachment, error) {
	// The scan is bounded rather than unbounded: a download must not turn
	// into an open-ended crawl of a pathological thread. 100 per page is
	// the server's maximum, and 20 pages covers any real support
	// conversation with room to spare. Exhausting the cap is reported as
	// "not found in the first N messages" rather than "not on this ticket",
	// because those are different facts and the second would be a lie.
	const perPage = 100
	const maxPages = 20

	scanned := 0
	exhausted := false
	for page := 1; page <= maxPages; page++ {
		res, err := s.ticketThread(ctx, r, code, TicketThreadOpts{Order: "asc", Page: page, PerPage: perPage})
		if err != nil {
			return nil, err
		}
		for _, msg := range res.Messages {
			for i := range msg.Attachments {
				if strings.EqualFold(msg.Attachments[i].UUID, uuid) {
					att := msg.Attachments[i]
					return &att, nil
				}
			}
		}
		scanned += len(res.Messages)
		if len(res.Messages) < perPage || scanned >= res.Total {
			break
		}
		if page == maxPages {
			// More messages exist that were never looked at.
			exhausted = true
		}
	}

	if exhausted {
		return nil, &Error{
			Code: CodeInvalidInput,
			Message: fmt.Sprintf(
				"attachment %s was not found in the first %d messages of ticket %s; read the thread with 'messages' to find the message that carries it",
				uuid, scanned, code),
		}
	}
	return nil, &Error{
		Code:    CodeInvalidInput,
		Message: fmt.Sprintf("attachment %s is not on ticket %s", uuid, code),
	}
}

// ticketDownload streams one attachment to disk. The bytes never pass
// through the JSON decode path; the metadata used to name and verify the
// file comes from the thread listing, which does. The file is written to a
// temporary name beside its destination, hashed while streaming, and only
// installed once the digest matches — so a corrupted transfer never
// leaves a plausible-looking file behind. See installDownload for why the
// install, not the stat below, is what enforces overwrite=false.
func (s *Service) ticketDownload(ctx context.Context, r ticketRoutes, code, uuid, dest string, overwrite bool) (*DownloadResult, error) {
	if err := validateTicketCode(code); err != nil {
		return nil, err
	}
	if err := validateAttachmentUUID(uuid); err != nil {
		return nil, err
	}

	att, err := s.ticketFindAttachment(ctx, r, code, uuid)
	if err != nil {
		return nil, err
	}

	target, err := resolveDownloadPath(dest, att.Filename)
	if err != nil {
		return nil, err
	}
	if _, statErr := os.Stat(target); statErr == nil && !overwrite {
		return nil, destinationExistsErr(target)
	}

	path := att.DownloadPath
	// download_path is server-supplied. Accept it only in the shape the
	// contract promises — a relative API path — and otherwise rebuild it,
	// so a malformed value cannot redirect the request.
	if !strings.HasPrefix(path, "/api/v1/") {
		path = r.download(code, uuid)
	}

	resp, err := s.api.GetStream(ctx, path)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	defer resp.Body.Close()

	tmp, err := os.CreateTemp(filepath.Dir(target), ".ndcli-download-*")
	if err != nil {
		return nil, wrapAPI("failed to create temporary file: %v", err)
	}
	tmpName := tmp.Name()
	cleanup := func() {
		tmp.Close()
		os.Remove(tmpName)
	}

	hasher := sha256.New()
	// Bounded by the server's own per-file cap plus one byte, so a
	// misbehaving server cannot fill the disk.
	written, err := io.Copy(io.MultiWriter(tmp, hasher), io.LimitReader(resp.Body, api.MaxAttachmentBytes+1))
	if err != nil {
		cleanup()
		return nil, wrapAPI("download failed: %v", err)
	}
	if written > api.MaxAttachmentBytes {
		cleanup()
		return nil, &Error{Code: CodeAPIError, Message: "download exceeded the 25 MiB attachment limit"}
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return nil, wrapAPI("download failed: %v", err)
	}

	digest := hex.EncodeToString(hasher.Sum(nil))
	// Fail closed: sha256 is a required field, so its absence means the
	// listing is not what this client understands, and an unverifiable
	// download must not land looking verified.
	if att.SHA256 == "" {
		os.Remove(tmpName)
		return nil, &Error{
			Code:    CodeAPIError,
			Message: fmt.Sprintf("attachment %s carries no sha256; refusing to write an unverifiable file", att.UUID),
		}
	}
	if !strings.EqualFold(digest, att.SHA256) {
		os.Remove(tmpName)
		return nil, &Error{
			Code:    CodeAPIError,
			Message: fmt.Sprintf("checksum mismatch for %s: expected %s, got %s — the file was not written", att.Filename, att.SHA256, digest),
		}
	}
	if att.SizeBytes > 0 && written != att.SizeBytes {
		os.Remove(tmpName)
		return nil, &Error{
			Code:    CodeAPIError,
			Message: fmt.Sprintf("size mismatch for %s: expected %d bytes, got %d — the file was not written", att.Filename, att.SizeBytes, written),
		}
	}

	if err := installDownload(tmpName, target, overwrite); err != nil {
		return nil, err
	}

	return &DownloadResult{
		Path:        target,
		Filename:    att.Filename,
		ContentType: att.ContentType,
		Size:        written,
		SHA256:      digest,
	}, nil
}

// destinationExistsErr is the refusal both no-replace checks return: the
// courtesy stat before the transfer and the install that actually enforces
// it. Built in one place so the two can never word it differently.
func destinationExistsErr(target string) error {
	return &Error{
		Code:    CodeDestinationExists,
		Message: fmt.Sprintf("%s already exists; %s", target, OverwriteHint),
	}
}

// installDownload moves the verified temporary file to its destination.
//
// With overwrite=false the install itself must refuse to replace anything:
// the stat before the transfer is only an early courtesy check, and a
// destination created during a transfer that may run for minutes would
// otherwise be clobbered by the rename. os.Link is the portable
// no-replace install — it fails with EEXIST on any existing directory
// entry, including a dangling symlink, which a stat cannot even see. On a
// filesystem without hard links it falls back to an O_EXCL create, which
// carries the same atomic create / no-replace guarantee. Note that only
// the link path also publishes a complete file atomically; see
// copyNoReplace for what the fallback does not promise.
//
// With overwrite=true the rename stays: it replaces the directory entry
// itself, so a symlink at the destination is replaced rather than
// followed into a write at whatever it points to.
func installDownload(tmpName, target string, overwrite bool) error {
	if overwrite {
		if err := os.Rename(tmpName, target); err != nil {
			os.Remove(tmpName)
			return wrapAPI("failed to write %v", err)
		}
		return nil
	}

	err := os.Link(tmpName, target)
	if err != nil && !os.IsExist(err) {
		// Hard links are not available everywhere (a different device,
		// a filesystem or platform that refuses them). Rather than
		// enumerate errnos per platform, fall back to the other
		// no-replace primitive and let it report its own failure.
		err = copyNoReplace(tmpName, target)
	}
	os.Remove(tmpName)
	switch {
	case err == nil:
		return nil
	case os.IsExist(err):
		return destinationExistsErr(target)
	default:
		return wrapAPI("failed to write %v", err)
	}
}

// copyNoReplace writes src to a newly created dst, failing if dst already
// exists. O_EXCL makes the create-or-fail decision atomically in the
// kernel, so it never replaces an existing entry and never follows a
// symlink at that name.
//
// What it does not offer is atomic publication of a complete file: the
// name appears before the bytes do, so a reader watching that path can
// catch it short. A copy that fails takes the destination back down, but a
// crash mid-copy cannot, and the file it leaves is a partial one. That is
// the price of the fallback, and it is only paid where os.Link is
// unavailable — the link path publishes a fully written file in one step.
func copyNoReplace(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(dst)
		return err
	}
	return nil
}

// resolveDownloadPath decides where an attachment lands: an explicit dest
// (a directory means "inside it"), or the attachment's own filename in the
// working directory. The server-sanitized filename is reduced to its base
// name regardless, so nothing can escape the chosen directory.
func resolveDownloadPath(dest, filename string) (string, error) {
	safe := filepath.Base(filepath.FromSlash(filename))
	if safe == "" || safe == "." || safe == ".." || safe == string(filepath.Separator) {
		safe = "attachment"
	}
	if dest == "" {
		return safe, nil
	}
	if info, err := os.Stat(dest); err == nil && info.IsDir() {
		return filepath.Join(dest, safe), nil
	}
	if strings.HasSuffix(dest, string(filepath.Separator)) {
		return "", &Error{Code: CodeInvalidInput, Message: fmt.Sprintf("destination directory %s does not exist", dest)}
	}
	return dest, nil
}

// --- org-side public API ---

// TicketList returns a page of the organization's tickets.
func (s *Service) TicketList(ctx context.Context, org string, opts TicketListOpts) (*TicketListResult, error) {
	if org == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "organization is required"}
	}
	opts.Org = "" // org-side listing is scoped by the path, not a filter
	return s.ticketList(ctx, orgTicketRoutes(org), opts)
}

// TicketGet returns one ticket by code.
func (s *Service) TicketGet(ctx context.Context, org, code string) (*models.Ticket, error) {
	if org == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "organization is required"}
	}
	return s.ticketGet(ctx, orgTicketRoutes(org), code)
}

// TicketThread returns a page of a ticket's messages.
func (s *Service) TicketThread(ctx context.Context, org, code string, opts TicketThreadOpts) (*TicketThreadResult, error) {
	if org == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "organization is required"}
	}
	return s.ticketThread(ctx, orgTicketRoutes(org), code, opts)
}

// TicketCreate opens a ticket with its first message.
func (s *Service) TicketCreate(ctx context.Context, org string, in TicketCreateInput) (*models.Ticket, error) {
	if org == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "organization is required"}
	}
	if strings.TrimSpace(in.Subject) == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "subject is required"}
	}
	if strings.TrimSpace(in.Message) == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "message is required"}
	}
	if len(in.Attachments) > MaxTicketAttachments {
		return nil, &Error{Code: CodeInvalidInput, Message: fmt.Sprintf("at most %d attachments per message", MaxTicketAttachments)}
	}
	if len(in.Devices) > MaxTicketDevices {
		return nil, &Error{Code: CodeInvalidInput, Message: fmt.Sprintf("at most %d related devices per ticket", MaxTicketDevices)}
	}
	priority := in.Priority
	if priority == "" {
		priority = "NORMAL"
	}
	category := in.Category
	if category == "" {
		category = "OTHER"
	}
	// The list fields are always sent as [] rather than null: NDManager
	// validates them as lists and rejects null outright.
	body := map[string]interface{}{
		"subject":      in.Subject,
		"message":      in.Message,
		"priority":     priority,
		"category":     category,
		"devices":      nonNilStrings(in.Devices),
		"participants": nonNilStrings(in.Participants),
		"attachments":  nonNilStrings(in.Attachments),
	}
	r := orgTicketRoutes(org)
	resp, err := s.api.Post(ctx, r.tickets(), body)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var ticket models.Ticket
	if err := api.ParseResponse(resp, &ticket); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &ticket, nil
}

// TicketUpdate patches subject, priority, category and/or the device list.
// At least one field must be supplied; the check is client-side so the
// mistake is named before a round trip.
func (s *Service) TicketUpdate(ctx context.Context, org, code string, in TicketUpdateInput) (*models.Ticket, error) {
	if org == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "organization is required"}
	}
	if err := validateTicketCode(code); err != nil {
		return nil, err
	}
	body := map[string]interface{}{}
	if in.Subject != nil {
		body["subject"] = *in.Subject
	}
	if in.Priority != "" {
		body["priority"] = in.Priority
	}
	if in.Category != "" {
		body["category"] = in.Category
	}
	if in.Devices != nil {
		if len(*in.Devices) > MaxTicketDevices {
			return nil, &Error{Code: CodeInvalidInput, Message: fmt.Sprintf("at most %d related devices per ticket", MaxTicketDevices)}
		}
		body["devices"] = nonNilStrings(*in.Devices)
	}
	if len(body) == 0 {
		return nil, &Error{Code: CodeInvalidInput, Message: "nothing to update: provide at least one of subject, priority, category or devices"}
	}
	r := orgTicketRoutes(org)
	resp, err := s.api.Patch(ctx, r.ticket(code), body)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var ticket models.Ticket
	if err := api.ParseResponse(resp, &ticket); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &ticket, nil
}

// TicketClose closes a ticket, optionally recording a closing message
// first.
func (s *Service) TicketClose(ctx context.Context, org, code, message string) (*models.Ticket, error) {
	if org == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "organization is required"}
	}
	return s.ticketTransition(ctx, orgTicketRoutes(org), code, "close", message)
}

// TicketReopen reopens a closed ticket.
func (s *Service) TicketReopen(ctx context.Context, org, code, message string) (*models.Ticket, error) {
	if org == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "organization is required"}
	}
	return s.ticketTransition(ctx, orgTicketRoutes(org), code, "reopen", message)
}

// TicketReply appends a RESPONSE. A reply to a closed ticket reopens it;
// the returned interaction carries the resulting ticket status.
func (s *Service) TicketReply(ctx context.Context, org, code, body string, attachments []string) (*models.TicketInteraction, error) {
	if org == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "organization is required"}
	}
	return s.ticketInteract(ctx, orgTicketRoutes(org), code, models.TicketKindResponse, body, attachments)
}

// TicketNote appends an INTERNAL_NOTE, visible only to this organization.
func (s *Service) TicketNote(ctx context.Context, org, code, body string, attachments []string) (*models.TicketInteraction, error) {
	if org == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "organization is required"}
	}
	return s.ticketInteract(ctx, orgTicketRoutes(org), code, models.TicketKindInternalNote, body, attachments)
}

// TicketParticipantAdd adds an org member to a ticket and returns the
// participant list after the change.
func (s *Service) TicketParticipantAdd(ctx context.Context, org, code, email string) ([]models.TicketParticipant, error) {
	return s.ticketParticipant(ctx, org, code, email, true)
}

// TicketParticipantRemove removes a participant and returns the list after
// the change.
func (s *Service) TicketParticipantRemove(ctx context.Context, org, code, email string) ([]models.TicketParticipant, error) {
	return s.ticketParticipant(ctx, org, code, email, false)
}

func (s *Service) ticketParticipant(ctx context.Context, org, code, email string, add bool) ([]models.TicketParticipant, error) {
	if org == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "organization is required"}
	}
	if err := validateTicketCode(code); err != nil {
		return nil, err
	}
	if strings.TrimSpace(email) == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "participant email is required"}
	}
	r := orgTicketRoutes(org)

	if add {
		out, err := s.api.Post(ctx, r.ticket(code)+"/participants", map[string]interface{}{"email": email})
		if err != nil {
			return nil, wrapAPI("%v", err)
		}
		var list models.TicketParticipantListResponse
		if err := api.ParseResponse(out, &list); err != nil {
			return nil, wrapAPI("%v", err)
		}
		return list.GetItems(), nil
	}

	out, err := s.api.Delete(ctx, r.ticket(code)+"/participants/"+url.PathEscape(email))
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var list models.TicketParticipantListResponse
	if err := api.ParseResponse(out, &list); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return list.GetItems(), nil
}

// TicketAttachmentUpload uploads one file and returns its (still unbound)
// attachment record; pass the uuid in the attachments list of a create or
// reply within 24 hours, or it is swept.
func (s *Service) TicketAttachmentUpload(ctx context.Context, org, path string) (*models.TicketAttachment, error) {
	if org == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "organization is required"}
	}
	return s.ticketUpload(ctx, orgTicketRoutes(org), "", path)
}

// TicketAttachmentUploadAll uploads every path, cleaning up after itself if
// any single upload fails.
func (s *Service) TicketAttachmentUploadAll(ctx context.Context, org string, paths []string) ([]string, error) {
	if org == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "organization is required"}
	}
	return s.ticketUploadAll(ctx, orgTicketRoutes(org), "", paths)
}

// TicketAttachmentDelete deletes an attachment that is still unbound.
func (s *Service) TicketAttachmentDelete(ctx context.Context, org, uuid string) error {
	if org == "" {
		return &Error{Code: CodeInvalidInput, Message: "organization is required"}
	}
	return s.ticketDeleteAttachment(ctx, orgTicketRoutes(org), uuid)
}

// TicketAttachmentDownload streams a bound attachment to disk, verifying
// its sha256 against the thread metadata before the file appears.
func (s *Service) TicketAttachmentDownload(ctx context.Context, org, code, uuid, dest string, overwrite bool) (*DownloadResult, error) {
	if org == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "organization is required"}
	}
	return s.ticketDownload(ctx, orgTicketRoutes(org), code, uuid, dest, overwrite)
}
