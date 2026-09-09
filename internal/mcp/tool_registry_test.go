package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/netdefense-io/NDCLI/internal/config"
)

// registeredToolNames enumerates the server's tools the way a real client
// does — an in-memory session and a paginated tools/list — rather than
// reaching into the registry, so the count reflects what a caller actually
// sees.
func registeredToolNames(t *testing.T) []string {
	t.Helper()

	s := &Server{
		mcpServer: mcp.NewServer(&mcp.Implementation{Name: "ndcli", Version: config.Version}, nil),
	}
	s.registerAll()

	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	ss, err := s.mcpServer.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()

	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).
		Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	var names []string
	for tool, err := range cs.Tools(ctx, nil) {
		if err != nil {
			t.Fatalf("tools/list: %v", err)
		}
		names = append(names, tool.Name)
	}
	return names
}

// TestToolRegistry_SupportAndResponderNames pins the tool surface after the
// rename of the two ticketing command groups: the organization side is
// ndcli.support.*, the responder side ndcli.respond.*, and the old
// ndcli.ticket.* names are gone. A reverted Name field is a silent break for
// every MCP caller, so it fails the build here instead.
func TestToolRegistry_SupportAndResponderNames(t *testing.T) {
	names := registeredToolNames(t)

	counts := map[string]int{}
	for _, name := range names {
		for _, prefix := range []string{"ndcli.support.", "ndcli.respond.", "ndcli.ticket."} {
			if strings.HasPrefix(name, prefix) {
				counts[prefix]++
			}
		}
	}

	for _, tc := range []struct {
		prefix string
		want   int
		note   string
	}{
		{"ndcli.support.", 14, "the organization side of the ticketing system"},
		{"ndcli.respond.", 12, "the responder console"},
		{"ndcli.ticket.", 0, "renamed to ndcli.support.*; no aliases were kept"},
	} {
		if got := counts[tc.prefix]; got != tc.want {
			t.Errorf("%d tools named %s*, want %d (%s):\n%s",
				got, tc.prefix, tc.want, tc.note, strings.Join(matching(names, tc.prefix), "\n"))
		}
	}

	if got := len(names); got != 159 {
		t.Errorf("the server registers %d tools, want 159 — update this count deliberately, with the README and CLAUDE.md", got)
	}
}

// TestToolRegistry_NamesAreUniqueAndWellFormed guards the shape of every tool
// name, not just the renamed ones: a duplicate silently overwrites its twin
// in the registry.
func TestToolRegistry_NamesAreUniqueAndWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, name := range registeredToolNames(t) {
		if seen[name] {
			t.Errorf("duplicate tool name %q — the later registration wins and its twin is unreachable", name)
		}
		seen[name] = true
		if !strings.HasPrefix(name, "ndcli.") {
			t.Errorf("tool %q does not use the ndcli.<domain>.<verb> convention", name)
		}
		if strings.Count(name, ".") < 2 {
			t.Errorf("tool %q is missing a domain or a verb", name)
		}
	}
}

func matching(names []string, prefix string) []string {
	var out []string
	for _, n := range names {
		if strings.HasPrefix(n, prefix) {
			out = append(out, "  "+n)
		}
	}
	return out
}
