package output

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// TestDetailedFormatter_OverWidthFieldsDoNotPanic reproduces the
// strings.Repeat negative-count panic that used to occur when a
// server-supplied name/status/label exceeds the fixed box width used by
// the detailed formatter. Every case here would crash the process on the
// pre-fix code path; after the fix the call must return normally and the
// full (untruncated) value must still be present in the rendered output.
func TestDetailedFormatter_OverWidthFieldsDoNotPanic(t *testing.T) {
	longName := strings.Repeat("A", 60)
	longStatus := "SOME_UNEXPECTED_LONG_STATUS_VALUE_THAT_IS_VERY_LONG"

	t.Run("FormatDevices name", func(t *testing.T) {
		var buf bytes.Buffer
		f := &DetailedFormatter{BaseFormatter: BaseFormatter{Writer: &buf}}
		device := models.Device{Name: longName, Status: "ENABLED"}
		if err := f.FormatDevices([]models.Device{device}, 1, nil); err != nil {
			t.Fatalf("FormatDevices returned error: %v", err)
		}
		if buf.Len() == 0 {
			t.Fatal("expected non-empty output")
		}
		if !strings.Contains(buf.String(), longName) {
			t.Fatal("output must contain the full device name")
		}
	})

	t.Run("FormatDevices status", func(t *testing.T) {
		var buf bytes.Buffer
		f := &DetailedFormatter{BaseFormatter: BaseFormatter{Writer: &buf}}
		device := models.Device{Name: "dev1", Status: longStatus}
		if err := f.FormatDevices([]models.Device{device}, 1, nil); err != nil {
			t.Fatalf("FormatDevices returned error: %v", err)
		}
		if !strings.Contains(buf.String(), longStatus) {
			t.Fatal("output must contain the full status value")
		}
	})

	t.Run("FormatOrganization name", func(t *testing.T) {
		var buf bytes.Buffer
		f := &DetailedFormatter{BaseFormatter: BaseFormatter{Writer: &buf}}
		org := &models.Organization{Name: longName, Status: "ENABLED"}
		if err := f.FormatOrganization(org); err != nil {
			t.Fatalf("FormatOrganization returned error: %v", err)
		}
		if !strings.Contains(buf.String(), longName) {
			t.Fatal("output must contain the full organization name")
		}
	})

	t.Run("FormatOrganization status", func(t *testing.T) {
		var buf bytes.Buffer
		f := &DetailedFormatter{BaseFormatter: BaseFormatter{Writer: &buf}}
		org := &models.Organization{Name: "org1", Status: longStatus}
		if err := f.FormatOrganization(org); err != nil {
			t.Fatalf("FormatOrganization returned error: %v", err)
		}
		if !strings.Contains(buf.String(), longStatus) {
			t.Fatal("output must contain the full status value")
		}
	})

	t.Run("FormatTemplate name", func(t *testing.T) {
		var buf bytes.Buffer
		f := &DetailedFormatter{BaseFormatter: BaseFormatter{Writer: &buf}}
		tmpl := &models.Template{Name: longName, Position: "APPEND"}
		if err := f.FormatTemplate(tmpl); err != nil {
			t.Fatalf("FormatTemplate returned error: %v", err)
		}
		if !strings.Contains(buf.String(), longName) {
			t.Fatal("output must contain the full template name")
		}
	})

	t.Run("FormatAuthMe email", func(t *testing.T) {
		var buf bytes.Buffer
		f := &DetailedFormatter{BaseFormatter: BaseFormatter{Writer: &buf}}
		email := strings.Repeat("a", 55) + "@example.com"
		authMe := &models.AuthMe{Email: email, Status: "ENABLED"}
		if err := f.FormatAuthMe(authMe); err != nil {
			t.Fatalf("FormatAuthMe returned error: %v", err)
		}
		if !strings.Contains(buf.String(), email) {
			t.Fatal("output must contain the full email")
		}
	})

	t.Run("FormatDeviceBackupStatus device and org", func(t *testing.T) {
		var buf bytes.Buffer
		f := &DetailedFormatter{BaseFormatter: BaseFormatter{Writer: &buf}}
		status := &models.DeviceBackupStatus{
			DeviceName:   longName,
			Organization: strings.Repeat("B", 60),
		}
		if err := f.FormatDeviceBackupStatus(status); err != nil {
			t.Fatalf("FormatDeviceBackupStatus returned error: %v", err)
		}
		if !strings.Contains(buf.String(), longName) {
			t.Fatal("output must contain the full device name")
		}
	})
}

