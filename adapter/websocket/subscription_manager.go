package websocket

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SubscriptionManager handles WebSocket subscription lifecycle following Saxo streaming API
// Per documentation: Subscriptions are sent via HTTP POST, WebSocket is read-only
type SubscriptionManager struct {
	subscriptions  map[string]*Subscription
	subscriptionMu sync.RWMutex
	client         *SaxoWebSocketClient

	// HTTP client for subscription requests (WebSocket is read-only!)
	baseURL      string
	getAuthToken func() (string, error) // Function to get access token

	// NEW: Subscription reset protection (CRITICAL FIX)
	// Following legacy broker_websocket.go pattern to prevent reset storms
	subscriptionUpdateInProgress bool      // Flag to prevent concurrent resets
	lastSubscriptionResetTime    time.Time // Timestamp of last reset for throttling
}

// NewSubscriptionManager creates subscription manager following Saxo streaming API patterns
// baseURL: Saxo OpenAPI base URL (e.g., "https://gateway.saxobank.com/sim/openapi")
// getAuthToken: Function to retrieve current access token
func NewSubscriptionManager(client *SaxoWebSocketClient, baseURL string, getAuthToken func() (string, error)) *SubscriptionManager {
	return &SubscriptionManager{
		subscriptions: make(map[string]*Subscription),
		client:        client,
		baseURL:       baseURL,
		getAuthToken:  getAuthToken,
	}
}

// SubscribeToInstrumentPrices establishes price feed subscription following Saxo streaming API
// Per documentation: Subscriptions are sent via HTTP POST, NOT via WebSocket!
// Endpoint: POST /trade/v1/infoprices/subscriptions
func (sm *SubscriptionManager) SubscribeToInstrumentPrices(instruments []string) error {
	sm.client.logger.Println("===============================================")
	sm.client.logger.Printf("SubscribeToInstrumentPrices: Starting price subscription for %d instruments", len(instruments))
	sm.client.logger.Printf("  Instruments: %v", instruments)
	sm.client.logger.Println("===============================================")

	sm.subscriptionMu.Lock()
	defer sm.subscriptionMu.Unlock()

	// Get UICs for instruments
	sm.client.logger.Println("SubscribeToInstrumentPrices: Mapping instruments to UICs...")
	uics := sm.getUicsForInstruments(instruments)
	sm.client.logger.Printf("  Mapped UICs: %v", uics)

	if len(uics) == 0 {
		sm.client.logger.Printf("❌ SubscribeToInstrumentPrices: No valid UICs found for instruments: %v", instruments)
		sm.client.logger.Println("   Hint: Did you call RegisterInstruments() before subscribing?")
		return fmt.Errorf("no valid UICs found for instruments")
	}

	// Get WebSocket Context ID (already established during connection)
	contextId := sm.client.contextID
	if contextId == "" {
		return fmt.Errorf("WebSocket not connected - no context ID")
	}
	sm.client.logger.Printf("  Using WebSocket Context ID: %s", contextId)

	// Build Saxo streaming subscription following API documentation
	// Reference: https://www.developer.saxo/openapi/learn/streaming
	// CRITICAL: Saxo API requires UICs as comma-separated STRING, not array
	// Legacy pattern: "Uics": strings.Join(uics, ",")
	// Convert []int to []string, then join with commas
	uicStrings := make([]string, len(uics))
	for i, uic := range uics {
		uicStrings[i] = strconv.Itoa(uic)
	}

	// Generate human-readable reference ID following legacy pattern
	referenceId := generateHumanReadableID("prices")

	subscriptionReq := map[string]interface{}{
		"ContextId":   contextId,
		"ReferenceId": referenceId,
		"RefreshRate": 1000,
		"Arguments": map[string]interface{}{
			"Uics":      strings.Join(uicStrings, ","), // Must be string: "5027,2,4,8,..."
			"AssetType": "FxSpot",
		},
	}

	sm.client.logger.Printf("SubscribeToInstrumentPrices: Sending subscription via HTTP POST...")
	sm.client.logger.Printf("  Subscription request: %+v", subscriptionReq)

	// Send subscription request via HTTP POST (NOT WebSocket!)
	if err := sm.sendSubscriptionRequest("/trade/v1/infoprices/subscriptions", subscriptionReq); err != nil {
		sm.client.logger.Printf("❌ SubscribeToInstrumentPrices: Failed to send HTTP POST: %v", err)
		return fmt.Errorf("failed to send price subscription: %w", err)
	}
	sm.client.logger.Println("✅ SubscribeToInstrumentPrices: HTTP POST successful, subscription created")

	// Track subscription state for reconnection logic
	subscription := &Subscription{
		ContextId:    contextId,
		ReferenceId:  referenceId,
		State:        "Active",
		SubscribedAt: time.Now(),
		Arguments:    subscriptionReq["Arguments"].(map[string]interface{}),
	}

	sm.subscriptions["price_feed"] = subscription

	sm.client.logger.Println("===============================================")
	sm.client.logger.Printf("✅ SubscribeToInstrumentPrices: Successfully subscribed to prices")
	sm.client.logger.Printf("   Instruments: %v", instruments)
	sm.client.logger.Printf("   UICs: %v", uics)
	sm.client.logger.Printf("   Context ID: %s", contextId)
	sm.client.logger.Println("===============================================")

	return nil
}

