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
)

func ticketJSON(t *testing.T, w http.ResponseWriter, status int, v interface{}) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func TestHandleTicketList_RequiresAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("no request should reach the API without auth")
	}))
	defer srv.Close()

	s := newTestServer(t, srv, "acme")
	result, err := s.handleTicketList(context.Background(), &mcp.CallToolRequest{
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

func TestTicketListCore_FiltersAndPagination(t *testing.T) {
	var query string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		ticketJSON(t, w, 200, models.TicketListResponse{
			Items: []models.Ticket{{Code: "Ab12Cd34", Subject: "vpn drops"}},
			Total: 1, Page: 1, PerPage: 50, Pages: 1,
		})
	}))
	defer srv.Close()

	s := newTestServer(t, srv, "acme")
	result, err := s.ticketListCore(context.Background(), &ticketListInput{Status: "open", SortBy: "created_at"})
	if err != nil {
		t.Fatalf("core: %v", err)
	}
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	if !resp.Success {
		t.Fatalf("expected success, got %+v", resp.Error)
	}
	// Enum input is case-insensitive and upper-cased on the wire; a bare
	// sort field gains the default direction.
	if !strings.Contains(query, "status=OPEN") || !strings.Contains(query, "sort_by=created_at%3Adesc") {
		t.Errorf("query = %q", query)
	}
	if resp.Pagination == nil || resp.Pagination.Total != 1 {
		t.Errorf("pagination = %+v", resp.Pagination)
	}
}

func TestTicketListCore_RejectsUnknownEnum(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("no request should be made for an invalid enum")
	}))
	defer srv.Close()

	s := newTestServer(t, srv, "acme")
	result, _ := s.ticketListCore(context.Background(), &ticketListInput{Status: "ONFIRE"})
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	if resp.Success {
		t.Fatal("expected a validation failure")
	}
}

// TestTicketCreateCore_AttachPathsUploadFirst covers the upload-then-bind
// contract: attach_paths are uploaded and the returned uuids travel in the
// create body, so a model never has to make two calls.
func TestTicketCreateCore_AttachPathsUploadFirst(t *testing.T) {
	var createBody map[string]json.RawMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/ticket-attachments") {
			ticketJSON(t, w, 201, models.TicketAttachment{UUID: "11111111-1111-1111-1111-111111111111"})
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&createBody)
		ticketJSON(t, w, 201, models.Ticket{Code: "Ab12Cd34", Status: "OPEN"})
	}))
	defer srv.Close()

	path := filepath.Join(t.TempDir(), "log.txt")
	if err := os.WriteFile(path, []byte("log line"), 0o600); err != nil {
		t.Fatal(err)
	}

	s := newTestServer(t, srv, "acme")
	result, err := s.ticketCreateCore(context.Background(), &ticketCreateInput{
		Subject: "vpn drops", Message: "nightly", AttachPaths: []string{path},
	})
	if err != nil {
		t.Fatalf("core: %v", err)
	}
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	if !resp.Success {
		t.Fatalf("expected success, got %+v", resp.Error)
	}
	if got := string(createBody["attachments"]); got != `["11111111-1111-1111-1111-111111111111"]` {
		t.Errorf("attachments = %s", got)
	}
}

func TestTicketParticipantRemoveCore_PreviewWithoutConfirm(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("no request should be made without confirm")
	}))
	defer srv.Close()

	s := newTestServer(t, srv, "acme")
	result, _ := s.ticketParticipantRemoveCore(context.Background(), &ticketParticipantInput{
		Code: "Ab12Cd34", Email: "bob@acme.com",
	})
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	if !resp.Success || !strings.Contains(resp.Message, "confirm=true") {
		t.Fatalf("expected a preview, got %+v (%q)", resp.Data, resp.Message)
	}
}

