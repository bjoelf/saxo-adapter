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

// getSubscriptionTypeFromPath maps endpoint path to subscription type name
// Following legacy broker_websocket.go pattern exactly
func getSubscriptionTypeFromPath(endpointPath string) string {
	switch endpointPath {
	case "/trade/v1/infoprices/subscriptions":
		return "prices"
	case "/port/v1/orders/subscriptions":
		return "orders"
	case "/root/v1/sessions/events/subscriptions/active":
		return "session"
	case "/port/v1/balances/subscriptions":
		return "balance"
	default:
		// Fallback to a generic name if unknown path
		return "subscription"
	}
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
