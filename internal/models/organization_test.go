package models

import "testing"

// The literal is copied from NDManager's OrganizationCreateRequest.validate_name,
// not read from the code under test.
func TestOrganizationNamePatternIsTheServersRule(t *testing.T) {
	if got, want := OrganizationNamePattern.String(), `^[a-zA-Z0-9._-]+$`; got != want {
		t.Errorf("OrganizationNamePattern = %s, want NDManager's %s", got, want)
	}
	for name, want := range map[string]bool{
		"acme-corp":   true,
		"acme.corp_2": true,
		"":            false,
		"acme corp":   false,
		"acme;rm":     false,
		"acme\n":      false,
	} {
		if got := OrganizationNamePattern.MatchString(name); got != want {
			t.Errorf("match %q = %v, want %v", name, got, want)
		}
	}
}
