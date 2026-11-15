package companion

import (
	"context"
	"encoding/hex"
	"errors"
	"net"
	"strconv"

	"github.com/tailscale/tailcat"
	"go4.org/mem"
	"tailscale.com/tailcfg"
	"tailscale.com/types/key"
)

// relayRegionID numbers the one relay in this end's own relay map. The number
// never leaves the process: the address encoding drops it, and the device
// numbers its side for itself.
const relayRegionID = 1

// libraryTunnel is the Tunnel the tunnel library provides: this end is its
// client, the device its server.
type libraryTunnel struct {
	client *tailcat.Client
	port   uint16
}

func newLibraryTunnel(cfg *Config, logf func(string, ...any)) (Tunnel, error) {
	return newTunnelOn(cfg, logf, relayNode(cfg.Relay))
}

// relayNode describes the relay as the configuration names it.
func relayNode(r Relay) *tailcfg.DERPNode {
	return &tailcfg.DERPNode{
		Name:     "relay",
		RegionID: relayRegionID,
		HostName: r.Hostname,
		DERPPort: r.Port,
		STUNPort: r.STUNPort,
	}
}

// newTunnelOn builds the tunnel over one relay node. The device's address is
// assembled here from its key, so nothing is looked up: an address without a
// relay would send the library to its default relay map.
func newTunnelOn(cfg *Config, logf func(string, ...any), node *tailcfg.DERPNode) (Tunnel, error) {
	addr, err := deviceAddress(cfg, node)
	if err != nil {
		return nil, err
	}
	clientKey, err := nodeKey(cfg.ClientKey)
	if err != nil {
		return nil, err
	}
	return &libraryTunnel{
		client: &tailcat.Client{Server: addr, Key: clientKey, Logf: logf},
		port:   uint16(cfg.Port),
	}, nil
}

// deviceAddress is the address of the device's end: its public keys, both
// derived from its tunnel key, the pre-shared key, and the relay.
func deviceAddress(cfg *Config, node *tailcfg.DERPNode) (tailcat.Addr, error) {
	serverKey, err := nodeKey(cfg.ServerKey)
	if err != nil {
		return "", err
	}
	info := tailcat.ConnInfo{
		ServerPublic:      tailcat.NodePublic{NodePublic: serverKey.Public()},
		ServerDiscoPublic: tailcat.DiscoPublicForNode(serverKey),
		PresharedKey:      tailcat.PresharedKey(cfg.PresharedKey),
		Region: []*tailcfg.DERPRegion{{
			RegionID: relayRegionID,
			Nodes:    []*tailcfg.DERPNode{node},
		}},
	}
	addr := info.Addr()
	parsed, err := tailcat.ParseAddr(addr)
	if err != nil {
		return "", err
	}
	if len(parsed.Region) != 1 || len(parsed.Region[0].Nodes) != 1 {
		return "", errors.New("the device address does not carry its relay")
	}
	return addr, nil
}

// nodeKey is k as a tunnel key, byte for byte. The device's path-discovery
// key is derived from these exact bytes on both ends.
func nodeKey(k Key) (key.NodePrivate, error) {
	text := make([]byte, hex.EncodedLen(len(k)))
	defer clear(text)
	hex.Encode(text, k[:])
	return key.ParseNodePrivateUntyped(mem.B(text))
}

func (t *libraryTunnel) Dial(ctx context.Context) (net.Conn, error) {
	return t.client.DialTCPPort(ctx, t.port)
}

func (t *libraryTunnel) Direct(ctx context.Context) (bool, error) {
	res, err := t.client.DiscoPing(ctx)
	if err != nil {
		return false, err
	}
	return res.Endpoint != "", nil
}

func (t *libraryTunnel) Drain(ctx context.Context) error { return t.client.DrainTCP(ctx) }

func (t *libraryTunnel) Close() error { return t.client.Close() }

// relayAcceptsConnections reports whether the relay takes a TCP connection.
// It runs only after the device failed to answer, to tell a relay that cannot
// be reached from a device that stayed silent.
func relayAcceptsConnections(ctx context.Context, relay Relay) bool {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(relay.Hostname, strconv.Itoa(relay.Port)))
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
