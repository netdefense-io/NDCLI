package models

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// SoftwarePolicy represents a reusable package inventory policy attached
// to templates. Content is a JSON document with `present` and `absent`
// string arrays; it's transported as an opaque string here so the CLI
// can render it as the user wrote it and the MCP can hand it back to the
// model without an intermediate Go shape rebuilding the JSON.
type SoftwarePolicy struct {
	Name         string       `json:"name"`
	Content      string       `json:"content,omitempty"`
	Organization string       `json:"organization_name,omitempty"`
	// TemplateNames is the list of templates this policy is currently
	// attached to. NDManager populates it on the single-policy GET
	// (describe) only — list endpoints omit it to avoid the JOIN on
	// every row. Nil here means "not loaded" (e.g. list response); an
	// empty slice means "explicitly not attached anywhere".
	TemplateNames []string     `json:"template_names,omitempty"`
	CreatedAt     FlexibleTime `json:"created_at"`
	UpdatedAt     FlexibleTime `json:"updated_at"`
}

// SoftwarePolicyListResponse mirrors the API's paginated list shape.
type SoftwarePolicyListResponse struct {
	Items []SoftwarePolicy `json:"items"`
	Total int              `json:"total"`
}

// EmptySoftwarePolicyContent is the canonical empty document — what
// `ndcli software create` produces when no content is supplied.
const EmptySoftwarePolicyContent = `{"present":[],"absent":[]}`

// SoftwarePolicyContent is the parsed form of the JSON document carried
// in SoftwarePolicy.Content. CLI mutation verbs (require/block/waive)
// work against this shape; the Marshal() method renders back to the
// canonical JSON the server stores.
type SoftwarePolicyContent struct {
	Present []string `json:"present"`
	Absent  []string `json:"absent"`

	// extra carries every top-level key this CLI version does not
	// model, byte-for-byte as the server sent it, so Marshal can hand
	// them back untouched. Forward compatibility is the point: an
	// older CLI pointed at a newer server must degrade to "cannot edit
	// the new fields", never "destroys them on the next
	// require-package". Deliberately unexported and untagged — these
	// keys are pass-through cargo, not something callers should reach
	// into.
	extra map[string]json.RawMessage

	// Repositories and External are optional. Nil means the document
	// does not carry the key at all, and Marshal will not invent it —
	// a policy written before this feature existed must round-trip
	// byte-identical.
	Repositories []Repository      `json:"repositories,omitempty"`
	External     []ExternalPackage `json:"external,omitempty"`
}

// Repository is one custom pkg(8) repository the policy asks NDAgent to
// configure on the device before any package work happens.
type Repository struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	// Priority orders repositories against each other. It is not a
	// safety boundary — pkg still installs the highest version it can
	// see, whatever the priority — so NDAgent additionally refuses an
	// install when more than one configured repository offers the same
	// package name.
	Priority int  `json:"priority"`
	Enabled  bool `json:"enabled"`
	// Signature is required by the server. `type: none` is accepted
	// only for organizations with allow_unverified_repos set.
	Signature RepositorySignature `json:"signature"`

	extra map[string]json.RawMessage
}

// RepositorySignature carries the trust configuration for a repository.
// Exactly one of Fingerprints / Pubkey is meaningful, selected by Type
// ("fingerprints", "pubkey", "none"). Fingerprint and key material is
// carried inline by value — never as a path to a file on the device,
// which the operator has no way to place there.
type RepositorySignature struct {
	Type         string                  `json:"type"`
	Fingerprints []RepositoryFingerprint `json:"fingerprints,omitempty"`
	Pubkey       string                  `json:"pubkey,omitempty"`

	extra map[string]json.RawMessage
}

// RepositoryFingerprint is one trusted-key fingerprint.
type RepositoryFingerprint struct {
	Function    string `json:"function"`
	Fingerprint string `json:"fingerprint"`

	extra map[string]json.RawMessage
}

// ExternalPackage is a package installed straight from a URL with
// `pkg add`, for software that lives outside any repository.
type ExternalPackage struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	URL     string `json:"url"`
	// Force maps to `pkg add -f` — reinstall even when pkg already
	// considers the package installed.
	Force bool `json:"force"`

	extra map[string]json.RawMessage
}

