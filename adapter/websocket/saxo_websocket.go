package websocket

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
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
	logger       *log.Logger

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
func NewSaxoWebSocketClient(authClient saxo.AuthClient, apiBaseURL string, websocketURL string, logger *log.Logger) *SaxoWebSocketClient {
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
	ws.logger.Printf("SaxoWebSocket: Subscribing to price feeds for %d instruments (AssetType: %s): %v", len(instruments), assetType, instruments)
	err := ws.subscriptionManager.SubscribeToInstrumentPrices(instruments, assetType)
	if err != nil {
		ws.logger.Printf("SaxoWebSocket: Price subscription failed: %v", err)
		return err
	}
	ws.logger.Printf("SaxoWebSocket: ✅ Price subscription successful for %d instruments (AssetType: %s)", len(instruments), assetType)
	return nil
}

// SubscribeToOrders delegates to subscription manager
func (ws *SaxoWebSocketClient) SubscribeToOrders(ctx context.Context) error {
	ws.logger.Println("SaxoWebSocket: Subscribing to order status updates...")

	// Fetch ClientKey from broker if not already cached
	if err := ws.ensureClientKey(ctx); err != nil {
		ws.logger.Printf("SaxoWebSocket: Failed to get ClientKey: %v", err)
		return fmt.Errorf("failed to get ClientKey for order subscription: %w", err)
	}

	ws.clientKeyMu.RLock()
	clientKey := ws.clientKey
	ws.clientKeyMu.RUnlock()

	ws.logger.Printf("SaxoWebSocket: Using ClientKey: %s", clientKey)
	err := ws.subscriptionManager.SubscribeToOrderUpdates(clientKey)
	if err != nil {
		ws.logger.Printf("SaxoWebSocket: Order subscription failed: %v", err)
		return err
	}
	ws.logger.Println("SaxoWebSocket: ✅ Order subscription successful")
	return nil
}

// SubscribeToPortfolio delegates to subscription manager
func (ws *SaxoWebSocketClient) SubscribeToPortfolio(ctx context.Context) error {
	ws.logger.Println("SaxoWebSocket: Subscribing to portfolio balance updates...")

	// Fetch ClientKey from broker if not already cached
	if err := ws.ensureClientKey(ctx); err != nil {
		ws.logger.Printf("SaxoWebSocket: Failed to get ClientKey: %v", err)
		return fmt.Errorf("failed to get ClientKey for portfolio subscription: %w", err)
	}

	ws.clientKeyMu.RLock()
	clientKey := ws.clientKey
	ws.clientKeyMu.RUnlock()

	ws.logger.Printf("SaxoWebSocket: Using ClientKey: %s", clientKey)
	err := ws.subscriptionManager.SubscribeToPortfolioUpdates(clientKey)
	if err != nil {
		ws.logger.Printf("SaxoWebSocket: Portfolio subscription failed: %v", err)
		return err
	}
	ws.logger.Println("SaxoWebSocket: ✅ Portfolio subscription successful")
	return nil
}

