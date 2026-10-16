package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// releasedAgentOperations is the list of named operations the released firewall
// agent understands, written out. The README's security model states it to the
// public, so it describes the agent that is released, not the one in
// development: a feature that ships dark does not change it. The release that
// makes the agent understand another operation changes this list, the count
// and the copies in the other repositories together, and fails this test until
// it does.
var releasedAgentOperations = []string{
	"ping", "config sync", "config pull", "restart", "reboot", "shutdown", "backup", "remote session", "plugin install", "firmware upgrade", "firmware track step",
}

func TestReadmeStatesTheOperationsOfTheReleasedAgent(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	readme := string(raw)

	claim := regexp.MustCompile(`understands exactly \*\*(\w+) named operations\*\*: ([^.]+)\. It rejects anything else\.`).FindStringSubmatch(readme)
	if claim == nil {
		t.Fatal("the README no longer states the agent's named operations in the form this test reads")
	}
	if claim[1] != "eleven" {
		t.Errorf("the README counts %q named operations, want eleven", claim[1])
	}
	if listed := strings.Split(claim[2], ", "); !slices.Equal(listed, releasedAgentOperations) {
		t.Errorf("the README lists the operations\n%q\nwant the released agent's\n%q", listed, releasedAgentOperations)
	}
	if len(releasedAgentOperations) != 11 {
		t.Errorf("the released agent's list holds %d operations, and the README counts eleven", len(releasedAgentOperations))
	}
	if !strings.Contains(readme, "One of those eleven operations is a remote session") {
		t.Error(`the README no longer says "One of those eleven operations is a remote session"`)
	}
}
