package saxo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

// CreateBrokerServices creates Saxo broker client with injected auth client
// Following dependency injection pattern like NewSaxoWebSocketClient()
func CreateBrokerServices(authClient AuthClient, logger *log.Logger) (BrokerClient, error) {
	// Start authentication keeper if already authenticated (legacy WebSocket lifecycle pattern)
	if authClient.IsAuthenticated() {
		provider := os.Getenv("PROVIDER")
		if provider == "" {
			provider = "saxo"
		}
		authClient.StartAuthenticationKeeper(provider)
		logger.Println("Authentication keeper started for token refresh")
	} else {
		logger.Println("Not authenticated - use /broker/login to authenticate")
	}

	// Create broker client (adapter layer)
	brokerClient := NewSaxoBrokerClient(authClient, authClient.GetBaseURL(), logger)

	return brokerClient, nil
}

// cachedHistoricalData represents cached market data for an instrument
type cachedHistoricalData struct {
	Data      []HistoricalDataPoint
	Timestamp time.Time
}

// SaxoBrokerClient implements BrokerClient interface
// All Saxo-specific details are handled internally
type SaxoBrokerClient struct {
	authClient AuthClient
	baseURL    string
	logger     *log.Logger

	// Historical data cache following legacy SinglePivotHistory caching pattern
	historyCache map[string]*cachedHistoricalData
	cacheMutex   sync.RWMutex
	cacheExpiry  time.Duration // Default: 1 hour like legacy system
}

// NewSaxoBrokerClient creates a new Saxo broker client
func NewSaxoBrokerClient(authClient AuthClient, baseURL string, logger *log.Logger) *SaxoBrokerClient {
	return &SaxoBrokerClient{
		authClient:   authClient,
		baseURL:      baseURL,
		logger:       logger,
		historyCache: make(map[string]*cachedHistoricalData),
		cacheExpiry:  1 * time.Hour, // Following legacy 1-hour cache pattern
	}
}

