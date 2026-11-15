package pathfinder

// link is one session's path to the device, once the device is on the other
// end. It carries whole frames both ways and says when, and why, it ended.
// Two things are one: the websocket relay (RelayClient), and a session on the
// direct-peer + encrypted relay transport (companionLink). Streams, the
// shell, the webadmin tunnel and exec run on a link and are the same on both.
type link interface {
	// SendFrame queues one encoded frame for the device.
	SendFrame(data []byte) error
	// OnFrame sets the handler that is given each frame the device sends.
	OnFrame(handler func([]byte))
	// SetStreamManager names the streams to close when the link ends.
	SetStreamManager(mgr *StreamManager)
	// IsConnected is true until the link ends.
	IsConnected() bool
	// EndReason says why the other side ended the link; it is "" while the
	// link runs.
	EndReason() string
	// Done is closed once the link has ended.
	Done() <-chan struct{}
	Close() error
}

var _ link = (*RelayClient)(nil)
