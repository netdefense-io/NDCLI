package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRunReadsTheRunCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"type":"REBOOT","organization":"acme","run_id":"Rn4kT8qW","total":1,`+
			`"tasks":[{"task":"T1","device_name":"fw1","status":"PENDING"}]}`)
	}))
	defer srv.Close()

	result, err := newTestService(t, srv).Run(context.Background(), "acme", RunOpts{Type: "REBOOT", Devices: []string{"fw1"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.RunID != "Rn4kT8qW" {
		t.Errorf("RunID = %q", result.RunID)
	}
}