// SubscribeToSessionEvents delegates to subscription manager
// Reference: pivot-web/broker/broker_websocket.go:63 - sessionsSubscriptionPath
func (ws *SaxoWebSocketClient) SubscribeToSessionEvents(ctx context.Context) error {
	ws.logger.Println("SaxoWebSocket: Subscribing to session events...")
	err := ws.subscriptionManager.SubscribeToSessionEvents()
	if err != nil {
		ws.logger.Printf("SaxoWebSocket: Session events subscription failed: %v", err)
		return err
	}
	ws.logger.Println("SaxoWebSocket: ✅ Session events subscription successful")
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
		ws.logger.Printf("ensureClientKey: Using cached ClientKey: %s", ws.clientKey)
		return nil
	}
	ws.clientKeyMu.RUnlock()

	// Need to fetch - acquire write lock
	ws.clientKeyMu.Lock()
	defer ws.clientKeyMu.Unlock()

	// Double-check after acquiring write lock (another goroutine may have fetched)
	if ws.clientKey != "" {
		ws.logger.Printf("ensureClientKey: ClientKey was fetched by another goroutine: %s", ws.clientKey)
		return nil
	}

	// Fetch from broker via authClient's broker client
	// The authClient should provide access to the broker client
	// We need to create a temporary broker client or use a different approach

	// CRITICAL FIX: We need to access the broker client through the auth client
	// The saxo-adapter pattern is: authClient -> brokerClient -> GetClientInfo()
	// Since SaxoWebSocketClient only has authClient, we need to create a broker client

	ws.logger.Println("ensureClientKey: Fetching ClientKey from /port/v1/users/me...")

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
	ws.logger.Printf("ensureClientKey: ✅ Successfully fetched and cached ClientKey: %s", ws.clientKey)

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
		ws.logger.Println("readMessages: Reader goroutine exiting")

		// Panic recovery
		if r := recover(); r != nil {
			ws.logger.Printf("Panic in readMessages: %v", r)
		}
	}()

	ws.logger.Println("readMessages: Reader goroutine started")

	for {
		// Check for context cancellation (clean shutdown)
		select {
		case <-ws.ctx.Done():
			ws.logger.Println("readMessages: Context canceled, exiting")
			return
		default:
			// Continue reading
		}

		// Set read deadline (1 minute - aligns with Saxo's _heartbeat every ~60s)
		deadline := time.Now().Add(1 * time.Minute)
		if err := ws.conn.SetReadDeadline(deadline); err != nil {
			ws.logger.Printf("readMessages: WARNING - Failed to set read deadline: %v", err)
		}

		// BLOCKING READ - but that's OK, this goroutine ONLY reads
		messageType, message, err := ws.conn.ReadMessage()

		if err != nil {
			// Log detailed error information
			ws.logger.Println("===============================================")
			ws.logger.Printf("❌ readMessages: ReadMessage ERROR detected")
			ws.logger.Printf("   Error: %v", err)
			ws.logger.Printf("   Error type: %T", err)

			// Check if it's a close error
			if closeErr, ok := err.(*websocket.CloseError); ok {
				ws.logger.Printf("   Close error code: %d", closeErr.Code)
				ws.logger.Printf("   Close error text: %q", closeErr.Text)
			}

			// Check for network errors
			if netErr, ok := err.(net.Error); ok {
				ws.logger.Printf("   Network error - Timeout: %v, Temporary: %v", netErr.Timeout(), netErr.Temporary())
			}

			ws.logger.Println("===============================================")

			// Don't process error here - just report it to processor
			select {
			case ws.connectionErrors <- err:
				ws.logger.Printf("readMessages: Error sent to processor channel")
			case <-ws.ctx.Done():
				ws.logger.Println("readMessages: Context canceled while sending error")
				return
			case <-time.After(1 * time.Second):
				ws.logger.Printf("❌ readMessages: CRITICAL - Error channel full, dropping error: %v", err)
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
				ws.logger.Printf("readMessages: Queue backpressure detected - %d messages pending (type=%d, size=%d bytes)",
					queueLen, messageType, len(message))
			}
		case <-ws.ctx.Done():
			return
		case <-time.After(1 * time.Second):
			// Channel full - this is a problem, always log
			ws.logger.Printf("readMessages: CRITICAL - Message channel full, dropping message (type=%d, size=%d bytes, queue=%d)",
				messageType, len(message), len(ws.incomingMessages))
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
		ws.logger.Println("processMessages: Processor goroutine exiting")

		// Panic recovery
		if r := recover(); r != nil {
			ws.logger.Printf("Panic in processMessages: %v", r)
		}
	}()

	ws.logger.Println("processMessages: Processor goroutine started")

	for {
		select {
		case <-ws.ctx.Done():
			ws.logger.Println("processMessages: Context canceled, exiting")
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
			ws.logger.Printf("processOneMessage: Message handling error: %v", err)
		}

	case websocket.TextMessage:
		ws.logger.Printf("processOneMessage: Received unexpected text message")
		if err := ws.messageHandler.ProcessMessage(msg.Data); err != nil {
			ws.logger.Printf("processOneMessage: Message handling error: %v", err)
		}

	case websocket.CloseMessage:
		ws.logger.Println("processOneMessage: Received close frame from server")
		ws.connectionManager.CloseConnection()

	case websocket.PingMessage:
		// Saxo Bank does NOT use WebSocket Ping/Pong frames
		// They use application-level _heartbeat control messages instead
		// Per Saxo documentation: Client NEVER writes to WebSocket (only reads)
		// CRITICAL: Removed Pong write - this was causing race condition and Close 1006 errors!
		ws.logger.Println("processOneMessage: Received unexpected ping frame (Saxo doesn't use these)")

	case websocket.PongMessage:
		// Saxo Bank does NOT use WebSocket Ping/Pong frames
		ws.logger.Println("processOneMessage: Received unexpected pong frame (Saxo doesn't use these)")

	default:
		ws.logger.Printf("processOneMessage: Unknown message type: %d", msg.MessageType)
	}
}