func TestTicketAttachmentDeleteCore_PreviewWithoutConfirm(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("no request should be made without confirm")
	}))
	defer srv.Close()

	s := newTestServer(t, srv, "acme")
	result, _ := s.ticketAttachmentDeleteCore(context.Background(), &ticketAttachmentDeleteInput{
		UUID: "11111111-1111-1111-1111-111111111111",
	})
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	if !resp.Success || !strings.Contains(resp.Message, "confirm=true") {
		t.Fatalf("expected a preview, got %q", resp.Message)
	}
}

// ticketDownloadTestServer serves a one-attachment thread plus the bytes.
func ticketDownloadTestServer(t *testing.T, content []byte) *httptest.Server {
	t.Helper()
	sum := sha256.Sum256(content)
	digest := hex.EncodeToString(sum[:])
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/interactions") {
			ticketJSON(t, w, 200, models.TicketInteractionListResponse{
				Items: []models.TicketInteraction{{
					UUID: "i-1", Kind: "RESPONSE", Body: "attached",
					Attachments: []models.TicketAttachment{{
						UUID:     "22222222-2222-2222-2222-222222222222",
						Filename: "capture.txt", ContentType: "text/plain",
						SizeBytes: int64(len(content)), SHA256: digest,
						DownloadPath: "/api/v1/organizations/acme/tickets/Ab12Cd34/attachments/22222222-2222-2222-2222-222222222222",
					}},
				}},
				Total: 1, Page: 1, PerPage: 100, Pages: 1,
			})
			return
		}
		_, _ = w.Write(content)
	}))
}

func TestTicketAttachmentDownloadCore_PathAndOverwrite(t *testing.T) {
	content := []byte("captured bytes")
	srv := ticketDownloadTestServer(t, content)
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "saved.txt")
	s := newTestServer(t, srv, "acme")
	in := &ticketDownloadInput{Code: "Ab12Cd34", UUID: "22222222-2222-2222-2222-222222222222", Path: dest}

	result, err := s.ticketAttachmentDownloadCore(context.Background(), in)
	if err != nil {
		t.Fatalf("core: %v", err)
	}
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	if !resp.Success {
		t.Fatalf("expected success, got %+v", resp.Error)
	}
	if got, _ := os.ReadFile(dest); string(got) != string(content) {
		t.Errorf("file = %q", got)
	}

	// A second call without overwrite must refuse rather than replace.
	result, _ = s.ticketAttachmentDownloadCore(context.Background(), in)
	resp = ToolResponse{}
	decodeToolResult(t, result, &resp)
	if resp.Success {
		t.Fatal("expected the second download to refuse to overwrite")
	}

	in.Overwrite = true
	result, _ = s.ticketAttachmentDownloadCore(context.Background(), in)
	resp = ToolResponse{}
	decodeToolResult(t, result, &resp)
	if !resp.Success {
		t.Fatalf("expected overwrite=true to succeed, got %+v", resp.Error)
	}
}

func TestTicketPostMessageText(t *testing.T) {
	if got := ticketPostMessageText("Ab12Cd34", models.TicketKindResponse, "OPEN"); !strings.Contains(got, "now OPEN") {
		t.Errorf("reply text should carry the returned status: %q", got)
	}
	if got := ticketPostMessageText("Ab12Cd34", models.TicketKindInternalNote, ""); !strings.HasPrefix(got, "Internal note") {
		t.Errorf("note text = %q", got)
	}
}

