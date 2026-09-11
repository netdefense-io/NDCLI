package service

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizePrefixCIDRs(t *testing.T) {
	tests := []struct {
		name    string
		in      []string
		want    string
		wantErr string
	}{
		{
			name: "a single block",
			in:   []string{"10.0.0.0/24"},
			want: "10.0.0.0/24",
		},
		{
			name: "repeated flags join with commas, the form NDManager splits on",
			in:   []string{"10.0.0.0/24", "10.1.0.0/24"},
			want: "10.0.0.0/24,10.1.0.0/24",
		},
		{
			name: "one comma-separated argument is accepted too",
			in:   []string{"10.0.0.0/24,10.1.0.0/24"},
			want: "10.0.0.0/24,10.1.0.0/24",
		},
		{
			name: "surrounding whitespace is trimmed",
			in:   []string{" 10.0.0.0/24 , 10.1.0.0/24 "},
			want: "10.0.0.0/24,10.1.0.0/24",
		},
		{
			name: "IPv6 is accepted",
			in:   []string{"2001:db8::/32"},
			want: "2001:db8::/32",
		},
		{
			// IPv6 has several valid spellings of one network. The stored form
			// has to be canonical, because ensurePrefixVariable compares with
			// string equality: storing the typed text would make a re-run with
			// a differently-spelled but identical value look like a mismatch
			// and refuse, which is exactly what "same value = already done"
			// exists to prevent.
			name: "uppercase IPv6 hex is canonicalised",
			in:   []string{"2001:0DB8::/32"},
			want: "2001:db8::/32",
		},
		{
			name: "explicit zero groups are canonicalised",
			in:   []string{"2001:db8:0:0:0:0:0:0/32"},
			want: "2001:db8::/32",
		},
		{
			name: "two spellings of the same network dedupe to one",
			in:   []string{"2001:0DB8::/32", "2001:db8::/32"},
			want: "2001:db8::/32",
		},
		{
			name: "a canonicalised IPv6 host address keeps its /128",
			in:   []string{"2001:0DB8::1/128"},
			want: "2001:db8::1/128",
		},
		{
			name: "order is preserved and duplicates dropped",
			in:   []string{"10.1.0.0/24", "10.0.0.0/24", "10.1.0.0/24"},
			want: "10.1.0.0/24,10.0.0.0/24",
		},
		{
			// net.ParseCIDR accepts this: it returns the address and the
			// network separately and objects to neither. Without an explicit
			// check it would be stored exactly as typed and reach AllowedIPs
			// intact — the same shape of silent failure this validation exists
			// to stop.
			name:    "host bits are rejected",
			in:      []string{"10.20.0.5/24"},
			wantErr: "has host bits set",
		},
		{
			name:    "the refusal names both plausible intentions",
			in:      []string{"10.20.0.5/24"},
			wantErr: "write 10.20.0.0/24 for the whole subnet, or 10.20.0.5/32",
		},
		{
			name:    "IPv6 host bits are rejected too",
			in:      []string{"2001:db8::1/32"},
			wantErr: "write 2001:db8::/32 for the whole subnet, or 2001:db8::1/128",
		},
		{
			name: "a single-address /32 is fine, it has no host bits",
			in:   []string{"10.20.0.5/32"},
			want: "10.20.0.5/32",
		},
		{
			name: "a single-address /128 is fine",
			in:   []string{"2001:db8::1/128"},
			want: "2001:db8::1/128",
		},
		{
			name:    "a bare address without a mask is rejected",
			in:      []string{"10.0.0.1"},
			wantErr: "not a valid CIDR",
		},
		{
			name:    "an impossible mask is rejected",
			in:      []string{"10.0.0.0/33"},
			wantErr: "not a valid CIDR",
		},
		{
			name:    "prose is rejected",
			in:      []string{"the office network"},
			wantErr: "not a valid CIDR",
		},
		{
			// NDManager stores this happily and explode_prefix_cidrs turns it
			// into an empty list, rendering a peer that carries only its own
			// overlay /32 — a tunnel that comes up and moves no traffic, with
			// no error anywhere. Catch it here.
			name:    "a value that would split to nothing is rejected",
			in:      []string{" "},
			wantErr: "publishes nothing",
		},
		{
			name:    "commas alone are rejected",
			in:      []string{",,"},
			wantErr: "publishes nothing",
		},
		{
			name:    "no value at all is rejected",
			in:      nil,
			wantErr: "publishes nothing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizePrefixCIDRs(tt.in)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected an error containing %q, got value %q", tt.wantErr, got)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error = %q, want it to contain %q", err.Error(), tt.wantErr)
				}
				var svcErr *Error
				if !errors.As(err, &svcErr) || svcErr.Code != CodeInvalidInput {
					t.Errorf("expected a service Error coded %s, got %#v", CodeInvalidInput, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestNormalizePrefixCIDRsRejectsBeforeAnyWrite is the reason validation lives
// client-side: NDManager validates none of it. The value column accepts any
// string and whatever is stored goes verbatim into the peer's AllowedIPs, so
// an unchecked value fails on the device rather than at the API.
func TestNormalizePrefixCIDRsRejectsBeforeAnyWrite(t *testing.T) {
	if _, err := NormalizePrefixCIDRs([]string{"10.0.0.0/24", "not-a-cidr"}); err == nil {
		t.Fatal("a malformed entry anywhere in the list must reject the whole list")
	}
}

func TestNetworkPrefixProvisionErrorNamesCompletedSteps(t *testing.T) {
	err := &NetworkPrefixProvisionError{
		Step:      "publishing the prefix on \"hq\"",
		Completed: []string{"organization-scope variable \"lan\" created with value \"10.0.0.0/24\""},
		Err:       errors.New("boom"),
	}

	msg := err.Error()
	for _, want := range []string{
		"publishing the prefix",
		"boom",
		"Already completed",
		"organization-scope variable",
		"Re-run the same command",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("message missing %q:\n%s", want, msg)
		}
	}

	if !errors.Is(err, err.Err) {
		t.Error("the underlying cause should stay reachable through errors.Is")
	}
}

// TestNetworkPrefixProvisionErrorStaysQuietWithNothingDone: when the very
// first write fails, nothing was created, and claiming otherwise would send
// the user looking for state that does not exist.
func TestNetworkPrefixProvisionErrorStaysQuietWithNothingDone(t *testing.T) {
	err := &NetworkPrefixProvisionError{Step: "creating the organization-scope variable", Err: errors.New("nope")}
	if strings.Contains(err.Error(), "Already completed") {
		t.Errorf("nothing was completed, the message should not say otherwise:\n%s", err.Error())
	}
}

func TestVariableMismatchError(t *testing.T) {
	org := &VariableMismatchError{
		Scope: VarScopeOrg, Name: "lan", Existing: "10.0.0.0/24", Want: "10.9.0.0/24",
	}
	for _, want := range []string{"organization scope", "10.0.0.0/24", "10.9.0.0/24", "will not overwrite", VariableMismatchHint} {
		if !strings.Contains(org.Error(), want) {
			t.Errorf("org message missing %q: %s", want, org.Error())
		}
	}

	dev := &VariableMismatchError{
		Scope: VarScopeDevice, Entity: "fw01", Name: "lan", Existing: "10.0.0.0/24", Want: "10.9.0.0/24",
	}
	if !strings.Contains(dev.Error(), `device "fw01"`) {
		t.Errorf("device message should name the device: %s", dev.Error())
	}
}

// TestVariableMismatchCarriesStructuredCause: a front-end must be able to read
// the scope as a field rather than parsing it back out of the sentence, and
// the *Error wrapper must keep the code MCP switches on.
func TestVariableMismatchCarriesStructuredCause(t *testing.T) {
	err := variableMismatch(VarScopeDevice, "fw01", "lan", "a", "b")

	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != CodeVariableValueMismatch {
		t.Fatalf("expected a service Error coded %s, got %#v", CodeVariableValueMismatch, err)
	}

	var mismatch *VariableMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatal("the structured cause should be reachable through errors.As")
	}
	if mismatch.Scope != VarScopeDevice || mismatch.Entity != "fw01" || mismatch.Name != "lan" {
		t.Errorf("cause fields = %+v", mismatch)
	}

	// Reachable through the provisioning wrapper too, which is how it actually
	// arrives at a front-end.
	wrapped := &NetworkPrefixProvisionError{Step: "step", Err: err}
	var throughWrapper *VariableMismatchError
	if !errors.As(wrapped, &throughWrapper) {
		t.Error("the cause should survive the provisioning wrapper")
	}
}
