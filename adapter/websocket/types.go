package websocket

import "time"

// websocketMessage wraps a WebSocket message with metadata for separated reader/processor architecture
// Following legacy pattern from broker_websocket.go - enables async message processing
type websocketMessage struct {
	MessageType int       // WebSocket message type (Binary, Text, Close, Ping, Pong)
	Data        []byte    // Message payload (copied to prevent buffer reuse issues)
	ReceivedAt  time.Time // Timestamp when message was received
}

// Subscription represents a WebSocket subscription following Saxo streaming API patterns
// Used for tracking subscription lifecycle during complex reconnection logic
// Following legacy SubscriptionInfo pattern from broker_websocket.go
type Subscription struct {
	ContextId           string                 `json:"ContextId"`
	ReferenceId         string                 `json:"ReferenceId"`
	State               string                 `json:"State"`
	SubscribedAt        time.Time              `json:"SubscribedAt"`
	Arguments           map[string]interface{} `json:"Arguments"`
	SubscriptionMessage map[string]interface{} // Original subscription message for resubscription
	EndpointPath        string                 // Saxo API endpoint path for this subscription
	LastMessageTime     time.Time              // Track last message for timeout detection
}

// ResetMessage represents a subscription reset control message from Saxo
// Following legacy pattern for handling _resetsubscriptions control messages
type ResetMessage struct {
	ReferenceID        string   `json:"ReferenceId"`
	TargetReferenceIds []string `json:"TargetReferenceIds"`
}

// HeartbeatMessage represents a heartbeat control message from Saxo
// Following legacy pattern for _heartbeat control messages
type HeartbeatMessage struct {
	ReferenceID string `json:"ReferenceId"`
	Heartbeats  []struct {
		OriginatingReferenceID string `json:"OriginatingReferenceId"`
		Reason                 string `json:"Reason"` // "NoNewData", "SubscriptionTemporarilyDisabled", "SubscriptionPermanentlyDisabled"
	} `json:"Heartbeats"`
}

// SaxoSessionCapabilities represents session state from Saxo API
// Following legacy pattern for session event monitoring
type SaxoSessionCapabilities struct {
	InactivityTimeout int    `json:"InactivityTimeout"`
	RefreshRate       int    `json:"RefreshRate"`
	State             string `json:"State"`
	Snapshot          struct {
		AuthenticationLevel string `json:"AuthenticationLevel"`
		DataLevel           string `json:"DataLevel"`
		TradeLevel          string `json:"TradeLevel"` // Should be "FullTradingAndChat"
	} `json:"Snapshot"`
}
