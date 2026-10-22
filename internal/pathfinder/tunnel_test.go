//go:build !windows

package pathfinder

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"testing"
	"time"
)

// A reply the device sends and closes right behind must reach the local TCP
// client in full before the tunnel half-closes the connection.
func TestTunnelDeliversTheWholeReplyBeforeClosing(t *testing.T) {
	mgr := NewStreamManager(newPairedRelay())
	tunnel := NewTunnel(0, "webadmin", mgr)
	if err := tunnel.Start(); err != nil {
		t.Fatalf("start tunnel: %v", err)
	}
	defer tunnel.Stop()

	for id := uint32(1); id <= 25; id++ {
		conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", tunnel.Port()))
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		waitForStream(t, mgr, id)
		sent := sendFrames(mgr, id, 64)

		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		got, err := io.ReadAll(conn)
		conn.Close()
		if err != nil {
			t.Fatalf("connection %d: read: %v", id, err)
		}
		if !bytes.Equal(got, sent) {
			t.Fatalf("connection %d: client read %d of %d bytes", id, len(got), len(sent))
		}
	}
}

func waitForStream(t *testing.T, mgr *StreamManager, id uint32) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mgr.streamsMu.RLock()
		_, ok := mgr.streams[id]
		mgr.streamsMu.RUnlock()
		if ok {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("tunnel did not open stream %d", id)
}
