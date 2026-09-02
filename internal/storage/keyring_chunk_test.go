package storage

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

// fakeKeyring is an in-memory keyringOps that enforces a per-secret byte cap,
// standing in for the OS keyring so the chunking logic can be driven past its
// limits deterministically.
type fakeKeyring struct {
	entries map[string]string
	// limit is the largest secret Set accepts. Zero means unlimited.
	limit int
}

func newFakeKeyring(limit int) *fakeKeyring {
	return &fakeKeyring{entries: map[string]string{}, limit: limit}
}

func (f *fakeKeyring) key(service, account string) string {
	return service + "\x00" + account
}

func (f *fakeKeyring) Set(service, account, secret string) error {
	if f.limit > 0 && len(secret) > f.limit {
		return keyring.ErrSetDataTooBig
	}
	f.entries[f.key(service, account)] = secret
	return nil
}

func (f *fakeKeyring) Get(service, account string) (string, error) {
	v, ok := f.entries[f.key(service, account)]
	if !ok {
		return "", keyring.ErrNotFound
	}
	return v, nil
}

func (f *fakeKeyring) Delete(service, account string) error {
	k := f.key(service, account)
	if _, ok := f.entries[k]; !ok {
		return keyring.ErrNotFound
	}
	delete(f.entries, k)
	return nil
}

// partCount counts part entries for account across every generation.
func (f *fakeKeyring) partCount(service, account string) int {
	n := 0
	for k := range f.entries {
		if strings.HasPrefix(k, f.key(service, account)+partSuffix) {
			n++
		}
	}
	return n
}

// genPartCount counts part entries for one generation of account.
func (f *fakeKeyring) genPartCount(service, account string, gen int) int {
	n := 0
	for i := 1; i <= maxParts; i++ {
		if _, ok := f.entries[f.key(service, partAccount(account, gen, i))]; ok {
			n++
		}
	}
	return n
}

const (
	testService = "io.netdefense.ndcli-test"
	testAccount = "alice@example.com@control.netdefense.io"
)

// newChunkedKeyring builds a chunkedKeyring over ops whose declared budget is
// limit, regardless of the host platform.
func newChunkedKeyring(ops keyringOps, limit int) chunkedKeyring {
	return chunkedKeyring{
		ops:     ops,
		service: testService,
		limit:   func(string, string) int { return limit },
	}
}

// newTestKeyring returns a chunkedKeyring whose declared limit matches what
// the fake backend actually enforces.
func newTestKeyring(limit int) (chunkedKeyring, *fakeKeyring) {
	fake := newFakeKeyring(limit)
	return newChunkedKeyring(fake, limit), fake
}

func payload(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte('a' + i%26)
	}
	return b
}

func TestChunkedKeyringRoundTripsSmallPayloadInOneEntry(t *testing.T) {
	ck, fake := newTestKeyring(1000)
	want := payload(400)

	if err := ck.save(testAccount, want); err != nil {
		t.Fatalf("save: %v", err)
	}
	if got := fake.partCount(testService, testAccount); got != 0 {
		t.Errorf("payload fits in one entry, expected 0 part entries, got %d", got)
	}

	got, err := ck.load(testAccount)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("load returned %d bytes, want the %d bytes that were saved", len(got), len(want))
	}

	if err := ck.clear(testAccount); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if len(fake.entries) != 0 {
		t.Errorf("clear left %d keyring entries behind: %v", len(fake.entries), fake.entries)
	}
}

func TestChunkedKeyringSplitsOversizedPayload(t *testing.T) {
	const limit = 500
	ck, fake := newTestKeyring(limit)

	// This is the reported failure: a token bundle larger than the keyring
	// will accept in a single secret.
	want := payload(limit*4 + 137)

	if err := ck.save(testAccount, want); err != nil {
		t.Fatalf("save of %d-byte payload into a %d-byte-per-entry keyring: %v", len(want), limit, err)
	}

	parts := fake.partCount(testService, testAccount)
	if parts != 5 {
		t.Errorf("expected %d bytes at %d bytes per entry to use 5 part entries, got %d", len(want), limit, parts)
	}

	primary, err := fake.Get(testService, testAccount)
	if err != nil {
		t.Fatalf("primary entry missing: %v", err)
	}
	manifest, ok := parseManifest(primary)
	if !ok {
		t.Fatalf("primary entry should hold a part manifest, got %q", primary)
	}
	if manifest.Parts != parts || manifest.Bytes != len(want) {
		t.Errorf("manifest = %+v, want Parts=%d Bytes=%d", manifest, parts, len(want))
	}
	if manifest.Sum != checksum(want) {
		t.Errorf("manifest checksum %q does not cover the payload (want %q)", manifest.Sum, checksum(want))
	}

	got, err := ck.load(testAccount)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("reassembled payload does not match what was saved (got %d bytes, want %d)", len(got), len(want))
	}
}

