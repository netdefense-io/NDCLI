package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/netdefense-io/NDCLI/internal/models"
)

func sampleTicket() *models.Ticket {
	now := models.FlexibleTime{Time: time.Date(2026, 9, 8, 10, 0, 0, 0, time.UTC)}
	return &models.Ticket{
		Code: "Ab12Cd34", Subject: "VPN tunnel drops nightly",
		Status: "PENDING", Priority: "HIGH", Category: "BUG",
		CreatedBy:      &models.TicketActor{Email: "bob@acme.com", Name: "Bob Smith"},
		LastActivityAt: now, CreatedAt: now,
		Participants: []models.TicketParticipant{{Email: "bob@acme.com", IsCreator: true, Enabled: true, AddedAt: now}},
		Devices:      []models.TicketDeviceRef{{UUID: "d-1", Name: "fw-branch-01"}},
		Organization: &models.TicketOrganizationRef{Name: "acme"},
	}
}

func sampleThread() []models.TicketInteraction {
	now := models.FlexibleTime{Time: time.Date(2026, 9, 8, 10, 5, 0, 0, time.UTC)}
	return []models.TicketInteraction{
		{UUID: "i-1", Kind: "RESPONSE", Body: "The tunnel drops at 02:00.", CreatedAt: now,
			Author: &models.TicketAuthor{Kind: "USER", Email: "bob@acme.com", Name: "Bob Smith"}},
		{UUID: "i-2", Kind: "INTERNAL_NOTE", Body: "Checked the peer config.", CreatedAt: now,
			Author: &models.TicketAuthor{Kind: "USER", Name: "Ann"},
			Attachments: []models.TicketAttachment{{
				UUID: "a-1", Filename: "capture.pcap", ContentType: "application/vnd.tcpdump.pcap", SizeBytes: 2048,
			}}},
	}
}

// TestTicketFormatters_AllRenderersProduceOutput is the smoke test for the
// four renderers: every ticket method must run without error and put the
// substantive fields somewhere in the output.
func TestTicketFormatters_AllRenderersProduceOutput(t *testing.T) {
	ticket := sampleTicket()
	thread := sampleThread()
	att := &models.TicketAttachment{UUID: "a-1", Filename: "capture.pcap", ContentType: "application/vnd.tcpdump.pcap", SizeBytes: 2048, SHA256: "deadbeef"}
	dl := &models.TicketDownloadResult{Path: "/tmp/capture.pcap", Filename: "capture.pcap", Size: 2048, SHA256: "deadbeef"}

	for _, tc := range []struct {
		name string
		make func(w *bytes.Buffer) Formatter
	}{
		{"simple", func(w *bytes.Buffer) Formatter { return &SimpleFormatter{BaseFormatter: BaseFormatter{Writer: w}} }},
		{"detailed", func(w *bytes.Buffer) Formatter { return &DetailedFormatter{BaseFormatter: BaseFormatter{Writer: w}} }},
		{"table", func(w *bytes.Buffer) Formatter { return &TableFormatter{BaseFormatter: BaseFormatter{Writer: w}} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			f := tc.make(&buf)
			for _, call := range []func() error{
				func() error { return f.FormatTicketList([]models.Ticket{*ticket}, 1, true) },
				func() error { return f.FormatTicketDetail(ticket, thread, len(thread)) },
				func() error { return f.FormatTicketThread(thread, len(thread)) },
				func() error { return f.FormatTicketAttachment(att) },
				func() error { return f.FormatTicketDownload(dl) },
				func() error { return f.FormatTicketParticipants(ticket.Participants) },
			} {
				if err := call(); err != nil {
					t.Fatalf("formatter returned error: %v", err)
				}
			}
			out := buf.String()
			// The table renderer writes its grid to stdout (repo-wide
			// behaviour), so only the non-grid parts land in buf.
			if !strings.Contains(out, "Ab12Cd34") {
				t.Errorf("%s output is missing the ticket code:\n%s", tc.name, out)
			}
			if !strings.Contains(out, "internal note") {
				t.Errorf("%s output must label internal notes:\n%s", tc.name, out)
			}
			// Bodies are printed as-is: no escaping, no markup.
			if !strings.Contains(out, "The tunnel drops at 02:00.") {
				t.Errorf("%s output is missing a message body:\n%s", tc.name, out)
			}
		})
	}
}

func TestTicketFormatters_JSONRoundTrips(t *testing.T) {
	var buf bytes.Buffer
	f := &JSONFormatter{BaseFormatter: BaseFormatter{Writer: &buf}, Indent: true}
	if err := f.FormatTicketDetail(sampleTicket(), sampleThread(), 2); err != nil {
		t.Fatalf("FormatTicketDetail: %v", err)
	}
	var out struct {
		Ticket        models.Ticket              `json:"ticket"`
		Messages      []models.TicketInteraction `json:"messages"`
		MessagesTotal int                        `json:"messages_total"`
	}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("JSON did not round-trip: %v\n%s", err, buf.String())
	}
	if out.Ticket.Code != "Ab12Cd34" || len(out.Messages) != 2 || out.MessagesTotal != 2 {
		t.Errorf("unexpected round-trip result: %+v", out)
	}
}

