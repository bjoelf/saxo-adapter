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

// Saxo streaming API endpoint constants
// Per documentation: https://www.developer.saxo/openapi/learn/streaming
const (
	EndpointPrices        = "/trade/v1/infoprices/subscriptions"
	EndpointOrders        = "/port/v1/orders/subscriptions"
	EndpointBalance       = "/port/v1/balances/subscriptions"
	EndpointSessionEvents = "/root/v1/sessions/events/subscriptions/active"
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
// assetType: "FxSpot", "ContractFutures", "CfdOnFutures", etc.
func (sm *SubscriptionManager) SubscribeToInstrumentPrices(instruments []string, assetType string) error {
	sm.client.logger.Println("===============================================")
	sm.client.logger.Printf("SubscribeToInstrumentPrices: Starting price subscription for %d instruments (AssetType: %s)", len(instruments), assetType)
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
	feedReferenceId := assetType + "prices"
	referenceId := generateHumanReadableID(feedReferenceId)

	subscriptionReq := map[string]interface{}{
		"ContextId":   contextId,
		"ReferenceId": referenceId,
		"RefreshRate": 1000,
		"Arguments": map[string]interface{}{
			"Uics":      strings.Join(uicStrings, ","), // Must be string: "5027,2,4,8,..."
			"AssetType": assetType,                     // Use parameter from caller (FxSpot, ContractFutures, etc.)
		},
	}

	sm.client.logger.Printf("SubscribeToInstrumentPrices: Sending subscription via HTTP POST...")
	sm.client.logger.Printf("  Subscription request: %+v", subscriptionReq)

	// Send subscription request via HTTP POST (NOT WebSocket!)
	if err := sm.sendSubscriptionRequest(EndpointPrices, subscriptionReq); err != nil {
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
		EndpointPath: EndpointPrices,
	}

	sm.subscriptions["price_feed"] = subscription

	sm.client.logger.Println("===============================================")
	sm.client.logger.Printf("✅ SubscribeToInstrumentPrices: Successfully subscribed to prices")
	sm.client.logger.Printf("✅ ReferenceId: %s", referenceId)
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

	if err := sm.sendSubscriptionRequest(EndpointOrders, subscriptionReq); err != nil {
		return fmt.Errorf("failed to send order subscription: %w", err)
	}

	subscription := &Subscription{
		ContextId:    contextId,
		ReferenceId:  "order_updates",
		State:        "Active",
		SubscribedAt: time.Now(),
		Arguments:    subscriptionReq["Arguments"].(map[string]interface{}),
		EndpointPath: EndpointOrders,
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

	if err := sm.sendSubscriptionRequest(EndpointBalance, subscriptionReq); err != nil {
		return fmt.Errorf("failed to send portfolio subscription: %w", err)
	}

	subscription := &Subscription{
		ContextId:    contextId,
		ReferenceId:  "portfolio_balance",
		State:        "Active",
		SubscribedAt: time.Now(),
		Arguments:    subscriptionReq["Arguments"].(map[string]interface{}),
		EndpointPath: EndpointBalance,
	}

	sm.subscriptions["portfolio_balance"] = subscription
	sm.client.logger.Println("✅ Subscribed to portfolio balance updates via HTTP POST")

	return nil
}

// SubscribeToSessionEvents establishes session event subscription for connection robustness
// Per Saxo API: POST /root/v1/sessions/events/subscriptions/active
// Reference: pivot-web/broker/broker_websocket.go:63 - sessionsSubscriptionPath
func (sm *SubscriptionManager) SubscribeToSessionEvents() error {
	sm.subscriptionMu.Lock()
	defer sm.subscriptionMu.Unlock()

	// Get WebSocket Context ID
	contextId := sm.client.contextID
	if contextId == "" {
		return fmt.Errorf("WebSocket not connected - no context ID")
	}

	// Generate human-readable reference ID following legacy pattern
	referenceId := generateHumanReadableID("session_events")

	// Session events subscription following API documentation
	// This subscription has minimal arguments - monitors connection health
	subscriptionReq := map[string]interface{}{
		"ContextId":   contextId,
		"ReferenceId": referenceId,
		"RefreshRate": 1000,
		"Format":      "application/json",
	}

	sm.client.logger.Printf("SubscribeToSessionEvents: Sending subscription via HTTP POST...")
	sm.client.logger.Printf("  Subscription request: %+v", subscriptionReq)

	if err := sm.sendSubscriptionRequest(EndpointSessionEvents, subscriptionReq); err != nil {
		sm.client.logger.Printf("❌ SubscribeToSessionEvents: Failed to send HTTP POST: %v", err)
		return fmt.Errorf("failed to send session events subscription: %w", err)
	}

	subscription := &Subscription{
		ContextId:    contextId,
		ReferenceId:  referenceId,
		State:        "Active",
		SubscribedAt: time.Now(),
		Arguments:    map[string]interface{}{}, // No special arguments for session events
		EndpointPath: EndpointSessionEvents,
	}

	sm.subscriptions["session_events"] = subscription
	sm.client.logger.Println("✅ Subscribed to session events via HTTP POST")

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

// generateNewReferenceId creates a new reference ID by replacing the timestamp suffix
// This preserves asset type prefixes like "FxSpotprices", "ContractFuturesprices", etc.
// Old: FxSpotprices-20251220-152651 -> New: FxSpotprices-20251220-153045
func (sm *SubscriptionManager) generateNewReferenceId(oldReferenceId string) string {
	if len(oldReferenceId) > 15 {
		// Extract prefix (everything except last 15 chars) and add new timestamp
		prefix := oldReferenceId[:len(oldReferenceId)-15]
		newTimestamp := time.Now().Format("20060102-150405")
		return prefix + newTimestamp
	}
	// Fallback for malformed IDs: append timestamp
	newTimestamp := time.Now().Format("20060102-150405")
	return fmt.Sprintf("%s-%s", oldReferenceId, newTimestamp)
}

// HandleSubscriptions handles subscription restoration/reset following Saxo streaming API
// Per documentation: Subscriptions MUST be sent via HTTP POST, NOT WebSocket writes
// Reference: https://www.developer.saxo/openapi/learn/streaming
//
// Parameters:
//   - keepCurrentReferenceIds: If true, reuses existing reference IDs; if false, generates new ones
//   - targetReferenceIds: Specific reference IDs to resubscribe (empty = all subscriptions)
//     CRITICAL: These are actual Saxo reference IDs (e.g., "FxSpotprices-20251220-145408"),
//     NOT internal subscription type keys (e.g., "price_feed")
//
// Usage scenarios:
//   - Full reconnection: HandleSubscriptions(false, nil) - new IDs, all subscriptions
//   - Subscription reset: HandleSubscriptions(false, []string{"FxSpotprices-20251220-145408"}) - new ID for specific subscription
//   - Partial refresh: HandleSubscriptions(true, []string{"order_updates"}) - keep ID, refresh specific subscription
func (sm *SubscriptionManager) HandleSubscriptions(keepCurrentReferenceIds bool, targetReferenceIds []string) error {
	sm.subscriptionMu.Lock()
	defer sm.subscriptionMu.Unlock()

	// Determine which subscriptions to resubscribe
	var subsToProcess map[string]*Subscription
	if len(targetReferenceIds) == 0 {
		// Resubscribe all subscriptions
		subsToProcess = sm.subscriptions
		sm.client.logger.Printf("ResubscribeAll: Resubscribing to ALL %d subscriptions via HTTP POST (keepIDs=%v)", len(sm.subscriptions), keepCurrentReferenceIds)
	} else {
		// Resubscribe only specific subscriptions by matching ReferenceId field
		// CRITICAL: targetReferenceIds contains actual Saxo reference IDs (e.g., "FxSpotprices-20251220-145408")
		subsToProcess = make(map[string]*Subscription)
		for mapKey, sub := range sm.subscriptions {
			for _, targetRefId := range targetReferenceIds {
				if sub.ReferenceId == targetRefId {
					subsToProcess[mapKey] = sub
					sm.client.logger.Printf("  Matched ReferenceId '%s' to subscription type '%s'", targetRefId, mapKey)
					break
				}
			}
		}

		// Log any unmatched reference IDs
		for _, targetRefId := range targetReferenceIds {
			found := false
			for _, sub := range subsToProcess {
				if sub.ReferenceId == targetRefId {
					found = true
					break
				}
			}
			if !found {
				sm.client.logger.Printf("⚠️ ResubscribeAll: ReferenceId '%s' not found in active subscriptions", targetRefId)
			}
		}

		sm.client.logger.Printf("ResubscribeAll: Resubscribing to %d specific subscriptions via HTTP POST (keepIDs=%v)", len(subsToProcess), keepCurrentReferenceIds)
	}

	if len(subsToProcess) == 0 {
		sm.client.logger.Println("ResubscribeAll: No subscriptions to resubscribe")
		return nil
	}

	// Reestablish subscriptions via HTTP POST
	for refId, subscription := range subsToProcess {
		oldReferenceId := subscription.ReferenceId
		var newReferenceId string
		var subscriptionReq map[string]interface{}

		if keepCurrentReferenceIds {
			// Keep existing reference ID
			newReferenceId = oldReferenceId
			subscriptionReq = map[string]interface{}{
				"ContextId":   sm.client.contextID,
				"ReferenceId": newReferenceId,
				"RefreshRate": 1000,
				"Format":      "application/json",
				"Arguments":   subscription.Arguments,
			}
			sm.client.logger.Printf("  Resubscribing '%s' (keeping ID: %s)", refId, newReferenceId)
		} else {
			// Generate new reference ID by replacing timestamp
			newReferenceId = sm.generateNewReferenceId(oldReferenceId)
			subscriptionReq = map[string]interface{}{
				"ContextId":          sm.client.contextID,
				"ReferenceId":        newReferenceId,
				"ReplaceReferenceId": oldReferenceId, // Atomic replacement per Saxo docs
				"RefreshRate":        1000,
				"Format":             "application/json",
				"Arguments":          subscription.Arguments,
			}
			sm.client.logger.Printf("  Resubscribing '%s' (old: %s -> new: %s, ReplaceReferenceId: %s)",
				refId, oldReferenceId, newReferenceId, oldReferenceId)
		}

		// Use stored endpoint path (single source of truth)
		endpoint := subscription.EndpointPath
		if endpoint == "" {
			sm.client.logger.Printf("❌ Subscription '%s' has no endpoint path stored, skipping", refId)
			continue
		}

		// Send HTTP POST subscription request (correct per Saxo API documentation)
		if err := sm.sendSubscriptionRequest(endpoint, subscriptionReq); err != nil {
			return fmt.Errorf("failed to resubscribe %s: %w", refId, err)
		}

		// Update subscription tracking
		if !keepCurrentReferenceIds && newReferenceId != subscription.ReferenceId {
			// Reference ID changed - update tracking
			subscription.ReferenceId = newReferenceId
			subscription.EndpointPath = endpoint

			// Update subscription map (if refId was the ReferenceId, update key)
			if refId == subscription.ReferenceId {
				sm.subscriptions[newReferenceId] = subscription
				delete(sm.subscriptions, refId)
			}
		}

		// Update subscription state
		subscription.State = "Active"
		subscription.SubscribedAt = time.Now()

		// Add small delay between resubscriptions to avoid overwhelming server
		// Only needed when processing multiple subscriptions during reset
		if !keepCurrentReferenceIds && len(subsToProcess) > 1 {
			time.Sleep(500 * time.Millisecond)
		}
	}

	sm.client.logger.Printf("✅ ResubscribeAll: Successfully resubscribed %d subscriptions", len(subsToProcess))
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
			// Partial reset - resubscribe specific subscriptions with new IDs
			sm.client.logger.Printf("HandleSubscriptionReset: Resetting specific subscriptions: %v", timedOutSubs)

			// Use ResubscribeAll with specific targets and generate new IDs
			// Following Saxo API documentation: subscriptions via HTTP POST, not WebSocket writes
			if err := sm.HandleSubscriptions(false, timedOutSubs); err != nil {
				sm.client.logger.Printf("HandleSubscriptionReset: ResubscribeAll failed: %v", err)
			}
		}
	}(targetReferenceIds)

	return nil
}

// getUicsForInstruments extracts UICs from ticker list using dynamic mapping
// CRITICAL FIX: No more hardcoded UICs - uses RegisterInstruments() mapping from fx.json
// Also supports direct UIC strings (e.g., "21", "31") for simple examples
// When UICs are passed directly, creates bidirectional mapping: UIC → "21" (ticker is UIC string)
func (sm *SubscriptionManager) getUicsForInstruments(instruments []string) []int {
	sm.client.mappingMu.Lock()
	defer sm.client.mappingMu.Unlock()

	// Use map to deduplicate UICs (CRITICAL FIX for Saxo API requirement)
	// Saxo API requires: "The UICs in the list must be unique"
	uicMap := make(map[int]bool)

	for _, instrument := range instruments {
		// First, try to parse as direct UIC (numeric string)
		if uic, err := strconv.Atoi(instrument); err == nil {
			uicMap[uic] = true

			// Create reverse mapping: UIC → ticker (ticker is the UIC string itself)
			// This allows price messages to be converted without predefined mappings
			sm.client.uicToTicker[uic] = instrument
			sm.client.tickerToUic[instrument] = uic

			sm.client.logger.Printf("  Using direct UIC: %s -> %d (created mapping)", instrument, uic)
		} else if uic, exists := sm.client.tickerToUic[instrument]; exists {
			// Otherwise, look up ticker in mapping
			uicMap[uic] = true
			sm.client.logger.Printf("  Mapped ticker: %s -> %d", instrument, uic)
		} else {
			sm.client.logger.Printf("Warning: No UIC mapping for ticker %s (RegisterInstruments not called?)", instrument)
		}
	}

	// Convert map to slice (deduplicated UICs)
	uics := make([]int, 0, len(uicMap))
	for uic := range uicMap {
		uics = append(uics, uic)
	}

	if len(uics) > 0 {
		sm.client.logger.Printf("SubscriptionManager: Mapped %d instruments to %d unique UICs: %v", len(instruments), len(uics), uics)
	}

	return uics
}
