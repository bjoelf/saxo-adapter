# Saxo Bank Adapter Library - AI Coding Instructions

## Library Purpose & Philosophy

**This is a standalone, reusable Go library** providing Saxo Bank OpenAPI integration. It is **NOT** trading-platform-specific - any Go project can consume it via `go get`.

### Core Design Principles

1. **Broker Abstraction**: Consumers use generic interfaces (`BrokerClient`, `AuthClient`, `WebSocketClient`) - never Saxo-specific types
2. **Internal Conversion**: All Saxo API complexity is hidden behind interfaces, converted internally
3. **Zero External State**: Library is stateless except for token storage (configurable via `TokenStorage` interface)
4. **Standalone Testability**: Tests use mock servers, no external dependencies required

## Architecture Pattern: Interface-Driven Design

```
Consumer Application (e.g., pivot-web2)
    ↓ imports "github.com/bjoelf/saxo-adapter/adapter"
    ↓ uses BrokerClient interface with generic types
Conversion Layer (saxo.go, oauth.go)
    ↓ converts generic → Saxo-specific types internally
Saxo Bank OpenAPI
```

**Key Rule**: Consumers NEVER see Saxo-specific types (like `SaxoOrderRequest`). They only use generic types defined in `interfaces.go`.

## Project Structure

```
saxo-adapter/
├── adapter/                      # Core library code
│   ├── interfaces.go            # Public contracts (BrokerClient, AuthClient, WebSocketClient)
│   ├── types.go                 # Generic types (OrderRequest, Instrument, Balance)
│   ├── saxo.go                  # BrokerClient implementation (~1100 lines)
│   ├── oauth.go                 # AuthClient implementation with auto-refresh
│   ├── market_data.go           # Historical data with 1-hour caching
│   ├── token_storage.go         # Token persistence interface & file impl
│   ├── config.go                # Test configuration utilities
│   ├── websocket/               # Real-time streaming subsystem
│   │   ├── connection_manager.go    # WebSocket lifecycle & reconnection
│   │   ├── subscription_manager.go  # Subscription tracking & recovery
│   │   ├── message_handler.go       # Binary protocol parsing
│   │   └── types.go                 # WebSocket-specific types
│   └── websocket/mocktesting/   # Mock WebSocket server for tests
├── examples/                    # Usage examples
│   ├── basic_auth/             # OAuth2 authentication flow
│   ├── websocket_prices/       # Real-time price streaming
│   └── place_order/            # Order placement example
├── docs/
│   ├── ARCHITECTURE.md         # Layer architecture & design patterns
│   └── AUTHENTICATION.md       # OAuth2 flow documentation
└── data/                       # Runtime token storage (gitignored)
```

## Critical Development Workflows

### Local Testing

```bash
# Unit tests with mock server (no credentials needed)
go test ./adapter/...

# WebSocket-specific tests
go test ./adapter/websocket/...

# Integration tests (requires SAXO_CLIENT_ID, SAXO_CLIENT_SECRET for SIM)
export SAXO_CLIENT_ID="your_sim_client_id"
export SAXO_CLIENT_SECRET="your_sim_secret"
go test -tags=integration ./adapter/integration_test.go

# All tests
go test ./...
```

### Publishing New Versions

**This library uses semantic versioning with Git tags:**

```bash
# After making changes and committing
git tag v0.4.2
git push origin v0.4.2

# Consumers update with
go get github.com/bjoelf/saxo-adapter@v0.4.2
go mod tidy
```

### Local Development with Consumer App

**Use `replace` directive in consumer's `go.mod` during active development:**

```go
// In pivot-web2/go.mod (or other consumer)
replace github.com/bjoelf/saxo-adapter => ../saxo-adapter
```

**Workflow:**
1. Make changes in `saxo-adapter/`
2. Changes immediately reflected in consumer app (no publishing needed)
3. Test integration in consumer
4. When stable, remove `replace` directive and publish tag

## Key Interface Contracts

### BrokerClient Interface (`interfaces.go`)

**Order Management:**
- `PlaceOrder(ctx, OrderRequest)` - Place new order (stop, limit, market)
- `ModifyOrder(ctx, OrderModificationRequest)` - Update existing order (price, trailing stop)
- `CancelOrder(ctx, CancelOrderRequest)` - Cancel pending order
- `ClosePosition(ctx, ClosePositionRequest)` - Close open position at market

**Queries:**
- `GetOpenOrders(ctx)` - List all pending orders
- `GetOpenPositions(ctx)` - List all open positions
- `GetBalance(ctx)` - Account balance & margin
- `GetAccounts(ctx)` - User account list

**Market Data:**
- `GetHistoricalData(ctx, Instrument, days)` - OHLC bars (cached 1 hour)
- `GetTradingSchedule(ctx, params)` - Market open/close times
- `GetInstrumentDetails(ctx, uics)` - Instrument specs (tick size, lot sizes)

