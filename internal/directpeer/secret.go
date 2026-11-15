// Package directpeer makes the secret of one session on the direct-peer +
// encrypted relay transport: a random seed, the keys both ends derive from
// it, and the seed sealed to the device's identity key. Only the sealed form
// and the public keys leave this program, so nothing between it and the
// device (the server, its database, the relay) can read or join the session.
//
// Every label, encoding and order here is shared with the device, which
// derives the same keys from the seed it opens and refuses a session whose
// values differ. testdata/connect-direct-peer-v1.json is the vector all
// programs of a session are tested against; it is copied here unchanged.
package directpeer

import (
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/hpke"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"

	"filippo.io/edwards25519"
)

const (
	// sealInfo separates this use of the device's key from every other.
	sealInfo = "netdefense-connect-v1"

	labelDeviceKey    = "netdefense-connect-v1 server-node-key"
	labelClientKey    = "netdefense-connect-v1 client-node-key"
	labelPresharedKey = "netdefense-connect-v1 preshared-key"

	// sealedVersion is the version of the sealed message's layout.
	sealedVersion = 1
	// maxSealedBytes is the most a sealed message may take, before base64.
	maxSealedBytes = 1024
)

// Secret is one session's secret. It is made for one request and never sent
// again: a second request, after any failure, gets a new one.
type Secret struct {
	seed [32]byte
	// ConnectID names the session in every message about it.
	ConnectID string
	// DeviceKey and ClientKey are the two tunnel keys, clamped; the device
	// derives DeviceKey for itself and runs it. PresharedKey is used as is.
	DeviceKey    [32]byte
	ClientKey    [32]byte
	PresharedKey [32]byte
	// DevicePublic and ClientPublic are their public keys, which the relay
	// admits.
	DevicePublic [32]byte
	ClientPublic [32]byte
}

// New makes a secret from a fresh random seed.
func New() (*Secret, error) {
	var seed [32]byte
	var id [16]byte
	if _, err := rand.Read(seed[:]); err != nil {
		return nil, err
	}
	if _, err := rand.Read(id[:]); err != nil {
		return nil, err
	}
	return fromSeed(seed, hex.EncodeToString(id[:]))
}

func fromSeed(seed [32]byte, connectID string) (*Secret, error) {
	s := &Secret{seed: seed, ConnectID: connectID}
	for _, k := range []struct {
		label string
		out   *[32]byte
		clamp bool
	}{
		{labelDeviceKey, &s.DeviceKey, true},
		{labelClientKey, &s.ClientKey, true},
		{labelPresharedKey, &s.PresharedKey, false},
	} {
		derived, err := hkdf.Key(sha256.New, seed[:], nil, k.label, len(k.out))
		if err != nil {
			return nil, err
		}
		copy(k.out[:], derived)
		clear(derived)
		if k.clamp {
			// The form of a Curve25519 private key. Both ends clamp right
			// after deriving, and use these exact bytes from then on.
			k.out[0] &= 248
			k.out[31] = k.out[31]&127 | 64
		}
	}
	for _, k := range []struct {
		private, public *[32]byte
	}{{&s.DeviceKey, &s.DevicePublic}, {&s.ClientKey, &s.ClientPublic}} {
		private, err := ecdh.X25519().NewPrivateKey(k.private[:])
		if err != nil {
			return nil, err
		}
		copy(k.public[:], private.PublicKey().Bytes())
	}
	return s, nil
}

// AdmitKeys are the two public keys as the server registers them with the
// relay: lowercase hex.
func (s *Secret) AdmitKeys() (device, client string) {
	return hex.EncodeToString(s.DevicePublic[:]), hex.EncodeToString(s.ClientPublic[:])
}

// Wipe clears the secret once the session's companion has the keys.
func (s *Secret) Wipe() {
	clear(s.seed[:])
	clear(s.DeviceKey[:])
	clear(s.ClientKey[:])
	clear(s.PresharedKey[:])
}

// Binding is what the device checks the sealed seed against: its own
// identity, the relay and the instant the request expires, all as the
// server gave them. The values are sealed as given; a difference of one
// byte on the device's side refuses the session.
type Binding struct {
	DeviceUUID    string
	RelayHostname string
	RelayPort     int
	RelaySTUNPort int
	ExpiresAt     int64
}