// splitExtra returns the top-level keys of `raw` that are not in
// `modeled`, with their values as the server sent them. Used at every
// level of the document so an older CLI cannot delete a field it
// simply does not know about yet.
func splitExtra(raw []byte, modeled ...string) map[string]json.RawMessage {
	var all map[string]json.RawMessage
	if err := json.Unmarshal(raw, &all); err != nil {
		return nil
	}
	for _, k := range modeled {
		delete(all, k)
	}
	if len(all) == 0 {
		return nil
	}
	return all
}

// mergeExtra renders v and splices the carried-through keys back in,
// alphabetically, after the modeled ones. Splicing the raw bytes keeps
// the values byte-identical rather than re-encoding them.
func mergeExtra(v any, extra map[string]json.RawMessage) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	if len(extra) == 0 {
		return b, nil
	}
	keys := make([]string, 0, len(extra))
	for k := range extra {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := make([]byte, 0, len(b)+64*len(keys))
	out = append(out, b[:len(b)-1]...) // everything but the closing brace
	for _, k := range keys {
		keyJSON, err := json.Marshal(k)
		if err != nil {
			continue
		}
		if len(out) > 1 { // not "{" — something is already there
			out = append(out, ',')
		}
		out = append(out, keyJSON...)
		out = append(out, ':')
		out = append(out, extra[k]...)
	}
	return append(out, '}'), nil
}

// UnmarshalJSON defaults Enabled to true when the key is absent. The Go
// zero value would say false, which is the opposite of what the server
// means by an omitted `enabled`.
func (r *Repository) UnmarshalJSON(b []byte) error {
	type alias Repository
	tmp := alias{Enabled: true}
	if err := json.Unmarshal(b, &tmp); err != nil {
		return err
	}
	*r = Repository(tmp)
	r.extra = splitExtra(b, "name", "url", "priority", "enabled", "signature")
	return nil
}

// MarshalJSON always writes priority and enabled, even at their default
// values, so the stored document says what it means rather than relying
// on the reader to apply the same defaults.
func (r Repository) MarshalJSON() ([]byte, error) {
	type alias Repository
	return mergeExtra(alias(r), r.extra)
}

func (s *RepositorySignature) UnmarshalJSON(b []byte) error {
	type alias RepositorySignature
	var tmp alias
	if err := json.Unmarshal(b, &tmp); err != nil {
		return err
	}
	*s = RepositorySignature(tmp)
	s.extra = splitExtra(b, "type", "fingerprints", "pubkey")
	return nil
}

func (s RepositorySignature) MarshalJSON() ([]byte, error) {
	type alias RepositorySignature
	return mergeExtra(alias(s), s.extra)
}

func (f *RepositoryFingerprint) UnmarshalJSON(b []byte) error {
	type alias RepositoryFingerprint
	var tmp alias
	if err := json.Unmarshal(b, &tmp); err != nil {
		return err
	}
	*f = RepositoryFingerprint(tmp)
	f.extra = splitExtra(b, "function", "fingerprint")
	return nil
}

func (f RepositoryFingerprint) MarshalJSON() ([]byte, error) {
	type alias RepositoryFingerprint
	return mergeExtra(alias(f), f.extra)
}

func (e *ExternalPackage) UnmarshalJSON(b []byte) error {
	type alias ExternalPackage
	var tmp alias
	if err := json.Unmarshal(b, &tmp); err != nil {
		return err
	}
	*e = ExternalPackage(tmp)
	e.extra = splitExtra(b, "name", "version", "url", "force")
	return nil
}

func (e ExternalPackage) MarshalJSON() ([]byte, error) {
	type alias ExternalPackage
	return mergeExtra(alias(e), e.extra)
}