// TestFormatTokenCreated_FieldsAreInsideTheBox is the token-side companion to
// the ticket regression: this renderer had the same defect, printing the
// titled top border and the bottom border back to back and then printing its
// fields underneath, outside the box entirely.
//
// Three things are outside the box on purpose — the warning callout, the
// token itself (kept on a bare line so it stays easy to select and copy) and
// the closing NDCLI_TOKEN hint — so the assertion runs over the box region
// and then checks that the four boxed fields did not escape it.
func TestFormatTokenCreated_FieldsAreInsideTheBox(t *testing.T) {
	org := "acme"
	token := "ndpat_" + strings.Repeat("k", 48)
	expires := models.FlexibleTime{Time: time.Date(2027, 1, 31, 0, 0, 0, 0, time.UTC)}
	var buf bytes.Buffer
	f := &DetailedFormatter{BaseFormatter: BaseFormatter{Writer: &buf}}
	err := f.FormatTokenCreated(models.TokenCreateResponse{
		Token: token, Name: "ci-runner", Scope: "org", Org: &org, ExpiresAt: &expires,
	})
	if err != nil {
		t.Fatalf("FormatTokenCreated: %v", err)
	}

	out := buf.String()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	first, last := -1, -1
	for i, line := range lines {
		if strings.HasPrefix(line, BoxTopLeft) && first < 0 {
			first = i
		}
		if strings.HasPrefix(line, BoxBottomLeft) {
			last = i
		}
	}
	if first < 0 || last <= first {
		t.Fatalf("no box in the output:\n%s", out)
	}

	fields := []string{"Name", "ci-runner", "Scope", "Org", "acme", "Expires"}
	assertBoxedFields(t, strings.Join(lines[first:last+1], "\n"),
		"Personal Access Token Created", fields)

	outside := append(append([]string{}, lines[:first]...), lines[last+1:]...)
	for _, line := range outside {
		for _, field := range fields {
			if strings.Contains(line, field) {
				t.Errorf("field %q was printed outside the box: %q", field, line)
			}
		}
	}
	// These three belong outside it, the token on its own bare line.
	joined := strings.Join(outside, "\n")
	for _, want := range []string{"Copy your token now", token, "NDCLI_TOKEN"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected %q outside the box:\n%s", want, out)
		}
	}
	for _, line := range lines[first : last+1] {
		if strings.Contains(line, token) {
			t.Errorf("the token must stay outside the box, on a bare line: %q", line)
		}
	}
}

// TestFormatTokenCreated_NarrowTerminalDoesNotPanic is the regression for the
// crash the width clamp made reachable. Before the clamp every caller passed a
// generous fixed width, so TopLineWithTitle's title arithmetic never went
// negative. Once the box started sizing itself to the terminal, this renderer's
// 30-column title no longer fit in a narrow window and
// `ndcli auth token create -f detailed` panicked outright.
func TestFormatTokenCreated_NarrowTerminalDoesNotPanic(t *testing.T) {
	const forced = 24
	forceBoxWidth(t, forced)

	org := "acme"
	var buf bytes.Buffer
	f := &DetailedFormatter{BaseFormatter: BaseFormatter{Writer: &buf}}
	if err := f.FormatTokenCreated(models.TokenCreateResponse{
		Token: "ndpat_" + strings.Repeat("k", 48),
		Name:  "ci-runner", Scope: "org", Org: &org,
	}); err != nil {
		t.Fatalf("FormatTokenCreated: %v", err)
	}

	out := buf.String()
	var top string
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		// Only the box is held to the width. The warning, the bare token and
		// the closing hint are unbordered prose that the terminal soft-wraps;
		// the token in particular must stay on one line to stay copyable.
		if !strings.ContainsAny(line, BoxVertical+BoxTopLeft+BoxBottomLeft) {
			continue
		}
		if w := visibleLength(line); w > forced {
			t.Errorf("box line is %d columns, over the %d limit: %q", w, forced, line)
		}
		if strings.HasPrefix(line, BoxTopLeft) {
			top = line
		}
	}
	if top == "" {
		t.Fatalf("no top border in the output:\n%s", out)
	}
	if !strings.Contains(top, "…") {
		t.Errorf("the title does not fit and must be truncated with an ellipsis: %q", top)
	}
	if !strings.Contains(top, "Personal") {
		t.Errorf("the truncated title should still open with the real one: %q", top)
	}
}
