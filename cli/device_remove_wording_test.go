package cli

import (
	"strings"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/service"
)

// TestDeviceRemoveWarningLines_StatesTheOnBoxConsequences pins the pre-
// confirmation warning. Deleting a device is final and makes the box wipe
// itself, so an operator must not be able to answer the prompt without having
// been told that.
func TestDeviceRemoveWarningLines_StatesTheOnBoxConsequences(t *testing.T) {
	lines := deviceRemoveWarningLines("clarence")
	if len(lines) != 2 {
		t.Fatalf("warning is a header plus the shared sentence, got %d lines: %v", len(lines), lines)
	}

	joined := strings.Join(lines, " ")
	if !strings.Contains(joined, "clarence") {
		t.Errorf("warning must name the device; got: %s", joined)
	}
	// Verbatim, not a paraphrase: a reworded copy here would let this surface
	// drift from the MCP tool description and the TUI modal.
	if !strings.Contains(joined, service.DeviceRemoveConsequence) {
		t.Errorf("warning must carry the shared consequence wording; got: %s", joined)
	}
}

// TestDeviceRemoveConsequence_MatchesTheCanonicalSentence pins the shared
// constant against a literal copy of the canonical decommission sentence,
// rather than only against itself the way every other surface's test
// does — every one of those tests would keep passing if the constant
// itself drifted.
func TestDeviceRemoveConsequence_MatchesTheCanonicalSentence(t *testing.T) {
	const canonical = "Permanently deletes the device. On its next contact the device removes all NetDefense-managed configuration (firewall rules, aliases, VPN, users, external authentication servers, package repositories), uninstalls the NetDefense agent and repository, and cannot be restored. Re-adding it requires a fresh installation."
	if service.DeviceRemoveConsequence != canonical {
		t.Errorf("DeviceRemoveConsequence drifted from the canonical sentence:\ngot:  %s\nwant: %s", service.DeviceRemoveConsequence, canonical)
	}
}

// TestDeviceRemoveHelp_SaysPermanentAndSelfDecommission keeps --help honest:
// the long help has to carry the same finality as the prompt, because a
// scripted caller reads the help and never sees the prompt.
func TestDeviceRemoveHelp_SaysPermanentAndSelfDecommission(t *testing.T) {
	long := deviceRemoveCmd.Long
	if !strings.Contains(long, "permanent") {
		t.Errorf("long help must say removal is permanent, got: %s", long)
	}
	if !strings.Contains(long, "self-decommission") {
		t.Errorf("long help must name the self-decommission, got: %s", long)
	}
	if !strings.Contains(long, service.DeviceRemoveConsequence) {
		t.Errorf("long help must carry the shared consequence wording, got: %s", long)
	}
}
