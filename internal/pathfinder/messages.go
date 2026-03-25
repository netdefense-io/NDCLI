package pathfinder

import "encoding/json"

// MessageType identifies the type of signaling message
type MessageType string

const (
	MsgTypeRegister    MessageType = "register"
	MsgTypeRegistered  MessageType = "registered"
	MsgTypePaired      MessageType = "paired"
	MsgTypePeerReady   MessageType = "peer_ready"
	MsgTypePeerOffline MessageType = "peer_offline"
	MsgTypeError       MessageType = "error"
)

// Message is the signaling message structure
type Message struct {
	Type    MessageType     `json:"type"`
	From    string          `json:"from,omitempty"`
	To      string          `json:"to,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Error   string          `json:"error,omitempty"`
}

// RegisterPayload contains registration data
type RegisterPayload struct {
	SessionKey string `json:"session_key"`
	Role       string `json:"role"`
	DeviceID   string `json:"device_id,omitempty"`
}

// RegisteredPayload contains registration confirmation data
type RegisteredPayload struct {
	SessionKey string `json:"session_key"`
	Role       string `json:"role"`
	PeerOnline bool   `json:"peer_online"`
}
