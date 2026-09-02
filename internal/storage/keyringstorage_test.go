package storage

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/config"
)

// newTestKeyringStorage wires a KeyringStorage to an in-memory keyring and
// points the config machinery at a scratch file, so Save/Clear can exercise
// the auth.account bookkeeping without touching the real config or keyring.
func newTestKeyringStorage(t *testing.T, limit int) (*KeyringStorage, *fakeKeyring) {
	t.Helper()
	if err := config.Load(filepath.Join(t.TempDir(), "config.yaml")); err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	fake := newFakeKeyring(limit)
	return newTestKeyringStorageOver(t, fake, limit)
}

// newTestKeyringStorageOver is newTestKeyringStorage over a caller-supplied
// backend, with warnings captured instead of printed.
func newTestKeyringStorageOver(t *testing.T, ops keyringOps, limit int) (*KeyringStorage, *fakeKeyring) {
	t.Helper()
	fake, _ := ops.(*fakeKeyring)
	return &KeyringStorage{
		keyring: chunkedKeyring{
			ops:     ops,
			service: KeyringService,
			limit:   func(string, string) int { return limit },
		},
		warn: &bytes.Buffer{},
	}, fake
}

// TestKeyringStorageRoundTripTracksAccountInConfig covers the bookkeeping that
// makes multi-config isolation work: Load has no argument, so it can only find
// the session if Save recorded which account it belongs to.
func TestKeyringStorageRoundTripTracksAccountInConfig(t *testing.T) {
	ks, fake := newTestKeyringStorage(t, 4000)

	const credentialKey = "alice@example.com@control.netdefense.io"
	want := []byte(`{"access_token":"a","refresh_token":"r","token_type":"Bearer"}`)

	if err := ks.Save(want, credentialKey); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if got := config.Get().Auth.Account; got != credentialKey {
		t.Fatalf("Save must record auth.account for a later Load to resolve, got %q", got)
	}
	if got := ks.GetCurrentCredentialKey(); got != credentialKey {
		t.Errorf("GetCurrentCredentialKey() = %q, want %q", got, credentialKey)
	}
	if ks.Name() != "keyring" {
		t.Errorf("Name() = %q, want \"keyring\"", ks.Name())
	}

	got, err := ks.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("Load returned %q, want %q", got, want)
	}

	if err := ks.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if got := config.Get().Auth.Account; got != "" {
		t.Errorf("Clear must forget the account, config still has %q", got)
	}
	if len(fake.entries) != 0 {
		t.Errorf("Clear left %d keyring entries behind: %v", len(fake.entries), fake.entries)
	}

	// With no account configured there is nothing to resolve.
	after, err := ks.Load()
	if err != nil {
		t.Fatalf("Load after Clear: %v", err)
	}
	if after != nil {
		t.Errorf("Load after Clear returned %q, want nil", after)
	}
}

// TestKeyringStorageOversizedPayloadRoundTrips is the reported bug at the
// Storage-interface level: a bundle larger than one keyring secret must save
// and load through KeyringStorage, not just through chunkedKeyring.
func TestKeyringStorageOversizedPayloadRoundTrips(t *testing.T) {
	const limit = 400
	ks, fake := newTestKeyringStorage(t, limit)

	const credentialKey = "alice@example.com@control.netdefense.io"
	want := bytes.Repeat([]byte("t"), limit*6)

	if err := ks.Save(want, credentialKey); err != nil {
		t.Fatalf("Save of a %d-byte payload into a %d-byte-per-entry keyring: %v", len(want), limit, err)
	}
	if got := fake.partCount(KeyringService, credentialKey); got == 0 {
		t.Error("expected the oversized payload to be split across part entries")
	}

	got, err := ks.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("Load returned %d bytes, want %d", len(got), len(want))
	}

	if err := ks.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if len(fake.entries) != 0 {
		t.Errorf("Clear left %d keyring entries behind", len(fake.entries))
	}
}

func TestKeyringStorageSaveRequiresCredentialKey(t *testing.T) {
	ks, fake := newTestKeyringStorage(t, 4000)

	err := ks.Save([]byte("payload"), "")
	if err == nil {
		t.Fatal("expected Save to reject an empty credential key")
	}
	if len(fake.entries) != 0 {
		t.Errorf("rejected Save still wrote %d entries", len(fake.entries))
	}
}