func TestChunkedKeyringReadsSingleEntryWrittenBeforeChunking(t *testing.T) {
	ck, fake := newTestKeyring(4000)

	// Exactly what a release predating chunking left in the keyring.
	legacy := `{"access_token":"legacy-access","refresh_token":"legacy-refresh","token_type":"Bearer","id_token":"legacy-id"}`
	if err := fake.Set(testService, testAccount, legacy); err != nil {
		t.Fatalf("seed: %v", err)
	}

	got, err := ck.load(testAccount)
	if err != nil {
		t.Fatalf("load of a pre-chunking entry must keep working: %v", err)
	}
	if string(got) != legacy {
		t.Errorf("load returned %q, want the untouched legacy document %q", got, legacy)
	}
}

// failNthPartSet fails the nth part write, leaving earlier parts stored. It
// models a keyring that becomes unavailable partway through a multi-part write.
type failNthPartSet struct {
	*fakeKeyring
	failOn int
	seen   int
}

func (f *failNthPartSet) Set(service, account, secret string) error {
	if strings.Contains(account, partSuffix) {
		f.seen++
		if f.seen == f.failOn {
			return fmt.Errorf("keyring is locked")
		}
	}
	return f.fakeKeyring.Set(service, account, secret)
}

// TestChunkedKeyringMidWriteFailureLeavesPreviousSessionIntact is the reason
// part writes go to an unreferenced generation. Without the swap, a write that
// dies partway would leave parts 1..k-1 holding new bytes and k..N holding old
// ones. Because the part count and per-part sizes do not depend on the payload,
// a token refresh writes the same shape as the session it replaces, so the
// spliced result has exactly the expected total length and an aggregate
// byte-count check waves it through -- the user only finds out when the JSON
// fails to parse and they are forced to log in again.
func TestChunkedKeyringMidWriteFailureLeavesPreviousSessionIntact(t *testing.T) {
	const limit = 300
	fake := newFakeKeyring(limit)

	oldSession := bytes.Repeat([]byte("O"), limit*5)
	if err := newChunkedKeyring(fake, limit).save(testAccount, oldSession); err != nil {
		t.Fatalf("storing the original session: %v", err)
	}

	// Same length as the stored session, which is what a refresh looks like.
	newSession := bytes.Repeat([]byte("N"), limit*5)
	failing := &failNthPartSet{fakeKeyring: fake, failOn: 3}
	if err := newChunkedKeyring(failing, limit).save(testAccount, newSession); err == nil {
		t.Fatal("expected the interrupted write to report an error")
	}

	got, err := newChunkedKeyring(fake, limit).load(testAccount)
	if err != nil {
		t.Fatalf("the previous session must still load after an interrupted write: %v", err)
	}
	if !bytes.Equal(got, oldSession) {
		t.Fatalf("interrupted write corrupted the stored session: read %d bytes, %d of which are new-session bytes",
			len(got), bytes.Count(got, []byte("N")))
	}

	// And the account must still be writable afterwards.
	if err := newChunkedKeyring(fake, limit).save(testAccount, newSession); err != nil {
		t.Fatalf("retry after an interrupted write: %v", err)
	}
	got, err = newChunkedKeyring(fake, limit).load(testAccount)
	if err != nil {
		t.Fatalf("load after retry: %v", err)
	}
	if !bytes.Equal(got, newSession) {
		t.Error("retry did not publish the new session")
	}
}

func TestChunkedKeyringAlternatesGenerationsAndPrunesThePrevious(t *testing.T) {
	const limit = 300
	ck, fake := newTestKeyring(limit)

	if err := ck.save(testAccount, payload(limit*3)); err != nil {
		t.Fatalf("first save: %v", err)
	}
	first, ok, err := ck.liveManifest(testAccount)
	if err != nil || !ok {
		t.Fatalf("first save did not publish a manifest (ok=%v, err=%v)", ok, err)
	}
	if first.Gen != 0 {
		t.Errorf("first write should use generation 0, got %d", first.Gen)
	}

	if err := ck.save(testAccount, payload(limit*3)); err != nil {
		t.Fatalf("second save: %v", err)
	}
	second, _, _ := ck.liveManifest(testAccount)
	if second.Gen == first.Gen {
		t.Errorf("second write must land in the other generation, both used %d", second.Gen)
	}

	// Only the live generation should survive the swap.
	if got := fake.genPartCount(testService, testAccount, first.Gen); got != 0 {
		t.Errorf("superseded generation %d still has %d part entries", first.Gen, got)
	}
	if got := fake.genPartCount(testService, testAccount, second.Gen); got != second.Parts {
		t.Errorf("live generation %d has %d part entries, want %d", second.Gen, got, second.Parts)
	}

	// A third write must return to the first generation, not grow the space.
	if err := ck.save(testAccount, payload(limit*3)); err != nil {
		t.Fatalf("third save: %v", err)
	}
	third, _, _ := ck.liveManifest(testAccount)
	if third.Gen != first.Gen {
		t.Errorf("generations should alternate between %d values, got %d", generations, third.Gen)
	}
	if got := fake.partCount(testService, testAccount); got != third.Parts {
		t.Errorf("expected only the live generation to remain (%d entries), got %d", third.Parts, got)
	}
}

