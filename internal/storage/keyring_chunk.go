package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"runtime"
	"strconv"
	"strings"

	"github.com/zalando/go-keyring"
)

// Every OS keyring caps the size of a single secret, and the caps are small
// enough that one OAuth2 token bundle can exceed them:
//
//   - macOS: go-keyring shells out to /usr/bin/security and refuses to run
//     once the assembled command line passes 4096 bytes, which works out to
//     roughly 2.9-3.0 KB of payload.
//   - Windows: the Credential Manager blob is capped at 2560 bytes.
//   - Linux/secret-service: no hard cap in practice.
//
// chunkedKeyring keeps the primary entry addressed by the credential key and
// spills anything that does not fit into numbered part entries, leaving a
// small manifest behind in the primary entry so reads can find them.
//
// A single keyring.Set is atomic, but a multi-part write is not, so parts are
// written under a generation the live manifest does not point at. The manifest
// Set is then the one instant at which the new session becomes visible. A
// failure at any earlier point -- a locked keyring, a revoked session, a
// crash -- leaves the previous generation intact and still loadable, rather
// than splicing new parts onto old ones.
type chunkedKeyring struct {
	ops     keyringOps
	service string
	// limit reports the largest secret the backend will accept for an account.
	// It is a field rather than a direct call so tests can force chunking
	// without depending on the host keyring.
	limit func(service, account string) int
}

// keyringOps is the subset of go-keyring that chunkedKeyring needs.
type keyringOps interface {
	Set(service, account, secret string) error
	Get(service, account string) (string, error)
	Delete(service, account string) error
}

// systemKeyring routes keyringOps to the real OS keyring.
type systemKeyring struct{}

func (systemKeyring) Set(service, account, secret string) error {
	return keyring.Set(service, account, secret)
}

func (systemKeyring) Get(service, account string) (string, error) {
	return keyring.Get(service, account)
}

func (systemKeyring) Delete(service, account string) error {
	return keyring.Delete(service, account)
}

const (
	// partSuffix introduces the generation and index of a part entry in the
	// keyring account name, e.g. "alice@example.com@host#part0-1".
	partSuffix = "#part"

	// generations is how many part sets can exist at once. Two is all the
	// swap needs: the live one and the one being written.
	generations = 2

	// maxParts bounds part enumeration so a corrupt manifest cannot make load
	// or clear spin, and caps how much a single account may store.
	maxParts = 32

	// minPartBytes floors the adaptive shrink in save.
	minPartBytes = 256

	// manifestVersion is the schema version of the primary-entry manifest.
	manifestVersion = 1

	// maxPayloadBytes bounds what a manifest may claim, so a corrupt one
	// cannot drive a large allocation before the length and checksum checks
	// get a chance to reject it. It is the most any account could legitimately
	// hold: every part filled to the largest per-part budget of any platform.
	maxPayloadBytes = maxParts * defaultMaxSecretBytes

	// manifestMarker is the JSON key that distinguishes a manifest from a
	// token document. StoredTokens never carries it, which is what lets load
	// read single-entry payloads written by releases that predate chunking.
	manifestMarker = "ndcli_chunked"
)

// partManifest is written to the primary entry when the payload is split. It
// is the only pointer to the part entries, so replacing it is what commits a
// multi-part write.
type partManifest struct {
	Version int `json:"ndcli_chunked"`
	// Gen selects which set of part entries this manifest describes.
	Gen   int    `json:"gen"`
	Parts int    `json:"parts"`
	Bytes int    `json:"bytes"`
	Sum   string `json:"sum"`
}

