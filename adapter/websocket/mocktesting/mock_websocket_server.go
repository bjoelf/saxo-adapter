package mocktesting

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// MockSaxoWebSocketServer provides a test WebSocket server that mimics Saxo Bank's WebSocket API
// Implements the exact binary protocol as documented at:
// https://www.developer.saxo/openapi/learn/streaming
type MockSaxoWebSocketServer struct {
	server    *httptest.Server
	upgrader  websocket.Upgrader
	clients   map[*websocket.Conn]bool
	clientsMu sync.RWMutex

	// Subscription tracking following Saxo streaming API patterns
	subscriptions map[string]MockSubscription
	subscMu       sync.RWMutex

	// Message ID counter (must be unique per message)
	messageIDCounter uint64
}

// MockSubscription tracks subscription state for testing following Saxo patterns
type MockSubscription struct {
	ContextId   string                 `json:"ContextId"`
	ReferenceId string                 `json:"ReferenceId"`
	Arguments   map[string]interface{} `json:"Arguments"`
	State       string                 `json:"State"`
}

// NewMockSaxoWebSocketServer creates a new mock WebSocket server for testing
func NewMockSaxoWebSocketServer() *MockSaxoWebSocketServer {
	mock := &MockSaxoWebSocketServer{
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		clients:          make(map[*websocket.Conn]bool),
		subscriptions:    make(map[string]MockSubscription),
		messageIDCounter: 1,
	}

	// Create HTTPS test server for WebSocket Secure (wss://) connections
	// This matches Saxo's production pattern: https:// -> wss://
	// Use a router to handle both WebSocket upgrade and HTTP POST subscription requests
	mux := http.NewServeMux()
	mux.HandleFunc("/streaming/ws/connect", mock.handleWebSocket)
	mux.HandleFunc("/trade/v1/infoprices/subscriptions", mock.handlePriceSubscription)
	mux.HandleFunc("/port/v1/orders/subscriptions", mock.handleOrderSubscription)
	mux.HandleFunc("/port/v1/balances/subscriptions", mock.handleBalanceSubscription)

	mock.server = httptest.NewTLSServer(mux)
	return mock
}

// GetBaseURL returns the HTTP base URL for subscription API calls
// Following Saxo pattern where subscriptions are sent via HTTP POST
// Returns: http://127.0.0.1:port (mock equivalent of https://gateway.saxobank.com/sim/openapi)
func (m *MockSaxoWebSocketServer) GetBaseURL() string {
	// Return HTTP URL as-is for subscription HTTP POST requests
	return m.server.URL
}

// GetWebSocketURL returns the WebSocket base URL for streaming connection
// Following Saxo pattern: separate domain for WebSocket streaming with full path
// Returns: https://127.0.0.1:port/streaming/ws (NOTE: https not http!)
// This allows connection_manager.go to convert https:// -> wss:// correctly
// Real Saxo SIM: https://sim-streaming.saxobank.com/sim/oapi/streaming/ws
// Real Saxo LIVE: https://live-streaming.saxobank.com/oapi/streaming/ws
// The connection manager will convert to wss:// and append: /connect?contextid=xxx
func (m *MockSaxoWebSocketServer) GetWebSocketURL() string {
	// CRITICAL: Return https:// URL so connection manager can convert to wss://
	// Following legacy broker_websocket.go pattern where websocketURL is https://
	// connection_manager.go does: strings.Replace(websocketURL, "https://", "wss://", 1)
	baseURL := strings.Replace(m.server.URL, "http://", "https://", 1)
	return baseURL + "/streaming/ws"
}

// GetHTTPClient returns the HTTP client configured for TLS test server
// This client accepts self-signed certificates from the TLS test server
func (m *MockSaxoWebSocketServer) GetHTTPClient() *http.Client {
	return m.server.Client()
}

// Close shuts down the mock server
func (m *MockSaxoWebSocketServer) Close() {
	m.clientsMu.Lock()
	defer m.clientsMu.Unlock()

	// Close all WebSocket connections
	for conn := range m.clients {
		conn.Close()
	}
	m.clients = make(map[*websocket.Conn]bool)
	m.server.Close()
}