func TestChunkedKeyringRewriteDropsStaleParts(t *testing.T) {
	const limit = 300
	ck, fake := newTestKeyring(limit)

	if err := ck.save(testAccount, payload(limit*6)); err != nil {
		t.Fatalf("first save: %v", err)
	}
	if got := fake.partCount(testService, testAccount); got != 6 {
		t.Fatalf("setup: expected 6 part entries, got %d", got)
	}

	// A later login with a smaller bundle must not leave the tail of the
	// previous one behind.
	want := payload(limit*2 + 10)
	if err := ck.save(testAccount, want); err != nil {
		t.Fatalf("second save: %v", err)
	}
	if got := fake.partCount(testService, testAccount); got != 3 {
		t.Errorf("expected the shorter payload to leave 3 part entries, got %d", got)
	}

	got, err := ck.load(testAccount)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("load spliced stale bytes: got %d bytes, want %d", len(got), len(want))
	}
}

func TestChunkedKeyringInlineRewriteDropsStaleParts(t *testing.T) {
	const limit = 300
	ck, fake := newTestKeyring(limit)

	if err := ck.save(testAccount, payload(limit*4)); err != nil {
		t.Fatalf("first save: %v", err)
	}

	// Shrinking below the cap goes back to a single entry; the parts from the
	// chunked write have to go, or clear would orphan them.
	want := payload(50)
	if err := ck.save(testAccount, want); err != nil {
		t.Fatalf("second save: %v", err)
	}
	if got := fake.partCount(testService, testAccount); got != 0 {
		t.Errorf("expected an inline rewrite to remove every part entry, got %d", got)
	}

	got, err := ck.load(testAccount)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("load returned %q, want %q", got, want)
	}
}

func TestChunkedKeyringClearRemovesEveryPart(t *testing.T) {
	const limit = 200
	ck, fake := newTestKeyring(limit)

	if err := ck.save(testAccount, payload(limit*7)); err != nil {
		t.Fatalf("save: %v", err)
	}
	// A second account must survive the clear untouched.
	other := "bob@example.com@control.netdefense.io"
	if err := ck.save(other, payload(limit*3)); err != nil {
		t.Fatalf("save other: %v", err)
	}

	if err := ck.clear(testAccount); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if got := fake.partCount(testService, testAccount); got != 0 {
		t.Errorf("clear left %d part entries behind", got)
	}
	if _, err := fake.Get(testService, testAccount); err == nil {
		t.Error("clear left the primary entry behind")
	}
	if got := fake.partCount(testService, other); got != 3 {
		t.Errorf("clear disturbed another account: expected 3 part entries, got %d", got)
	}
}

// TestChunkedKeyringClearRemovesBothGenerations covers the window where a
// failed write has left parts in the non-live generation: logout still has to
// remove them, because they hold fragments of a real session.
func TestChunkedKeyringClearRemovesBothGenerations(t *testing.T) {
	const limit = 300
	fake := newFakeKeyring(limit)

	if err := newChunkedKeyring(fake, limit).save(testAccount, payload(limit*4)); err != nil {
		t.Fatalf("save: %v", err)
	}
	failing := &failNthPartSet{fakeKeyring: fake, failOn: 3}
	if err := newChunkedKeyring(failing, limit).save(testAccount, payload(limit*4)); err == nil {
		t.Fatal("expected the interrupted write to fail")
	}
	if got := fake.partCount(testService, testAccount); got < 5 {
		t.Fatalf("setup: expected parts in both generations, got %d", got)
	}

	if err := newChunkedKeyring(fake, limit).clear(testAccount); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if len(fake.entries) != 0 {
		t.Errorf("clear left %d entries behind, including orphaned session fragments: %v", len(fake.entries), fake.entries)
	}
}

