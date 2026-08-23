package models

import (
	"encoding/json"
	"testing"
)

func parsed(t *testing.T, raw string) *SoftwarePolicyContent {
	t.Helper()
	c, err := ParseSoftwarePolicyContent(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return c
}

func TestParse_Empty(t *testing.T) {
	c := parsed(t, "")
	if len(c.Present) != 0 || len(c.Absent) != 0 {
		t.Fatalf("empty parse: %+v", c)
	}
}

func TestParse_MissingKeys(t *testing.T) {
	c := parsed(t, `{}`)
	if c.Present == nil || c.Absent == nil {
		t.Fatal("missing keys should land empty arrays, not nil")
	}
}

func TestMarshal_SortedAndCompact(t *testing.T) {
	c := &SoftwarePolicyContent{Present: []string{"z", "a", "m"}, Absent: []string{"b", "a"}}
	out := c.Marshal()
	var roundtrip SoftwarePolicyContent
	if err := json.Unmarshal([]byte(out), &roundtrip); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	if roundtrip.Present[0] != "a" || roundtrip.Present[1] != "m" || roundtrip.Present[2] != "z" {
		t.Errorf("present not sorted: %v", roundtrip.Present)
	}
	if roundtrip.Absent[0] != "a" || roundtrip.Absent[1] != "b" {
		t.Errorf("absent not sorted: %v", roundtrip.Absent)
	}
}

func TestRequire_FreshAdd(t *testing.T) {
	c := parsed(t, `{"present":[],"absent":[]}`)
	o := c.Require("bash")
	if o.Action != "required" || o.From != "" {
		t.Errorf("fresh require: got %+v", o)
	}
	if !containsString(c.Present, "bash") {
		t.Error("bash should be in present")
	}
}

func TestRequire_AlreadyRequired_NoOp(t *testing.T) {
	c := parsed(t, `{"present":["bash"],"absent":[]}`)
	o := c.Require("bash")
	if o.Action != "no-change" || o.From != PackageStateRequired {
		t.Errorf("already required: got %+v", o)
	}
	if o.Changed() {
		t.Error("Changed() should be false on no-change")
	}
}

func TestRequire_MovesFromBlocked(t *testing.T) {
	c := parsed(t, `{"present":[],"absent":["bash"]}`)
	o := c.Require("bash")
	if o.Action != "moved" || o.From != PackageStateBlocked {
		t.Errorf("move from blocked: got %+v", o)
	}
	if containsString(c.Absent, "bash") {
		t.Error("bash should be gone from absent")
	}
	if !containsString(c.Present, "bash") {
		t.Error("bash should be in present")
	}
}

func TestBlock_FreshAdd(t *testing.T) {
	c := parsed(t, `{"present":[],"absent":[]}`)
	o := c.Block("nano")
	if o.Action != "blocked" {
		t.Errorf("fresh block: got %+v", o)
	}
}

func TestBlock_AlreadyBlocked(t *testing.T) {
	c := parsed(t, `{"present":[],"absent":["nano"]}`)
	o := c.Block("nano")
	if o.Action != "no-change" || o.From != PackageStateBlocked {
		t.Errorf("already blocked: got %+v", o)
	}
}

func TestBlock_MovesFromRequired(t *testing.T) {
	c := parsed(t, `{"present":["bash"],"absent":[]}`)
	o := c.Block("bash")
	if o.Action != "moved" || o.From != PackageStateRequired {
		t.Errorf("move from required: got %+v", o)
	}
}

func TestWaive_FromRequired(t *testing.T) {
	c := parsed(t, `{"present":["bash"],"absent":[]}`)
	o := c.Waive("bash")
	if o.Action != "waived" || o.From != PackageStateRequired {
		t.Errorf("waive required: got %+v", o)
	}
	if containsString(c.Present, "bash") {
		t.Error("bash should be gone")
	}
}

func TestWaive_FromBlocked(t *testing.T) {
	c := parsed(t, `{"present":[],"absent":["nano"]}`)
	o := c.Waive("nano")
	if o.Action != "waived" || o.From != PackageStateBlocked {
		t.Errorf("waive blocked: got %+v", o)
	}
}

func TestWaive_NotPresent_NoOp(t *testing.T) {
	c := parsed(t, `{"present":[],"absent":[]}`)
	o := c.Waive("bash")
	if o.Action != "no-change" || o.From != "" {
		t.Errorf("waive nothing: got %+v", o)
	}
}

func TestMutations_DontMutateOriginalSlice(t *testing.T) {
	// Guard against the marshal()-sort accidentally mutating the
	// in-memory content struct between calls. After a sequence of
	// mutations, in-place slice membership should still reflect the
	// last operation, not be aliased to some serialized form.
	c := parsed(t, `{"present":["z","a"],"absent":[]}`)
	c.Marshal()
	if c.Present[0] != "z" || c.Present[1] != "a" {
		t.Errorf("Marshal sorted in place: %v", c.Present)
	}
}

func TestRoundTrip_RequireBlockWaive(t *testing.T) {
	c := parsed(t, EmptySoftwarePolicyContent)
	c.Require("bash")
	c.Require("vim")
	c.Block("nano")
	c.Block("bash") // moves bash to blocked
	c.Waive("vim")  // drops vim entirely
	out := c.Marshal()
	want := `{"present":[],"absent":["bash","nano"]}`
	if out != want {
		t.Errorf("roundtrip mismatch\n got:  %s\n want: %s", out, want)
	}
}

// TestRoundTrip_PreservesUnknownKeys is the regression test for the
// destroy-on-write path: an older CLI talking to a newer server must
// degrade to "cannot edit the new fields", never "silently deletes
// them". `repositories` and `external` are the concrete keys this
// matters for today; `future_key` stands in for whatever ships next.
func TestRoundTrip_PreservesUnknownKeys(t *testing.T) {
	// The repository and external entries are spelled out in full —
	// every modeled field present, in the order Marshal emits them —
	// so this stays a byte-identity check rather than quietly becoming
	// a test of default-filling.
	raw := `{"present":[],"absent":[],` +
		`"repositories":[{"name":"mimugmail","url":"https://opn-repo.routerperformance.net/repo/${ABI}","priority":5,"enabled":true,"signature":{"type":"none"}}],` +
		`"external":[{"name":"ookla-speedtest","version":"1.2.0","url":"https://install.speedtest.net/x.pkg","force":false}],` +
		`"future_key":1}`

	c := parsed(t, raw)
	c.Require("bash")
	out := c.Marshal()

	var got map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("marshal produced invalid JSON: %v (%s)", err, out)
	}

	var want map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &want); err != nil {
		t.Fatalf("fixture is not valid JSON: %v", err)
	}

	for _, key := range []string{"repositories", "external", "future_key"} {
		if _, ok := got[key]; !ok {
			t.Fatalf("Marshal dropped %q: %s", key, out)
		}
		if string(got[key]) != string(want[key]) {
			t.Errorf("Marshal mangled %q:\n got %s\nwant %s", key, got[key], want[key])
		}
	}

	// The edit the operator actually asked for still lands.
	var present []string
	if err := json.Unmarshal(got["present"], &present); err != nil {
		t.Fatalf("present is not a string array: %v", err)
	}
	if len(present) != 1 || present[0] != "bash" {
		t.Errorf("Require did not take effect: %v", present)
	}
}

