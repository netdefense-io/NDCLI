package pathfinder

import (
	"context"
	"time"

	"github.com/netdefense-io/NDCLI/internal/config"
)

// pairDirect starts the companion and waits until it has reached the device.
// With beat it repeats its progress while it waits, as the websocket relay's
// pairing wait does for a caller that resets its timeout on progress.
func (c *Client) pairDirect(ctx context.Context, beat bool) (*companionLink, error) {
	d := c.direct
	timeout := d.ConnectTimeout
	if timeout <= 0 {
		timeout = directConnectTimeout
	}

	c.progress("Starting a " + transportName + " session...")
	path, err := locateCompanion()
	if err != nil {
		return nil, err
	}
	proc, err := startCompanion(path)
	if err != nil {
		return nil, err
	}

	cfg := companionConfig{
		Relay:                 companionRelay{Hostname: d.RelayHost, Port: d.RelayPort, STUNPort: d.RelaySTUNPort},
		Port:                  directDevicePort,
		ClientKey:             d.ClientKey,
		ServerKey:             d.DeviceKey,
		PresharedKey:          d.PresharedKey,
		ConnectTimeoutSeconds: RetryAfterSeconds(timeout),
		Debug:                 config.Get().Debug.Enabled,
	}
	err = proc.configure(cfg)
	cfg.ClientKey, cfg.ServerKey, cfg.PresharedKey = companionKey{}, companionKey{}, companionKey{}
	// The companion has the keys now, or never will: a session is dialed
	// once, and nothing here needs them again.
	clear(d.ClientKey[:])
	clear(d.DeviceKey[:])
	clear(d.PresharedKey[:])
	if err != nil {
		proc.discard()
		return nil, &DirectError{Failure: DirectCompanionFailed, Message: companionName + " stopped before it was given the session"}
	}

	c.progress("Waiting for device...")
	stopBeat := func() {}
	if beat {
		left := timeout
		if deadline, ok := ctx.Deadline(); ok {
			left = min(left, time.Until(deadline))
		}
		stopBeat = c.heartbeatSaying(pairingHeartbeat, left, "Still waiting for the device to answer (%ds left)")
	}
	err = proc.awaitConnected(ctx, cfg)
	stopBeat()
	if err != nil {
		proc.discard()
		if ctx.Err() != nil {
			return nil, &PathfinderError{Message: "pairing failed: " + context.Cause(ctx).Error(), Err: ctx.Err()}
		}
		return nil, err
	}
	return newCompanionLink(proc, d.OnPath), nil
}

// DirectProtocol is the tunnel generation this build's companion runs, which
// a front-end states when it asks for a session.
const DirectProtocol = companionProtocol

// CheckCompanion reports, as a DirectError, a companion that is missing or
// not of this release. It starts the companion, reads who it is and lets it
// go, so a caller can refuse a session before it asks the device for one.
func CheckCompanion() error {
	path, err := locateCompanion()
	if err != nil {
		return err
	}
	proc, err := startCompanion(path)
	if err != nil {
		return err
	}
	proc.discard()
	return nil
}
