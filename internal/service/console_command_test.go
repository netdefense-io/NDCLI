package service

import (
	"errors"
	"strings"
	"testing"
)

// A console command is at most 32 KiB, so its exec request fits in one relay
// frame; an empty or longer one is refused before anything is sent.
func TestCheckConsoleCommand(t *testing.T) {
	if err := CheckConsoleCommand(strings.Repeat("a", MaxConsoleCommandBytes)); err != nil {
		t.Errorf("32 KiB command: %v", err)
	}
	for in, msg := range map[string]string{
		"": "command is required",
		strings.Repeat("a", MaxConsoleCommandBytes+1): "command must be at most 32768 bytes (32 KiB); it has 32769",
	} {
		var svcErr *Error
		if err := CheckConsoleCommand(in); !errors.As(err, &svcErr) || svcErr.Code != CodeInvalidInput || svcErr.Message != msg {
			t.Errorf("%d-byte command: %v, want INVALID_INPUT %q", len(in), err, msg)
		}
	}
}
