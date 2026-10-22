package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

const templateResponse = `{"name":"edge","description":"","position":"APPEND","snippet_count":0,` +
	`"created_at":"2026-10-22T00:00:00Z","updated_at":"2026-10-22T00:00:00Z"}`

type templateRequest struct {
	method, path string
	body         map[string]string
}

func recordTemplateRequests(t *testing.T) (*Service, *[]templateRequest) {
	t.Helper()
	var requests []templateRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := templateRequest{method: r.Method, path: r.URL.Path}
		_ = json.NewDecoder(r.Body).Decode(&req.body)
		requests = append(requests, req)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(templateResponse))
	}))
	t.Cleanup(srv.Close)
	return newTestService(t, srv), &requests
}

// The server takes PREPEND and APPEND in upper case only, and refuses any
// other spelling with a 422.
func TestTemplatePositionIsSentUpperCase(t *testing.T) {
	svc, requests := recordTemplateRequests(t)

	if _, err := svc.TemplateCreate(context.Background(), "acme", TemplateCreateOpts{Name: "edge", Position: "append"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.TemplateUpdate(context.Background(), "acme", "edge", TemplateUpdateOpts{Position: " Prepend "}); err != nil {
		t.Fatalf("update: %v", err)
	}

	if len(*requests) != 2 {
		t.Fatalf("requests = %+v", *requests)
	}
	create, update := (*requests)[0], (*requests)[1]
	if create.method != http.MethodPost || create.body["position"] != "APPEND" {
		t.Errorf("create sent %s %v, want position APPEND", create.method, create.body)
	}
	if update.method != http.MethodPatch || update.path != "/api/v1/organizations/acme/templates/edge" ||
		len(update.body) != 1 || update.body["position"] != "PREPEND" {
		t.Errorf("update sent %s %s %v, want PATCH {position: PREPEND}", update.method, update.path, update.body)
	}
}

// Any other position is refused before a request, so an update that also
// renames does not rename and then fail.
func TestTemplatePositionOtherThanPrependOrAppendIsRefused(t *testing.T) {
	svc, requests := recordTemplateRequests(t)

	_, createErr := svc.TemplateCreate(context.Background(), "acme", TemplateCreateOpts{Name: "edge", Position: "middle"})
	_, updateErr := svc.TemplateUpdate(context.Background(), "acme", "edge", TemplateUpdateOpts{NewName: "edge-2", Position: "middle"})

	for name, err := range map[string]error{"create": createErr, "update": updateErr} {
		var svcErr *Error
		if !errors.As(err, &svcErr) || svcErr.Code != CodeInvalidInput || svcErr.Message != `position must be PREPEND or APPEND, not "middle"` {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if len(*requests) != 0 {
		t.Errorf("requests were sent: %+v", *requests)
	}
}
