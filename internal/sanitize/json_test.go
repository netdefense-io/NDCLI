package sanitize

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

// JSON takes every control String removes out of JSON text, raw or escaped,
// and keeps the rest byte for byte.
func TestJSON(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"escaped ESC", `{"a":"x\u001by"}`, `{"a":"xy"}`},
		{"upper-case hex", `{"a":"x\u001By"}`, `{"a":"xy"}`},
		{"escaped BEL in a key", `{"k\u0007":1}`, `{"k":1}`},
		{"C1 escape", `{"a":"\u009b31m"}`, `{"a":"31m"}`},
		{"DEL escape", `{"a":"\u007f"}`, `{"a":""}`},
		{"backspace and form feed", `{"a":"\b\f\n\t\r"}`, `{"a":"\n\t\r"}`},
		{"tab and newline as \\u", `{"a":"\u0009\u000a"}`, `{"a":"\u0009\u000a"}`},
		{"raw C1 and DEL", "{\"a\":\"x\u009by\u007fz\"}", `{"a":"xyz"}`},
		{"escaped backslash before u001b", `{"a":"\\u001b"}`, `{"a":"\\u001b"}`},
		{"escaped backslash then a real escape", `{"a":"\\\u001b"}`, `{"a":"\\"}`},
		{"escaped quote stays in the string", `{"a":"say \"\u001b\" ok"}`, `{"a":"say \"\" ok"}`},
		{"surrogate pair", `{"a":"\ud83d\ude00"}`, `{"a":"\ud83d\ude00"}`},
		{"lone high surrogate", `{"a":"\ud83d\u0007"}`, `{"a":"\ufffd"}`},
		{"lone low surrogate", `{"a":"\ude00\u001b"}`, `{"a":"\ufffd"}`},
		{"control between surrogate halves", `{"a":"\ud83d\u001b\ude00"}`, `{"a":"\ufffd\ufffd"}`},
		{"raw DEL between surrogate halves", "{\"a\":\"\\ud83d\u007f\\ude00\"}", `{"a":"\ufffd\ufffd"}`},
		{"backslash before a raw control", "\"\\\u001b\"", `""`},
		{"backslash, raw control, then escape text", "\"\\\u001bu001b\"", `"u001b"`},
		{"backspace between surrogate halves", `{"a":"\ud83d\b\ude00"}`, `{"a":"\ufffd\ufffd"}`},
		{"high, high, low", `{"a":"\ud83d\ud83d\ude00"}`, `{"a":"\ufffd\ud83d\ude00"}`},
		{"escaped backslash, raw control, escape text", "\"\\\\\u007fu001b\"", `"\\u001b"`},
		{"short \\u with a control in its digits", "\"\\u00\u007f1b\"", `"001b"`},
		{"backslash before a byte that is not UTF-8", "\"\\\xe2\u007f\"", `"\ufffd"`},
		{"order, numbers and spacing kept", `{ "z": 1.50, "a": "\u0007", "n": 12345678901234567890 }`, `{ "z": 1.50, "a": "", "n": 12345678901234567890 }`},
		{"other escapes kept", `{"a":"\/é "}`, `{"a":"\/é "}`},
		{"not a string", `[1,true,null]`, `[1,true,null]`},
		{"invalid UTF-8 around a removed DEL", "\"\xd3\x7f\xbc\"", "\"\ufffd\ufffd\""},
	} {
		got := string(JSON([]byte(tc.in)))
		if got != tc.want {
			t.Errorf("%s: JSON(%s) = %s, want %s", tc.name, tc.in, got, tc.want)
		}
		if json.Valid([]byte(tc.in)) && !json.Valid([]byte(got)) {
			t.Errorf("%s: valid input became invalid: %s", tc.name, got)
		}
	}
	clean := []byte(`{"a":"b"}`)
	if got := JSON(clean); &got[0] != &clean[0] {
		t.Error("clean JSON was copied")
	}
}

// For valid JSON, decoding what JSON returns gives what decoding the input and
// scrubbing every string and key would give, and the result stays valid.
func FuzzJSON(f *testing.F) {
	for _, seed := range []string{`{"a":"x\u001by"}`, `{"a":"\\u001b"}`, `["\ud83d\ude00","\b"]`, "\"\u009b\"", `{"k\u0007":[1,"\f"]}`, `"\ud83d\u001b"`, "\"\xd3\x7f\xbc\"", `{"a\u0007":1,"a":2}`, `"\ud83d\u001b\ude00"`, `{"n":1.50e+3}`} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		if !json.Valid(b) {
			return
		}
		out := JSON(b)
		if !json.Valid(out) {
			t.Fatalf("JSON(%q) = %q is not valid", b, out)
		}
		in, got := decodeNumbers(b), decodeNumbers(out)
		if hasStrippedRune(got) {
			t.Fatalf("JSON(%q) = %q still decodes to a control: %#v", b, out, got)
		}
		if keysCollide(in) {
			return
		}
		Struct(reflect.ValueOf(&in))
		Struct(reflect.ValueOf(&got))
		if !reflect.DeepEqual(in, got) {
			t.Fatalf("JSON(%q) = %q decodes to %#v, want %#v", b, out, got, in)
		}
	})
}

// keysCollide reports whether two keys of an object in v scrub to the same
// one: JSON keeps both and a decoder the later, where Struct keeps the
// rewritten one.
func keysCollide(v interface{}) bool {
	switch t := v.(type) {
	case map[string]interface{}:
		seen := make(map[string]bool, len(t))
		for k, e := range t {
			clean := String(k)
			if seen[clean] || keysCollide(e) {
				return true
			}
			seen[clean] = true
		}
	case []interface{}:
		for _, e := range t {
			if keysCollide(e) {
				return true
			}
		}
	}
	return false
}

// decodeNumbers decodes b keeping each number's text, so the fuzz property
// also sees a change to it.
func decodeNumbers(b []byte) interface{} {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var v interface{}
	_ = dec.Decode(&v)
	return v
}

// hasStrippedRune reports whether a string or key in v holds a rune String
// removes.
func hasStrippedRune(v interface{}) bool {
	switch t := v.(type) {
	case string:
		return String(t) != t
	case map[string]interface{}:
		for k, e := range t {
			if String(k) != k || hasStrippedRune(e) {
				return true
			}
		}
	case []interface{}:
		for _, e := range t {
			if hasStrippedRune(e) {
				return true
			}
		}
	}
	return false
}
