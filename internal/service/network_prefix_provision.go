package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/models"
)

// CodeVariableValueMismatch is returned when a variable the caller wants to
// provision already exists holding a different value. It has its own code so
// each front-end can name its own way forward — `ndcli variable org set` in
// the CLI, the variable_set tool over MCP — while the message stays
// surface-neutral. See VariableMismatchHint.
const CodeVariableValueMismatch = "VARIABLE_VALUE_MISMATCH"

// VariableMismatchHint is the placeholder each front-end replaces with its own
// way of changing the value. The service cannot name a CLI flag or an MCP tool
// without being wrong on the other surface, so it names neither and leaves a
// marker — the same split CodeDestinationExists and OverwriteHint use.
const VariableMismatchHint = "<change-it-with>"

// cidrSeparator is the separator NDManager splits a prefix variable's value
// on. Confirmed against explode_prefix_cidrs in NDManager's
// src/services/vpn_renderer.py, which splits on "," only and strips
// surrounding whitespace per element. Space- and newline-separated values are
// NOT split — they become a single nonsense element.
const cidrSeparator = ","

// NetworkPrefixProvisionResult reports what NetworkPrefixProvision actually
// did, step by step, so a caller can tell a fresh provision from a re-run
// that found everything already in place.
type NetworkPrefixProvisionResult struct {
	Prefix                *models.VpnMemberPrefix
	Value                 string
	OrgVariableCreated    bool
	DeviceVariableCreated bool
	PrefixCreated         bool
}

// NetworkPrefixProvisionError reports a failure partway through the three
// writes, naming the ones that already completed. There is no transaction
// spanning them (confirmed against NDManager: three separate endpoints, no
// bulk or transactional variant), so the honest thing is to say exactly where
// it stopped rather than to imply nothing happened.
type NetworkPrefixProvisionError struct {
	Step      string
	Completed []string
	Err       error
}

func (e *NetworkPrefixProvisionError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s failed: %v", e.Step, e.Err)
	if len(e.Completed) > 0 {
		b.WriteString("\n\nAlready completed, and left in place:")
		for _, c := range e.Completed {
			fmt.Fprintf(&b, "\n  • %s", c)
		}
		b.WriteString("\n\nRe-run the same command once the cause is fixed: the completed steps are")
		b.WriteString("\nrecognised as already done and it will continue from where it stopped.")
	}
	return b.String()
}

func (e *NetworkPrefixProvisionError) Unwrap() error { return e.Err }

// NormalizePrefixCIDRs validates a CIDR list and renders it in the form
// NDManager stores.
//
// NDManager validates none of this: the value column takes any string up to
// 64 KiB, and whatever is stored goes verbatim into the peer's AllowedIPs.
// A malformed entry is not rejected anywhere in the control plane — it fails
// at WireGuard, on the device. Worse, a value that splits to nothing (" ",
// ",,") yields an empty CIDR list, which renders a peer carrying only its own
// overlay /32: a tunnel that comes up and silently carries no traffic. So the
// check belongs here, before any write.
func NormalizePrefixCIDRs(cidrs []string) (string, error) {
	var cleaned []string
	seen := map[string]bool{}

	for _, raw := range cidrs {
		// Accept a comma-separated argument as well as a repeated flag: the
		// stored form is comma-separated, so both read naturally.
		for _, part := range strings.Split(raw, cidrSeparator) {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			ip, ipnet, err := net.ParseCIDR(part)
			if err != nil {
				return "", &Error{
					Code:    CodeInvalidInput,
					Message: fmt.Sprintf("%q is not a valid CIDR block (expected a form like 10.0.0.0/24 or 2001:db8::/32)", part),
				}
			}
			// net.ParseCIDR accepts host bits — it returns the address and the
			// network separately and complains about neither, so "10.20.0.5/24"
			// parses cleanly and would be stored exactly as typed. That is the
			// same shape of failure this validation exists for: syntactically
			// valid, almost certainly a typo, and nothing downstream objects
			// before WireGuard sees it.
			//
			// Rejected rather than silently normalised: both plausible
			// intentions are one keystroke away from each other and rewriting
			// the value would hide which one was applied.
			if !ip.Equal(ipnet.IP) {
				return "", &Error{
					Code: CodeInvalidInput,
					Message: fmt.Sprintf(
						"%q has host bits set; write %s for the whole subnet, or %s/%d for just that address",
						part, ipnet.String(), ip.String(), hostMaskBits(ip)),
				}
			}
			// Store the canonical rendering, not the text as typed. IPv6 has
			// several valid spellings of one network — hex case, "::" against
			// explicit zero groups — and ensurePrefixVariable compares values
			// with string equality. Storing "2001:0DB8::/32" and re-running
			// later with "2001:db8::/32" would compare unequal and refuse as a
			// mismatch, breaking the "same value = already done" property the
			// whole resumable design rests on: a user following the documented
			// re-run recovery would hit a refusal they could not act on.
			//
			// This is normalisation, not the rewrite rejected above for host
			// bits. Every spelling here denotes the same network, so nothing
			// about the user's intent is being guessed at; "10.20.0.5/24" and
			// "10.20.0.0/24" denote different things, which is why that one is
			// refused instead.
			canonical := ipnet.String()
			if seen[canonical] {
				continue
			}
			seen[canonical] = true
			cleaned = append(cleaned, canonical)
		}
	}

	if len(cleaned) == 0 {
		return "", &Error{
			Code:    CodeInvalidInput,
			Message: "no CIDR blocks were given; a prefix variable with an empty value publishes nothing and leaves a tunnel that carries no traffic",
		}
	}
	return strings.Join(cleaned, cidrSeparator), nil
}

