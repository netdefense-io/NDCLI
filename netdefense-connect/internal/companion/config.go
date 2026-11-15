// Package companion is the netdefense-connect program. It carries one remote
// session's bytes between its parent (ndcli, netdefense-mcp or the netdefense
// TUI) and the device, over an encrypted tunnel that runs peer to peer when
// the two networks allow it and through a relay otherwise.
//
// The parent starts one process per session and talks to it over three pipes:
//
//	stdin   one configuration message, then the session's bytes to the device
//	stdout  the session's bytes from the device
//	stderr  one JSON event per line
//
// Keys arrive only in the configuration message, never on the command line or
// in the environment. Parent and companion ship together and carry the same
// version, so neither side reads a message written by another release.
package companion

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net/netip"
	"strings"
)

// Protocol is the tunnel generation this build speaks. It changes whenever a
// change to the tunnel library, or to how this program uses it, would stop it
// from reaching a device on the previous generation.
const Protocol = 1

// maxConfigBytes bounds the configuration message.
const maxConfigBytes = 16 * 1024

// The limits of Config.ConnectTimeoutSeconds.
const (
	minConnectTimeoutSeconds = 1
	maxConnectTimeoutSeconds = 600
)

// Key is 32 bytes of key material, base64 in JSON.
type Key [32]byte

func (k Key) MarshalJSON() ([]byte, error) {
	return json.Marshal(base64.StdEncoding.EncodeToString(k[:]))
}

// UnmarshalJSON never puts the input in its error: it may be a key.
func (k *Key) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return errors.New("a key is not a string")
	}
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil || len(raw) != len(k) {
		return errors.New("a key is not 32 bytes of base64")
	}
	copy(k[:], raw)
	clear(raw)
	return nil
}

func (k Key) isZero() bool { return k == Key{} }

// clamped reports whether k has the form of a Curve25519 private key. Both
// ends hand their tunnel keys to the library in that form, byte for byte: the
// library derives a second key from those bytes, so a key clamped on one end
// only would give a session that never leaves the relay.
func (k Key) clamped() bool {
	return k[0]&7 == 0 && k[31]&0x80 == 0 && k[31]&0x40 != 0
}

// Relay is the relay both ends meet on.
type Relay struct {
	Hostname string `json:"hostname"`
	Port     int    `json:"port"`
	STUNPort int    `json:"stun_port"`
}

// Config is the message the parent writes before the session's bytes: a
// 4-byte big-endian length, then this object as JSON.
type Config struct {
	Relay Relay `json:"relay"`
	// Port is the TCP port the device serves the session on.
	Port int `json:"port"`
	// ClientKey is this end's tunnel key; ServerKey is the device's, from
	// which its public keys are derived.
	ClientKey    Key `json:"client_key"`
	ServerKey    Key `json:"server_key"`
	PresharedKey Key `json:"preshared_key"`
	// ConnectTimeoutSeconds is how long to keep trying to reach the device.
	ConnectTimeoutSeconds int `json:"connect_timeout_seconds"`
	// Debug turns the tunnel's own log lines into log events.
	Debug bool `json:"debug,omitempty"`
}

// ConfigError is a configuration message this program refuses. Detail names
// what is wrong and never holds a value from the message.
type ConfigError struct{ Detail string }

func (e *ConfigError) Error() string { return "configuration refused: " + e.Detail }

// ReadConfig reads the configuration message. It returns io.EOF when r ends
// before the message starts.
func ReadConfig(r io.Reader) (*Config, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, io.EOF
		}
		return nil, &ConfigError{Detail: "the message ended inside its length"}
	}
	n := binary.BigEndian.Uint32(header[:])
	if n == 0 || n > maxConfigBytes {
		return nil, &ConfigError{Detail: "the message length is out of range"}
	}
	buf := make([]byte, n)
	defer clear(buf)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, &ConfigError{Detail: "the message is shorter than its length"}
	}

	dec := json.NewDecoder(bytes.NewReader(buf))
	dec.DisallowUnknownFields()
	var cfg Config
	if err := dec.Decode(&cfg); err != nil {
		cfg.wipe()
		return nil, &ConfigError{Detail: "the message is not this version's configuration"}
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		cfg.wipe()
		return nil, &ConfigError{Detail: "the message holds more than one object"}
	}
	if detail := cfg.problem(); detail != "" {
		cfg.wipe()
		return nil, &ConfigError{Detail: detail}
	}
	return &cfg, nil
}

// problem names the first thing wrong with c, or is empty.
func (c *Config) problem() string {
	switch {
	case !validHostname(c.Relay.Hostname):
		return "relay.hostname is not a host name or an address"
	case !validPort(c.Relay.Port):
		return "relay.port is not 1 to 65535"
	case !validPort(c.Relay.STUNPort):
		return "relay.stun_port is not 1 to 65535"
	case !validPort(c.Port):
		return "port is not 1 to 65535"
	case c.ClientKey.isZero():
		return "client_key is missing"
	case c.ServerKey.isZero():
		return "server_key is missing"
	case c.PresharedKey.isZero():
		return "preshared_key is missing"
	case !c.ClientKey.clamped():
		return "client_key is not a clamped key"
	case !c.ServerKey.clamped():
		return "server_key is not a clamped key"
	case c.ClientKey == c.ServerKey:
		return "client_key and server_key are the same key"
	case c.ConnectTimeoutSeconds < minConnectTimeoutSeconds || c.ConnectTimeoutSeconds > maxConnectTimeoutSeconds:
		return "connect_timeout_seconds is not 1 to 600"
	}
	return ""
}

// wipe clears the keys once the tunnel holds its own copies.
func (c *Config) wipe() {
	clear(c.ClientKey[:])
	clear(c.ServerKey[:])
	clear(c.PresharedKey[:])
}

func validPort(p int) bool { return p >= 1 && p <= 65535 }

// validHostname accepts an IP address without a zone, or a DNS name of
// letters, digits and hyphens: the forms a relay's certificate is issued for.
func validHostname(h string) bool {
	if h == "" || len(h) > 253 {
		return false
	}
	if addr, err := netip.ParseAddr(h); err == nil {
		return addr.Zone() == ""
	}
	for label := range strings.SplitSeq(h, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range []byte(label) {
			if !hostnameByte(c) {
				return false
			}
		}
	}
	return true
}

func hostnameByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-'
}
