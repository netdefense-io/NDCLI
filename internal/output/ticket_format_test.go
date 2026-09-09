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
// contract: `ndcli support download -f json` and its responder twin marshal
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

// assertBoxedFields checks that a detailed-format block is a real box: the
// first line is a titled top border carrying wantTitle, the last is a bottom
// border of the same visible width, and every line in between is a content
// line delimited by │ on both sides. Each name in wantFields must appear on
// one of those inner lines.
func assertBoxedFields(t *testing.T, out, wantTitle string, wantFields []string) {
	t.Helper()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) < 3 {
		t.Fatalf("expected a box of at least 3 lines, got:\n%s", out)
	}
	top, bottom := lines[0], lines[len(lines)-1]
	if !strings.HasPrefix(top, BoxTopLeft) || !strings.HasSuffix(top, BoxTopRight) {
		t.Errorf("first line is not a top border: %q", top)
	}
	if !strings.Contains(top, wantTitle) {
		t.Errorf("top border is missing the title %q: %q", wantTitle, top)
	}
	if !strings.HasPrefix(bottom, BoxBottomLeft) || !strings.HasSuffix(bottom, BoxBottomRight) {
		t.Errorf("last line is not a bottom border: %q", bottom)
	}
	if visibleLength(top) != visibleLength(bottom) {
		t.Errorf("borders disagree on width: top %d, bottom %d", visibleLength(top), visibleLength(bottom))
	}
	inner := lines[1 : len(lines)-1]
	if len(inner) == 0 {
		t.Fatalf("the box is empty — the fields were printed outside it:\n%s", out)
	}
	for _, line := range inner {
		if !strings.HasPrefix(line, BoxVertical) || !strings.HasSuffix(line, BoxVertical) {
			t.Errorf("line is not inside the box: %q", line)
		}
		if visibleLength(line) != visibleLength(top) {
			t.Errorf("content line width %d != border width %d: %q", visibleLength(line), visibleLength(top), line)
		}
	}
	body := strings.Join(inner, "\n")
	for _, field := range wantFields {
		if !strings.Contains(body, field) {
			t.Errorf("field %q is not inside the box:\n%s", field, out)
		}
	}
}

// TestDetailedTicketRenderers_FieldsAreInsideTheBox is the regression for
// the collapsed box: every detailed ticket/support renderer used to print
// the titled top border and the bottom border back to back and then print
// its fields underneath, so the box was an empty one-line rectangle holding
// nothing and the fields sat outside it entirely.
func TestDetailedTicketRenderers_FieldsAreInsideTheBox(t *testing.T) {
	ticket := sampleTicket()
	ticket.WebURL = "https://app.netdefense.io/support/tickets/Ab12Cd34"

	for _, tc := range []struct {
		name   string
		render func(f *DetailedFormatter) error
		title  string
		fields []string
		// trailing output the renderer adds after the box
		cut string
	}{
		{
			name:   "list",
			render: func(f *DetailedFormatter) error { return f.FormatTicketList([]models.Ticket{*ticket}, 1, true) },
			title:  "Ab12Cd34",
			fields: []string{"Subject", "VPN tunnel drops nightly", "Organization", "acme", "Status", "Priority", "Category", "Created By", "Last Activity"},
			cut:    "\nTotal:",
		},
		{
			name:   "detail",
			render: func(f *DetailedFormatter) error { return f.FormatTicketDetail(ticket, nil, 0) },
			title:  "Ticket Ab12Cd34",
			fields: []string{"Subject", "Status", "Priority", "Category", "Created By", "Created", "Last Activity", "Web", "Participant", "bob@acme.com", "Device", "fw-branch-01"},
		},
		{
			name: "attachment",
			render: func(f *DetailedFormatter) error {
				return f.FormatTicketAttachment(&models.TicketAttachment{
					UUID: "a-1", Filename: "capture.pcap",
					ContentType: "application/vnd.tcpdump.pcap", SizeBytes: 2048, SHA256: "deadbeef",
				})
			},
			title:  "Attachment",
			fields: []string{"UUID", "a-1", "Filename", "capture.pcap", "Content Type", "Size", "SHA-256", "deadbeef"},
		},
		{
			name: "support profile",
			render: func(f *DetailedFormatter) error {
				return f.FormatSupportProfile(&models.SupportResponderProfile{
					DisplayName: "Ann Responder", Email: "ann@netdefense.io",
					Status: "ENABLED", Principal: "LOGIN", CanWrite: true,
				})
			},
			title:  "Support Responder",
			fields: []string{"Display Name", "Ann Responder", "Email", "ann@netdefense.io", "Status", "Principal", "Access"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			f := &DetailedFormatter{BaseFormatter: BaseFormatter{Writer: &buf}}
			if err := tc.render(f); err != nil {
				t.Fatalf("render: %v", err)
			}
			out := buf.String()
			if tc.cut != "" {
				out = strings.SplitN(out, tc.cut, 2)[0]
			}
			assertBoxedFields(t, out, tc.title, tc.fields)
		})
	}
}

