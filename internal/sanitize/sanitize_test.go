package sanitize

import (
	"reflect"
	"strings"
	"testing"
)

func TestString_StripsEscapeAndControlBytes(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"clear screen", "evil\x1b[2J\x1b[Hname", "evil[2J[Hname"},
		{"osc title", "\x1b]0;pwned\x07visible", "]0;pwnedvisible"},
		{"bel alone", "beep\x07here", "beephere"},
		{"del", "foo\x7fbar", "foobar"},
		{"tab and newline preserved", "line1\tcol\nline2", "line1\tcol\nline2"},
		{"carriage return preserved", "line1\r\nline2", "line1\r\nline2"},
		{"clean string untouched", "perfectly normal name", "perfectly normal name"},
		{"empty string", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := String(tc.in)
			if got != tc.want {
				t.Errorf("String(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if strings.ContainsRune(got, '\x1b') {
				t.Errorf("String(%q) result still contains ESC: %q", tc.in, got)
			}
		})
	}
}

// TestString_StripsC1ControlBytes is REVERT-SENSITIVE: it fails against the
// pre-amendment String(), which only stripped 7-bit C0 controls (0x00-0x1F)
// and DEL (0x7F) at the byte level. The 8-bit C1 range (U+0080-U+009F) —
// notably the single-byte-equivalent CSI (U+009B) and OSC (U+009D)
// introducers, honored by tmux and various VTE-based terminals exactly like
// their 7-bit ESC-prefixed forms — passed straight through unfiltered.
func TestString_StripsC1ControlBytes(t *testing.T) {
	csi := string(rune(0x9b)) // 8-bit CSI introducer
	osc := string(rune(0x9d)) // 8-bit OSC introducer

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"8-bit CSI", "evil" + csi + "2Jname", "evil2Jname"},
		{"8-bit OSC", "evil" + osc + "0;pwnedname", "evil0;pwnedname"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := String(tc.in)
			if got != tc.want {
				t.Errorf("String(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if strings.ContainsRune(got, rune(0x9b)) || strings.ContainsRune(got, rune(0x9d)) {
				t.Errorf("String(%q) result still contains a C1 control rune: %q", tc.in, got)
			}
		})
	}
}

// TestString_StripsFullC1Range exercises every code point in U+0080-U+009F
// (not just CSI/OSC) to guard against a narrower fix that only special-cased
// the two introducers named in the finding.
func TestString_StripsFullC1Range(t *testing.T) {
	for r := rune(0x80); r <= 0x9f; r++ {
		in := "a" + string(r) + "b"
		got := String(in)
		if got != "ab" {
			t.Errorf("String(%q) = %q, want %q (C1 rune U+%04X not stripped)", in, got, "ab", r)
		}
	}
}

type leaf struct {
	Name string
}

type nested struct {
	Title string
	Items []leaf
	Meta  map[string]string
	Child *leaf
	Skip  int
}

func TestStruct_RecursesThroughFieldsSlicesPointersAndMaps(t *testing.T) {
	v := &nested{
		Title: "evil\x1b[2Jname",
		Items: []leaf{
			{Name: "\x1b]0;pwned\x07item"},
		},
		Meta: map[string]string{
			"key": "tainted\x1b[31mvalue",
		},
		Child: &leaf{Name: "child\x1bname"},
		Skip:  7,
	}

	Struct(reflect.ValueOf(v))

	if strings.ContainsRune(v.Title, '\x1b') {
		t.Errorf("Title still contains ESC: %q", v.Title)
	}
	if v.Title != "evil[2Jname" {
		t.Errorf("Title = %q, want surrounding text preserved", v.Title)
	}
	if strings.ContainsRune(v.Items[0].Name, '\x1b') {
		t.Errorf("Items[0].Name still contains ESC: %q", v.Items[0].Name)
	}
	if got := v.Meta["key"]; strings.ContainsRune(got, '\x1b') {
		t.Errorf("Meta[key] still contains ESC: %q", got)
	}
	if strings.ContainsRune(v.Child.Name, '\x1b') {
		t.Errorf("Child.Name still contains ESC: %q", v.Child.Name)
	}
	if v.Skip != 7 {
		t.Errorf("non-string field must be untouched, got %d", v.Skip)
	}
}

