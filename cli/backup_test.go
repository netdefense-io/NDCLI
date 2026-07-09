package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestWarnPlaintextSecretFlag(t *testing.T) {
	var buf bytes.Buffer
	warnPlaintextSecretFlag(&buf, "s3-access-key")

	out := buf.String()
	for _, want := range []string{"process listings", "shell history", "s3-access-key"} {
		if !strings.Contains(out, want) {
			t.Errorf("warning missing %q; full output: %q", want, out)
		}
	}
}

func TestBackupConfigSetS3AccessKeyFlagChangedDetection(t *testing.T) {
	cmd := backupConfigSetCmd
	if err := cmd.Flags().Set("s3-access-key", "x"); err != nil {
		t.Fatalf("failed to set flag: %v", err)
	}
	defer cmd.Flags().Set("s3-access-key", "")

	if !cmd.Flags().Changed("s3-access-key") {
		t.Error("expected Flags().Changed(\"s3-access-key\") to be true after Set")
	}
}
