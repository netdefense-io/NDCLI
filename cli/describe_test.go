package cli

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// runDescribe runs a describe command against a fake server that answers
// path with body.
func runDescribe(t *testing.T, format, path string, body any, run func() error) string {
	t.Helper()
	stdout, _ := runStreams(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != path {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	}, format, "", run)
	return stdout
}

func describeSnippet(t *testing.T, format string, want models.Snippet) string {
	t.Helper()
	return runDescribe(t, format, "/api/v1/organizations/acme/snippets/"+want.Name, want, func() error {
		return runSnippetDescribe(snippetDescribeCmd, []string{want.Name})
	})
}

func describeSoftwarePolicy(t *testing.T, format string, want models.SoftwarePolicy) string {
	t.Helper()
	return runDescribe(t, format, "/api/v1/organizations/acme/software-policies/"+want.Name, want, func() error {
		return runSoftwareDescribe(softwareDescribeCmd, []string{want.Name})
	})
}

// TestSnippetDescribeJSONIsOneDocument: `snippet describe -f json` printed the
// snippet object followed by a "Content:" heading and the indented content, so
// the output never parsed, whatever the type.
func TestSnippetDescribeJSONIsOneDocument(t *testing.T) {
	for _, typ := range models.SnippetCreatableTypes {
		t.Run(typ, func(t *testing.T) {
			want := models.Snippet{
				Name:     "s-" + strings.ToLower(typ),
				Type:     typ,
				Content:  `{"name":"corp","descr":"say \"hi\"\nbye","value":"${corp_value}"}`,
				Priority: 100,
			}

			out := describeSnippet(t, "json", want)

			var got models.Snippet
			if err := json.Unmarshal([]byte(out), &got); err != nil {
				t.Fatalf("output is not one JSON document: %v\n%s", err, out)
			}
			if got.Name != want.Name || got.Type != want.Type || got.Content != want.Content || got.Priority != want.Priority {
				t.Errorf("decoded %+v, want %+v", got, want)
			}
		})
	}
}

// TestSoftwareDescribeJSONIsOneDocument: `software describe -f json` had the
// same trailing content block as `snippet describe`.
func TestSoftwareDescribeJSONIsOneDocument(t *testing.T) {
	want := models.SoftwarePolicy{
		Name:          "base",
		Content:       `{"present":["bash"],"absent":["telnet"]}`,
		TemplateNames: []string{"edge"},
	}

	out := describeSoftwarePolicy(t, "json", want)

	var got models.SoftwarePolicy
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not one JSON document: %v\n%s", err, out)
	}
	if got.Name != want.Name || got.Content != want.Content || strings.Join(got.TemplateNames, ",") != "edge" {
		t.Errorf("decoded %+v, want %+v", got, want)
	}
}

// TestDescribeHumanFormatsShowContent: the content block moved into the
// formatters, and every human format must still print it, indented.
func TestDescribeHumanFormatsShowContent(t *testing.T) {
	snippet := models.Snippet{Name: "corp", Type: "ALIAS", Content: `{"name":"corp","type":"host"}`, Priority: 100}
	snippetBlock := "Content:\n{\n  \"name\": \"corp\",\n  \"type\": \"host\"\n}\n"
	policy := models.SoftwarePolicy{Name: "base", Content: `{"present":["bash"],"absent":[]}`}
	policyBlock := "Content:\n{\n  \"present\": [\n    \"bash\"\n  ],\n  \"absent\": []\n}\n"

	for _, format := range []string{"table", "simple", "detailed"} {
		t.Run(format, func(t *testing.T) {
			if out := describeSnippet(t, format, snippet); !strings.Contains(out, snippetBlock) {
				t.Errorf("snippet content block missing:\n%s", out)
			}
			if out := describeSoftwarePolicy(t, format, policy); !strings.Contains(out, policyBlock) {
				t.Errorf("software policy content block missing:\n%s", out)
			}
		})
	}
}

// runJSONCommand runs the command at path with -f json against a fake server
// that answers every request with an empty object, and returns stdout.
func runJSONCommand(t *testing.T, path, args []string) string {
	t.Helper()
	cmd, _, err := rootCmd.Find(path)
	if err != nil || cmd.RunE == nil {
		t.Fatalf("find %v: %v", path, err)
	}
	stdout, _ := runStreams(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}, "json", "", func() error { return cmd.RunE(cmd, args) })
	return stdout
}

// TestDescribeAndShowCommandsPrintOneJSONDocument covers every describe and
// show command that renders through the formatter, so output printed after
// the formatter fails here whichever command it is added to. auth show and
// config show are not listed: they ignore -f and print text in every format.
func TestDescribeAndShowCommandsPrintOneJSONDocument(t *testing.T) {
	prepareRootCommand()

	for _, tc := range []struct{ path, args []string }{
		{[]string{"backup", "config", "show"}, nil},
		{[]string{"backup", "show"}, []string{"fw01"}},
		{[]string{"device", "describe"}, []string{"fw01"}},
		{[]string{"network", "describe"}, []string{"corp"}},
		{[]string{"network", "link", "describe"}, []string{"corp", "fw01", "fw02"}},
		{[]string{"network", "member", "describe"}, []string{"corp", "fw01"}},
		{[]string{"org", "describe"}, []string{"acme"}},
		{[]string{"ou", "describe"}, []string{"branch"}},
		{[]string{"respond", "show"}, []string{"Ab3dE5fG"}},
		{[]string{"schedule", "describe"}, []string{"nightly"}},
		{[]string{"snippet", "describe"}, []string{"corp"}},
		{[]string{"software", "describe"}, []string{"base"}},
		{[]string{"support", "show"}, []string{"Ab3dE5fG"}},
		{[]string{"task", "describe"}, []string{"T1"}},
		{[]string{"template", "describe"}, []string{"edge"}},
		{[]string{"variable", "org", "describe"}, []string{"lan"}},
	} {
		t.Run(strings.Join(tc.path, " "), func(t *testing.T) {
			out := runJSONCommand(t, tc.path, tc.args)
			var doc any
			if err := json.Unmarshal([]byte(out), &doc); err != nil {
				t.Fatalf("output is not one JSON document: %v\n%s", err, out)
			}
		})
	}
}