// buildSaxoBinaryMessage creates a binary message following Saxo's exact protocol
// Message format (as per Saxo documentation):
// - Bytes 0-7:   Message ID (uint64 little-endian)
// - Bytes 8-9:   Reserved (2 bytes, set to 0)
// - Byte 10:     Reference ID size
// - Bytes 11+:   Reference ID (ASCII string)
// - Next byte:   Payload format (0=JSON, 1=Protobuf)
// - Next 4 bytes: Payload size (uint32 little-endian)
// - Remaining:   Payload data
func (m *MockSaxoWebSocketServer) buildSaxoBinaryMessage(referenceID string, payloadJSON interface{}) ([]byte, error) {
	// Get next message ID (atomic increment for thread safety)
	messageID := atomic.AddUint64(&m.messageIDCounter, 1)

	// Marshal payload to JSON
	payload, err := json.Marshal(payloadJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	refIDBytes := []byte(referenceID)
	refIDSize := byte(len(refIDBytes))
	payloadSize := uint32(len(payload))

	// Calculate total message size
	totalSize := 8 + 2 + 1 + len(refIDBytes) + 1 + 4 + len(payload)
	message := make([]byte, totalSize)

	offset := 0

	// Bytes 0-7: Message ID (uint64 little-endian)
	binary.LittleEndian.PutUint64(message[offset:offset+8], messageID)
	offset += 8

	// Bytes 8-9: Reserved (set to 0)
	message[offset] = 0
	message[offset+1] = 0
	offset += 2

	// Byte 10: Reference ID size
	message[offset] = refIDSize
	offset++

	// Bytes 11+: Reference ID (ASCII)
	copy(message[offset:offset+int(refIDSize)], refIDBytes)
	offset += int(refIDSize)

	// Payload format (0 = JSON)
	message[offset] = 0
	offset++

	// Payload size (uint32 little-endian)
	binary.LittleEndian.PutUint32(message[offset:offset+4], payloadSize)
	offset += 4

	// Payload data
	copy(message[offset:], payload)

	return message, nil
}

// handleWebSocket upgrades HTTP connections to WebSocket and handles messages
func (m *MockSaxoWebSocketServer) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Verify authorization header (following Saxo API patterns)
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") && !strings.HasPrefix(authHeader, "BEARER ") {
		http.Error(w, "Missing or invalid Authorization header", http.StatusUnauthorized)
		return
	}

	// Upgrade connection to WebSocket
	conn, err := m.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	// Track connection with thread safety
	m.clientsMu.Lock()
	m.clients[conn] = true
	m.clientsMu.Unlock()

	defer func() {
		m.clientsMu.Lock()
		delete(m.clients, conn)
		m.clientsMu.Unlock()
	}()

	// Keep connection alive - WebSocket clients will send subscription requests via HTTP
	// The WebSocket is only for receiving streaming data
	for {
		// Read messages (but don't process - subscriptions come via HTTP POST)
		_, _, err := conn.ReadMessage()
		if err != nil {
			break
		}
	}
}

// handlePriceSubscription handles HTTP POST /trade/v1/infoprices/subscriptions
// Following Saxo API pattern: Returns 201 Created with Location header
func (m *MockSaxoWebSocketServer) handlePriceSubscription(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Verify authorization header
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		http.Error(w, "Missing or invalid Authorization header", http.StatusUnauthorized)
		return
	}

	// Read and track subscription request
	var subscriptionReq map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&subscriptionReq); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Store subscription
	referenceID := subscriptionReq["ReferenceId"].(string)
	m.subscMu.Lock()
	m.subscriptions[referenceID] = MockSubscription{
		ContextId:   subscriptionReq["ContextId"].(string),
		ReferenceId: referenceID,
		Arguments:   subscriptionReq["Arguments"].(map[string]interface{}),
		State:       "Active",
	}
	m.subscMu.Unlock()

	// Return 201 Created following Saxo API pattern
	w.Header().Set("Location", fmt.Sprintf("/trade/v1/infoprices/subscriptions/%s", referenceID))
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"State":       "Active",
		"ReferenceId": referenceID,
	})
}