// PlaceOrder implements BrokerClient.PlaceOrder
// Converts generic OrderRequest to Saxo-specific format internally
func (sbc *SaxoBrokerClient) PlaceOrder(ctx context.Context, req OrderRequest) (*OrderResponse, error) {
	sbc.logger.Printf("PlaceOrder: Processing order for %s", req.Instrument.Ticker)

	// Check authentication
	if !sbc.authClient.IsAuthenticated() {
		return nil, fmt.Errorf("not authenticated with broker")
	}

	// Convert generic OrderRequest to Saxo-specific format
	saxoReq, err := sbc.convertToSaxoOrder(req)
	if err != nil {
		return nil, fmt.Errorf("failed to convert order request: %w", err)
	}

	// Marshal request body
	reqBody, err := json.Marshal(saxoReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST",
		sbc.baseURL+"/trade/v2/orders", bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set headers
	httpReq.Header.Set("Content-Type", "application/json")
	// Execute request with OAuth2 auto-refresh
	resp, err := sbc.doRequest(ctx, httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	// Handle response
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return nil, sbc.handleErrorResponse(resp)
	}

	// Parse success response
	var saxoResp SaxoOrderResponse
	if err := json.NewDecoder(resp.Body).Decode(&saxoResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Convert Saxo response to generic format
	genericResp := sbc.convertFromSaxoResponse(saxoResp)

	sbc.logger.Printf("Order placed successfully: OrderID=%s, Status=%s",
		genericResp.OrderID, genericResp.Status)

	return genericResp, nil
}

// DeleteOrder implements BrokerClient.DeleteOrder
func (sbc *SaxoBrokerClient) DeleteOrder(ctx context.Context, orderID string) error {
	sbc.logger.Printf("DeleteOrder: Cancelling order %s", orderID)

	// Check authentication
	if !sbc.authClient.IsAuthenticated() {
		return fmt.Errorf("not authenticated with broker")
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "DELETE",
		sbc.baseURL+"/trade/v2/orders/"+orderID, nil)
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set headers
	// Execute request with OAuth2 auto-refresh
	resp, err := sbc.doRequest(ctx, httpReq)
	if err != nil {
		return fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	// Handle response
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return sbc.handleErrorResponse(resp)
	}

	sbc.logger.Printf("Order cancelled successfully: %s", orderID)
	return nil
}

// CancelOrder implements BrokerClient.CancelOrder
// Uses Saxo API: DELETE /trade/v2/orders/{OrderIds}?AccountKey={AccountKey}
func (sbc *SaxoBrokerClient) CancelOrder(ctx context.Context, req CancelOrderRequest) error {
	sbc.logger.Printf("CancelOrder: Cancelling order %s for account %s", req.OrderID, req.AccountKey)

	// Check authentication
	if !sbc.authClient.IsAuthenticated() {
		return fmt.Errorf("not authenticated with broker")
	}

	// Build URL with query parameters following Saxo API documentation
	url := fmt.Sprintf("%s/trade/v2/orders/%s?AccountKey=%s",
		sbc.baseURL, req.OrderID, req.AccountKey)

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set headers
	// Execute request with OAuth2 auto-refresh
	resp, err := sbc.doRequest(ctx, httpReq)
	if err != nil {
		return fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	// Handle response - 200/204 = success
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNoContent {
		sbc.logger.Printf("Order cancelled successfully: %s", req.OrderID)
		return nil
	}

	// Handle error - check if order was already filled
	return sbc.handleErrorResponse(resp)
}

// ClosePosition implements BrokerClient.ClosePosition
// Closes position by placing an opposite market order
//
// For accounts with Real-time (Intraday) netting: Opposing positions are netted immediately
// For accounts with End-of-Day netting: Positions are netted overnight
//
// Note: Real-time netting does NOT support relating orders to positions.
// Therefore we use a simple opposite market order which works for both netting modes.
// Reference: https://www.developer.saxo/openapi/learn/fifo-real-time-netting
func (sbc *SaxoBrokerClient) ClosePosition(ctx context.Context, req ClosePositionRequest) (*OrderResponse, error) {
	sbc.logger.Printf("ClosePosition: Closing position %s (NetPositionID: %s) for account %s",
		req.PositionID, req.NetPositionID, req.AccountKey)

	// Check authentication
	if !sbc.authClient.IsAuthenticated() {
		return nil, fmt.Errorf("not authenticated with broker")
	}

	// Determine opposite direction to close position
	// If position is Buy (long), we need to Sell to close
	// If position is Sell (short), we need to Buy to close
	oppositeSide := "Sell"
	if req.BuySell == "Sell" {
		oppositeSide = "Buy"
	}

	// Build simple market order to close position
	// This works for both real-time and end-of-day netting
	closeOrder := SaxoOrderRequest{
		AccountKey:  req.AccountKey,
		Uic:         req.Uic,
		AssetType:   req.AssetType,
		BuySell:     oppositeSide,
		Amount:      req.Amount,
		OrderType:   "Market",
		ManualOrder: true, // Manual order - user clicked Close Position button
	}

	// Set order duration
	closeOrder.OrderDuration.DurationType = "DayOrder"

	// Marshal request body
	reqBody, err := json.Marshal(closeOrder)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal close order: %w", err)
	}

	sbc.logger.Printf("ClosePosition: Placing %s market order for %.0f units", oppositeSide, req.Amount)
	sbc.logger.Printf("ClosePosition: Request payload: %s", string(reqBody))

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST",
		sbc.baseURL+"/trade/v2/orders", bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set headers
	httpReq.Header.Set("Content-Type", "application/json")

	// Execute request with OAuth2 auto-refresh
	resp, err := sbc.doRequest(ctx, httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	// Handle response
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, sbc.handleErrorResponse(resp)
	}

	// Parse response
	var saxoResp SaxoOrderResponse
	if err := json.NewDecoder(resp.Body).Decode(&saxoResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	sbc.logger.Printf("Position close order placed successfully. OrderID: %s", saxoResp.OrderId)

	// Convert to generic response
	return sbc.convertFromSaxoResponse(saxoResp), nil
}

// ModifyOrder implements BrokerClient.ModifyOrder
func (sbc *SaxoBrokerClient) ModifyOrder(ctx context.Context, req OrderModificationRequest) (*OrderResponse, error) {
	sbc.logger.Printf("ModifyOrder: Modifying order %s to price %s", req.OrderID, req.OrderPrice)

	// Check authentication
	if !sbc.authClient.IsAuthenticated() {
		return nil, fmt.Errorf("not authenticated with broker")
	}

	// Build modification payload following legacy SaxoMoveStopParams/SaxoToMarketParams pattern
	payload := map[string]interface{}{
		"AccountKey": req.AccountKey,
		"OrderType":  req.OrderType,
		"AssetType":  req.AssetType,
		"OrderDuration": map[string]interface{}{
			"DurationType": req.OrderDuration.DurationType,
		},
	}

	// Add OrderPrice only if specified (market orders don't have price)
	if req.OrderPrice != "" {
		payload["OrderPrice"] = req.OrderPrice
	}

	// Marshal request payload
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal modification request: %w", err)
	}

	// Create HTTP PATCH request following legacy PatchBrokerData pattern
	httpReq, err := http.NewRequestWithContext(ctx, "PATCH",
		sbc.baseURL+"/trade/v2/orders/"+req.OrderID, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set headers
	accessToken, err := sbc.authClient.GetAccessToken()
	if err != nil {
		return nil, fmt.Errorf("failed to get access token: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+accessToken)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Request-ID", fmt.Sprintf("modify-%d", time.Now().Unix()))

	// Send request
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check for success (200-299 status codes)
	// Saxo typically returns 204 No Content for successful order modifications
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d: order modification failed", resp.StatusCode)
	}

	sbc.logger.Printf("Order modified successfully: %s", req.OrderID)
	return &OrderResponse{
		OrderID:   req.OrderID,
		Status:    "Modified",
		Timestamp: time.Now().Format(time.RFC3339),
	}, nil
}

// GetOrderStatus implements BrokerClient.GetOrderStatus
func (sbc *SaxoBrokerClient) GetOrderStatus(ctx context.Context, orderID string) (*OrderStatus, error) {
	sbc.logger.Printf("GetOrderStatus: Checking order %s", orderID)

	// Check authentication
	if !sbc.authClient.IsAuthenticated() {
		return nil, fmt.Errorf("not authenticated with broker")
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "GET",
		sbc.baseURL+"/trade/v2/orders/"+orderID, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set headers
	httpReq.Header.Set("Content-Type", "application/json")
	// Execute request with OAuth2 auto-refresh
	resp, err := sbc.doRequest(ctx, httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	// Handle response
	if resp.StatusCode != http.StatusOK {
		return nil, sbc.handleErrorResponse(resp)
	}

	// Parse response
	var saxoStatus SaxoOrderStatus
	if err := json.NewDecoder(resp.Body).Decode(&saxoStatus); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Convert to generic format
	genericStatus := sbc.convertFromSaxoStatus(saxoStatus)

	return genericStatus, nil
}

// GetOpenOrders retrieves all open orders from Saxo API
// Used by recovery system to match live orders to signals
func (sbc *SaxoBrokerClient) GetOpenOrders(ctx context.Context) ([]LiveOrder, error) {
	// Saxo API endpoint: GET /port/v1/orders/me
	url := fmt.Sprintf("%s/port/v1/orders/me", sbc.baseURL)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add authorization header
	// Execute request with OAuth2 auto-refresh
	resp, err := sbc.doRequest(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to get open orders: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, sbc.handleErrorResponse(resp)
	}

	// Parse Saxo response
	var saxoResponse SaxoOpenOrdersResponse
	if err := json.NewDecoder(resp.Body).Decode(&saxoResponse); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Convert Saxo orders to domain LiveOrders
	liveOrders := make([]LiveOrder, 0, len(saxoResponse.Data))
	for _, saxoOrder := range saxoResponse.Data {
		liveOrder := sbc.convertFromSaxoOpenOrder(saxoOrder)
		liveOrders = append(liveOrders, liveOrder)
	}

	sbc.logger.Printf("GetOpenOrders: Retrieved %d open orders", len(liveOrders))
	return liveOrders, nil
}

// GetOpenPositions retrieves all open positions from Saxo API
// Endpoint: GET /port/v1/positions/me
func (sbc *SaxoBrokerClient) GetOpenPositions(ctx context.Context) (*SaxoOpenPositionsResponse, error) {
	url := fmt.Sprintf("%s/port/v1/positions/me", sbc.baseURL)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add authorization header
	// Execute request with OAuth2 auto-refresh
	resp, err := sbc.doRequest(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to get open positions: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, sbc.handleErrorResponse(resp)
	}

	// Parse Saxo response
	var saxoResponse SaxoOpenPositionsResponse
	if err := json.NewDecoder(resp.Body).Decode(&saxoResponse); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	sbc.logger.Printf("GetOpenPositions: Retrieved %d open positions", len(saxoResponse.Data))
	return &saxoResponse, nil
}

// GetNetPositions retrieves aggregated net positions from Saxo API
// Endpoint: GET /port/v1/netpositions/me
// NetPositions aggregate multiple individual positions of the same instrument
// Example: 3 long EURUSD positions = 1 net position showing total exposure
func (sbc *SaxoBrokerClient) GetNetPositions(ctx context.Context) (*SaxoNetPositionsResponse, error) {
	url := fmt.Sprintf("%s/port/v1/netpositions/me", sbc.baseURL)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Execute request with OAuth2 auto-refresh
	resp, err := sbc.doRequest(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to get net positions: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, sbc.handleErrorResponse(resp)
	}

	// Parse Saxo response
	var saxoResponse SaxoNetPositionsResponse
	if err := json.NewDecoder(resp.Body).Decode(&saxoResponse); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	sbc.logger.Printf("GetNetPositions: Retrieved %d net positions", len(saxoResponse.Data))
	return &saxoResponse, nil
}

// GetClosedPositions retrieves closed positions from Saxo API
// Endpoint: GET /port/v1/closedpositions/me
func (sbc *SaxoBrokerClient) GetClosedPositions(ctx context.Context) (*SaxoClosedPositionsResponse, error) {
	url := fmt.Sprintf("%s/port/v1/closedpositions/me", sbc.baseURL)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add authorization header
	// Execute request with OAuth2 auto-refresh
	resp, err := sbc.doRequest(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to get closed positions: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, sbc.handleErrorResponse(resp)
	}

	// Parse Saxo response
	var saxoResponse SaxoClosedPositionsResponse
	if err := json.NewDecoder(resp.Body).Decode(&saxoResponse); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	sbc.logger.Printf("GetClosedPositions: Retrieved %d closed positions", len(saxoResponse.Data))
	return &saxoResponse, nil
}

// GetAccounts implements BrokerClient.GetAccounts with generic return type
func (sbc *SaxoBrokerClient) GetAccounts(ctx context.Context) (*Accounts, error) {
	sbc.logger.Printf("GetAccounts: Fetching accounts")

	url := fmt.Sprintf("%s/port/v1/accounts/me", sbc.baseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := sbc.doRequest(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to get accounts: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, sbc.handleErrorResponse(resp)
	}

	var saxoResp SaxoAccountResponse
	if err := json.NewDecoder(resp.Body).Decode(&saxoResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Convert to generic Accounts (identical schema)
	accounts := &Accounts{
		Data: make([]Account, len(saxoResp.Data)),
	}
	for i := range saxoResp.Data {
		accounts.Data[i] = Account(saxoResp.Data[i])
	}

	return accounts, nil
}

// GetAccountBalance retrieves account balance from Saxo API
// Endpoint: GET /port/v1/balances/me
func (sbc *SaxoBrokerClient) GetAccountBalance(ctx context.Context) (*SaxoBalance, error) {
	url := fmt.Sprintf("%s/port/v1/balances/me", sbc.baseURL)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add authorization header
	// Execute request with OAuth2 auto-refresh
	resp, err := sbc.doRequest(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to get balance: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, sbc.handleErrorResponse(resp)
	}

	// Parse Saxo response
	var balance SaxoBalance
	if err := json.NewDecoder(resp.Body).Decode(&balance); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	sbc.logger.Printf("GetAccountBalance: Retrieved balance - Total: %.2f %s, Margin Used: %.2f, Margin Available: %.2f",
		balance.TotalValue, balance.Currency, balance.MarginUsedByCurrentPositions, balance.MarginAvailableForTrading)
	return &balance, nil
}

// GetMarginOverview retrieves detailed margin breakdown by instrument
// Endpoint: GET /port/v1/balances/marginoverview?ClientKey={clientKey}
func (sbc *SaxoBrokerClient) GetMarginOverview(ctx context.Context, clientKey string) (*SaxoMarginOverview, error) {
	url := fmt.Sprintf("%s/port/v1/balances/marginoverview?FieldGroups=DisplayAndFormat&ClientKey=%s",
		sbc.baseURL, clientKey)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add authorization header
	// Execute request with OAuth2 auto-refresh
	resp, err := sbc.doRequest(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to get margin overview: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, sbc.handleErrorResponse(resp)
	}

	// Parse Saxo response
	var marginOverview SaxoMarginOverview
	if err := json.NewDecoder(resp.Body).Decode(&marginOverview); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	sbc.logger.Printf("GetMarginOverview: Retrieved %d margin groups", len(marginOverview.Groups))
	return &marginOverview, nil
}

// GetClientInfo retrieves client/user information from Saxo API
// Endpoint: GET /port/v1/users/me
func (sbc *SaxoBrokerClient) GetClientInfo(ctx context.Context) (*SaxoClientInfo, error) {
	url := fmt.Sprintf("%s/port/v1/users/me", sbc.baseURL)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add authorization header
	// Execute request with OAuth2 auto-refresh
	resp, err := sbc.doRequest(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to get client info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, sbc.handleErrorResponse(resp)
	}

	// Parse Saxo response
	var clientInfo SaxoClientInfo
	if err := json.NewDecoder(resp.Body).Decode(&clientInfo); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	sbc.logger.Printf("GetClientInfo: Retrieved client info for %s (ClientKey: %s)", clientInfo.Name, clientInfo.ClientKey)
	return &clientInfo, nil
}

// GetBalance implements BrokerClient.GetBalance with generic return type
func (sbc *SaxoBrokerClient) GetBalance(ctx context.Context) (*Balance, error) {
	sbc.logger.Printf("GetBalance: Fetching account balance")

	// Get Saxo-specific balance
	saxoBalance, err := sbc.GetAccountBalance(ctx)
	if err != nil {
		return nil, err
	}

	// Convert Saxo-specific SaxoBalance to generic Balance (identical schema)
	return (*Balance)(saxoBalance), nil
}

// Private conversion methods - handle Saxo-specific format internally
// TODO: cleanup this is final order conversion logic. Remove all other conversion code.
func (sbc *SaxoBrokerClient) convertToSaxoOrder(req OrderRequest) (SaxoOrderRequest, error) {
	saxoReq := SaxoOrderRequest{
		AccountKey: req.AccountKey,           // Required account key
		BuySell:    req.Side,                 // "Buy" or "Sell"
		Amount:     float64(req.Size),        // Order size as float64
		OrderType:  req.OrderType,            // "Market", "Limit", "Stop"
		AssetType:  req.Instrument.AssetType, // Use enriched AssetType from futures.json
	}

	// Set price for non-market orders
	if req.OrderType != "Market" && req.Price > 0 {
		saxoReq.OrderPrice = req.Price
	}

	// Set order duration following Saxo patterns
	saxoReq.OrderDuration.DurationType = req.Duration
	if req.Duration == "" {
		saxoReq.OrderDuration.DurationType = "DayOrder" // Default
	}

	// Validate enriched instrument data
	if req.Instrument.Identifier == 0 {
		return saxoReq, fmt.Errorf("instrument %s is not enriched - Identifier (UIC) is missing. Run instrument enrichment first", req.Instrument.Ticker)
	}
	if req.Instrument.AssetType == "" {
		return saxoReq, fmt.Errorf("instrument %s is missing AssetType. This should be loaded from futures.json", req.Instrument.Ticker)
	}

	// Use enriched UIC from instrument enrichment service
	saxoReq.Uic = req.Instrument.Identifier

	return saxoReq, nil
}

func (sbc *SaxoBrokerClient) convertFromSaxoResponse(saxoResp SaxoOrderResponse) *OrderResponse {
	return &OrderResponse{
		OrderID: saxoResp.OrderId,
		Status:  saxoResp.Status,
		//Message:   saxoResp.Message,
		Timestamp: saxoResp.Timestamp,
	}
}

func (sbc *SaxoBrokerClient) convertFromSaxoStatus(saxoStatus SaxoOrderStatus) *OrderStatus {
	return &OrderStatus{
		OrderID: saxoStatus.OrderId,
		Status:  saxoStatus.Status,
		//FilledQuantity:    saxoStatus.FilledAmount,
		//RemainingQuantity: saxoStatus.Amount - saxoStatus.FilledAmount,
		//AveragePrice:      saxoStatus.ExecutionPrice,
		//Timestamp:         saxoStatus.Timestamp,
	}
}

// convertFromSaxoOpenOrder converts Saxo open order to domain LiveOrder
func (sbc *SaxoBrokerClient) convertFromSaxoOpenOrder(saxoOrder SaxoOpenOrder) LiveOrder {
	// Parse order time
	orderTime, err := time.Parse(time.RFC3339, saxoOrder.OrderTime)
	if err != nil {
		sbc.logger.Printf("WARNING: Failed to parse order time %s: %v", saxoOrder.OrderTime, err)
		orderTime = time.Now()
	}

	// Convert related orders
	relatedOrders := make([]RelatedOrder, len(saxoOrder.RelatedOpenOrders))
	for i, related := range saxoOrder.RelatedOpenOrders {
		relatedOrders[i] = RelatedOrder{
			OrderID:       related.OrderID,
			OpenOrderType: related.OpenOrderType,
			OrderPrice:    related.OrderPrice,
			Status:        related.Status,
		}
	}

	return LiveOrder{
		OrderID:        saxoOrder.OrderID,
		Uic:            saxoOrder.Uic,
		Ticker:         saxoOrder.DisplayAndFormat.Symbol,
		AssetType:      saxoOrder.AssetType,
		OrderType:      saxoOrder.OrderType,
		Amount:         saxoOrder.Amount,
		Price:          saxoOrder.OrderPrice,
		StopLimitPrice: 0, // TODO: Extract from order details if available
		OrderTime:      orderTime,
		Status:         saxoOrder.Status,
		RelatedOrders:  relatedOrders,

		// Additional Saxo fields
		BuySell: saxoOrder.BuySell,
	}
}

// GetTradingSchedule retrieves trading schedule from Saxo API with generic return type
// Following legacy broker/broker_http.go GetSaxoTradingSchedule pattern
// Endpoint: /ref/v1/instruments/tradingschedule/{UIC}/{AssetType}
func (sbc *SaxoBrokerClient) GetTradingSchedule(ctx context.Context, params TradingScheduleParams) (*TradingSchedule, error) {
	endpoint := fmt.Sprintf("/ref/v1/instruments/tradingschedule/%d/%s", params.Uic, params.AssetType)

	req, err := http.NewRequestWithContext(ctx, "GET", sbc.baseURL+endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := sbc.doRequest(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, sbc.handleErrorResponse(resp)
	}

	var saxoSchedule SaxoTradingSchedule
	if err := json.NewDecoder(resp.Body).Decode(&saxoSchedule); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	sbc.logger.Printf("Trading schedule retrieved for UIC %d: %d sessions", params.Uic, len(saxoSchedule.Sessions))

	// Convert to generic TradingSchedule (identical schema - convert each phase)
	phases := make([]TradingPhase, len(saxoSchedule.Phases))
	for i, p := range saxoSchedule.Phases {
		phases[i] = TradingPhase(p)
	}
	sessions := make([]TradingPhase, len(saxoSchedule.Sessions))
	for i, s := range saxoSchedule.Sessions {
		sessions[i] = TradingPhase(s)
	}

	return &TradingSchedule{
		Phases:   phases,
		Sessions: sessions,
	}, nil
}

// convertFromSaxoPrice converts Saxo price response to generic format
// Following legacy broker/broker_http.go price conversion patterns
func (sbc *SaxoBrokerClient) convertFromSaxoPrice(saxoPrice SaxoPriceResponse, ticker string) *PriceData {
	if len(saxoPrice.Data) == 0 {
		sbc.logger.Printf("Warning: Empty price data for %s", ticker)
		return &PriceData{
			Ticker:    ticker,
			Bid:       0.0,
			Ask:       0.0,
			Mid:       0.0,
			Spread:    0.0,
			Timestamp: "",
		}
	}

	// Use most recent data point (last in array)
	latest := saxoPrice.Data[len(saxoPrice.Data)-1]

	// Calculate mid price and spread following FX domain knowledge
	bid := latest.CloseBid
	ask := latest.CloseAsk
	mid := (bid + ask) / 2.0
	spread := ask - bid

	return &PriceData{
		Ticker:    ticker,
		Bid:       bid,
		Ask:       ask,
		Mid:       mid,
		Spread:    spread,
		Timestamp: latest.Time,
	}
}

// convertFromSaxoAccount converts Saxo account response to generic format
// Following legacy portfolio balance patterns from broker/broker_http.go
func (sbc *SaxoBrokerClient) convertFromSaxoAccount(saxoAccount SaxoAccountInfo) *AccountInfo {
	return &AccountInfo{
		AccountKey:  saxoAccount.AccountKey,
		AccountType: saxoAccount.AccountType,
		Currency:    saxoAccount.Currency,
		Balance:     0.0, // Need to fetch balance separately from /port/v1/balances
		MarginUsed:  0.0, // Will be populated by balance call
		MarginFree:  0.0, // Will be populated by balance call
	}
}

// doRequest executes an HTTP request using OAuth2 auto-refresh client
// This ensures tokens are automatically refreshed before requests, triggering
// external refresh notifications for WebSocket re-authorization
func (sbc *SaxoBrokerClient) doRequest(ctx context.Context, req *http.Request) (*http.Response, error) {
	httpClient, err := sbc.authClient.GetHTTPClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get HTTP client: %w", err)
	}
	return httpClient.Do(req)
}

// handleErrorResponse handles HTTP error responses
func (sbc *SaxoBrokerClient) handleErrorResponse(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
}

// SearchInstruments implements BrokerClient.SearchInstruments
// Searches for instruments matching criteria
func (sbc *SaxoBrokerClient) SearchInstruments(ctx context.Context, params InstrumentSearchParams) ([]Instrument, error) {
	sbc.logger.Printf("SearchInstruments: Searching for %s instruments with keywords '%s'", params.AssetType, params.Keywords)

	if !sbc.authClient.IsAuthenticated() {
		return nil, fmt.Errorf("not authenticated with broker")
	}

	// Build URL with query parameters
	url := fmt.Sprintf("%s/ref/v1/instruments/?AssetType=%s&ExchangeId=%s&Keywords=%s&Skip=0",
		sbc.baseURL, params.AssetType, params.Exchange, params.Keywords)

	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := sbc.doRequest(ctx, httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, sbc.handleErrorResponse(resp)
	}

	// Parse Saxo API response
	var saxoResp struct {
		Data []struct {
			Identifier   int    `json:"Identifier"`
			Symbol       string `json:"Symbol"`
			Description  string `json:"Description"`
			AssetType    string `json:"AssetType"`
			ExchangeID   string `json:"ExchangeId"`
			CurrencyCode string `json:"CurrencyCode"`
		} `json:"Data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&saxoResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Convert to generic Instrument format
	instruments := make([]Instrument, len(saxoResp.Data))
	for i, item := range saxoResp.Data {
		instruments[i] = Instrument{
			Identifier:  item.Identifier,
			Uic:         item.Identifier,
			Symbol:      item.Symbol,
			Description: item.Description,
			AssetType:   item.AssetType,
			Exchange:    item.ExchangeID,
			Currency:    item.CurrencyCode,
		}
	}

	sbc.logger.Printf("Found %d instruments", len(instruments))
	return instruments, nil
}

// GetInstrumentDetails implements BrokerClient.GetInstrumentDetails
// Gets detailed instrument information for multiple UICs
func (sbc *SaxoBrokerClient) GetInstrumentDetails(ctx context.Context, uics []int) ([]InstrumentDetail, error) {
	sbc.logger.Printf("GetInstrumentDetails: Fetching details for %d instruments", len(uics))

	if !sbc.authClient.IsAuthenticated() {
		return nil, fmt.Errorf("not authenticated with broker")
	}

	// Convert UICs to comma-separated string
	uicsStr := fmt.Sprintf("%d", uics[0])
	for i := 1; i < len(uics); i++ {
		uicsStr += fmt.Sprintf(",%d", uics[i])
	}

	url := fmt.Sprintf("%s/ref/v1/instruments/details?Uics=%s", sbc.baseURL, uicsStr)

	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := sbc.doRequest(ctx, httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, sbc.handleErrorResponse(resp)
	}

	// Parse Saxo API response
	var saxoResp struct {
		Data []struct {
			Identifier            int     `json:"Identifier"`
			TickSize              float64 `json:"TickSize"`
			ExpiryDate            string  `json:"ExpiryDate"`
			NoticeDate            string  `json:"NoticeDate"`
			PriceToContractFactor float64 `json:"PriceToContractFactor"`
			Format                struct {
				Decimals          int    `json:"Decimals"`
				OrderDecimals     int    `json:"OrderDecimals"`
				Format            string `json:"Format"`
				NumeratorDecimals int    `json:"NumeratorDecimals"`
			} `json:"Format"`
		} `json:"Data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&saxoResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Convert to generic InstrumentDetail format
	details := make([]InstrumentDetail, len(saxoResp.Data))
	for i, item := range saxoResp.Data {
		detail := InstrumentDetail{
			Uic:                   item.Identifier,
			TickSize:              item.TickSize,
			Decimals:              item.Format.Decimals,
			OrderDecimals:         item.Format.OrderDecimals,
			PriceToContractFactor: item.PriceToContractFactor,
			Format:                item.Format.Format,
			NumeratorDecimals:     item.Format.NumeratorDecimals,
		}

		// Parse dates if available
		if item.ExpiryDate != "" {
			if expiry, err := time.Parse("2006-01-02", item.ExpiryDate); err == nil {
				detail.ExpiryDate = expiry
			}
		}
		if item.NoticeDate != "" {
			if notice, err := time.Parse("2006-01-02", item.NoticeDate); err == nil {
				detail.NoticeDate = notice
			}
		}

		details[i] = detail
	}

	sbc.logger.Printf("Retrieved details for %d instruments", len(details))
	return details, nil
}

// GetInstrumentPrices implements BrokerClient.GetInstrumentPrices
// Gets price information (including open interest) for instrument selection
func (sbc *SaxoBrokerClient) GetInstrumentPrices(ctx context.Context, uics []int, fieldGroups string) ([]InstrumentPriceInfo, error) {
	sbc.logger.Printf("GetInstrumentPrices: Fetching prices for %d instruments", len(uics))

	if !sbc.authClient.IsAuthenticated() {
		return nil, fmt.Errorf("not authenticated with broker")
	}

	// Convert UICs to comma-separated string
	uicsStr := fmt.Sprintf("%d", uics[0])
	for i := 1; i < len(uics); i++ {
		uicsStr += fmt.Sprintf(",%d", uics[i])
	}

	url := fmt.Sprintf("%s/trade/v1/infoprices/list?Uics=%s&FieldGroups=%s&AssetType=ContractFutures",
		sbc.baseURL, uicsStr, fieldGroups)

	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := sbc.doRequest(ctx, httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, sbc.handleErrorResponse(resp)
	}

	// Parse Saxo API response
	var saxoResp struct {
		Data []struct {
			Uic                    int `json:"Uic"`
			InstrumentPriceDetails struct {
				OpenInterest float64 `json:"OpenInterest"`
			} `json:"InstrumentPriceDetails"`
			Quote struct {
				Mid float64 `json:"Mid"`
			} `json:"Quote"`
		} `json:"Data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&saxoResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Convert to generic InstrumentPriceInfo format
	prices := make([]InstrumentPriceInfo, len(saxoResp.Data))
	for i, item := range saxoResp.Data {
		prices[i] = InstrumentPriceInfo{
			Uic:          item.Uic,
			OpenInterest: item.InstrumentPriceDetails.OpenInterest,
			LastPrice:    item.Quote.Mid,
		}
	}

	sbc.logger.Printf("Retrieved prices for %d instruments", len(prices))
	return prices, nil
}
