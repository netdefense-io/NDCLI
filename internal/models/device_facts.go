package models

import (
	"fmt"
	"sort"
	"strings"
)

// Device facts are agent-reported, informational, and optional.
//
// NDAgent collects a small set of durable facts about the box it runs on
// (timezone, interface roles, OPNsense/OS version, hostname) and ships them
// over the authenticated WebSocket; NDBroker stores them and NDManager echoes
// them back on the device payload as `facts`. They are held here as a
// `map[string]any` rather than a struct on purpose, following the same
// "unknown keys are cargo, not garbage" rule as SoftwarePolicyContent: a
// newer agent may report a fact this binary has never heard of, and the JSON
// formatter must still print it instead of silently dropping it. The typed
// accessors below pull out the keys this binary knows how to render, and
// every one of them tolerates the key being absent, null, or the wrong JSON
// type — facts arrive over an unsigned channel and nothing here may panic or
// fail a command because a device reported nonsense.
//
// Facts are never authoritative. They are not used to authorize anything and
// never silently convert a user's input; the scheduling tools show the
// device's timezone alongside the requested one and warn on a mismatch, they
// do not adopt it.

// DeviceTimezoneFact is the device's own configured timezone, as reported.
type DeviceTimezoneFact struct {
	// Name is an IANA timezone name, e.g. "America/Sao_Paulo".
	Name string
	// UTCOffsetSec is the offset from UTC in seconds at collection time.
	UTCOffsetSec int
	// Abbrev is the zone abbreviation at collection time, e.g. "-03".
	Abbrev string
}

// Display renders the timezone the way every surface shows it:
// "America/Sao_Paulo (UTC-03:00)".
func (t DeviceTimezoneFact) Display() string {
	return fmt.Sprintf("%s (%s)", t.Name, FormatUTCOffset(t.UTCOffsetSec))
}

// DeviceInterfaceFact is one entry of the device's interface assignment:
// the OPNsense role ("wan", "lan", "opt1") and the physical interface bound
// to it.
type DeviceInterfaceFact struct {
	Role    string
	If      string
	Descr   string
	Enabled bool
}

// DeviceOSFact is the device's operating system identity.
type DeviceOSFact struct {
	Platform string
	Version  string
}

// Display renders "FreeBSD 15.0-RELEASE", or whichever half is present.
func (o DeviceOSFact) Display() string {
	switch {
	case o.Platform != "" && o.Version != "":
		return o.Platform + " " + o.Version
	case o.Platform != "":
		return o.Platform
	default:
		return o.Version
	}
}

// DeviceOPNsenseFact is the OPNsense firmware identity.
type DeviceOPNsenseFact struct {
	Version string
	Series  string
}

// HasFacts reports whether the device carries any agent-reported facts at
// all. An older agent that does not report them yet leaves the map nil, and
// every surface then renders exactly what it rendered before this existed.
func (d *Device) HasFacts() bool {
	return len(d.Facts) > 0
}

// TimezoneFact returns the device's reported timezone. ok is false when the
// device reports no timezone, or reports one without a usable IANA name —
// an offset with no name cannot be handed back to time.LoadLocation and is
// not worth rendering on its own.
func (d *Device) TimezoneFact() (DeviceTimezoneFact, bool) {
	obj, ok := factObject(d.Facts, "timezone")
	if !ok {
		return DeviceTimezoneFact{}, false
	}
	name := factString(obj, "name")
	if name == "" {
		return DeviceTimezoneFact{}, false
	}
	return DeviceTimezoneFact{
		Name:         name,
		UTCOffsetSec: factInt(obj, "utc_offset_sec"),
		Abbrev:       factString(obj, "abbrev"),
	}, true
}

