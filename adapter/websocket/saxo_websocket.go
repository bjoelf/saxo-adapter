package websocket

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	saxo "github.com/bjoelf/saxo-adapter/adapter"
	"github.com/gorilla/websocket"
)

// SaxoWebSocketClient implements real-time data streaming following legacy broker_websocket.go patterns
type SaxoWebSocketClient struct {
	// Connection management - following legacy WebSocket patterns
	conn         *websocket.Conn
	apiBaseURL   string // For HTTP API calls (subscriptions, etc.) - https://gateway.saxobank.com/sim/openapi
	websocketURL string // For WebSocket connection - https://sim-streaming.saxobank.com/sim/oapi
	authClient   saxo.AuthClient
	logger       *slog.Logger

	// Component managers - following clean architecture separation
	subscriptionManager *SubscriptionManager
	connectionManager   *ConnectionManager
	messageHandler      *MessageHandler

	// Channel coordination - feeds into strategy_manager channels
	priceUpdateChan     chan saxo.PriceUpdate
	orderUpdateChan     chan saxo.OrderUpdate
	portfolioUpdateChan chan saxo.PortfolioUpdate

	// NEW: Separated reader/processor architecture channels (CRITICAL FIX)
	// Following legacy broker_websocket.go breakthrough pattern
	incomingMessages    chan websocketMessage // Buffer 100 messages - prevents blocking during HTTP calls
	connectionErrors    chan error            // Buffer 10 errors - reader reports errors to processor
	reconnectionTrigger chan error            // Buffer 5 reconnection requests - prevents deadlock

	// Message tracking - following legacy timeout detection patterns
	lastMessageTimestamps   map[string]time.Time
	lastMessageTimestampsMu sync.RWMutex
	lastSequenceNumber      uint64

	// Context ID for this WebSocket connection session
	contextID string

	// Lifecycle management - 22:00 UTC patterns
	ctx    context.Context
	cancel context.CancelFunc

	// NEW: Goroutine lifecycle tracking (CRITICAL for clean shutdown)
	// Following legacy pattern from broker_websocket.go
	readerRunning              bool          // Tracks if reader goroutine is active
	readerDone                 chan struct{} // Signals when reader goroutine exits
	readerMu                   sync.Mutex    // Protects reader goroutine state
	processorRunning           bool          // Tracks if processor goroutine is active
	processorDone              chan struct{} // Signals when processor goroutine exits
	processorMu                sync.Mutex    // Protects processor goroutine state
	reconnectionHandlerRunning bool          // Tracks if reconnection handler goroutine is active
	reconnectionHandlerDone    chan struct{} // Signals when reconnection handler exits
	reconnectionHandlerMu      sync.Mutex    // Protects reconnection handler state
	reconnectInProgress        bool          // Flag to prevent concurrent reconnection attempts
	reconnectMu                sync.Mutex    // Protects reconnection state

	// Reconnection logic - exponential backoff following legacy patterns
	maxReconnectAttempts int
	baseReconnectDelay   time.Duration

	// ClientKey for order and portfolio subscriptions (fetched from /port/v1/users/me)
	// CRITICAL: Saxo API requires ClientKey for order/portfolio subscriptions
	clientKey   string       // Cached ClientKey from GetClientInfo
	clientKeyMu sync.RWMutex // Protects ClientKey access

	// Token refresh timer - following legacy broker_websocket.go pattern
	// Timer fires ~18 minutes (2 min before token expires) to reauthorize WebSocket
	tokenRefreshTimer *time.Timer
}

// NewSaxoWebSocketClient creates WebSocket client following legacy broker_websocket.go patterns
// apiBaseURL: For HTTP API calls (e.g., https://gateway.saxobank.com/sim/openapi)
// websocketURL: For WebSocket connection (e.g., https://sim-streaming.saxobank.com/sim/oapi)
func NewSaxoWebSocketClient(authClient saxo.AuthClient, apiBaseURL string, websocketURL string, logger *slog.Logger) *SaxoWebSocketClient {
	// NOTE: Context will be created in EstablishConnection(), not here
	// Following legacy broker_websocket.go pattern where context is created in startWebSocket()
	// This prevents context lifecycle issues during reconnections

	client := &SaxoWebSocketClient{
		apiBaseURL:            apiBaseURL,
		websocketURL:          websocketURL,
		authClient:            authClient,
		logger:                logger,
		lastMessageTimestamps: make(map[string]time.Time),
		priceUpdateChan:       make(chan saxo.PriceUpdate, 100),
		orderUpdateChan:       make(chan saxo.OrderUpdate, 100),
		portfolioUpdateChan:   make(chan saxo.PortfolioUpdate, 100),
		// NEW: Initialize separated reader/processor channels (CRITICAL FIX)
		// Following legacy broker_websocket.go breakthrough pattern
		incomingMessages:     make(chan websocketMessage, 100), // Buffer 100 messages - prevents blocking
		connectionErrors:     make(chan error, 10),             // Buffer 10 errors
		reconnectionTrigger:  make(chan error, 5),              // Buffer 5 reconnection requests
		ctx:                  nil,                              // Will be created in EstablishConnection
		cancel:               nil,                              // Will be created in EstablishConnection
		maxReconnectAttempts: 10,
		baseReconnectDelay:   time.Second * 2,
		lastSequenceNumber:   0,
	}

	// Initialize component managers following clean architecture patterns
	// CRITICAL: Pass HTTP API base URL to subscription manager for HTTP POST subscriptions
	// Subscription manager needs the API base URL (gateway.saxobank.com), not WebSocket URL
	getTokenFunc := func() (string, error) {
		return authClient.GetAccessToken()
	}
	client.subscriptionManager = NewSubscriptionManager(client, apiBaseURL, getTokenFunc)
	client.connectionManager = NewConnectionManager(client)
	client.messageHandler = NewMessageHandler(client)

	return client
}