func TestChunkedKeyringRetriesWhenDeclaredLimitIsTooOptimistic(t *testing.T) {
	// The backend enforces 400 bytes but the limit function claims 8000, the
	// situation that arises if go-keyring changes its internal accounting.
	fake := newFakeKeyring(400)
	ck := newChunkedKeyring(fake, 8000)

	want := payload(3000)
	if err := ck.save(testAccount, want); err != nil {
		t.Fatalf("save should recover from an over-optimistic limit, got: %v", err)
	}

	got, err := ck.load(testAccount)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("load returned %d bytes, want %d", len(got), len(want))
	}
}

// TestChunkedKeyringGivesUpOnceHalvingCannotHelp bounds the retry loop: each
// attempt costs a keyring round trip, which on macOS is a subprocess spawn on
// the synchronous login path.
func TestChunkedKeyringGivesUpOnceHalvingCannotHelp(t *testing.T) {
	// A backend that rejects everything, so every attempt fails.
	always := &alwaysTooBigKeyring{}
	ck := newChunkedKeyring(always, 4096)

	if err := ck.save(testAccount, payload(3000)); err == nil {
		t.Fatal("expected save to give up")
	}

	// 4096 -> 2048 -> 1024 -> 512 -> 256 (floor), then stop: 5 attempts.
	// Anything materially larger means the loop is not converging.
	if always.attempts > 6 {
		t.Errorf("save made %d attempts before giving up; the halving loop should converge in ~5", always.attempts)
	}
	if always.attempts < 2 {
		t.Errorf("save gave up after %d attempts without retrying at all", always.attempts)
	}
}

type alwaysTooBigKeyring struct {
	attempts int
}

func (a *alwaysTooBigKeyring) Set(service, account, secret string) error {
	a.attempts++
	return keyring.ErrSetDataTooBig
}
func (a *alwaysTooBigKeyring) Get(string, string) (string, error) { return "", keyring.ErrNotFound }
func (a *alwaysTooBigKeyring) Delete(string, string) error        { return keyring.ErrNotFound }

func TestChunkedKeyringSaveFailsOnNonSizeErrors(t *testing.T) {
	ck := newChunkedKeyring(errKeyring{err: fmt.Errorf("keyring is locked")}, 1000)

	err := ck.save(testAccount, payload(100))
	if err == nil || !strings.Contains(err.Error(), "locked") {
		t.Fatalf("expected the underlying keyring error to surface, got: %v", err)
	}
}

type errKeyring struct{ err error }

func (e errKeyring) Set(string, string, string) error   { return e.err }
func (e errKeyring) Get(string, string) (string, error) { return "", e.err }
func (e errKeyring) Delete(string, string) error        { return e.err }

func TestChunkedKeyringLoadReportsMissingPart(t *testing.T) {
	const limit = 200
	ck, fake := newTestKeyring(limit)

	if err := ck.save(testAccount, payload(limit*3)); err != nil {
		t.Fatalf("save: %v", err)
	}
	live, _, _ := ck.liveManifest(testAccount)
	if err := fake.Delete(testService, partAccount(testAccount, live.Gen, 2)); err != nil {
		t.Fatalf("delete part: %v", err)
	}

	_, err := ck.load(testAccount)
	if err == nil {
		t.Fatal("expected load to fail when a part entry is missing")
	}
	if !strings.Contains(err.Error(), "auth login") {
		t.Errorf("error should tell the user how to recover, got: %v", err)
	}
}

func TestChunkedKeyringLoadReportsTruncatedPart(t *testing.T) {
	const limit = 200
	ck, fake := newTestKeyring(limit)

	if err := ck.save(testAccount, payload(limit*3)); err != nil {
		t.Fatalf("save: %v", err)
	}
	live, _, _ := ck.liveManifest(testAccount)
	if err := fake.Set(testService, partAccount(testAccount, live.Gen, 2), "short"); err != nil {
		t.Fatalf("truncate part: %v", err)
	}

	_, err := ck.load(testAccount)
	if err == nil {
		t.Fatal("expected load to fail when the reassembled payload is the wrong length")
	}
	if !strings.Contains(err.Error(), "corrupt") {
		t.Errorf("error should say the stored credentials are corrupt, got: %v", err)
	}
}

