package mcp

import (
	"errors"
	"strings"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/service"
)

// TestProvisionError covers the two things an MCP caller needs from a failed
// `prefix add --value`: a hint naming something it can actually invoke, and a
// machine-readable code.
//
// The code is the part that was lost. errorResult reads it from a type switch
// on the concrete type, so flattening through fmt.Errorf left error.code empty
// for every failure on this path — a bad CIDR that had CodeInvalidInput and a
// genuine mismatch that had CodeVariableValueMismatch alike, while the sibling
// branch beside it reported codes fine.
func TestProvisionError(t *testing.T) {
	t.Run("a coded service error keeps its code", func(t *testing.T) {
		in := &service.Error{Code: service.CodeInvalidInput, Message: "not a valid CIDR block"}

		got := provisionError(in)

		var svcErr *service.Error
		if !errors.As(got, &svcErr) {
			t.Fatalf("errorResult needs a *service.Error, got %T", got)
		}
		// errorResult type-switches on the concrete type, so being merely
		// reachable through errors.As is not enough.
		if _, ok := got.(*service.Error); !ok {
			t.Errorf("the concrete type must be *service.Error, got %T", got)
		}
		if svcErr.Code != service.CodeInvalidInput {
			t.Errorf("code = %q, want %q", svcErr.Code, service.CodeInvalidInput)
		}
		if !strings.Contains(svcErr.Message, "not a valid CIDR block") {
			t.Errorf("message lost: %q", svcErr.Message)
		}
	})

	t.Run("a wrapped mismatch keeps its code and gains the tool name", func(t *testing.T) {
		cause := &service.VariableMismatchError{
			Scope: service.VarScopeOrg, Name: "lan", Existing: "a", Want: "b",
		}
		// How it actually arrives: the mismatch happens partway through the
		// three writes, so it reaches the handler inside the provisioning
		// wrapper rather than as a bare *service.Error.
		wrapped := &service.NetworkPrefixProvisionError{
			Step: "creating the organization-scope variable",
			Err: &service.Error{
				Code:    service.CodeVariableValueMismatch,
				Message: cause.Error(),
				Err:     cause,
			},
		}

		got := provisionError(wrapped)

		svcErr, ok := got.(*service.Error)
		if !ok {
			t.Fatalf("the concrete type must be *service.Error, got %T", got)
		}
		if svcErr.Code != service.CodeVariableValueMismatch {
			t.Errorf("code = %q, want %q", svcErr.Code, service.CodeVariableValueMismatch)
		}
		if strings.Contains(svcErr.Message, service.VariableMismatchHint) {
			t.Errorf("the placeholder reached the caller:\n%s", svcErr.Message)
		}
		if !strings.Contains(svcErr.Message, "ndcli.variable.set") {
			t.Errorf("expected the MCP tool to be named:\n%s", svcErr.Message)
		}
		// The wrapper's own context is what tells the caller which step failed.
		if !strings.Contains(svcErr.Message, "creating the organization-scope variable") {
			t.Errorf("the step context was dropped:\n%s", svcErr.Message)
		}
		if !errors.Is(got, error(wrapped)) {
			t.Error("the original error should stay reachable")
		}
	})

	t.Run("an uncoded error passes through untouched", func(t *testing.T) {
		in := errors.New("context deadline exceeded")

		if got := provisionError(in); got != in {
			t.Errorf("expected the original error, got %#v", got)
		}
	})

	t.Run("nil stays nil", func(t *testing.T) {
		if got := provisionError(nil); got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})
}

// TestProvisionMessage is the MCP half of the tri-state that
// TestNetworkPrefixProvisionLeavesPublishUnknownWhenReadBackFails covers at
// the service layer.
//
// The CLI got this treatment and MCP did not: the message said "published"
// unconditionally, including on a re-run against an existing publish=false
// prefix and on the case where the read-back failed and nobody had looked. The
// surface whose consumer is a model was the one giving the confident wrong
// answer.
func TestProvisionMessage(t *testing.T) {
	tests := []struct {
		name      string
		created   bool
		published interface{}
		want      string
		notWant   string
	}{
		{
			name:      "a prefix this call created is published",
			created:   true,
			published: true,
			want:      "published on fw01 in hq",
		},
		{
			name:      "an existing prefix is not described as published by this call",
			created:   false,
			published: true,
			want:      "already exists on fw01 in hq",
			notWant:   "published on",
		},
		{
			name:      "publish=false says it is not advertised",
			created:   false,
			published: false,
			want:      "publish=false",
			notWant:   "published on",
		},
		{
			// The one that matters most: nobody read the flag, so the message
			// must not imply one.
			name:      "an unread flag is reported as unknown",
			created:   false,
			published: nil,
			want:      "could not be read back and is unknown",
			notWant:   "published on",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := provisionMessage("lan", "fw01", "hq", "10.20.0.0/24", tt.created, tt.published)

			if !strings.Contains(got, tt.want) {
				t.Errorf("message missing %q:\n%s", tt.want, got)
			}
			if tt.notWant != "" && strings.Contains(got, tt.notWant) {
				t.Errorf("message should not claim %q:\n%s", tt.notWant, got)
			}
		})
	}
}
