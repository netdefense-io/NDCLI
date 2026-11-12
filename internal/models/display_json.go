package models

import (
	"bytes"

	"github.com/netdefense-io/NDCLI/internal/sanitize"
)

// DisplayJSON is a JSON value the client only shows, never sends back: a
// scheduled task's request. Its decode scrubs it with sanitize.JSON, escaped
// controls included, so `-f json | jq -r` cannot turn a \u001b in it into a
// live escape, whichever path decoded it. Its bytes, key order and number text
// are otherwise kept.
type DisplayJSON []byte

// UnmarshalJSON keeps a scrubbed copy of b; encoding/json has already checked
// that b is one valid value.
func (d *DisplayJSON) UnmarshalJSON(b []byte) error {
	*d = DisplayJSON(bytes.Clone(sanitize.JSON(b)))
	return nil
}

func (d DisplayJSON) MarshalJSON() ([]byte, error) {
	if len(d) == 0 {
		return []byte("null"), nil
	}
	return d, nil
}
