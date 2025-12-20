package websocket

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"time"
)

// parseMessage processes incoming Saxo WebSocket binary messages
// Following exact legacy broker_websocket.go binary protocol parsing
//
// Saxo WebSocket Binary Protocol:
// - Bytes 0-8: Message Identifier (uint64, little-endian)
// - Bytes 8-10: Reserved
// - Byte 10: Reference ID Size (uint8)
// - Bytes 11 to 11+RefIDSize: Reference ID (string)
// - Byte after Reference ID: Payload Format (0 = JSON)
// - Next 4 bytes: Payload Size (uint32, little-endian)
// - Remaining bytes: Payload (JSON)
func parseMessage(message []byte) (*ParsedMessage, error) {
	if len(message) < 16 {
		return nil, fmt.Errorf("message too short: %d bytes (minimum 16 required)", len(message))
	}

	// Byte index 0-8: Message Identifier
	messid := binary.LittleEndian.Uint64(message[0:8])

	// Byte index 8-10: Reserved (skip)

	// Byte index 10: Reference ID Size
	srefid := int(message[10])

	// Byte index 11: Reference ID
	if len(message) < 11+srefid {
		return nil, fmt.Errorf("message too short for reference ID: %d bytes", len(message))
	}
	refID := string(message[11 : 11+srefid])

	// Byte after Reference ID: Payload Format
	payloadFormatOffset := 11 + srefid
	if len(message) <= payloadFormatOffset {
		return nil, fmt.Errorf("message too short for payload format")
	}
	payloadFormat := message[payloadFormatOffset]

	// Next 4 bytes: Payload Size
	payloadSizeOffset := payloadFormatOffset + 1
	if len(message) < payloadSizeOffset+4 {
		return nil, fmt.Errorf("message too short for payload size")
	}
	payloadSize := binary.LittleEndian.Uint32(message[payloadSizeOffset : payloadSizeOffset+4])

	// Payload
	payloadStart := payloadSizeOffset + 4
	payloadEnd := payloadStart + int(payloadSize)
	if len(message) < payloadEnd {
		return nil, fmt.Errorf("message too short for payload: expected %d, got %d", payloadEnd, len(message))
	}
	payload := message[payloadStart:payloadEnd]

	return &ParsedMessage{
		MessageID:     messid,
		ReferenceID:   refID,
		PayloadFormat: payloadFormat,
		Payload:       payload,
	}, nil
}

// ParsedMessage represents a parsed Saxo WebSocket binary message
type ParsedMessage struct {
	MessageID     uint64 // Sequence number for reconnection
	ReferenceID   string // Subscription reference or control message ID
	PayloadFormat byte   // 0 = JSON
	Payload       []byte // Message payload
}

// IsControlMessage determines if this is a control message
func (pm *ParsedMessage) IsControlMessage() bool {
	return isControlMessage(pm.ReferenceID)
}

// String provides a debug representation
func (pm *ParsedMessage) String() string {
	return fmt.Sprintf("Message{ID:%d, RefID:%s, Format:%d, PayloadSize:%d}",
		pm.MessageID, pm.ReferenceID, pm.PayloadFormat, len(pm.Payload))
}

// handleHeartbeat processes heartbeat control messages
// Following legacy pattern for updating subscription timestamps
func handleHeartbeat(payload []byte, ws *SaxoWebSocketClient) error {
	var heartbeat []HeartbeatMessage
	err := json.Unmarshal(payload, &heartbeat)
	if err != nil {
		return fmt.Errorf("failed to parse heartbeat message: %w", err)
	}

	// Process each heartbeat and update timestamps
	for i, h := range heartbeat {
		if len(h.Heartbeats) <= i {
			continue
		}

		hb := h.Heartbeats[i]
		switch hb.Reason {
		case "NoNewData":
			// Normal heartbeat - update timestamp
			ws.lastMessageTimestampsMu.Lock()
			ws.lastMessageTimestamps[hb.OriginatingReferenceID] = time.Now()
			ws.lastMessageTimestampsMu.Unlock()
		case "SubscriptionTemporarilyDisabled":
			log.Printf("Subscription %s temporarily disabled", hb.OriginatingReferenceID)
		case "SubscriptionPermanentlyDisabled":
			log.Printf("Subscription %s permanently disabled", hb.OriginatingReferenceID)
		default:
			log.Printf("Unknown heartbeat reason: %s", hb.Reason)
		}
	}

	return nil
}

// handleDisconnect processes disconnect control messages
func handleDisconnect(payload []byte, ws *SaxoWebSocketClient) error {
	log.Println("Received disconnect message from Saxo - user needs to log in again")
	// Trigger graceful shutdown
	ws.Close()
	return nil
}

// handleResetSubscriptions processes subscription reset control messages
// Following legacy pattern for handling _resetsubscriptions
func handleResetSubscriptions(payload []byte, ws *SaxoWebSocketClient) error {
	log.Printf("Received reset subscriptions message: %s", string(payload))

	var resets []ResetMessage
	err := json.Unmarshal(payload, &resets)
	if err != nil {
		return fmt.Errorf("failed to parse reset message: %w", err)
	}

	// Handle each reset request
	for _, reset := range resets {
		if err := ws.subscriptionManager.HandleSubscriptionReset(reset.TargetReferenceIds); err != nil {
			log.Printf("Failed to reset subscription(s): %v", err)
		}
	}

	return nil
}
