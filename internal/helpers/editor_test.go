package helpers

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveEditorWithAbsolutePath(t *testing.T) {
	resolved, err := resolveEditor("/bin/sh")
	if err != nil {
		t.Fatalf("resolveEditor(/bin/sh) failed: %v", err)
	}
	if resolved != "/bin/sh" {
		t.Errorf("expected /bin/sh, got %q", resolved)
	}
}

func TestResolveEditorWithPathLookup(t *testing.T) {
	resolved, err := resolveEditor("sh")
	if err != nil {
		t.Fatalf("resolveEditor(sh) failed: %v", err)
	}
	if !filepath.IsAbs(resolved) {
		t.Errorf("expected absolute path, got %q", resolved)
	}
}

func TestResolveEditorRejectsNonexistent(t *testing.T) {
	_, err := resolveEditor("nonexistent-editor-binary-xyz")
	if err == nil {
		t.Error("expected error for nonexistent editor, got nil")
	}
}

func TestResolveEditorRejectsNonexistentAbsolutePath(t *testing.T) {
	_, err := resolveEditor("/nonexistent/path/to/editor")
	if err == nil {
		t.Error("expected error for nonexistent absolute path editor, got nil")
	}
}

func TestResolveEditorRejectsDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	_, err := resolveEditor(tmpDir)
	if err == nil {
		t.Error("expected error when editor is a directory, got nil")
	}
}

func TestResolveEditorRejectsEmptyString(t *testing.T) {
	_, err := resolveEditor("")
	if err == nil {
		t.Error("expected error for empty editor string, got nil")
	}
}

func TestResolveEditorHandlesEditorWithArgs(t *testing.T) {
	resolved, err := resolveEditor("sh -c")
	if err != nil {
		t.Fatalf("resolveEditor('sh -c') failed: %v", err)
	}
	if !filepath.IsAbs(resolved) {
		t.Errorf("expected absolute path, got %q", resolved)
	}
}

func TestEnsureTmpDirCreatesDirectory(t *testing.T) {
	dir, err := ensureTmpDir()
	if err != nil {
		t.Fatalf("ensureTmpDir failed: %v", err)
	}

	// Verify the directory exists
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("temp directory not created at %s: %v", dir, err)
	}
	if !info.IsDir() {
		t.Error("ensureTmpDir result is not a directory")
	}

	// Verify it's under an ndcli config directory, not system /tmp
	if strings.HasPrefix(dir, os.TempDir()) {
		t.Errorf("temp dir should not be under system temp %q, got %q", os.TempDir(), dir)
	}

	// Verify path ends with ndcli/tmp
	if !strings.HasSuffix(dir, filepath.Join("ndcli", "tmp")) {
		t.Errorf("expected path ending with ndcli/tmp, got %q", dir)
	}

	// Verify directory permissions
	perm := info.Mode().Perm()
	if perm != 0700 {
		t.Errorf("expected directory permissions 0700, got %o", perm)
	}
}

func TestEditContentRejectsSymlinkSwap(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a target file the symlink will point to
	targetFile := filepath.Join(tmpDir, "malicious.txt")
	if err := os.WriteFile(targetFile, []byte("malicious content"), 0600); err != nil {
		t.Fatalf("failed to create target file: %v", err)
	}

	// Create a script that replaces the temp file with a symlink
	scriptPath := filepath.Join(tmpDir, "evil-editor.sh")
	script := `#!/bin/sh
rm "$1"
ln -s "` + targetFile + `" "$1"
`
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("failed to create editor script: %v", err)
	}

	// Override EDITOR
	origEditor := os.Getenv("EDITOR")
	os.Setenv("EDITOR", scriptPath)
	defer os.Setenv("EDITOR", origEditor)

	_, err := EditContent("original content", ".txt")
	if err == nil {
		t.Error("expected error when temp file is replaced with symlink, got nil")
	}
}

func TestEditContentPreservesContent(t *testing.T) {
	// Use 'true' as editor (does nothing, exits 0)
	if _, err := exec.LookPath("true"); err != nil {
		t.Skip("'true' command not available")
	}

	origEditor := os.Getenv("EDITOR")
	os.Setenv("EDITOR", "true")
	defer os.Setenv("EDITOR", origEditor)

	result, err := EditContent("hello world", ".txt")
	if err != nil {
		t.Fatalf("EditContent failed: %v", err)
	}

	// Content should be unchanged since 'true' doesn't modify the file
	if result != "hello world" {
		t.Errorf("expected 'hello world', got %q", result)
	}
}

func TestEditContentEditorModifiesContent(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a script that overwrites the file with new content
	scriptPath := filepath.Join(tmpDir, "overwrite-editor.sh")
	script := `#!/bin/sh
echo "edited" > "$1"
`
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("failed to create editor script: %v", err)
	}

	origEditor := os.Getenv("EDITOR")
	os.Setenv("EDITOR", scriptPath)
	defer os.Setenv("EDITOR", origEditor)

	result, err := EditContent("original", ".txt")
	if err != nil {
		t.Fatalf("EditContent failed: %v", err)
	}

	if strings.TrimSpace(result) != "edited" {
		t.Errorf("expected 'edited', got %q", result)
	}
}
