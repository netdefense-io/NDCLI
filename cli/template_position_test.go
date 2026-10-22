package cli

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// template update --position append used to go out verbatim, which the
// server refuses: it takes PREPEND and APPEND in upper case only.
func TestTemplateUpdatePositionInAnyCase(t *testing.T) {
	cmd := findCommand(t, "template", "update")
	if err := cmd.ParseFlags([]string{"--position", "append"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Flags().Set("position", ""); cmd.Flags().Lookup("position").Changed = false })

	var sent []string
	var err error
	stdout, _ := runStreams(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		sent = append(sent, r.Method+" "+r.URL.Path+" "+strings.TrimSpace(string(raw)))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"name":"edge","position":"APPEND"}`)
	}, "table", "", func() error {
		err = runTemplateUpdate(cmd, []string{"edge"})
		return nil
	})

	if err != nil {
		t.Fatal(err)
	}
	if len(sent) != 1 || sent[0] != `PATCH /api/v1/organizations/acme/templates/edge {"position":"APPEND"}` {
		t.Errorf("sent %v", sent)
	}
	if !strings.Contains(stdout, "Template position updated: edge") {
		t.Errorf("stdout = %q", stdout)
	}
}
