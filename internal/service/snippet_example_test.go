package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const registryBody = `{"schema":1,"types":[
 {"type":"RULE","label":"Rule","category":"Firewall","keywords":[],"docs":"/docs/getting-started/snippet-format/#rule","examples":[
  {"id":"allow-https","name":"Allow HTTPS","subtype":null,"content":{"interface":"lan","action":"pass"}}]},
 {"type":"TRUST_CERT","label":"Cert","category":"Certificates","keywords":[],"docs":"/d#trust-cert","examples":[
  {"id":"cert-variable","name":"Cert from variables","subtype":null,"content":{"name":"fw","key":"${FW_KEY}"},
   "requires":{"examples":["TRUST_CA/trust-ca-variable"],"variables":["FW_KEY"]}}]}]}`

func registryServer(t *testing.T, status int) *Service {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/snippet-types" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if status == http.StatusOK {
			_, _ = io.WriteString(w, registryBody)
		} else {
			_, _ = io.WriteString(w, `{"detail":"Not Found"}`)
		}
	}))
	t.Cleanup(srv.Close)
	return newTestService(t, srv)
}

func TestSnippetExampleList(t *testing.T) {
	res, err := registryServer(t, 200).SnippetExample(context.Background(), "rule", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Type != "RULE" || len(res.Examples) != 1 || res.Selected != nil {
		t.Errorf("got %+v", res)
	}
}

func TestSnippetExampleSelectedAndNeeds(t *testing.T) {
	res, err := registryServer(t, 200).SnippetExample(context.Background(), "TRUST_CERT", "cert-variable")
	if err != nil {
		t.Fatal(err)
	}
	content, err := SnippetExampleContent(res.Selected)
	if err != nil || !strings.Contains(content, "\n  \"name\": \"fw\"") {
		t.Errorf("content not indented JSON: %q %v", content, err)
	}
	if got := SnippetExampleNeeds(res.Selected); got != "Needs: variables FW_KEY; examples TRUST_CA/trust-ca-variable" {
		t.Errorf("needs = %q", got)
	}
}

func TestSnippetExampleUnknownNamesChoices(t *testing.T) {
	s := registryServer(t, 200)
	_, err := s.SnippetExample(context.Background(), "NOPE", "")
	if err == nil || !strings.Contains(err.Error(), "RULE, TRUST_CERT") {
		t.Errorf("type error = %v", err)
	}
	_, err = s.SnippetExample(context.Background(), "RULE", "nope")
	if err == nil || !strings.Contains(err.Error(), "allow-https") {
		t.Errorf("id error = %v", err)
	}
}

func TestSnippetExampleOlderServer(t *testing.T) {
	_, err := registryServer(t, 404).SnippetExample(context.Background(), "RULE", "")
	if err == nil || err.Error() != "this server has no snippet examples" {
		t.Errorf("err = %v", err)
	}
	if ErrorCode(err) != CodeServerTooOld {
		t.Errorf("code = %s", ErrorCode(err))
	}
}

func TestSnippetExampleCodedNotFoundKeepsItsCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"code":"SOMETHING_ELSE","detail":"gone"}`)
	}))
	defer srv.Close()
	_, err := newTestService(t, srv).SnippetExample(context.Background(), "RULE", "")
	if err == nil || strings.Contains(err.Error(), "no snippet examples") {
		t.Fatalf("a coded 404 must not read as an old server: %v", err)
	}
	if got := ErrorCode(err); got != "SOMETHING_ELSE" {
		t.Errorf("code = %q, want SOMETHING_ELSE", got)
	}
}

func TestSnippetExampleKeepsRegistryKeyOrder(t *testing.T) {
	res, err := registryServer(t, 200).SnippetExample(context.Background(), "RULE", "allow-https")
	if err != nil {
		t.Fatal(err)
	}
	content, err := SnippetExampleContent(res.Selected)
	if err != nil {
		t.Fatal(err)
	}
	if want := "{\n  \"interface\": \"lan\",\n  \"action\": \"pass\"\n}"; content != want {
		t.Errorf("content = %q, want %q", content, want)
	}
}

func registryWith(t *testing.T, body string) *Service {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return newTestService(t, srv)
}

func TestSnippetExampleRefusesBrokenContent(t *testing.T) {
	// A raw ESC byte inside a JSON string is not valid JSON.
	_, err := registryWith(t, "{\"types\":[{\"type\":\"RULE\",\"examples\":[{\"id\":\"a\",\"name\":\"A\",\"content\":{\"x\":\"\x1b[2J\"}}]}]}").
		SnippetExample(context.Background(), "RULE", "a")
	if err == nil {
		t.Error("raw control character in content must be refused")
	}
	_, err = registryWith(t, `{"types":[{"type":"RULE","examples":[{"id":"a","name":"A"}]}]}`).
		SnippetExample(context.Background(), "RULE", "a")
	if err == nil {
		t.Error("missing content must be refused")
	}
}

func TestSnippetExampleStripsC1Controls(t *testing.T) {
	res, err := registryWith(t, "{\"types\":[{\"type\":\"RULE\",\"examples\":[{\"id\":\"a\",\"name\":\"A\",\"content\":{\"x\":\"a\u009bb\"}}]}]}").
		SnippetExample(context.Background(), "RULE", "a")
	if err != nil {
		t.Fatal(err)
	}
	content, _ := SnippetExampleContent(res.Selected)
	if strings.ContainsRune(content, 0x9b) {
		t.Errorf("C1 control survived: %q", content)
	}
}
