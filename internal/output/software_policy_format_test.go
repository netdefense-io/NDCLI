package output

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/models"
)

const policyWithEntries = `{"present":["bash"],"absent":[],` +
	`"repositories":[{"name":"r","url":"https://example.invalid/r","priority":1,"enabled":true,"signature":{"type":"none"}}],` +
	`"external":[{"name":"e","version":"1.0","url":"https://example.invalid/e.pkg","force":false}]}`

func TestSoftwarePolicyCounts(t *testing.T) {
	c := softwarePolicyCounts(policyWithEntries)
	if c.Present != 1 || c.Absent != 0 || c.Repositories != 1 || c.External != 1 {
		t.Errorf("counts: %+v", c)
	}
	if !c.HasEntries() {
		t.Error("HasEntries should be true")
	}

	plain := softwarePolicyCounts(`{"present":["a","b"],"absent":["c"]}`)
	if plain.Present != 2 || plain.Absent != 1 {
		t.Errorf("counts: %+v", plain)
	}
	if plain.HasEntries() {
		t.Error("a policy with no repositories/external must not report entries")
	}

	// Malformed content must degrade to zeroes, never panic — a bad row
	// should not take a whole listing down.
	if got := softwarePolicyCounts(`{not json`); got != (softwarePolicyCountSet{}) {
		t.Errorf("malformed content: %+v", got)
	}
}

// The repository / external columns appear only for policies that use
// them, so `software list` over a fleet of ordinary package policies
// looks exactly as it did before this feature.
func TestTableFormatter_EntryColumnsOnlyWhenUsed(t *testing.T) {
	plain := renderPolicies(t, models.SoftwarePolicy{Name: "plain", Content: `{"present":[],"absent":[]}`})
	if strings.Contains(plain, "Repos") || strings.Contains(plain, "External") {
		t.Errorf("entry columns leaked into a plain listing:\n%s", plain)
	}

	rich := renderPolicies(t, models.SoftwarePolicy{Name: "rich", Content: policyWithEntries})
	if !strings.Contains(rich, "Repos") || !strings.Contains(rich, "External") {
		t.Errorf("entry columns missing:\n%s", rich)
	}

	// One policy using the keys turns the columns on for the whole
	// table; the rows that don't use them read 0 rather than blank.
	mixed := renderPolicies(t,
		models.SoftwarePolicy{Name: "plain", Content: `{"present":[],"absent":[]}`},
		models.SoftwarePolicy{Name: "rich", Content: policyWithEntries})
	if !strings.Contains(mixed, "Repos") {
		t.Errorf("entry columns missing from a mixed table:\n%s", mixed)
	}
}

func TestSimpleFormatter_EntryCounts(t *testing.T) {
	var buf bytes.Buffer
	f := &SimpleFormatter{BaseFormatter: BaseFormatter{Writer: &buf}}
	if err := f.FormatSoftwarePolicies([]models.SoftwarePolicy{
		{Name: "plain", Content: `{"present":[],"absent":[]}`},
		{Name: "rich", Content: policyWithEntries},
	}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		switch {
		case strings.Contains(line, "plain") && strings.Contains(line, "repos:"):
			t.Errorf("plain policy should not mention repos: %s", line)
		case strings.Contains(line, "rich") && !strings.Contains(line, "repos: 1"):
			t.Errorf("rich policy should report repos: %s", line)
		}
	}
}

func TestDetailedFormatter_EntryCounts(t *testing.T) {
	var buf bytes.Buffer
	f := &DetailedFormatter{BaseFormatter: BaseFormatter{Writer: &buf}}
	if err := f.FormatSoftwarePolicy(&models.SoftwarePolicy{Name: "rich", Content: policyWithEntries}); err != nil {
		t.Fatal(err)
	}
	if out := buf.String(); !strings.Contains(out, "Repository count") || !strings.Contains(out, "External count") {
		t.Errorf("detailed view missing entry counts:\n%s", out)
	}

	buf.Reset()
	if err := f.FormatSoftwarePolicy(&models.SoftwarePolicy{Name: "plain", Content: `{"present":[],"absent":[]}`}); err != nil {
		t.Fatal(err)
	}
	if out := buf.String(); strings.Contains(out, "Repository count") {
		t.Errorf("plain view should stay short:\n%s", out)
	}
}

// renderPolicies captures stdout rather than the formatter's Writer:
// StyledTable.Render prints directly, so a buffer on the formatter
// would come back empty and every assertion here would pass without
// testing anything.
func renderPolicies(t *testing.T, policies ...models.SoftwarePolicy) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = stdout }()

	f := NewTableFormatter()
	formatErr := f.FormatSoftwarePolicies(policies)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatal(err)
	}
	if formatErr != nil {
		t.Fatal(formatErr)
	}
	return buf.String()
}