### WebSocketClient Interface (`websocket/types.go`)

**Subscription Methods:**
- `SubscribeToPrices(ctx, uics, callbacks)` - Real-time price feeds
- `SubscribeToOrders(ctx, clientKey, callbacks)` - Order status updates
- `SubscribeToPortfolio(ctx, clientKey, callbacks)` - Position & balance updates
- `SubscribeToSessionEvents(ctx, callbacks)` - Session state monitoring

**Lifecycle:**
- `Connect()` - Establish WebSocket connection (auto-reconnects on failure)
- `Disconnect()` - Graceful shutdown, cleanup subscriptions
- `IsConnected()` - Connection health check

**Key Features:**
- Automatic reconnection with exponential backoff
- Subscription recovery after reconnection
- Binary protocol parsing (Saxo streaming format)
- Heartbeat monitoring (70-second timeout)

## Type Conversion Pattern (CRITICAL)

### Consumer Code (Generic Types)
```go
import saxo "github.com/bjoelf/saxo-adapter/adapter"

// Consumer uses ONLY generic types
req := saxo.OrderRequest{
    Instrument: saxo.Instrument{Ticker: "EURUSD"},
    OrderType:  "StopLimit",
    BuySell:    "Buy",
    Amount:     10000,
    OrderPrice: 1.1000,
}

client := saxo.NewSaxoBrokerClient(authClient, baseURL, logger)
resp, err := client.PlaceOrder(ctx, req)  // Generic in, generic out
```

### Internal Conversion (`saxo.go`)
```go
// Library converts generic → Saxo-specific internally
func (sbc *SaxoBrokerClient) convertToSaxoOrder(req OrderRequest) (*SaxoOrderRequest, error) {
    // Convert generic OrderRequest to Saxo's specific format
    saxoReq := &SaxoOrderRequest{
        Uic:       req.Instrument.Uic,
        AssetType: req.Instrument.AssetType,
        // ... map all fields to Saxo's structure
    }
    return saxoReq, nil
}
```

**Rule**: Saxo-specific types (prefixed with `Saxo*`) NEVER leak into public interfaces.

## OAuth2 Authentication Flow

### Token Management Pattern

**AuthClient** handles all OAuth2 complexity:
- Token persistence via `TokenStorage` interface (default: file-based)
- Automatic refresh before expiry (default: 2 minutes early)
- Background refresh goroutine (`StartAuthenticationKeeper`)

### Usage Pattern
```go
authClient := saxo.NewAuthClient(clientID, clientSecret, baseURL, tokenStorage, logger)

// Initial authentication (web flow)
authURL, _ := authClient.GenerateAuthURL("saxo", "random-state")
// User visits authURL, gets code
authClient.ExchangeCodeForToken(ctx, code, "saxo")

// Start background refresh
authClient.StartAuthenticationKeeper("saxo")

// Create broker client
brokerClient := saxo.NewSaxoBrokerClient(authClient, baseURL, logger)
```

**Token Lifecycle:**
- Access token: ~20 minutes
- Refresh token: ~7 days
- Auto-refresh: 2 minutes before expiry
- Re-authentication required if refresh token expires

## WebSocket Implementation Details

### Connection Timing (CRITICAL for Trading Apps)

**Following Legacy Pattern**: Connect 2 minutes before first market opens, not at fixed time.

**Implementation in consumer (not in library):**
```go
// Scheduler determines earliest market open time from signals
firstMarketOpen := findEarliestMarketOpen(signals)
connectTime := firstMarketOpen.Add(-2 * time.Minute)

// Wait until connect time
time.Sleep(time.Until(connectTime))

// Library handles connection, subscription, reconnection
wsClient.Connect()
wsClient.SubscribeToPrices(ctx, uics, callbacks)
```

**Why library doesn't schedule**: Scheduling is application logic. Library provides lifecycle methods, consumer decides timing.

### Binary Protocol Parsing

**Saxo WebSocket uses custom binary format**, not JSON:

**Message Structure:**
```
[MessageID (8 bytes)][ReferenceID (10 chars)][Payload (variable)]
```

**Library handles parsing internally** (`websocket/message_parser.go`):
- Extracts MessageID and ReferenceID
- Parses JSON payload
- Routes to subscription-specific callbacks

**Consumer only receives parsed data:**
```go
callbacks := saxo.PriceCallbacks{
    OnUpdate: func(data saxo.PriceUpdate) {
        fmt.Printf("Price: %s = %.5f\n", data.Ticker, data.Quote.Mid)
    },
}
```

### Reconnection Strategy

