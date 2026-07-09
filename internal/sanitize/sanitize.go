// Package sanitize strips terminal control sequences from
// server-supplied string data before it reaches an output formatter.
//
// NDManager response bodies flow directly into fmt.Fprintf/sb.WriteString
// calls across the table/simple/detailed/json formatters. Without
// stripping, a hostile or compromised control plane (or any org member
// naming a device/variable/etc.) can embed ANSI/VT escape sequences that
// the operator's terminal will interpret: screen clears, cursor moves,
// title-bar spoofing, alternate-screen switches, or OSC-52 clipboard
// writes. Coverage spans both the 7-bit C0 control range (the classic
// ESC-prefixed introducers, e.g. ESC [ for CSI, ESC ] for OSC) and the
// 8-bit C1 control range in its UTF-8 encoded form (U+0080-U+009F),
// since some terminals (notably tmux and various VTE-based emulators)
// honor the single-byte-equivalent C1 introducers — CSI (U+009B) and OSC
// (U+009D) chief among them — as readily as the 7-bit ESC-prefixed forms.
package sanitize

import (
	"reflect"
	"strings"
)

// String removes control code points a terminal would interpret as the
// start of an escape/control sequence: the C0 range (ESC 0x1B, BEL 0x07,
// and the rest of 0x00-0x1F, plus DEL 0x7F) and the C1 range as decoded
// UTF-8 runes (U+0080-U+009F, which includes the 8-bit CSI U+009B and OSC
// U+009D introducers), while preserving tab, newline, and CR so multi-line
// descriptions/messages still render correctly.
//
// This is a rune-based scan rather than a byte-based one specifically to
// cover the C1 range safely: C1 code points are multi-byte in UTF-8 (2
// bytes each), so stripping them by byte value would either miss them
// entirely or risk shredding an unrelated multi-byte character that
// happens to share a byte value with a C1 code point. Decoding rune by
// rune sidesteps that: every rune this function removes is either a
// single-byte C0/DEL control or a decoded C1 control, never a fragment of
// a different valid character.
func String(s string) string {
	hasControl := false
	for _, r := range s {
		if isStrippedRune(r) {
			hasControl = true
			break
		}
	}
	if !hasControl {
		return s
	}

	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if !isStrippedRune(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func isStrippedRune(r rune) bool {
	switch r {
	case '\t', '\n', '\r':
		return false
	}
	// C0 (0x00-0x1F) + DEL (0x7F), or C1 (0x80-0x9F).
	return r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f)
}

// Struct walks a decoded value in place — struct fields, pointers,
// slices/arrays, and map values with string keys — and sanitizes every
// string leaf found via String(). It is meant to be called once, right
// after a successful JSON decode, on the value produced by
// json.Decoder.Decode (typically reflect.ValueOf(target) where target is
// a pointer).
//
// Kinds that can't be inspected without risking a panic on arbitrary
// decoded data (interface, chan, func, and unexported/unaddressable
// fields) are left untouched — a safe no-op rather than a new crash
// surface. Map values obtained via MapIndex aren't addressable, so only
// leaf string values in a map are rewritten (via SetMapIndex); nested
// structs/pointers inside a map value are not sanitized. No current
// model needs that, but it's a known limitation.
func Struct(v reflect.Value) {
	if !v.IsValid() {
		return
	}

	switch v.Kind() {
	case reflect.Ptr:
		if v.IsNil() {
			return
		}
		Struct(v.Elem())
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			field := v.Field(i)
			if !field.CanSet() {
				continue
			}
			Struct(field)
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			Struct(v.Index(i))
		}
	case reflect.Map:
		sanitizeMap(v)
	case reflect.String:
		if v.CanSet() {
			v.SetString(String(v.String()))
		}
	default:
		// bool, numeric, interface, chan, func, etc. — nothing to sanitize.
	}
}

// sanitizeMap rewrites string-valued map entries in place. Map values
// are not addressable, so each value is read, sanitized, and written
// back via SetMapIndex rather than mutated directly.
func sanitizeMap(v reflect.Value) {
	if v.IsNil() {
		return
	}
	for _, key := range v.MapKeys() {
		val := v.MapIndex(key)
		if val.Kind() != reflect.String {
			continue
		}
		v.SetMapIndex(key, reflect.ValueOf(String(val.String())))
	}
}
