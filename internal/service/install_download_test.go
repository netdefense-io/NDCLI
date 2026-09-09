package service

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// copyNoReplace is the fallback half of installDownload, reached only on a
// filesystem that refuses hard links. Nothing in the download tests can
// force that condition portably, so the primitive is exercised directly
// here — its no-replace guarantee is what the whole overwrite=false path
// rests on when os.Link is unavailable.

func TestCopyNoReplace_CreatesFileWithContentAndMode(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	content := []byte("attachment payload")
	if err := os.WriteFile(src, content, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := copyNoReplace(src, dst); err != nil {
		t.Fatalf("copyNoReplace: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil || string(got) != string(content) {
		t.Errorf("destination content = %q (err %v)", got, err)
	}
	// It copies rather than moves: installDownload removes the temporary
	// file itself, on every path, and must not depend on this one doing it.
	if _, err := os.Stat(src); err != nil {
		t.Errorf("source should survive the copy: %v", err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(dst)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("destination mode = %04o, want 0600", perm)
		}
	}
}

func TestCopyNoReplace_RefusesExistingFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.WriteFile(src, []byte("attachment payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("important data"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := copyNoReplace(src, dst)
	if err == nil {
		t.Fatal("expected a refusal, the destination already exists")
	}
	// installDownload maps exactly this classification onto
	// CodeDestinationExists, so an error that is not IsExist would be
	// reported to the user as a write failure instead of a refusal.
	if !os.IsExist(err) {
		t.Errorf("expected an IsExist error, got %v", err)
	}
	if got, _ := os.ReadFile(dst); string(got) != "important data" {
		t.Errorf("the destination was modified: %q", got)
	}
}

// A dangling symlink is the case a stat cannot see: it follows the link,
// finds nothing, and reports the name as free. O_EXCL refuses it anyway,
// and must not create the file the link points at.
func TestCopyNoReplace_RefusesDanglingSymlink(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	pointee := filepath.Join(dir, "nowhere")
	if err := os.WriteFile(src, []byte("attachment payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(pointee, dst); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	err := copyNoReplace(src, dst)
	if err == nil {
		t.Fatal("expected a refusal for a dangling symlink")
	}
	if !os.IsExist(err) {
		t.Errorf("expected an IsExist error, got %v", err)
	}
	if target, lerr := os.Readlink(dst); lerr != nil || target != pointee {
		t.Errorf("symlink was disturbed: %q (err %v)", target, lerr)
	}
	if _, err := os.Lstat(pointee); !os.IsNotExist(err) {
		t.Errorf("the symlink was followed and its pointee created: %v", err)
	}
}

// The fallback publishes the name before the bytes, so a copy that fails
// midway must take the destination back down rather than leave a short
// file where a complete attachment is expected. Reading a directory is the
// cheapest portable way to make the copy, and only the copy, fail.
func TestCopyNoReplace_RemovesPartialDestinationOnCopyFailure(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "srcdir")
	dst := filepath.Join(dir, "dst")
	if err := os.Mkdir(src, 0o700); err != nil {
		t.Fatal(err)
	}

	err := copyNoReplace(src, dst)
	if err == nil {
		t.Fatal("expected the copy to fail with a directory as its source")
	}
	if os.IsExist(err) {
		t.Errorf("a copy failure must not be classified as destination-exists: %v", err)
	}
	// Whether the failure lands at the open or during the copy is
	// platform-dependent; that nothing is left at the destination is not.
	if _, statErr := os.Lstat(dst); !os.IsNotExist(statErr) {
		t.Errorf("a partial destination was left behind: %v", statErr)
	}
}

// This is installDownload's os.Link EEXIST branch, not the fallback: link(2)
// fails EEXIST on an existing target whatever the filesystem thinks of hard
// links, so copyNoReplace is never reached here. The fallback's own
// refusal is covered by TestCopyNoReplace_RefusesExistingFile, which calls
// it directly. What this pins is the mapping either branch feeds: an
// IsExist error becomes DESTINATION_EXISTS, and the temporary file goes.
func TestInstallDownload_LinkRefusalIsReportedAsDestinationExists(t *testing.T) {
	dir := t.TempDir()
	tmp := filepath.Join(dir, "tmp")
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(tmp, []byte("attachment payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("important data"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := installDownload(tmp, target, false)
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != CodeDestinationExists {
		t.Fatalf("expected CodeDestinationExists, got %v", err)
	}
	if !strings.Contains(err.Error(), OverwriteHint) {
		t.Errorf("expected the overwrite hint, got %v", err)
	}
	if got, _ := os.ReadFile(target); string(got) != "important data" {
		t.Errorf("the destination was replaced: %q", got)
	}
	// The temporary file is the caller's to lose, on every path.
	if _, statErr := os.Stat(tmp); !os.IsNotExist(statErr) {
		t.Errorf("the temporary file survived: %v", statErr)
	}
}