**Exponential backoff with subscription recovery:**
1. Detect disconnection (heartbeat timeout, network error)
2. Wait with exponential backoff (1s, 2s, 4s, max 30s)
3. Re-establish WebSocket connection
4. Replay all active subscriptions automatically
5. Resume callbacks as if nothing happened

**Consumer sees seamless reconnection** - callbacks continue working.

## Testing Patterns

### Unit Tests with Mock Server

**Pattern**: Use `mock_saxo_server.go` for HTTP tests, `mocktesting/` for WebSocket tests.

```go
// Example unit test structure
func TestPlaceOrder(t *testing.T) {
    mockAuth := &MockAuthClient{authenticated: true}
    client := NewSaxoBrokerClient(mockAuth, mockAuth.GetBaseURL(), logger)
    
    req := OrderRequest{
        Instrument: Instrument{Ticker: "EURUSD", Uic: 21},
        OrderType:  "Limit",
        BuySell:    "Buy",
        Amount:     10000,
        OrderPrice: 1.1000,
    }
    
    resp, err := client.PlaceOrder(context.Background(), req)
    // Assert response
}
```

### Integration Tests (Require Credentials)

**Use `-tags=integration` to separate from unit tests:**

```go
//go:build integration
// +build integration

func TestRealSaxoConnection(t *testing.T) {
    config := LoadTestConfig()  // Reads SAXO_CLIENT_ID, SAXO_CLIENT_SECRET
    if !config.IsIntegrationTestEnabled() {
        t.Skip("Integration tests disabled")
    }
    // Test against real SIM environment
}
```

**Run with:** `go test -tags=integration ./...`

## Common Integration Issues

### Issue: "not authenticated with broker"

**Cause**: Consumer called BrokerClient methods before authenticating AuthClient.

**Fix**:
```go
// WRONG
client := saxo.NewSaxoBrokerClient(authClient, baseURL, logger)
client.PlaceOrder(...)  // ❌ Fails

// CORRECT
authClient.Login(ctx)  // or ExchangeCodeForToken
client := saxo.NewSaxoBrokerClient(authClient, baseURL, logger)
client.PlaceOrder(...)  // ✅ Works
```

### Issue: WebSocket subscriptions not working after reconnection

**Cause**: Consumer didn't call `StartAuthenticationKeeper()` - tokens expired during connection.

**Fix**:
```go
authClient.StartAuthenticationKeeper("saxo")  // Start before WebSocket connect
wsClient.Connect()
```

### Issue: "invalid UIC" or "instrument not found"

**Cause**: Instrument struct missing required fields (Uic, AssetType).

**Fix**: Always populate full Instrument struct:
```go
instrument := saxo.Instrument{
    Ticker:    "EURUSD",
    Uic:       21,
    AssetType: "FxSpot",
    Symbol:    "EURUSD",
}
```

## Dependency Management

**Minimal dependencies (only 2):**
```go
require (
    github.com/gorilla/websocket v1.5.0  // WebSocket client
    golang.org/x/oauth2 v0.33.0          // OAuth2 flow
)
```

**No trading-platform dependencies** - this is a standalone library.

## Version Stability (Pre-1.0)

**Current Status:** v0.4.x - Stable development phase

**Interface Stability:**
- 🟢 **Stable (locked for v1.0)**: BrokerClient core methods, AuthClient, WebSocketClient streaming
- 🟡 **Experimental (may change)**: Advanced position management, multi-account operations

**Breaking Changes**: May occur in 0.x versions. Always check release notes when updating.

## Code Style Guidelines

**Naming Conventions:**
- Public interfaces: `BrokerClient`, `AuthClient` (no `Saxo` prefix - generic)
- Internal types: `SaxoOrderRequest`, `SaxoBrokerClient` (with `Saxo` prefix)
- Converters: `convertToSaxo*`, `convertFromSaxo*` pattern

**Error Handling:**
```go
if err != nil {
    return nil, fmt.Errorf("descriptive context: %w", err)  // Always wrap errors
}
```

**Logging:**
```go
logger.Printf("Module: Action detail: %v", data)  // Prefix with module/function name
```

## Documentation References

- **[ARCHITECTURE.md](docs/ARCHITECTURE.md)**: Complete layer architecture, interface contracts
- **[AUTHENTICATION.md](docs/AUTHENTICATION.md)**: OAuth2 flow, token management
- **[README.md](README.md)**: Installation, quick start, stability status
- **[examples/](examples/)**: Working code examples for common use cases

## When to Modify This Library vs Consumer

**Modify Library When:**
- Adding new Saxo API endpoints
- Fixing WebSocket protocol issues
- Improving reconnection logic
- Adding new generic types to interfaces

**Modify Consumer When:**
- Implementing trading strategy logic
- Scheduling when to connect/disconnect
- UI/web handlers
- Business-specific data persistence

**Rule**: Library provides capabilities, consumer orchestrates them.
