package websocket

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// ConnectionManager handles WebSocket connection lifecycle following legacy broker_websocket.go patterns
// Manages 22:00 UTC connection establishment and complex reconnection logic
type ConnectionManager struct {
	client       *SaxoWebSocketClient
	connected    bool
	reconnecting bool

	// Reconnection strategy following legacy exponential backoff patterns
	reconnectAttempts    int
	maxReconnectAttempts int
	baseReconnectDelay   time.Duration
	maxReconnectDelay    time.Duration
}

// NewConnectionManager creates connection manager following legacy WebSocket lifecycle patterns
func NewConnectionManager(client *SaxoWebSocketClient) *ConnectionManager {
	return &ConnectionManager{
		client:               client,
		maxReconnectAttempts: 10,
		baseReconnectDelay:   time.Second * 2,
		maxReconnectDelay:    time.Minute * 5,
	}
}

// EstablishConnection creates WebSocket connection following 22:00 UTC lifecycle pattern
func (cm *ConnectionManager) EstablishConnection(ctx context.Context) error {
	cm.client.logger.Println("===============================================")
	cm.client.logger.Println("EstablishConnection: Starting WebSocket connection")
	cm.client.logger.Println("===============================================")

	if cm.connected {
		cm.client.logger.Println("EstablishConnection: Connection already established")
		return fmt.Errorf("connection already established")
	}

	// Verify authentication before connection - critical for Saxo WebSocket
	cm.client.logger.Println("EstablishConnection: Checking authentication...")
	if !cm.client.authClient.IsAuthenticated() {
		cm.client.logger.Println("❌ EstablishConnection: Authentication FAILED - no valid token")
		return fmt.Errorf("authentication required for WebSocket connection")
	}
	cm.client.logger.Println("✅ EstablishConnection: Authentication verified")

	cm.client.logger.Println("EstablishConnection: Getting access token...")
	accessToken, err := cm.client.authClient.GetAccessToken()
	if err != nil {
		cm.client.logger.Printf("❌ EstablishConnection: Failed to get access token: %v", err)
		return fmt.Errorf("failed to get access token: %w", err)
	}
	cm.client.logger.Printf("✅ EstablishConnection: Access token obtained (length=%d)", len(accessToken))

	// Generate context ID for this WebSocket connection session
	// Following legacy generateHumanReadableID pattern: "websocket-{timestamp}"
	contextId := generateHumanReadableID("websocket")
	cm.client.logger.Printf("EstablishConnection: Generated context ID: %s", contextId)

	// Build WebSocket URL following legacy connectWebSocket pattern
	wsURL := cm.buildWebSocketURL(contextId, 0) // 0 = no lastMessage (fresh connection)
	cm.client.logger.Printf("EstablishConnection: WebSocket URL: %s", wsURL)

	// Configure connection headers with OAuth2 token
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+accessToken)
	headers.Set("User-Agent", "PivotWeb/2.0")

	cm.client.logger.Println("EstablishConnection: Configuring headers...")
	cm.client.logger.Printf("  - Authorization: Bearer <token length=%d>", len(accessToken))
	cm.client.logger.Println("  - User-Agent: PivotWeb/2.0")

	cm.client.logger.Printf("EstablishConnection: Establishing WebSocket connection to: %s", wsURL)

	// Get HTTP client to extract TLS config (for tests with self-signed certs)
	httpClient, err := cm.client.authClient.GetHTTPClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to get HTTP client: %w", err)
	}

	// Create WebSocket connection with timeout
	// Use TLS config from HTTP client if available (for test compatibility)
	dialer := websocket.Dialer{
		HandshakeTimeout: 30 * time.Second,
		ReadBufferSize:   4096,
		WriteBufferSize:  4096,
	}

	// For TLS connections, use the HTTP client's transport TLS config
	// This ensures test mock servers with self-signed certs work properly
	if transport, ok := httpClient.Transport.(*http.Transport); ok && transport.TLSClientConfig != nil {
		dialer.TLSClientConfig = transport.TLSClientConfig
	}

	cm.client.logger.Println("EstablishConnection: Dialing WebSocket...")
	conn, resp, err := dialer.DialContext(ctx, wsURL, headers)
	if err != nil {
		if resp != nil {
			cm.client.logger.Printf("❌ EstablishConnection: WebSocket handshake FAILED with status: %d", resp.StatusCode)
			cm.client.logger.Printf("❌ EstablishConnection: Response headers: %v", resp.Header)
		} else {
			cm.client.logger.Printf("❌ EstablishConnection: Dial failed (no response): %v", err)
		}
		return fmt.Errorf("failed to establish WebSocket connection: %w", err)
	}
	cm.client.logger.Println("✅ EstablishConnection: WebSocket dial successful")

	// Configure connection settings following legacy patterns
	conn.SetReadDeadline(time.Time{})  // No read timeout
	conn.SetWriteDeadline(time.Time{}) // No write timeout

	// Set close handler for graceful shutdown
	conn.SetCloseHandler(func(code int, text string) error {
		cm.client.logger.Printf("WebSocket close received: code=%d, text=%s", code, text)
		cm.handleConnectionClosed()
		return nil
	})

	// Connection established successfully
	cm.client.conn = conn
	cm.client.contextID = contextId // Use the contextId we generated earlier
	cm.client.lastSequenceNumber = 0
	cm.connected = true
	cm.reconnectAttempts = 0

	cm.client.logger.Println("✅ EstablishConnection: WebSocket connection established successfully")
	cm.client.logger.Printf("   - Context ID: %s", cm.client.contextID)
	cm.client.logger.Printf("   - Local address: %v", conn.LocalAddr())
	cm.client.logger.Printf("   - Remote address: %v", conn.RemoteAddr())

	// NEW: Start separated reader/processor/reconnection goroutines
	// Following legacy broker_websocket.go breakthrough pattern - CRITICAL FIX

	// CRITICAL: Create NEW context right before starting goroutines
	// Following legacy startWebSocket pattern (broker_websocket.go:167)
	// This ensures goroutines use a fresh, non-canceled context
	cm.client.logger.Println("EstablishConnection: Creating fresh context for goroutines")
	cm.client.ctx, cm.client.cancel = context.WithCancel(context.Background())

	cm.client.logger.Println("EstablishConnection: Starting goroutines...")

	// Start reader goroutine (ONLY reads from WebSocket)
	cm.client.logger.Println("  - Starting reader goroutine")
	go cm.client.readMessages()

	// Start processor goroutine (handles messages and errors)
	cm.client.logger.Println("  - Starting processor goroutine")
	go cm.client.processMessages()

	// CRITICAL: Check if reconnection handler goroutine is already running (singleton pattern)
	// Following legacy broker_websocket.go pattern - prevents duplicate handlers
	cm.client.reconnectionHandlerMu.Lock()
	if cm.client.reconnectionHandlerRunning {
		cm.client.reconnectionHandlerMu.Unlock()
		cm.client.logger.Println("  - Reconnection handler already running, skipping start")
	} else {
		cm.client.reconnectionHandlerMu.Unlock()
		cm.client.logger.Println("  - Starting reconnection handler goroutine")
		go cm.client.handleReconnectionRequests()
	}

	// Start subscription monitoring (timeout detection)
	cm.client.logger.Println("  - Starting subscription monitoring goroutine")
	go cm.startSubscriptionMonitoring()

	cm.client.logger.Println("===============================================")
	cm.client.logger.Println("✅ EstablishConnection: All goroutines started successfully")
	cm.client.logger.Println("===============================================")
	return nil
}