// SubscribeToOrderUpdates establishes order status subscription for signal management
// Per Saxo API: POST /port/v1/orders/subscriptions
func (sm *SubscriptionManager) SubscribeToOrderUpdates(clientKey string) error {
	sm.subscriptionMu.Lock()
	defer sm.subscriptionMu.Unlock()

	// Get WebSocket Context ID
	contextId := sm.client.contextID
	if contextId == "" {
		return fmt.Errorf("WebSocket not connected - no context ID")
	}

	// Saxo order streaming subscription following API documentation
	subscriptionReq := map[string]interface{}{
		"ContextId":   contextId,
		"ReferenceId": "order_updates",
		"RefreshRate": 1000,
		"Format":      "application/json",
		"Arguments": map[string]interface{}{
			"ClientKey": clientKey,
		},
	}

	if err := sm.sendSubscriptionRequest("/port/v1/orders/subscriptions", subscriptionReq); err != nil {
		return fmt.Errorf("failed to send order subscription: %w", err)
	}

	subscription := &Subscription{
		ContextId:    contextId,
		ReferenceId:  "order_updates",
		State:        "Active",
		SubscribedAt: time.Now(),
		Arguments:    subscriptionReq["Arguments"].(map[string]interface{}),
	}

	sm.subscriptions["order_updates"] = subscription
	sm.client.logger.Println("✅ Subscribed to order status updates via HTTP POST")

	return nil
}

// SubscribeToPortfolioUpdates establishes balance and margin subscription
// Per Saxo API: POST /port/v1/balances/subscriptions
func (sm *SubscriptionManager) SubscribeToPortfolioUpdates(clientKey string) error {
	sm.subscriptionMu.Lock()
	defer sm.subscriptionMu.Unlock()

	// Get WebSocket Context ID
	contextId := sm.client.contextID
	if contextId == "" {
		return fmt.Errorf("WebSocket not connected - no context ID")
	}

	// Portfolio balance subscription following API documentation
	subscriptionReq := map[string]interface{}{
		"ContextId":   contextId,
		"ReferenceId": "portfolio_balance",
		"RefreshRate": 1000,
		"Format":      "application/json",
		"Arguments": map[string]interface{}{
			"ClientKey": clientKey,
		},
	}

	if err := sm.sendSubscriptionRequest("/port/v1/balances/subscriptions", subscriptionReq); err != nil {
		return fmt.Errorf("failed to send portfolio subscription: %w", err)
	}

	subscription := &Subscription{
		ContextId:    contextId,
		ReferenceId:  "portfolio_balance",
		State:        "Active",
		SubscribedAt: time.Now(),
		Arguments:    subscriptionReq["Arguments"].(map[string]interface{}),
	}

	sm.subscriptions["portfolio_balance"] = subscription
	sm.client.logger.Println("✅ Subscribed to portfolio balance updates via HTTP POST")

	return nil
}

