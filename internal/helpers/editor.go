package helpers

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/netdefense-io/NDCLI/internal/config"
)

// EditContent opens an external editor for editing content
func EditContent(content, extension string) (string, error) {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = os.Getenv("VISUAL")
	}
	if editor == "" {
		// Try common editors
		for _, e := range []string{"vim", "vi", "nano", "notepad"} {
			if _, err := exec.LookPath(e); err == nil {
				editor = e
				break
			}
		}
	}
	if editor == "" {
		return "", fmt.Errorf("no editor found. Please set the EDITOR environment variable")
	}

	// Resolve the editor to an absolute path to validate it exists
	editorPath, err := resolveEditor(editor)
	if err != nil {
		return "", err
	}

	// Create temp file in the NDCLI config directory instead of system /tmp
	tmpDir, err := ensureTmpDir()
	if err != nil {
		return "", fmt.Errorf("failed to create secure temp directory: %w", err)
	}

	tmpFile, err := os.CreateTemp(tmpDir, "ndcli-*"+extension)
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	// Write content
	if _, err := tmpFile.WriteString(content); err != nil {
		return "", fmt.Errorf("failed to write to temp file: %w", err)
	}
	tmpFile.Close()

	// Record file info before editing for integrity check
	infoBefore, err := os.Lstat(tmpPath)
	if err != nil {
		return "", fmt.Errorf("failed to stat temp file: %w", err)
	}

	// Open editor
	cmd := exec.Command(editorPath, tmpPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("editor exited with error: %w", err)
	}

	// Verify file integrity after editing: check it's still a regular file
	// owned by us and not a symlink
	infoAfter, err := os.Lstat(tmpPath)
	if err != nil {
		return "", fmt.Errorf("failed to read edited file: %w", err)
	}
	if infoAfter.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("temp file was replaced with a symlink, aborting")
	}
	if !os.SameFile(infoBefore, infoAfter) {
		return "", fmt.Errorf("temp file was replaced during editing, aborting")
	}

	// Read edited content
	edited, err := os.ReadFile(tmpPath)
	if err != nil {
		return "", fmt.Errorf("failed to read edited file: %w", err)
	}

	return string(edited), nil
}

// resolveEditor validates the editor command and resolves it to an absolute path.
// It supports editors specified with arguments (e.g., "code --wait").
func resolveEditor(editor string) (string, error) {
	// Handle editors with arguments (e.g., "code --wait")
	parts := strings.Fields(editor)
	if len(parts) == 0 {
		return "", fmt.Errorf("empty editor command")
	}

	binary := parts[0]

	// If already an absolute path, verify it exists and is executable
	if filepath.IsAbs(binary) {
		info, err := os.Stat(binary)
		if err != nil {
			return "", fmt.Errorf("editor %q not found: %w", binary, err)
		}
		if info.IsDir() {
			return "", fmt.Errorf("editor %q is a directory, not an executable", binary)
		}
		return binary, nil
	}

	// Resolve via PATH lookup
	resolved, err := exec.LookPath(binary)
	if err != nil {
		return "", fmt.Errorf("editor %q not found in PATH: %w", binary, err)
	}

	return resolved, nil
}

// ensureTmpDir creates a secure temporary directory under the NDCLI config directory
func ensureTmpDir() (string, error) {
	configDir, err := config.EnsureConfigDir()
	if err != nil {
		return "", err
	}

	tmpDir := filepath.Join(configDir, "tmp")
	if err := os.MkdirAll(tmpDir, 0700); err != nil {
		return "", err
	}

	return tmpDir, nil
}
