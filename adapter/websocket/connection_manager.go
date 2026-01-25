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
	cm.client.logger.Info("Starting WebSocket connection",
		"function", "EstablishConnection")

	if cm.connected {
		cm.client.logger.Info("Connection already established",
			"function", "EstablishConnection")
		return fmt.Errorf("connection already established")
	}

	// Verify authentication before connection - critical for Saxo WebSocket
	cm.client.logger.Debug("Checking authentication",
		"function", "EstablishConnection")
	if !cm.client.authClient.IsAuthenticated() {
		cm.client.logger.Error("Authentication failed",
			"function", "EstablishConnection",
			"reason", "no valid token")
		return fmt.Errorf("authentication required for WebSocket connection")
	}
	cm.client.logger.Info("Authentication verified",
		"function", "EstablishConnection")

	cm.client.logger.Debug("Getting access token",
		"function", "EstablishConnection")
	accessToken, err := cm.client.authClient.GetAccessToken()
	if err != nil {
		cm.client.logger.Error("Failed to get access token",
			"function", "EstablishConnection",
			"error", err)
		return fmt.Errorf("failed to get access token: %w", err)
	}
	cm.client.logger.Debug("Access token obtained",
		"function", "EstablishConnection",
		"token_length", len(accessToken))

	// Generate context ID for this WebSocket connection session
	// Following legacy generateHumanReadableID pattern: "websocket-{timestamp}"
	contextId := generateHumanReadableID("websocket")
	cm.client.logger.Debug("Generated context ID",
		"function", "EstablishConnection",
		"context_id", contextId)

	// Build WebSocket URL following legacy connectWebSocket pattern
	wsURL := cm.buildWebSocketURL(contextId, 0) // 0 = no lastMessage (fresh connection)
	cm.client.logger.Debug("WebSocket URL prepared",
		"function", "EstablishConnection",
		"url", wsURL)

	// Configure connection headers with OAuth2 token
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+accessToken)
	headers.Set("User-Agent", "PivotWeb/2.0")

	cm.client.logger.Debug("Configuring headers",
		"function", "EstablishConnection",
		"token_length", len(accessToken))

	cm.client.logger.Info("Establishing WebSocket connection",
		"function", "EstablishConnection",
		"url", wsURL)

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

	cm.client.logger.Debug("Dialing WebSocket",
		"function", "EstablishConnection")
	conn, resp, err := dialer.DialContext(ctx, wsURL, headers)
	if err != nil {
		if resp != nil {
			cm.client.logger.Error("WebSocket handshake failed",
				"function", "EstablishConnection",
				"status_code", resp.StatusCode,
				"headers", resp.Header)
		} else {
			cm.client.logger.Error("WebSocket dial failed",
				"function", "EstablishConnection",
				"error", err)
		}
		return fmt.Errorf("failed to establish WebSocket connection: %w", err)
	}
	cm.client.logger.Info("WebSocket dial successful",
		"function", "EstablishConnection")

	// Configure connection settings following legacy patterns
	conn.SetReadDeadline(time.Time{})  // No read timeout
	conn.SetWriteDeadline(time.Time{}) // No write timeout

	// Set close handler for graceful shutdown
	conn.SetCloseHandler(func(code int, text string) error {
		cm.client.logger.Info("WebSocket close received",
			"function", "SetCloseHandler",
			"code", code,
			"text", text)
		cm.handleConnectionClosed()
		return nil
	})

	// Connection established successfully
	cm.client.conn = conn
	cm.client.contextID = contextId // Use the contextId we generated earlier
	cm.client.lastSequenceNumber = 0
	cm.connected = true
	cm.reconnectAttempts = 0

	cm.client.logger.Info("WebSocket connection established successfully",
		"function", "EstablishConnection",
		"context_id", cm.client.contextID,
		"local_addr", conn.LocalAddr().String(),
		"remote_addr", conn.RemoteAddr().String())

	// NEW: Start separated reader/processor/reconnection goroutines
	// Following legacy broker_websocket.go breakthrough pattern - CRITICAL FIX

	// CRITICAL: Create NEW context right before starting goroutines
	// Following legacy startWebSocket pattern (broker_websocket.go:167)
	// This ensures goroutines use a fresh, non-canceled context
	cm.client.logger.Debug("Creating fresh context for goroutines",
		"function", "EstablishConnection")
	cm.client.ctx, cm.client.cancel = context.WithCancel(context.Background())

	cm.client.logger.Info("Starting goroutines",
		"function", "EstablishConnection")

	// Start reader goroutine (ONLY reads from WebSocket)
	cm.client.logger.Debug("Starting reader goroutine",
		"function", "EstablishConnection")
	go cm.client.readMessages()

	// Start processor goroutine (handles messages and errors)
	cm.client.logger.Debug("Starting processor goroutine",
		"function", "EstablishConnection")
	go cm.client.processMessages()

	// CRITICAL: Check if reconnection handler goroutine is already running (singleton pattern)
	// Following legacy broker_websocket.go pattern - prevents duplicate handlers
	cm.client.reconnectionHandlerMu.Lock()
	if cm.client.reconnectionHandlerRunning {
		cm.client.reconnectionHandlerMu.Unlock()
		cm.client.logger.Debug("Reconnection handler already running",
			"function", "EstablishConnection")
	} else {
		cm.client.reconnectionHandlerMu.Unlock()
		cm.client.logger.Debug("Starting reconnection handler goroutine",
			"function", "EstablishConnection")
		go cm.client.handleReconnectionRequests()
	}

	// Start subscription monitoring (timeout detection)
	cm.client.logger.Debug("Starting subscription monitoring goroutine",
		"function", "EstablishConnection")
	go cm.startSubscriptionMonitoring()

	// Start token refresh timer - CRITICAL for keeping WebSocket alive
	// Following legacy broker_websocket.go pattern (line 165)
	cm.client.logger.Debug("Starting token refresh timer",
		"function", "EstablishConnection")
	timeleft := cm.client.startTokenRefreshTimer()
	cm.client.logger.Debug("Token refresh scheduled",
		"function", "EstablishConnection",
		"expires_in", timeleft)

	// If token has negative timeleft, then the token has expired
	if timeleft < 0 {
		cm.client.logger.Error("Token has expired",
			"function", "EstablishConnection")
		return fmt.Errorf("token has expired, refresh failed")
	}

	cm.client.logger.Info("All goroutines started successfully",
		"function", "EstablishConnection")
	return nil
}