// TestTicketFormatters_EmptyListsRenderAsArrays keeps `-f json` machine-
// friendly: an empty page is [], never null.
func TestTicketFormatters_EmptyListsRenderAsArrays(t *testing.T) {
	var buf bytes.Buffer
	f := &JSONFormatter{BaseFormatter: BaseFormatter{Writer: &buf}}
	if err := f.FormatTicketList(nil, 0, false); err != nil {
		t.Fatalf("FormatTicketList: %v", err)
	}
	if !strings.Contains(buf.String(), `"tickets":[]`) && !strings.Contains(buf.String(), `"tickets": []`) {
		t.Errorf("empty ticket list should render as []: %s", buf.String())
	}
}

func TestTicketAuthorDisplayName(t *testing.T) {
	cases := map[string]*models.TicketAuthor{
		"Bob Smith":          {Kind: "USER", Name: "Bob Smith", Email: "bob@acme.com"},
		"bob@acme.com":       {Kind: "USER", Email: "bob@acme.com"},
		"NetDefense Support": {Kind: "SUPPORT"},
	}
	for want, author := range cases {
		if got := author.DisplayName(); got != want {
			t.Errorf("DisplayName() = %q, want %q", got, want)
		}
	}
	var missing *models.TicketAuthor
	if got := missing.DisplayName(); got != "(deleted account)" {
		t.Errorf("nil author = %q", got)
	}
}

// TestTicketWriteConfirmations_ProduceJSON is the regression for commands
// that only change state: close, reopen, reply, note and attachment delete
// used to print a success line straight to stdout and return, leaving
// `-f json` with nothing to parse.
func TestTicketWriteConfirmations_ProduceJSON(t *testing.T) {
	ticket := sampleTicket()
	ticket.Status = "CLOSED"
	msg := &models.TicketInteraction{
		UUID: "i-9", Kind: "RESPONSE", Body: "on it", TicketStatus: "OPEN",
		CreatedAt: models.FlexibleTime{Time: time.Now()},
	}

	var buf bytes.Buffer
	f := &JSONFormatter{BaseFormatter: BaseFormatter{Writer: &buf}}

	if err := f.FormatTicketStatusChange(ticket); err != nil {
		t.Fatalf("FormatTicketStatusChange: %v", err)
	}
	var back models.Ticket
	if err := json.Unmarshal(buf.Bytes(), &back); err != nil || back.Status != "CLOSED" {
		t.Errorf("status change did not round-trip: %+v (%v)", back, err)
	}

	buf.Reset()
	if err := f.FormatTicketMessagePosted("Ab12Cd34", msg); err != nil {
		t.Fatalf("FormatTicketMessagePosted: %v", err)
	}
	var posted struct {
		TicketCode   string `json:"ticket_code"`
		TicketStatus string `json:"ticket_status"`
	}
	if err := json.Unmarshal(buf.Bytes(), &posted); err != nil {
		t.Fatalf("posted message did not round-trip: %v", err)
	}
	// A reply to a closed ticket reopens it; the caller must be able to read
	// the status the server returned.
	if posted.TicketCode != "Ab12Cd34" || posted.TicketStatus != "OPEN" {
		t.Errorf("posted = %+v", posted)
	}

	buf.Reset()
	if err := f.FormatTicketAttachmentDeleted("a-1"); err != nil {
		t.Fatalf("FormatTicketAttachmentDeleted: %v", err)
	}
	var deleted struct {
		UUID    string `json:"uuid"`
		Deleted bool   `json:"deleted"`
	}
	if err := json.Unmarshal(buf.Bytes(), &deleted); err != nil || !deleted.Deleted {
		t.Errorf("deletion did not round-trip: %+v (%v)", deleted, err)
	}
}

// TestTicketWriteConfirmations_RenderInEveryFormat keeps the human formats
// from going silent on the same commands.
func TestTicketWriteConfirmations_RenderInEveryFormat(t *testing.T) {
	ticket := sampleTicket()
	ticket.Status = "CLOSED"
	msg := &models.TicketInteraction{UUID: "i-9", Kind: "INTERNAL_NOTE", Body: "b",
		CreatedAt: models.FlexibleTime{Time: time.Now()}}

	for name, make := range map[string]func(*bytes.Buffer) Formatter{
		"simple":   func(w *bytes.Buffer) Formatter { return &SimpleFormatter{BaseFormatter: BaseFormatter{Writer: w}} },
		"detailed": func(w *bytes.Buffer) Formatter { return &DetailedFormatter{BaseFormatter: BaseFormatter{Writer: w}} },
		"table":    func(w *bytes.Buffer) Formatter { return &TableFormatter{BaseFormatter: BaseFormatter{Writer: w}} },
	} {
		var buf bytes.Buffer
		f := make(&buf)
		if err := f.FormatTicketStatusChange(ticket); err != nil {
			t.Fatalf("%s status: %v", name, err)
		}
		if err := f.FormatTicketMessagePosted("Ab12Cd34", msg); err != nil {
			t.Fatalf("%s posted: %v", name, err)
		}
		if err := f.FormatTicketAttachmentDeleted("a-1"); err != nil {
			t.Fatalf("%s deleted: %v", name, err)
		}
		out := buf.String()
		if !strings.Contains(out, "CLOSED") || !strings.Contains(out, "Internal note") {
			t.Errorf("%s output is missing the outcome:\n%s", name, out)
		}
	}
}

