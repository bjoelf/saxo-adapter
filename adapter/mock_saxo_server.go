package saxo

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"
)

// MockSaxoServer provides HTTP mock server for unit testing
// Following legacy broker_http.go patterns without external dependencies
type MockSaxoServer struct {
	server    *httptest.Server
	responses map[string]MockResponse
	requests  []MockRequest // Track requests for verification
}

// MockResponse represents a configured mock response
type MockResponse struct {
	StatusCode int
	Body       interface{}
	Headers    map[string]string
}

// MockRequest tracks incoming requests for verification
type MockRequest struct {
	Method  string
	Path    string
	Body    string
	Headers map[string]string
}

// NewMockSaxoServer creates a new mock server
func NewMockSaxoServer() *MockSaxoServer {
	mock := &MockSaxoServer{
		responses: make(map[string]MockResponse),
		requests:  make([]MockRequest, 0),
	}

	// Create HTTP test server
	mock.server = httptest.NewServer(http.HandlerFunc(mock.handleRequest))

	// Set default responses following Saxo API patterns
	mock.setDefaultResponses()

	return mock
}

// Close shuts down the mock server
func (m *MockSaxoServer) Close() {
	m.server.Close()
}

// GetBaseURL returns the mock server base URL
func (m *MockSaxoServer) GetBaseURL() string {
	return m.server.URL
}

// SetOrderPlacementResponse configures mock response for order placement
func (m *MockSaxoServer) SetOrderPlacementResponse(response SaxoOrderResponse, statusCode int) {
	m.responses["POST /trade/v2/orders"] = MockResponse{
		StatusCode: statusCode,
		Body:       response,
		Headers:    map[string]string{"Content-Type": "application/json"},
	}
}

// SetOrderCancellationResponse configures mock response for order cancellation
func (m *MockSaxoServer) SetOrderCancellationResponse(statusCode int, message string) {
	m.responses["DELETE /trade/v2/orders"] = MockResponse{
		StatusCode: statusCode,
		Body:       map[string]string{"Message": message},
		Headers:    map[string]string{"Content-Type": "application/json"},
	}
}

// SetAuthenticationResponse configures mock OAuth2 token response
func (m *MockSaxoServer) SetAuthenticationResponse(token SaxoToken, statusCode int) {
	m.responses["POST /token"] = MockResponse{
		StatusCode: statusCode,
		Body:       token,
		Headers:    map[string]string{"Content-Type": "application/json"},
	}
}

// GetRequests returns all captured requests for verification
func (m *MockSaxoServer) GetRequests() []MockRequest {
	return m.requests
}

// ClearRequests clears the request history
func (m *MockSaxoServer) ClearRequests() {
	m.requests = make([]MockRequest, 0)
}

// Private methods

func (m *MockSaxoServer) handleRequest(w http.ResponseWriter, r *http.Request) {
	// Capture request for verification
	body := ""
	if r.Body != nil {
		bodyBytes := make([]byte, r.ContentLength)
		r.Body.Read(bodyBytes)
		body = string(bodyBytes)
	}

	headers := make(map[string]string)
	for key, values := range r.Header {
		headers[key] = strings.Join(values, ", ")
	}

	m.requests = append(m.requests, MockRequest{
		Method:  r.Method,
		Path:    r.URL.Path,
		Body:    body,
		Headers: headers,
	})

	// Find matching response
	key := fmt.Sprintf("%s %s", r.Method, r.URL.Path)
	response, exists := m.responses[key]

	if !exists {
		// Default 404 response
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{
			"ErrorCode": "NotFound",
			"Message":   "Endpoint not found",
		})
		return
	}

	// Set headers
	for key, value := range response.Headers {
		w.Header().Set(key, value)
	}

	// Set status code
	w.WriteHeader(response.StatusCode)

	// Write response body
	if response.Body != nil {
		json.NewEncoder(w).Encode(response.Body)
	}
}

func (m *MockSaxoServer) setDefaultResponses() {
	// Default successful order placement response
	m.SetOrderPlacementResponse(SaxoOrderResponse{
		OrderId:   "12345678",
		Status:    "Working",
		Message:   "Order placed successfully",
		Timestamp: time.Now().Format(time.RFC3339),
	}, http.StatusCreated)

	// Default successful order cancellation
	m.SetOrderCancellationResponse(http.StatusOK, "Order cancelled successfully")

	// Default authentication response
	m.SetAuthenticationResponse(SaxoToken{
		AccessToken:  "mock_access_token",
		RefreshToken: "mock_refresh_token",
		TokenType:    "Bearer",
		ExpiresIn:    3600,
		Scope:        "trading",
	}, http.StatusOK)
}