// sendSubscriptionRequest sends HTTP POST subscription request following Saxo streaming API
// Per documentation: Subscriptions are ALWAYS sent via HTTP POST, never via WebSocket
// Reference: https://www.developer.saxo/openapi/learn/streaming#Subscription-example
func (sm *SubscriptionManager) sendSubscriptionRequest(endpoint string, subscriptionReq map[string]interface{}) error {
	// Get access token
	token, err := sm.getAuthToken()
	if err != nil {
		return fmt.Errorf("failed to get access token: %w", err)
	}

	// Marshal request body
	reqBody, err := json.Marshal(subscriptionReq)
	if err != nil {
		return fmt.Errorf("failed to marshal subscription request: %w", err)
	}

	sm.client.logger.Printf("sendSubscriptionRequest: POST %s", endpoint)
	sm.client.logger.Printf("  Body: %s", string(reqBody))

	// Create HTTP POST request
	url := sm.baseURL + endpoint
	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(reqBody))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set headers per Saxo API requirements
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	// Get HTTP client from auth client (for TLS configuration in tests)
	httpClient, err := sm.client.authClient.GetHTTPClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to get HTTP client: %w", err)
	}

	// Send request
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check response status - Saxo returns 201 Created on successful subscription
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		sm.client.logger.Printf("❌ Subscription failed: Status=%d, Body=%s", resp.StatusCode, string(bodyBytes))
		return fmt.Errorf("subscription request failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	// Log success
	sm.client.logger.Printf("✅ Subscription created successfully: Status=%d", resp.StatusCode)

	// Note: The Location header contains the subscription resource URL for deletion
	// We should store this for later deletion, but for now we just log it
	location := resp.Header.Get("Location")
	if location != "" {
		sm.client.logger.Printf("   Location: %s", location)
	}

	return nil
}

// ResubscribeAll handles reconnection subscription restoration following Saxo streaming API
// Per documentation: Subscriptions sent via HTTP POST, not WebSocket writes
func (sm *SubscriptionManager) ResubscribeAll() error {
	sm.subscriptionMu.RLock()
	defer sm.subscriptionMu.RUnlock()

	sm.client.logger.Printf("Resubscribing to %d active subscriptions via HTTP POST", len(sm.subscriptions))

	// Reestablish all active subscriptions - critical for WebSocket lifecycle
	for refId, subscription := range sm.subscriptions {
		// Build subscription request with new context ID
		subscriptionReq := map[string]interface{}{
			"ContextId":   sm.client.contextID,
			"ReferenceId": subscription.ReferenceId,
			"RefreshRate": 1000,
			"Format":      "application/json",
			"Arguments":   subscription.Arguments,
		}

		// Determine endpoint based on reference ID
		var endpoint string
		switch refId {
		case "price_feed":
			endpoint = "/trade/v1/infoprices/subscriptions"
		case "order_updates":
			endpoint = "/port/v1/orders/subscriptions"
		case "portfolio_balance":
			endpoint = "/port/v1/balances/subscriptions"
		default:
			sm.client.logger.Printf("❌ Unknown subscription type: %s", refId)
			continue
		}

		if err := sm.sendSubscriptionRequest(endpoint, subscriptionReq); err != nil {
			return fmt.Errorf("failed to resubscribe %s: %w", refId, err)
		}

		// Update subscription state
		subscription.State = "Active"
		subscription.SubscribedAt = time.Now()
	}

	return nil
}

// UpdateSubscriptionState handles subscription confirmation messages
func (sm *SubscriptionManager) UpdateSubscriptionState(contextId, state string) {
	sm.subscriptionMu.Lock()
	defer sm.subscriptionMu.Unlock()

	if subscription, exists := sm.subscriptions[contextId]; exists {
		subscription.State = state
		sm.client.logger.Printf("Subscription %s state updated: %s", contextId, state)
	}
}

// RemoveSubscription cleans up subscription following WebSocket shutdown patterns
func (sm *SubscriptionManager) RemoveSubscription(contextId string) {
	sm.subscriptionMu.Lock()
	defer sm.subscriptionMu.Unlock()

	delete(sm.subscriptions, contextId)
	sm.client.logger.Printf("Removed subscription: %s", contextId)
}

// GetActiveSubscriptions returns current subscription state for monitoring
func (sm *SubscriptionManager) GetActiveSubscriptions() map[string]*Subscription {
	sm.subscriptionMu.RLock()
	defer sm.subscriptionMu.RUnlock()

	// Return copy to prevent external modification
	result := make(map[string]*Subscription)
	for k, v := range sm.subscriptions {
		result[k] = &Subscription{
			ContextId:    v.ContextId,
			ReferenceId:  v.ReferenceId,
			State:        v.State,
			SubscribedAt: v.SubscribedAt,
			Arguments:    v.Arguments,
		}
	}

	return result
}