// HandleConnectionError processes connection failures and triggers reconnection
func (cm *ConnectionManager) HandleConnectionError(err error) {
	cm.client.logger.Printf("WebSocket connection error: %v", err)

	cm.handleConnectionClosed()

	if !cm.reconnecting {
		cm.reconnecting = true
		go cm.reconnectWithBackoff()
	}
}

// reconnectWithBackoff implements exponential backoff reconnection following legacy patterns
func (cm *ConnectionManager) reconnectWithBackoff() {
	defer func() {
		cm.reconnecting = false
	}()

	for cm.reconnectAttempts < cm.maxReconnectAttempts {
		cm.reconnectAttempts++

		// Calculate exponential backoff delay
		delay := time.Duration(cm.reconnectAttempts) * cm.baseReconnectDelay
		if delay > cm.maxReconnectDelay {
			delay = cm.maxReconnectDelay
		}

		cm.client.logger.Printf("Reconnection attempt %d/%d in %v",
			cm.reconnectAttempts, cm.maxReconnectAttempts, delay)

		// Wait before reconnection attempt
		select {
		case <-cm.client.ctx.Done():
			cm.client.logger.Println("Reconnection cancelled due to context cancellation")
			return
		case <-time.After(delay):
			// Continue with reconnection attempt
		}

		// Attempt to reestablish connection
		if err := cm.EstablishConnection(cm.client.ctx); err != nil {
			cm.client.logger.Printf("Reconnection attempt %d failed: %v", cm.reconnectAttempts, err)
			continue
		}

		// Resubscribe to all previous subscriptions with new reference IDs
		if err := cm.client.subscriptionManager.HandleSubscriptions(nil); err != nil {
			cm.client.logger.Printf("Resubscription failed after reconnection: %v", err)
			cm.handleConnectionClosed()
			continue
		}

		cm.client.logger.Println("WebSocket reconnection successful with subscription restoration")
		return
	}

	cm.client.logger.Printf("Max reconnection attempts reached (%d), giving up", cm.maxReconnectAttempts)
}

