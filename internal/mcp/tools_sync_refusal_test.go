package mcp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func syncApplyAnswering(t *testing.T, status int, body string) ToolResponse {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/sync" {
			t.Errorf("unexpected request path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()

	result, err := newTestServer(t, srv, "acme").syncApply(context.Background(), &syncApplyInput{Confirm: true})
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	var resp ToolResponse
	decodeToolResult(t, result, &resp)
	return resp
}

// A sync every device refused is a failed call, as the CLI's exit status
// says, with the per-device reasons still in data.
func TestSyncApply_EveryDeviceRefusedIsAFailedCall(t *testing.T) {
	resp := syncApplyAnswering(t, http.StatusBadRequest,
		`{"message":"Sync triggered for 0 device(s); 2 error(s)","devices_affected":0,"skipped":0,"tasks":[],`+
			`"errors":[{"device_name":"fw-a","error":"device offline"},{"device_name":"fw-b","error":"device offline"}]}`)

	if resp.Success {
		t.Fatal("success = true for a sync every device refused")
	}
	if resp.Error == nil || resp.Error.Message != "sync failed for all devices" || resp.Error.Code != "API_ERROR" {
		t.Errorf("error = %+v, want API_ERROR / sync failed for all devices", resp.Error)
	}
	data, _ := resp.Data.(map[string]interface{})
	if errs, _ := data["errors"].([]interface{}); len(errs) != 2 {
		t.Errorf("data.errors = %v, want both device errors", data["errors"])
	}
	if data["status_code"] != float64(http.StatusBadRequest) {
		t.Errorf("data.status_code = %v, want 400", data["status_code"])
	}
}

// A sync that triggered any device stays a successful call, its errors in data.
func TestSyncApply_PartialSyncIsASuccessfulCall(t *testing.T) {
	resp := syncApplyAnswering(t, http.StatusMultiStatus,
		`{"message":"Sync triggered for 1 device(s); 1 error(s)","devices_affected":1,"skipped":0,`+
			`"tasks":[{"task":"T1","device_name":"fw-a"}],"errors":[{"device_name":"fw-b","error":"device offline"}]}`)

	if !resp.Success {
		t.Fatalf("success = false for a partial sync: %+v", resp.Error)
	}
}

// A refusal of the request itself carries the server's code.
func TestSyncApply_RefusedRequestCarriesTheServerCode(t *testing.T) {
	resp := syncApplyAnswering(t, http.StatusForbidden,
		`{"error":"This personal access token is read-only","code":"TOKEN_READ_ONLY"}`)

	if resp.Success {
		t.Fatal("success = true for a 403")
	}
	if resp.Error == nil || resp.Error.Code != "TOKEN_READ_ONLY" {
		t.Errorf("error = %+v, want code TOKEN_READ_ONLY", resp.Error)
	}
}

// A 400 that names no refused device is the server's error, not a sync result.
func TestSyncApply_PlainBadRequestCarriesTheServerCode(t *testing.T) {
	resp := syncApplyAnswering(t, http.StatusBadRequest, `{"error":"Invalid device pattern","code":"INVALID_PATTERN"}`)

	if resp.Success {
		t.Fatal("success = true for a plain 400")
	}
	if resp.Error == nil || resp.Error.Code != "INVALID_PATTERN" || resp.Error.Message != "Invalid device pattern" {
		t.Errorf("error = %+v, want INVALID_PATTERN / Invalid device pattern", resp.Error)
	}
}
