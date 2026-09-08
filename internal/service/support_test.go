package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/models"
)

func TestSupportMe_ReturnsProfile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/support/me" {
			t.Errorf("path = %s", r.URL.Path)
		}
		ticketJSON(t, w, 200, models.SupportResponderProfile{
			DisplayName: "Dana Reed", Email: "dana@netdefense.io",
			Status: "ENABLED", Principal: "login", CanWrite: true,
		})
	}))
	defer srv.Close()

	profile, err := newTestService(t, srv).SupportMe(context.Background())
	if err != nil {
		t.Fatalf("SupportMe: %v", err)
	}
	if profile.Email != "dana@netdefense.io" || !profile.CanWrite {
		t.Errorf("unexpected profile: %+v", profile)
	}
}

// TestSupport403RendersFixedMessage covers the whole point of supportErr:
// every refusal on a /support route returns the same generic body by design,
// so the client must say what a responder can actually act on rather than
// echoing "Access denied".
func TestSupport403RendersFixedMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ticketJSON(t, w, http.StatusForbidden, map[string]string{
			"error": "Access denied", "code": "ACCESS_DENIED",
		})
	}))
	defer srv.Close()

	svc := newTestService(t, srv)
	ctx := context.Background()

	// A read-only PAT write, a non-responder read, and a disabled responder
	// are indistinguishable on the wire and all render the same sentence.
	calls := map[string]func() error{
		"me":     func() error { _, err := svc.SupportMe(ctx); return err },
		"list":   func() error { _, err := svc.SupportList(ctx, TicketListOpts{}); return err },
		"get":    func() error { _, err := svc.SupportGet(ctx, "Ab12Cd34"); return err },
		"reply":  func() error { _, err := svc.SupportReply(ctx, "Ab12Cd34", "hi", nil); return err },
		"close":  func() error { _, err := svc.SupportClose(ctx, "Ab12Cd34", ""); return err },
		"update": func() error { _, err := svc.SupportUpdate(ctx, "Ab12Cd34", "HIGH", ""); return err },
		"delete": func() error {
			return svc.SupportAttachmentDelete(ctx, "11111111-1111-1111-1111-111111111111")
		},
	}
	for name, call := range calls {
		err := call()
		if err == nil {
			t.Fatalf("%s: expected a 403 error", name)
		}
		if err.Error() != SupportNotResponderMessage {
			t.Errorf("%s: error = %q, want the fixed responder message", name, err.Error())
		}
	}
}

// TestSupportErr_PassesOtherErrorsThrough guards against the fixed message
// swallowing a genuine failure.
func TestSupportErr_PassesOtherErrorsThrough(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ticketJSON(t, w, http.StatusNotFound, map[string]string{
			"error": "Resource not found", "code": "NOT_FOUND",
		})
	}))
	defer srv.Close()

	_, err := newTestService(t, srv).SupportGet(context.Background(), "Ab12Cd34")
	if err == nil || !strings.Contains(err.Error(), "Resource not found") {
		t.Fatalf("a 404 must survive untouched, got %v", err)
	}
}

func TestSupportList_OrgIsAFilter(t *testing.T) {
	var query url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/support/tickets" {
			t.Errorf("path = %s", r.URL.Path)
		}
		query = r.URL.Query()
		ticketJSON(t, w, 200, models.TicketListResponse{
			Items: []models.Ticket{{Code: "Ab12Cd34", Organization: &models.TicketOrganizationRef{Name: "acme"}}},
			Total: 1, Page: 1, PerPage: 50, Pages: 1,
		})
	}))
	defer srv.Close()

	res, err := newTestService(t, srv).SupportList(context.Background(), TicketListOpts{Org: "acme", Status: "OPEN"})
	if err != nil {
		t.Fatalf("SupportList: %v", err)
	}
	if query.Get("org") != "acme" || query.Get("status") != "OPEN" {
		t.Errorf("query = %v", query)
	}
	if res.Tickets[0].OrgName() != "acme" {
		t.Errorf("support list rows must carry the organization: %+v", res.Tickets[0])
	}
}