// Connect establishes WebSocket connection following 22:00 UTC lifecycle pattern
func (ws *SaxoWebSocketClient) Connect(ctx context.Context) error {
	// Delegate to connection manager - following legacy startWebSocket() pattern
	// EstablishConnection will start ALL goroutines with unified lifecycle
	return ws.connectionManager.EstablishConnection(ctx)
}

// SubscribeToPrices delegates to subscription manager following clean architecture
// assetType: "FxSpot", "ContractFutures", "CfdOnFutures", etc.
func (ws *SaxoWebSocketClient) SubscribeToPrices(ctx context.Context, instruments []string, assetType string) error {
	ws.logger.Info("Subscribing to price feeds",
		"function", "SubscribeToPrices",
		"instrument_count", len(instruments),
		"asset_type", assetType,
		"instruments", instruments)
	err := ws.subscriptionManager.SubscribeToInstrumentPrices(instruments, assetType)
	if err != nil {
		ws.logger.Error("Price subscription failed",
			"function", "SubscribeToPrices",
			"error", err)
		return err
	}
	ws.logger.Info("Price subscription successful",
		"function", "SubscribeToPrices",
		"instrument_count", len(instruments),
		"asset_type", assetType)
	return nil
}

// SubscribeToOrders delegates to subscription manager
func (ws *SaxoWebSocketClient) SubscribeToOrders(ctx context.Context) error {
	ws.logger.Info("Subscribing to order status updates",
		"function", "SubscribeToOrders")

	// Fetch ClientKey from broker if not already cached
	if err := ws.ensureClientKey(ctx); err != nil {
		ws.logger.Error("Failed to get ClientKey",
			"function", "SubscribeToOrders",
			"error", err)
		return fmt.Errorf("failed to get ClientKey for order subscription: %w", err)
	}

	ws.clientKeyMu.RLock()
	clientKey := ws.clientKey
	ws.clientKeyMu.RUnlock()

	ws.logger.Debug("Using ClientKey for orders",
		"function", "SubscribeToOrders",
		"client_key", clientKey)
	err := ws.subscriptionManager.SubscribeToOrderUpdates(clientKey)
	if err != nil {
		ws.logger.Error("Order subscription failed",
			"function", "SubscribeToOrders",
			"error", err)
		return err
	}
	ws.logger.Info("Order subscription successful",
		"function", "SubscribeToOrders")
	return nil
}

// SubscribeToPortfolio delegates to subscription manager
func (ws *SaxoWebSocketClient) SubscribeToPortfolio(ctx context.Context) error {
	ws.logger.Info("Subscribing to portfolio balance updates",
		"function", "SubscribeToPortfolio")

	// Fetch ClientKey from broker if not already cached
	if err := ws.ensureClientKey(ctx); err != nil {
		ws.logger.Error("Failed to get ClientKey",
			"function", "SubscribeToPortfolio",
			"error", err)
		return fmt.Errorf("failed to get ClientKey for portfolio subscription: %w", err)
	}

	ws.clientKeyMu.RLock()
	clientKey := ws.clientKey
	ws.clientKeyMu.RUnlock()

	ws.logger.Debug("Using ClientKey for portfolio",
		"function", "SubscribeToPortfolio",
		"client_key", clientKey)
	err := ws.subscriptionManager.SubscribeToPortfolioUpdates(clientKey)
	if err != nil {
		ws.logger.Error("Portfolio subscription failed",
			"function", "SubscribeToPortfolio",
			"error", err)
		return err
	}
	ws.logger.Info("Portfolio subscription successful",
		"function", "SubscribeToPortfolio")
	return nil
}

// SubscribeToSessionEvents delegates to subscription manager
// Reference: pivot-web/broker/broker_websocket.go:63 - sessionsSubscriptionPath
func (ws *SaxoWebSocketClient) SubscribeToSessionEvents(ctx context.Context) error {
	ws.logger.Info("Subscribing to session events",
		"function", "SubscribeToSessionEvents")
	err := ws.subscriptionManager.SubscribeToSessionEvents()
	if err != nil {
		ws.logger.Error("Session events subscription failed",
			"function", "SubscribeToSessionEvents",
			"error", err)
		return err
	}
	ws.logger.Info("Session events subscription successful",
		"function", "SubscribeToSessionEvents")
	return nil
}

