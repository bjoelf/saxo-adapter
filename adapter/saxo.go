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

	"github.com/bjoelf/pivot-web2/internal/domain"
	"github.com/bjoelf/pivot-web2/internal/ports"
)

// CreateBrokerServices creates Saxo auth and broker clients with environment configuration
func CreateBrokerServices(logger *log.Logger) (ports.AuthClient, ports.BrokerClient, error) {
	// Create auth client with environment configuration
	authClient, err := CreateSaxoAuthClient(logger)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create auth client: %w", err)
	}

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

	return authClient, brokerClient, nil
}

// cachedHistoricalData represents cached market data for an instrument
type cachedHistoricalData struct {
	Data      []ports.HistoricalDataPoint
	Timestamp time.Time
}

// SaxoBrokerClient implements ports.BrokerClient interface
// All Saxo-specific details are handled internally
type SaxoBrokerClient struct {
	authClient ports.AuthClient
	httpClient *http.Client
	baseURL    string
	logger     *log.Logger

	// Historical data cache following legacy SinglePivotHistory caching pattern
	historyCache map[string]*cachedHistoricalData
	cacheMutex   sync.RWMutex
	cacheExpiry  time.Duration // Default: 1 hour like legacy system
}

// NewSaxoBrokerClient creates a new Saxo broker client
func NewSaxoBrokerClient(authClient ports.AuthClient, baseURL string, logger *log.Logger) *SaxoBrokerClient {
	return &SaxoBrokerClient{
		authClient:   authClient,
		baseURL:      baseURL,
		logger:       logger,
		historyCache: make(map[string]*cachedHistoricalData),
		cacheExpiry:  1 * time.Hour, // Following legacy 1-hour cache pattern
	}
}