// Seal seals the seed and its binding to the device, whose identity key
// (Ed25519, base64) the server names. It returns the sealed message, base64.
func (s *Secret) Seal(deviceIdentityKey string, b Binding) (string, error) {
	recipient, err := recipientKey(deviceIdentityKey)
	if err != nil {
		return "", err
	}
	plaintext, err := s.plaintext(b)
	if err != nil {
		return "", err
	}
	defer clear(plaintext)
	sealed, err := hpke.Seal(recipient, hpke.HKDFSHA256(), hpke.ChaCha20Poly1305(), []byte(sealInfo), plaintext)
	if err != nil {
		return "", fmt.Errorf("the session secret could not be sealed to the device's key: %w", err)
	}
	if len(sealed) > maxSealedBytes {
		return "", fmt.Errorf("the sealed session secret is %d bytes, more than the %d the device takes", len(sealed), maxSealedBytes)
	}
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// recipientKey is the key a message to the device is sealed to: its Ed25519
// identity key in its X25519 form. The device opens the message with the
// same conversion of its private key.
func recipientKey(deviceIdentityKey string) (hpke.PublicKey, error) {
	raw, err := base64.StdEncoding.DecodeString(deviceIdentityKey)
	if err != nil || len(raw) != 32 {
		return nil, errors.New("the device's identity key is not 32 bytes of base64")
	}
	point, err := new(edwards25519.Point).SetBytes(raw)
	if err != nil {
		return nil, errors.New("the device's identity key is not a valid key")
	}
	recipient, err := hpke.DHKEM(ecdh.X25519()).NewPublicKey(point.BytesMontgomery())
	if err != nil {
		return nil, errors.New("the device's identity key is not a valid key")
	}
	return recipient, nil
}

// plaintext is the message that is sealed: one CBOR map of seven entries.
func (s *Secret) plaintext(b Binding) ([]byte, error) {
	switch {
	case b.DeviceUUID == "":
		return nil, errors.New("no device to seal the session secret to")
	case b.RelayHostname == "":
		return nil, errors.New("no relay to bind the session secret to")
	case b.RelayPort < 1 || b.RelayPort > 65535 || b.RelaySTUNPort < 1 || b.RelaySTUNPort > 65535:
		return nil, errors.New("the relay's ports are not 1 to 65535")
	case b.ExpiresAt <= 0:
		return nil, errors.New("no expiry to bind the session secret to")
	}
	out := cborHead(nil, cborMap, 7)
	out = cborUint(cborText(out, "v"), sealedVersion)
	out = cborBytes(cborText(out, "seed"), s.seed[:])
	out = cborText(cborText(out, "connect_id"), s.ConnectID)
	out = cborText(cborText(out, "device_uuid"), b.DeviceUUID)
	out = cborBytes(cborText(out, "client_key"), s.ClientPublic[:])
	out = cborHead(cborText(out, "relay"), cborMap, 3)
	out = cborText(cborText(out, "hostname"), b.RelayHostname)
	out = cborUint(cborText(out, "derp_port"), uint64(b.RelayPort))
	out = cborUint(cborText(out, "stun_port"), uint64(b.RelaySTUNPort))
	out = cborUint(cborText(out, "exp"), uint64(b.ExpiresAt))
	return out, nil
}

// The CBOR major types this package writes.
const (
	cborUnsigned byte = 0
	cborByteStr  byte = 2
	cborTextStr  byte = 3
	cborMap      byte = 5
)

// cborHead appends the head of an item: its major type and its value or
// length, in the shortest form.
func cborHead(b []byte, major byte, n uint64) []byte {
	major <<= 5
	switch {
	case n < 24:
		return append(b, major|byte(n))
	case n <= 0xff:
		return append(b, major|24, byte(n))
	case n <= 0xffff:
		return append(b, major|25, byte(n>>8), byte(n))
	case n <= 0xffffffff:
		return append(b, major|26, byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
	}
	return append(b, major|27, byte(n>>56), byte(n>>48), byte(n>>40), byte(n>>32), byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
}

func cborUint(b []byte, n uint64) []byte { return cborHead(b, cborUnsigned, n) }

func cborBytes(b, p []byte) []byte { return append(cborHead(b, cborByteStr, uint64(len(p))), p...) }

func cborText(b []byte, s string) []byte {
	return append(cborHead(b, cborTextStr, uint64(len(s))), s...)
}