// TestPaginationLine_SilentInJSON guards the trailing page line that every
// list command appends after the formatter: in JSON mode it would land
// after the closing brace and make the document unparseable. This is
// repo-wide behaviour, not ticket-specific.
func TestPaginationLine_SilentInJSON(t *testing.T) {
	original := selectedFormat
	defer func() { selectedFormat = original }()

	_ = GetFormatter("json")
	if got := paginationLine(1, 100, 10); got != "" {
		t.Errorf("json mode must produce no pagination line, got %q", got)
	}

	_ = GetFormatter("table")
	if got := paginationLine(1, 100, 10); !strings.Contains(got, "Page 1 of 10") {
		t.Errorf("table mode should still produce the line, got %q", got)
	}
	// A single page has nothing to page through in any format.
	if got := paginationLine(1, 5, 10); got != "" {
		t.Errorf("a single page needs no line, got %q", got)
	}
}

func TestSupportProfileFormatters(t *testing.T) {
	profile := &models.SupportResponderProfile{
		DisplayName: "Dana Reed", Email: "dana@netdefense.io",
		Status: "ENABLED", Principal: "pat", CanWrite: false,
	}
	for name, make := range map[string]func(*bytes.Buffer) Formatter{
		"simple":   func(w *bytes.Buffer) Formatter { return &SimpleFormatter{BaseFormatter: BaseFormatter{Writer: w}} },
		"detailed": func(w *bytes.Buffer) Formatter { return &DetailedFormatter{BaseFormatter: BaseFormatter{Writer: w}} },
		"table":    func(w *bytes.Buffer) Formatter { return &TableFormatter{BaseFormatter: BaseFormatter{Writer: w}} },
	} {
		var buf bytes.Buffer
		if err := make(&buf).FormatSupportProfile(profile); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		out := buf.String()
		if !strings.Contains(out, "dana@netdefense.io") {
			t.Errorf("%s output is missing the responder email:\n%s", name, out)
		}
		// A read-only principal must be visibly read-only.
		if !strings.Contains(out, "read-only") {
			t.Errorf("%s output must say the principal cannot write:\n%s", name, out)
		}
	}

	var buf bytes.Buffer
	if err := (&JSONFormatter{BaseFormatter: BaseFormatter{Writer: &buf}}).FormatSupportProfile(profile); err != nil {
		t.Fatalf("json: %v", err)
	}
	var back models.SupportResponderProfile
	if err := json.Unmarshal(buf.Bytes(), &back); err != nil || back.Email != profile.Email || back.CanWrite {
		t.Errorf("JSON did not round-trip: %+v (%v)", back, err)
	}
}

// TestSupportTicketList_ShowsOrgColumn keeps the support list distinguishable
// from the org list: the owning organization is the first thing a responder
// needs.
func TestSupportTicketList_ShowsOrgColumn(t *testing.T) {
	var buf bytes.Buffer
	f := &SimpleFormatter{BaseFormatter: BaseFormatter{Writer: &buf}}
	if err := f.FormatTicketList([]models.Ticket{*sampleTicket()}, 1, true); err != nil {
		t.Fatalf("FormatTicketList: %v", err)
	}
	if !strings.Contains(buf.String(), "acme") {
		t.Errorf("support list must show the organization:\n%s", buf.String())
	}
}

// TestTicketDownloadJSON_FieldNamesArePinned exists because this shape is a
// contract: `ndcli ticket download -f json` and its support twin marshal
// TicketDownloadResult straight out, so a tag change here is a change to
// what scripts parse. size_bytes matches the field name the API uses on an
// attachment; the two should not drift apart, and neither should silently
// change again.
func TestTicketDownloadJSON_FieldNamesArePinned(t *testing.T) {
	var buf bytes.Buffer
	f := &JSONFormatter{BaseFormatter: BaseFormatter{Writer: &buf}}
	err := f.FormatTicketDownload(&models.TicketDownloadResult{
		Path: "/tmp/capture.pcap", Filename: "capture.pcap",
		ContentType: "application/vnd.tcpdump.pcap", Size: 2048, SHA256: "deadbeef",
	})
	if err != nil {
		t.Fatalf("FormatTicketDownload: %v", err)
	}

	var got map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	for _, key := range []string{"path", "filename", "content_type", "size_bytes", "sha256"} {
		if _, ok := got[key]; !ok {
			t.Errorf("missing field %q in %v", key, got)
		}
	}
	if _, ok := got["size"]; ok {
		t.Error(`"size" was renamed to "size_bytes"; both must not be present`)
	}
	if n, _ := got["size_bytes"].(float64); int64(n) != 2048 {
		t.Errorf("size_bytes = %v, want 2048", got["size_bytes"])
	}
}