// hostMaskBits returns the single-address prefix length for ip: 32 for IPv4,
// 128 for IPv6.
func hostMaskBits(ip net.IP) int {
	if ip.To4() != nil {
		return 32
	}
	return 128
}

// NetworkPrefixProvision creates the org-scope variable, creates the
// device-scope override, and publishes the prefix — the three writes a user
// otherwise issues by hand.
//
// It never overwrites an existing value. An existing variable whose value
// already matches is treated as that step being done, which is what makes the
// whole operation safe to re-run after a partial failure; an existing variable
// holding something else is refused, because an org-scope variable is the
// inheritance root for every device under it and this command has no mandate
// to change one.
func (s *Service) NetworkPrefixProvision(ctx context.Context, org, vpnName, deviceName, variableName string, cidrs []string, publish *bool) (*NetworkPrefixProvisionResult, error) {
	if vpnName == "" || deviceName == "" || variableName == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "vpn network, device, and variable names are required"}
	}

	value, err := NormalizePrefixCIDRs(cidrs)
	if err != nil {
		return nil, err
	}

	result := &NetworkPrefixProvisionResult{Value: value}
	var completed []string

	// Step 1 — the org-scope definition. A device-scope variable can only
	// exist as an override of one, so this has to come first.
	description := fmt.Sprintf("Prefix published on VPN %q by %q", vpnName, deviceName)

	created, err := s.ensurePrefixVariable(ctx, VarScopeOrg, org, "", variableName, value, description)
	if err != nil {
		return nil, &NetworkPrefixProvisionError{Step: "creating the organization-scope variable", Err: err}
	}
	result.OrgVariableCreated = created
	if created {
		completed = append(completed, fmt.Sprintf("organization-scope variable %q created with value %q", variableName, value))
	}

	// Step 2 — the device override the prefix actually points at.
	created, err = s.ensurePrefixVariable(ctx, VarScopeDevice, org, deviceName, variableName, value, description)
	if err != nil {
		return nil, &NetworkPrefixProvisionError{
			Step:      fmt.Sprintf("creating the device-scope override on %q", deviceName),
			Completed: completed,
			Err:       err,
		}
	}
	result.DeviceVariableCreated = created
	if created {
		completed = append(completed, fmt.Sprintf("device-scope override on %q created with value %q", deviceName, value))
	}

	// Step 3 — publish.
	prefix, err := s.NetworkPrefixAdd(ctx, org, vpnName, deviceName, variableName, publish)
	if err != nil {
		if isAlreadyExists(err) {
			// Everything this command sets up is in place; re-running after a
			// partial failure lands here. Read the prefix back so the caller
			// can report its real publish flag — the one it already had, which
			// need not match what this invocation asked for.
			//
			// If that read fails, result.Prefix stays nil and the caller must
			// say the state is unconfirmed rather than assume: claiming a
			// publish flag nobody looked at is the same failure as reporting a
			// command finished when it was still running.
			existing, getErr := s.networkPrefixGet(ctx, org, vpnName, deviceName, variableName)
			if getErr == nil {
				result.Prefix = existing
			}
			return result, nil
		}
		return nil, &NetworkPrefixProvisionError{
			Step:      fmt.Sprintf("publishing the prefix on %q", vpnName),
			Completed: completed,
			Err:       err,
		}
	}
	result.Prefix = prefix
	result.PrefixCreated = true
	return result, nil
}