// TestChunkedKeyringLoadDetectsSameLengthCorruption covers what a byte-count
// check alone cannot: a part replaced by different content of identical length.
func TestChunkedKeyringLoadDetectsSameLengthCorruption(t *testing.T) {
	const limit = 200
	ck, fake := newTestKeyring(limit)

	if err := ck.save(testAccount, payload(limit*3)); err != nil {
		t.Fatalf("save: %v", err)
	}
	live, _, _ := ck.liveManifest(testAccount)
	if err := fake.Set(testService, partAccount(testAccount, live.Gen, 2), strings.Repeat("X", limit)); err != nil {
		t.Fatalf("corrupt part: %v", err)
	}

	_, err := ck.load(testAccount)
	if err == nil {
		t.Fatal("expected load to reject a part whose content changed but whose length did not")
	}
	if !strings.Contains(err.Error(), "checksum") {
		t.Errorf("error should name the checksum mismatch, got: %v", err)
	}
}

func TestChunkedKeyringLoadReturnsNilWhenAbsent(t *testing.T) {
	ck, _ := newTestKeyring(1000)

	got, err := ck.load(testAccount)
	if err != nil {
		t.Fatalf("load of an absent account should not error, got: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for an absent account, got %q", got)
	}
}

func TestChunkedKeyringRejectsPayloadNeedingTooManyParts(t *testing.T) {
	const limit = 100
	ck, _ := newTestKeyring(limit)

	err := ck.save(testAccount, payload(limit*(maxParts+5)))
	if err == nil {
		t.Fatal("expected save to refuse a payload needing more than maxParts entries")
	}
	if !strings.Contains(err.Error(), "keyring entries") {
		t.Errorf("error should explain the entry limit, got: %v", err)
	}
}

func TestParseManifestDoesNotMisreadTokenDocuments(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"token document", `{"access_token":"abc","token_type":"Bearer","user_info":{"email":"a@b.io"}}`},
		{"marker only inside a value", `{"access_token":"ndcli_chunked-not-a-manifest"}`},
		{"marker with wrong version", `{"ndcli_chunked":99,"gen":0,"parts":2,"bytes":10}`},
		{"marker with zero parts", `{"ndcli_chunked":1,"gen":0,"parts":0,"bytes":10}`},
		{"marker with too many parts", fmt.Sprintf(`{"ndcli_chunked":1,"gen":0,"parts":%d,"bytes":10}`, maxParts+1)},
		{"marker with out-of-range generation", fmt.Sprintf(`{"ndcli_chunked":1,"gen":%d,"parts":2,"bytes":10}`, generations)},
		{"marker with negative generation", `{"ndcli_chunked":1,"gen":-1,"parts":2,"bytes":10}`},
		{"marker with negative byte count", `{"ndcli_chunked":1,"gen":0,"parts":2,"bytes":-1}`},
		{"marker claiming an implausible byte count", fmt.Sprintf(`{"ndcli_chunked":1,"gen":0,"parts":2,"bytes":%d}`, maxPayloadBytes+1)},
		{"not json", `ndcli_chunked`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := parseManifest(tc.raw); ok {
				t.Errorf("parseManifest(%q) = true, want false", tc.raw)
			}
		})
	}

	valid, err := json.Marshal(partManifest{Version: manifestVersion, Gen: 1, Parts: 3, Bytes: 42, Sum: "abc"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	m, ok := parseManifest(string(valid))
	if !ok || m.Parts != 3 || m.Bytes != 42 || m.Gen != 1 || m.Sum != "abc" {
		t.Errorf("parseManifest(%q) = %+v, %v; want the round-tripped manifest", valid, m, ok)
	}
}

// shellSafe mirrors the character class al.essio.dev/pkg/shellescape leaves
// unquoted. darwinMaxSecretBytes assumes the service name, account name and
// base64 secret all fall inside it.
var shellSafe = regexp.MustCompile(`^[\w@%+=:,./-]+$`)

func TestDarwinMaxSecretBytesFitsTheSecurityCommandLine(t *testing.T) {
	accounts := []string{
		"a@b.io",
		"alice@example.com@control.netdefense.io",
		strings.Repeat("x", 200) + "@control.netdefense.io",
	}
	for _, account := range accounts {
		t.Run(fmt.Sprintf("account-len-%d", len(account)), func(t *testing.T) {
			n := darwinMaxSecretBytes(KeyringService, account)
			if n <= 0 {
				t.Skipf("no budget for a %d-byte account name", len(account))
			}

			// Rebuild the command go-keyring's macOS backend pipes to
			// /usr/bin/security for a secret of exactly n bytes.
			encoded := darwinSecretPrefix + 4*((n+2)/3)
			command := len("add-generic-password -U -s ") + len(KeyringService) +
				len(" -a ") + len(account) + len(" -w ") + encoded + len("\n")

			if command > darwinCommandLimit {
				t.Errorf("a %d-byte payload builds a %d-byte command, over the %d-byte limit", n, command, darwinCommandLimit)
			}
			// Guard against drifting so conservative that ordinary bundles
			// start getting split for no reason.
			if command < darwinCommandLimit-2*darwinSafetyMargin {
				t.Errorf("budget is needlessly small: a %d-byte payload only reaches %d of %d bytes", n, command, darwinCommandLimit)
			}
			if !shellSafe.MatchString(KeyringService) {
				t.Errorf("service name %q would be shell-quoted, invalidating the budget", KeyringService)
			}
		})
	}
}

// TestDarwinMaxSecretBytesMatchesMeasuredCeiling pins the budget formula to
// ceilings measured by binary-searching keyring.Set against a real macOS
// keychain under a throwaway service name. Only the length of the account name
// enters the formula, so synthetic names of the measured lengths reproduce the
// measurement exactly.
func TestDarwinMaxSecretBytesMatchesMeasuredCeiling(t *testing.T) {
	const probeService = "io.netdefense.ndcli-limitprobe" // 30 bytes
	cases := []struct {
		accountLen int
		measured   int
	}{
		{6, 3003},
		{45, 2973},
		{98, 2934},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("account-len-%d", tc.accountLen), func(t *testing.T) {
			account := strings.Repeat("a", tc.accountLen)
			// The real ceiling is the budget without the safety margin.
			actual := darwinMaxSecretBytes(probeService, account) + darwinSafetyMargin/4*3
			if actual != tc.measured {
				t.Errorf("formula yields a %d-byte ceiling, but the keychain measured %d", actual, tc.measured)
			}
		})
	}
}