// HandleConnectionError processes connection failures and triggers reconnection
func (cm *ConnectionManager) HandleConnectionError(err error) {
	cm.client.logger.Error("WebSocket connection error",
		"function", "HandleConnectionError",
		"error", err)

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

		cm.client.logger.Info("Reconnection attempt",
			"function", "reconnectWithBackoff",
			"attempt", cm.reconnectAttempts,
			"max_attempts", cm.maxReconnectAttempts,
			"delay", delay)

		// Wait before reconnection attempt
		select {
		case <-cm.client.ctx.Done():
			cm.client.logger.Info("Reconnection cancelled",
				"function", "reconnectWithBackoff",
				"reason", "context cancellation")
			return
		case <-time.After(delay):
			// Continue with reconnection attempt
		}

		// Attempt to reestablish connection
		if err := cm.EstablishConnection(cm.client.ctx); err != nil {
			cm.client.logger.Warn("Reconnection attempt failed",
				"function", "reconnectWithBackoff",
				"attempt", cm.reconnectAttempts,
				"error", err)
			continue
		}

		// Resubscribe to all previous subscriptions with new reference IDs
		if err := cm.client.subscriptionManager.HandleSubscriptions(nil); err != nil {
			cm.client.logger.Warn("Resubscription failed after reconnection",
				"function", "reconnectWithBackoff",
				"error", err)
			cm.handleConnectionClosed()
			continue
		}

		cm.client.logger.Info("WebSocket reconnection successful",
			"function", "reconnectWithBackoff")
		return
	}

	cm.client.logger.Error("Max reconnection attempts reached",
		"function", "reconnectWithBackoff",
		"max_attempts", cm.maxReconnectAttempts)
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
				cm.client.logger.Warn("All subscriptions timed out, triggering reconnect",
					"function", "startSubscriptionMonitoring",
					"timed_out_count", len(timedOut))
				select {
				case cm.client.reconnectionTrigger <- fmt.Errorf("all subscriptions timed out"):
					cm.client.logger.Debug("Reconnection request queued",
						"function", "startSubscriptionMonitoring")
				default:
					cm.client.logger.Debug("Reconnection already queued",
						"function", "startSubscriptionMonitoring")
				}
				return
			} else if len(timedOut) > 0 {
				// Partial timeout - attempt subscription reset via subscription manager
				cm.client.logger.Warn("Partial timeout detected",
					"function", "startSubscriptionMonitoring",
					"timed_out", len(timedOut),
					"total", totalSubscriptions)
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
	cm.client.logger.Info("Closing WebSocket connection",
		"function", "CloseConnection")

	if !cm.connected {
		cm.client.logger.Debug("Already closed (no-op)",
			"function", "CloseConnection")
		return nil // Already closed
	}

	if cm.client.conn != nil {
		cm.client.logger.Debug("Sending close message",
			"function", "CloseConnection")
		// Send close message
		err := cm.client.conn.WriteMessage(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
		)
		if err != nil {
			cm.client.logger.Warn("Error sending close message",
				"function", "CloseConnection",
				"error", err)
		} else {
			cm.client.logger.Debug("Close message sent",
				"function", "CloseConnection")
		}

		cm.client.logger.Debug("Closing TCP connection",
			"function", "CloseConnection")
		// Close connection
		err = cm.client.conn.Close()
		if err != nil {
			cm.client.logger.Warn("Error closing connection",
				"function", "CloseConnection",
				"error", err)
		} else {
			cm.client.logger.Debug("TCP connection closed",
				"function", "CloseConnection")
		}

		cm.client.conn = nil
	}

	cm.connected = false
	cm.reconnectAttempts = 0

	cm.client.logger.Info("WebSocket connection closed successfully",
		"function", "CloseConnection")
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
