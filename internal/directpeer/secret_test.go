package directpeer

import (
	"bytes"
	"crypto/ecdh"
	"crypto/hpke"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
)

// vector is the part of the shared test vector this package reads.
type vector struct {
	DeviceKey struct {
		Ed25519PublicB64 string `json:"ed25519_public_b64"`
		X25519PrivateHex string `json:"x25519_private_hex"`
		X25519PublicHex  string `json:"x25519_public_hex"`
	} `json:"device_key"`
	SessionKeys struct {
		SeedHex                    string `json:"seed_hex"`
		ServerNodeKeyHKDFOutputHex string `json:"server_node_key_hkdf_output_hex"`
		ServerNodePrivateHex       string `json:"server_node_private_hex"`
		ServerNodePublicHex        string `json:"server_node_public_hex"`
		ClientNodeKeyHKDFOutputHex string `json:"client_node_key_hkdf_output_hex"`
		ClientNodePrivateHex       string `json:"client_node_private_hex"`
		ClientNodePublicHex        string `json:"client_node_public_hex"`
		PresharedKeyHex            string `json:"preshared_key_hex"`
	} `json:"session_keys"`
	Sealed struct {
		Plaintext struct {
			ConnectID  string `json:"connect_id"`
			DeviceUUID string `json:"device_uuid"`
			Relay      struct {
				Hostname string `json:"hostname"`
				DERPPort int    `json:"derp_port"`
				STUNPort int    `json:"stun_port"`
			} `json:"relay"`
			Exp int64 `json:"exp"`
		} `json:"plaintext"`
		PlaintextCBORHex string `json:"plaintext_cbor_hex"`
		SealedB64        string `json:"sealed_b64"`
		MustNotOpen      struct {
			AnotherInfoB64 string `json:"sealed_with_another_info_b64"`
			TamperedB64    string `json:"tampered_b64"`
		} `json:"must_not_open"`
	} `json:"sealed"`
	Payloads struct {
		Open struct {
			AdmitKeys struct {
				Client string `json:"client"`
				Server string `json:"server"`
			} `json:"admit_keys"`
		} `json:"open"`
	} `json:"payloads"`
}

