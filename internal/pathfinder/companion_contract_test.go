package pathfinder

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// companionSource is the companion's package, in its own module. This module
// cannot import it, so the tests below read its source: what the two modules
// agree on is written in each, and must be the same.
const companionSource = "../../netdefense-connect/internal/companion"

// companionDeclarations parses the companion's package and returns its
// constants by name, and the JSON names of each struct's fields in order.
func companionDeclarations(t *testing.T) (constants map[string]string, fields map[string][]string) {
	t.Helper()
	constants, fields = map[string]string{}, map[string][]string{}
	files, err := filepath.Glob(filepath.Join(companionSource, "*.go"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no source in %s: %v", companionSource, err)
	}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, spec := range gen.Specs {
				switch spec := spec.(type) {
				case *ast.ValueSpec:
					if gen.Tok != token.CONST {
						continue
					}
					for i, ident := range spec.Names {
						if i >= len(spec.Values) {
							continue
						}
						lit, ok := spec.Values[i].(*ast.BasicLit)
						if !ok {
							continue
						}
						value := lit.Value
						if lit.Kind == token.STRING {
							if value, err = strconv.Unquote(lit.Value); err != nil {
								t.Fatal(err)
							}
						}
						constants[ident.Name] = value
					}
				case *ast.TypeSpec:
					st, ok := spec.Type.(*ast.StructType)
					if !ok {
						continue
					}
					for _, field := range st.Fields.List {
						if field.Tag == nil {
							continue
						}
						tag, err := strconv.Unquote(field.Tag.Value)
						if err != nil {
							t.Fatal(err)
						}
						fields[spec.Name.Name] = append(fields[spec.Name.Name], reflect.StructTag(tag).Get("json"))
					}
				}
			}
		}
	}
	return constants, fields
}

func jsonFields(v any) []string {
	var tags []string
	typ := reflect.TypeOf(v)
	for i := range typ.NumField() {
		tags = append(tags, typ.Field(i).Tag.Get("json"))
	}
	return tags
}

func TestTheCompanionsConstantsAreTheOnesWrittenHere(t *testing.T) {
	theirs, _ := companionDeclarations(t)
	for name, ours := range map[string]string{
		"Protocol":               strconv.Itoa(companionProtocol),
		"EventHello":             companionHello,
		"EventConnected":         companionConnected,
		"EventPath":              companionPath,
		"EventClosed":            companionClosed,
		"EventError":             companionFailed,
		"EventLog":               companionLog,
		"PathDirect":             companionPathDirect,
		"PathRelayed":            companionPathRelayed,
		"ReasonParentClosed":     companionParentClosed,
		"ReasonPeerClosed":       companionPeerClosed,
		"ReasonTerminated":       companionTerminated,
		"ReasonBadConfig":        companionBadConfig,
		"ReasonRelayUnreachable": companionRelayUnreachable,
		"ReasonNoAnswer":         companionNoAnswer,
		"ReasonInternal":         companionInternal,
	} {
		got, ok := theirs[name]
		if !ok {
			t.Errorf("the companion no longer declares %s", name)
			continue
		}
		if got != ours {
			t.Errorf("%s is %q in the companion and %q here", name, got, ours)
		}
	}

	// A new event, path or reason on the companion's side needs a decision
	// here, even if the decision is to ignore it.
	known := 16
	found := 0
	for name := range theirs {
		if name == "Protocol" || strings.HasPrefix(name, "Event") || strings.HasPrefix(name, "Path") || strings.HasPrefix(name, "Reason") {
			found++
		}
	}
	if found != known {
		t.Errorf("the companion declares %d protocol, event, path and reason constants; this file knows %d", found, known)
	}
}

func TestTheCompanionsMessagesHaveTheFieldsWrittenHere(t *testing.T) {
	_, theirs := companionDeclarations(t)
	for name, ours := range map[string][]string{
		"Config": jsonFields(companionConfig{}),
		"Relay":  jsonFields(companionRelay{}),
		"Event":  jsonFields(companionEvent{}),
	} {
		if got := theirs[name]; !reflect.DeepEqual(got, ours) {
			t.Errorf("%s has the JSON fields %q in the companion and %q here", name, got, ours)
		}
	}
}

// The companion is found by its file name; the build names the file.
func TestTheCompanionIsBuiltUnderTheNameItIsLookedUpBy(t *testing.T) {
	makefile, err := os.ReadFile("../../Makefile")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^CONNECT_BINARY_NAME=(\S+)$`).FindSubmatch(makefile)
	if m == nil || string(m[1]) != companionName {
		t.Errorf("the Makefile builds the companion as %q; it is looked up as %q", m, companionName)
	}
	if _, err := os.Stat(filepath.Join("../..", companionName, "go.mod")); err != nil {
		t.Errorf("the companion's module is not in ../../%s: %v", companionName, err)
	}
}