// HandleSubscriptionReset handles subscription reset requests from Saxo
// Following legacy handleSubscriptionsResets() pattern with CRITICAL protection logic
func (sm *SubscriptionManager) HandleSubscriptionReset(targetReferenceIds []string) error {
	sm.subscriptionMu.Lock()

	// CRITICAL: Check if reconnection is in progress (skip reset, fresh subscriptions coming)
	// Following legacy broker_websocket.go pattern
	sm.client.reconnectMu.Lock()
	if sm.client.reconnectInProgress {
		sm.client.reconnectMu.Unlock()
		sm.subscriptionMu.Unlock()
		sm.client.logger.Println("HandleSubscriptionReset: Skipping reset (reconnection in progress)")
		return nil
	}
	sm.client.reconnectMu.Unlock()

	// CRITICAL: Check if reset already in progress
	if sm.subscriptionUpdateInProgress {
		sm.subscriptionMu.Unlock()
		sm.client.logger.Println("HandleSubscriptionReset: Reset already in progress, skipping")
		return nil
	}

	// CRITICAL: Throttle full resets (30s cooldown) to prevent cascading reset storms
	// Increased from 10s to 30s following legacy pattern after debugging
	if len(targetReferenceIds) == 0 && time.Since(sm.lastSubscriptionResetTime) < 30*time.Second {
		sm.subscriptionMu.Unlock()
		sm.client.logger.Println("HandleSubscriptionReset: Recent full reset detected, skipping to avoid storm")
		return nil
	}

	// Mark reset in progress
	sm.subscriptionUpdateInProgress = true
	sm.subscriptionMu.Unlock()

	// Perform reset asynchronously to avoid blocking reader goroutine
	go func(timedOutSubs []string) {
		defer func() {
			sm.subscriptionMu.Lock()
			sm.subscriptionUpdateInProgress = false
			// CRITICAL: Update timestamp AFTER work completes, not before starting
			sm.lastSubscriptionResetTime = time.Now()
			sm.subscriptionMu.Unlock()
		}()

		if len(timedOutSubs) == 0 {
			// Full reset requested
			sm.client.logger.Println("HandleSubscriptionReset: Full reset triggered")

			// Full reset should trigger reconnection instead
			select {
			case sm.client.reconnectionTrigger <- fmt.Errorf("subscription reset requested"):
				sm.client.logger.Println("HandleSubscriptionReset: Reconnection request queued")
			default:
				sm.client.logger.Println("HandleSubscriptionReset: Reconnection already queued")
			}
		} else {
			// Partial reset
			sm.client.logger.Printf("HandleSubscriptionReset: Resetting specific subscriptions: %v", timedOutSubs)
			sm.subscriptionMu.Lock()
			if err := sm.resetSpecificSubscriptions(timedOutSubs); err != nil {
				sm.client.logger.Printf("HandleSubscriptionReset: resetSpecificSubscriptions failed: %v", err)
			}
			sm.subscriptionMu.Unlock()
		}
	}(targetReferenceIds)

	return nil
}

// resetAllSubscriptions resets all active subscriptions
func (sm *SubscriptionManager) resetAllSubscriptions() error {
	for oldRef, subscription := range sm.subscriptions {
		// Generate new ID with subscription type from endpoint
		subscriptionType := getSubscriptionTypeFromPath(subscription.EndpointPath)
		newRef := generateHumanReadableID(subscriptionType)
		if err := sm.resetSubscription(oldRef, newRef, subscription); err != nil {
			sm.client.logger.Printf("Failed to reset subscription %s: %v", oldRef, err)
			continue
		}
		// Sleep between resets to avoid overwhelming the server
		time.Sleep(10 * time.Second)
	}
	return nil
}