// save writes data for account, splitting it across part entries when it does
// not fit in one.
func (c chunkedKeyring) save(account string, data []byte) error {
	// The limit is our model of a rule enforced inside the backend. If the
	// model is wrong -- a different go-keyring version, argument quoting we
	// did not account for, a stricter Credential Manager policy -- shrink and
	// retry instead of failing the login outright. The limit strictly
	// decreases to minPartBytes and then stops, so this cannot spin.
	//
	// Retrying is not free on macOS. go-keyring v0.2.6 starts its
	// "/usr/bin/security -i" child before checking the command length, and the
	// oversized branch returns without closing the child's stdin or reaping it
	// (keyring_darwin.go:76-89), so every rejected Set leaves one hung process
	// behind until the calling process exits. That is invisible for a
	// short-lived ndcli invocation but accumulates in netdefense-mcp, which is
	// long-lived. It is why maxKeyringSecretBytes is derived from go-keyring's
	// own arithmetic and pinned to measured ceilings by tests: this loop is a
	// safety net that should never fire, not a sizing strategy.
	budget := c.limit(c.service, account)
	limit := budget
	for {
		parts, err := c.write(account, data, limit)
		if err == nil {
			return nil
		}
		if !isCredentialBlobTooBig(err) {
			return err
		}
		if limit <= minPartBytes {
			// Out of room to shrink. There is no debug flag or log on the
			// login path, so this string is the only diagnostic anyone gets:
			// it carries the numbers needed to tell a wrong budget from a
			// genuinely oversized session. The account name is reported as a
			// length and never verbatim -- it is an email address and a host,
			// and this text ends up pasted into bug reports.
			return fmt.Errorf("keyring rejected the session even after splitting it: %d bytes, per-entry budget %d bytes for a %d-character account name, %d parts attempted: %w",
				len(data), budget, len(account), parts, err)
		}
		if limit /= 2; limit < minPartBytes {
			limit = minPartBytes
		}
	}
}

// liveManifest returns the manifest currently published in the primary entry.
// The bool reports whether one is published; the error reports that we could
// not find out, which is a different thing and must not be collapsed into it.
func (c chunkedKeyring) liveManifest(account string) (partManifest, bool, error) {
	raw, err := c.ops.Get(c.service, account)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return partManifest{}, false, nil
		}
		return partManifest{}, false, err
	}
	m, ok := parseManifest(raw)
	return m, ok, nil
}

// nextGeneration picks a generation no reader is currently following.
//
// Treating an unreadable primary entry as "nothing is stored" would defeat the
// whole swap: if a chunked session is live in generation 0 and the Get fails
// transiently, defaulting to 0 would write parts over the very set a reader
// would load, which is the corruption the generations exist to prevent. A
// keyring we cannot read is a keyring we must not guess about.
func (c chunkedKeyring) nextGeneration(account string) (int, error) {
	live, published, err := c.liveManifest(account)
	if err != nil {
		return 0, fmt.Errorf("cannot determine which keyring entries are in use: %w", err)
	}
	if !published {
		// Absent, or a single-entry payload: no part set is referenced.
		return 0, nil
	}
	return (live.Gen + 1) % generations, nil
}

// write stores data, returning the number of keyring entries it used, or
// attempted to use when it fails. save reports that count on give-up.
func (c chunkedKeyring) write(account string, data []byte, limit int) (int, error) {
	if limit <= 0 {
		return 0, fmt.Errorf("keyring account name %q is too long to store a secret (%d characters leaves a %d-byte budget)", account, len(account), limit)
	}

	if len(data) > limit {
		return c.writeParts(account, data, limit)
	}

	// One Set, atomically replacing whatever was there. Nothing else has to
	// land for this session to be complete.
	if err := c.ops.Set(c.service, account, string(data)); err != nil {
		return 1, err
	}
	// The primary entry is no longer a manifest, so every part of every
	// generation is now unreachable. Sweeping them costs two backend calls
	// (one probe per generation, stopping at the first gap) even in the usual
	// case where there is nothing to remove. That stays: the parts hold
	// fragments of a real session, an interrupted write can leave them behind
	// while the primary entry still looks like a plain payload, and the only
	// other sweep is logout, which may never come. Two calls against an
	// operation already dominated by a network round trip is the right trade.
	c.pruneAllParts(account)
	return 1, nil
}

