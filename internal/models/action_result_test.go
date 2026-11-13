package models

import (
	"encoding/json"
	"testing"
)

// The resource's key and "action" always carry the result's own values: a
// Details entry of the same name cannot replace them.
func TestActionResultKeepsItsReservedKeys(t *testing.T) {
	r := ActionResult{Resource: "template", Name: "base", Action: "snippet_added",
		Details: map[string]interface{}{"template": "other", "action": "other", "snippet": "dns"}}
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]interface{}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["template"] != "base" || doc["action"] != "snippet_added" || doc["snippet"] != "dns" || len(doc) != 3 {
		t.Errorf("json %s", raw)
	}
	if _, present := doc["Message"]; present {
		t.Errorf("Message reached the JSON: %s", raw)
	}
}

// approve-all counts by the Failed flag, not by an empty error text.
func TestApproveAllCountsByTheFailedFlag(t *testing.T) {
	r := ApproveAllResult{Results: []ApproveOutcome{{Device: "fw01"}, {Device: "fw02", Failed: true}}}
	if approved, failed := r.Counts(); approved != 1 || failed != 1 {
		t.Errorf("counts %d/%d", approved, failed)
	}
	raw, _ := json.Marshal(r)
	if string(raw) != `{"approved":1,"approved_names":["fw01"],"failed_count":1,"failed":[{"device":"fw02","error":""}]}` {
		t.Errorf("json %s", raw)
	}
}

// A result without a resource or an action is a bug in the command, refused
// rather than written as {"": …}.
func TestActionResultRefusesAnEmptyResourceOrAction(t *testing.T) {
	for _, r := range []ActionResult{{Name: "x", Action: "created"}, {Resource: "ou", Name: "x"}} {
		if _, err := json.Marshal(r); err == nil {
			t.Errorf("%+v marshalled", r)
		}
	}
}
