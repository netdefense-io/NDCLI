package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/models"
)

// ticketJSON writes v as a JSON response with the given status.
func ticketJSON(t *testing.T, w http.ResponseWriter, status int, v interface{}) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}

func TestTicketList_QueryParams(t *testing.T) {
	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/organizations/acme/tickets" {
			t.Errorf("path = %s", r.URL.Path)
		}
		got = r.URL.Query()
		ticketJSON(t, w, 200, models.TicketListResponse{
			Items: []models.Ticket{{Code: "Ab12Cd34", Subject: "hi"}},
			Total: 1, Page: 2, PerPage: 10, Pages: 1,
		})
	}))
	defer srv.Close()

	svc := newTestService(t, srv)
	res, err := svc.TicketList(context.Background(), "acme", TicketListOpts{
		Status: "OPEN", Priority: "HIGH", Category: "BUG",
		Participant: "me", Device: "dev-uuid", Q: "vpn",
		SortBy: "created_at:asc", Page: 2, PerPage: 10,
	})
	if err != nil {
		t.Fatalf("TicketList: %v", err)
	}
	for k, want := range map[string]string{
		"status": "OPEN", "priority": "HIGH", "category": "BUG",
		"participant": "me", "device": "dev-uuid", "q": "vpn",
		"sort_by": "created_at:asc", "page": "2", "per_page": "10",
	} {
		if got.Get(k) != want {
			t.Errorf("query %s = %q, want %q", k, got.Get(k), want)
		}
	}
	// The org-side listing is scoped by the path; an `org` filter would be
	// a support-surface concept leaking across.
	if got.Has("org") {
		t.Errorf("org filter must not be sent on the org-side list")
	}
	if len(res.Tickets) != 1 || res.Total != 1 {
		t.Errorf("unexpected result: %+v", res)
	}
}

func TestTicketCreate_ListsSerializeAsArrays(t *testing.T) {
	var body map[string]json.RawMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		ticketJSON(t, w, 201, models.Ticket{Code: "Ab12Cd34", Status: "OPEN"})
	}))
	defer srv.Close()

	svc := newTestService(t, srv)
	_, err := svc.TicketCreate(context.Background(), "acme", TicketCreateInput{
		Subject: "s", Message: "m",
	})
	if err != nil {
		t.Fatalf("TicketCreate: %v", err)
	}
	for _, field := range []string{"devices", "participants", "attachments"} {
		if got := string(body[field]); got != "[]" {
			t.Errorf("%s = %s, want [] (never null)", field, got)
		}
	}
	if got := string(body["priority"]); got != `"NORMAL"` {
		t.Errorf("priority default = %s", got)
	}
	if got := string(body["category"]); got != `"OTHER"` {
		t.Errorf("category default = %s", got)
	}
}

func TestTicketUpdate_RequiresAtLeastOneField(t *testing.T) {
	svc := newTestService(t, httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("no request should be made")
	})))
	_, err := svc.TicketUpdate(context.Background(), "acme", "Ab12Cd34", TicketUpdateInput{})
	if err == nil {
		t.Fatal("expected an error")
	}
	if e, ok := err.(*Error); !ok || e.Code != CodeInvalidInput {
		t.Errorf("expected CodeInvalidInput, got %v", err)
	}
}

func TestTicketCode_Validated(t *testing.T) {
	svc := newTestService(t, httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("no request should be made for an invalid code")
	})))
	if _, err := svc.TicketGet(context.Background(), "acme", "../../etc"); err == nil {
		t.Fatal("expected an error for a non-conforming code")
	}
}

// --- raw upload ---

