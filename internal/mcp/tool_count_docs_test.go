package mcp

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"regexp"
	"strconv"
	"testing"
)

// The tool count written in the files that leave the repository (the MCP
// Registry template, the MCPB manifest template, the README) and in
// CLAUDE.md follows the registry: the templates once said 129 while the
// server registered 177 (#270).
func TestDocumentedToolCountsMatchTheRegistry(t *testing.T) {
	want := len(registeredToolNames(t))
	// A whole-server count has three digits; the per-domain counts in
	// CLAUDE.md ("18 tools") have fewer and are not checked here.
	count := regexp.MustCompile(`\b(\d{3})\s+(?:\*\*)?(?:\[MCP\]\([^)]*\)\s+|MCP\s+)?tools?\b`)

	for _, path := range []string{"../../server.json.tmpl", "../../mcpb/manifest.json.tmpl"} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			Description string `json:"description"`
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		checkToolCount(t, path+" description", doc.Description, count, want)
	}
	for _, path := range []string{"../../README.md", "../../CLAUDE.md"} {
		raw, err := os.ReadFile(path)
		// The public mirror carries the README and the templates, not CLAUDE.md.
		if errors.Is(err, fs.ErrNotExist) && path == "../../CLAUDE.md" {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		checkToolCount(t, path, string(raw), count, want)
	}
}

// checkToolCount fails unless text states a tool count, and every count it
// states is want.
func checkToolCount(t *testing.T, where, text string, count *regexp.Regexp, want int) {
	t.Helper()
	found := count.FindAllStringSubmatch(text, -1)
	if len(found) == 0 {
		t.Errorf("%s states no tool count", where)
	}
	for _, m := range found {
		if n, _ := strconv.Atoi(m[1]); n != want {
			t.Errorf("%s says %q; the server registers %d tools", where, m[0], want)
		}
	}
}
