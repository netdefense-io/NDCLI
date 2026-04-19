package wizard

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/fatih/color"
)

func captureStdout(fn func()) string {
	color.NoColor = true
	r, w, _ := os.Pipe()
	stdout := os.Stdout
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = stdout
	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}

// Note: the fatih/color package writes to its own captured stdout at init,
// so color.Cyan output doesn't go through os.Stdout redirect. We assert only
// on the numbered-step headers (printed via fmt.Println), which is enough to
// verify the ordering — regression guard for NDCLI-go#12.

func TestShowFinalInstructions_NoOrgNoToken_CreateOrgBeforeSettings(t *testing.T) {
	out := captureStdout(func() { ShowFinalInstructions("", "") })
	iOrg := strings.Index(out, "2. Create an organization first")
	iSettings := strings.Index(out, "3. In OPNsense, go to")
	if iOrg < 0 || iSettings < 0 || iOrg >= iSettings {
		t.Fatalf("expected '2. Create an organization first' before '3. In OPNsense, go to'\n%s", out)
	}
}

func TestShowFinalInstructions_OrgOnly_SettingsBeforeGetToken(t *testing.T) {
	out := captureStdout(func() { ShowFinalInstructions("", "lab") })
	iSettings := strings.Index(out, "2. In OPNsense, go to")
	iGetToken := strings.Index(out, "3. Get your registration token with")
	if iSettings < 0 || iGetToken < 0 || iSettings >= iGetToken {
		t.Fatalf("expected '2. In OPNsense, go to' before '3. Get your registration token with'\n%s", out)
	}
}

func TestShowFinalInstructions_WithTokenAndOrg_SettingsBeforeTokenPrompt(t *testing.T) {
	out := captureStdout(func() { ShowFinalInstructions("ABC123", "lab") })
	iSettings := strings.Index(out, "2. In OPNsense, go to")
	iEnter := strings.Index(out, "3. Enter Registration Token")
	if iSettings < 0 || iEnter < 0 || iSettings >= iEnter {
		t.Fatalf("expected '2. In OPNsense, go to' before '3. Enter Registration Token'\n%s", out)
	}
}
