package websocket

import (
	"fmt"
	"time"
)

// generateHumanReadableID generates a human-readable subscription ID with type and timestamp
// Following legacy broker_websocket.go pattern exactly
// Returns format: "{subscriptionType}-{YYYYMMDD-HHMMSS}"
// Examples: "websocket-20241119-130831", "prices-20241119-130832", "orders-20241119-130833"
func generateHumanReadableID(subscriptionType string) string {
	timestamp := time.Now().Format("20060102-150405")
	return fmt.Sprintf("%s-%s", subscriptionType, timestamp)
}

// isControlMessage determines if a message is a control message from Saxo
// Following legacy patterns for _heartbeat, _disconnect, _resetsubscriptions
func isControlMessage(refID string) bool {
	switch refID {
	case "_heartbeat", "_disconnect", "_resetsubscriptions":
		return true
	default:
		return false
	}
}
