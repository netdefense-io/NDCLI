package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/service"
)

func TestHandleSupportMe_RequiresAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("no request should reach the API without auth")
	}))
	defer srv.Close()

	s := newTestServer(t, srv, "")
	result, err := s.handleSupportMe(context.Background(), &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{}`)},
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	if resp.Success {
		t.Fatal("expected the auth gate to reject the call")
	}
}

// TestSupportCore_NeedsNoOrganization is the structural guarantee: the
// support surface is cross-org, so a server with no configured default
// organization must still work.
func TestSupportCore_NeedsNoOrganization(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ticketJSON(t, w, 200, models.TicketListResponse{
			Items: []models.Ticket{{Code: "Ab12Cd34", Organization: &models.TicketOrganizationRef{Name: "acme"}}},
			Total: 1, Page: 1, PerPage: 50, Pages: 1,
		})
	}))
	defer srv.Close()

	s := newTestServer(t, srv, "") // no default organization at all
	result, err := s.supportListCore(context.Background(), &supportListInput{})
	if err != nil {
		t.Fatalf("core: %v", err)
	}
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	if !resp.Success {
		t.Fatalf("support list must not require an organization: %+v", resp.Error)
	}
}

func TestSupportListCore_OrganizationIsAFilter(t *testing.T) {
	var query string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		ticketJSON(t, w, 200, models.TicketListResponse{Total: 0, Page: 1, PerPage: 50, Pages: 0})
	}))
	defer srv.Close()

	s := newTestServer(t, srv, "other-org")
	if _, err := s.supportListCore(context.Background(), &supportListInput{Organization: "acme", Priority: "urgent"}); err != nil {
		t.Fatalf("core: %v", err)
	}
	if !strings.Contains(query, "org=acme") || !strings.Contains(query, "priority=URGENT") {
		t.Errorf("query = %q", query)
	}
}

// TestSupportWriteCore_ReadOnlyTokenGetsFixedMessage covers an RO PAT
// writing on the support surface: the API answers 403 with its generic body
// and the tool must return the one sentence that explains it.
func TestSupportWriteCore_ReadOnlyTokenGetsFixedMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ticketJSON(t, w, http.StatusForbidden, map[string]string{
			"error": "Access denied", "code": "ACCESS_DENIED",
		})
	}))
	defer srv.Close()

	s := newTestServer(t, srv, "")
	result, _ := s.supportPostCore(context.Background(), &supportPostInput{
		Code: "Ab12Cd34", Message: "we are looking into it",
	}, models.TicketKindResponse)

	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	if resp.Success {
		t.Fatal("expected the write to be refused")
	}
	if resp.Error == nil || resp.Error.Message != service.SupportNotResponderMessage {
		t.Errorf("error = %+v, want the fixed responder message", resp.Error)
	}
}

func TestSupportAttachmentDeleteCore_PreviewWithoutConfirm(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("no request should be made without confirm")
	}))
	defer srv.Close()

	s := newTestServer(t, srv, "")
	result, _ := s.supportAttachmentDeleteCore(context.Background(), &supportAttachmentDeleteInput{
		UUID: "33333333-3333-3333-3333-333333333333",
	})
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	if !resp.Success || !strings.Contains(resp.Message, "confirm=true") {
		t.Fatalf("expected a preview, got %q", resp.Message)
	}
}

func TestSupportAttachmentUploadCore_TicketScopedPath(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		ticketJSON(t, w, 201, models.TicketAttachment{UUID: "33333333-3333-3333-3333-333333333333"})
	}))
	defer srv.Close()

	p := filepath.Join(t.TempDir(), "trace.log")
	if err := os.WriteFile(p, []byte("trace"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := newTestServer(t, srv, "")
	result, err := s.supportAttachmentUploadCore(context.Background(), &supportAttachmentUploadInput{Code: "Ab12Cd34", Path: p})
	if err != nil {
		t.Fatalf("core: %v", err)
	}
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	if !resp.Success {
		t.Fatalf("upload failed: %+v", resp.Error)
	}
	if path != "/api/v1/support/tickets/Ab12Cd34/attachments" {
		t.Errorf("path = %s", path)
	}
}

func TestSupportAttachmentDownloadCore_NoOverwriteWithoutFlag(t *testing.T) {
	content := []byte("responder capture")
	sum := sha256.Sum256(content)
	digest := hex.EncodeToString(sum[:])
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/interactions") {
			ticketJSON(t, w, 200, models.TicketInteractionListResponse{
				Items: []models.TicketInteraction{{
					UUID: "i-1", Kind: "RESPONSE", Body: "attached",
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
	s := newTestServer(t, srv, "")
	in := &supportDownloadInput{Code: "Ab12Cd34", UUID: "33333333-3333-3333-3333-333333333333", Path: dest}

	result, _ := s.supportAttachmentDownloadCore(context.Background(), in)
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	if !resp.Success {
		t.Fatalf("first download failed: %+v", resp.Error)
	}

	result, _ = s.supportAttachmentDownloadCore(context.Background(), in)
	resp = ToolResponse{}
	decodeToolResult(t, result, &resp)
	if resp.Success {
		t.Fatal("expected the second download to refuse to overwrite")
	}
}

// TestSupportAttachmentTransfers_NotBoundByJSONTimeout is the support-side
// twin of the org-side regression: a raw transfer handed the 30 s JSON
// budget as its parent would be capped at 30 s regardless of the api
// package's own per-transfer deadline.
func TestSupportAttachmentTransfers_NotBoundByJSONTimeout(t *testing.T) {
	const serverDelay = 120 * time.Millisecond

	content := []byte("responder payload")
	sum := sha256.Sum256(content)
	digest := hex.EncodeToString(sum[:])

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/attachments"):
			time.Sleep(serverDelay)
			ticketJSON(t, w, 201, models.TicketAttachment{UUID: "55555555-5555-5555-5555-555555555555"})
		case strings.HasSuffix(r.URL.Path, "/interactions"):
			ticketJSON(t, w, 200, models.TicketInteractionListResponse{
				Items: []models.TicketInteraction{{
					UUID: "i-1", Kind: "RESPONSE", Body: "attached",
					Attachments: []models.TicketAttachment{{
						UUID:     "55555555-5555-5555-5555-555555555555",
						Filename: "slow.bin", SizeBytes: int64(len(content)), SHA256: digest,
						DownloadPath: "/api/v1/support/tickets/Ab12Cd34/attachments/55555555-5555-5555-5555-555555555555",
					}},
				}},
				Total: 1, Page: 1, PerPage: 100, Pages: 1,
			})
		default:
			time.Sleep(serverDelay)
			_, _ = w.Write(content)
		}
	}))
	defer srv.Close()

	original := apiTimeout
	apiTimeout = 30 * time.Millisecond
	defer func() { apiTimeout = original }()

	s := newTestServer(t, srv, "")
	dir := t.TempDir()
	path := filepath.Join(dir, "slow.bin")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	result, _ := s.supportAttachmentUploadCore(context.Background(), &supportAttachmentUploadInput{
		Code: "Ab12Cd34", Path: path,
	})
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	if !resp.Success {
		t.Errorf("support upload must not be bound by apiTimeout: %+v", resp.Error)
	}

	result, _ = s.supportAttachmentDownloadCore(context.Background(), &supportDownloadInput{
		Code: "Ab12Cd34", UUID: "55555555-5555-5555-5555-555555555555",
		Path: filepath.Join(dir, "out.bin"),
	})
	resp = ToolResponse{}
	decodeToolResult(t, result, &resp)
	if !resp.Success {
		t.Errorf("support download must not be bound by apiTimeout: %+v", resp.Error)
	}
}

// TestSupportUpdateCore_PreviewWithoutConfirm keeps the two twin surfaces
// aligned: gating ndcli.ticket.update but not its support counterpart would
// be the surprising arrangement.
func TestSupportUpdateCore_PreviewWithoutConfirm(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("no request should be made without confirm")
	}))
	defer srv.Close()

	s := newTestServer(t, srv, "")
	result, _ := s.supportUpdateCore(context.Background(), &supportUpdateInput{Code: "Ab12Cd34", Priority: "HIGH"})
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	if !resp.Success || !strings.Contains(resp.Message, "confirm=true") {
		t.Fatalf("expected a preview, got %q", resp.Message)
	}
}

// TestSupportPostCore_DiscardsUploadsWhenThePostFails is the support twin of
// the org-side cleanup: uploads staged for a reply that then fails are bound
// to nothing and are deleted rather than left for the sweep.
func TestSupportPostCore_DiscardsUploadsWhenThePostFails(t *testing.T) {
	var deleted []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodDelete:
			deleted = append(deleted, r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:])
			ticketJSON(t, w, 200, map[string]string{"message": "deleted"})
		case strings.HasSuffix(r.URL.Path, "/attachments"):
			ticketJSON(t, w, 201, models.TicketAttachment{UUID: "99999999-9999-9999-9999-999999999999"})
		default:
			ticketJSON(t, w, 409, map[string]string{"error": "ticket is closed", "code": "CONFLICT"})
		}
	}))
	defer srv.Close()

	path := filepath.Join(t.TempDir(), "trace.log")
	if err := os.WriteFile(path, []byte("trace"), 0o600); err != nil {
		t.Fatal(err)
	}

	s := newTestServer(t, srv, "")
	result, _ := s.supportPostCore(context.Background(), &supportPostInput{
		Code: "Ab12Cd34", Message: "looking into it", AttachPaths: []string{path},
	}, models.TicketKindResponse)

	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	if resp.Success {
		t.Fatal("expected the reply to fail")
	}
	if len(deleted) != 1 || deleted[0] != "99999999-9999-9999-9999-999999999999" {
		t.Errorf("expected the staged upload to be discarded, got %v", deleted)
	}
}

// TestSupportAttachPaths_CombinedCapCheckedBeforeUpload is the support twin
// of the org-side guard, sharing one rule in the service layer.
func TestSupportAttachPaths_CombinedCapCheckedBeforeUpload(t *testing.T) {
	uploads := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uploads++
		ticketJSON(t, w, 201, models.TicketAttachment{UUID: "cccccccc-0000-0000-0000-000000000000"})
	}))
	defer srv.Close()

	dir := t.TempDir()
	paths := make([]string, 8)
	for i := range paths {
		p := filepath.Join(dir, fmt.Sprintf("f%d.txt", i))
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		paths[i] = p
	}
	existing := make([]string, 5)
	for i := range existing {
		existing[i] = fmt.Sprintf("dddddddd-0000-0000-0000-00000000000%d", i)
	}

	s := newTestServer(t, srv, "")
	_, _, errResult := s.uploadSupportAttachPaths("Ab12Cd34", existing, paths)
	if errResult == nil {
		t.Fatal("expected 5 uuids + 8 paths to exceed the cap")
	}
	if uploads != 0 {
		t.Errorf("nothing may be uploaded once the call is over the cap; got %d uploads", uploads)
	}
}

// TestSupportToolDescriptions_ReadToolsDoNotClaimReadsAreBlocked keeps the
// two responder notes on the right tools: a read tool carrying the write
// note reads as if a read-only token could not read at all.
func TestSupportToolDescriptions_ReadToolsDoNotClaimReadsAreBlocked(t *testing.T) {
	if !strings.Contains(responderReadNote, "may read") {
		t.Errorf("the read note must say reads are allowed: %q", responderReadNote)
	}
	if strings.Contains(responderReadNote, "cannot write") || strings.Contains(responderReadNote, "are refused") {
		t.Errorf("the read note must not describe write refusals: %q", responderReadNote)
	}
	if !strings.Contains(responderWriteNote, "read-write token") {
		t.Errorf("the write note must name what writing needs: %q", responderWriteNote)
	}
}

// TestContextForCleanup_IndependentOfAFailedCall is the point of the
// separate cleanup context: the most likely reason a write failed is that
// its own deadline expired, and a cleanup inherited from that context would
// do nothing precisely when it is needed.
func TestContextForCleanup_IndependentOfAFailedCall(t *testing.T) {
	failed, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	<-failed.Done()

	cleanupCtx, cleanupCancel := contextForCleanup()
	defer cleanupCancel()
	if cleanupCtx.Err() != nil {
		t.Fatalf("the cleanup context must be usable after the call it follows died: %v", cleanupCtx.Err())
	}
	if _, ok := cleanupCtx.Deadline(); !ok {
		t.Error("the cleanup context must still be bounded")
	}
}