func TestSupportUpdate_RequiresAField(t *testing.T) {
	svc := newTestService(t, httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("no request should be made")
	})))
	if _, err := svc.SupportUpdate(context.Background(), "Ab12Cd34", "", ""); err == nil {
		t.Fatal("expected an error when neither field is given")
	}
}

// TestSupportAttachmentUpload_UsesTicketScopedPath is the one route shape
// that differs from the org side: a responder uploads into the ticket, whose
// organization scopes the file.
func TestSupportAttachmentUpload_UsesTicketScopedPath(t *testing.T) {
	var path, body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		body = string(raw)
		ticketJSON(t, w, 201, models.TicketAttachment{UUID: "11111111-1111-1111-1111-111111111111"})
	}))
	defer srv.Close()

	p := filepath.Join(t.TempDir(), "trace.log")
	if err := os.WriteFile(p, []byte("trace"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := newTestService(t, srv).SupportAttachmentUpload(context.Background(), "Ab12Cd34", p); err != nil {
		t.Fatalf("upload: %v", err)
	}
	if path != "/api/v1/support/tickets/Ab12Cd34/attachments" {
		t.Errorf("path = %s", path)
	}
	if body != "trace" {
		t.Errorf("body = %q", body)
	}
}

func TestSupportAttachmentDownload_VerifiesChecksum(t *testing.T) {
	content := []byte("responder capture")
	sum := sha256.Sum256(content)
	digest := hex.EncodeToString(sum[:])
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/interactions") {
			if !strings.HasPrefix(r.URL.Path, "/api/v1/support/") {
				t.Errorf("thread must be read from the support surface, got %s", r.URL.Path)
			}
			ticketJSON(t, w, 200, models.TicketInteractionListResponse{
				Items: []models.TicketInteraction{{
					UUID: "i-1", Kind: "INTERNAL_NOTE", Body: "support-only note",
					Attachments: []models.TicketAttachment{{
						UUID:     "33333333-3333-3333-3333-333333333333",
						Filename: "capture.log", SizeBytes: int64(len(content)), SHA256: digest,
						DownloadPath: "/api/v1/support/tickets/Ab12Cd34/attachments/33333333-3333-3333-3333-333333333333",
					}},
				}},
				Total: 1, Page: 1, PerPage: 100, Pages: 1,
			})
			return
		}
		_, _ = w.Write(content)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "out.log")
	res, err := newTestService(t, srv).SupportAttachmentDownload(
		context.Background(), "Ab12Cd34", "33333333-3333-3333-3333-333333333333", dest, false)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if res.SHA256 != digest {
		t.Errorf("digest = %s", res.SHA256)
	}
	if got, _ := os.ReadFile(dest); string(got) != string(content) {
		t.Errorf("file = %q", got)
	}
}

// TestSupportAttachmentDelete_UsesSupportScopedPath pins the delete route.
// It is the mirror of the upload-path test: unlike the upload, which is
// scoped to the ticket, the delete is addressed by uuid alone under the
// support prefix, so a copy-paste of the org-side path would be a silent
// cross-surface call.
func TestSupportAttachmentDelete_UsesSupportScopedPath(t *testing.T) {
	var path, method string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, method = r.URL.Path, r.Method
		ticketJSON(t, w, 200, map[string]string{"message": "Attachment deleted successfully"})
	}))
	defer srv.Close()

	if err := newTestService(t, srv).SupportAttachmentDelete(
		context.Background(), "33333333-3333-3333-3333-333333333333"); err != nil {
		t.Fatalf("SupportAttachmentDelete: %v", err)
	}
	if method != http.MethodDelete {
		t.Errorf("method = %s, want DELETE", method)
	}
	if path != "/api/v1/support/ticket-attachments/33333333-3333-3333-3333-333333333333" {
		t.Errorf("path = %s", path)
	}
}
