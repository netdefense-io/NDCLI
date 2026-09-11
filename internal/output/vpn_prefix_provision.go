package output

import "fmt"

// VpnPrefixProvision reports what `ndcli network prefix add --value` did.
//
// Declared here rather than reusing the service result so internal/output
// keeps its existing dependencies: it knows internal/models and nothing else
// of the application layer.
//
// The Created flags matter to the reader: re-running the command after a
// partial failure is the documented recovery, so "created" and "was already
// there" have to be distinguishable rather than both rendering as success.
type VpnPrefixProvision struct {
	Network               string
	Device                string
	Variable              string
	Value                 string
	OrgVariableCreated    bool
	DeviceVariableCreated bool
	PrefixCreated         bool
	// Published is nil when the prefix was already there and reading it back
	// failed, so its publish flag was never observed. Rendering a value we did
	// not confirm would be the same failure as reporting a command finished
	// while it was still running.
	Published *bool
}

// headline renders the summary line.
//
// Conditional for the same reason steps() is: "publish" names both the act of
// creating the prefix and the flag deciding whether peers see it, so a fixed
// "published" header sits above "publish=false — not advertised to peers" and
// contradicts it. Only a prefix this call actually created was published by
// it.
//
// The wording matches internal/mcp's provisionMessage deliberately. The two
// renderers stay separate — one is human prose, the other a machine surface —
// but a user reading both should not have to wonder whether they describe the
// same thing.
func (p VpnPrefixProvision) headline() string {
	if p.PrefixCreated {
		return fmt.Sprintf("Prefix %s (%s) published on %s in %s", p.Variable, p.Value, p.Device, p.Network)
	}
	return fmt.Sprintf("Prefix %s (%s) already exists on %s in %s", p.Variable, p.Value, p.Device, p.Network)
}

// publishNote renders the trailing caveat, if there is one: the prefix exists
// but is not advertised, or its publish flag was never read back. label is the
// format's own prefix and applies to both, so an unknown state is not quieter
// than a known-false one.
func (p VpnPrefixProvision) publishNote(label string) string {
	switch {
	case p.Published == nil:
		return label + "the prefix already existed; its publish flag could not be read back and is unknown.\n"
	case !*p.Published:
		return label + "publish=false — the prefix exists but is not advertised to peers.\n"
	default:
		return ""
	}
}

// steps renders one line per step, saying whether this run did it.
//
// The wording deliberately avoids "published" for the already-there case.
// "publish" names two different things here — the act of creating the prefix
// and the flag that decides whether peers see it — so "already published"
// sitting above "publish=false, not advertised to peers" reads as a
// contradiction to anyone who does not already know both senses. "already
// exists" says only what was established.
func (p VpnPrefixProvision) steps() []string {
	mark := func(created bool, did, already string) string {
		if created {
			return did
		}
		return already
	}
	return []string{
		mark(p.OrgVariableCreated,
			fmt.Sprintf("created organization-scope variable %q = %s", p.Variable, p.Value),
			fmt.Sprintf("organization-scope variable %q already had this value", p.Variable)),
		mark(p.DeviceVariableCreated,
			fmt.Sprintf("created device-scope override on %q", p.Device),
			fmt.Sprintf("device-scope override on %q already had this value", p.Device)),
		mark(p.PrefixCreated,
			fmt.Sprintf("published the prefix on %q", p.Network),
			fmt.Sprintf("prefix already exists on %q", p.Network)),
	}
}

// FormatVpnPrefixProvisioned renders the provisioning summary (table).
func (f *TableFormatter) FormatVpnPrefixProvisioned(p VpnPrefixProvision) error {
	ColorSuccess.Fprintf(f.Writer, "✓ ")
	fmt.Fprintf(f.Writer, "%s\n", p.headline())
	for _, step := range p.steps() {
		fmt.Fprintf(f.Writer, "  • %s\n", step)
	}
	fmt.Fprint(f.Writer, p.publishNote("  Note: "))
	return nil
}

// FormatVpnPrefixProvisioned renders the provisioning summary (simple).
func (f *SimpleFormatter) FormatVpnPrefixProvisioned(p VpnPrefixProvision) error {
	fmt.Fprintf(f.Writer, "%s\n", p.headline())
	for _, step := range p.steps() {
		fmt.Fprintf(f.Writer, "  - %s\n", step)
	}
	fmt.Fprint(f.Writer, p.publishNote("  "))
	return nil
}

// FormatVpnPrefixProvisioned renders the provisioning summary (detailed).
func (f *DetailedFormatter) FormatVpnPrefixProvisioned(p VpnPrefixProvision) error {
	ColorSuccess.Fprintf(f.Writer, "✓ ")
	fmt.Fprintf(f.Writer, "%s\n", p.headline())
	for _, step := range p.steps() {
		fmt.Fprintf(f.Writer, "  • %s\n", step)
	}
	fmt.Fprint(f.Writer, p.publishNote("  Note: "))
	return nil
}

// FormatVpnPrefixProvisioned renders the provisioning summary as JSON.
func (f *JSONFormatter) FormatVpnPrefixProvisioned(p VpnPrefixProvision) error {
	return f.output(map[string]interface{}{
		"network":                 p.Network,
		"device":                  p.Device,
		"variable":                p.Variable,
		"value":                   p.Value,
		"org_variable_created":    p.OrgVariableCreated,
		"device_variable_created": p.DeviceVariableCreated,
		"prefix_created":          p.PrefixCreated,
		// null rather than a guess when the flag was never read back.
		"published": p.Published,
	})
}