func TestMaxKeyringSecretBytesLeavesRoomForATokenBundle(t *testing.T) {
	// A post-fix bundle is around 1.6 KB; every platform must take that
	// without splitting, otherwise the common path pays for chunking.
	const typicalBundle = 1700
	got := maxKeyringSecretBytes(KeyringService, testAccount)
	if got < typicalBundle {
		t.Errorf("per-entry budget is %d bytes, below the %d-byte typical bundle", got, typicalBundle)
	}
}

// partDeleteErrKeyring fails Delete for part entries but not for the primary,
// standing in for a keyring that goes half-unavailable mid-logout.
type partDeleteErrKeyring struct {
	*fakeKeyring
}

func (p partDeleteErrKeyring) Delete(service, account string) error {
	if strings.Contains(account, partSuffix) {
		return fmt.Errorf("keyring is locked")
	}
	return p.fakeKeyring.Delete(service, account)
}

func TestChunkedKeyringSaveSurvivesFailedPartCleanup(t *testing.T) {
	fake := newFakeKeyring(1000)
	ck := newChunkedKeyring(partDeleteErrKeyring{fakeKeyring: fake}, 1000)

	// A login that fits inline must succeed even though the housekeeping
	// delete of leftover parts cannot run.
	want := payload(100)
	if err := ck.save(testAccount, want); err != nil {
		t.Fatalf("save must not fail because stale-part cleanup failed: %v", err)
	}
	got, err := ck.load(testAccount)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("load returned %q, want %q", got, want)
	}
}

