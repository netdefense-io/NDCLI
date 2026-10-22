package service

import (
	"errors"
	"testing"
)

// The MCP tools reach ResolveOrg, so its refusal names the organization
// parameter, not a CLI flag.
func TestResolveOrgNamesTheParameter(t *testing.T) {
	_, err := (&Service{}).ResolveOrg("")
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != CodeOrgRequired ||
		svcErr.Message != "Organization is required: pass organization, or set organization.name in the config." {
		t.Errorf("err = %v", err)
	}
}
