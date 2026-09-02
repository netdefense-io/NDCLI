package storage

import (
	"errors"
	"os"
	"testing"

	"github.com/zalando/go-keyring"
)

// liveTestService is deliberately distinct from KeyringService so a live run
// can never touch a real ndcli session.
const (
	liveTestService = "io.netdefense.ndcli-livetest"
	liveTestAccount = "livetest@example.invalid@control.netdefense.io"
)

// TestChunkedKeyringAgainstRealKeyring exercises the chunking path against the
// host's actual keyring, which is the only way to confirm the per-platform size
// budgets match what the OS enforces. It is opt-in because it mutates the
// user's keyring:
//
//	NDCLI_KEYRING_LIVE_TEST=1 go test ./internal/storage/ -run RealKeyring -v
func TestChunkedKeyringAgainstRealKeyring(t *testing.T) {
	if os.Getenv("NDCLI_KEYRING_LIVE_TEST") != "1" {
		t.Skip("set NDCLI_KEYRING_LIVE_TEST=1 to run against the host keyring")
	}

	ck := chunkedKeyring{
		ops:     systemKeyring{},
		service: liveTestService,
		limit:   maxKeyringSecretBytes,
	}
	t.Cleanup(func() {
		if err := ck.clear(liveTestAccount); err != nil {
			t.Errorf("cleanup: %v", err)
		}
	})

	// Counts part entries across every generation, so a leftover set from an
	// interrupted write would show up rather than being skipped.
	livePartCount := func() int {
		n := 0
		for gen := 0; gen < generations; gen++ {
			for i := 1; i <= maxParts; i++ {
				if _, err := ck.ops.Get(liveTestService, partAccount(liveTestAccount, gen, i)); err != nil {
					if errors.Is(err, keyring.ErrNotFound) {
						break
					}
					t.Fatalf("probing generation %d part %d: %v", gen, i, err)
				}
				n++
			}
		}
		return n
	}

	budget := maxKeyringSecretBytes(liveTestService, liveTestAccount)
	t.Logf("per-entry budget for this platform: %d bytes", budget)

	// Four times the budget: comfortably past what a single secret can hold.
	big := payload(budget * 4)
	if err := ck.save(liveTestAccount, big); err != nil {
		t.Fatalf("save of %d bytes: %v", len(big), err)
	}
	parts := livePartCount()
	t.Logf("saved %d bytes across %d part entries", len(big), parts)
	if parts < 2 {
		t.Fatalf("expected the payload to be split, got %d part entries", parts)
	}

	got, err := ck.load(liveTestAccount)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if string(got) != string(big) {
		t.Fatalf("round trip mismatch: read %d bytes, wrote %d", len(got), len(big))
	}

	// Shrinking back under the budget must retire every part entry.
	small := payload(64)
	if err := ck.save(liveTestAccount, small); err != nil {
		t.Fatalf("second save: %v", err)
	}
	if n := livePartCount(); n != 0 {
		t.Errorf("inline rewrite left %d part entries behind", n)
	}
	got, err = ck.load(liveTestAccount)
	if err != nil {
		t.Fatalf("load after shrink: %v", err)
	}
	if string(got) != string(small) {
		t.Fatalf("round trip mismatch after shrink: got %q", got)
	}

	// And clear must leave nothing at all.
	if err := ck.save(liveTestAccount, big); err != nil {
		t.Fatalf("third save: %v", err)
	}
	if err := ck.clear(liveTestAccount); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if n := livePartCount(); n != 0 {
		t.Errorf("clear left %d part entries behind", n)
	}
	if _, err := ck.ops.Get(liveTestService, liveTestAccount); !errors.Is(err, keyring.ErrNotFound) {
		t.Errorf("clear left the primary entry behind (err=%v)", err)
	}
}
