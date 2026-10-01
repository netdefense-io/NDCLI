package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// TestGetValues_RepeatsAKeyInTheQuery: Get takes one value per key, so the
// repeated ?type=SYNC&type=BACKUP form NDManager's list endpoints accept had no
// way onto the wire.
func TestGetValues_RepeatsAKeyInTheQuery(t *testing.T) {
	var gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	c := NewClient(srv.URL, false, nil)

	for _, tc := range []struct {
		name   string
		params url.Values
		want   string
	}{
		{"repeated key keeps its order", url.Values{"type": {"SYNC", "BACKUP"}, "organization": {"acme"}}, "organization=acme&type=SYNC&type=BACKUP"},
		{"no params, no question mark", nil, ""},
	} {
		resp, err := c.GetValues(context.Background(), "/api/v1/tasks", tc.params)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		resp.Body.Close()
		if gotPath != "/api/v1/tasks" {
			t.Errorf("%s: path = %q", tc.name, gotPath)
		}
		if gotQuery != tc.want {
			t.Errorf("%s: query = %q, want %q", tc.name, gotQuery, tc.want)
		}
	}
}