func TestStruct_NilPointerAndEmptyMapDoNotPanic(t *testing.T) {
	v := &nested{}
	Struct(reflect.ValueOf(v)) // Child is nil, Meta/Items are nil — must not panic
}

func TestStruct_InvalidValueIsNoOp(t *testing.T) {
	Struct(reflect.Value{})
}

// TestStructSanitizesCargoMaps covers the shape a model uses to keep
// unmodeled keys — map[string]interface{}, as in Device.Facts and
// SoftwarePolicyContent's extra keys. Every value in such a map reports
// Kind() == Interface, so the string check alone skipped all of them and
// agent-supplied escape sequences reached the formatters intact.
func TestStructSanitizesCargoMaps(t *testing.T) {
	esc := "fw\x1b[2J\x1b]0;pwned\x07"
	clean := "fw[2J]0;pwned"

	payload := map[string]interface{}{
		"hostname": esc,
		"timezone": map[string]interface{}{"name": esc, "utc_offset_sec": float64(-10800)},
		"interfaces": []interface{}{
			map[string]interface{}{"role": esc, "if": "em0", "enabled": true},
		},
		"count": float64(3),
		"flag":  true,
		"null":  nil,
	}
	target := struct {
		Facts map[string]interface{}
	}{Facts: payload}

	Struct(reflect.ValueOf(&target))

	if got := target.Facts["hostname"]; got != clean {
		t.Fatalf("top-level string not sanitized: %q", got)
	}
	tz := target.Facts["timezone"].(map[string]interface{})
	if got := tz["name"]; got != clean {
		t.Fatalf("nested map string not sanitized: %q", got)
	}
	if tz["utc_offset_sec"] != float64(-10800) {
		t.Fatalf("a non-string value was altered: %v", tz["utc_offset_sec"])
	}
	ifaces := target.Facts["interfaces"].([]interface{})
	entry := ifaces[0].(map[string]interface{})
	if got := entry["role"]; got != clean {
		t.Fatalf("string inside a slice of maps not sanitized: %q", got)
	}
	if entry["enabled"] != true || target.Facts["count"] != float64(3) || target.Facts["flag"] != true {
		t.Fatal("non-string values must pass through untouched")
	}
	if target.Facts["null"] != nil {
		t.Fatal("a null value must stay null")
	}
}

// TestStructSanitizesStringsInsideInterfaceSlices: a bare []interface{}
// of strings is addressable element by element, so it is rewritten in
// place rather than through a map write-back.
func TestStructSanitizesStringsInsideInterfaceSlices(t *testing.T) {
	target := struct{ Items []interface{} }{
		Items: []interface{}{"a\x1b[31mb", float64(1), nil},
	}
	Struct(reflect.ValueOf(&target))
	if target.Items[0] != "a[31mb" {
		t.Fatalf("string in an interface slice not sanitized: %q", target.Items[0])
	}
	if target.Items[1] != float64(1) || target.Items[2] != nil {
		t.Fatal("non-string elements must pass through untouched")
	}
}

// TestSanitizeMapHandlesNamedStringValueTypes: SetMapIndex panics when
// handed a plain string for a map whose element type is a named string
// type, so the write-back converts.
func TestSanitizeMapHandlesNamedStringValueTypes(t *testing.T) {
	type label string
	target := struct{ M map[string]label }{M: map[string]label{"k": "v\x1b[0m"}}
	Struct(reflect.ValueOf(&target))
	if target.M["k"] != "v[0m" {
		t.Fatalf("named string map value not sanitized: %q", target.M["k"])
	}
}
