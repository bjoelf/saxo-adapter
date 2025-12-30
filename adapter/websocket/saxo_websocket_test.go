package websocket

import (
	"context"
	"log"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/bjoelf/saxo-adapter/adapter/websocket/mocktesting"
)

// MockAuthClient implements saxo.AuthClient for testing
type MockAuthClient struct {
	authenticated bool
	accessToken   string
	httpClient    *http.Client
}

func (m *MockAuthClient) IsAuthenticated() bool           { return m.authenticated }
func (m *MockAuthClient) GetAccessToken() (string, error) { return m.accessToken, nil }
func (m *MockAuthClient) GetHTTPClient(ctx context.Context) (*http.Client, error) {
	if m.httpClient != nil {
		return m.httpClient, nil
	}
	return http.DefaultClient, nil
}
func (m *MockAuthClient) Login(ctx context.Context) error           { return nil }
func (m *MockAuthClient) Logout() error                             { return nil }
func (m *MockAuthClient) RefreshToken(ctx context.Context) error    { return nil }
func (m *MockAuthClient) StartAuthenticationKeeper(provider string) {}
func (m *MockAuthClient) StartTokenEarlyRefresh(ctx context.Context, wsConnected <-chan bool, wsContextID <-chan string) {
}

// GetBaseURL returns mock base URL
func (m *MockAuthClient) GetBaseURL() string {
	return "https://gateway.saxobank.com/sim/openapi"
}

// GetWebSocketURL returns mock WebSocket URL (new streaming domain)
func (m *MockAuthClient) GetWebSocketURL() string {
	return "https://sim-streaming.saxobank.com/sim/oapi"
}

// SetRedirectURL sets OAuth redirect URL (mock implementation)
func (m *MockAuthClient) SetRedirectURL(provider string, redirectURL string) error {
	return nil
}

// BuildRedirectURL builds OAuth redirect URL (mock implementation)
func (m *MockAuthClient) BuildRedirectURL(host string, provider string) string {
	return "http://localhost:3001/oauth/saxo/callback"
}

// GenerateAuthURL generates OAuth authorization URL (mock implementation)
func (m *MockAuthClient) GenerateAuthURL(provider string, state string) (string, error) {
	return "https://mock.auth.url", nil
}

// ExchangeCodeForToken exchanges OAuth code for token (mock implementation)
func (m *MockAuthClient) ExchangeCodeForToken(ctx context.Context, code string, provider string) error {
	m.authenticated = true
	return nil
}

func TestSaxoWebSocketClient_Connect(t *testing.T) {
	// Setup mock server following legacy WebSocket testing patterns
	mockServer := mocktesting.NewMockSaxoWebSocketServer()
	defer mockServer.Close()

	// Create mock auth client with TLS client for self-signed certificates
	mockAuth := &MockAuthClient{
		authenticated: true,
		accessToken:   "test_token_123",
		httpClient:    mockServer.GetHTTPClient(),
	}

	// Create WebSocket client with mock server URL
	logger := log.New(os.Stdout, "TEST: ", log.LstdFlags)
	client := NewSaxoWebSocketClient(mockAuth, mockServer.GetBaseURL(), mockServer.GetWebSocketURL(), logger)

	// Test connection establishment
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := client.Connect(ctx)
	if err != nil {
		t.Fatalf("Failed to connect to mock WebSocket server: %v", err)
	}

	// Verify connection state
	if !client.connectionManager.IsConnected() {
		t.Error("Expected client to be connected")
	}

	// Test graceful shutdown
	if err := client.Close(); err != nil {
		t.Errorf("Failed to close WebSocket connection: %v", err)
	}
}

func TestSaxoWebSocketClient_PriceSubscription(t *testing.T) {
	// Setup mock server and client
	mockServer := mocktesting.NewMockSaxoWebSocketServer()
	defer mockServer.Close()

	mockAuth := &MockAuthClient{
		authenticated: true,
		accessToken:   "test_token_123",
		httpClient:    mockServer.GetHTTPClient(),
	}

	logger := log.New(os.Stdout, "TEST: ", log.LstdFlags)
	client := NewSaxoWebSocketClient(mockAuth, mockServer.GetBaseURL(), mockServer.GetWebSocketURL(), logger)

	// Connect to mock server
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client.Close()

	// Test price subscription
	tickers := []string{"21", "22"}
	if err := client.SubscribeToPrices(ctx, tickers, "FxSpot"); err != nil {
		t.Fatalf("Failed to subscribe to prices: %v", err)
	}

	// NOTE: In the real Saxo API, subscriptions are created via HTTP POST to the API,
	// not via the WebSocket itself. The WebSocket is only for receiving streaming data.
	// Therefore, we don't check for subscription registration in the mock server here.

	// Test price update reception
	go func() {
		time.Sleep(50 * time.Millisecond)
		mockServer.SendPriceUpdate("21", 1.1000, 1.1002)
	}()

	// Listen for price update
	select {
	case priceUpdate := <-client.GetPriceUpdateChannel():
		if priceUpdate.Uic != 21 {
			t.Errorf("Expected UIC 21, got %d", priceUpdate.Uic)
		}
		if priceUpdate.Bid != 1.1000 {
			t.Errorf("Expected bid 1.1000, got %f", priceUpdate.Bid)
		}
		if priceUpdate.Ask != 1.1002 {
			t.Errorf("Expected ask 1.1002, got %f", priceUpdate.Ask)
		}
	case <-time.After(2 * time.Second):
		t.Error("Timeout waiting for price update")
	}
}

