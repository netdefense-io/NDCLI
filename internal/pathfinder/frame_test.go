package pathfinder

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// TestDecodeFrame_LengthWraparound is revert-sensitive: on the pre-fix code,
// a length field in [0xFFFFFFF7, 0xFFFFFFFF] makes the "9+length" bounds
// arithmetic wrap around in uint32, the bounds check passes, and the
// subsequent slice expression panics with "slice bounds out of range".
// Post-fix, DecodeFrame must reject the oversized length before doing any
// arithmetic on it and return an error instead of panicking.
func TestDecodeFrame_LengthWraparound(t *testing.T) {
	data := make([]byte, 10)
	binary.BigEndian.PutUint32(data[5:9], 0xFFFFFFF8) // 9+length wraps to 1 in uint32

	_, err := DecodeFrame(data)
	if err == nil {
		t.Fatal("expected error for wraparound length, got nil (pre-fix this would panic instead)")
	}
}

// TestDecodeFrame_OversizedLength verifies the maxFrameDataLen cap rejects a
// length one byte over the limit, without needing to send that much data.
func TestDecodeFrame_OversizedLength(t *testing.T) {
	data := make([]byte, 9)
	binary.BigEndian.PutUint32(data[5:9], maxFrameDataLen+1)

	_, err := DecodeFrame(data)
	if err == nil {
		t.Fatal("expected error for length exceeding maxFrameDataLen, got nil")
	}
}

// TestDecodeFrame_RoundTrip guards against over-tightening: a normal small
// frame must still encode/decode unchanged.
func TestDecodeFrame_RoundTrip(t *testing.T) {
	orig := &Frame{
		Type:     FrameTypeData,
		StreamID: 42,
		Data:     []byte("hello world"),
	}

	encoded := EncodeFrame(orig)
	decoded, err := DecodeFrame(encoded)
	if err != nil {
		t.Fatalf("unexpected error decoding a valid frame: %v", err)
	}
	if decoded.Type != orig.Type {
		t.Errorf("Type = %v, want %v", decoded.Type, orig.Type)
	}
	if decoded.StreamID != orig.StreamID {
		t.Errorf("StreamID = %v, want %v", decoded.StreamID, orig.StreamID)
	}
	if !bytes.Equal(decoded.Data, orig.Data) {
		t.Errorf("Data = %q, want %q", decoded.Data, orig.Data)
	}
}

// TestDecodeFrame_TooShort verifies the pre-existing short-buffer guard is
// unaffected by the length-cap change.
func TestDecodeFrame_TooShort(t *testing.T) {
	_, err := DecodeFrame(make([]byte, 8))
	if err == nil {
		t.Fatal("expected error for a buffer shorter than the 9-byte header, got nil")
	}
}
