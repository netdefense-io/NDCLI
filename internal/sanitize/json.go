package sanitize

import (
	"bytes"
	"unicode/utf16"
	"unicode/utf8"
)

// JSON returns the JSON text b with every control character String would
// remove taken out, whether written raw (DEL and the C1 range, which JSON
// allows unescaped in a string) or, in a string, as an escape (\u001b, \b,
// \f). Everything else is kept byte for byte: key order, number text,
// whitespace, other escapes. An escaped backslash is a backslash, so in
// "\\u001b" the u001b is text and stays. A surrogate pair stands for a
// character above the BMP and is never a control.
//
// When b needs rewriting at all (it has a backslash or a raw control), two
// things that decode as U+FFFD are written as U+FFFD, so removing a control
// can never join what surrounded it into a new character: a byte that is not
// UTF-8, and a surrogate escape that is not half of a pair. Two keys of an
// object that scrub to the same one both stay, and a decoder keeps the later.
func JSON(b []byte) []byte {
	if !jsonHasControl(b) {
		return b
	}
	out := make([]byte, 0, len(b))
	inString := false
	for i := 0; i < len(b); {
		c := b[i]
		switch {
		case c == '"':
			inString = !inString
			out = append(out, c)
			i++
		case c == '\\' && inString:
			n, action := jsonEscape(b[i:])
			switch action {
			case escapeKeep:
				out = append(out, b[i:i+n]...)
			case escapeReplacement:
				out = append(out, `\ufffd`...)
			}
			i += n
		default:
			r, size := utf8.DecodeRune(b[i:])
			if r == utf8.RuneError && size <= 1 {
				out = utf8.AppendRune(out, utf8.RuneError)
				i++
				continue
			}
			if !isStrippedRune(r) {
				out = append(out, b[i:i+size]...)
			}
			i += size
		}
	}
	return out
}

// jsonHasControl is a quick check for anything JSON would change: a raw
// stripped rune, or an escape that may stand for one.
func jsonHasControl(b []byte) bool {
	if bytes.IndexByte(b, '\\') >= 0 {
		return true
	}
	for _, r := range string(b) {
		if r != utf8.RuneError && isStrippedRune(r) {
			return true
		}
	}
	return false
}

type escapeAction int

const (
	escapeKeep escapeAction = iota
	escapeDrop
	escapeReplacement
)

// jsonEscape reads the escape at the start of b (b[0] is the backslash) and
// returns its length and what to write for it. A backslash before a raw
// control goes with it: kept alone it could make an escape of what follows.
func jsonEscape(b []byte) (n int, action escapeAction) {
	if len(b) < 2 {
		return len(b), escapeKeep
	}
	switch b[1] {
	case '"', '\\', '/', 'n', 'r', 't':
		return 2, escapeKeep
	case 'b', 'f':
		return 2, escapeDrop
	case 'u':
		r, ok := hex4(b[2:])
		if !ok {
			// Not an escape (invalid JSON). Dropping the \u keeps a removal
			// in the digits after it from making one.
			return 2, escapeDrop
		}
		if utf16.IsSurrogate(r) {
			if len(b) >= 12 && b[6] == '\\' && b[7] == 'u' {
				if lo, ok := hex4(b[8:]); ok && utf16.DecodeRune(r, lo) != utf8.RuneError {
					return 12, escapeKeep
				}
			}
			return 6, escapeReplacement
		}
		if isStrippedRune(r) {
			return 6, escapeDrop
		}
		return 6, escapeKeep
	}
	r, size := utf8.DecodeRune(b[1:])
	switch {
	case isStrippedRune(r):
		return 1 + size, escapeDrop
	case r == utf8.RuneError && size <= 1:
		return 1 + size, escapeReplacement
	}
	return 1 + size, escapeKeep
}

// hex4 reads four hex digits.
func hex4(b []byte) (rune, bool) {
	if len(b) < 4 {
		return 0, false
	}
	var r rune
	for _, c := range b[:4] {
		r <<= 4
		switch {
		case c >= '0' && c <= '9':
			r |= rune(c - '0')
		case c >= 'a' && c <= 'f':
			r |= rune(c-'a') + 10
		case c >= 'A' && c <= 'F':
			r |= rune(c-'A') + 10
		default:
			return 0, false
		}
	}
	return r, true
}