// ParseSoftwarePolicyContent is tolerant of missing keys and treats a
// nil array the same as an empty one — matches how the server-side
// validator accepts present-only or absent-only documents.
func ParseSoftwarePolicyContent(raw string) (*SoftwarePolicyContent, error) {
	if raw == "" {
		return &SoftwarePolicyContent{Present: []string{}, Absent: []string{}}, nil
	}
	var c SoftwarePolicyContent
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return nil, fmt.Errorf("parse software policy content: %w", err)
	}
	// Second pass over the same bytes to catch the keys the struct
	// above has no field for. It cannot fail if the first pass
	// succeeded on an object; a valid non-object document (a bare
	// array, say) already failed there.
	var all map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &all); err == nil {
		for k, v := range all {
			switch k {
			case "present", "absent", "repositories", "external":
				continue
			}
			if c.extra == nil {
				c.extra = make(map[string]json.RawMessage, len(all))
			}
			c.extra[k] = v
		}
	}
	if c.Present == nil {
		c.Present = []string{}
	}
	if c.Absent == nil {
		c.Absent = []string{}
	}
	return &c, nil
}

// Marshal renders the content back to compact JSON with sorted lists.
// Sorting is intentional: it makes diffs between successive
// `require-package` / `block-package` calls trivial to read, and it
// matches the deterministic-payload posture NDManager's sync merge
// already takes on the wire (alphabetical).
//
// Keys this CLI does not model (see extra) are re-emitted verbatim
// after `absent`, in alphabetical order. Order is fixed rather than
// incidental so two runs over the same document produce the same
// bytes — the document is a payload input downstream, and a stable
// rendering keeps diffs and any content comparison meaningful.
func (c *SoftwarePolicyContent) Marshal() string {
	present := append([]string{}, c.Present...)
	absent := append([]string{}, c.Absent...)
	sort.Strings(present)
	sort.Strings(absent)
	if present == nil {
		present = []string{}
	}
	if absent == nil {
		absent = []string{}
	}

	presentJSON, _ := json.Marshal(present)
	absentJSON, _ := json.Marshal(absent)

	var b bytes.Buffer
	b.WriteString(`{"present":`)
	b.Write(presentJSON)
	b.WriteString(`,"absent":`)
	b.Write(absentJSON)

	// Sorted by name so two runs over the same document produce the
	// same bytes; the entries are order-independent to the agent.
	if len(c.Repositories) > 0 {
		repos := append([]Repository{}, c.Repositories...)
		sort.Slice(repos, func(i, j int) bool { return repos[i].Name < repos[j].Name })
		if reposJSON, err := json.Marshal(repos); err == nil {
			b.WriteString(`,"repositories":`)
			b.Write(reposJSON)
		}
	}
	if len(c.External) > 0 {
		ext := append([]ExternalPackage{}, c.External...)
		sort.Slice(ext, func(i, j int) bool { return ext[i].Name < ext[j].Name })
		if extJSON, err := json.Marshal(ext); err == nil {
			b.WriteString(`,"external":`)
			b.Write(extJSON)
		}
	}

	extraKeys := make([]string, 0, len(c.extra))
	for k := range c.extra {
		extraKeys = append(extraKeys, k)
	}
	sort.Strings(extraKeys)
	for _, k := range extraKeys {
		keyJSON, err := json.Marshal(k)
		if err != nil {
			continue
		}
		b.WriteByte(',')
		b.Write(keyJSON)
		b.WriteByte(':')
		b.Write(c.extra[k])
	}
	b.WriteByte('}')
	return b.String()
}

// PackagePolicyState is where a package currently sits in a policy.
type PackagePolicyState string

const (
	PackageStateNone     PackagePolicyState = ""
	PackageStateRequired PackagePolicyState = "required"
	PackageStateBlocked  PackagePolicyState = "blocked"
)

// PackageActionOutcome describes what a require/block/waive call did
// to a single package. The CLI renders one line per outcome and the
// MCP returns the array verbatim so the LLM can summarize.
type PackageActionOutcome struct {
	Package string             `json:"package"`
	// Action is the user-visible verb: "required", "blocked", "waived",
	// "moved", or "no-change".
	Action string             `json:"action"`
	// From is the prior state when the action actually changed things —
	// useful for "Waived bash (was: required)" and move notices. Empty
	// when nothing changed or there was nothing prior.
	From PackagePolicyState `json:"from,omitempty"`
}

