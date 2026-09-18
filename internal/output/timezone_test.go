package output

import "testing"

// TestApplyConfiguredTimezone covers the fallback both front-ends depend on:
// a valid name is applied, an invalid one reports an error and leaves the
// display zone at system local rather than at whatever was set before.
func TestApplyConfiguredTimezone(t *testing.T) {
	t.Cleanup(func() { _ = SetTimezone("Local") })

	if err := ApplyConfiguredTimezone("America/Sao_Paulo"); err != nil {
		t.Fatalf("valid zone rejected: %v", err)
	}
	if got := GetTimezone(); got != "America/Sao_Paulo" {
		t.Fatalf("timezone = %q, want America/Sao_Paulo", got)
	}

	if err := ApplyConfiguredTimezone("Mars/Olympus_Mons"); err == nil {
		t.Fatal("expected an invalid timezone to be reported")
	}
	if got := GetTimezone(); got != "Local" {
		t.Fatalf("after an invalid zone, timezone = %q, want the Local fallback", got)
	}

	if err := ApplyConfiguredTimezone(""); err != nil {
		t.Fatalf("empty zone rejected: %v", err)
	}
	if got := GetTimezone(); got != "Local" {
		t.Fatalf("empty timezone = %q, want Local", got)
	}
}