// resetSpecificSubscriptions resets only specified subscriptions
func (sm *SubscriptionManager) resetSpecificSubscriptions(targetReferenceIds []string) error {
	for _, targetId := range targetReferenceIds {
		subscription, exists := sm.subscriptions[targetId]
		if !exists {
			sm.client.logger.Printf("Subscription not found for reference ID: %s", targetId)
			continue
		}

		// Generate new ID with subscription type from endpoint
		subscriptionType := getSubscriptionTypeFromPath(subscription.EndpointPath)
		newRef := generateHumanReadableID(subscriptionType)
		if err := sm.resetSubscription(targetId, newRef, subscription); err != nil {
			sm.client.logger.Printf("Failed to reset subscription %s: %v", targetId, err)
			continue
		}
		// Sleep between resets
		time.Sleep(10 * time.Second)
	}
	return nil
}

// resetSubscription resets a single subscription
func (sm *SubscriptionManager) resetSubscription(oldRef, newRef string, subscription *Subscription) error {
	// Build reset subscription message
	resetMessage := map[string]interface{}{
		"ContextId":          sm.client.contextID,
		"ReferenceId":        newRef,
		"ReplaceReferenceId": oldRef,
		"Arguments":          subscription.Arguments,
	}

	// Send reset request
	if err := sm.client.conn.WriteJSON(resetMessage); err != nil {
		return fmt.Errorf("failed to send reset request: %w", err)
	}

	// Update subscription tracking
	newSubscription := &Subscription{
		ContextId:           sm.client.contextID,
		ReferenceId:         newRef,
		State:               "Resetting",
		SubscribedAt:        time.Now(),
		Arguments:           subscription.Arguments,
		SubscriptionMessage: resetMessage,
		EndpointPath:        subscription.EndpointPath,
	}

	sm.subscriptions[newRef] = newSubscription
	delete(sm.subscriptions, oldRef)

	// Transfer callback handler
	if handler, exists := sm.client.GetCallbackHandler(oldRef); exists {
		sm.client.RegisterCallbackHandler(newRef, handler)
		sm.client.UnregisterCallbackHandler(oldRef)
	}

	sm.client.logger.Printf("Reset subscription: %s -> %s", oldRef, newRef)
	return nil
}

// getUicsForInstruments extracts UICs from ticker list using dynamic mapping
// CRITICAL FIX: No more hardcoded UICs - uses RegisterInstruments() mapping from fx.json
func (sm *SubscriptionManager) getUicsForInstruments(instruments []string) []int {
	sm.client.mappingMu.RLock()
	defer sm.client.mappingMu.RUnlock()

	var uics []int
	for _, ticker := range instruments {
		if uic, exists := sm.client.tickerToUic[ticker]; exists {
			uics = append(uics, uic)
		} else {
			sm.client.logger.Printf("Warning: No UIC mapping for ticker %s (RegisterInstruments not called?)", ticker)
		}
	}

	if len(uics) > 0 {
		sm.client.logger.Printf("SubscriptionManager: Mapped %d tickers to UICs: %v", len(uics), uics)
	}

	return uics
}

// SubscribeToSessionEvents establishes session event subscription
// Following legacy StartSessionEventSubscription pattern
// This monitors session state and can trigger session capability upgrades
func (sm *SubscriptionManager) SubscribeToSessionEvents(sessionHandler func(payload []byte)) error {
	sm.subscriptionMu.Lock()
	defer sm.subscriptionMu.Unlock()

	// Generate human-readable reference ID following legacy pattern
	referenceId := generateHumanReadableID("session")
	contextId := sm.client.contextID

	// Saxo session event subscription
	subscriptionReq := map[string]interface{}{
		"ContextId":   contextId,
		"ReferenceId": referenceId,
		"RefreshRate": 1000,
	}

	// Send subscription request
	if err := sm.client.conn.WriteJSON(subscriptionReq); err != nil {
		return fmt.Errorf("failed to send session subscription: %w", err)
	}

	// Track subscription
	subscription := &Subscription{
		ContextId:           contextId,
		ReferenceId:         referenceId,
		State:               "Subscribing",
		SubscribedAt:        time.Now(),
		SubscriptionMessage: subscriptionReq,
		EndpointPath:        "/root/v1/sessions/events/subscriptions/active",
	}

	sm.subscriptions[referenceId] = subscription

	// Register callback handler for session events
	sm.client.RegisterCallbackHandler(referenceId, sessionHandler)

	sm.client.logger.Println("Subscribed to session events")
	return nil
}
