# Saxo Adapter Architecture

## Design Philosophy

This adapter implements a **clean separation between generic broker operations and Saxo-specific implementation details**. Trading strategies and applications interact with generic, broker-agnostic interfaces while the adapter handles all Saxo Bank API complexity internally.

---

## Layer Architecture

```
┌─────────────────────────────────────────────────────────┐
│                   Trading Application                   │
└─────────────────────┬───────────────────────────────────┘
                      │
                      │ Uses generic types only
                      │ (OrderRequest, Instrument, etc.)
                      │
┌─────────────────────▼───────────────────────────────────┐
│              GENERIC INTERFACE LAYER                    │
│                (adapter/interfaces.go)                  │
│                                                         │
│  • BrokerClient interface                               │
│  • AuthClient interface                                 │
│  • WebSocketClient interface                            │
│                                                         │
│  Generic Types:                                         │
│  • Instrument, OrderRequest, OrderResponse              │
│  • LiveOrder, PriceUpdate, AccountInfo                  │
└─────────────────────┬───────────────────────────────────┘
                      │
                      │ Implements interfaces
                      │
┌─────────────────────▼───────────────────────────────────┐
│              CONVERSION LAYER                           │
│         (saxo.go, oauth.go, market_data.go)             │
│                                                         │
│  • convertToSaxoOrder(OrderRequest) → SaxoOrderRequest  │
│  • convertFromSaxoResponse() → OrderResponse            │
│  • Generic → Saxo-specific conversions                  │
└─────────────────────┬───────────────────────────────────┘
                      │
                      │ Uses Saxo-specific types
                      │
┌─────────────────────▼───────────────────────────────────┐
│              SAXO-SPECIFIC LAYER                        │
│                 (adapter/types.go)                      │
│                                                         │
│  • SaxoOrderRequest                                     │
│  • SaxoOrderResponse                                    │
│  • SaxoBalance, SaxoAccounts                            │
│  • SaxoTradingSchedule                                  │
│  • All Saxo Bank API structures                         │
└─────────────────────┬───────────────────────────────────┘
                      │
                      │ HTTP/WebSocket
                      │
┌─────────────────────▼───────────────────────────────────┐
│                  Saxo Bank OpenAPI                      │
│              (gateway.saxobank.com)                     │
└─────────────────────────────────────────────────────────┘
```

---

## Interface Contracts

### BrokerClient Interface
```go
type BrokerClient interface {
    // Orders
    PlaceOrder(ctx, OrderRequest) (*OrderResponse, error)
    ModifyOrder(ctx, OrderModificationRequest) (*OrderResponse, error)
    CancelOrder(ctx, CancelOrderRequest) error
    ClosePosition(ctx, ClosePositionRequest) (*OrderResponse, error)
    DeleteOrder(ctx, orderID string) error
    GetOrderStatus(ctx, orderID string) (*OrderStatus, error)
    
    // Queries
    GetOpenOrders(ctx) ([]LiveOrder, error)
    GetOpenPositions(ctx) (*OpenPositionsResponse, error)
    GetNetPositions(ctx) (*NetPositionsResponse, error)
    GetClosedPositions(ctx) (*ClosedPositionsResponse, error)
    
    // Account
    GetBalance(ctx) (*Balance, error)
    GetAccounts(ctx) (*Accounts, error)
    GetMarginOverview(ctx, clientKey string) (*MarginOverview, error)
    GetClientInfo(ctx) (*ClientInfo, error)
    
    // Market Data
    GetTradingSchedule(ctx, params) (*TradingSchedule, error)
    SearchInstruments(ctx, params) ([]Instrument, error)
    GetInstrumentDetails(ctx, uics []int) ([]InstrumentDetail, error)
    GetInstrumentPrices(ctx, uics []int, fieldGroups string) ([]InstrumentPriceInfo, error)
    GetInstrumentPrice(ctx, Instrument) (*PriceData, error)
    GetHistoricalData(ctx, Instrument, days int) ([]HistoricalDataPoint, error)
}
```

### AuthClient

```go
type AuthClient interface {
    Login(ctx) error
    Logout() error
    RefreshToken(ctx) error
    IsAuthenticated() bool
    GetAccessToken() (string, error)
    GetHTTPClient(ctx) (*http.Client, error)
    GetBaseURL() string
    GetWebSocketURL() string
    StartAuthenticationKeeper(provider string)
    StartTokenEarlyRefresh(ctx, wsConnected <-chan bool, wsContextID <-chan string)
}
```

### WebSocketClient
```go
type WebSocketClient interface {
    Connect(ctx) error
    Close() error
    
    // Subscriptions
    SubscribeToPrices(ctx, instruments []string) error
    SubscribeToOrders(ctx) error
    SubscribeToPortfolio(ctx) error
    SubscribeToSessionEvents(ctx) error
    
    // Channels
    GetPriceUpdateChannel() <-chan PriceUpdate
    GetOrderUpdateChannel() <-chan OrderUpdate
    GetPortfolioUpdateChannel() <-chan PortfolioUpdate
    
    SetStateChannels(stateChannel chan<- bool, contextIDChannel chan<- string)
}
```

**Key change in v0.4.0**: `SubscribeToPrices` now accepts UICs directly as strings ("21", "31") or ticker names.

## Generic Types

### OrderRequest (BREAKING CHANGE in v0.4.0)
```go
type OrderRequest struct {
    AccountKey string      // NEW: Required field
    Instrument Instrument
    Side       string      // "Buy" or "Sell"
    Size       int
    Price      float64
    OrderType  string      // "Market", "Limit", "StopIfTraded"
    Duration   string      // "DayOrder", "GoodTillDate"
}
```