// Channel accessor methods for strategy_manager integration
func (ws *SaxoWebSocketClient) GetPriceUpdateChannel() <-chan saxo.PriceUpdate {
	return ws.priceUpdateChan
}

// ensureClientKey fetches and caches ClientKey from broker if not already available
// CRITICAL: Saxo API requires ClientKey for order and portfolio subscriptions
// ClientKey identifies the client account and is required per API documentation:
// - POST /port/v1/orders/subscriptions requires Arguments.ClientKey
// - POST /port/v1/balances/subscriptions requires Arguments.ClientKey
// This method is idempotent - only fetches once and caches the result
func (ws *SaxoWebSocketClient) ensureClientKey(ctx context.Context) error {
	// Check if already cached
	ws.clientKeyMu.RLock()
	if ws.clientKey != "" {
		ws.clientKeyMu.RUnlock()
		ws.logger.Debug("Using cached ClientKey",
			"function", "ensureClientKey",
			"client_key", ws.clientKey)
		return nil
	}
	ws.clientKeyMu.RUnlock()

	// Need to fetch - acquire write lock
	ws.clientKeyMu.Lock()
	defer ws.clientKeyMu.Unlock()

	// Double-check after acquiring write lock (another goroutine may have fetched)
	if ws.clientKey != "" {
		ws.logger.Debug("ClientKey was fetched by another goroutine",
			"function", "ensureClientKey",
			"client_key", ws.clientKey)
		return nil
	}

	// Fetch from broker via authClient's broker client
	// The authClient should provide access to the broker client
	// We need to create a temporary broker client or use a different approach

	// CRITICAL FIX: We need to access the broker client through the auth client
	// The saxo-adapter pattern is: authClient -> brokerClient -> GetClientInfo()
	// Since SaxoWebSocketClient only has authClient, we need to create a broker client

	ws.logger.Debug("Fetching ClientKey from /port/v1/users/me",
		"function", "ensureClientKey")

	// Create a temporary broker client to fetch client info
	// Following saxo-adapter pattern: CreateBrokerServices(authClient, logger)
	brokerClient, err := saxo.CreateBrokerServices(ws.authClient, ws.logger)
	if err != nil {
		return fmt.Errorf("failed to create broker client for ClientKey fetch: %w", err)
	}

	clientInfo, err := brokerClient.GetClientInfo(ctx)
	if err != nil {
		return fmt.Errorf("failed to get client info: %w", err)
	}

	if clientInfo.ClientKey == "" {
		return fmt.Errorf("ClientKey is empty in response from /port/v1/users/me")
	}

	// Cache the ClientKey
	ws.clientKey = clientInfo.ClientKey
	ws.logger.Info("Successfully fetched and cached ClientKey",
		"function", "ensureClientKey",
		"client_key", ws.clientKey)

	return nil
}

func (ws *SaxoWebSocketClient) GetOrderUpdateChannel() <-chan saxo.OrderUpdate {
	return ws.orderUpdateChan
}

func (ws *SaxoWebSocketClient) GetPortfolioUpdateChannel() <-chan saxo.PortfolioUpdate {
	return ws.portfolioUpdateChan
}

// UpdateLastMessageTimestamp updates the last message timestamp for a subscription
// Following legacy timeout detection pattern
func (ws *SaxoWebSocketClient) UpdateLastMessageTimestamp(referenceID string) {
	ws.lastMessageTimestampsMu.Lock()
	defer ws.lastMessageTimestampsMu.Unlock()
	ws.lastMessageTimestamps[referenceID] = time.Now()
}

// GetLastMessageTimestamp retrieves the last message timestamp for a subscription
func (ws *SaxoWebSocketClient) GetLastMessageTimestamp(referenceID string) (time.Time, bool) {
	ws.lastMessageTimestampsMu.RLock()
	defer ws.lastMessageTimestampsMu.RUnlock()
	timestamp, exists := ws.lastMessageTimestamps[referenceID]
	return timestamp, exists
}