func loadVector(t *testing.T) vector {
	t.Helper()
	raw, err := os.ReadFile("testdata/connect-direct-peer-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var v vector
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func unhex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// vectorSecret is the secret of the vector's seed.
func vectorSecret(t *testing.T, v vector) *Secret {
	t.Helper()
	s, err := fromSeed([32]byte(unhex(t, v.SessionKeys.SeedHex)), v.Sealed.Plaintext.ConnectID)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func vectorBinding(v vector) Binding {
	p := v.Sealed.Plaintext
	return Binding{DeviceUUID: p.DeviceUUID, RelayHostname: p.Relay.Hostname, RelayPort: p.Relay.DERPPort, RelaySTUNPort: p.Relay.STUNPort, ExpiresAt: p.Exp}
}

// openAsTheDevice opens a sealed message with the vector's device key, the
// way the device does.
func openAsTheDevice(t *testing.T, v vector, sealedB64 string) ([]byte, error) {
	t.Helper()
	sealed, err := base64.StdEncoding.DecodeString(sealedB64)
	if err != nil {
		t.Fatal(err)
	}
	device, err := hpke.DHKEM(ecdh.X25519()).NewPrivateKey(unhex(t, v.DeviceKey.X25519PrivateHex))
	if err != nil {
		t.Fatal(err)
	}
	return hpke.Open(device, hpke.HKDFSHA256(), hpke.ChaCha20Poly1305(), []byte("netdefense-connect-v1"), sealed)
}

func TestTheVectorsKeysComeFromItsSeed(t *testing.T) {
	v := loadVector(t)
	s := vectorSecret(t, v)
	for _, k := range []struct {
		name string
		got  [32]byte
		want string
	}{
		{"the device's tunnel key", s.DeviceKey, v.SessionKeys.ServerNodePrivateHex},
		{"the client's tunnel key", s.ClientKey, v.SessionKeys.ClientNodePrivateHex},
		{"the pre-shared key", s.PresharedKey, v.SessionKeys.PresharedKeyHex},
		{"the device's public key", s.DevicePublic, v.SessionKeys.ServerNodePublicHex},
		{"the client's public key", s.ClientPublic, v.SessionKeys.ClientNodePublicHex},
	} {
		if got := hex.EncodeToString(k.got[:]); got != k.want {
			t.Errorf("%s = %s, want %s", k.name, got, k.want)
		}
	}
	device, client := s.AdmitKeys()
	if device != v.Payloads.Open.AdmitKeys.Server || client != v.Payloads.Open.AdmitKeys.Client {
		t.Errorf("admit keys = %s, %s; want %s, %s", device, client, v.Payloads.Open.AdmitKeys.Server, v.Payloads.Open.AdmitKeys.Client)
	}

	// The vector lists each tunnel key before clamping too. They differ from
	// the clamped ones, so a key used as derived would not be the device's.
	if hex.EncodeToString(s.DeviceKey[:]) == v.SessionKeys.ServerNodeKeyHKDFOutputHex || hex.EncodeToString(s.ClientKey[:]) == v.SessionKeys.ClientNodeKeyHKDFOutputHex {
		t.Error("a tunnel key was left as derived, not clamped")
	}
}

func TestTheDevicesIdentityKeyBecomesTheKeyItIsSealedTo(t *testing.T) {
	v := loadVector(t)
	recipient, err := recipientKey(v.DeviceKey.Ed25519PublicB64)
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(recipient.Bytes()); got != v.DeviceKey.X25519PublicHex {
		t.Errorf("recipient key = %s, want %s", got, v.DeviceKey.X25519PublicHex)
	}
}

// The device refuses a message whose values differ from what it expects, so
// what is sealed has to be this exact message.
func TestTheSealedMessageIsTheVectors(t *testing.T) {
	v := loadVector(t)
	plaintext, err := vectorSecret(t, v).plaintext(vectorBinding(v))
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(plaintext); got != v.Sealed.PlaintextCBORHex {
		t.Errorf("plaintext:\n got %s\nwant %s", got, v.Sealed.PlaintextCBORHex)
	}
}

func TestASealedSecretOpensWithTheDevicesKeyAndNoOther(t *testing.T) {
	v := loadVector(t)
	s := vectorSecret(t, v)
	want := unhex(t, v.Sealed.PlaintextCBORHex)

	// The opener used here is right: it opens the vector's own message, and
	// refuses the two the vector says must not open.
	if got, err := openAsTheDevice(t, v, v.Sealed.SealedB64); err != nil || !bytes.Equal(got, want) {
		t.Fatalf("the vector's sealed message did not open to its plaintext: %v", err)
	}
	for name, blob := range map[string]string{"sealed for another use": v.Sealed.MustNotOpen.AnotherInfoB64, "tampered with": v.Sealed.MustNotOpen.TamperedB64} {
		if _, err := openAsTheDevice(t, v, blob); err == nil {
			t.Fatalf("a message %s opened", name)
		}
	}

	first, err := s.Seal(v.DeviceKey.Ed25519PublicB64, vectorBinding(v))
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Seal(v.DeviceKey.Ed25519PublicB64, vectorBinding(v))
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Error("two seals of one secret are the same bytes")
	}
	for _, sealed := range []string{first, second} {
		got, err := openAsTheDevice(t, v, sealed)
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("a sealed secret did not open to the vector's plaintext: %v", err)
		}
		raw, _ := base64.StdEncoding.DecodeString(sealed)
		if len(raw) > maxSealedBytes {
			t.Errorf("the sealed secret is %d bytes, more than %d", len(raw), maxSealedBytes)
		}
		if len(raw) != len(want)+32+16 {
			t.Errorf("the sealed secret is %d bytes; want the encapsulated key, the plaintext and the tag: %d", len(raw), len(want)+32+16)
		}
	}

	// Another device cannot open it.
	other, err := hpke.DHKEM(ecdh.X25519()).GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := base64.StdEncoding.DecodeString(first)
	if _, err := hpke.Open(other, hpke.HKDFSHA256(), hpke.ChaCha20Poly1305(), []byte(sealInfo), raw); err == nil {
		t.Error("a sealed secret opened with another key")
	}
}

func TestAnIdentityKeyThatIsNotAKeyIsRefused(t *testing.T) {
	v := loadVector(t)
	s := vectorSecret(t, v)
	// 32 bytes that are no point: no x fits the curve for y = 2.
	notAPoint := make([]byte, 32)
	notAPoint[0] = 2
	for name, key := range map[string]string{
		"not base64":       "not base64!",
		"empty":            "",
		"31 bytes":         base64.StdEncoding.EncodeToString(make([]byte, 31)),
		"33 bytes":         base64.StdEncoding.EncodeToString(make([]byte, 33)),
		"not on the curve": base64.StdEncoding.EncodeToString(notAPoint),
	} {
		sealed, err := s.Seal(key, vectorBinding(v))
		if err == nil || sealed != "" {
			t.Errorf("%s: sealed to it (%q, %v)", name, sealed, err)
			continue
		}
		if key != "" && strings.Contains(err.Error(), key) {
			t.Errorf("%s: the error repeats the key: %v", name, err)
		}
	}
}

func TestABindingThatCannotBeSealed(t *testing.T) {
	v := loadVector(t)
	s := vectorSecret(t, v)
	for name, spoil := range map[string]func(*Binding){
		"no device":                 func(b *Binding) { b.DeviceUUID = "" },
		"no relay":                  func(b *Binding) { b.RelayHostname = "" },
		"relay port zero":           func(b *Binding) { b.RelayPort = 0 },
		"relay port too high":       func(b *Binding) { b.RelayPort = 65536 },
		"no STUN port":              func(b *Binding) { b.RelaySTUNPort = 0 },
		"no expiry":                 func(b *Binding) { b.ExpiresAt = 0 },
		"a host longer than a seal": func(b *Binding) { b.RelayHostname = strings.Repeat("a", 900) },
	} {
		b := vectorBinding(v)
		spoil(&b)
		if sealed, err := s.Seal(v.DeviceKey.Ed25519PublicB64, b); err == nil {
			t.Errorf("%s: sealed (%d characters)", name, len(sealed))
		}
	}
}

func TestEverySecretIsNew(t *testing.T) {
	a, err := New()
	if err != nil {
		t.Fatal(err)
	}
	b, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if a.seed == b.seed || a.ConnectID == b.ConnectID || a.DeviceKey == b.DeviceKey || a.ClientKey == b.ClientKey || a.PresharedKey == b.PresharedKey {
		t.Error("two secrets share a value")
	}
	id := regexp.MustCompile(`^[0-9a-f]{32}$`)
	for _, s := range []*Secret{a, b} {
		if !id.MatchString(s.ConnectID) {
			t.Errorf("connect id %q is not 32 lowercase hex characters", s.ConnectID)
		}
		for name, k := range map[string][32]byte{"device": s.DeviceKey, "client": s.ClientKey} {
			if k[0]&7 != 0 || k[31]&0x80 != 0 || k[31]&0x40 == 0 {
				t.Errorf("the %s tunnel key is not clamped", name)
			}
		}
		if s.DeviceKey == s.ClientKey || s.DevicePublic == s.ClientPublic {
			t.Error("both ends were given one key")
		}
	}

	a.Wipe()
	var zero [32]byte
	if a.seed != zero || a.DeviceKey != zero || a.ClientKey != zero || a.PresharedKey != zero {
		t.Error("a wiped secret still holds key material")
	}
}

// The heads of CBOR items, against the examples of the specification.
func TestCBORHeads(t *testing.T) {
	for _, tc := range []struct {
		n    uint64
		want string
	}{
		{0, "00"}, {1, "01"}, {23, "17"}, {24, "1818"}, {100, "1864"}, {255, "18ff"}, {256, "190100"},
		{1000, "1903e8"}, {65535, "19ffff"}, {65536, "1a00010000"}, {1000000, "1a000f4240"},
		{4294967295, "1affffffff"}, {4294967296, "1b0000000100000000"}, {1000000000000, "1b000000e8d4a51000"},
	} {
		if got := hex.EncodeToString(cborUint(nil, tc.n)); got != tc.want {
			t.Errorf("unsigned %d = %s, want %s", tc.n, got, tc.want)
		}
	}
	if got := hex.EncodeToString(cborText(nil, "IETF")); got != "6449455446" {
		t.Errorf(`text "IETF" = %s, want 6449455446`, got)
	}
	if got := hex.EncodeToString(cborBytes(nil, []byte{1, 2, 3, 4})); got != "4401020304" {
		t.Errorf("bytes 01020304 = %s, want 4401020304", got)
	}
	if got := hex.EncodeToString(cborHead(nil, cborMap, 2)); got != "a2" {
		t.Errorf("a map of two = %s, want a2", got)
	}
}