func TestTicketAttachmentUpload_SendsRawBytes(t *testing.T) {
	payload := []byte("bytes to send\n")
	var (
		gotBody        []byte
		gotType        string
		gotLen         string
		gotQueryName   string
		gotContentSize int64
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/organizations/acme/ticket-attachments" {
			t.Errorf("path = %s", r.URL.Path)
		}
		gotType = r.Header.Get("Content-Type")
		gotLen = r.Header.Get("Content-Length")
		gotContentSize = r.ContentLength
		gotQueryName = r.URL.Query().Get("filename")
		gotBody, _ = api.ReadBody(r.Body)
		ticketJSON(t, w, 201, models.TicketAttachment{UUID: "u-1", Filename: "notes.txt", SizeBytes: int64(len(payload))})
	}))
	defer srv.Close()

	dir := t.TempDir()
	path := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}

	svc := newTestService(t, srv)
	att, err := svc.TicketAttachmentUpload(context.Background(), "acme", path)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if string(gotBody) != string(payload) {
		t.Errorf("body = %q, want %q", gotBody, payload)
	}
	if gotType != "text/plain" {
		t.Errorf("Content-Type = %q, want text/plain (no parameters)", gotType)
	}
	if gotLen != fmt.Sprint(len(payload)) || gotContentSize != int64(len(payload)) {
		t.Errorf("Content-Length = %q / %d, want %d", gotLen, gotContentSize, len(payload))
	}
	if gotQueryName != "notes.txt" {
		t.Errorf("filename query = %q", gotQueryName)
	}
	if att.UUID != "u-1" {
		t.Errorf("uuid = %q", att.UUID)
	}
}

func TestTicketAttachmentUpload_UnknownExtensionFallsBackToOctetStream(t *testing.T) {
	var gotType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotType = r.Header.Get("Content-Type")
		ticketJSON(t, w, 201, models.TicketAttachment{UUID: "u-1"})
	}))
	defer srv.Close()

	path := filepath.Join(t.TempDir(), "capture.zzzz")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := newTestService(t, srv).TicketAttachmentUpload(context.Background(), "acme", path); err != nil {
		t.Fatalf("upload: %v", err)
	}
	if gotType != "application/octet-stream" {
		t.Errorf("Content-Type = %q, want application/octet-stream", gotType)
	}
}

func TestTicketAttachmentUpload_RefusesOversizedFileBeforeRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("no request should be made for an oversized file")
	}))
	defer srv.Close()

	path := filepath.Join(t.TempDir(), "big.bin")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	// Sparse: only the size matters, the bytes are never read.
	if err := f.Truncate(api.MaxAttachmentBytes + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()

	_, err = newTestService(t, srv).TicketAttachmentUpload(context.Background(), "acme", path)
	if err == nil {
		t.Fatal("expected a size error")
	}
	if !strings.Contains(err.Error(), "25 MiB") {
		t.Errorf("error should name the limit, got %v", err)
	}
}

func TestTicketAttachmentUpload_SurfacesServerTextOn413(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ticketJSON(t, w, http.StatusRequestEntityTooLarge, map[string]string{
			"error": "Attachment exceeds the 25 MiB limit",
			"code":  "ATTACHMENT_TOO_LARGE",
		})
	}))
	defer srv.Close()

	path := filepath.Join(t.TempDir(), "small.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := newTestService(t, srv).TicketAttachmentUpload(context.Background(), "acme", path)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "Attachment exceeds the 25 MiB limit") {
		t.Errorf("server text not surfaced: %v", err)
	}
}

// TestTicketAttachmentUploadAll_CleansUpOnFailure is the --attach contract:
// if any upload fails, nothing is posted and the uuids already created are
// deleted, so no orphan sits in the store waiting on housekeeping.
func TestTicketAttachmentUploadAll_CleansUpOnFailure(t *testing.T) {
	var uploads int
	var deleted []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			uploads++
			if uploads == 2 {
				ticketJSON(t, w, 500, map[string]string{"error": "store unavailable"})
				return
			}
			ticketJSON(t, w, 201, models.TicketAttachment{UUID: fmt.Sprintf("0000000%d-0000-0000-0000-000000000000", uploads)})
		case http.MethodDelete:
			deleted = append(deleted, strings.TrimPrefix(r.URL.Path, "/api/v1/organizations/acme/ticket-attachments/"))
			ticketJSON(t, w, 200, map[string]string{"message": "deleted"})
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	var paths []string
	for _, name := range []string{"a.txt", "b.txt"} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}

	_, err := newTestService(t, srv).TicketAttachmentUploadAll(context.Background(), "acme", paths)
	if err == nil {
		t.Fatal("expected the second upload to fail")
	}
	if len(deleted) != 1 || deleted[0] != "00000001-0000-0000-0000-000000000000" {
		t.Errorf("expected the first upload to be deleted, got %v", deleted)
	}
}

