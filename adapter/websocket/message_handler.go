package websocket

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	saxo "github.com/bjoelf/saxo-adapter/adapter"
)

// MessageHandler processes WebSocket messages following legacy broker_websocket.go patterns
// Handles price updates, order status changes, and portfolio updates for strategy_manager coordination
type MessageHandler struct {
	client *SaxoWebSocketClient
}

// NewMessageHandler creates message handler following legacy message processing patterns
func NewMessageHandler(client *SaxoWebSocketClient) *MessageHandler {
	return &MessageHandler{
		client: client,
	}
}

// StreamingPriceUpdate matches legacy streaming_prices.go format
type StreamingPriceUpdate struct {
	LastUpdated string     `json:"LastUpdated"`
	Quote       PriceQuote `json:"Quote"`
	Uic         int        `json:"Uic"`
}

// PriceQuote matches legacy priceQuote format
type PriceQuote struct {
	AskSize float64 `json:"AskSize"`
	BidSize float64 `json:"BidSize"`
	Ask     float64 `json:"Ask"`
	Bid     float64 `json:"Bid"`
	Mid     float64 `json:"Mid"`
}

// ProcessMessage routes incoming WebSocket messages following legacy patterns
// Uses binary protocol parser for Saxo WebSocket message format
func (mh *MessageHandler) ProcessMessage(message []byte) error {
	// Parse binary Saxo WebSocket message
	parsed, err := parseMessage(message)
	if err != nil {
		return fmt.Errorf("failed to parse WebSocket message: %w", err)
	}

	// Update sequence number for reconnection
	mh.client.lastSequenceNumber = parsed.MessageID

	// Route based on message type (control vs data)
	if parsed.IsControlMessage() {
		return mh.handleControlMessage(parsed)
	}

	return mh.handleDataMessage(parsed)
}

// handleControlMessage processes control messages (_heartbeat, _disconnect, _resetsubscriptions)
func (mh *MessageHandler) handleControlMessage(parsed *ParsedMessage) error {
	//mh.client.logger.Printf("🔧 Control message: messageId=%d, referenceId=%s", parsed.MessageID, parsed.ReferenceID)
	switch parsed.ReferenceID {
	case "_heartbeat":
		return handleHeartbeat(parsed.Payload, mh.client)
	case "_disconnect":
		return handleDisconnect(mh.client)
	case "_resetsubscriptions":
		return handleResetSubscriptions(parsed.Payload, mh.client)
	default:
		mh.client.logger.Warn("Unknown control message",
			"function", "handleControlMessage",
			"reference_id", parsed.ReferenceID)
	}
	return nil
}

// handleDataMessage routes data messages by reference ID following legacy subscription patterns
func (mh *MessageHandler) handleDataMessage(parsed *ParsedMessage) error {
	//mh.client.logger.Printf("🔄 Data message: messageId=%d, referenceId=%s", parsed.MessageID, parsed.ReferenceID)

	// Route based on reference ID prefix (human-readable IDs like "prices-20251119-132309")
	// Match by subscription type prefix to handle dynamic timestamp suffixes
	if strings.Contains(parsed.ReferenceID, PricesSubscriptionKey) {
		//mh.client.logger.Printf("Routing to price update handler")
		return mh.handlePriceUpdate(parsed.Payload)
	} else if strings.Contains(parsed.ReferenceID, OrderUpdatesSubscriptionKey) {
		//mh.client.logger.Printf("Routing to order update handler")
		return mh.handleOrderUpdate(parsed.Payload)
	} else if strings.Contains(parsed.ReferenceID, PortfolioBalanceSubscriptionKey) {
		//mh.client.logger.Printf("Routing to portfolio update handler")
		return mh.handlePortfolioUpdate(parsed.Payload)
	} else if strings.Contains(parsed.ReferenceID, SessionEventsSubscriptionKey) {
		//mh.client.logger.Printf("Routing to session update handler")
		mh.client.handleSessionEvent(parsed.Payload)
		return nil
	} else {
		mh.client.logger.Warn("Unknown data message reference",
			"function", "handleDataMessage",
			"reference_id", parsed.ReferenceID)
	}

	return nil
}