// TestMarshal_KeyOrderIsDeterministic pins the rendered order so
// successive require/block calls diff cleanly: present, absent, then
// any carried-through keys alphabetically.
func TestMarshal_KeyOrderIsDeterministic(t *testing.T) {
	c := parsed(t, `{"zeta":1,"present":[],"alpha":2,"absent":[]}`)
	got := c.Marshal()
	want := `{"present":[],"absent":[],"alpha":2,"zeta":1}`
	if got != want {
		t.Errorf("Marshal key order:\n got %s\nwant %s", got, want)
	}
}

// ---------------------------------------------------------------------
// Repositories and external packages
// ---------------------------------------------------------------------

func TestParseRepositories_TypedAndDefaulted(t *testing.T) {
	c := parsed(t, `{"present":[],"absent":[],"repositories":[
		{"name":"mimugmail","url":"https://example.invalid/repo/${ABI}","priority":5,"enabled":false,
		 "signature":{"type":"fingerprints","fingerprints":[{"function":"sha256","fingerprint":"ab"}]}},
		{"name":"other","url":"https://example.invalid/o","signature":{"type":"none"}}]}`)

	if len(c.Repositories) != 2 {
		t.Fatalf("want 2 repositories, got %d", len(c.Repositories))
	}
	if c.Repositories[0].Priority != 5 || c.Repositories[0].Enabled {
		t.Errorf("explicit fields lost: %+v", c.Repositories[0])
	}
	if c.Repositories[0].Signature.Type != "fingerprints" ||
		len(c.Repositories[0].Signature.Fingerprints) != 1 {
		t.Errorf("signature lost: %+v", c.Repositories[0].Signature)
	}
	// `enabled` defaults to true when the key is absent — the zero value
	// of a Go bool is the wrong answer here, and getting it wrong would
	// silently disable a repository the operator never turned off.
	if !c.Repositories[1].Enabled {
		t.Error("absent `enabled` must default to true, not false")
	}
	if c.Repositories[1].Priority != 0 {
		t.Errorf("absent `priority` must default to 0, got %d", c.Repositories[1].Priority)
	}
}

