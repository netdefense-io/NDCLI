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
	tunnel.secret = testSecret()
	if err := tunnel.Start(); err != nil {
		t.Fatalf("start tunnel: %v", err)
	}
	defer tunnel.Stop()

	for id := uint32(1); id <= 25; id++ {
		conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", tunnel.Port()))
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		request := fmt.Sprintf("GET / HTTP/1.1\r\nHost: localhost:%d\r\nCookie: ndcli-tunnel-%d=%s\r\n\r\n", tunnel.Port(), tunnel.Port(), testCookie)
		if _, err := io.WriteString(conn, request); err != nil {
			t.Fatalf("write request: %v", err)
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

// A device may answer a request and close the stream before the request's
// body has been sent (a refused upload). The answer reaches the client in full,
// even to a client that sends its whole body before it reads, and the body the
// tunnel no longer forwards does not reset the connection under it.
func TestTunnelDeliversAReplyTheDeviceClosedBeforeTheBodyWasSent(t *testing.T) {
	mgr := NewStreamManager(newPairedRelay())
	tunnel := NewTunnel(0, "webadmin", mgr)
	tunnel.secret = testSecret()
	if err := tunnel.Start(); err != nil {
		t.Fatalf("start tunnel: %v", err)
	}
	defer func() { _ = tunnel.Stop() }()

	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", tunnel.Port()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	body := bytes.Repeat([]byte("b"), 8<<20)
	head := fmt.Sprintf("POST /upload HTTP/1.1\r\nHost: 127.0.0.1:%d\r\nOrigin: http://127.0.0.1:%d\r\nCookie: ndcli-tunnel-%d=%s\r\nContent-Length: %d\r\n\r\n", tunnel.Port(), tunnel.Port(), tunnel.Port(), testCookie, len(body))
	if _, err := io.WriteString(conn, head); err != nil {
		t.Fatalf("write request head: %v", err)
	}
	waitForStream(t, mgr, 1)

	// More than the loopback socket buffers hold, so the reply is still being
	// copied when the body arrives.
	chunk := bytes.Repeat([]byte("r"), maxStreamFrameData)
	var sent bytes.Buffer
	for range 256 {
		sent.Write(chunk)
		mgr.handleMessage(EncodeFrame(&Frame{Type: FrameTypeData, StreamID: 1, Data: chunk}))
	}
	mgr.handleMessage(EncodeFrame(&Frame{Type: FrameTypeClose, StreamID: 1}))

	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	if _, err := conn.Write(body); err != nil {
		t.Fatalf("write request body: %v", err)
	}
	got, err := io.ReadAll(conn)
	if err != nil {
		t.Fatalf("read: %v (after %d of %d bytes)", err, len(got), sent.Len())
	}
	if !bytes.Equal(got, sent.Bytes()) {
		t.Fatalf("client read %d of %d bytes", len(got), sent.Len())
	}
}
