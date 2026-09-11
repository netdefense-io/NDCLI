package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/service"
)

// TestNamedVariableSetCommand covers the half of the CodeVariableValueMismatch
// contract that lives in this package. The service leaves a marker rather than
// naming a CLI command an MCP caller could not run; if nothing replaced it,
// the user would see the placeholder itself.
func TestNamedVariableSetCommand(t *testing.T) {
	orgCause := &service.VariableMismatchError{
		Scope: service.VarScopeOrg, Name: "lan", Existing: "a", Want: "b",
	}
	orgMismatch := &service.Error{
		Code:    service.CodeVariableValueMismatch,
		Message: orgCause.Error(),
		Err:     orgCause,
	}

	t.Run("names the org command and leaves no placeholder", func(t *testing.T) {
		got := namedVariableSetCommand(orgMismatch).Error()

		if strings.Contains(got, service.VariableMismatchHint) {
			t.Errorf("the placeholder survived into user-facing text:\n%s", got)
		}
		if !strings.Contains(got, "ndcli variable org set lan") {
			t.Errorf("expected the org command naming the variable:\n%s", got)
		}
	})

	t.Run("names the device command, with the device", func(t *testing.T) {
		deviceCause := &service.VariableMismatchError{
			Scope: service.VarScopeDevice, Entity: "fw01", Name: "lan", Existing: "a", Want: "b",
		}
		deviceMismatch := &service.Error{
			Code:    service.CodeVariableValueMismatch,
			Message: deviceCause.Error(),
			Err:     deviceCause,
		}

		got := namedVariableSetCommand(deviceMismatch).Error()
		if !strings.Contains(got, "ndcli variable device set fw01 lan") {
			t.Errorf("expected the device command naming the device and variable:\n%s", got)
		}
	})

	t.Run("reaches through the provisioning wrapper", func(t *testing.T) {
		// How it actually arrives: the mismatch happens partway through the
		// three writes, so the front-end sees the provisioning error and has
		// to unwrap to the cause.
		wrapped := &service.NetworkPrefixProvisionError{
			Step: "creating the organization-scope variable",
			Err:  orgMismatch,
		}

		got := namedVariableSetCommand(wrapped).Error()
		if strings.Contains(got, service.VariableMismatchHint) {
			t.Errorf("the placeholder survived through the wrapper:\n%s", got)
		}
		if !strings.Contains(got, "creating the organization-scope variable") {
			t.Errorf("the wrapper's own context should survive:\n%s", got)
		}
	})

	t.Run("leaves unrelated errors alone", func(t *testing.T) {
		plain := errors.New("something else")
		if got := namedVariableSetCommand(plain); got != plain {
			t.Errorf("an unrelated error should pass through untouched, got %v", got)
		}

		other := &service.Error{Code: service.CodeInvalidInput, Message: "nope"}
		if got := namedVariableSetCommand(other); got != error(other) {
			t.Errorf("a different service error should pass through untouched, got %v", got)
		}
	})
}