// TestTicketAttachmentTransfers_NotBoundByJSONTimeout is the regression for
// a silent 30 s cap on every attachment: a context deadline is the minimum
// of the whole chain, so handing a raw transfer the JSON budget as its
// parent wins over the api package's own per-transfer deadline and caps a
// 25 MiB upload at 30 s. The transfer handlers must build their context
// without that budget.
//
// The test shortens apiTimeout and makes the server slower than it. A JSON
// tool must fail; upload and download must not.
func TestTicketAttachmentTransfers_NotBoundByJSONTimeout(t *testing.T) {
	const serverDelay = 120 * time.Millisecond

	content := []byte("payload")
	sum := sha256.Sum256(content)
	digest := hex.EncodeToString(sum[:])

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/ticket-attachments"):
			time.Sleep(serverDelay)
			ticketJSON(t, w, 201, models.TicketAttachment{UUID: "44444444-4444-4444-4444-444444444444"})
		case strings.HasSuffix(r.URL.Path, "/interactions"):
			ticketJSON(t, w, 200, models.TicketInteractionListResponse{
				Items: []models.TicketInteraction{{
					UUID: "i-1", Kind: "RESPONSE", Body: "attached",
					Attachments: []models.TicketAttachment{{
						UUID:     "44444444-4444-4444-4444-444444444444",
						Filename: "slow.bin", SizeBytes: int64(len(content)), SHA256: digest,
						DownloadPath: "/api/v1/organizations/acme/tickets/Ab12Cd34/attachments/44444444-4444-4444-4444-444444444444",
					}},
				}},
				Total: 1, Page: 1, PerPage: 100, Pages: 1,
			})
		case strings.HasSuffix(r.URL.Path, "/attachments/44444444-4444-4444-4444-444444444444"):
			time.Sleep(serverDelay)
			_, _ = w.Write(content)
		default:
			time.Sleep(serverDelay)
			ticketJSON(t, w, 200, models.Ticket{Code: "Ab12Cd34", Status: "OPEN"})
		}
	}))
	defer srv.Close()

	original := apiTimeout
	apiTimeout = 30 * time.Millisecond // shorter than the server's delay
	defer func() { apiTimeout = original }()

	s := newTestServer(t, srv, "acme")
	dir := t.TempDir()
	path := filepath.Join(dir, "slow.bin")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	// Control: a JSON tool is bound by apiTimeout and must give up.
	result, _ := s.ticketGetCore(context.Background(), &ticketGetInput{Code: "Ab12Cd34"})
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	if resp.Success {
		t.Fatal("a JSON tool should be bound by apiTimeout; the control case did not fail")
	}

	// Upload: must outlive the JSON budget.
	result, _ = s.ticketAttachmentUploadCore(context.Background(), &ticketAttachmentUploadInput{Path: path})
	resp = ToolResponse{}
	decodeToolResult(t, result, &resp)
	if !resp.Success {
		t.Errorf("upload must not be bound by apiTimeout: %+v", resp.Error)
	}

	// Download: same, and it still verifies the digest.
	result, _ = s.ticketAttachmentDownloadCore(context.Background(), &ticketDownloadInput{
		Code: "Ab12Cd34", UUID: "44444444-4444-4444-4444-444444444444",
		Path: filepath.Join(dir, "out.bin"),
	})
	resp = ToolResponse{}
	decodeToolResult(t, result, &resp)
	if !resp.Success {
		t.Errorf("download must not be bound by apiTimeout: %+v", resp.Error)
	}
}

// TestTicketUpdateCore_PreviewWithoutConfirm keeps the update tool aligned
// with every other "update a named resource" tool in this server. It matters
// more here than most: devices is a wholesale replacement, so one
// uncontested call with an empty list would clear every related device.
func TestTicketUpdateCore_PreviewWithoutConfirm(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("no request should be made without confirm")
	}))
	defer srv.Close()

	s := newTestServer(t, srv, "acme")
	empty := []string{}
	result, _ := s.ticketUpdateCore(context.Background(), &ticketUpdateInput{Code: "Ab12Cd34", Devices: &empty})
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	if !resp.Success || !strings.Contains(resp.Message, "confirm=true") {
		t.Fatalf("expected a preview, got %q", resp.Message)
	}
}