// readMessages is a dedicated reader goroutine that ONLY reads from WebSocket
// Following legacy broker_websocket.go breakthrough pattern - CRITICAL FIX
// It never blocks on processing - just reads and passes messages to processor
// This prevents deadlock during subscription resets and HTTP calls
func (ws *SaxoWebSocketClient) readMessages() {
	// Track goroutine lifecycle
	ws.readerMu.Lock()
	ws.readerRunning = true
	ws.readerDone = make(chan struct{})
	ws.readerMu.Unlock()

	defer func() {
		ws.readerMu.Lock()
		ws.readerRunning = false
		if ws.readerDone != nil {
			close(ws.readerDone)
			ws.readerDone = nil
		}
		ws.readerMu.Unlock()
		ws.logger.Debug("Reader goroutine exiting",
			"function", "readMessages")

		// Panic recovery
		if r := recover(); r != nil {
			ws.logger.Error("Panic in readMessages",
				"function", "readMessages",
				"panic", r)
		}
	}()

	ws.logger.Info("Reader goroutine started",
		"function", "readMessages")

	for {
		// Check for context cancellation (clean shutdown)
		select {
		case <-ws.ctx.Done():
			ws.logger.Info("Context canceled, exiting reader",
				"function", "readMessages")
			return
		default:
			// Continue reading
		}

		// Set read deadline (1 minute - aligns with Saxo's _heartbeat every ~60s)
		deadline := time.Now().Add(1 * time.Minute)
		if err := ws.conn.SetReadDeadline(deadline); err != nil {
			ws.logger.Warn("Failed to set read deadline",
				"function", "readMessages",
				"error", err)
		}

		// BLOCKING READ - but that's OK, this goroutine ONLY reads
		messageType, message, err := ws.conn.ReadMessage()

		if err != nil {
			// Log detailed error information
			ws.logger.Error("ReadMessage ERROR detected",
				"function", "readMessages",
				"error", err,
				"error_type", fmt.Sprintf("%T", err))

			// Check if it's a close error
			if closeErr, ok := err.(*websocket.CloseError); ok {
				ws.logger.Error("WebSocket close error details",
					"function", "readMessages",
					"code", closeErr.Code,
					"text", closeErr.Text)
			}

			// Check for network errors
			if netErr, ok := err.(net.Error); ok {
				ws.logger.Error("Network error details",
					"function", "readMessages",
					"timeout", netErr.Timeout(),
					"temporary", netErr.Temporary())
			}

			// Don't process error here - just report it to processor
			select {
			case ws.connectionErrors <- err:
				ws.logger.Debug("Error sent to processor channel",
					"function", "readMessages")
			case <-ws.ctx.Done():
				ws.logger.Debug("Context canceled while sending error",
					"function", "readMessages")
				return
			case <-time.After(1 * time.Second):
				ws.logger.Error("CRITICAL - Error channel full, dropping error",
					"function", "readMessages",
					"error", err)
			}

			// Exit reader on any error - processor will decide what to do
			return
		}

		// Copy message data (ReadMessage may reuse buffer)
		messageCopy := make([]byte, len(message))
		copy(messageCopy, message)

		// Send to processor - non-blocking with timeout
		msg := websocketMessage{
			MessageType: messageType,
			Data:        messageCopy,
			ReceivedAt:  time.Now(),
		}

		select {
		case ws.incomingMessages <- msg:
			// Message queued successfully
			// Only log if queue is getting backed up (>10 messages)
			queueLen := len(ws.incomingMessages)
			if queueLen > 10 {
				ws.logger.Warn("Queue backpressure detected",
					"function", "readMessages",
					"pending_messages", queueLen,
					"message_type", messageType,
					"message_size", len(message))
			}
		case <-ws.ctx.Done():
			return
		case <-time.After(1 * time.Second):
			// Channel full - this is a problem, always log
			ws.logger.Error("CRITICAL - Message channel full, dropping message",
				"function", "readMessages",
				"message_type", messageType,
				"message_size", len(message),
				"queue_length", len(ws.incomingMessages))
		}
	}
}

// processMessages is a dedicated processor goroutine that handles messages and errors
// Following legacy broker_websocket.go breakthrough pattern - CRITICAL FIX
// It can block on processing without affecting the reader
func (ws *SaxoWebSocketClient) processMessages() {
	// Track goroutine lifecycle
	ws.processorMu.Lock()
	ws.processorRunning = true
	ws.processorDone = make(chan struct{})
	ws.processorMu.Unlock()

	defer func() {
		ws.processorMu.Lock()
		ws.processorRunning = false
		if ws.processorDone != nil {
			close(ws.processorDone)
			ws.processorDone = nil
		}
		ws.processorMu.Unlock()
		ws.logger.Debug("Processor goroutine exiting",
			"function", "processMessages")

		// Panic recovery
		if r := recover(); r != nil {
			ws.logger.Error("Panic in processMessages",
				"function", "processMessages",
				"panic", r)
		}
	}()

	ws.logger.Info("Processor goroutine started",
		"function", "processMessages")

	for {
		select {
		case <-ws.ctx.Done():
			ws.logger.Info("Context canceled, exiting processor",
				"function", "processMessages")
			return

		case msg := <-ws.incomingMessages:
			// Process message - can be slow, won't block reader
			ws.processOneMessage(msg)

		case err := <-ws.connectionErrors:
			// Handle error - can be slow, won't block reader
			ws.handleConnectionError(err)
		}
	}
}

