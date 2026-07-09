package output

import (
	"bytes"
	"strings"
	"testing"

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