// TestTicketCreateCore_DiscardsUploadsWhenThePostFails covers the other
// half of the attach_paths contract: the uploads succeed, the write they
// were staged for does not, and the uuids are now bound to nothing. They
// are deleted rather than left for the 24-hour sweep.
func TestTicketCreateCore_DiscardsUploadsWhenThePostFails(t *testing.T) {
	var deleted []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodDelete:
			deleted = append(deleted, r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:])
			ticketJSON(t, w, 200, map[string]string{"message": "deleted"})
		case strings.HasSuffix(r.URL.Path, "/ticket-attachments"):
			ticketJSON(t, w, 201, models.TicketAttachment{UUID: "66666666-6666-6666-6666-666666666666"})
		default:
			ticketJSON(t, w, 400, map[string]string{"error": "subject too long", "code": "VALIDATION_ERROR"})
		}
	}))
	defer srv.Close()

	path := filepath.Join(t.TempDir(), "log.txt")
	if err := os.WriteFile(path, []byte("log"), 0o600); err != nil {
		t.Fatal(err)
	}

	s := newTestServer(t, srv, "acme")
	result, _ := s.ticketCreateCore(context.Background(), &ticketCreateInput{
		Subject: "s", Message: "m", AttachPaths: []string{path},
	})
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	if resp.Success {
		t.Fatal("expected the create to fail")
	}
	if len(deleted) != 1 || deleted[0] != "66666666-6666-6666-6666-666666666666" {
		t.Errorf("expected the staged upload to be discarded, got %v", deleted)
	}
}

// TestUploadAttachPaths_LeavesCallerAttachmentsAlone makes sure only the
// uuids this call created are reported as discardable — an uuid the caller
// passed in was staged by someone else and is not ours to delete.
func TestUploadAttachPaths_LeavesCallerAttachmentsAlone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ticketJSON(t, w, 201, models.TicketAttachment{UUID: "77777777-7777-7777-7777-777777777777"})
	}))
	defer srv.Close()

	path := filepath.Join(t.TempDir(), "log.txt")
	if err := os.WriteFile(path, []byte("log"), 0o600); err != nil {
		t.Fatal(err)
	}

	s := newTestServer(t, srv, "acme")
	existing := []string{"88888888-8888-8888-8888-888888888888"}
	all, uploaded, errResult := s.uploadAttachPaths("acme", existing, []string{path})
	if errResult != nil {
		t.Fatal("unexpected error result")
	}
	if len(all) != 2 {
		t.Errorf("all = %v, want both the caller's uuid and the new one", all)
	}
	if len(uploaded) != 1 || uploaded[0] != "77777777-7777-7777-7777-777777777777" {
		t.Errorf("uploaded = %v, want only the uuid this call created", uploaded)
	}
	if len(existing) != 1 {
		t.Errorf("the caller's attachment slice was mutated: %v", existing)
	}
}

// TestTicketAttachPaths_CombinedCapCheckedBeforeUpload is the MCP twin of
// the CLI guard: the cap counts uuids the caller already holds together
// with the files it wants uploaded, and the check has to land before the
// first byte moves or an over-limit call uploads files it then abandons.
func TestTicketAttachPaths_CombinedCapCheckedBeforeUpload(t *testing.T) {
	uploads := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uploads++
		ticketJSON(t, w, 201, models.TicketAttachment{UUID: "aaaaaaaa-0000-0000-0000-000000000000"})
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
		existing[i] = fmt.Sprintf("bbbbbbbb-0000-0000-0000-00000000000%d", i)
	}

	s := newTestServer(t, srv, "acme")
	_, _, errResult := s.uploadAttachPaths("acme", existing, paths)
	if errResult == nil {
		t.Fatal("expected 5 uuids + 8 paths to exceed the cap")
	}
	var resp ToolResponse
	decodeToolResult(t, errResult, &resp)
	if resp.Success || !strings.Contains(resp.Error.Message, "at most 10 attachments") {
		t.Errorf("error = %+v", resp.Error)
	}
	if uploads != 0 {
		t.Errorf("nothing may be uploaded once the call is over the cap; got %d uploads", uploads)
	}
}

