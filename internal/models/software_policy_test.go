package models

import (
	"encoding/json"
	"testing"
)

func parsed(t *testing.T, raw string) *SoftwarePolicyContent {
	t.Helper()
	c, err := ParseSoftwarePolicyContent(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return c
}

func TestParse_Empty(t *testing.T) {
	c := parsed(t, "")
	if len(c.Present) != 0 || len(c.Absent) != 0 {
		t.Fatalf("empty parse: %+v", c)
	}
}

func TestParse_MissingKeys(t *testing.T) {
	c := parsed(t, `{}`)
	if c.Present == nil || c.Absent == nil {
		t.Fatal("missing keys should land empty arrays, not nil")
	}
}

func TestMarshal_SortedAndCompact(t *testing.T) {
	c := &SoftwarePolicyContent{Present: []string{"z", "a", "m"}, Absent: []string{"b", "a"}}
	out := c.Marshal()
	var roundtrip SoftwarePolicyContent
	if err := json.Unmarshal([]byte(out), &roundtrip); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	if roundtrip.Present[0] != "a" || roundtrip.Present[1] != "m" || roundtrip.Present[2] != "z" {
		t.Errorf("present not sorted: %v", roundtrip.Present)
	}
	if roundtrip.Absent[0] != "a" || roundtrip.Absent[1] != "b" {
		t.Errorf("absent not sorted: %v", roundtrip.Absent)
	}
}

func TestRequire_FreshAdd(t *testing.T) {
	c := parsed(t, `{"present":[],"absent":[]}`)
	o := c.Require("bash")
	if o.Action != "required" || o.From != "" {
		t.Errorf("fresh require: got %+v", o)
	}
	if !containsString(c.Present, "bash") {
		t.Error("bash should be in present")
	}
}

func TestRequire_AlreadyRequired_NoOp(t *testing.T) {
	c := parsed(t, `{"present":["bash"],"absent":[]}`)
	o := c.Require("bash")
	if o.Action != "no-change" || o.From != PackageStateRequired {
		t.Errorf("already required: got %+v", o)
	}
	if o.Changed() {
		t.Error("Changed() should be false on no-change")
	}
}

func TestRequire_MovesFromBlocked(t *testing.T) {
	c := parsed(t, `{"present":[],"absent":["bash"]}`)
	o := c.Require("bash")
	if o.Action != "moved" || o.From != PackageStateBlocked {
		t.Errorf("move from blocked: got %+v", o)
	}
	if containsString(c.Absent, "bash") {
		t.Error("bash should be gone from absent")
	}
	if !containsString(c.Present, "bash") {
		t.Error("bash should be in present")
	}
}

func TestBlock_FreshAdd(t *testing.T) {
	c := parsed(t, `{"present":[],"absent":[]}`)
	o := c.Block("nano")
	if o.Action != "blocked" {
		t.Errorf("fresh block: got %+v", o)
	}
}

func TestBlock_AlreadyBlocked(t *testing.T) {
	c := parsed(t, `{"present":[],"absent":["nano"]}`)
	o := c.Block("nano")
	if o.Action != "no-change" || o.From != PackageStateBlocked {
		t.Errorf("already blocked: got %+v", o)
	}
}

func TestBlock_MovesFromRequired(t *testing.T) {
	c := parsed(t, `{"present":["bash"],"absent":[]}`)
	o := c.Block("bash")
	if o.Action != "moved" || o.From != PackageStateRequired {
		t.Errorf("move from required: got %+v", o)
	}
}

func TestWaive_FromRequired(t *testing.T) {
	c := parsed(t, `{"present":["bash"],"absent":[]}`)
	o := c.Waive("bash")
	if o.Action != "waived" || o.From != PackageStateRequired {
		t.Errorf("waive required: got %+v", o)
	}
	if containsString(c.Present, "bash") {
		t.Error("bash should be gone")
	}
}

func TestWaive_FromBlocked(t *testing.T) {
	c := parsed(t, `{"present":[],"absent":["nano"]}`)
	o := c.Waive("nano")
	if o.Action != "waived" || o.From != PackageStateBlocked {
		t.Errorf("waive blocked: got %+v", o)
	}
}

func TestWaive_NotPresent_NoOp(t *testing.T) {
	c := parsed(t, `{"present":[],"absent":[]}`)
	o := c.Waive("bash")
	if o.Action != "no-change" || o.From != "" {
		t.Errorf("waive nothing: got %+v", o)
	}
}

func TestMutations_DontMutateOriginalSlice(t *testing.T) {
	// Guard against the marshal()-sort accidentally mutating the
	// in-memory content struct between calls. After a sequence of
	// mutations, in-place slice membership should still reflect the
	// last operation, not be aliased to some serialized form.
	c := parsed(t, `{"present":["z","a"],"absent":[]}`)
	c.Marshal()
	if c.Present[0] != "z" || c.Present[1] != "a" {
		t.Errorf("Marshal sorted in place: %v", c.Present)
	}
}

func TestRoundTrip_RequireBlockWaive(t *testing.T) {
	c := parsed(t, EmptySoftwarePolicyContent)
	c.Require("bash")
	c.Require("vim")
	c.Block("nano")
	c.Block("bash") // moves bash to blocked
	c.Waive("vim")  // drops vim entirely
	out := c.Marshal()
	want := `{"present":[],"absent":["bash","nano"]}`
	if out != want {
		t.Errorf("roundtrip mismatch\n got:  %s\n want: %s", out, want)
	}
}
