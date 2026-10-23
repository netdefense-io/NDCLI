package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

// accountRoute answers the account list the way NDManager does: sorted by
// email, 20 rows a page unless page_size asks for more (100 at most), with
// the matching total and page count in pagination.
func accountRoute(t *testing.T, total int, requests *[]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/organizations/acme/accounts" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		*requests = append(*requests, r.URL.RawQuery)
		page, size := 1, 20
		if v, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil {
			page = v
		}
		if v, err := strconv.Atoi(r.URL.Query().Get("page_size")); err == nil {
			size = min(v, 100)
		}
		accounts := []map[string]string{}
		for i := (page - 1) * size; i < min(page*size, total); i++ {
			accounts = append(accounts, map[string]string{"email": fmt.Sprintf("user%03d@acme.test", i), "role": "RO", "status": "ENABLED"})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"accounts":   accounts,
			"pagination": map[string]int{"page": page, "page_size": size, "total": total, "pages": (total + size - 1) / size},
			"quota":      map[string]any{"limit": 500, "used": total, "available": 500 - total},
		})
	}
}

// TestOrgAccountListReadsEveryPage: the route answers 20 accounts unless asked
// for more, and the list asked for nothing, so an organization with more
// accounts showed its first 20 in the CLI, the MCP tool and the TUI alike.
func TestOrgAccountListReadsEveryPage(t *testing.T) {
	for _, tc := range []struct {
		total    int
		requests []string
	}{
		{0, []string{"page=1&page_size=100"}},
		{25, []string{"page=1&page_size=100"}},
		{100, []string{"page=1&page_size=100"}},
		{230, []string{"page=1&page_size=100", "page=2&page_size=100", "page=3&page_size=100"}},
	} {
		t.Run(strconv.Itoa(tc.total), func(t *testing.T) {
			var requests []string
			srv := httptest.NewServer(accountRoute(t, tc.total, &requests))
			defer srv.Close()

			result, err := newTestService(t, srv).OrgAccountList(context.Background(), "acme")
			if err != nil {
				t.Fatalf("OrgAccountList: %v", err)
			}
			if result.Accounts == nil {
				t.Fatal("accounts is nil, which -f json prints as null rather than []")
			}
			if len(result.Accounts) != tc.total || result.Total != tc.total {
				t.Fatalf("got %d accounts, total %d; want %d of each", len(result.Accounts), result.Total, tc.total)
			}
			for i, a := range result.Accounts {
				if want := fmt.Sprintf("user%03d@acme.test", i); a.Email != want {
					t.Fatalf("account %d is %s, want %s", i, a.Email, want)
				}
			}
			if result.Quota == nil || result.Quota.Used != tc.total {
				t.Errorf("quota = %+v, want the route's", result.Quota)
			}
			if fmt.Sprint(requests) != fmt.Sprint(tc.requests) {
				t.Errorf("requests = %q, want %q", requests, tc.requests)
			}
		})
	}
}