// startSubscriptionMonitoring monitors subscription health following legacy patterns
// Replaces ping/pong approach - Saxo uses _heartbeat control messages instead
// Following legacy broker_websocket.go timeout detection pattern
func (cm *ConnectionManager) startSubscriptionMonitoring() {
	ticker := time.NewTicker(55 * time.Second) // Check every 55 seconds
	defer ticker.Stop()

	for {
		select {
		case <-cm.client.ctx.Done():
			return
		case <-ticker.C:
			if !cm.connected {
				continue
			}

			// Check for timed-out subscriptions (no message for >100 seconds)
			now := time.Now()
			var timedOut []string

			cm.client.lastMessageTimestampsMu.RLock()
			for refID, lastTimestamp := range cm.client.lastMessageTimestamps {
				if now.Sub(lastTimestamp) > 100*time.Second {
					timedOut = append(timedOut, refID)
				}
			}
			totalSubscriptions := len(cm.client.lastMessageTimestamps)
			cm.client.lastMessageTimestampsMu.RUnlock()

			// If all subscriptions timed out, trigger full reconnect
			if len(timedOut) > 0 && len(timedOut) == totalSubscriptions {
				cm.client.logger.Println("startSubscriptionMonitoring: All subscriptions timed out, triggering reconnect")
				select {
				case cm.client.reconnectionTrigger <- fmt.Errorf("all subscriptions timed out"):
					cm.client.logger.Println("startSubscriptionMonitoring: Reconnection request queued")
				default:
					cm.client.logger.Println("startSubscriptionMonitoring: Reconnection already queued")
				}
				return
			} else if len(timedOut) > 0 {
				// Partial timeout - attempt subscription reset via subscription manager
				cm.client.logger.Printf("startSubscriptionMonitoring: Partial timeout detected for %d/%d subscriptions",
					len(timedOut), totalSubscriptions)
			}
		}
	}
}

// handleConnectionClosed updates connection state following legacy cleanup patterns
func (cm *ConnectionManager) handleConnectionClosed() {
	cm.connected = false

	if cm.client.conn != nil {
		cm.client.conn.Close()
		cm.client.conn = nil
	}
}

// CloseConnection gracefully closes WebSocket connection
func (cm *ConnectionManager) CloseConnection() error {
	cm.client.logger.Println("===============================================")
	cm.client.logger.Println("CloseConnection: Closing WebSocket connection")
	cm.client.logger.Println("===============================================")

	if !cm.connected {
		cm.client.logger.Println("CloseConnection: Already closed (no-op)")
		return nil // Already closed
	}

	if cm.client.conn != nil {
		cm.client.logger.Println("CloseConnection: Sending close message...")
		// Send close message
		err := cm.client.conn.WriteMessage(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
		)
		if err != nil {
			cm.client.logger.Printf("⚠️  CloseConnection: Error sending close message: %v", err)
		} else {
			cm.client.logger.Println("✅ CloseConnection: Close message sent")
		}

		cm.client.logger.Println("CloseConnection: Closing TCP connection...")
		// Close connection
		err = cm.client.conn.Close()
		if err != nil {
			cm.client.logger.Printf("⚠️  CloseConnection: Error closing connection: %v", err)
		} else {
			cm.client.logger.Println("✅ CloseConnection: TCP connection closed")
		}

		cm.client.conn = nil
	}

	cm.connected = false
	cm.reconnectAttempts = 0

	cm.client.logger.Println("===============================================")
	cm.client.logger.Println("✅ CloseConnection: WebSocket connection closed successfully")
	cm.client.logger.Println("===============================================")
	return nil
}

// IsConnected returns current connection status
func (cm *ConnectionManager) IsConnected() bool {
	return cm.connected
}

// buildWebSocketURL constructs Saxo WebSocket URL following legacy connectWebSocket pattern
// Uses websocketURL from LoadSaxoEnvironmentConfig (oauth.go) which includes full streaming path
// SIM: wss://sim-streaming.saxobank.com/sim/oapi/streaming/ws/connect?contextid=xxx
// LIVE: wss://live-streaming.saxobank.com/oapi/streaming/ws/connect?contextid=xxx
func (cm *ConnectionManager) buildWebSocketURL(contextId string, lastMessage uint64) string {
	// Use websocketURL from client (already configured in oauth.go with /streaming/ws path)
	// Converts from https:// to wss://
	wsBaseURL := strings.Replace(cm.client.websocketURL, "https://", "wss://", 1)

	// Append /connect endpoint (websocketURL already includes /streaming/ws)
	fullURL := wsBaseURL + "/connect"

	// Add query parameters following legacy pattern
	params := "?contextid=" + contextId

	// Add messageid if reconnecting (lastMessage > 0)
	if lastMessage > 0 {
		params += fmt.Sprintf("&messageid=%d", lastMessage)
	}

	return fullURL + params
}