func TestSaxoWebSocketClient_ReconnectionLogic(t *testing.T) {
	// This test verifies the complex reconnection logic following legacy patterns
	// NOTE: With the new async architecture, reconnection has a 1-minute delay
	// This test verifies that connection loss is DETECTED, not that reconnection completes
	mockServer := mocktesting.NewMockSaxoWebSocketServer()

	mockAuth := &MockAuthClient{
		authenticated: true,
		accessToken:   "test_token_123",
		httpClient:    mockServer.GetHTTPClient(),
	}

	logger := log.New(os.Stdout, "TEST: ", log.LstdFlags)
	client := NewSaxoWebSocketClient(mockAuth, mockServer.GetBaseURL(), mockServer.GetWebSocketURL(), logger)

	// Connect initially
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Initial connection failed: %v", err)
	}

	// Subscribe to prices
	tickers := []string{"21"}
	if err := client.SubscribeToPrices(ctx, tickers, "FxSpot"); err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	// Verify initially connected
	if !client.connectionManager.IsConnected() {
		t.Error("Expected connection to be established initially")
	}

	// Simulate connection loss by closing mock server
	mockServer.Close()

	// Wait for reader goroutine to detect the connection loss
	// The reader will detect the error and send it to the processor
	// The processor will update the connection state and queue reconnection
	time.Sleep(200 * time.Millisecond)

	// Verify connection manager detected the failure
	// NOTE: We're testing that the error was DETECTED, not that reconnection completed
	// The reconnection handler has a 1-minute delay before attempting to reconnect
	if client.connectionManager.IsConnected() {
		t.Error("Expected connection to be detected as lost after server closure")
	}

	// Verify reconnection was queued (check channel length)
	// NOTE: We can't easily verify the reconnection trigger was sent without
	// exposing internal channels, so we just verify disconnection was detected

	// Cleanup - this will cancel the reconnection handler before it attempts reconnection
	client.Close()
}

func TestSaxoWebSocketClient_OrderUpdates(t *testing.T) {
	// Setup
	mockServer := mocktesting.NewMockSaxoWebSocketServer()
	defer mockServer.Close()

	mockAuth := &MockAuthClient{
		authenticated: true,
		accessToken:   "test_token_123",
		httpClient:    mockServer.GetHTTPClient(),
	}

	logger := log.New(os.Stdout, "TEST: ", log.LstdFlags)
	client := NewSaxoWebSocketClient(mockAuth, mockServer.GetBaseURL(), mockServer.GetWebSocketURL(), logger)

	// Connect and subscribe to orders
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client.Close()

	if err := client.SubscribeToOrders(ctx); err != nil {
		t.Fatalf("Failed to subscribe to orders: %v", err)
	}

	// Test order update reception
	go func() {
		time.Sleep(50 * time.Millisecond)
		mockServer.SendOrderUpdate("order_123", "Filled")
	}()

	// Listen for order update
	select {
	case orderUpdate := <-client.GetOrderUpdateChannel():
		if orderUpdate.OrderId != "order_123" {
			t.Errorf("Expected order ID order_123, got %s", orderUpdate.OrderId)
		}
		if orderUpdate.Status != "Filled" {
			t.Errorf("Expected status Filled, got %s", orderUpdate.Status)
		}
	case <-time.After(2 * time.Second):
		t.Error("Timeout waiting for order update")
	}
}

// Benchmark WebSocket message processing performance
func BenchmarkMessageProcessing(b *testing.B) {
	mockServer := mocktesting.NewMockSaxoWebSocketServer()
	defer mockServer.Close()

	mockAuth := &MockAuthClient{
		authenticated: true,
		accessToken:   "test_token_123",
	}

	logger := log.New(os.Stdout, "BENCH: ", log.LstdFlags)
	client := NewSaxoWebSocketClient(mockAuth, mockServer.GetBaseURL(), mockServer.GetWebSocketURL(), logger)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := client.Connect(ctx); err != nil {
		b.Fatalf("Failed to connect: %v", err)
	}
	defer client.Close()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		mockServer.SendPriceUpdate("21", 1.1000+float64(i)*0.0001, 1.1002+float64(i)*0.0001)
	}
}
