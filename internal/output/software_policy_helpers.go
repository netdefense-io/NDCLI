package output

import "encoding/json"

// softwarePolicyCounts cheaply extracts the present/absent list lengths
// from a SoftwarePolicy JSON content string for table/detailed/simple
// renders. Returns (0, 0) for content that fails to parse — bad rows
// shouldn't crash listings; the describe view will surface the raw
// content so the operator can see what's wrong.
func softwarePolicyCounts(content string) (present, absent int) {
	if content == "" {
		return 0, 0
	}
	var doc struct {
		Present []string `json:"present"`
		Absent  []string `json:"absent"`
	}
	if err := json.Unmarshal([]byte(content), &doc); err != nil {
		return 0, 0
	}
	return len(doc.Present), len(doc.Absent)
}
