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
	if len(data) < int(9+length) {
		return nil, fmt.Errorf("frame data incomplete: expected %d, got %d", 9+length, len(data))
	}

	f.Data = data[9 : 9+length]
	return f, nil
}
