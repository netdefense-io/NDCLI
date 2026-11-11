package resources

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/auth"
	"github.com/netdefense-io/NDCLI/internal/service"
)

// The ping form takes the same 1..100 count as ndcli and the MCP tool.
func TestDevicePingCountRange(t *testing.T) {
	var payloads []any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		payloads = append(payloads, body["payload"])
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"type":"PING","organization":"acme","total":1,"tasks":[{"task":"T1","device_name":"fw1","status":"PENDING"}]}`)
	}))
	defer srv.Close()
	svc := service.New(api.NewClient(srv.URL, false, auth.NewStaticTokenProvider("ndpat_test")), nil, nil)
	ping := func(count string) error {
		_, err := deviceResource{}.Execute(context.Background(), svc, "acme", "fw1", "p", map[string]string{"host": "1.1.1.1", "count": count})
		return err
	}

	for _, count := range []string{"0", "101", "1000", "ten"} {
		payloads = nil
		if err := ping(count); err == nil || err.Error() != "count must be a number between 1 and 100" {
			t.Errorf("count %q: err = %v, want the 1 to 100 refusal", count, err)
		}
		if len(payloads) != 0 {
			t.Errorf("count %q: %d request(s) sent, want none", count, len(payloads))
		}
	}

	for count, want := range map[string]any{
		"":    map[string]any{"target": "1.1.1.1"},
		"4":   map[string]any{"target": "1.1.1.1"},
		" 4":  map[string]any{"target": "1.1.1.1"},
		"04":  map[string]any{"target": "1.1.1.1"},
		"1":   map[string]any{"target": "1.1.1.1", "count": float64(1)},
		"100": map[string]any{"target": "1.1.1.1", "count": float64(100)},
	} {
		payloads = nil
		if err := ping(count); err != nil {
			t.Fatalf("count %q: %v", count, err)
		}
		if len(payloads) != 1 || !reflect.DeepEqual(payloads[0], want) {
			t.Errorf("count %q: payloads = %v, want [%v]", count, payloads, want)
		}
	}
}
