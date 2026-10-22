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

// A sync every device refused ends the action with an error, so the TUI
// shows it as a failure rather than a success toast.
func TestSyncActionRefusedForEveryDeviceIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"message":"Sync triggered for 0 device(s); 1 error(s)","devices_affected":0,"skipped":0,"tasks":[],`+
			`"errors":[{"device_name":"fw-01","error":"device offline"}]}`)
	}))
	defer srv.Close()
	svc := service.New(api.NewClient(srv.URL, false, auth.NewStaticTokenProvider("ndpat_test")), nil, nil)

	for _, key := range []string{"s", "S"} {
		msg, err := (syncResource{}).Execute(context.Background(), svc, "acme", "fw-01", key, nil)
		if err == nil || !strings.Contains(err.Error(), "sync failed for all devices") {
			t.Errorf("action %s: msg %q, err %v; want the refusal as an error", key, msg, err)
		}
	}
	if _, err := (deviceResource{}).Execute(context.Background(), svc, "acme", "fw-01", "s", nil); err == nil {
		t.Error("device sync action: want the refusal as an error")
	}
}