func TestParseExternal_Typed(t *testing.T) {
	c := parsed(t, `{"present":[],"absent":[],"external":[
		{"name":"ookla-speedtest","version":"1.2.0","url":"https://example.invalid/x.pkg","force":true}]}`)
	if len(c.External) != 1 {
		t.Fatalf("want 1 external, got %d", len(c.External))
	}
	e := c.External[0]
	if e.Name != "ookla-speedtest" || e.Version != "1.2.0" || !e.Force {
		t.Errorf("external fields lost: %+v", e)
	}
}

// Unknown keys *inside* a repository or external entry get the same
// pass-through treatment as unknown top-level keys — same reasoning,
// one level down.
func TestRepositories_PreserveUnknownNestedKeys(t *testing.T) {
	raw := `{"present":[],"absent":[],"repositories":[` +
		`{"name":"r1","url":"https://example.invalid/r","priority":1,"enabled":true,` +
		`"signature":{"type":"none"},"mirror_type":"srv"}]}`
	c := parsed(t, raw)
	c.SetRepository(Repository{Name: "r2", URL: "https://example.invalid/r2",
		Priority: 2, Enabled: true, Signature: RepositorySignature{Type: "none"}})

	var doc struct {
		Repositories []map[string]json.RawMessage `json:"repositories"`
	}
	if err := json.Unmarshal([]byte(c.Marshal()), &doc); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	for _, r := range doc.Repositories {
		var name string
		_ = json.Unmarshal(r["name"], &name)
		if name != "r1" {
			continue
		}
		if got, ok := r["mirror_type"]; !ok || string(got) != `"srv"` {
			t.Fatalf("nested unknown key dropped: %v", r)
		}
		return
	}
	t.Fatal("repository r1 disappeared")
}

func TestMarshal_RepositoriesSortedAndComplete(t *testing.T) {
	c := parsed(t, `{"present":[],"absent":[]}`)
	c.SetRepository(Repository{Name: "zulu", URL: "https://example.invalid/z", Signature: RepositorySignature{Type: "none"}})
	c.SetRepository(Repository{Name: "alpha", URL: "https://example.invalid/a", Priority: 3, Enabled: true, Signature: RepositorySignature{Type: "none"}})

	want := `{"present":[],"absent":[],"repositories":[` +
		`{"name":"alpha","url":"https://example.invalid/a","priority":3,"enabled":true,"signature":{"type":"none"}},` +
		`{"name":"zulu","url":"https://example.invalid/z","priority":0,"enabled":false,"signature":{"type":"none"}}]}`
	if got := c.Marshal(); got != want {
		t.Errorf("Marshal:\n got %s\nwant %s", got, want)
	}
}

func TestMarshal_OmitsEmptyRepositoriesAndExternal(t *testing.T) {
	// A document that never had the keys must not grow them — every
	// policy valid before this feature stays byte-identical.
	c := parsed(t, `{"present":["bash"],"absent":[]}`)
	if got, want := c.Marshal(), `{"present":["bash"],"absent":[]}`; got != want {
		t.Errorf("Marshal:\n got %s\nwant %s", got, want)
	}
}