// TestTicketAttachmentDownloadCore_ReturnsStructuredData keeps the download
// result machine-readable. The verified sha256 in particular has to be a
// field: a caller that wants to record what it received should not have to
// parse it back out of an English sentence.
func TestTicketAttachmentDownloadCore_ReturnsStructuredData(t *testing.T) {
	content := []byte("captured bytes")
	srv := ticketDownloadTestServer(t, content)
	defer srv.Close()

	sum := sha256.Sum256(content)
	dest := filepath.Join(t.TempDir(), "saved.txt")

	s := newTestServer(t, srv, "acme")
	result, err := s.ticketAttachmentDownloadCore(context.Background(), &ticketDownloadInput{
		Code: "Ab12Cd34", UUID: "22222222-2222-2222-2222-222222222222", Path: dest,
	})
	if err != nil {
		t.Fatalf("core: %v", err)
	}

	var resp struct {
		Success bool `json:"success"`
		Data    struct {
			Path      string `json:"path"`
			Filename  string `json:"filename"`
			SizeBytes int64  `json:"size_bytes"`
			SHA256    string `json:"sha256"`
		} `json:"data"`
		Message string `json:"message"`
	}
	decodeToolResult(t, result, &resp)
	if !resp.Success {
		t.Fatalf("expected success, got %s", resp.Message)
	}
	if resp.Data.Path != dest {
		t.Errorf("path = %q, want %q", resp.Data.Path, dest)
	}
	if resp.Data.Filename != "capture.txt" {
		t.Errorf("filename = %q", resp.Data.Filename)
	}
	if resp.Data.SizeBytes != int64(len(content)) {
		t.Errorf("size_bytes = %d, want %d", resp.Data.SizeBytes, len(content))
	}
	if resp.Data.SHA256 != hex.EncodeToString(sum[:]) {
		t.Errorf("sha256 = %q, want the verified digest", resp.Data.SHA256)
	}
	// The path must not be the only place the caller can find these.
	if strings.Contains(resp.Message, dest) {
		t.Errorf("the message should stay short, got %q", resp.Message)
	}
}

// TestTicketDownloadCore_OverwriteRefusalNamesNoFlag guards the shared
// message: an MCP caller has an overwrite parameter, not a command-line
// flag, and being told to pass one is unactionable.
func TestTicketDownloadCore_OverwriteRefusalNamesNoFlag(t *testing.T) {
	srv := ticketDownloadTestServer(t, []byte("captured bytes"))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "saved.txt")
	if err := os.WriteFile(dest, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}

	s := newTestServer(t, srv, "acme")
	result, _ := s.ticketAttachmentDownloadCore(context.Background(), &ticketDownloadInput{
		Code: "Ab12Cd34", UUID: "22222222-2222-2222-2222-222222222222", Path: dest,
	})
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	if resp.Success {
		t.Fatal("expected the download to refuse to overwrite")
	}
	if strings.Contains(resp.Error.Message, "--overwrite") {
		t.Errorf("an MCP caller has no flags: %q", resp.Error.Message)
	}
	if !strings.Contains(resp.Error.Message, "set overwrite") {
		t.Errorf("the message should name the parameter: %q", resp.Error.Message)
	}
}

// TestTicketPostTools_UseMessageParameter pins the parity rule that a tool
// parameter mirrors its CLI flag: reply and note take --message, so the
// tools take message, not body.
func TestTicketPostTools_UseMessageParameter(t *testing.T) {
	var in ticketPostInput
	if err := json.Unmarshal([]byte(`{"code":"Ab12Cd34","message":"hello"}`), &in); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if in.Message != "hello" {
		t.Errorf("message did not bind: %+v", in)
	}
	// Nothing has shipped with `body`, so it is simply not a parameter and
	// binds nowhere.
	var legacy ticketPostInput
	if err := json.Unmarshal([]byte(`{"code":"Ab12Cd34","body":"hello"}`), &legacy); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if legacy.Message != "" {
		t.Errorf("body must not bind to the message field, got %q", legacy.Message)
	}
}