// handleConnectionError decides what to do about connection errors
// Following legacy broker_websocket.go pattern - routes to reconnection handler
func (ws *SaxoWebSocketClient) handleConnectionError(err error) {
	ws.logger.Printf("handleConnectionError: Processing error: %v", err)

	// Classify error and decide strategy
	if websocket.IsCloseError(err, websocket.CloseNormalClosure) {
		ws.logger.Println("handleConnectionError: Normal closure, no reconnect needed")
		ws.connectionManager.CloseConnection()
		return
	}

	if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway) ||
		strings.Contains(err.Error(), "forcibly closed by the remote host") {
		ws.logger.Printf("handleConnectionError: Unexpected close, triggering full reconnect")

		// Mark connection as closed immediately
		ws.connectionManager.handleConnectionClosed()

		// Send to reconnection handler (non-blocking)
		select {
		case ws.reconnectionTrigger <- err:
			ws.logger.Println("handleConnectionError: Reconnection request queued")
		default:
			ws.logger.Println("handleConnectionError: Reconnection already queued, skipping duplicate")
		}
		return
	}

	if strings.Contains(err.Error(), "use of closed network connection") {
		ws.logger.Printf("handleConnectionError: Closed network connection: %v", err)
		ws.connectionManager.handleConnectionClosed()
		return
	}

	// Other errors - mark connection closed and send to reconnection handler
	ws.logger.Printf("handleConnectionError: Unhandled error type, queueing reconnect: %v", err)
	ws.connectionManager.handleConnectionClosed()

	select {
	case ws.reconnectionTrigger <- err:
		ws.logger.Println("handleConnectionError: Reconnection request queued")
	default:
		ws.logger.Println("handleConnectionError: Reconnection already queued, skipping duplicate")
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
		ws.logger.Println("Close: Waiting for reader goroutine to exit...")
		select {
		case <-readerDoneChannel:
			ws.logger.Println("Close: Reader exited cleanly")
		case <-time.After(5 * time.Second):
			ws.logger.Println("Close: Reader exit timeout (forced shutdown)")
		}
	}

	// CRITICAL: Wait for PROCESSOR goroutine to exit cleanly
	ws.processorMu.Lock()
	processorIsRunning := ws.processorRunning
	processorDoneChannel := ws.processorDone
	ws.processorMu.Unlock()

	if processorIsRunning && processorDoneChannel != nil {
		ws.logger.Println("Close: Waiting for processor goroutine to exit...")
		select {
		case <-processorDoneChannel:
			ws.logger.Println("Close: Processor exited cleanly")
		case <-time.After(5 * time.Second):
			ws.logger.Println("Close: Processor exit timeout (forced shutdown)")
		}
	}

	// CRITICAL: Wait for RECONNECTION HANDLER goroutine to exit cleanly
	// Following legacy pattern - ensure no goroutine leaks
	ws.reconnectionHandlerMu.Lock()
	reconnectionHandlerIsRunning := ws.reconnectionHandlerRunning
	reconnectionHandlerDoneChannel := ws.reconnectionHandlerDone
	ws.reconnectionHandlerMu.Unlock()

	if reconnectionHandlerIsRunning && reconnectionHandlerDoneChannel != nil {
		ws.logger.Println("Close: Waiting for reconnection handler goroutine to exit...")
		select {
		case <-reconnectionHandlerDoneChannel:
			ws.logger.Println("Close: Reconnection handler exited cleanly")
		case <-time.After(5 * time.Second):
			ws.logger.Println("Close: Reconnection handler exit timeout (forced shutdown)")
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
		ws.logger.Println("handleReconnectionRequests: Reconnection handler exiting")

		// Panic recovery
		if r := recover(); r != nil {
			ws.logger.Printf("Panic in handleReconnectionRequests: %v", r)
		}
	}()

	ws.logger.Println("handleReconnectionRequests: Reconnection handler started")
	for {
		select {
		case <-ws.ctx.Done():
			ws.logger.Println("handleReconnectionRequests: Context canceled, exiting")
			return
		case err := <-ws.reconnectionTrigger:
			ws.logger.Printf("handleReconnectionRequests: Processing reconnection request for error: %v", err)

			// Wait 15 seconds before attempting reconnection (gives time for cleanup)
			// Following legacy pattern - prevents rapid reconnection spam
			time.Sleep(15 * time.Second)

			// Attempt reconnection
			reconnectErr := ws.reconnectWebSocket()
			if reconnectErr != nil {
				ws.logger.Printf("handleReconnectionRequests: Reconnection failed: %v", reconnectErr)
			} else {
				ws.logger.Println("handleReconnectionRequests: Reconnection completed successfully")
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
		ws.logger.Println("reconnectWebSocket: Reconnect already in progress, skipping duplicate call")
		return nil
	}
	ws.reconnectInProgress = true
	ws.reconnectMu.Unlock()

	defer func() {
		ws.reconnectMu.Lock()
		ws.reconnectInProgress = false
		ws.reconnectMu.Unlock()
	}()

	ws.logger.Println("reconnectWebSocket: Reconnecting WebSocket...")

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
				ws.logger.Println("reconnectWebSocket: Reader exited cleanly")
			case <-time.After(5 * time.Second):
				ws.logger.Println("reconnectWebSocket: Reader exit timeout")
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
				ws.logger.Println("reconnectWebSocket: Processor exited cleanly")
			case <-time.After(5 * time.Second):
				ws.logger.Println("reconnectWebSocket: Processor exit timeout")
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
	ws.logger.Printf("reconnectWebSocket: Waiting %v before reconnection attempt", backoffDuration)
	time.Sleep(backoffDuration)

	// Attempt to establish new connection
	if err := ws.connectionManager.EstablishConnection(ws.ctx); err != nil {
		ws.logger.Printf("reconnectWebSocket: Failed to establish connection: %v", err)
		return err
	}

	// Resubscribe to all previous subscriptions with new context ID and new reference IDs
	if err := ws.subscriptionManager.HandleSubscriptions(nil); err != nil {
		ws.logger.Printf("reconnectWebSocket: Failed to resubscribe: %v", err)
		return err
	}

	ws.logger.Println("reconnectWebSocket: Reconnection completed successfully")
	return nil
}

// handleSessionEvent processes session event messages
// Following legacy TestForRealtime pattern
func (ws *SaxoWebSocketClient) handleSessionEvent(payload []byte) {
	var session SaxoSessionCapabilities
	err := json.Unmarshal(payload, &session)
	if err != nil {
		ws.logger.Printf("Failed to unmarshal session capabilities: %v", err)
		return
	}

	ws.logger.Printf("Session state: %s, TradeLevel: %s", session.State, session.Snapshot.TradeLevel)

	// Check if session has full trading capabilities
	if session.Snapshot.TradeLevel != "FullTradingAndChat" {
		ws.logger.Println("Session does not have FullTradingAndChat - attempting upgrade...")
		// Wait briefly before attempting upgrade
		time.Sleep(5 * time.Second)

		if err := ws.upgradeSessionCapabilities(); err != nil {
			ws.logger.Printf("Failed to upgrade session capabilities: %v", err)
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

	ws.logger.Println("✅ Session upgraded to FullTradingAndChat successfully")
	return nil
}

// startTokenRefreshTimer sets up the initial token refresh timer
// Returns the time until token expiry
// Following legacy broker_websocket.go pattern (lines 213-261)
func (c *SaxoWebSocketClient) startTokenRefreshTimer() time.Duration {
	c.logger.Println("startTokenRefreshTimer: Setting up token refresh timer")

	// Get current token to check expiry
	// CRITICAL: We can't call getValidToken as that's in oauth package
	// Instead, we rely on authClient being authenticated before WebSocket connection
	accessToken, err := c.authClient.GetAccessToken()
	if err != nil {
		c.logger.Printf("startTokenRefreshTimer: Failed to get access token: %v", err)
		return -1 * time.Second
	}

	// Get token expiry - we need to call a method that gives us expiry time
	// For now, assume standard 20-minute expiry from connection time
	// TODO: Enhance AuthClient interface to expose token expiry time
	expiryTime := 20 * time.Minute
	c.logger.Printf("startTokenRefreshTimer: Token expires in %s (estimated)", expiryTime)

	// Stop any existing timer before creating a new one
	if c.tokenRefreshTimer != nil {
		if !c.tokenRefreshTimer.Stop() {
			// Timer already fired or was stopped, drain the channel if needed
			select {
			case <-c.tokenRefreshTimer.C:
			default:
			}
		}
		c.logger.Println("startTokenRefreshTimer: Stopped existing token refresh timer")
	}

	// Calculate when to fire: 2 minutes before token expires
	// Following legacy pattern: fireIn = expiryTime - 2*time.Minute (~18 minutes)
	fireIn := expiryTime - 2*time.Minute
	if fireIn < 0 {
		fireIn = 30 * time.Second // Token expires very soon, try again in 30s
		c.logger.Printf("startTokenRefreshTimer: WARNING - Token expires in less than 2 minutes, scheduling immediate retry")
	}

	// Create timer that will call refreshTokenAndReschedule
	// Following legacy pattern: time.AfterFunc with method reference
	c.tokenRefreshTimer = time.AfterFunc(fireIn, c.refreshTokenAndReschedule)
	c.logger.Printf("startTokenRefreshTimer: Timer set to fire in %s (2 minutes before token expiry)", fireIn)

	// Verify we have a valid token
	if len(accessToken) == 0 {
		c.logger.Println("startTokenRefreshTimer: WARNING - Access token is empty")
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
			c.logger.Printf("Panic in refreshTokenAndReschedule: %v", r)
			// Even on panic, try to reschedule
			c.scheduleNextRefresh()
			return
		}
		// Normal path: reschedule at the end
		c.scheduleNextRefresh()
	}()

	c.logger.Println("refreshTokenAndReschedule: Timer fired, checking if refresh needed")

	// Check if WebSocket connection exists
	// Following legacy pattern: if ws.Connection == nil (line 293)
	if c.conn == nil {
		c.logger.Println("refreshTokenAndReschedule: No WebSocket connection to reauthorize")
		return // Still reschedules via defer
	}

	// Check if we have a context ID
	if c.contextID == "" {
		c.logger.Println("refreshTokenAndReschedule: No context ID available")
		return
	}

	// Perform the token refresh via WebSocket reauthorization
	// Following legacy pattern: ws.reAuthoriseWebSocket() (line 300)
	c.logger.Println("refreshTokenAndReschedule: Attempting to reauthorize WebSocket connection")
	err := c.authClient.ReauthorizeWebSocket(context.Background(), c.contextID)
	if err != nil {
		c.logger.Printf("refreshTokenAndReschedule: Reauthorization failed: %v", err)
		return
	}
	c.logger.Println("refreshTokenAndReschedule: Token refreshed successfully")
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
		c.logger.Printf("scheduleNextRefresh: Token expires soon, will retry in 30s")
	}

	// Reset the timer
	// Following legacy pattern: ws.tokenRefreshTimer.Reset(nextFire)
	if c.tokenRefreshTimer != nil {
		c.tokenRefreshTimer.Reset(nextFire)
		c.logger.Printf("scheduleNextRefresh: Timer rescheduled to fire in %s", nextFire)
	} else {
		// Timer was nil (shouldn't happen, but handle it)
		c.logger.Println("scheduleNextRefresh: WARNING - Timer was nil, creating new timer")
		c.tokenRefreshTimer = time.AfterFunc(nextFire, c.refreshTokenAndReschedule)
		c.logger.Printf("scheduleNextRefresh: New timer created to fire in %s", nextFire)
	}
}