// ensurePrefixVariable makes sure a variable exists at scope holding value,
// creating it if absent. It reports whether it created one. An existing
// variable with a different value is an error, never an overwrite.
func (s *Service) ensurePrefixVariable(ctx context.Context, scope VariableScope, org, entity, name, value, description string) (bool, error) {
	existing, err := s.VariableGet(ctx, scope, org, entity, name)
	if err == nil && existing != nil {
		if existing.Value == value {
			return false, nil
		}
		return false, variableMismatch(scope, entity, name, existing.Value, value)
	}
	if err != nil && !isNotFound(err) {
		return false, err
	}

	if _, err := s.VariableCreate(ctx, scope, org, entity, VariableCreateOpts{
		Name:  name,
		Value: value,
		// Name the network and device: someone auditing variables later finds
		// these without a scope or an owner otherwise, and a bare "VPN
		// published prefix" does not say which VPN.
		Description: description,
	}); err != nil {
		if !isAlreadyExists(err) {
			return false, err
		}
		// Someone created it between the read above and this write. Resolve it
		// the same way as if it had been there all along rather than failing a
		// sequence that is otherwise safe to re-run.
		raced, getErr := s.VariableGet(ctx, scope, org, entity, name)
		if getErr != nil {
			return false, err
		}
		if raced.Value != value {
			return false, variableMismatch(scope, entity, name, raced.Value, value)
		}
		return false, nil
	}
	return true, nil
}

// VariableMismatchError carries the details of a refused overwrite as fields.
//
// The scope is a field rather than something a front-end parses back out of
// the rendered sentence: a message reworded, or another path reusing
// CodeVariableValueMismatch with different phrasing, would break substring
// sniffing silently and at run time. NDManager carries an open issue for
// exactly that anti-pattern in its own routers; there is no reason to import
// it here.
//
// It is returned as the cause inside a *Error so the code-based handling every
// other caller relies on is unchanged.
type VariableMismatchError struct {
	Scope    VariableScope
	Entity   string // device name at device scope; empty at org scope
	Name     string
	Existing string
	Want     string
}

func (e *VariableMismatchError) Error() string {
	where := "organization scope"
	if e.Scope == VarScopeDevice {
		where = fmt.Sprintf("device %q", e.Entity)
	}
	return fmt.Sprintf(
		"variable %q already exists at %s with value %q, and this command will not overwrite it (requested %q). %s",
		e.Name, where, e.Existing, e.Want, VariableMismatchHint)
}

// variableMismatch builds the refusal, keeping the structured cause reachable
// through errors.As while the *Error keeps the code MCP switches on.
func variableMismatch(scope VariableScope, entity, name, existing, want string) error {
	cause := &VariableMismatchError{
		Scope: scope, Entity: entity, Name: name, Existing: existing, Want: want,
	}
	return &Error{
		Code:    CodeVariableValueMismatch,
		Message: cause.Error(),
		Err:     cause,
	}
}

// prefixScanPages bounds networkPrefixGet's paging, mirroring the bounded scan
// the attachment download uses.
const (
	prefixScanPages   = 20
	prefixScanPerPage = 100
)

// networkPrefixGet fetches a single prefix by variable name. There is no
// single-prefix endpoint, so it pages the member's list.
//
// Hitting the page bound is reported as "not in the first N", never as "not on
// this member": the scan never established the second.
func (s *Service) networkPrefixGet(ctx context.Context, org, vpnName, deviceName, variableName string) (*models.VpnMemberPrefix, error) {
	scanned := 0
	for page := 1; page <= prefixScanPages; page++ {
		result, err := s.NetworkPrefixList(ctx, org, vpnName, deviceName, page, prefixScanPerPage)
		if err != nil {
			return nil, err
		}
		for i := range result.Prefixes {
			if result.Prefixes[i].VariableName == variableName {
				return &result.Prefixes[i], nil
			}
		}
		scanned += len(result.Prefixes)
		if len(result.Prefixes) < prefixScanPerPage || scanned >= result.Total {
			return nil, &Error{
				Code:    CodeAPIError,
				Message: fmt.Sprintf("prefix %q not found on %q", variableName, deviceName),
			}
		}
	}
	return nil, &Error{
		Code:    CodeAPIError,
		Message: fmt.Sprintf("prefix %q not found in the first %d prefixes on %q", variableName, scanned, deviceName),
	}
}

// isNotFound reports whether err came back as a 404 from the API.
func isNotFound(err error) bool {
	var apiErr *api.APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound
}

// isAlreadyExists reports whether err is NDManager's duplicate-resource
// conflict, and nothing else.
//
// Matched on the code alone. A bare 409 is not enough: this answer decides
// that a write failed only because the thing was already there, so the caller
// treats the step as done and moves on. NDManager models other 409 codes
// (VARIABLE_CONFLICT among them), and accepting any conflict would turn one of
// those into a silent success. That is the same failure the tri-state Published
// exists to avoid — asserting a state nobody examined.
//
// NDManager sets the code on all three endpoints this package writes to
// (confirmed against its variables and vpn_networks routers), so there is
// nothing to fall back to.
func isAlreadyExists(err error) bool {
	var apiErr *api.APIError
	return errors.As(err, &apiErr) && apiErr.Code == alreadyExistsCode
}

// alreadyExistsCode is NDManager's machine-readable duplicate-resource code.
const alreadyExistsCode = "ALREADY_EXISTS"