func TestChunkedKeyringClearRemovesPrimaryEvenIfPartCleanupFails(t *testing.T) {
	fake := newFakeKeyring(1000)
	if err := fake.Set(testService, testAccount, "the-session"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := fake.Set(testService, partAccount(testAccount, 0, 1), "stale"); err != nil {
		t.Fatalf("seed part: %v", err)
	}

	ck := newChunkedKeyring(partDeleteErrKeyring{fakeKeyring: fake}, 1000)

	err := ck.clear(testAccount)
	if err == nil {
		t.Error("clear should report that part cleanup failed")
	}
	// It has to be reported as housekeeping, not as a failed logout: the
	// session secret itself was removed.
	var cleanup partCleanupError
	if !errors.As(err, &cleanup) {
		t.Errorf("clear returned %T, want a partCleanupError so callers can tell the session is gone", err)
	}
	if _, err := fake.Get(testService, testAccount); err == nil {
		t.Error("clear must still remove the primary entry when part cleanup fails")
	}
}

// getErrKeyring makes reads of the primary entry fail with something other
// than ErrNotFound, modelling a keyring that is writable but momentarily
// unreadable.
type getErrKeyring struct {
	*fakeKeyring
	primary string
	err     error
}

func (g getErrKeyring) Get(service, account string) (string, error) {
	if account == g.primary {
		return "", g.err
	}
	return g.fakeKeyring.Get(service, account)
}

// TestChunkedKeyringRefusesToGuessGenerationWhenPrimaryIsUnreadable covers the
// hole an "any error means nothing is stored" shortcut would open. If the live
// manifest cannot be read, the free generation is unknown, and defaulting to 0
// would write parts straight over a session live in generation 0 -- the exact
// corruption the swap exists to prevent. The write has to fail instead.
func TestChunkedKeyringRefusesToGuessGenerationWhenPrimaryIsUnreadable(t *testing.T) {
	const limit = 300
	fake := newFakeKeyring(limit)

	stored := bytes.Repeat([]byte("O"), limit*4)
	if err := newChunkedKeyring(fake, limit).save(testAccount, stored); err != nil {
		t.Fatalf("storing the original session: %v", err)
	}
	live, ok, err := newChunkedKeyring(fake, limit).liveManifest(testAccount)
	if err != nil || !ok {
		t.Fatalf("setup: no live manifest (ok=%v, err=%v)", ok, err)
	}
	if live.Gen != 0 {
		t.Fatalf("setup expects the live session in generation 0, got %d", live.Gen)
	}

	blocked := getErrKeyring{fakeKeyring: fake, primary: testAccount, err: fmt.Errorf("keyring is locked")}
	err = newChunkedKeyring(blocked, limit).save(testAccount, bytes.Repeat([]byte("N"), limit*4))
	if err == nil {
		t.Fatal("save must not guess a generation when it cannot read the live one")
	}
	if !strings.Contains(err.Error(), "in use") {
		t.Errorf("error should say the live entries could not be determined, got: %v", err)
	}

	// The stored session must be exactly as it was.
	got, loadErr := newChunkedKeyring(fake, limit).load(testAccount)
	if loadErr != nil {
		t.Fatalf("the live session must survive a refused write: %v", loadErr)
	}
	if !bytes.Equal(got, stored) {
		t.Errorf("refused write damaged the live session: %d of %d bytes are new-session bytes",
			bytes.Count(got, []byte("N")), len(got))
	}
}

// TestChunkedKeyringInlineWriteSurvivesUnreadablePrimary is the other half:
// an inline payload needs no generation, so a transient read failure must not
// block a login that does not touch part entries at all.
func TestChunkedKeyringInlineWriteSurvivesUnreadablePrimary(t *testing.T) {
	fake := newFakeKeyring(1000)
	blocked := getErrKeyring{fakeKeyring: fake, primary: testAccount, err: fmt.Errorf("keyring is locked")}

	want := payload(100)
	if err := newChunkedKeyring(blocked, 1000).save(testAccount, want); err != nil {
		t.Fatalf("an inline write needs no generation and must not read the primary: %v", err)
	}

	got, err := newChunkedKeyring(fake, 1000).load(testAccount)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("load returned %q, want %q", got, want)
	}
}

// countingKeyring records how many backend calls a write costs. Each is a
// subprocess spawn on macOS, on the synchronous login and token-refresh paths.
type countingKeyring struct {
	*fakeKeyring
	gets, sets, deletes int
}

func (c *countingKeyring) Set(service, account, secret string) error {
	c.sets++
	return c.fakeKeyring.Set(service, account, secret)
}

func (c *countingKeyring) Get(service, account string) (string, error) {
	c.gets++
	return c.fakeKeyring.Get(service, account)
}

func (c *countingKeyring) Delete(service, account string) error {
	c.deletes++
	return c.fakeKeyring.Delete(service, account)
}

// TestChunkedKeyringInlineWriteDoesNotReadTheKeyring pins the common path.
// A current session fits in one entry, so saving it must not pay for the
// generation lookup that only chunked writes need.
func TestChunkedKeyringInlineWriteDoesNotReadTheKeyring(t *testing.T) {
	counting := &countingKeyring{fakeKeyring: newFakeKeyring(4000)}
	ck := newChunkedKeyring(counting, 4000)

	// Roughly the size of a real post-fix token bundle.
	if err := ck.save(testAccount, payload(1600)); err != nil {
		t.Fatalf("save: %v", err)
	}
	if counting.gets != 0 {
		t.Errorf("inline save made %d keyring reads, want 0", counting.gets)
	}
	if counting.sets != 1 {
		t.Errorf("inline save made %d keyring writes, want 1", counting.sets)
	}
	// One probe per generation for the stale-part sweep, which is deliberate;
	// see the comment on the inline branch of write. Pinned so the common path
	// cannot quietly get more expensive.
	if counting.deletes != generations {
		t.Errorf("inline save made %d keyring deletes, want %d", counting.deletes, generations)
	}

	// A second save of the same shape must cost the same.
	before := *counting
	if err := ck.save(testAccount, payload(1600)); err != nil {
		t.Fatalf("second save: %v", err)
	}
	if counting.gets != before.gets {
		t.Errorf("steady-state save made %d extra keyring reads, want 0", counting.gets-before.gets)
	}
}