// --- download ---

// ticketDownloadServer serves a thread listing carrying one attachment plus
// the attachment bytes themselves.
func ticketDownloadServer(t *testing.T, filename string, content []byte, digest string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/interactions"):
			ticketJSON(t, w, 200, models.TicketInteractionListResponse{
				Items: []models.TicketInteraction{{
					UUID: "i-1", Kind: "RESPONSE", Body: "see attached",
					Attachments: []models.TicketAttachment{{
						UUID:         "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
						Filename:     filename,
						ContentType:  "text/plain",
						SizeBytes:    int64(len(content)),
						SHA256:       digest,
						DownloadPath: "/api/v1/organizations/acme/tickets/Ab12Cd34/attachments/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
					}},
				}},
				Total: 1, Page: 1, PerPage: 100, Pages: 1,
			})
		default:
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(content)
		}
	}))
}

func TestTicketAttachmentDownload_WritesAndVerifies(t *testing.T) {
	content := []byte("attachment payload")
	sum := sha256.Sum256(content)
	digest := hex.EncodeToString(sum[:])
	srv := ticketDownloadServer(t, "capture.txt", content, digest)
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "out.txt")
	res, err := newTestService(t, srv).TicketAttachmentDownload(
		context.Background(), "acme", "Ab12Cd34", "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", dest, false)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if res.Path != dest || res.SHA256 != digest || res.Size != int64(len(content)) {
		t.Errorf("unexpected result: %+v", res)
	}
	on, err := os.ReadFile(dest)
	if err != nil || string(on) != string(content) {
		t.Errorf("file content = %q (err %v)", on, err)
	}
	// The temporary file must not survive the rename.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("expected only the downloaded file in %s, got %d entries", dir, len(entries))
	}
}

func TestTicketAttachmentDownload_ChecksumMismatchLeavesNoFile(t *testing.T) {
	content := []byte("attachment payload")
	srv := ticketDownloadServer(t, "capture.txt", content, strings.Repeat("0", 64))
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "out.txt")
	_, err := newTestService(t, srv).TicketAttachmentDownload(
		context.Background(), "acme", "Ab12Cd34", "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", dest, false)
	if err == nil {
		t.Fatal("expected a checksum error")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("unexpected error: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("expected no file left behind, got %v", entries)
	}
}

func TestTicketAttachmentDownload_RefusesOverwriteWithoutFlag(t *testing.T) {
	content := []byte("payload")
	sum := sha256.Sum256(content)
	srv := ticketDownloadServer(t, "capture.txt", content, hex.EncodeToString(sum[:]))
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "out.txt")
	if err := os.WriteFile(dest, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	svc := newTestService(t, srv)
	_, err := svc.TicketAttachmentDownload(context.Background(), "acme", "Ab12Cd34", "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", dest, false)
	if err == nil || !strings.Contains(err.Error(), OverwriteHint) {
		t.Fatalf("expected an overwrite refusal carrying OverwriteHint, got %v", err)
	}
	// The message is shared with MCP, where there is no such thing as a
	// flag, so the code carries the meaning and the CLI adds its own
	// wording (see namedOverwriteFlag).
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != CodeDestinationExists {
		t.Errorf("expected CodeDestinationExists, got %v", err)
	}
	if strings.Contains(err.Error(), "--overwrite") {
		t.Error("the shared message must not name a CLI flag")
	}
	if got, _ := os.ReadFile(dest); string(got) != "existing" {
		t.Errorf("existing file was modified: %q", got)
	}
	// With the flag, the same call succeeds and replaces the file.
	if _, err := svc.TicketAttachmentDownload(context.Background(), "acme", "Ab12Cd34", "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", dest, true); err != nil {
		t.Fatalf("download with overwrite: %v", err)
	}
	if got, _ := os.ReadFile(dest); string(got) != string(content) {
		t.Errorf("file was not replaced: %q", got)
	}
}

