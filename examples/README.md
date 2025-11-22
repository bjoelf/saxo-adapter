# Saxo Adapter - Examples

This directory contains working examples demonstrating **broker-agnostic programming** using the saxo-adapter library's generic interfaces.

## 🎯 Design Philosophy

These examples follow **clean architecture principles**:

- ✅ **Code uses generic interfaces** (`BrokerClient`, `AuthClient`, `WebSocketClient`)
- ✅ **Broker-agnostic types** (`OrderRequest`, `Instrument`, `PriceUpdate`)
- ✅ **Easy to swap brokers** - Change Saxo to Interactive Brokers without changing application code
- ✅ **Testable** - Mock interfaces for unit testing
- ✅ **Professional** - Industry-standard dependency inversion principle

**Key Insight**: Application code should NEVER import broker-specific types. Always use the generic interface layer!

## Prerequisites

Before running these examples, ensure you have:

1. **Go 1.21 or higher** installed
2. **Saxo Bank Developer Account** (SIM or LIVE)
   - Register at: https://www.developer.saxo/
   - Create an OpenAPI application
   - Get your Client ID and Secret

3. **Environment Variables** set up:

```bash
# Required
export SAXO_ENVIRONMENT=sim           # or "live"
export SIM_CLIENT_ID=your_client_id
export SIM_CLIENT_SECRET=your_secret

# Optional
export TOKEN_STORAGE_PATH=./data      # Default: ./data
export PROVIDER=saxo                  # Default: saxo
```

Alternatively, create a `.env` file in the saxo-adapter root directory:

```bash
SAXO_ENVIRONMENT=sim
SIM_CLIENT_ID=your_client_id_here
SIM_CLIENT_SECRET=your_secret_here
TOKEN_STORAGE_PATH=./data
PROVIDER=saxo
```

## Examples

### 1. Basic Authentication (`basic_auth/`)

**Purpose**: Demonstrates broker-agnostic OAuth2 authentication using generic interfaces.

**What it does**:
- Uses `adapter.AuthClient` interface (not Saxo-specific!)
- Uses `adapter.BrokerClient` interface for account operations
- Shows how to swap broker implementations without code changes
- Demonstrates interface-based programming best practices

**Key Code Pattern**:
```go
// Declare using generic interfaces
var authClient adapter.AuthClient
var brokerClient adapter.BrokerClient

// Factory creates Saxo implementation (configuration layer)
authClient, brokerClient, _ = adapter.CreateBrokerServices(logger)

// From here on: use ONLY generic interface methods!
authClient.Login(ctx)                    // Generic method
balance, _ := brokerClient.GetBalance()  // Generic method
```

**Run it**:
```bash
cd examples/basic_auth
go run main.go
```

**Expected output**:
```
=== Saxo Adapter - Basic Authentication Example ===

This example demonstrates broker-agnostic authentication
using generic interfaces (AuthClient, BrokerClient)

Creating broker services...
✅ Broker services created successfully

Starting authentication...
⚠️  A browser window will open for broker login
✅ Authentication successful!

Verifying authentication by fetching account balance...

=== Account Information ===
💰 Account Balance: 100000.00 USD
📊 Cash Balance:    100000.00 USD

✅ Session is authenticated and active

Key Takeaways:
  - Application code uses only generic interfaces
  - AuthClient.Login() works with any broker
  - BrokerClient.GetBalance() is broker-agnostic
  - Easy to swap Saxo for another broker implementation
```

---

### 2. Place Order (`place_order/`)

**Purpose**: Demonstrates broker-agnostic order placement using generic types.

**What it does**:
- Uses `adapter.OrderRequest` (generic type - works with ANY broker!)
- Uses `adapter.Instrument` (broker-agnostic instrument representation)
- Places order via `BrokerClient.PlaceOrder()` interface method
- Receives `adapter.OrderResponse` (generic response type)

**Key Code Pattern**:
```go
// Create order using GENERIC types (not Saxo-specific!)
order := adapter.OrderRequest{
    Instrument: adapter.Instrument{
        Ticker:     "EURUSD",
        Identifier: 21,           // Broker-specific ID
        AssetType:  "FxSpot",
    },
    Side:      "Buy",
    Size:      1000,
    OrderType: "Market",
    Duration:  "DayOrder",
}

// Place order using GENERIC interface method
var brokerClient adapter.BrokerClient
response, _ := brokerClient.PlaceOrder(ctx, order)

// Receive GENERIC response type
fmt.Printf("Order ID: %s, Status: %s\n", response.OrderID, response.Status)
```

**Run it**:
```bash
cd examples/place_order
go run main.go
```

**Expected output**:
```
=== Saxo Adapter - Place Order Example ===

This example demonstrates broker-agnostic order placement
using generic interfaces (BrokerClient, OrderRequest)

Authenticating...
✅ Authenticated successfully

💰 Current Balance: 100000.00 USD

Order Details:
  Instrument: EURUSD (ID: 21)
  Side:       Buy
  Size:       1000 units
  Type:       Market
  Duration:   DayOrder

Placing order...
✅ Order placed successfully!
  Order ID:   12345678
  Status:     Filled

📋 Open Orders: 0

Key Takeaways:
  - OrderRequest is a generic, broker-agnostic type
  - BrokerClient.PlaceOrder() interface is the same for all brokers
  - OrderResponse is generic - no Saxo-specific details leaked
  - Same code works with Interactive Brokers, Alpaca, etc.
```

**⚠️ Warning**: This example places a **real order** (in SIM environment). Always use the SIM environment for testing!