// handleOrderSubscription handles HTTP POST /port/v1/orders/subscriptions
func (m *MockSaxoWebSocketServer) handleOrderSubscription(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Verify authorization header
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		http.Error(w, "Missing or invalid Authorization header", http.StatusUnauthorized)
		return
	}

	// Read and track subscription request
	var subscriptionReq map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&subscriptionReq); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Store subscription
	referenceID := subscriptionReq["ReferenceId"].(string)
	m.subscMu.Lock()
	m.subscriptions[referenceID] = MockSubscription{
		ContextId:   subscriptionReq["ContextId"].(string),
		ReferenceId: referenceID,
		Arguments:   subscriptionReq["Arguments"].(map[string]interface{}),
		State:       "Active",
	}
	m.subscMu.Unlock()

	// Return 201 Created
	w.Header().Set("Location", fmt.Sprintf("/port/v1/orders/subscriptions/%s", referenceID))
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"State":       "Active",
		"ReferenceId": referenceID,
	})
}

// handleBalanceSubscription handles HTTP POST /port/v1/balances/subscriptions
func (m *MockSaxoWebSocketServer) handleBalanceSubscription(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Verify authorization header
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		http.Error(w, "Missing or invalid Authorization header", http.StatusUnauthorized)
		return
	}

	// Read and track subscription request
	var subscriptionReq map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&subscriptionReq); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Store subscription
	referenceID := subscriptionReq["ReferenceId"].(string)
	m.subscMu.Lock()
	m.subscriptions[referenceID] = MockSubscription{
		ContextId:   subscriptionReq["ContextId"].(string),
		ReferenceId: referenceID,
		Arguments:   subscriptionReq["Arguments"].(map[string]interface{}),
		State:       "Active",
	}
	m.subscMu.Unlock()

	// Return 201 Created
	w.Header().Set("Location", fmt.Sprintf("/port/v1/balances/subscriptions/%s", referenceID))
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"State":       "Active",
		"ReferenceId": referenceID,
	})
}

// SendPriceUpdate simulates price feed message following Saxo streaming binary protocol
// CRITICAL: Saxo sends price array directly, NOT wrapped in {"Data": [...]}
// Legacy pattern: json.Unmarshal(incoming, &priceUpdates) where priceUpdates is []StreamingPriceUpdate
func (m *MockSaxoWebSocketServer) SendPriceUpdate(ticker string, bid, ask float64) error {
	// Find the price subscription reference ID (human-readable like "prices-20251119-132651")
	m.subscMu.Lock()
	var priceRefId string
	for refId := range m.subscriptions {
		if refId == "prices" || refId == "price_feed" || len(refId) > 7 && refId[:7] == "prices-" {
			priceRefId = refId
			break
		}
	}
	m.subscMu.Unlock()

	if priceRefId == "" {
		return fmt.Errorf("no price subscription found")
	}

	// Saxo sends array of price updates DIRECTLY, not wrapped in object
	// This matches legacy streaming_prices.go: json.Unmarshal(incoming, &priceUpdates)
	payloadJSON := []interface{}{
		map[string]interface{}{
			"Uic":       m.getUicForTicker(ticker),
			"AssetType": "FxSpot",
			"Quote": map[string]interface{}{
				"Bid": bid,
				"Ask": ask,
				"Mid": (bid + ask) / 2,
			},
			"LastUpdated": time.Now().Format(time.RFC3339),
		},
	}

	binaryMsg, err := m.buildSaxoBinaryMessage(priceRefId, payloadJSON)
	if err != nil {
		return err
	}

	return m.broadcastBinaryMessage(binaryMsg)
}

// SendOrderUpdate simulates order status message following Saxo binary protocol
func (m *MockSaxoWebSocketServer) SendOrderUpdate(orderId, status string) error {
	// Saxo streaming format has a "Data" array
	payloadJSON := map[string]interface{}{
		"Data": []interface{}{
			map[string]interface{}{
				"OrderId":      orderId,
				"Status":       status,
				"FilledAmount": 0.0,
				"BuySell":      "Buy",
				"AssetType":    "FxSpot",
			},
		},
	}

	binaryMsg, err := m.buildSaxoBinaryMessage("order_updates", payloadJSON)
	if err != nil {
		return err
	}

	return m.broadcastBinaryMessage(binaryMsg)
}

