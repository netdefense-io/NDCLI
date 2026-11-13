package cli

import (
	"encoding/json"
	"fmt"
)

// objectFields is an object's JSON fields, for a command whose MCP tool
// returns the object itself as its result: the CLI's -f json then carries the
// same top-level keys, with "action" beside them. Each value stays the bytes
// the object encoded to, so a spec's request keeps its key order and number
// text. The resource key and "action" are left out: the result writes its own.
func objectFields(v interface{}, resource string) (map[string]interface{}, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("encode the result: %w", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, fmt.Errorf("encode the result: %w", err)
	}
	out := make(map[string]interface{}, len(fields))
	for k, v := range fields {
		if k != resource && k != "action" {
			out[k] = v
		}
	}
	return out, nil
}