func TestTicketAttachmentDownload_UnknownAttachment(t *testing.T) {
	srv := ticketDownloadServer(t, "capture.txt", []byte("x"), strings.Repeat("a", 64))
	defer srv.Close()
	_, err := newTestService(t, srv).TicketAttachmentDownload(
		context.Background(), "acme", "Ab12Cd34", "ffffffff-ffff-ffff-ffff-ffffffffffff", filepath.Join(t.TempDir(), "o"), false)
	if err == nil || !strings.Contains(err.Error(), "is not on ticket") {
		t.Fatalf("expected a not-on-ticket error, got %v", err)
	}
}

// ticketDownloadRaceServer serves the same thread listing as
// ticketDownloadServer, but runs onBody just before the attachment bytes
// go out — i.e. after ticketDownload has already stat'ed the destination
// and decided it was free. It is the whole window the no-replace install
// has to close.
func ticketDownloadRaceServer(t *testing.T, content []byte, digest string, onBody func()) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/interactions"):
			ticketJSON(t, w, 200, models.TicketInteractionListResponse{
				Items: []models.TicketInteraction{{
					UUID: "i-1", Kind: "RESPONSE", Body: "see attached",
					Attachments: []models.TicketAttachment{{
						UUID:         "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
						Filename:     "capture.txt",
						ContentType:  "text/plain",
						SizeBytes:    int64(len(content)),
						SHA256:       digest,
						DownloadPath: "/api/v1/organizations/acme/tickets/Ab12Cd34/attachments/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
					}},
				}},
				Total: 1, Page: 1, PerPage: 100, Pages: 1,
			})
		default:
			onBody()
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(content)
		}
	}))
}

// tmpLeft counts the leftover .ndcli-download-* files in dir.
func tmpLeft(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".ndcli-download-") {
			n++
		}
	}
	return n
}

// A destination that appears *during* the transfer must not be replaced:
// the pre-transfer stat is only an early courtesy check, the install itself
// has to refuse to replace anything.
func TestTicketAttachmentDownload_DestinationCreatedDuringTransfer(t *testing.T) {
	content := []byte("attachment payload")
	sum := sha256.Sum256(content)
	digest := hex.EncodeToString(sum[:])

	dir := t.TempDir()
	dest := filepath.Join(dir, "out.txt")
	srv := ticketDownloadRaceServer(t, content, digest, func() {
		if err := os.WriteFile(dest, []byte("important data"), 0o600); err != nil {
			t.Errorf("seed destination: %v", err)
		}
	})
	defer srv.Close()

	_, err := newTestService(t, srv).TicketAttachmentDownload(
		context.Background(), "acme", "Ab12Cd34", "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", dest, false)
	if err == nil {
		t.Fatal("expected a destination-exists refusal, got a successful download")
	}
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != CodeDestinationExists {
		t.Errorf("expected CodeDestinationExists, got %v", err)
	}
	if !strings.Contains(err.Error(), OverwriteHint) {
		t.Errorf("expected the overwrite hint, got %v", err)
	}
	if got, _ := os.ReadFile(dest); string(got) != "important data" {
		t.Errorf("the destination was replaced: %q", got)
	}
	if n := tmpLeft(t, dir); n != 0 {
		t.Errorf("expected the temporary file to be removed, %d left", n)
	}
}

// The same window with overwrite=true is a deliberate replace.
func TestTicketAttachmentDownload_DestinationCreatedDuringTransferOverwrite(t *testing.T) {
	content := []byte("attachment payload")
	sum := sha256.Sum256(content)
	digest := hex.EncodeToString(sum[:])

	dir := t.TempDir()
	dest := filepath.Join(dir, "out.txt")
	srv := ticketDownloadRaceServer(t, content, digest, func() {
		if err := os.WriteFile(dest, []byte("important data"), 0o600); err != nil {
			t.Errorf("seed destination: %v", err)
		}
	})
	defer srv.Close()

	if _, err := newTestService(t, srv).TicketAttachmentDownload(
		context.Background(), "acme", "Ab12Cd34", "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", dest, true); err != nil {
		t.Fatalf("download with overwrite: %v", err)
	}
	if got, _ := os.ReadFile(dest); string(got) != string(content) {
		t.Errorf("file was not replaced: %q", got)
	}
	if n := tmpLeft(t, dir); n != 0 {
		t.Errorf("expected the temporary file to be removed, %d left", n)
	}
}