// SendPortfolioUpdate simulates balance message following Saxo binary protocol
func (m *MockSaxoWebSocketServer) SendPortfolioUpdate(balance, marginUsed, marginFree float64) error {
	// Saxo streaming format has a "Data" array
	payloadJSON := map[string]interface{}{
		"Data": []interface{}{
			map[string]interface{}{
				"TotalValue":      balance,
				"MarginUsed":      marginUsed,
				"MarginAvailable": marginFree,
				"Currency":        "USD",
			},
		},
	}

	binaryMsg, err := m.buildSaxoBinaryMessage("portfolio_balance", payloadJSON)
	if err != nil {
		return err
	}

	return m.broadcastBinaryMessage(binaryMsg)
}

// SendHeartbeat sends a heartbeat control message following Saxo protocol
func (m *MockSaxoWebSocketServer) SendHeartbeat(originatingRefID, reason string) error {
	payloadJSON := map[string]interface{}{
		"ReferenceId": "_heartbeat",
		"Heartbeats": []map[string]interface{}{
			{
				"OriginatingReferenceId": originatingRefID,
				"Reason":                 reason, // "NoNewData", "SubscriptionTemporarilyDisabled", etc.
			},
		},
	}

	binaryMsg, err := m.buildSaxoBinaryMessage("_heartbeat", payloadJSON)
	if err != nil {
		return err
	}

	return m.broadcastBinaryMessage(binaryMsg)
}

// SendResetSubscriptions sends a reset subscription control message
func (m *MockSaxoWebSocketServer) SendResetSubscriptions(targetRefIDs []string) error {
	payloadJSON := map[string]interface{}{
		"ReferenceId":        "_resetsubscriptions",
		"Timestamp":          time.Now().Format(time.RFC3339),
		"TargetReferenceIds": targetRefIDs,
	}

	binaryMsg, err := m.buildSaxoBinaryMessage("_resetsubscriptions", payloadJSON)
	if err != nil {
		return err
	}

	return m.broadcastBinaryMessage(binaryMsg)
}

// SendDisconnect sends a disconnect control message
func (m *MockSaxoWebSocketServer) SendDisconnect() error {
	payloadJSON := map[string]interface{}{
		"ReferenceId": "_disconnect",
	}

	binaryMsg, err := m.buildSaxoBinaryMessage("_disconnect", payloadJSON)
	if err != nil {
		return err
	}

	return m.broadcastBinaryMessage(binaryMsg)
}

// GetActiveSubscriptions returns current test subscriptions for verification
func (m *MockSaxoWebSocketServer) GetActiveSubscriptions() map[string]MockSubscription {
	m.subscMu.RLock()
	defer m.subscMu.RUnlock()

	// Return copy to prevent external modification
	result := make(map[string]MockSubscription)
	for k, v := range m.subscriptions {
		result[k] = v
	}

	return result
}

// Private helper methods

// broadcastBinaryMessage sends binary message to all connected test clients
func (m *MockSaxoWebSocketServer) broadcastBinaryMessage(binaryMsg []byte) error {
	m.clientsMu.RLock()
	defer m.clientsMu.RUnlock()

	for conn := range m.clients {
		// Send as binary WebSocket frame (not text/JSON)
		if err := conn.WriteMessage(websocket.BinaryMessage, binaryMsg); err != nil {
			return fmt.Errorf("failed to send binary test message: %w", err)
		}
	}
	return nil
}

// getUicForTicker returns test UIC mapping following Saxo instrument patterns
func (m *MockSaxoWebSocketServer) getUicForTicker(ticker string) int {
	// Test UIC mapping following legacy broker patterns
	uicMap := map[string]int{
		"EURUSD": 21,
		"GBPUSD": 22,
		"USDJPY": 23,
		"USDCHF": 24,
		"AUDUSD": 25,
		"USDCAD": 26,
		"NZDUSD": 27,
		"EURJPY": 28,
		"EURGBP": 29,
		"EURCHF": 30,
	}
	return uicMap[ticker]
}