func TestSetRepository_ReplacesByName(t *testing.T) {
	c := parsed(t, `{"present":[],"absent":[]}`)
	if o := c.SetRepository(Repository{Name: "r", URL: "https://example.invalid/1", Signature: RepositorySignature{Type: "none"}}); o.Action != "added" {
		t.Errorf("first set: want added, got %q", o.Action)
	}
	if o := c.SetRepository(Repository{Name: "r", URL: "https://example.invalid/2", Signature: RepositorySignature{Type: "none"}}); o.Action != "updated" {
		t.Errorf("second set: want updated, got %q", o.Action)
	}
	if len(c.Repositories) != 1 {
		t.Fatalf("same name added twice: %+v", c.Repositories)
	}
	if c.Repositories[0].URL != "https://example.invalid/2" {
		t.Errorf("update did not take: %+v", c.Repositories[0])
	}
	// Re-setting identical values is a no-op so the CLI can skip the PUT.
	if o := c.SetRepository(Repository{Name: "r", URL: "https://example.invalid/2", Signature: RepositorySignature{Type: "none"}}); o.Action != "no-change" {
		t.Errorf("identical set: want no-change, got %q", o.Action)
	}
}

func TestRemoveRepository(t *testing.T) {
	c := parsed(t, `{"present":[],"absent":[],"repositories":[{"name":"r","url":"https://example.invalid/r","signature":{"type":"none"}}]}`)
	if o := c.RemoveRepository("r"); o.Action != "removed" {
		t.Errorf("want removed, got %q", o.Action)
	}
	if len(c.Repositories) != 0 {
		t.Errorf("still present: %+v", c.Repositories)
	}
	if o := c.RemoveRepository("r"); o.Action != "no-change" {
		t.Errorf("second remove: want no-change, got %q", o.Action)
	}
	// The key is dropped entirely rather than left as an empty array.
	if got, want := c.Marshal(), `{"present":[],"absent":[]}`; got != want {
		t.Errorf("Marshal after last removal:\n got %s\nwant %s", got, want)
	}
}

func TestSetExternal_ReplacesByName(t *testing.T) {
	c := parsed(t, `{"present":[],"absent":[]}`)
	if o := c.SetExternal(ExternalPackage{Name: "e", Version: "1.0", URL: "https://example.invalid/e.pkg"}); o.Action != "added" {
		t.Errorf("want added, got %q", o.Action)
	}
	// Identity is the name alone: the same package at a new version is
	// an update, not a second entry. NDAgent reconciles by name, so two
	// entries sharing one name would be a policy that fights itself.
	if o := c.SetExternal(ExternalPackage{Name: "e", Version: "2.0", URL: "https://example.invalid/e2.pkg"}); o.Action != "updated" {
		t.Errorf("want updated, got %q", o.Action)
	}
	if len(c.External) != 1 || c.External[0].Version != "2.0" {
		t.Fatalf("external not replaced: %+v", c.External)
	}
	if o := c.RemoveExternal("e"); o.Action != "removed" {
		t.Errorf("want removed, got %q", o.Action)
	}
	if o := c.RemoveExternal("e"); o.Action != "no-change" {
		t.Errorf("want no-change, got %q", o.Action)
	}
}

// ---------------------------------------------------------------------
// Patch semantics
// ---------------------------------------------------------------------

// The bug these guard against: updating one field of an existing entry
// used to require restating every other field, because the write is a
// wholesale replace. A caller that left `enabled` at its zero/default
// value silently re-enabled a repository the operator had turned off.
func TestApplyRepositoryPatch_PreservesOmittedFields(t *testing.T) {
	c := parsed(t, `{"present":[],"absent":[],"repositories":[
		{"name":"r","url":"https://example.invalid/1","priority":7,"enabled":false,
		 "signature":{"type":"fingerprints","fingerprints":[{"function":"sha256","fingerprint":"ab"}]}}]}`)

	newURL := "https://example.invalid/2"
	outcome, err := c.ApplyRepositoryPatch(RepositoryPatch{Name: "r", URL: &newURL})
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	if outcome.Action != "updated" {
		t.Errorf("want updated, got %q", outcome.Action)
	}

	got := c.Repositories[0]
	if got.URL != newURL {
		t.Errorf("url not applied: %q", got.URL)
	}
	if got.Priority != 7 {
		t.Errorf("priority reset to %d, want 7 preserved", got.Priority)
	}
	if got.Enabled {
		t.Error("a disabled repository was silently re-enabled by an unrelated edit")
	}
	if got.Signature.Type != "fingerprints" || len(got.Signature.Fingerprints) != 1 {
		t.Errorf("signature reset: %+v", got.Signature)
	}
}

