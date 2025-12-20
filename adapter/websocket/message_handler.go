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
	switch parsed.ReferenceID {
	case "_heartbeat":
		return handleHeartbeat(parsed.Payload, mh.client)
	case "_disconnect":
		return handleDisconnect(parsed.Payload, mh.client)
	case "_resetsubscriptions":
		return handleResetSubscriptions(parsed.Payload, mh.client)
	default:
		mh.client.logger.Printf("Unknown control message: %s", parsed.ReferenceID)
	}
	return nil
}

// handleDataMessage routes data messages by reference ID following legacy subscription patterns
func (mh *MessageHandler) handleDataMessage(parsed *ParsedMessage) error {
	//mh.client.logger.Printf("🔄 Data message: messageId=%d, referenceId=%s", parsed.MessageID, parsed.ReferenceID)

	// Route based on reference ID prefix (human-readable IDs like "prices-20251119-132309")
	// Match by subscription type prefix to handle dynamic timestamp suffixes
	if strings.HasPrefix(parsed.ReferenceID, "prices-") {
		//mh.client.logger.Printf("Routing to price update handler")
		return mh.handlePriceUpdate(parsed.Payload)
	} else if strings.HasPrefix(parsed.ReferenceID, "orders-") {
		//mh.client.logger.Printf("Routing to order update handler")
		return mh.handleOrderUpdate(parsed.Payload)
	} else if strings.HasPrefix(parsed.ReferenceID, "balance-") {
		//mh.client.logger.Printf("Routing to portfolio update handler")
		return mh.handlePortfolioUpdate(parsed.Payload)
	} else if strings.HasPrefix(parsed.ReferenceID, "session-") {
		mh.client.logger.Printf("Routing to session update handler")
		mh.client.handleSessionEvent(parsed.Payload)
		return nil
	} else {
		mh.client.logger.Printf("Unknown data message reference: %s", parsed.ReferenceID)
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

	// Process each price update in the array
	for _, priceData := range priceUpdates {
		// Convert to PriceUpdate for strategy manager
		priceUpdate, err := mh.convertPriceData(priceData)
		if err != nil {
			mh.client.logger.Printf("Failed to convert price data: %v", err)
			continue
		}

		//mh.client.logger.Printf("✅ Price update parsed: %s bid=%.5f ask=%.5f mid=%.5f",
		//	priceUpdate.Ticker, priceUpdate.Bid, priceUpdate.Ask, priceUpdate.Mid)

		// Send to strategy_manager via channel following legacy coordination patterns
		select {
		case mh.client.priceUpdateChan <- *priceUpdate:
			// mh.client.logger.Printf("✅ Price update sent to channel: %s", priceUpdate.Ticker)
			// Price update sent successfully
		default:
			mh.client.logger.Printf("Price update channel full, dropping update for %s", priceUpdate.Ticker)
		}
	}

	return nil
}

// handleOrderUpdate processes order status messages following legacy order coordination patterns
func (mh *MessageHandler) handleOrderUpdate(payload []byte) error {
	mh.client.logger.Printf("Order update received (payload size: %d bytes)", len(payload))

	// Parse JSON payload
	var orderData map[string]interface{}
	if err := json.Unmarshal(payload, &orderData); err != nil {
		return fmt.Errorf("failed to unmarshal order data: %w", err)
	}

	// Convert to OrderUpdate
	orderUpdate, err := mh.parseOrderData(orderData)
	if err != nil {
		return fmt.Errorf("failed to parse order data: %w", err)
	}

	// Send to channel (non-blocking)
	select {
	case mh.client.orderUpdateChan <- *orderUpdate:
		mh.client.logger.Printf("Order update sent: OrderId=%s Status=%s", orderUpdate.OrderId, orderUpdate.Status)
	default:
		mh.client.logger.Printf("Order update channel full, dropping update for OrderId=%s", orderUpdate.OrderId)
	}

	return nil
}

// parseOrderData extracts order information from Saxo streaming format
func (mh *MessageHandler) parseOrderData(orderData map[string]interface{}) (*saxo.OrderUpdate, error) {
	// Extract order ID
	orderIdRaw, exists := orderData["OrderId"]
	if !exists {
		return nil, fmt.Errorf("missing OrderId in order data")
	}

	orderId := fmt.Sprintf("%v", orderIdRaw)

	// Extract order status following legacy order management patterns
	status, exists := orderData["Status"].(string)
	if !exists {
		return nil, fmt.Errorf("missing Status in order data")
	}

	// Extract filled size if available
	filledSize := 0.0
	if filled, exists := orderData["FilledAmount"]; exists {
		if filledFloat, err := mh.convertToFloat64(filled); err == nil {
			filledSize = filledFloat
		}
	}

	return &saxo.OrderUpdate{
		OrderId:    orderId,
		Status:     status,
		FilledSize: filledSize,
		UpdatedAt:  time.Now(),
	}, nil
}

// handlePortfolioUpdate processes portfolio balance messages following legacy portfolio coordination patterns
func (mh *MessageHandler) handlePortfolioUpdate(payload []byte) error {
	mh.client.logger.Printf("Portfolio update received (payload size: %d bytes)", len(payload))

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
		mh.client.logger.Printf("Portfolio update sent: Balance=%.2f MarginUsed=%.2f", portfolioUpdate.Balance, portfolioUpdate.MarginUsed)
	default:
		mh.client.logger.Printf("Portfolio update channel full, dropping update")
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

// convertPriceData converts StreamingPriceUpdate to saxo.PriceUpdate
func (mh *MessageHandler) convertPriceData(priceData StreamingPriceUpdate) (*saxo.PriceUpdate, error) {
	// Look up ticker from UIC
	mh.client.mappingMu.RLock()
	ticker, exists := mh.client.uicToTicker[priceData.Uic]
	mh.client.mappingMu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("no ticker mapping for UIC %d", priceData.Uic)
	}

	return &saxo.PriceUpdate{
		Ticker:    ticker,
		Bid:       priceData.Quote.Bid,
		Ask:       priceData.Quote.Ask,
		Mid:       priceData.Quote.Mid,
		Timestamp: time.Now(),
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