// handlePriceUpdate processes price feed messages following legacy price coordination patterns
// CRITICAL: Saxo sends price updates as JSON array directly, not wrapped in object
// Legacy pattern: json.Unmarshal(incoming, &priceUpdates) where priceUpdates is []StreamingPriceUpdate
func (mh *MessageHandler) handlePriceUpdate(payload []byte) error {
	// Parse as array of price updates following legacy streaming_prices.go pattern
	var priceUpdates []StreamingPriceUpdate
	if err := json.Unmarshal(payload, &priceUpdates); err != nil {
		return fmt.Errorf("failed to unmarshal price updates: %w", err)
	}

	if len(priceUpdates) == 0 {
		return fmt.Errorf("empty price update array")
	}

	//mh.client.logger.Printf("🔍 PARSED: Received %d price updates", len(priceUpdates))

	// Process each price update in the array
	for _, priceData := range priceUpdates {
		// DEBUG: Log structured data from Saxo
		//mh.client.logger.Printf("🔍 UPDATE[%d]: UIC=%d, Bid=%.5f, Ask=%.5f, Mid=%.5f, LastUpdated=%s", i, priceData.Uic, priceData.Quote.Bid, priceData.Quote.Ask, priceData.Quote.Mid, priceData.LastUpdated)

		// Create PriceUpdate directly from Saxo data - no conversion needed!
		// Use Saxo's native UIC for signal matching
		priceUpdate := saxo.PriceUpdate{
			Uic:       priceData.Uic,
			Bid:       priceData.Quote.Bid,
			Ask:       priceData.Quote.Ask,
			Mid:       priceData.Quote.Mid,
			Timestamp: time.Now(),
		}

		//mh.client.logger.Printf("🔍 CREATED: UIC=%d, bid=%.5f, ask=%.5f, mid=%.5f",	priceUpdate.Uic, priceUpdate.Bid, priceUpdate.Ask, priceUpdate.Mid)

		// Skip price updates where ALL values are zero (closed markets, stale data)
		// If ANY value is non-zero, it's valid and should be sent
		if priceUpdate.Bid == 0 && priceUpdate.Ask == 0 && priceUpdate.Mid == 0 {
			//mh.client.logger.Printf("Skipping all-zero price update for UIC %d", priceUpdate.Uic)
			continue
		}

		// Send to strategy_manager via channel following legacy coordination patterns
		select {
		case mh.client.priceUpdateChan <- priceUpdate:
			//mh.client.logger.Printf("🔍 SENT TO CHANNEL: UIC=%d", priceUpdate.Uic)
		default:
			mh.client.logger.Warn("Price update channel full, dropping update",
				"function", "handlePriceUpdate",
				"uic", priceUpdate.Uic)
		}
	}

	return nil
}

// handleOrderUpdate processes order status messages following legacy order coordination patterns
// CRITICAL: Saxo sends order updates as JSON ARRAY, not single object
// Legacy: pivot-web/strategy_manager/streaming_orders.go:82 - var streamingOrders []StreamingOrders
// Following same pattern as handlePriceUpdate which correctly uses array
func (mh *MessageHandler) handleOrderUpdate(payload []byte) error {
	mh.client.logger.Debug("Order update received",
		"function", "handleOrderUpdate",
		"payload_size", len(payload))

	// Parse JSON payload AS ARRAY (matching legacy pattern)
	var orderDataArray []map[string]interface{}
	if err := json.Unmarshal(payload, &orderDataArray); err != nil {
		return fmt.Errorf("failed to unmarshal order data: %w", err)
	}

	// Process each order update in the array
	for _, orderData := range orderDataArray {
		// Convert to OrderUpdate
		orderUpdate, err := mh.parseOrderData(orderData)
		if err != nil {
			// Log error but continue with other orders
			mh.client.logger.Warn("Failed to parse order data, skipping",
				"function", "handleOrderUpdate",
				"error", err)
			continue
		}

		// Send to channel (non-blocking)
		select {
		case mh.client.orderUpdateChan <- *orderUpdate:
			mh.client.logger.Debug("Order update sent",
				"function", "handleOrderUpdate",
				"order_id", orderUpdate.OrderId,
				"status", orderUpdate.Status)
		default:
			mh.client.logger.Warn("Order update channel full, dropping update",
				"function", "handleOrderUpdate",
				"order_id", orderUpdate.OrderId)
		}
	}

	return nil
}