// A dangling symlink is a directory entry like any other: without
// overwrite it must be refused, and the link must survive untouched
// rather than being followed into a write at its target.
func TestTicketAttachmentDownload_DanglingSymlinkRefused(t *testing.T) {
	content := []byte("attachment payload")
	sum := sha256.Sum256(content)
	srv := ticketDownloadServer(t, "capture.txt", content, hex.EncodeToString(sum[:]))
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "out.txt")
	pointee := filepath.Join(dir, "nowhere.txt")
	if err := os.Symlink(pointee, dest); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	_, err := newTestService(t, srv).TicketAttachmentDownload(
		context.Background(), "acme", "Ab12Cd34", "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", dest, false)
	if err == nil {
		t.Fatal("expected a destination-exists refusal for a dangling symlink")
	}
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != CodeDestinationExists {
		t.Errorf("expected CodeDestinationExists, got %v", err)
	}
	target, lerr := os.Readlink(dest)
	if lerr != nil || target != pointee {
		t.Errorf("symlink was disturbed: %q (err %v)", target, lerr)
	}
	if _, err := os.Lstat(pointee); !os.IsNotExist(err) {
		t.Errorf("the symlink was followed and its pointee written: %v", err)
	}
	if n := tmpLeft(t, dir); n != 0 {
		t.Errorf("expected the temporary file to be removed, %d left", n)
	}
}

// With overwrite the symlink *entry* is replaced by the file. What the
// link pointed at stays as it was: replacing the entry must never mean
// writing through the link.
func TestTicketAttachmentDownload_SymlinkOverwriteReplacesEntryNotPointee(t *testing.T) {
	content := []byte("attachment payload")
	sum := sha256.Sum256(content)
	srv := ticketDownloadServer(t, "capture.txt", content, hex.EncodeToString(sum[:]))
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "out.txt")
	pointee := filepath.Join(dir, "pointee.txt")
	if err := os.WriteFile(pointee, []byte("pointee data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(pointee, dest); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if _, err := newTestService(t, srv).TicketAttachmentDownload(
		context.Background(), "acme", "Ab12Cd34", "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", dest, true); err != nil {
		t.Fatalf("download with overwrite: %v", err)
	}
	info, err := os.Lstat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Error("the symlink entry was not replaced by a regular file")
	}
	if got, _ := os.ReadFile(dest); string(got) != string(content) {
		t.Errorf("destination content = %q", got)
	}
	if got, _ := os.ReadFile(pointee); string(got) != "pointee data" {
		t.Errorf("the symlink was followed and its pointee overwritten: %q", got)
	}
}

// --- validation helpers ---

func TestValidateTicketEnum(t *testing.T) {
	got, err := ValidateTicketEnum("priority", "high", models.TicketPriorities)
	if err != nil || got != "HIGH" {
		t.Errorf("case-insensitive input should upper-case: %q, %v", got, err)
	}
	if _, err := ValidateTicketEnum("priority", "urgentish", models.TicketPriorities); err == nil {
		t.Error("expected an error for an unknown value")
	}
	if got, err := ValidateTicketEnum("priority", "", models.TicketPriorities); err != nil || got != "" {
		t.Errorf("empty must pass through: %q, %v", got, err)
	}
}

