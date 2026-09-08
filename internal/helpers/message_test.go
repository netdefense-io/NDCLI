package helpers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadMessage_Sources(t *testing.T) {
	if got, err := ReadMessage("hello", "", true); err != nil || got != "hello" {
		t.Errorf("--message = %q, %v", got, err)
	}

	path := filepath.Join(t.TempDir(), "body.txt")
	if err := os.WriteFile(path, []byte("from a file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The trailing newline an editor leaves behind is not part of the body.
	if got, err := ReadMessage("", path, true); err != nil || got != "from a file" {
		t.Errorf("--message-file = %q, %v", got, err)
	}
}

func TestReadMessage_MutuallyExclusive(t *testing.T) {
	if _, err := ReadMessage("a", "b", true); err == nil {
		t.Fatal("expected --message and --message-file to conflict")
	}
}

func TestReadMessage_RequiredAndOptional(t *testing.T) {
	if _, err := ReadMessage("", "", true); err == nil {
		t.Fatal("expected an error when a body is required")
	}
	if got, err := ReadMessage("", "", false); err != nil || got != "" {
		t.Errorf("optional body = %q, %v", got, err)
	}
}

func TestReadMessage_RejectsOversizedBody(t *testing.T) {
	_, err := ReadMessage(strings.Repeat("x", MaxMessageBytes+1), "", true)
	if err == nil || !strings.Contains(err.Error(), "64 KiB") {
		t.Fatalf("expected a size error naming the limit, got %v", err)
	}
}

func TestReadMessage_MissingFile(t *testing.T) {
	if _, err := ReadMessage("", filepath.Join(t.TempDir(), "nope.txt"), true); err == nil {
		t.Fatal("expected an error for a missing file")
	}
}

// TestReadMessage_FileReadIsBounded guards against pulling an arbitrarily
// large file into memory just to reject it: the file branch reads under the
// same limit the stdin branch uses.
func TestReadMessage_FileReadIsBounded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big.txt")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", MaxMessageBytes*2)), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := ReadMessage("", path, true)
	if err == nil || !strings.Contains(err.Error(), "64 KiB") {
		t.Fatalf("expected a size error naming the limit, got %v", err)
	}
}

// TestReadMessage_TruncationNotMaskedByNewlineTrim is the subtle case: the
// read is bounded at the cap plus one byte, so if that last byte is a
// newline, trimming before the size check would bring the length back under
// the cap and submit a silently truncated message.
func TestReadMessage_TruncationNotMaskedByNewlineTrim(t *testing.T) {
	path := filepath.Join(t.TempDir(), "body.txt")
	// Exactly the cap, then a newline at the cutoff, then more text that the
	// bounded read will never see.
	body := strings.Repeat("x", MaxMessageBytes) + "\n" + strings.Repeat("y", 1024)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadMessage("", path, true); err == nil {
		t.Fatal("expected the oversized body to be rejected, not silently truncated")
	}
}
