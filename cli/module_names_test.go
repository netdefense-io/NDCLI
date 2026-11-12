package cli

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// User-facing text names no internal module of the control plane (#283): a
// message says "the server", a host "the NetDefense server". NDAgent is the
// agent's public name and stays. The guard reads every string literal outside
// tests in cli/ and internal/, so a new message is checked the moment it is
// written.
func TestNoStringNamesAControlPlaneModule(t *testing.T) {
	module := regexp.MustCompile(`NDManager|NDBroker|NDPathFinder|NDDataModels|NDWeb`)
	fset := token.NewFileSet()
	for _, root := range []string{".", "../internal"} {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			f, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				return err
			}
			ast.Inspect(f, func(n ast.Node) bool {
				if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING && module.MatchString(lit.Value) {
					t.Errorf("%s: %s names a control-plane module", fset.Position(lit.Pos()), module.FindString(lit.Value))
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
