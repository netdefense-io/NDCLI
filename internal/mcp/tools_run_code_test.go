package mcp

import (
	"testing"

	"github.com/netdefense-io/NDCLI/internal/models"
)

func TestRunResultResponseCarriesTheRunCode(t *testing.T) {
	data, summary := runResultResponse(&models.RunResult{Type: "REBOOT", Organization: "acme", RunID: "Rn4kT8qW", Total: 1}, nil, "REBOOT")
	if data["run_id"] != "Rn4kT8qW" || summary != "1 REBOOT task(s) created; run Rn4kT8qW" {
		t.Errorf("run_id = %v, summary = %q", data["run_id"], summary)
	}

	data, summary = runResultResponse(&models.RunResult{Type: "REBOOT", Organization: "acme", Total: 1}, nil, "REBOOT")
	if _, ok := data["run_id"]; ok || summary != "1 REBOOT task(s) created" {
		t.Errorf("without a run code: data = %v, summary = %q", data, summary)
	}
}