func TestValidateTicketSort(t *testing.T) {
	cases := map[string]string{
		"created_at:asc":   "created_at:asc",
		"created_at":       "created_at:desc",
		"LAST_ACTIVITY_AT": "last_activity_at:desc",
	}
	for in, want := range cases {
		got, err := ValidateTicketSort(in)
		if err != nil || got != want {
			t.Errorf("ValidateTicketSort(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"subject:sideways", "nonesuch:asc"} {
		if _, err := ValidateTicketSort(bad); err == nil {
			t.Errorf("ValidateTicketSort(%q) should fail", bad)
		}
	}
}

func TestResolveDownloadPath(t *testing.T) {
	dir := t.TempDir()
	// A directory destination puts the attachment inside it.
	got, err := resolveDownloadPath(dir, "notes.txt")
	if err != nil || got != filepath.Join(dir, "notes.txt") {
		t.Errorf("directory dest = %q, %v", got, err)
	}
	// A traversal-shaped filename is reduced to its base name.
	got, err = resolveDownloadPath("", "../../etc/passwd")
	if err != nil || got != "passwd" {
		t.Errorf("traversal filename = %q, %v", got, err)
	}
}

// TestTicketAttachmentDownload_MissingChecksumFailsClosed guards the fail-
// closed rule: sha256 is required by the API contract, so a listing without
// one is not a shape this client understands, and an unverifiable file must
// not land looking verified.
func TestTicketAttachmentDownload_MissingChecksumFailsClosed(t *testing.T) {
	srv := ticketDownloadServer(t, "capture.txt", []byte("payload"), "")
	defer srv.Close()

	dir := t.TempDir()
	_, err := newTestService(t, srv).TicketAttachmentDownload(
		context.Background(), "acme", "Ab12Cd34", "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		filepath.Join(dir, "out.txt"), false)
	if err == nil || !strings.Contains(err.Error(), "no sha256") {
		t.Fatalf("expected a fail-closed refusal, got %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("expected no file left behind, got %v", entries)
	}
}

// TestTicketDevices_CapEnforcedClientSide keeps the documented "max 20"
// honest: the flag help states the number, so exceeding it is named here
// rather than round-tripped into a generic server error.
func TestTicketDevices_CapEnforcedClientSide(t *testing.T) {
	svc := newTestService(t, httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("no request should be made once the device cap is exceeded")
	})))
	devices := make([]string, MaxTicketDevices+1)
	for i := range devices {
		devices[i] = fmt.Sprintf("device-%d", i)
	}
	ctx := context.Background()

	_, err := svc.TicketCreate(ctx, "acme", TicketCreateInput{Subject: "s", Message: "m", Devices: devices})
	if err == nil || !strings.Contains(err.Error(), "at most 20 related devices") {
		t.Errorf("create: expected a device-cap error, got %v", err)
	}
	_, err = svc.TicketUpdate(ctx, "acme", "Ab12Cd34", TicketUpdateInput{Devices: &devices})
	if err == nil || !strings.Contains(err.Error(), "at most 20 related devices") {
		t.Errorf("update: expected a device-cap error, got %v", err)
	}
}

// TestTicketFindAttachment_ScanCapIsReportedHonestly covers the bounded
// thread scan: when the cap is reached with messages still unread, the
// error must say the attachment was not found in what was scanned, not that
// it is absent from the ticket — the second would be a claim the scan never
// established.
func TestTicketFindAttachment_ScanCapIsReportedHonestly(t *testing.T) {
	// A thread that always reports far more messages than any page returns,
	// so the scan runs to its page cap.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		items := make([]models.TicketInteraction, 100)
		for i := range items {
			items[i] = models.TicketInteraction{UUID: fmt.Sprintf("i-%d", i), Kind: "RESPONSE", Body: "x"}
		}
		ticketJSON(t, w, 200, models.TicketInteractionListResponse{
			Items: items, Total: 100000, Page: 1, PerPage: 100, Pages: 1000,
		})
	}))
	defer srv.Close()

	_, err := newTestService(t, srv).TicketAttachmentDownload(
		context.Background(), "acme", "Ab12Cd34", "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		filepath.Join(t.TempDir(), "out"), false)
	if err == nil {
		t.Fatal("expected the scan to give up")
	}
	if !strings.Contains(err.Error(), "not found in the first 2000 messages") {
		t.Errorf("the error must name what was actually scanned, got %v", err)
	}
}

// TestTicketFindAttachment_ShortThreadSaysNotOnTicket is the other side: a
// thread the scan read to the end supports the stronger claim.
func TestTicketFindAttachment_ShortThreadSaysNotOnTicket(t *testing.T) {
	srv := ticketDownloadServer(t, "capture.txt", []byte("x"), strings.Repeat("a", 64))
	defer srv.Close()

	_, err := newTestService(t, srv).TicketAttachmentDownload(
		context.Background(), "acme", "Ab12Cd34", "ffffffff-ffff-ffff-ffff-ffffffffffff",
		filepath.Join(t.TempDir(), "out"), false)
	if err == nil || !strings.Contains(err.Error(), "is not on ticket") {
		t.Fatalf("expected the definite answer for a fully scanned thread, got %v", err)
	}
}
