package pathfinder

import (
	"encoding/binary"
	"fmt"
)

const (
	// Frame types for multiplexing multiple streams over a single WebSocket
	FrameTypeData  byte = 0x01
	FrameTypeClose byte = 0x02
	FrameTypeOpen  byte = 0x03
	FrameTypeAck   byte = 0x04
)

// maxFrameDataLen caps a single frame payload, matching the relay WebSocket
// read limit (see RelayClient.readPump's SetReadLimit). Rejecting any length
// above this before computing 9+length keeps that arithmetic well below the
// range where a uint32 sum could wrap.
const maxFrameDataLen = 10 * 1024 * 1024

// Frame represents a multiplexed data frame
type Frame struct {
	Type     byte
	StreamID uint32
	Data     []byte
}

// EncodeFrame encodes a frame for transmission
// Frame format: [type:1][stream_id:4][length:4][data:length]
func EncodeFrame(f *Frame) []byte {
	buf := make([]byte, 9+len(f.Data))
	buf[0] = f.Type
	binary.BigEndian.PutUint32(buf[1:5], f.StreamID)
	binary.BigEndian.PutUint32(buf[5:9], uint32(len(f.Data)))
	copy(buf[9:], f.Data)
	return buf
}

// DecodeFrame decodes a frame from received data
func DecodeFrame(data []byte) (*Frame, error) {
	if len(data) < 9 {
		return nil, fmt.Errorf("frame too short: %d bytes", len(data))
	}

	f := &Frame{
		Type:     data[0],
		StreamID: binary.BigEndian.Uint32(data[1:5]),
	}

	length := binary.BigEndian.Uint32(data[5:9])
	if length > maxFrameDataLen {
		return nil, fmt.Errorf("frame data too large: %d bytes (max %d)", length, maxFrameDataLen)
	}

	end := 9 + int(length)
	if len(data) < end {
		return nil, fmt.Errorf("frame data incomplete: expected %d, got %d", end, len(data))
	}

	f.Data = data[9:end]
	return f, nil
}
