package helpers

import (
	"strings"
	"testing"
)

func TestPrettyJSONObject(t *testing.T) {
	got := PrettyJSON(`{"b":1,"a":2}`)
	want := "{\n  \"b\": 1,\n  \"a\": 2\n}"
	if got != want {
		t.Errorf("PrettyJSON mismatch:\n got %q\nwant %q", got, want)
	}
}

func TestPrettyJSONPreservesKeyOrder(t *testing.T) {
	got := PrettyJSON(`{"z":1,"a":2,"m":3}`)
	if !strings.Contains(got, "\"z\": 1") || strings.Index(got, "\"z\"") > strings.Index(got, "\"a\"") {
		t.Errorf("key order not preserved: %q", got)
	}
}

func TestPrettyJSONInvalidUnchanged(t *testing.T) {
	in := "not json at all"
	if PrettyJSON(in) != in {
		t.Errorf("invalid JSON should pass through unchanged")
	}
}

func TestPrettyJSONEmpty(t *testing.T) {
	if PrettyJSON("") != "" {
		t.Errorf("empty string should pass through")
	}
}

func TestMinifyJSONStripsWhitespace(t *testing.T) {
	in := "{\n  \"a\": 1,\n  \"b\": 2\n}"
	got := MinifyJSON(in)
	want := `{"a":1,"b":2}`
	if got != want {
		t.Errorf("MinifyJSON mismatch:\n got %q\nwant %q", got, want)
	}
}

func TestMinifyJSONInvalidUnchanged(t *testing.T) {
	in := "garbage"
	if MinifyJSON(in) != in {
		t.Errorf("invalid JSON should pass through unchanged")
	}
}

func TestRoundTripIdentity(t *testing.T) {
	original := `{"name":"test","items":[1,2,3],"nested":{"k":"v"}}`
	if got := MinifyJSON(PrettyJSON(original)); got != original {
		t.Errorf("round-trip changed minified form: got %q want %q", got, original)
	}
}
