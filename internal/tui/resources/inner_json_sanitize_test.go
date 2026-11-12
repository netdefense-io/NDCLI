package resources

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/auth"
	"github.com/netdefense-io/NDCLI/internal/service"
)

const controlChars = "\x1b\x07\u009b"

// A task's message or payload is often JSON in a string; the describe view
// decodes it again, so its keys and values are scrubbed there.
func TestTaskJSONSectionIsSanitized(t *testing.T) {
	section, ok := jsonSection("Result", `{"out\u001b[2Jput": "done\u001b]0;x\u0007", "code": "\u009b31m"}`)
	if !ok || len(section.Fields) != 2 {
		t.Fatalf("section %+v, ok %v", section, ok)
	}
	for _, f := range section.Fields {
		if strings.ContainsAny(f.Label+f.Value, controlChars) {
			t.Errorf("field %q = %q carries a control character", f.Label, f.Value)
		}
	}
}

// A software policy's content is JSON in a string; the describe view shows
// the packages and repositories decoded from it, scrubbed.
func TestSoftwareDescribeIsSanitized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"name": "base", "content": "{\"present\": [\"nginx\\u001b[2J\"], \"absent\": [\"telnet\\u0007\"], \"repositories\": [{\"name\": \"r\\u001b[31m\", \"url\": \"https://r.example\\u009b1\", \"enabled\": true, \"priority\": 1, \"signature\": {\"type\": \"none\\u001b\"}}], \"external\": [{\"name\": \"x\\u001b[2J\", \"version\": \"1\\u0007\", \"url\": \"https://x.example\\u009b\"}]}"}`)
	}))
	defer srv.Close()
	svc := service.New(api.NewClient(srv.URL, false, auth.NewStaticTokenProvider("ndpat_test")), nil, nil)
	sections, err := softwareResource{}.Describe(context.Background(), svc, "acme", "base")
	if err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	for _, s := range sections {
		text.WriteString(s.Text)
		for _, f := range s.Fields {
			text.WriteString(f.Label + f.Value)
		}
	}
	if strings.ContainsAny(text.String(), controlChars) || !strings.Contains(text.String(), "nginx[2J") || !strings.Contains(text.String(), "x[2J") {
		t.Errorf("describe carries a control character, or lost the package: %q", text.String())
	}
}