// PlaceOrder implements ports.BrokerClient.PlaceOrder
// Converts generic OrderRequest to Saxo-specific format internally
func (sbc *SaxoBrokerClient) PlaceOrder(ctx context.Context, req ports.OrderRequest) (*ports.OrderResponse, error) {
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

// DeleteOrder implements ports.BrokerClient.DeleteOrder
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

// CancelOrder implements ports.BrokerClient.CancelOrder
// Uses Saxo API: DELETE /trade/v2/orders/{OrderIds}?AccountKey={AccountKey}
func (sbc *SaxoBrokerClient) CancelOrder(ctx context.Context, req ports.CancelOrderRequest) error {
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

// ClosePosition implements ports.BrokerClient.ClosePosition
// Closes position by placing an opposite market order
//
// For accounts with Real-time (Intraday) netting: Opposing positions are netted immediately
// For accounts with End-of-Day netting: Positions are netted overnight
//
// Note: Real-time netting does NOT support relating orders to positions.
// Therefore we use a simple opposite market order which works for both netting modes.
// Reference: https://www.developer.saxo/openapi/learn/fifo-real-time-netting
func (sbc *SaxoBrokerClient) ClosePosition(ctx context.Context, req ports.ClosePositionRequest) (*ports.OrderResponse, error) {
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
	accessToken, err := sbc.authClient.GetAccessToken()
	if err != nil {
		return nil, fmt.Errorf("failed to get access token: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+accessToken)
	httpReq.Header.Set("Content-Type", "application/json")

	// Execute request
	resp, err := sbc.httpClient.Do(httpReq)
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

// ModifyOrder implements ports.BrokerClient.ModifyOrder
func (sbc *SaxoBrokerClient) ModifyOrder(ctx context.Context, req ports.OrderModificationRequest) (*ports.OrderResponse, error) {
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
	return &ports.OrderResponse{
		OrderID:   req.OrderID,
		Status:    "Modified",
		Timestamp: time.Now().Format(time.RFC3339),
	}, nil
}

// GetOrderStatus implements ports.BrokerClient.GetOrderStatus
func (sbc *SaxoBrokerClient) GetOrderStatus(ctx context.Context, orderID string) (*ports.OrderStatus, error) {
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
func (sbc *SaxoBrokerClient) GetOpenOrders(ctx context.Context) ([]domain.LiveOrder, error) {
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
	liveOrders := make([]domain.LiveOrder, 0, len(saxoResponse.Data))
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

// GetAccounts implements ports.BrokerClient.GetAccounts
// TODO: Implement actual Saxo accounts API call
func (sbc *SaxoBrokerClient) GetAccounts(force bool) (*ports.SaxoAccounts, error) {
	sbc.logger.Printf("GetAccounts: Called with force=%v - TODO: implement", force)
	// Placeholder implementation - needs actual Saxo API integration
	return &ports.SaxoAccounts{}, fmt.Errorf("GetAccounts not yet implemented")
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

// GetBalance implements ports.BrokerClient.GetBalance (legacy signature)
// TODO: Implement actual Saxo balance API call
func (sbc *SaxoBrokerClient) GetBalance(force bool) (*ports.SaxoPortfolioBalance, error) {
	sbc.logger.Printf("GetBalance: Called with force=%v - TODO: implement", force)
	// Placeholder implementation - needs actual Saxo API integration
	return &ports.SaxoPortfolioBalance{}, fmt.Errorf("GetBalance not yet implemented")
}

// Private conversion methods - handle Saxo-specific format internally
// TODO: cleanup this is final order conversion logic. Remove all other conversion code.
func (sbc *SaxoBrokerClient) convertToSaxoOrder(req ports.OrderRequest) (SaxoOrderRequest, error) {
	saxoReq := SaxoOrderRequest{
		BuySell:   req.Side,                 // "Buy" or "Sell"
		Amount:    float64(req.Size),        // Order size as float64
		OrderType: req.OrderType,            // "Market", "Limit", "Stop"
		AssetType: req.Instrument.AssetType, // Use enriched AssetType from futures.json
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

func (sbc *SaxoBrokerClient) convertFromSaxoResponse(saxoResp SaxoOrderResponse) *ports.OrderResponse {
	return &ports.OrderResponse{
		OrderID: saxoResp.OrderId,
		Status:  saxoResp.Status,
		//Message:   saxoResp.Message,
		Timestamp: saxoResp.Timestamp,
	}
}

func (sbc *SaxoBrokerClient) convertFromSaxoStatus(saxoStatus SaxoOrderStatus) *ports.OrderStatus {
	return &ports.OrderStatus{
		OrderID: saxoStatus.OrderId,
		Status:  saxoStatus.Status,
		//FilledQuantity:    saxoStatus.FilledAmount,
		//RemainingQuantity: saxoStatus.Amount - saxoStatus.FilledAmount,
		//AveragePrice:      saxoStatus.ExecutionPrice,
		//Timestamp:         saxoStatus.Timestamp,
	}
}

// convertFromSaxoOpenOrder converts Saxo open order to domain LiveOrder
func (sbc *SaxoBrokerClient) convertFromSaxoOpenOrder(saxoOrder SaxoOpenOrder) domain.LiveOrder {
	// Parse order time
	orderTime, err := time.Parse(time.RFC3339, saxoOrder.OrderTime)
	if err != nil {
		sbc.logger.Printf("WARNING: Failed to parse order time %s: %v", saxoOrder.OrderTime, err)
		orderTime = time.Now()
	}

	// Convert related orders
	relatedOrders := make([]domain.RelatedOrder, len(saxoOrder.RelatedOpenOrders))
	for i, related := range saxoOrder.RelatedOpenOrders {
		relatedOrders[i] = domain.RelatedOrder{
			OrderID:       related.OrderID,
			OpenOrderType: related.OpenOrderType,
			OrderPrice:    related.OrderPrice,
			Status:        related.Status,
		}
	}

	return domain.LiveOrder{
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
		BuySell:          saxoOrder.BuySell,
		OrderDuration:    saxoOrder.OrderDuration.DurationType,
		OrderRelation:    saxoOrder.OrderRelation,
		AccountKey:       saxoOrder.AccountKey,
		ClientKey:        saxoOrder.ClientKey,
		DistanceToMarket: saxoOrder.DistanceToMarket,
		IsMarketOpen:     saxoOrder.IsMarketOpen,
		MarketPrice:      saxoOrder.MarketPrice,
		OrderAmountType:  "Quantity", // Default, TODO: get from actual order
	}
}

func (sbc *SaxoBrokerClient) handleErrorResponse(resp *http.Response) error {
	// Read the response body first
	bodyBytes, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return fmt.Errorf("HTTP %d (failed to read error response body: %v)", resp.StatusCode, readErr)
	}

	// Log the raw response for debugging
	sbc.logger.Printf("Saxo API Error Response (HTTP %d): %s", resp.StatusCode, string(bodyBytes))

	// Try to decode as structured error
	var saxoErr SaxoErrorResponse
	if err := json.Unmarshal(bodyBytes, &saxoErr); err != nil {
		return fmt.Errorf("HTTP %d (raw response: %s)", resp.StatusCode, string(bodyBytes))
	}

	// Build error message - handle empty fields gracefully
	if saxoErr.ErrorCode == "" && saxoErr.Message == "" {
		return fmt.Errorf("HTTP %d (raw response: %s)", resp.StatusCode, string(bodyBytes))
	}
	return fmt.Errorf("HTTP %d: %s", resp.StatusCode, saxoErr.Message)
}

// GetTradingSchedule retrieves trading schedule from Saxo API
// Following legacy broker/broker_http.go GetSaxoTradingSchedule pattern
// Endpoint: /ref/v1/instruments/tradingschedule/{UIC}/{AssetType}
func (sbc *SaxoBrokerClient) GetTradingSchedule(params ports.SaxoTradingScheduleParams) (ports.SaxoTradingSchedule, error) {
	endpoint := fmt.Sprintf("/ref/v1/instruments/tradingschedule/%s/%s", params.Uic, params.AssetType)

	req, err := http.NewRequest("GET", sbc.baseURL+endpoint, nil)
	if err != nil {
		return ports.SaxoTradingSchedule{}, fmt.Errorf("failed to create request: %w", err)
	}

	// Add authorization header
	token, err := sbc.authClient.GetAccessToken()
	if err != nil {
		return ports.SaxoTradingSchedule{}, fmt.Errorf("failed to get access token: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := sbc.httpClient.Do(req)
	if err != nil {
		return ports.SaxoTradingSchedule{}, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ports.SaxoTradingSchedule{}, sbc.handleErrorResponse(resp)
	}

	var schedule ports.SaxoTradingSchedule
	if err := json.NewDecoder(resp.Body).Decode(&schedule); err != nil {
		return ports.SaxoTradingSchedule{}, fmt.Errorf("failed to decode response: %w", err)
	}

	sbc.logger.Printf("Trading schedule retrieved for UIC %s: %d sessions", params.Uic, len(schedule.Sessions))
	return schedule, nil
}

// convertFromSaxoPrice converts Saxo price response to generic format
// Following legacy broker/broker_http.go price conversion patterns
func (sbc *SaxoBrokerClient) convertFromSaxoPrice(saxoPrice SaxoPriceResponse, ticker string) *ports.PriceData {
	if len(saxoPrice.Data) == 0 {
		sbc.logger.Printf("Warning: Empty price data for %s", ticker)
		return &ports.PriceData{
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

	return &ports.PriceData{
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
func (sbc *SaxoBrokerClient) convertFromSaxoAccount(saxoAccount SaxoAccountInfo) *ports.AccountInfo {
	return &ports.AccountInfo{
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

// Compile-time interface checks to ensure SaxoBrokerClient implements required interfaces
var (
	_ ports.MarketDataClient = (*SaxoBrokerClient)(nil)
)