// processOneMessage handles a single WebSocket message
// Following legacy broker_websocket.go pattern
func (ws *SaxoWebSocketClient) processOneMessage(msg websocketMessage) {
	//ws.logger.Printf("📥 WebSocket message received: type=%d, size=%d bytes", msg.MessageType, len(msg.Data))

	switch msg.MessageType {
	case websocket.BinaryMessage:
		//ws.logger.Printf("Processing binary message (size=%d bytes)", len(msg.Data))
		// Delegate to message handler
		if err := ws.messageHandler.ProcessMessage(msg.Data); err != nil {
			ws.logger.Error("Message handling error",
				"function", "processOneMessage",
				"message_type", "binary",
				"error", err)
		}

	case websocket.TextMessage:
		ws.logger.Warn("Received unexpected text message",
			"function", "processOneMessage")
		if err := ws.messageHandler.ProcessMessage(msg.Data); err != nil {
			ws.logger.Error("Message handling error",
				"function", "processOneMessage",
				"message_type", "text",
				"error", err)
		}

	case websocket.CloseMessage:
		ws.logger.Info("Received close frame from server",
			"function", "processOneMessage")
		ws.connectionManager.CloseConnection()

	case websocket.PingMessage:
		// Saxo Bank does NOT use WebSocket Ping/Pong frames
		// They use application-level _heartbeat control messages instead
		// Per Saxo documentation: Client NEVER writes to WebSocket (only reads)
		// CRITICAL: Removed Pong write - this was causing race condition and Close 1006 errors!
		ws.logger.Warn("Received unexpected ping frame",
			"function", "processOneMessage",
			"note", "Saxo doesn't use WebSocket ping/pong")

	case websocket.PongMessage:
		// Saxo Bank does NOT use WebSocket Ping/Pong frames
		ws.logger.Warn("Received unexpected pong frame",
			"function", "processOneMessage",
			"note", "Saxo doesn't use WebSocket ping/pong")

	default:
		ws.logger.Warn("Unknown message type",
			"function", "processOneMessage",
			"message_type", msg.MessageType)
	}
}

// handleConnectionError decides what to do about connection errors
// Following legacy broker_websocket.go pattern - routes to reconnection handler
func (ws *SaxoWebSocketClient) handleConnectionError(err error) {
	ws.logger.Error("Processing connection error",
		"function", "handleConnectionError",
		"error", err)

	// Classify error and decide strategy
	if websocket.IsCloseError(err, websocket.CloseNormalClosure) {
		ws.logger.Info("Normal closure, no reconnect needed",
			"function", "handleConnectionError")
		ws.connectionManager.CloseConnection()
		return
	}

	if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway) ||
		strings.Contains(err.Error(), "forcibly closed by the remote host") {
		ws.logger.Warn("Unexpected close, triggering full reconnect",
			"function", "handleConnectionError",
			"error", err)

		// Mark connection as closed immediately
		ws.connectionManager.handleConnectionClosed()

		// Send to reconnection handler (non-blocking)
		select {
		case ws.reconnectionTrigger <- err:
			ws.logger.Debug("Reconnection request queued",
				"function", "handleConnectionError")
		default:
			ws.logger.Debug("Reconnection already queued, skipping duplicate",
				"function", "handleConnectionError")
		}
		return
	}

	if strings.Contains(err.Error(), "use of closed network connection") {
		ws.logger.Debug("Closed network connection detected",
			"function", "handleConnectionError",
			"error", err)
		ws.connectionManager.handleConnectionClosed()
		return
	}

	// Other errors - mark connection closed and send to reconnection handler
	ws.logger.Warn("Unhandled error type, queueing reconnect",
		"function", "handleConnectionError",
		"error", err)
	ws.connectionManager.handleConnectionClosed()

	select {
	case ws.reconnectionTrigger <- err:
		ws.logger.Debug("Reconnection request queued",
			"function", "handleConnectionError")
	default:
		ws.logger.Debug("Reconnection already queued, skipping duplicate",
			"function", "handleConnectionError")
	}
}

// Close terminates WebSocket connection following 21:00 UTC shutdown pattern
func (ws *SaxoWebSocketClient) Close() error {
	// Cancel context to stop goroutines (if context exists)
	if ws.cancel != nil {
		ws.cancel()
	}

	// CRITICAL: Wait for READER goroutine to exit cleanly
	// Following legacy broker_websocket.go cleanup pattern
	ws.readerMu.Lock()
	readerIsRunning := ws.readerRunning
	readerDoneChannel := ws.readerDone
	ws.readerMu.Unlock()

	if readerIsRunning && readerDoneChannel != nil {
		ws.logger.Info("Waiting for reader goroutine to exit",
			"function", "Close")
		select {
		case <-readerDoneChannel:
			ws.logger.Info("Reader exited cleanly",
				"function", "Close")
		case <-time.After(5 * time.Second):
			ws.logger.Warn("Reader exit timeout (forced shutdown)",
				"function", "Close")
		}
	}

	// CRITICAL: Wait for PROCESSOR goroutine to exit cleanly
	ws.processorMu.Lock()
	processorIsRunning := ws.processorRunning
	processorDoneChannel := ws.processorDone
	ws.processorMu.Unlock()

	if processorIsRunning && processorDoneChannel != nil {
		ws.logger.Info("Waiting for processor goroutine to exit",
			"function", "Close")
		select {
		case <-processorDoneChannel:
			ws.logger.Info("Processor exited cleanly",
				"function", "Close")
		case <-time.After(5 * time.Second):
			ws.logger.Warn("Processor exit timeout (forced shutdown)",
				"function", "Close")
		}
	}

	// CRITICAL: Wait for RECONNECTION HANDLER goroutine to exit cleanly
	// Following legacy pattern - ensure no goroutine leaks
	ws.reconnectionHandlerMu.Lock()
	reconnectionHandlerIsRunning := ws.reconnectionHandlerRunning
	reconnectionHandlerDoneChannel := ws.reconnectionHandlerDone
	ws.reconnectionHandlerMu.Unlock()

	if reconnectionHandlerIsRunning && reconnectionHandlerDoneChannel != nil {
		ws.logger.Info("Waiting for reconnection handler goroutine to exit",
			"function", "Close")
		select {
		case <-reconnectionHandlerDoneChannel:
			ws.logger.Info("Reconnection handler exited cleanly",
				"function", "Close")
		case <-time.After(5 * time.Second):
			ws.logger.Warn("Reconnection handler exit timeout (forced shutdown)",
				"function", "Close")
		}
	}

	// Delegate to connection manager for actual connection cleanup
	return ws.connectionManager.CloseConnection()
}

