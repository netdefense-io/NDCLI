package cli

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// TestSnippetListAndCreateTypeFlagsUseCreatableList guards `snippet list
// --type` and `snippet create --type` against a hand-edited type list
// drifting from models.SnippetCreatableTypes. Equality, not a substring
// check: the pullable list is currently a prefix of the joined creatable
// list, so a substring check here would not catch a create/list flag that
// was accidentally built from the wrong (smaller) list.
func TestSnippetListAndCreateTypeFlagsUseCreatableList(t *testing.T) {
	types := models.SnippetTypesHelp(models.SnippetCreatableTypes)

	for _, tc := range []struct {
		flagLookup func() string
		prefix     string
		name       string
	}{
		{func() string { return snippetListCmd.Flags().Lookup("type").Usage }, "Filter by type: ", "list"},
		{func() string { return snippetCreateCmd.Flags().Lookup("type").Usage }, "Snippet type (required): ", "create"},
	} {
		want := tc.prefix + types
		if got := tc.flagLookup(); got != want {
			t.Errorf("%s --type usage = %q, want %q", tc.name, got, want)
		}
	}
}

// TestSnippetPullTypeFlagUsesPullableList guards `snippet pull --type`
// against drifting from models.SnippetPullableTypes, and pins the existing
// default (no behavior change). Equality, not a substring check: the
// pullable list is currently a prefix of the joined creatable list, so a
// substring check would still pass if `pull --type` were accidentally built
// from SnippetCreatableTypes instead.
func TestSnippetPullTypeFlagUsesPullableList(t *testing.T) {
	flag := snippetPullCmd.Flags().Lookup("type")
	if flag == nil {
		t.Fatal("pull: no --type flag registered")
	}
	want := "Config type to pull: " + models.SnippetTypesHelp(models.SnippetPullableTypes)
	if flag.Usage != want {
		t.Errorf("pull --type usage = %q, want %q", flag.Usage, want)
	}
	if flag.DefValue != "ALIAS" {
		t.Errorf("pull --type default = %q, want ALIAS (no behavior change)", flag.DefValue)
	}
}

// TestSnippetCreateMissingTypeErrorUsesCreatableList guards the --type
// required error text. Equality, not a substring check — see
// TestSnippetPullTypeFlagUsesPullableList.
func TestSnippetCreateMissingTypeErrorUsesCreatableList(t *testing.T) {
	err := requireSnippetType("")
	if err == nil {
		t.Fatal("requireSnippetType(\"\") = nil, want an error")
	}
	want := fmt.Sprintf("--type is required (%s)", models.SnippetTypesHelp(models.SnippetCreatableTypes))
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
	if err := requireSnippetType("USER"); err != nil {
		t.Errorf("requireSnippetType(\"USER\") = %v, want nil", err)
	}
}

// snippetPullMatchLineRE matches a "- For TYPE: ..." bullet in the pull
// command's Long help text.
var snippetPullMatchLineRE = regexp.MustCompile(`(?m)^- For ([A-Z_]+):`)

// TestSnippetPullLongDocumentsExactlyThePullableTypes guards the "Matching
// behavior" section of `snippet pull`'s help text against drifting from
// models.SnippetPullableTypes — every pullable type must have a bullet, and
// no other type may appear.
func TestSnippetPullLongDocumentsExactlyThePullableTypes(t *testing.T) {
	matches := snippetPullMatchLineRE.FindAllStringSubmatch(snippetPullCmd.Long, -1)
	documented := make(map[string]bool, len(matches))
	for _, m := range matches {
		documented[m[1]] = true
	}

	want := make(map[string]bool, len(models.SnippetPullableTypes))
	for _, ty := range models.SnippetPullableTypes {
		want[ty] = true
		if !documented[ty] {
			t.Errorf("pull --help documents no matching behavior for pullable type %q", ty)
		}
	}
	for ty := range documented {
		if !want[ty] {
			t.Errorf("pull --help documents matching behavior for %q, which is not in models.SnippetPullableTypes", ty)
		}
	}
}