func TestApplyRepositoryPatch_NewEntryDefaultsAndRequirements(t *testing.T) {
	c := parsed(t, `{"present":[],"absent":[]}`)

	// A new entry has nothing to inherit, so the fields with no default
	// must be supplied rather than silently becoming empty strings.
	if _, err := c.ApplyRepositoryPatch(RepositoryPatch{Name: "r"}); err == nil {
		t.Error("a new repository with no URL should be refused")
	}
	url := "https://example.invalid/r"
	if _, err := c.ApplyRepositoryPatch(RepositoryPatch{Name: "r", URL: &url}); err == nil {
		t.Error("a new repository with no signature should be refused")
	}

	sig := RepositorySignature{Type: "none"}
	if _, err := c.ApplyRepositoryPatch(RepositoryPatch{Name: "r", URL: &url, Signature: &sig}); err != nil {
		t.Fatalf("patch: %v", err)
	}
	got := c.Repositories[0]
	// enabled defaults to true, priority to 0 — the wire-format defaults,
	// not the Go zero values.
	if !got.Enabled {
		t.Error("a new repository must default to enabled")
	}
	if got.Priority != 0 {
		t.Errorf("priority default: %d", got.Priority)
	}
}

func TestApplyRepositoryPatch_ExplicitFalseIsNotOmitted(t *testing.T) {
	c := parsed(t, `{"present":[],"absent":[],"repositories":[
		{"name":"r","url":"https://example.invalid/1","priority":3,"enabled":true,"signature":{"type":"none"}}]}`)
	disabled := false
	if _, err := c.ApplyRepositoryPatch(RepositoryPatch{Name: "r", Enabled: &disabled}); err != nil {
		t.Fatalf("patch: %v", err)
	}
	if c.Repositories[0].Enabled {
		t.Error("an explicit false must disable the repository")
	}
	if c.Repositories[0].Priority != 3 {
		t.Errorf("priority disturbed: %d", c.Repositories[0].Priority)
	}
}

func TestApplyExternalPatch_PreservesOmittedFields(t *testing.T) {
	c := parsed(t, `{"present":[],"absent":[],"external":[
		{"name":"e","version":"1.0","url":"https://example.invalid/e.pkg","force":true}]}`)

	version := "2.0"
	if _, err := c.ApplyExternalPatch(ExternalPatch{Name: "e", Version: &version}); err != nil {
		t.Fatalf("patch: %v", err)
	}
	got := c.External[0]
	if got.Version != "2.0" {
		t.Errorf("version not applied: %q", got.Version)
	}
	if got.URL != "https://example.invalid/e.pkg" {
		t.Errorf("url reset: %q", got.URL)
	}
	if !got.Force {
		t.Error("force silently cleared by an unrelated edit")
	}

	if _, err := c.ApplyExternalPatch(ExternalPatch{Name: "new"}); err == nil {
		t.Error("a new external package with no version should be refused")
	}
}

func TestParseFingerprint(t *testing.T) {
	cases := []struct {
		in       string
		function string
		digest   string
	}{
		{"sha256:a1b2c3", "sha256", "a1b2c3"},
		{"a1b2c3", "sha256", "a1b2c3"},
		{"  SHA256:A1B2C3  ", "sha256", "a1b2c3"},
		// Colon-separated hex is a common display format. Splitting on
		// the first colon unconditionally would read "A1" as the hash
		// function and silently send the rest, colons included.
		{"A1:B2:C3:D4", "sha256", "a1b2c3d4"},
		{"sha256:A1:B2:C3", "sha256", "a1b2c3"},
	}
	for _, tc := range cases {
		got, err := ParseFingerprint(tc.in)
		if err != nil {
			t.Errorf("ParseFingerprint(%q): %v", tc.in, err)
			continue
		}
		if got.Function != tc.function || got.Fingerprint != tc.digest {
			t.Errorf("ParseFingerprint(%q) = %+v, want {%s %s}", tc.in, got, tc.function, tc.digest)
		}
	}

	for _, bad := range []string{"", "   ", "sha256:", "::"} {
		if _, err := ParseFingerprint(bad); err == nil {
			t.Errorf("ParseFingerprint(%q) should have failed", bad)
		}
	}
}