// handleReconnectionRequests runs in a separate goroutine to handle reconnection requests
// Following legacy broker_websocket.go breakthrough pattern - CRITICAL FIX
// This prevents deadlock where processor goroutine tries to reconnect while needing to exit
func (ws *SaxoWebSocketClient) handleReconnectionRequests() {
	// Track goroutine lifecycle following legacy pattern
	ws.reconnectionHandlerMu.Lock()
	ws.reconnectionHandlerRunning = true
	ws.reconnectionHandlerDone = make(chan struct{})
	ws.reconnectionHandlerMu.Unlock()

	defer func() {
		ws.reconnectionHandlerMu.Lock()
		ws.reconnectionHandlerRunning = false
		if ws.reconnectionHandlerDone != nil {
			close(ws.reconnectionHandlerDone)
			ws.reconnectionHandlerDone = nil
		}
		ws.reconnectionHandlerMu.Unlock()
		ws.logger.Debug("Reconnection handler exiting",
			"function", "handleReconnectionRequests")

		// Panic recovery
		if r := recover(); r != nil {
			ws.logger.Error("Panic in handleReconnectionRequests",
				"function", "handleReconnectionRequests",
				"panic", r)
		}
	}()

	ws.logger.Info("Reconnection handler started",
		"function", "handleReconnectionRequests")
	for {
		select {
		case <-ws.ctx.Done():
			ws.logger.Info("Context canceled, exiting reconnection handler",
				"function", "handleReconnectionRequests")
			return
		case err := <-ws.reconnectionTrigger:
			ws.logger.Info("Processing reconnection request",
				"function", "handleReconnectionRequests",
				"error", err)

			// Wait 15 seconds before attempting reconnection (gives time for cleanup)
			// Following legacy pattern - prevents rapid reconnection spam
			time.Sleep(15 * time.Second)

			// Attempt reconnection
			reconnectErr := ws.reconnectWebSocket()
			if reconnectErr != nil {
				ws.logger.Error("Reconnection failed",
					"function", "handleReconnectionRequests",
					"error", reconnectErr)
			} else {
				ws.logger.Info("Reconnection completed successfully",
					"function", "handleReconnectionRequests")
			}
		}
	}
}

// reconnectWebSocket handles the full reconnection process
// Following legacy broker_websocket.go pattern
func (ws *SaxoWebSocketClient) reconnectWebSocket() error {
	ws.reconnectMu.Lock()
	if ws.reconnectInProgress {
		ws.reconnectMu.Unlock()
		ws.logger.Debug("Reconnect already in progress, skipping duplicate call",
			"function", "reconnectWebSocket")
		return nil
	}
	ws.reconnectInProgress = true
	ws.reconnectMu.Unlock()

	defer func() {
		ws.reconnectMu.Lock()
		ws.reconnectInProgress = false
		ws.reconnectMu.Unlock()
	}()

	ws.logger.Info("Reconnecting WebSocket",
		"function", "reconnectWebSocket")

	// CRITICAL: Close existing connection and wait for goroutines to exit
	if ws.conn != nil {
		// Cancel context to signal goroutines to stop (if context exists)
		if ws.cancel != nil {
			ws.cancel()
		}

		// Wait for reader to exit
		ws.readerMu.Lock()
		if ws.readerRunning && ws.readerDone != nil {
			readerDoneChannel := ws.readerDone
			ws.readerMu.Unlock()

			select {
			case <-readerDoneChannel:
				ws.logger.Info("reconnectWebSocket: Reader exited cleanly")
			case <-time.After(5 * time.Second):
				ws.logger.Warn("reconnectWebSocket: Reader exit timeout")
			}
		} else {
			ws.readerMu.Unlock()
		}

		// Wait for processor to exit
		ws.processorMu.Lock()
		if ws.processorRunning && ws.processorDone != nil {
			processorDoneChannel := ws.processorDone
			ws.processorMu.Unlock()

			select {
			case <-processorDoneChannel:
				ws.logger.Debug("Processor exited cleanly",
					"function", "reconnectWebSocket")
			case <-time.After(5 * time.Second):
				ws.logger.Warn("Processor exit timeout",
					"function", "reconnectWebSocket")
			}
		} else {
			ws.processorMu.Unlock()
		}

		// Close connection
		ws.connectionManager.CloseConnection()
	}

	// NOTE: Context will be created in EstablishConnection, not here
	// Following legacy pattern where startWebSocket creates context right before goroutines

	// Wait before reconnecting (exponential backoff)
	backoffDuration := time.Second * 10
	ws.logger.Info("Waiting before reconnection attempt",
		"function", "reconnectWebSocket",
		"backoff_duration", backoffDuration)
	time.Sleep(backoffDuration)

	// CRITICAL: Create fresh context AFTER old goroutines have exited
	// The old ws.ctx was cancelled above to stop goroutines
	// Now that they've exited, create a new context for the new connection
	// This prevents DNS/connection failures on slow networks while avoiding race conditions
	ws.ctx, ws.cancel = context.WithCancel(context.Background())
	ws.logger.Debug("Created fresh context for reconnection after goroutines exited",
		"function", "reconnectWebSocket")

	// Attempt to establish new connection
	if err := ws.connectionManager.EstablishConnection(ws.ctx); err != nil {
		ws.logger.Error("Failed to establish connection",
			"function", "reconnectWebSocket",
			"error", err)
		return err
	}

	// Resubscribe to all previous subscriptions with new context ID and new reference IDs
	if err := ws.subscriptionManager.HandleSubscriptions(nil); err != nil {
		ws.logger.Error("Failed to resubscribe",
			"function", "reconnectWebSocket",
			"error", err)
		return err
	}

	ws.logger.Info("Reconnection completed successfully",
		"function", "reconnectWebSocket")
	return nil
}