// TestDetailedTicketBox_ClampsToWidthAndWrapsLongValues pins the sizing
// rule. The box grows past its 66-column floor to fit the widest field, but
// never past the terminal width: growing wider than the window would wrap
// every border and look worse than the bug this replaced. A value too wide
// for the clamped box is wrapped onto continuation lines, never truncated.
func TestDetailedTicketBox_ClampsToWidthAndWrapsLongValues(t *testing.T) {
	const forced = 72
	forceBoxWidth(t, forced)

	// 255 'x' characters, the only 'x' anywhere in the sample ticket, so a
	// count over the whole output proves nothing was dropped in the wrap.
	subject := strings.Repeat("x", 255)
	ticket := sampleTicket()
	ticket.Subject = subject

	var buf bytes.Buffer
	f := &DetailedFormatter{BaseFormatter: BaseFormatter{Writer: &buf}}
	if err := f.FormatTicketDetail(ticket, nil, 0); err != nil {
		t.Fatalf("FormatTicketDetail: %v", err)
	}
	out := buf.String()

	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if w := visibleLength(line); w > forced {
			t.Errorf("line is %d columns, over the %d limit: %q", w, forced, line)
		}
	}
	if got := strings.Count(out, "x"); got != len(subject) {
		t.Errorf("subject text lost in the wrap: %d of %d characters survived", got, len(subject))
	}
	assertBoxedFields(t, out, "Ticket Ab12Cd34", []string{"Subject", "Status", "Device"})
}

// TestDetailedTicketBox_NarrowTerminalStaysInsideTheBorder is the regression
// for the wrap arithmetic at the narrow end. The first cut of the clamp
// widened a value back to the full content width when little room was left
// after the label, but still printed the label ahead of it, so the line ran
// past the right border by the width of the label — at a forced 24 columns a
// ticket box emitted lines of 39.
func TestDetailedTicketBox_NarrowTerminalStaysInsideTheBorder(t *testing.T) {
	const forced = 24
	forceBoxWidth(t, forced)

	ticket := sampleTicket()
	ticket.WebURL = "https://app.netdefense.io/support/tickets/Ab12Cd34"

	var buf bytes.Buffer
	f := &DetailedFormatter{BaseFormatter: BaseFormatter{Writer: &buf}}
	if err := f.FormatTicketDetail(ticket, nil, 0); err != nil {
		t.Fatalf("FormatTicketDetail: %v", err)
	}
	out := buf.String()

	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if w := visibleLength(line); w > forced {
			t.Errorf("line is %d columns, over the %d limit: %q", w, forced, line)
		}
	}
	assertBoxedFields(t, out, "Ticket", []string{"Subject", "Status", "Web"})
}

// TestDetailedTicketBox_LeadingWhitespaceIsNotDuplicated is the regression
// for the two-width wrap. wrapToWidth splits on fields, so the first chunk it
// returns is not a literal prefix of a value that starts with whitespace: the
// prefix trim was a no-op, the "not a prefix" guard did not fire, and the
// opening chunk was printed once on its own line and again at the head of the
// re-wrapped remainder.
func TestDetailedTicketBox_LeadingWhitespaceIsNotDuplicated(t *testing.T) {
	const forced = 24
	forceBoxWidth(t, forced)

	words := []string{"alpha", "beta", "gamma", "delta", "epsilon", "zeta", "eta", "theta"}
	ticket := sampleTicket()
	ticket.Subject = "   " + strings.Join(words, " ")

	var buf bytes.Buffer
	f := &DetailedFormatter{BaseFormatter: BaseFormatter{Writer: &buf}}
	if err := f.FormatTicketDetail(ticket, nil, 0); err != nil {
		t.Fatalf("FormatTicketDetail: %v", err)
	}
	out := buf.String()

	// Count whole tokens, not substrings: "eta" also sits inside "beta".
	seen := map[string]int{}
	for _, line := range strings.Split(out, "\n") {
		for _, tok := range strings.Fields(strings.Trim(line, BoxVertical+BoxHorizontal+BoxTopLeft+BoxTopRight+BoxBottomLeft+BoxBottomRight+" ")) {
			seen[tok]++
		}
	}
	for _, w := range words {
		if seen[w] != 1 {
			t.Errorf("word %q appears %d times, want exactly 1:\n%s", w, seen[w], out)
		}
	}
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if w := visibleLength(line); w > forced {
			t.Errorf("line is %d columns, over the %d limit: %q", w, forced, line)
		}
	}
}
