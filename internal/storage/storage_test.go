package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveRejectsSymlink(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a real file that the symlink will point to
	targetFile := filepath.Join(tmpDir, "target.json")
	if err := os.WriteFile(targetFile, []byte("target"), 0600); err != nil {
		t.Fatalf("failed to create target file: %v", err)
	}

	fs := &FileStorage{basePath: filepath.Join(tmpDir, "auth.json")}

	// Determine where Save will actually write (host-scoped path)
	actualPath := fs.getHostScopedPath()

	// Create the parent directory so the symlink can be placed
	if err := os.MkdirAll(filepath.Dir(actualPath), 0700); err != nil {
		t.Fatalf("failed to create parent dir: %v", err)
	}

	// Create a symlink at the actual write path
	if err := os.Symlink(targetFile, actualPath); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	err := fs.Save([]byte(`{"token":"secret"}`), "")
	if err == nil {
		t.Error("expected error when saving to symlink path, got nil")
	}

	// Verify the error message mentions symlink
	if err != nil && !containsString(err.Error(), "symlink") {
		t.Errorf("expected error to mention symlink, got: %v", err)
	}

	// Verify target file was not overwritten
	data, _ := os.ReadFile(targetFile)
	if string(data) != "target" {
		t.Error("target file was modified despite symlink rejection")
	}
}

func TestSaveCreatesFileWithCorrectPermissions(t *testing.T) {
	tmpDir := t.TempDir()

	fs := &FileStorage{basePath: filepath.Join(tmpDir, "auth.json")}
	actualPath := fs.getHostScopedPath()

	err := fs.Save([]byte(`{"token":"test"}`), "")
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Verify file exists and has correct permissions
	info, err := os.Stat(actualPath)
	if err != nil {
		t.Fatalf("failed to stat file at %s: %v", actualPath, err)
	}

	perm := info.Mode().Perm()
	if perm != 0600 {
		t.Errorf("expected file permissions 0600, got %o", perm)
	}

	// Verify content was written
	data, err := os.ReadFile(actualPath)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if string(data) != `{"token":"test"}` {
		t.Errorf("unexpected file content: %s", data)
	}
}

func TestSaveOverwritesExistingRegularFile(t *testing.T) {
	tmpDir := t.TempDir()

	fs := &FileStorage{basePath: filepath.Join(tmpDir, "auth.json")}
	actualPath := fs.getHostScopedPath()

	// Create existing file at the host-scoped path
	if err := os.MkdirAll(filepath.Dir(actualPath), 0700); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}
	if err := os.WriteFile(actualPath, []byte("old"), 0600); err != nil {
		t.Fatalf("failed to create existing file: %v", err)
	}

	err := fs.Save([]byte("new"), "")
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	data, _ := os.ReadFile(actualPath)
	if string(data) != "new" {
		t.Errorf("expected 'new', got %q", string(data))
	}
}

func TestSaveCreatesParentDirectories(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "subdir", "nested", "auth.json")

	fs := &FileStorage{basePath: filePath}

	err := fs.Save([]byte("data"), "")
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Verify the file was created (at the host-scoped path)
	actualPath := fs.getHostScopedPath()
	dirInfo, err := os.Stat(filepath.Dir(actualPath))
	if err != nil {
		t.Fatalf("parent directory not created: %v", err)
	}
	dirPerm := dirInfo.Mode().Perm()
	if dirPerm&0077 != 0 {
		t.Errorf("parent directory is group/world accessible: %o", dirPerm)
	}
}

func TestLoadNonExistentFileReturnsNil(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "nonexistent.json")

	fs := &FileStorage{basePath: filePath}

	data, err := fs.Load()
	if err != nil {
		t.Errorf("expected no error for nonexistent file, got: %v", err)
	}
	if data != nil {
		t.Errorf("expected nil data for nonexistent file, got: %s", data)
	}
}

func TestClearRemovesFile(t *testing.T) {
	tmpDir := t.TempDir()

	fs := &FileStorage{basePath: filepath.Join(tmpDir, "auth.json")}

	// Create file first
	if err := fs.Save([]byte("data"), ""); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Clear it
	if err := fs.Clear(); err != nil {
		t.Fatalf("Clear failed: %v", err)
	}

	// Verify file is gone
	actualPath := fs.getHostScopedPath()
	if _, err := os.Stat(actualPath); !os.IsNotExist(err) {
		t.Error("file still exists after Clear")
	}
}

func TestClearNonExistentFileSucceeds(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "nonexistent.json")

	fs := &FileStorage{basePath: filePath}

	if err := fs.Clear(); err != nil {
		t.Errorf("Clear of nonexistent file should succeed, got: %v", err)
	}
}

func containsString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