func TestKeyringStorageWithoutAccountIsInert(t *testing.T) {
	ks, _ := newTestKeyringStorage(t, 4000)

	got, err := ks.Load()
	if err != nil {
		t.Fatalf("Load with no account configured should not error, got: %v", err)
	}
	if got != nil {
		t.Errorf("Load with no account configured returned %q, want nil", got)
	}
	if err := ks.Clear(); err != nil {
		t.Errorf("Clear with no account configured should be a no-op, got: %v", err)
	}
}

// primaryDeleteErrKeyring fails Delete for the primary entry only, so the
// session secret survives a logout attempt.
type primaryDeleteErrKeyring struct {
	*fakeKeyring
	primary string
}

func (p primaryDeleteErrKeyring) Delete(service, account string) error {
	if account == p.primary {
		return fmt.Errorf("keyring is locked")
	}
	return p.fakeKeyring.Delete(service, account)
}

// TestKeyringStorageClearSucceedsWhenOnlyCleanupFails covers a logout where the
// session secret is genuinely gone but stale part entries could not be swept.
// Reporting that as "logout failed" would send the user chasing a failure that
// did not happen, and would leave auth.account pointing at a deleted session.
func TestKeyringStorageClearSucceedsWhenOnlyCleanupFails(t *testing.T) {
	if err := config.Load(filepath.Join(t.TempDir(), "config.yaml")); err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	fake := newFakeKeyring(4000)
	warn := &bytes.Buffer{}
	ks := &KeyringStorage{
		keyring: chunkedKeyring{
			ops:     partDeleteErrKeyring{fakeKeyring: fake},
			service: KeyringService,
			limit:   func(string, string) int { return 4000 },
		},
		warn: warn,
	}

	const credentialKey = "alice@example.com@control.netdefense.io"
	if err := ks.Save([]byte(`{"access_token":"a"}`), credentialKey); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// A stale part entry that cleanup will not be able to remove.
	if err := fake.Set(KeyringService, partAccount(credentialKey, 0, 1), "stale"); err != nil {
		t.Fatalf("seed stale part: %v", err)
	}

	if err := ks.Clear(); err != nil {
		t.Fatalf("Clear must succeed when only housekeeping failed, got: %v", err)
	}
	if got := config.Get().Auth.Account; got != "" {
		t.Errorf("Clear must still forget the account, config has %q", got)
	}
	if _, err := fake.Get(KeyringService, credentialKey); err == nil {
		t.Error("the session secret should have been removed")
	}
	if !strings.Contains(warn.String(), "leftover keyring entries") {
		t.Errorf("expected a warning about the leftover entries, got: %q", warn.String())
	}
}

// TestKeyringStorageClearFailsWhenSessionSurvives is the other side: if the
// primary entry is still there the logout really did fail, so the error must
// propagate and auth.account must stay put for a retry to find.
func TestKeyringStorageClearFailsWhenSessionSurvives(t *testing.T) {
	if err := config.Load(filepath.Join(t.TempDir(), "config.yaml")); err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	fake := newFakeKeyring(4000)
	const credentialKey = "alice@example.com@control.netdefense.io"

	ks, _ := newTestKeyringStorageOver(t, fake, 4000)
	if err := ks.Save([]byte(`{"access_token":"a"}`), credentialKey); err != nil {
		t.Fatalf("Save: %v", err)
	}

	blocked := &KeyringStorage{
		keyring: chunkedKeyring{
			ops:     primaryDeleteErrKeyring{fakeKeyring: fake, primary: credentialKey},
			service: KeyringService,
			limit:   func(string, string) int { return 4000 },
		},
		warn: &bytes.Buffer{},
	}
	err := blocked.Clear()
	if err == nil {
		t.Fatal("Clear must fail while the session secret is still stored")
	}
	if got := config.Get().Auth.Account; got != credentialKey {
		t.Errorf("a failed Clear must leave auth.account in place for a retry, got %q", got)
	}
	if _, getErr := fake.Get(KeyringService, credentialKey); getErr != nil {
		t.Error("the session secret should still be present")
	}
}