// Changed returns true when the underlying content was mutated. Callers
// use it to skip the PUT round-trip if every outcome was a no-op.
func (o PackageActionOutcome) Changed() bool {
	return o.Action != "no-change"
}

// Require ensures `pkg` is in the Present list. If it's currently in
// Absent, the package is moved (Absent → Present) and the outcome
// reports Action="moved", From="blocked". A package already in Present
// is a no-op.
func (c *SoftwarePolicyContent) Require(pkg string) PackageActionOutcome {
	if containsString(c.Present, pkg) {
		return PackageActionOutcome{Package: pkg, Action: "no-change", From: PackageStateRequired}
	}
	if containsString(c.Absent, pkg) {
		c.Absent = removeString(c.Absent, pkg)
		c.Present = append(c.Present, pkg)
		return PackageActionOutcome{Package: pkg, Action: "moved", From: PackageStateBlocked}
	}
	c.Present = append(c.Present, pkg)
	return PackageActionOutcome{Package: pkg, Action: "required"}
}

// Block ensures `pkg` is in the Absent list, symmetric to Require.
func (c *SoftwarePolicyContent) Block(pkg string) PackageActionOutcome {
	if containsString(c.Absent, pkg) {
		return PackageActionOutcome{Package: pkg, Action: "no-change", From: PackageStateBlocked}
	}
	if containsString(c.Present, pkg) {
		c.Present = removeString(c.Present, pkg)
		c.Absent = append(c.Absent, pkg)
		return PackageActionOutcome{Package: pkg, Action: "moved", From: PackageStateRequired}
	}
	c.Absent = append(c.Absent, pkg)
	return PackageActionOutcome{Package: pkg, Action: "blocked"}
}

// Waive removes `pkg` from whichever list it currently sits in. A
// package in neither list is a no-op (the outcome reports
// Action="no-change", From=""), which is intentional — the operator's
// intent is "I don't care about this package," and the policy already
// satisfies that.
func (c *SoftwarePolicyContent) Waive(pkg string) PackageActionOutcome {
	if containsString(c.Present, pkg) {
		c.Present = removeString(c.Present, pkg)
		return PackageActionOutcome{Package: pkg, Action: "waived", From: PackageStateRequired}
	}
	if containsString(c.Absent, pkg) {
		c.Absent = removeString(c.Absent, pkg)
		return PackageActionOutcome{Package: pkg, Action: "waived", From: PackageStateBlocked}
	}
	return PackageActionOutcome{Package: pkg, Action: "no-change"}
}

