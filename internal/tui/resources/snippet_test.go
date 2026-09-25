package resources

import (
	"reflect"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// TestSnippetCreateFormUsesCreatableTypeList guards the TUI's "n" (create)
// action form against a hand-edited type list drifting from
// models.SnippetCreatableTypes.
func TestSnippetCreateFormUsesCreatableTypeList(t *testing.T) {
	for _, action := range (snippetResource{}).Actions() {
		if action.Key != "n" {
			continue
		}
		for _, field := range action.Form {
			if field.Key != "type" {
				continue
			}
			if !reflect.DeepEqual(field.Options, models.SnippetCreatableTypes) {
				t.Errorf("create form type options = %v, want models.SnippetCreatableTypes %v", field.Options, models.SnippetCreatableTypes)
			}
			return
		}
		t.Fatal("create (n) action has no type field")
	}
	t.Fatal("no create (n) action found")
}