### Instrument
```go
type Instrument struct {
    Ticker     string   // "EURUSD"
    Identifier int      // UIC (Saxo-specific ID)
    AssetType  string   // "FxSpot", "ContractFutures"
    Exchange   string
    Currency   string
    TickSize   float64
    Decimals   int
}
```

### PriceUpdate
```go
type PriceUpdate struct {
    Ticker    string
    Bid       float64
    Ask       float64
    Mid       float64
    Timestamp time.Time
}
```

## Layer Architecture

```
Application Code (broker-agnostic)
         ↓
Generic Interfaces (BrokerClient, AuthClient, WebSocketClient)
         ↓
Conversion Layer (convertToSaxoOrder, convertFromSaxoResponse)
         ↓
Saxo-Specific Types (SaxoOrderRequest, SaxoBalance)
         ↓
Saxo Bank OpenAPI
```

## Implementation Pattern

## Implementation Pattern

```go
// Public method (generic interface)
func (sbc *SaxoBrokerClient) PlaceOrder(ctx context.Context, req OrderRequest) (*OrderResponse, error) {
    // Validate
    if !sbc.authClient.IsAuthenticated() {
        return nil, fmt.Errorf("not authenticated")
    }
    
    // Convert generic → Saxo-specific
    saxoReq := convertToSaxoOrder(req)
    
    // Call Saxo API
    resp := callSaxoAPI(saxoReq)
    
    // Convert Saxo-specific → generic
    return convertFromSaxoResponse(resp), nil
}
```

## OAuth Flow

```
1. authClient.Login()
2. Browser opens → User authenticates
3. Token saved to data/saxo_token.bin
4. StartAuthenticationKeeper() → Auto-refresh every 58min
5. StartTokenEarlyRefresh() → Auto-refresh every 18min (WebSocket)
```

## WebSocket Architecture

```
┌──────────────────────────────────────────────────────────┐
│              SaxoWebSocketClient                         │
└───┬──────────────────────────────────────────────────┬───┘
    │                                                  │
    │                                                  │
┌───▼─────────────────┐                    ┌──────────▼────────┐
│ ConnectionManager   │                    │ SubscriptionManager│
│                     │                    │                    │
│ • Connect/Reconnect │                    │ • Price feeds      │
│ • Heartbeat         │                    │ • Order updates    │
│ • Error handling    │                    │ • Portfolio        │
└───┬─────────────────┘                    └──────────┬────────┘
    │                                                  │
    │                                                  │
    │              ┌───────────────────┐              │
    └──────────────►  MessageHandler   ◄──────────────┘
                   │                   │
                   │ • Parse messages  │
                   │ • Route to channels
                   │ • Error handling  │
                   └───────┬───────────┘
                           │
                   ┌───────▼───────────┐
                   │  Output Channels  │
                   │                   │
                   │ • PriceUpdates    │
                   │ • OrderUpdates    │
                   │ • PortfolioUpdates│
                   └───────────────────┘
```

**Key feature**: Automatic reconnection with subscription recovery.

## Thread Safety

- Token access: mutex-protected
- WebSocket channels: goroutine-safe
- Subscription map: mutex-protected
- HTTP client: reused (connection pooling)

## Multi-Broker Pattern

Same interface works with any broker:

```go
var broker BrokerClient

switch config.Broker {
case "saxo":
    broker = saxo.NewSaxoBrokerClient(authClient, logger)
case "ibkr":
    broker = ibkr.NewIBKRBrokerClient(authClient, logger)
}

// Same code, any broker
broker.PlaceOrder(ctx, order)
```

## v0.4.0 Migration Guide

### Breaking Changes

**1. OrderRequest requires AccountKey**

Before:
```go
order := OrderRequest{
    Instrument: Instrument{Ticker: "EURUSD", Identifier: 21, AssetType: "FxSpot"},
    Side: "Buy",
    Size: 1000,
}
```

After:
```go
accounts, _ := brokerClient.GetAccounts(ctx)
accountKey := accounts.Data[0].AccountKey

order := OrderRequest{
    AccountKey: accountKey,  // NEW: Required
    Instrument: Instrument{Ticker: "EURUSD", Identifier: 21, AssetType: "FxSpot"},
    Side: "Buy",
    Size: 1000,
}
```

**2. WebSocket SubscribeToPrices accepts UICs directly**

Before (required instrument mapping):
```go
wsClient.RegisterInstruments(instruments)
wsClient.SubscribeToPrices(ctx, []string{"EURUSD", "USDJPY"})
```

After (UICs as strings work directly):
```go
wsClient.SubscribeToPrices(ctx, []string{"21", "31", "1"})
// Creates automatic mapping: UIC 21 → ticker "21"
```

### Examples Updated

All examples (`basic_auth`, `place_order`, `websocket_prices`) work with v0.4.0.

## Testing

```bash
# Unit tests
go test ./adapter/...

# Integration tests (requires credentials)
export SAXO_ENVIRONMENT=sim
export SAXO_CLIENT_ID=your_id
export SAXO_CLIENT_SECRET=your_secret
go test ./adapter -v -run Integration
```

## Performance

- Token caching: in-memory + file persistence
- HTTP connection pooling: automatic
- WebSocket: single connection for all subscriptions
- Message batching: 1-second refresh rate

## Summary

✅ Clean interfaces for multi-broker support  
✅ Type-safe conversions  
✅ Automatic authentication  
✅ WebSocket with auto-reconnection  
✅ Production-ready for algorithmic trading
