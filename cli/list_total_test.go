package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"testing"

	"github.com/spf13/cobra"
)

// accountPages answers the account list as NDManager does: 20 rows a page
// unless page_size asks for more, with the total in pagination.
func accountPages(total int) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		page, size := 1, 20
		if v, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil {
			page = v
		}
		if v, err := strconv.Atoi(r.URL.Query().Get("page_size")); err == nil {
			size = min(v, 100)
		}
		accounts := []map[string]string{}
		for i := (page - 1) * size; i < min(page*size, total); i++ {
			accounts = append(accounts, map[string]string{"email": fmt.Sprintf("user%02d@acme.test", i)})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"accounts":   accounts,
			"pagination": map[string]int{"page": page, "page_size": size, "total": total, "pages": (total + size - 1) / size},
		})
	}
}

// TestListJSONCarriesTheServersTotal: -f json printed the number of rows on
// the page as "total" for these lists, so a script paging through one stopped
// after the first page. Each route answers one row of a 42-row list, except
// the account list, which is read to the end.
func TestListJSONCarriesTheServersTotal(t *testing.T) {
	for _, tc := range []struct {
		name  string
		cmd   []string
		run   func(*cobra.Command, []string) error
		args  []string
		path  string
		body  string
		route func(w http.ResponseWriter, r *http.Request)
		key   string
		rows  int
	}{
		{name: "org list", cmd: []string{"org", "list"}, run: runOrgList,
			path: "/api/v1/organizations",
			body: `{"organizations":[{"name":"acme"}],"count":42}`,
			key:  "organizations", rows: 1},
		{name: "org account list", cmd: []string{"org", "account", "list"}, run: runOrgAccountList,
			path:  "/api/v1/organizations/acme/accounts",
			route: accountPages(42),
			key:   "accounts", rows: 42},
		{name: "ou list", cmd: []string{"ou", "list"}, run: runOUList,
			path: "/api/v1/organizations/acme/ous",
			body: `{"ous":[{"name":"branch"}],"count":1,"total":42,"page":1,"page_size":20,"total_pages":3}`,
			key:  "organizational_units", rows: 1},
		{name: "ou template list", cmd: []string{"ou", "template", "list"}, run: runOUTemplateList, args: []string{"branch"},
			path: "/api/v1/organizations/acme/ous/branch/templates",
			body: `{"items":[{"name":"base"}],"total":42,"ou":"branch","organization":"acme"}`,
			key:  "templates", rows: 1},
		{name: "template list", cmd: []string{"template", "list"}, run: runTemplateList,
			path: "/api/v1/organizations/acme/templates",
			body: `{"items":[{"name":"base"}],"total":42}`,
			key:  "templates", rows: 1},
		{name: "snippet list", cmd: []string{"snippet", "list"}, run: runSnippetList,
			path: "/api/v1/organizations/acme/snippets",
			body: `{"items":[{"name":"ssh"}],"total":42}`,
			key:  "snippets", rows: 1},
		{name: "software list", cmd: []string{"software", "list"}, run: runSoftwareList,
			path: "/api/v1/organizations/acme/software-policies",
			body: `{"items":[{"name":"tools"}],"total":42}`,
			key:  "software_policies", rows: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := findCommand(t, tc.cmd...)
			handler := func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tc.path {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				if tc.route != nil {
					tc.route(w, r)
					return
				}
				_, _ = io.WriteString(w, tc.body)
			}
			stdout, _ := runStreams(t, handler, "json", "", func() error { return tc.run(cmd, tc.args) })

			var got map[string]json.RawMessage
			decodeOne(t, stdout, &got)
			var total int
			var rows []json.RawMessage
			if err := json.Unmarshal(got["total"], &total); err != nil || total != 42 {
				t.Errorf("total = %s, want the server's 42", got["total"])
			}
			if err := json.Unmarshal(got[tc.key], &rows); err != nil || len(rows) != tc.rows {
				t.Errorf("%s has %d rows, want %d", tc.key, len(rows), tc.rows)
			}
		})
	}
}