---

### 3. WebSocket Price Subscription (`websocket_prices/`)

**Purpose**: Demonstrates broker-agnostic real-time data streaming via generic WebSocket interface.

**What it does**:
- Uses `adapter.WebSocketClient` interface (not Saxo-specific!)
- Subscribes to real-time prices using generic interface methods
- Receives `adapter.PriceUpdate` (generic price update type)
- Works with ANY broker that implements WebSocketClient

**Key Code Pattern**:
```go
// Create WebSocket client using GENERIC interface
var wsClient adapter.WebSocketClient
wsClient = adapter.NewSaxoWebSocketClient(authClient, logger)

// Connect using GENERIC interface method
wsClient.Connect(ctx)

// Subscribe using GENERIC interface method
wsClient.SubscribeToPrices(ctx, []string{"21", "31", "1"})

// Receive GENERIC price updates
priceChannel := wsClient.GetPriceUpdateChannel()
for price := range priceChannel {
    // Generic PriceUpdate type with Ticker, Bid, Ask, Mid
    fmt.Printf("%s: Bid=%.5f Ask=%.5f\n", price.Ticker, price.Bid, price.Ask)
}
```

**Run it**:
```bash
cd examples/websocket_prices
go run main.go
```

**Expected output**:
```
=== Saxo Adapter - WebSocket Price Subscription Example ===

This example demonstrates broker-agnostic real-time data streaming
using the generic WebSocketClient interface

Authenticating...
✅ Authenticated successfully

Connecting to WebSocket...
✅ WebSocket connected successfully

Subscribing to price feeds:
  - EURUSD (ID 21)
  - USDJPY (ID 31)
  - GBPUSD (ID 1)
✅ Subscribed to price feeds

📊 Listening to real-time prices... (Press Ctrl+C to stop)

Instrument | Bid      | Ask      | Spread   | Time
-----------|----------|----------|----------|---------------------
EURUSD     | 1.08450  | 1.08452  | 0.00002  | 14:23:15
USDJPY     | 149.123  | 149.125  | 0.00200  | 14:23:15
GBPUSD     | 1.26345  | 1.26348  | 0.00003  | 14:23:16
...

Key Takeaways:
  - WebSocketClient is a generic, broker-agnostic interface
  - PriceUpdate is a generic type (Ticker, Bid, Ask, Mid)
  - Same code works with any broker implementing the interface
  - Easy to mock WebSocketClient for testing
```

**Stop it**: Press `Ctrl+C` or wait 30 seconds for automatic shutdown.

---

## Common Issues

### "Failed to create broker services: missing environment variable"

**Solution**: Ensure all required environment variables are set:
```bash
export SAXO_ENVIRONMENT=sim
export SIM_CLIENT_ID=your_client_id
export SIM_CLIENT_SECRET=your_secret
```

### "Authentication failed: invalid_client"

**Cause**: Incorrect Client ID or Secret.

**Solution**: 
1. Verify credentials in Saxo Developer Portal
2. Ensure you're using SIM credentials with `SAXO_ENVIRONMENT=sim`
3. Check for extra spaces or quotes in environment variables

### "Browser doesn't open for OAuth2 login"

**Solution**: 
1. Check the terminal output for the authentication URL
2. Manually copy and paste the URL into your browser
3. After login, you'll be redirected to localhost with a code
4. The adapter will capture the code automatically

### "WebSocket connection failed"

**Possible causes**:
1. Invalid or expired authentication token
2. Network/firewall blocking WebSocket connections
3. Saxo API outage

**Solution**:
1. Delete token file and re-authenticate: `rm -rf ./data/saxo_token.bin`
2. Check your network allows WebSocket connections (port 443)
3. Check Saxo API status: https://www.developer.saxo/status

---

## Next Steps

After running these examples:

1. **Understand the architecture benefits**:
   - Your application code is **broker-agnostic**
   - You can swap Saxo for another broker without changing business logic
   - Interfaces make testing easy (mock `BrokerClient`, `AuthClient`, etc.)
   - Clean separation: configuration layer vs application layer

2. **Customize for your use case**:
   ```go
   // Application layer - broker-agnostic!
   type TradingBot struct {
       broker adapter.BrokerClient  // Generic interface
       auth   adapter.AuthClient    // Generic interface
   }
   
   // Works with ANY broker implementation
   func (bot *TradingBot) PlaceOrder(order adapter.OrderRequest) {
       response, _ := bot.broker.PlaceOrder(ctx, order)
       // ... business logic ...
   }
   ```

3. **Read the architecture documentation**:
   - [Architecture Guide](../docs/ARCHITECTURE.md) - Understand the layer design
   - [Completion Status](../docs/COMPLETION_STATUS.md) - Feature coverage
   - [API Interfaces](../adapter/interfaces.go) - Complete interface definitions

4. **Explore multi-broker support** (future):
   ```go
   // Configuration layer determines which broker
   var brokerClient adapter.BrokerClient
   
   if os.Getenv("BROKER") == "saxo" {
       brokerClient = createSaxoClient()
   } else if os.Getenv("BROKER") == "ib" {
       brokerClient = createInteractiveBrokersClient()
   }
   
   // Application code stays the same!
   brokerClient.PlaceOrder(ctx, order)
   ```

---

## Support

- **GitHub Issues**: https://github.com/bjoelf/saxo-adapter/issues
- **Saxo API Docs**: https://www.developer.saxo/openapi/learn
- **API Status**: https://www.developer.saxo/status

---

## License

See [LICENSE](../LICENSE) file in the root directory.
