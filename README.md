# Saxo Bank Adapter for Go

**Standalone Saxo Bank OpenAPI adapter for Go** - OAuth2 authentication, REST API client, and WebSocket streaming.

## ⚠️ Pre-1.0 Status: Stable Development

**Current Version:** `v0.4.0` 🎉  
**Release Date:** December 2, 2025  
**Status:** Core features stable, API refinement ongoing

> **Note:** This library is in the **0.x stable phase**. 

### What Works ✅

- ✅ OAuth2 authentication with automatic token refresh
- ✅ RESTful API client for orders, positions, and market data
- ✅ Order modification (trailing stops, market conversions)
- ✅ Historical chart data with 1-hour caching
- ✅ WebSocket streaming for real-time updates:
  - Price feeds (`SubscribeToPrices`)
  - Order status updates (`SubscribeToOrders`)
  - Portfolio balance (`SubscribeToPortfolio`)
  - Session events (`SubscribeToSessionEvents`)
- ✅ Automatic WebSocket reconnection with subscription recovery
- ✅ All core types and interfaces defined locally

### Interface Stability Levels

#### 🟢 Stable (Production-ready, locked for v1.0)

- `BrokerClient` core methods (PlaceOrder, ModifyOrder, GetBalance, GetAccounts)
- `AuthClient` authentication methods (Login, GetAccessToken, token refresh)
- `WebSocketClient` streaming operations:
  - SubscribeToPrices, SubscribeToOrders, SubscribeToPortfolio, SubscribeToSessionEvents
  - Connection management, automatic reconnection
- `MarketDataClient` methods:
  - GetHistoricalData (with caching), GetTradingSchedule

#### 🟡 Experimental (May change before v1.0)

- Advanced position management (GetNetPositions, GetClosedPositions)
- Multi-account operations
- Complex order types (OCO, brackets)

## Installation

```bash
go get github.com/bjoelf/saxo-adapter@latest
```

## Configuration to get examples running

