package companion

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"regexp"
	"testing"

	"github.com/tailscale/tailcat"
)

// vector is the part of the shared test vector this module reads. The file
// is the same in every program that takes part in a session, and is copied
// here unchanged.
type vector struct {
	SessionKeys struct {
		ServerNodeKeyHKDFOutputHex string `json:"server_node_key_hkdf_output_hex"`
		ServerNodePrivateHex       string `json:"server_node_private_hex"`
		ServerNodePublicHex        string `json:"server_node_public_hex"`
		ServerDiscoPublicHex       string `json:"server_disco_public_hex"`
		ClientNodePrivateHex       string `json:"client_node_private_hex"`
		ClientNodePublicHex        string `json:"client_node_public_hex"`
		PresharedKeyHex            string `json:"preshared_key_hex"`
	} `json:"session_keys"`
	Sealed struct {
		Plaintext struct {
			Relay struct {
				Hostname string `json:"hostname"`
				DERPPort int    `json:"derp_port"`
				STUNPort int    `json:"stun_port"`
			} `json:"relay"`
		} `json:"plaintext"`
	} `json:"sealed"`
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

func keyFromHex(t *testing.T, text string) Key {
	t.Helper()
	raw, err := hex.DecodeString(text)
	if err != nil || len(raw) != len(Key{}) {
		t.Fatalf("not a 32-byte key in hex: %q", text)
	}
	return Key(raw)
}

// The vector's public keys were derived from its private keys by the rules
// both ends follow. The tunnel library has to derive the same ones, the
// device's path-discovery key included, or the two ends of a session would
// each hold a different picture of the other.
func TestTheLibraryDerivesTheVectorsPublicKeys(t *testing.T) {
	v := loadVector(t)
	server, err := nodeKey(keyFromHex(t, v.SessionKeys.ServerNodePrivateHex))
	if err != nil {
		t.Fatal(err)
	}
	client, err := nodeKey(keyFromHex(t, v.SessionKeys.ClientNodePrivateHex))
	if err != nil {
		t.Fatal(err)
	}
	if got := server.Public().UntypedHexString(); got != v.SessionKeys.ServerNodePublicHex {
		t.Errorf("device public key = %s, want %s", got, v.SessionKeys.ServerNodePublicHex)
	}
	if got := client.Public().UntypedHexString(); got != v.SessionKeys.ClientNodePublicHex {
		t.Errorf("client public key = %s, want %s", got, v.SessionKeys.ClientNodePublicHex)
	}
	if got := hex.EncodeToString(tailcat.DiscoPublicForNode(server).AppendTo(nil)); got != v.SessionKeys.ServerDiscoPublicHex {
		t.Errorf("device path-discovery key = %s, want %s", got, v.SessionKeys.ServerDiscoPublicHex)
	}
}

func TestTheDeviceAddressOfTheVector(t *testing.T) {
	v := loadVector(t)
	cfg := Config{
		Relay:        Relay{Hostname: v.Sealed.Plaintext.Relay.Hostname, Port: v.Sealed.Plaintext.Relay.DERPPort, STUNPort: v.Sealed.Plaintext.Relay.STUNPort},
		Port:         1,
		ClientKey:    keyFromHex(t, v.SessionKeys.ClientNodePrivateHex),
		ServerKey:    keyFromHex(t, v.SessionKeys.ServerNodePrivateHex),
		PresharedKey: keyFromHex(t, v.SessionKeys.PresharedKeyHex),

		ConnectTimeoutSeconds: 30,
	}
	if problem := cfg.problem(); problem != "" {
		t.Fatalf("the vector's session is refused: %s", problem)
	}
	addr, err := deviceAddress(&cfg, relayNode(cfg.Relay))
	if err != nil {
		t.Fatal(err)
	}
	info, err := tailcat.ParseAddr(addr)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.ServerPublic.UntypedHexString(); got != v.SessionKeys.ServerNodePublicHex {
		t.Errorf("the address names the device key %s, want %s", got, v.SessionKeys.ServerNodePublicHex)
	}
	if got := hex.EncodeToString(info.ServerDiscoPublic.AppendTo(nil)); got != v.SessionKeys.ServerDiscoPublicHex {
		t.Errorf("the address names the path-discovery key %s, want %s", got, v.SessionKeys.ServerDiscoPublicHex)
	}
	if got := hex.EncodeToString(info.PresharedKey[:]); got != v.SessionKeys.PresharedKeyHex {
		t.Errorf("the address carries the pre-shared key %s, want %s", got, v.SessionKeys.PresharedKeyHex)
	}
	node := info.Region[0].Nodes[0]
	if node.HostName != "relay-eu.example.net" || node.DERPPort != 443 || node.STUNPort != 3478 {
		t.Errorf("relay = %s:%d (STUN %d), want relay-eu.example.net:443 (STUN 3478)", node.HostName, node.DERPPort, node.STUNPort)
	}
}

// A tunnel key arrives clamped. The key the vector lists before clamping
// differs from the clamped one, and must be refused rather than used: the
// device would hold the clamped one.
func TestAKeyThatWasNotClampedIsRefused(t *testing.T) {
	v := loadVector(t)
	if !keyFromHex(t, v.SessionKeys.ServerNodePrivateHex).clamped() || !keyFromHex(t, v.SessionKeys.ClientNodePrivateHex).clamped() {
		t.Fatal("the vector's tunnel keys read as not clamped")
	}
	unclamped := keyFromHex(t, v.SessionKeys.ServerNodeKeyHKDFOutputHex)
	if unclamped.clamped() {
		t.Fatal("the vector's key before clamping reads as clamped")
	}
	cfg := validConfig()
	cfg.ServerKey = unclamped
	if got := cfg.problem(); got != "server_key is not a clamped key" {
		t.Errorf("problem = %q, want the device key refused", got)
	}
}

// Protocol names everything two ends must share, the tunnel library's exact
// version first. An update that moves the library, or the dependency whose
// version it fixes, changes what this end speaks: it comes with a new
// Protocol, or it does not come.
func TestProtocolNamesTheLibraryVersionsOfThisBuild(t *testing.T) {
	want := map[int][2]string{
		1: {"v0.7.0", "v1.103.0-pre.0.20260916030321-a2263542f260"},
	}[Protocol]
	if want == [2]string{} {
		t.Fatalf("Protocol %d has no library versions listed in this test", Protocol)
	}
	gomod, err := os.ReadFile("../../go.mod")
	if err != nil {
		t.Fatal(err)
	}
	for module, version := range map[string]string{"github.com/tailscale/tailcat": want[0], "tailscale.com": want[1]} {
		m := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(module) + ` (\S+)`).FindSubmatch(gomod)
		if m == nil {
			t.Errorf("go.mod does not require %s", module)
			continue
		}
		if string(m[1]) != version {
			t.Errorf("go.mod requires %s %s; Protocol %d is %s", module, m[1], Protocol, version)
		}
	}
}