// handleSessionEvent processes session event messages
// Following legacy TestForRealtime pattern
func (ws *SaxoWebSocketClient) handleSessionEvent(payload []byte) {
	var session SaxoSessionCapabilities
	err := json.Unmarshal(payload, &session)
	if err != nil {
		ws.logger.Error("Failed to unmarshal session capabilities",
			"function", "handleSessionEvent",
			"error", err)
		return
	}

	ws.logger.Info("Session state received",
		"function", "handleSessionEvent",
		"state", session.State,
		"trade_level", session.Snapshot.TradeLevel)

	// Check if session has full trading capabilities
	if session.Snapshot.TradeLevel != "FullTradingAndChat" {
		ws.logger.Warn("Session does not have FullTradingAndChat, attempting upgrade",
			"function", "handleSessionEvent",
			"current_level", session.Snapshot.TradeLevel)
		// Wait briefly before attempting upgrade
		time.Sleep(5 * time.Second)

		if err := ws.upgradeSessionCapabilities(); err != nil {
			ws.logger.Error("Failed to upgrade session capabilities",
				"function", "handleSessionEvent",
				"error", err)
		}
	}
}

// upgradeSessionCapabilities requests full trading and chat privileges
// Following legacy SetFullTradingAndChat() pattern from broker_http.go
func (ws *SaxoWebSocketClient) upgradeSessionCapabilities() error {
	ctx := context.Background()

	// Get access token for authentication
	accessToken, err := ws.authClient.GetAccessToken()
	if err != nil {
		return fmt.Errorf("failed to get access token: %w", err)
	}

	// Build request body following legacy SaxoTradeLevelParams pattern
	type tradeLevelRequest struct {
		TradeLevel string `json:"TradeLevel"`
	}
	reqBody := tradeLevelRequest{TradeLevel: "FullTradingAndChat"}

	// Marshal request
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal session capability request: %w", err)
	}

	// Build HTTP request following Saxo API pattern
	endpoint := ws.apiBaseURL + "/root/v1/sessions/capabilities"
	req, err := http.NewRequestWithContext(ctx, "PATCH", endpoint, bytes.NewBuffer(jsonBody))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers following legacy broker pattern
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)

	// Execute request
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send session capability upgrade request: %w", err)
	}
	defer resp.Body.Close()

	// Check response status (202 No Content expected for success following legacy pattern)
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("session capability upgrade failed with status: %d", resp.StatusCode)
	}

	ws.logger.Info("Session upgraded to FullTradingAndChat successfully",
		"function", "upgradeSessionCapabilities")
	return nil
}