1. You need a developer account with Saxo Bank
2. You need to create a client (for demo environment)
3. You need to set a Redirect URL to (http://localhost:8080/oauth/callback)

For details on client creation:
<https://github.com/SaxoBank/openapi-samples-csharp/tree/master/authentication/Authentication_CodeFlow>

### Export Environment Variables

```bash
# Export your Saxo credentials to your shell
export SAXO_ENVIRONMENT=sim
export SAXO_CLIENT_ID="your_client_id_here"
export SAXO_CLIENT_SECRET="your_client_secret_here"

# Verify they're loaded
echo "Environment: $SAXO_ENVIRONMENT"
echo "Client ID: $SAXO_CLIENT_ID"
echo "Client Secret: $SAXO_CLIENT_SECRET"

# Now run oauth examples
cd /home/bjorn/dev/saxo-adapter
go run ./examples/basic_auth/main.go
#

# This will open a browser and show Saxo's regular oauth login page.
# This variable deermine saxo enviroment: export SAXO_ENVIRONMENT=sim

# For live trading enviroment change to
# export SAXO_ENVIRONMENT=live
# Warning! live enviroment is... live. 
# Broker will execute order with your accounts money.

# There are four examples in /examples/ folder
# To run any other change path to main.go. 
# For example:
# go run ./examples/place_order/main.go

```
## Note: 
For an example with persistent SAXO_CLIENT_ID and SAXO_CLIENT_SECRET variables 
using an .env file please look at: 

(https://github.com/bjoelf/fx-collector)


## Features

### OAuth2 Authentication

- Automatic token refresh
- SIM and LIVE environment support
- Secure token storage

### REST API Client

- Order placement and management
- Position and portfolio tracking  
- Market data retrieval
- Trading schedule queries

### WebSocket Streaming

- Real-time price updates
- Order status notifications
- Portfolio balance updates
- Robust reconnection handling

## Architecture

This adapter follows clean architecture principles with a focus on **interface stability during pre-1.0 development**.

### Design Philosophy

**Pre-1.0 Strategy:**

- **Core interfaces** kept minimal and stable (PlaceOrder, GetBalance, etc.)
- **Extension interfaces** for advanced features (can evolve in 0.x versions)
- **Planned interfaces** documented but not enforced yet
- **Breaking changes acceptable** in 0.x versions (semver-compliant)

**Post-1.0 Strategy:**

- Core interfaces locked (changes only in major versions)
- New features added via optional extension interfaces
- Full semantic versioning guarantees

### Interface Organization

```go
// ============================================================================
// STABLE CORE (v0.x) - Minimal changes expected
// ============================================================================

type BrokerClient interface {
    PlaceOrder(ctx context.Context, req OrderRequest) (*OrderResponse, error)
    CancelOrder(ctx context.Context, req CancelOrderRequest) error
    GetBalance(force bool) (*SaxoPortfolioBalance, error)
    GetAccounts(force bool) (*SaxoAccounts, error)
    GetOpenOrders(ctx context.Context) ([]LiveOrder, error)
}

type AuthClient interface {
    Login(ctx context.Context) error
    IsAuthenticated() bool
    GetAccessToken() (string, error)
    // ... other stable auth methods
}

// ============================================================================
// EXPERIMENTAL (v0.x) - May change before v1.0
// ============================================================================

type MarketDataClient interface {
    GetHistoricalData(ctx, instrument, days) ([]HistoricalDataPoint, error)
    GetTradingSchedule(params) (SaxoTradingSchedule, error)
    // May add more methods in 0.x versions
}

// ============================================================================
// PLANNED (Future) - Interface draft, not implemented
// ============================================================================

// Coming in v0.4.0
type MarginCalculator interface {
    GetMarginRequirement(ctx, instrument) (float64, error)
    CalculatePositionMargin(ctx, position) (float64, error)
}
```

### Interface Evolution Pattern

**During 0.x (Now → v1.0):**

- Can add methods to interfaces (breaking changes OK)
- Document changes in CHANGELOG.md
- Provide migration guides for breaking changes

**After v1.0:**

- Core interfaces frozen
- New features via extension interfaces only
- Type assertions for optional capabilities:

  ```go
  if calc, ok := client.(MarginCalculator); ok {
      margin, _ := calc.GetMarginRequirement(ctx, instrument)
  }
  ```

### Versioning Policy

**Pre-1.0 (Current):**

- `v0.x.0 → v0.(x+1).0` - May include breaking changes
- `v0.3.x → v0.3.(x+1)` - Backward compatible additions/fixes
- Pin exact versions: `require github.com/bjoelf/saxo-adapter v0.3.5`

**Post-1.0 (Future):**

- `v1.x.x → v2.0.0` - Breaking changes (rare, with migration guide)
- `v1.0.x → v1.1.0` - New extension interfaces (non-breaking)
- `v1.1.x → v1.1.y` - Bug fixes only

### Interfaces (Contracts)

- `AuthClient` - OAuth2 authentication
- `BrokerClient` - Order and position management
- `MarketDataClient` - Market data retrieval
- `WebSocketClient` - Real-time streaming

### Types

- Generic types: `Instrument`, `OrderRequest`, `OrderResponse`, etc.
- Saxo-specific types: `SaxoOrderRequest`, `SaxoBalance`, etc.

### Implementations

- `SaxoAuthClient` - Implements `AuthClient`
- `SaxoBrokerClient` - Implements `BrokerClient`  
- `SaxoWebSocketClient` - Implements `WebSocketClient`

## Directory Structure

```
saxo-adapter/
├── adapter/              # Main adapter implementation
│   ├── interfaces.go    # Interface definitions (contracts)
│   ├── types.go         # Saxo-specific types
│   ├── oauth.go         # OAuth2 authentication (672 lines)
│   ├── saxo.go          # Main broker client (838 lines, includes ModifyOrder)
│   ├── market_data.go   # Market data client (375 lines, includes GetHistoricalData)
│   ├── token_storage.go # Token persistence
│   └── websocket/       # WebSocket client (2,800+ lines)
│       ├── saxo_websocket.go        # Main client with 4 subscription methods
│       ├── connection_manager.go    # Reconnection logic
│       ├── subscription_manager.go  # All 4 Saxo subscriptions (prices, orders, portfolio, sessions)
│       ├── message_handler.go       # Message routing
│       └── mocktesting/             # Test infrastructure
└── docs/                # Documentation
    ├── ARCHITECTURE.md
    ├── AUTHENTICATION.md
    └── COMPLETION_STATUS.md
```

## Dependencies

**External packages**:

- `golang.org/x/oauth2` - OAuth2 authentication
- `github.com/gorilla/websocket` - WebSocket client

**No internal dependencies** - This is a standalone adapter that can be imported by any Go project.

## Development

### Contributing

We welcome contributions! Since we're in pre-1.0 development, we're especially interested in:

## 🎯 Feature Requests**

- What broker operations do you need?
- What use cases should we support?
- What would make the API more intuitive?

## 🐛 Bug Reports**

- Issues with authentication
- WebSocket connection problems
- API incompatibilities

## 📖 Documentation**

- Usage examples
- Integration guides
- Best practices

**Please open an issue before submitting large PRs** - We may be redesigning that area!

### Build

```bash
go build ./...
```

### Test  

```bash
go test ./adapter/...
```

### Run Integration Tests

```bash
# Set environment variables first
export SAXO_ENVIRONMENT=sim
export SAXO_CLIENT_ID=your_id
export SAXO_CLIENT_SECRET=your_secret

go test ./adapter -v -run Integration
```

### Version Management

**For Consumers:**
Pin to exact versions during 0.x phase:

```go
// go.mod
require github.com/bjoelf/saxo-adapter v0.3.5
```

**For Maintainers:**

- Update CHANGELOG.md with every release
- Tag releases: `git tag v0.3.0 && git push --tags`
- Mark breaking changes: `[BREAKING]` in changelog

## Stability Commitment

**What we promise:**

- ✅ Core trading operations (PlaceOrder, GetBalance) will remain stable
- ✅ All breaking changes documented in CHANGELOG.md
- ✅ Migration guides for version updates
- ✅ No silent breaking changes

**What we don't promise (until v1.0):**

- ⚠️ Interface signatures may change in minor versions (0.x.0)
- ⚠️ Experimental features may be redesigned
- ⚠️ Planned features may be removed if not needed

**After v1.0.0:**

- Full semantic versioning guarantees
- Breaking changes only in major versions
- Deprecation warnings before removal
- Long-term support commitment

## License

MIT License - See LICENSE file

## References

- **Documentation:** See `docs/` directory
  - [ARCHITECTURE.md](docs/ARCHITECTURE.md) - Design philosophy and patterns
  - [AUTHENTICATION.md](docs/AUTHENTICATION.md) - OAuth2 setup guide
  - [COMPLETION_STATUS.md](docs/COMPLETION_STATUS.md) - Implementation status
- **Issues & Feature Requests:** <https://github.com/bjoelf/saxo-adapter/issues>
- **Discussions:** <https://github.com/bjoelf/saxo-adapter/discussions>

## Changelog

See [CHANGELOG.md](CHANGELOG.md) for version history and breaking changes.

---

**Questions?** Open an issue or start a discussion!  
**Need a feature?** Tell us about your use case - we're designing the v1.0 API now!