// TestLoadDoesNotAllocateOnAnImplausibleManifest guards the pre-allocation in
// load, which sizes a buffer from a manifest field before the length and
// checksum checks can reject it. A rejected manifest is read as an opaque
// payload instead, which allocates nothing beyond the entry itself.
func TestLoadDoesNotAllocateOnAnImplausibleManifest(t *testing.T) {
	ck, fake := newTestKeyring(4000)

	huge := fmt.Sprintf(`{"ndcli_chunked":1,"gen":0,"parts":2,"bytes":%d,"sum":"0"}`, 1<<40)
	if err := fake.Set(testService, testAccount, huge); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if _, ok := parseManifest(huge); ok {
		t.Fatal("a manifest claiming a terabyte must be rejected")
	}

	got, err := ck.load(testAccount)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if string(got) != huge {
		t.Errorf("a rejected manifest should be returned as an opaque payload, got %q", got)
	}

	// The bound must still admit the largest legitimate payload.
	if maxPayloadBytes < maxParts*windowsMaxSecretBytes {
		t.Errorf("maxPayloadBytes %d is below what %d full parts could hold", maxPayloadBytes, maxParts)
	}
}

// TestChunkedKeyringGiveUpErrorCarriesDiagnostics pins the numbers in the
// give-up message. There is no debug flag or log on the login path, so this
// string is the whole diagnostic: it has to separate "our budget is wrong"
// from "this session is genuinely too big", without leaking the account name.
func TestChunkedKeyringGiveUpErrorCarriesDiagnostics(t *testing.T) {
	const budget = 4096
	always := &alwaysTooBigKeyring{}
	ck := newChunkedKeyring(always, budget)

	data := payload(3000)
	err := ck.save(testAccount, data)
	if err == nil {
		t.Fatal("expected save to give up")
	}
	msg := err.Error()

	// Parts attempted on the final try, when the limit has reached the floor.
	finalParts := (len(data) + minPartBytes - 1) / minPartBytes

	for _, want := range []string{
		fmt.Sprintf("%d bytes", len(data)),
		fmt.Sprintf("budget %d bytes", budget),
		fmt.Sprintf("%d-character account name", len(testAccount)),
		fmt.Sprintf("%d parts attempted", finalParts),
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("give-up error is missing %q; got: %s", want, msg)
		}
	}

	// The backend error must still be reachable underneath the context.
	if !errors.Is(err, keyring.ErrSetDataTooBig) {
		t.Errorf("give-up error must wrap the backend error, got: %v", err)
	}

	// The account name is an email address and a host. It must never appear
	// verbatim in a string a user pastes into a public issue.
	if strings.Contains(msg, testAccount) {
		t.Errorf("account name leaked into the give-up error: %s", msg)
	}
}

// TestChunkedKeyringTooLongAccountErrorCarriesLengths covers the other message
// a user could hit: an account name that consumes the whole command-line
// budget, leaving nothing for the secret.
func TestChunkedKeyringTooLongAccountErrorCarriesLengths(t *testing.T) {
	// A budget of zero is what darwinMaxSecretBytes returns once the account
	// name has eaten the entire command line.
	ck := newChunkedKeyring(newFakeKeyring(1000), 0)

	longAccount := strings.Repeat("a", 300) + "@control.netdefense.io"
	err := ck.save(longAccount, payload(10))
	if err == nil {
		t.Fatal("expected save to reject an account name with no budget left")
	}
	msg := err.Error()

	if !strings.Contains(msg, "too long") {
		t.Errorf("error should say the account name is too long; got: %s", msg)
	}
	for _, want := range []string{
		fmt.Sprintf("%d characters", len(longAccount)),
		"0-byte budget",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("too-long error is missing %q; got: %s", want, msg)
		}
	}
}

// TestDarwinMaxSecretBytesReportsNoBudgetForAnUnusableAccount ties the message
// above to the real formula: past a certain account length there is genuinely
// no room, which is the condition that produces a zero budget.
func TestDarwinMaxSecretBytesReportsNoBudgetForAnUnusableAccount(t *testing.T) {
	unusable := strings.Repeat("a", darwinCommandLimit)
	if got := darwinMaxSecretBytes(KeyringService, unusable); got != 0 {
		t.Errorf("darwinMaxSecretBytes for a %d-character account = %d, want 0", len(unusable), got)
	}
}