func (c chunkedKeyring) writeParts(account string, data []byte, limit int) (int, error) {
	// Resolved here rather than in save: which generation is free only matters
	// once a payload turns out not to fit in a single entry, and a current
	// session is about 1.6 KB against a ~2900-byte budget, so it nearly always
	// does fit. Reading it eagerly would add a keyring round trip -- a
	// subprocess spawn on macOS -- to every login and every access-token
	// refresh, including in the long-lived netdefense-mcp server.
	gen, err := c.nextGeneration(account)
	if err != nil {
		return 0, err
	}

	// Part account names are longer than the primary one, so on platforms
	// where the cap depends on the account name every part gets a smaller
	// budget. Size all parts against the longest name we might use.
	partLimit := c.limit(c.service, partAccount(account, gen, maxParts))
	if partLimit <= 0 {
		return 0, fmt.Errorf("keyring account name %q is too long to store a secret (%d characters leaves a %d-byte budget)", account, len(account), partLimit)
	}
	if partLimit > limit {
		partLimit = limit
	}

	parts := (len(data) + partLimit - 1) / partLimit
	if parts > maxParts {
		return parts, fmt.Errorf("credentials need %d keyring entries but only %d are supported", parts, maxParts)
	}

	// These writes are invisible: the live manifest still points at the other
	// generation, so a failure here leaves the stored session untouched.
	for i := 1; i <= parts; i++ {
		end := min(i*partLimit, len(data))
		if err := c.ops.Set(c.service, partAccount(account, gen, i), string(data[(i-1)*partLimit:end])); err != nil {
			return parts, err
		}
	}

	manifest, err := json.Marshal(partManifest{
		Version: manifestVersion,
		Gen:     gen,
		Parts:   parts,
		Bytes:   len(data),
		Sum:     checksum(data),
	})
	if err != nil {
		return parts, fmt.Errorf("failed to build keyring manifest: %w", err)
	}

	// The commit point. Until this Set lands the old session is what loads;
	// after it lands the new one is, with no state in between.
	if err := c.ops.Set(c.service, account, string(manifest)); err != nil {
		return parts, err
	}

	// The previous generation is unreachable now, as is any tail left by an
	// earlier write of this generation that needed more parts.
	c.pruneParts(account, (gen+1)%generations, 1)
	c.pruneParts(account, gen, parts+1)
	return parts, nil
}

// load returns the payload stored for account, or nil when there is none.
func (c chunkedKeyring) load(account string) ([]byte, error) {
	raw, err := c.ops.Get(c.service, account)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}

	manifest, ok := parseManifest(raw)
	if !ok {
		// Either written inline by this code, or by a release that predates
		// chunking. Both are the payload itself.
		return []byte(raw), nil
	}

	buf := make([]byte, 0, manifest.Bytes)
	for i := 1; i <= manifest.Parts; i++ {
		part, err := c.ops.Get(c.service, partAccount(account, manifest.Gen, i))
		if err != nil {
			if errors.Is(err, keyring.ErrNotFound) {
				return nil, fmt.Errorf("stored credentials are incomplete: keyring entry %d of %d is missing; run 'ndcli auth login' to sign in again", i, manifest.Parts)
			}
			return nil, err
		}
		buf = append(buf, part...)
	}

	// The generation swap keeps a failed write from ever publishing a spliced
	// payload. These checks catch anything that damages the parts afterwards.
	if len(buf) != manifest.Bytes {
		return nil, fmt.Errorf("stored credentials are corrupt: expected %d bytes across %d keyring entries but read %d; run 'ndcli auth login' to sign in again", manifest.Bytes, manifest.Parts, len(buf))
	}
	if got := checksum(buf); got != manifest.Sum {
		return nil, fmt.Errorf("stored credentials are corrupt: keyring entries checksum %s, expected %s; run 'ndcli auth login' to sign in again", got, manifest.Sum)
	}
	return buf, nil
}

// partCleanupError marks a clear() failure that only affected leftover part
// entries. The primary entry -- the one that actually holds the session -- was
// removed, so the logout itself succeeded and callers must not report it as a
// failure.
type partCleanupError struct{ err error }

func (e partCleanupError) Error() string { return e.err.Error() }
func (e partCleanupError) Unwrap() error { return e.err }

// clear removes the primary entry and every part entry for account.
//
// A returned partCleanupError means the session is gone and only housekeeping
// failed. Any other error means the session secret is still in the keyring.
func (c chunkedKeyring) clear(account string) error {
	// The primary entry is attempted even when part cleanup fails: a logout
	// has to revoke every credential it can actually reach.
	var partsErr error
	for gen := 0; gen < generations; gen++ {
		if err := c.deleteParts(account, gen, 1); err != nil && partsErr == nil {
			partsErr = err
		}
	}

	if err := c.ops.Delete(c.service, account); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return err
	}
	if partsErr != nil {
		return partCleanupError{err: partsErr}
	}
	return nil
}

// deleteParts removes part entries of one generation starting at index from.
// Parts are always written contiguously from 1, so the first missing index
// ends the set.
func (c chunkedKeyring) deleteParts(account string, gen, from int) error {
	for i := from; i <= maxParts; i++ {
		err := c.ops.Delete(c.service, partAccount(account, gen, i))
		if err == nil {
			continue
		}
		if errors.Is(err, keyring.ErrNotFound) {
			return nil
		}
		return err
	}
	return nil
}