// parseOrderData extracts order information from Saxo streaming format
// Handles both Phase 1 (entry with RelatedOpenOrders) and Phase 2 (flat structure)
// Following legacy pivot-web/strategy_manager/streaming_orders.go:13-75 StreamingOrders struct
func (mh *MessageHandler) parseOrderData(orderData map[string]interface{}) (*saxo.OrderUpdate, error) {
	// Extract order ID (required)
	orderIdRaw, exists := orderData["OrderId"]
	if !exists {
		return nil, fmt.Errorf("missing OrderId in order data")
	}

	orderId := fmt.Sprintf("%v", orderIdRaw)

	orderUpdate := &saxo.OrderUpdate{
		OrderId:   orderId,
		UpdatedAt: time.Now(),
	}

	// Extract order status (may be missing in some updates)
	if status, exists := orderData["Status"].(string); exists {
		orderUpdate.Status = status
	}

	// Extract filled size if available
	if filled, exists := orderData["FilledAmount"]; exists {
		if filledFloat, err := mh.convertToFloat64(filled); err == nil {
			orderUpdate.FilledSize = filledFloat
		}
	}

	// Extract OpenOrderType (Phase 1 & 2)
	if openOrderType, exists := orderData["OpenOrderType"].(string); exists {
		orderUpdate.OpenOrderType = openOrderType
	}

	// Extract OrderPrice (Price field in JSON)
	if price, exists := orderData["Price"]; exists {
		if priceFloat, err := mh.convertToFloat64(price); err == nil {
			orderUpdate.OrderPrice = priceFloat
		}
	}

	// Extract Uic
	if uic, exists := orderData["Uic"]; exists {
		if uicFloat, err := mh.convertToFloat64(uic); err == nil {
			uicInt := int(uicFloat)
			orderUpdate.Uic = &uicInt
		}
	}

	// Extract Amount
	if amount, exists := orderData["Amount"]; exists {
		if amountFloat, err := mh.convertToFloat64(amount); err == nil {
			amountInt := int(amountFloat)
			orderUpdate.Amount = &amountInt
		}
	}

	// Extract __meta_deleted (Phase 2: order cancellation/deletion)
	if metaDeleted, exists := orderData["__meta_deleted"].(bool); exists {
		orderUpdate.MetaDeleted = &metaDeleted
	}

	// Phase 1: Extract RelatedOpenOrders (entry order with nested exit orders)
	if relatedOrdersRaw, exists := orderData["RelatedOpenOrders"]; exists {
		if relatedOrdersArray, ok := relatedOrdersRaw.([]interface{}); ok {
			for _, relatedRaw := range relatedOrdersArray {
				if relatedMap, ok := relatedRaw.(map[string]interface{}); ok {
					relatedOrder := saxo.RelatedOrder{}

					// Extract related order fields
					if relOrderId, exists := relatedMap["OrderId"]; exists {
						relatedOrder.OrderID = fmt.Sprintf("%v", relOrderId)
					}
					if openOrderType, exists := relatedMap["OpenOrderType"].(string); exists {
						relatedOrder.OpenOrderType = openOrderType
					}
					if orderPrice, exists := relatedMap["OrderPrice"]; exists {
						if priceFloat, err := mh.convertToFloat64(orderPrice); err == nil {
							relatedOrder.OrderPrice = priceFloat
						}
					}
					if amount, exists := relatedMap["Amount"]; exists {
						if amountFloat, err := mh.convertToFloat64(amount); err == nil {
							relatedOrder.Amount = amountFloat
						}
					}
					if status, exists := relatedMap["Status"].(string); exists {
						relatedOrder.Status = status
					}
					if metaDeleted, exists := relatedMap["__meta_deleted"].(bool); exists {
						relatedOrder.MetaDeleted = &metaDeleted
					}

					orderUpdate.RelatedOpenOrders = append(orderUpdate.RelatedOpenOrders, relatedOrder)
				}
			}
		}
	}

	return orderUpdate, nil
}

// handlePortfolioUpdate processes portfolio balance messages following legacy portfolio coordination patterns
func (mh *MessageHandler) handlePortfolioUpdate(payload []byte) error {
	mh.client.logger.Debug("Portfolio update received",
		"function", "handlePortfolioUpdate",
		"payload_size", len(payload))

	// Parse JSON payload
	var portfolioData map[string]interface{}
	if err := json.Unmarshal(payload, &portfolioData); err != nil {
		return fmt.Errorf("failed to unmarshal portfolio data: %w", err)
	}

	// Convert to PortfolioUpdate
	portfolioUpdate, err := mh.parsePortfolioData(portfolioData)
	if err != nil {
		return fmt.Errorf("failed to parse portfolio data: %w", err)
	}

	// Send to channel (non-blocking)
	select {
	case mh.client.portfolioUpdateChan <- *portfolioUpdate:
		mh.client.logger.Debug("Portfolio update sent",
			"function", "handlePortfolioUpdate",
			"balance", portfolioUpdate.Balance,
			"margin_used", portfolioUpdate.MarginUsed)
	default:
		mh.client.logger.Warn("Portfolio update channel full, dropping update",
			"function", "handlePortfolioUpdate")
	}

	return nil
}

// parsePortfolioData extracts balance information from Saxo streaming format
func (mh *MessageHandler) parsePortfolioData(portfolioData map[string]interface{}) (*saxo.PortfolioUpdate, error) {
	// Extract balance information following legacy balance patterns
	balance, err := mh.extractFloat64(portfolioData, "TotalValue")
	if err != nil {
		balance = 0.0 // Default if not available
	}

	marginUsed, err := mh.extractFloat64(portfolioData, "MarginUsed")
	if err != nil {
		marginUsed = 0.0
	}

	marginFree, err := mh.extractFloat64(portfolioData, "MarginAvailable")
	if err != nil {
		marginFree = 0.0
	}

	return &saxo.PortfolioUpdate{
		Balance:    balance,
		MarginUsed: marginUsed,
		MarginFree: marginFree,
		UpdatedAt:  time.Now(),
	}, nil
}

// Helper methods for data extraction and conversion

func (mh *MessageHandler) extractFloat64(data map[string]interface{}, key string) (float64, error) {
	value, exists := data[key]
	if !exists {
		return 0, fmt.Errorf("key %s not found", key)
	}

	return mh.convertToFloat64(value)
}

func (mh *MessageHandler) convertToFloat64(value interface{}) (float64, error) {
	switch v := value.(type) {
	case float64:
		return v, nil
	case float32:
		return float64(v), nil
	case int:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case string:
		return strconv.ParseFloat(v, 64)
	default:
		return 0, fmt.Errorf("cannot convert %T to float64", value)
	}
}