// InterfaceFacts returns the device's reported interface assignments, in the
// order the device reported them. Entries that carry neither a role nor an
// interface name are dropped: there is nothing to show for them.
func (d *Device) InterfaceFacts() []DeviceInterfaceFact {
	raw, ok := d.Facts["interfaces"].([]interface{})
	if !ok {
		return nil
	}
	out := make([]DeviceInterfaceFact, 0, len(raw))
	for _, item := range raw {
		obj, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		iface := DeviceInterfaceFact{
			Role:  factString(obj, "role"),
			If:    factString(obj, "if"),
			Descr: factString(obj, "descr"),
			// Absent means enabled: only an explicit false marks an
			// interface down. Reading a missing key as false would
			// print "(disabled: wan, lan)" for an agent that simply
			// does not report the flag.
			Enabled: factBoolDefault(obj, "enabled", true),
		}
		if iface.Role == "" && iface.If == "" {
			continue
		}
		out = append(out, iface)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// OPNsenseFact returns the reported OPNsense firmware identity.
func (d *Device) OPNsenseFact() (DeviceOPNsenseFact, bool) {
	obj, ok := factObject(d.Facts, "opnsense")
	if !ok {
		return DeviceOPNsenseFact{}, false
	}
	f := DeviceOPNsenseFact{
		Version: factString(obj, "version"),
		Series:  factString(obj, "series"),
	}
	if f.Version == "" && f.Series == "" {
		return DeviceOPNsenseFact{}, false
	}
	return f, true
}

// OSFact returns the reported operating system identity.
func (d *Device) OSFact() (DeviceOSFact, bool) {
	obj, ok := factObject(d.Facts, "os")
	if !ok {
		return DeviceOSFact{}, false
	}
	f := DeviceOSFact{
		Platform: factString(obj, "platform"),
		Version:  factString(obj, "version"),
	}
	if f.Platform == "" && f.Version == "" {
		return DeviceOSFact{}, false
	}
	return f, true
}

// HostnameFact returns the device's reported hostname.
func (d *Device) HostnameFact() (string, bool) {
	h := factString(d.Facts, "hostname")
	return h, h != ""
}

// InterfacesSummary renders the interface list as the one compact line every
// human-facing formatter prints:
//
//	wan=em0 lan=em1 opt1=em2 (disabled: opt1)
//
// Disabled interfaces stay in the main list — a role that exists but is off
// is still an assignment the reader is looking for — and are named once in
// the trailing note so the line does not need a column per flag. Returns ""
// when the device reported no interfaces.
func (d *Device) InterfacesSummary() string {
	ifaces := d.InterfaceFacts()
	if len(ifaces) == 0 {
		return ""
	}
	pairs := make([]string, 0, len(ifaces))
	var disabled []string
	for _, i := range ifaces {
		name := i.Role
		if name == "" {
			name = i.If
			pairs = append(pairs, name)
		} else if i.If == "" {
			pairs = append(pairs, name)
		} else {
			pairs = append(pairs, name+"="+i.If)
		}
		if !i.Enabled {
			disabled = append(disabled, name)
		}
	}
	line := strings.Join(pairs, " ")
	if len(disabled) > 0 {
		line += fmt.Sprintf(" (disabled: %s)", strings.Join(disabled, ", "))
	}
	return line
}

// UnknownFactKeys lists the top-level fact keys this binary has no renderer
// for, sorted. They still reach a JSON consumer untouched — this only tells a
// human-facing formatter that the device reported something newer than the
// CLI knows how to lay out.
func (d *Device) UnknownFactKeys() []string {
	if len(d.Facts) == 0 {
		return nil
	}
	known := map[string]bool{
		"v": true, "hash": true, "timezone": true, "interfaces": true,
		"opnsense": true, "os": true, "hostname": true,
	}
	var out []string
	for k := range d.Facts {
		if !known[k] {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// FormatUTCOffset renders a seconds-from-UTC offset as "UTC-03:00".
//
// It is written out rather than borrowed from time.Format because the offset
// arrives as a bare integer from the device, with no instant attached.
func FormatUTCOffset(sec int) string {
	sign := "+"
	if sec < 0 {
		sign = "-"
		sec = -sec
	}
	return fmt.Sprintf("UTC%s%02d:%02d", sign, sec/3600, (sec%3600)/60)
}

// factObject reads one sub-object out of a facts map, tolerating a missing
// key, a null, or a value of the wrong type.
func factObject(m map[string]interface{}, key string) (map[string]interface{}, bool) {
	if m == nil {
		return nil, false
	}
	obj, ok := m[key].(map[string]interface{})
	if !ok || len(obj) == 0 {
		return nil, false
	}
	return obj, true
}

func factString(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	s, _ := m[key].(string)
	return strings.TrimSpace(s)
}

// factInt reads an integer fact. JSON numbers decode to float64 through
// map[string]interface{}, so that is the type actually checked; the int and
// int64 cases are there for a facts map built in Go rather than decoded
// (tests, and the TUI's fixtures).
func factInt(m map[string]interface{}, key string) int {
	if m == nil {
		return 0
	}
	switch v := m[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	default:
		return 0
	}
}

// factBoolDefault reads a boolean fact, returning def when the key is
// absent, null, or not a bool.
func factBoolDefault(m map[string]interface{}, key string, def bool) bool {
	if m == nil {
		return def
	}
	b, ok := m[key].(bool)
	if !ok {
		return def
	}
	return b
}