// startTokenRefreshTimer sets up the initial token refresh timer
// Returns the time until token expiry
// Following legacy broker_websocket.go pattern (lines 213-261)
func (c *SaxoWebSocketClient) startTokenRefreshTimer() time.Duration {
	c.logger.Debug("Setting up token refresh timer",
		"function", "startTokenRefreshTimer")

	// Get current token to check expiry
	// CRITICAL: We can't call getValidToken as that's in oauth package
	// Instead, we rely on authClient being authenticated before WebSocket connection
	accessToken, err := c.authClient.GetAccessToken()
	if err != nil {
		c.logger.Error("Failed to get access token",
			"function", "startTokenRefreshTimer",
			"error", err)
		return -1 * time.Second
	}

	// Get token expiry - we need to call a method that gives us expiry time
	// For now, assume standard 20-minute expiry from connection time
	// TODO: Enhance AuthClient interface to expose token expiry time
	expiryTime := 20 * time.Minute
	c.logger.Debug("Token expiry estimated",
		"function", "startTokenRefreshTimer",
		"expiry_time", expiryTime)

	// Stop any existing timer before creating a new one
	if c.tokenRefreshTimer != nil {
		if !c.tokenRefreshTimer.Stop() {
			// Timer already fired or was stopped, drain the channel if needed
			select {
			case <-c.tokenRefreshTimer.C:
			default:
			}
		}
		c.logger.Debug("Stopped existing token refresh timer",
			"function", "startTokenRefreshTimer")
	}

	// Calculate when to fire: 2 minutes before token expires
	// Following legacy pattern: fireIn = expiryTime - 2*time.Minute (~18 minutes)
	fireIn := expiryTime - 2*time.Minute
	if fireIn < 0 {
		fireIn = 30 * time.Second // Token expires very soon, try again in 30s
		c.logger.Warn("Token expires in less than 2 minutes, scheduling immediate retry",
			"function", "startTokenRefreshTimer")
	}

	// Create timer that will call refreshTokenAndReschedule
	// Following legacy pattern: time.AfterFunc with method reference
	c.tokenRefreshTimer = time.AfterFunc(fireIn, c.refreshTokenAndReschedule)
	c.logger.Debug("Timer set to fire before token expiry",
		"function", "startTokenRefreshTimer",
		"fire_in", fireIn)

	// Verify we have a valid token
	if len(accessToken) == 0 {
		c.logger.Warn("Access token is empty",
			"function", "startTokenRefreshTimer")
		return -1 * time.Second
	}

	return expiryTime
}

// refreshTokenAndReschedule is the callback that refreshes token and ALWAYS reschedules itself
// Following legacy broker_websocket.go pattern (lines 263-308)
func (c *SaxoWebSocketClient) refreshTokenAndReschedule() {
	// CRITICAL: Always reschedule timer at the end, regardless of success/failure
	// Following legacy pattern with defer
	defer func() {
		if r := recover(); r != nil {
			c.logger.Error("Panic in refreshTokenAndReschedule",
				"function", "refreshTokenAndReschedule",
				"panic", r)
			// Even on panic, try to reschedule
			c.scheduleNextRefresh()
			return
		}
		// Normal path: reschedule at the end
		c.scheduleNextRefresh()
	}()

	c.logger.Debug("Timer fired, checking if refresh needed",
		"function", "refreshTokenAndReschedule")

	// Check if WebSocket connection exists
	// Following legacy pattern: if ws.Connection == nil (line 293)
	if c.conn == nil {
		c.logger.Debug("No WebSocket connection to reauthorize",
			"function", "refreshTokenAndReschedule")
		return // Still reschedules via defer
	}

	// Check if we have a context ID
	if c.contextID == "" {
		c.logger.Debug("No context ID available",
			"function", "refreshTokenAndReschedule")
		return
	}

	// Perform the token refresh via WebSocket reauthorization
	// Following legacy pattern: ws.reAuthoriseWebSocket() (line 300)
	c.logger.Info("Attempting to reauthorize WebSocket connection",
		"function", "refreshTokenAndReschedule")
	err := c.authClient.ReauthorizeWebSocket(context.Background(), c.contextID)
	if err != nil {
		c.logger.Error("Reauthorization failed",
			"function", "refreshTokenAndReschedule",
			"error", err)
		return
	}
	c.logger.Info("Token refreshed successfully",
		"function", "refreshTokenAndReschedule")
}

// scheduleNextRefresh calculates when the next refresh should occur and schedules it
// Following legacy broker_websocket.go pattern (lines 310-344)
func (c *SaxoWebSocketClient) scheduleNextRefresh() {
	// Assume standard 20-minute token expiry
	// In production, token was just refreshed, so we have fresh 20 minutes
	expiryTime := 20 * time.Minute

	// Calculate next fire time: 2 minutes before expiry
	// Following legacy pattern: nextFire = expiryTime - 2*time.Minute
	nextFire := expiryTime - 2*time.Minute

	// If token expires in less than 2 minutes, try again soon
	if nextFire < 0 {
		nextFire = 30 * time.Second
		c.logger.Warn("Token expires soon, will retry in 30s",
			"function", "scheduleNextRefresh")
	}

	// Reset the timer
	// Following legacy pattern: ws.tokenRefreshTimer.Reset(nextFire)
	if c.tokenRefreshTimer != nil {
		c.tokenRefreshTimer.Reset(nextFire)
		c.logger.Debug("Timer rescheduled",
			"function", "scheduleNextRefresh",
			"next_fire", nextFire)
	} else {
		// Timer was nil (shouldn't happen, but handle it)
		c.logger.Warn("Timer was nil, creating new timer",
			"function", "scheduleNextRefresh")
		c.tokenRefreshTimer = time.AfterFunc(nextFire, c.refreshTokenAndReschedule)
		c.logger.Debug("New timer created",
			"function", "scheduleNextRefresh",
			"next_fire", nextFire)
	}
}