// pruneParts is deleteParts as best-effort cleanup. Unreferenced parts are
// inert on read -- load only follows the generation and count named by the
// live manifest -- so failing to remove them must not fail the write that
// just succeeded.
func (c chunkedKeyring) pruneParts(account string, gen, from int) {
	_ = c.deleteParts(account, gen, from)
}

// pruneAllParts drops every part entry for account, in either generation.
func (c chunkedKeyring) pruneAllParts(account string) {
	for gen := 0; gen < generations; gen++ {
		c.pruneParts(account, gen, 1)
	}
}

func partAccount(account string, gen, index int) string {
	return account + partSuffix + strconv.Itoa(gen) + "-" + strconv.Itoa(index)
}

// checksum guards against a part set that was damaged after it was committed.
// It is an integrity check against accident, not against an attacker: anyone
// who can rewrite the parts can rewrite the manifest too. It is also what
// catches the one case the generation swap cannot prevent: two processes
// writing the same account concurrently -- an interactive ndcli refresh
// overlapping a netdefense-mcp one -- can both read the live manifest before
// either publishes, pick the same free generation, and interleave their parts.
// That fails loudly on the next read instead of silently, but it is a narrow
// regression against the single-Set behaviour this replaced, where OS keyring
// atomicity gave clean last-writer-wins. It needs both writes to be chunked,
// so it cannot arise for the inline-sized sessions ndcli stores today.
func checksum(data []byte) string {
	return strconv.FormatUint(uint64(crc32.ChecksumIEEE(data)), 16)
}

// parseManifest reports whether raw is a part manifest rather than a payload.
func parseManifest(raw string) (partManifest, bool) {
	var manifest partManifest
	if !strings.Contains(raw, manifestMarker) {
		return manifest, false
	}
	if err := json.Unmarshal([]byte(raw), &manifest); err != nil {
		return manifest, false
	}
	if manifest.Version != manifestVersion ||
		manifest.Gen < 0 || manifest.Gen >= generations ||
		manifest.Parts < 1 || manifest.Parts > maxParts ||
		manifest.Bytes < 0 || manifest.Bytes > maxPayloadBytes {
		return partManifest{}, false
	}
	return manifest, true
}

const (
	// darwinCommandLimit is the ceiling go-keyring enforces on the
	// "add-generic-password" line it pipes to /usr/bin/security.
	darwinCommandLimit = 4096

	// darwinCommandOverhead is the length of the literal parts of that line:
	// "add-generic-password -U -s " + " -a " + " -w " + "\n".
	darwinCommandOverhead = 36

	// darwinSecretPrefix is the "go-keyring-base64:" marker go-keyring puts in
	// front of the base64-encoded secret.
	darwinSecretPrefix = 18

	// darwinSafetyMargin leaves room for a future go-keyring that quotes its
	// arguments or lengthens the prefix.
	darwinSafetyMargin = 64

	// windowsMaxSecretBytes sits just under the 2560-byte
	// CRED_MAX_CREDENTIAL_BLOB_SIZE the Credential Manager enforces.
	windowsMaxSecretBytes = 2496

	// defaultMaxSecretBytes applies to secret-service and the in-memory
	// fallback provider, neither of which documents a ceiling. It is high
	// enough that chunking never engages for real token bundles.
	defaultMaxSecretBytes = 32768
)

// maxKeyringSecretBytes reports the largest plaintext secret keyring.Set will
// accept for (service, account) on this platform.
func maxKeyringSecretBytes(service, account string) int {
	switch runtime.GOOS {
	case "darwin":
		return darwinMaxSecretBytes(service, account)
	case "windows":
		return windowsMaxSecretBytes
	default:
		return defaultMaxSecretBytes
	}
}

// darwinMaxSecretBytes derives the payload budget from go-keyring's macOS
// backend, which builds
//
//	add-generic-password -U -s <service> -a <account> -w <secret>\n
//
// and returns ErrSetDataTooBig once that string exceeds darwinCommandLimit.
// <secret> is "go-keyring-base64:" followed by base64 of the payload, and none
// of the three substituted values are shell-quoted in practice: base64's
// alphabet, the ndcli service name and "user@host" account names all fall
// inside the safe set shellescape leaves alone.
func darwinMaxSecretBytes(service, account string) int {
	encoded := darwinCommandLimit - darwinCommandOverhead - darwinSafetyMargin -
		darwinSecretPrefix - len(service) - len(account)
	if encoded < 0 {
		return 0
	}
	// base64 turns every 3 payload bytes into 4 encoded bytes.
	return encoded / 4 * 3
}