func containsString(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func removeString(s []string, v string) []string {
	out := make([]string, 0, len(s))
	for _, x := range s {
		if x != v {
			out = append(out, x)
		}
	}
	return out
}

// EntryActionOutcome describes what a repository/external mutation did
// to a single named entry. Deliberately separate from
// PackageActionOutcome: there is no present/absent axis here, so there
// is no "moved" and no From state.
type EntryActionOutcome struct {
	Name string `json:"name"`
	// Action is "added", "updated", "removed", or "no-change".
	Action string `json:"action"`
}

// Changed reports whether the content was mutated, so callers can skip
// the PUT round-trip.
func (o EntryActionOutcome) Changed() bool {
	return o.Action != "no-change"
}

// RepositoryPatch is a partial repository update. A nil field means
// "leave whatever is already there", which is what makes it safe to
// change one field of an existing entry without restating the rest —
// the entry is replaced wholesale on write, so anything the caller
// does not carry forward would be reset.
//
// On a new entry the nil fields take the wire-format defaults:
// priority 0 and enabled true. URL and Signature are required for a
// new entry, since neither has a sensible default.
type RepositoryPatch struct {
	Name      string
	URL       *string
	Priority  *int
	Enabled   *bool
	Signature *RepositorySignature
}

// ApplyRepositoryPatch resolves the patch against the entry that is
// already there (if any) and hands the result to SetRepository. It
// returns an error only when a new entry is missing something that has
// no default.
func (c *SoftwarePolicyContent) ApplyRepositoryPatch(patch RepositoryPatch) (EntryActionOutcome, error) {
	var (
		repo  Repository
		found bool
	)
	for _, existing := range c.Repositories {
		if existing.Name == patch.Name {
			repo, found = existing, true
			break
		}
	}
	if !found {
		repo = Repository{Name: patch.Name, Priority: 0, Enabled: true}
		if patch.URL == nil {
			return EntryActionOutcome{}, fmt.Errorf("repository url is required to add %q", patch.Name)
		}
		if patch.Signature == nil {
			return EntryActionOutcome{}, fmt.Errorf("signature configuration is required to add %q", patch.Name)
		}
	}
	if patch.URL != nil {
		repo.URL = *patch.URL
	}
	if patch.Priority != nil {
		repo.Priority = *patch.Priority
	}
	if patch.Enabled != nil {
		repo.Enabled = *patch.Enabled
	}
	if patch.Signature != nil {
		repo.Signature = *patch.Signature
	}
	return c.SetRepository(repo), nil
}

// ExternalPatch is the same idea for an external package. Version and
// URL are required on a new entry; force defaults to false.
type ExternalPatch struct {
	Name    string
	Version *string
	URL     *string
	Force   *bool
}

// ApplyExternalPatch resolves the patch against the existing entry and
// hands the result to SetExternal.
func (c *SoftwarePolicyContent) ApplyExternalPatch(patch ExternalPatch) (EntryActionOutcome, error) {
	var (
		pkg   ExternalPackage
		found bool
	)
	for _, existing := range c.External {
		if existing.Name == patch.Name {
			pkg, found = existing, true
			break
		}
	}
	if !found {
		pkg = ExternalPackage{Name: patch.Name}
		if patch.Version == nil {
			return EntryActionOutcome{}, fmt.Errorf("version is required to add external package %q", patch.Name)
		}
		if patch.URL == nil {
			return EntryActionOutcome{}, fmt.Errorf("url is required to add external package %q", patch.Name)
		}
	}
	if patch.Version != nil {
		pkg.Version = *patch.Version
	}
	if patch.URL != nil {
		pkg.URL = *patch.URL
	}
	if patch.Force != nil {
		pkg.Force = *patch.Force
	}
	return c.SetExternal(pkg), nil
}

// SetRepository adds `repo`, or replaces the existing entry with the
// same name. Name is the identity: NDAgent writes one file per
// repository name, so two entries sharing a name are a policy that
// fights itself. Setting values identical to what is already there is
// a no-op, which is what makes `add-repo` safe to re-run.
//
// This is a wholesale replace. Callers updating one field of an
// existing entry should go through ApplyRepositoryPatch rather than
// constructing a Repository from scratch and silently resetting
// everything they left out.
func (c *SoftwarePolicyContent) SetRepository(repo Repository) EntryActionOutcome {
	for i, existing := range c.Repositories {
		if existing.Name != repo.Name {
			continue
		}
		// Carry forward anything this CLI version doesn't model, so an
		// edit to a field we understand doesn't drop the ones we don't.
		repo.extra = existing.extra
		if repositoriesEqual(existing, repo) {
			return EntryActionOutcome{Name: repo.Name, Action: "no-change"}
		}
		c.Repositories[i] = repo
		return EntryActionOutcome{Name: repo.Name, Action: "updated"}
	}
	c.Repositories = append(c.Repositories, repo)
	return EntryActionOutcome{Name: repo.Name, Action: "added"}
}

// RemoveRepository drops the entry with the given name. Removing one
// that isn't there is a no-op — the operator's intent ("this policy
// should not configure that repository") is already satisfied.
func (c *SoftwarePolicyContent) RemoveRepository(name string) EntryActionOutcome {
	for i, existing := range c.Repositories {
		if existing.Name != name {
			continue
		}
		c.Repositories = append(c.Repositories[:i], c.Repositories[i+1:]...)
		if len(c.Repositories) == 0 {
			// Drop the key rather than leaving `"repositories":[]`.
			c.Repositories = nil
		}
		return EntryActionOutcome{Name: name, Action: "removed"}
	}
	return EntryActionOutcome{Name: name, Action: "no-change"}
}

// SetExternal adds `pkg`, or replaces the existing entry with the same
// name — including at a different version, which is an upgrade of that
// entry rather than a second one.
func (c *SoftwarePolicyContent) SetExternal(pkg ExternalPackage) EntryActionOutcome {
	for i, existing := range c.External {
		if existing.Name != pkg.Name {
			continue
		}
		pkg.extra = existing.extra
		if externalEqual(existing, pkg) {
			return EntryActionOutcome{Name: pkg.Name, Action: "no-change"}
		}
		c.External[i] = pkg
		return EntryActionOutcome{Name: pkg.Name, Action: "updated"}
	}
	c.External = append(c.External, pkg)
	return EntryActionOutcome{Name: pkg.Name, Action: "added"}
}

// RemoveExternal drops the external entry with the given name.
func (c *SoftwarePolicyContent) RemoveExternal(name string) EntryActionOutcome {
	for i, existing := range c.External {
		if existing.Name != name {
			continue
		}
		c.External = append(c.External[:i], c.External[i+1:]...)
		if len(c.External) == 0 {
			c.External = nil
		}
		return EntryActionOutcome{Name: name, Action: "removed"}
	}
	return EntryActionOutcome{Name: name, Action: "no-change"}
}

// repositoriesEqual compares the modeled fields only. Pass-through
// keys are copied from the existing entry before this is called, so
// they can never be the thing that makes two entries differ.
func repositoriesEqual(a, b Repository) bool {
	if a.Name != b.Name || a.URL != b.URL || a.Priority != b.Priority || a.Enabled != b.Enabled {
		return false
	}
	if a.Signature.Type != b.Signature.Type || a.Signature.Pubkey != b.Signature.Pubkey {
		return false
	}
	if len(a.Signature.Fingerprints) != len(b.Signature.Fingerprints) {
		return false
	}
	for i := range a.Signature.Fingerprints {
		if a.Signature.Fingerprints[i].Function != b.Signature.Fingerprints[i].Function ||
			a.Signature.Fingerprints[i].Fingerprint != b.Signature.Fingerprints[i].Fingerprint {
			return false
		}
	}
	return true
}

func externalEqual(a, b ExternalPackage) bool {
	return a.Name == b.Name && a.Version == b.Version && a.URL == b.URL && a.Force == b.Force
}

// knownFingerprintFunctions is the set of hash names a fingerprint may
// be prefixed with. The server currently restricts `function` to
// sha256; this list is what ParseFingerprint uses to tell a function
// prefix from the first byte of a colon-separated hex digest.
var knownFingerprintFunctions = map[string]string{"sha256": "sha256"}

// ParseFingerprint accepts the shapes an operator is likely to paste:
//
//	sha256:a1b2…          → {sha256, a1b2…}
//	a1b2…                 → {sha256, a1b2…}
//	A1:B2:C3:…            → {sha256, a1b2c3…}
//	sha256:A1:B2:C3:…     → {sha256, a1b2c3…}
//
// Splitting on the first colon unconditionally would mangle the
// colon-separated display format — "A1:B2:C3" would come out as
// function "A1". So a prefix is only treated as a function name when
// it actually is one; otherwise the whole string is the digest.
// Separator colons and surrounding space are stripped, and the hex is
// lowercased, because the wire format wants bare lowercase hex. Whether
// the result is a *valid* digest is the server's call.
func ParseFingerprint(raw string) (RepositoryFingerprint, error) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return RepositoryFingerprint{}, fmt.Errorf("fingerprint is required")
	}

	function := "sha256"
	if idx := strings.Index(text, ":"); idx >= 0 {
		if named, ok := knownFingerprintFunctions[strings.ToLower(strings.TrimSpace(text[:idx]))]; ok {
			function = named
			text = text[idx+1:]
		}
	}

	digest := strings.ToLower(strings.Join(strings.Split(strings.TrimSpace(text), ":"), ""))
	digest = strings.ReplaceAll(digest, " ", "")
	if digest == "" {
		return RepositoryFingerprint{}, fmt.Errorf("malformed fingerprint %q: expected sha256:<hex>", raw)
	}
	return RepositoryFingerprint{Function: function, Fingerprint: digest}, nil
}
