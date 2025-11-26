# Saxo Bank Adapter for Go

**Standalone Saxo Bank OpenAPI adapter for Go** - OAuth2 authentication, REST API client, and WebSocket streaming.

## ⚠️ Pre-1.0 Status: Experimental Development

**Current Version:** `v0.2.0-dev` (Pre-release)  
**Status:** Active development, API may change frequently

> **Warning:** This library is in the **0.x pre-release phase**. Breaking changes may occur in minor versions (0.x.0) as we refine the API based on real-world usage. Pin to exact versions in production: `require github.com/bjoelf/saxo-adapter v0.2.5`

### What Works ✅
- ✅ OAuth2 authentication with automatic token refresh
- ✅ RESTful API client for orders, positions, and market data
- ✅ WebSocket streaming for real-time price feeds
- ✅ Fully self-contained - no imports from pivot-web2
- ✅ All core types and interfaces defined locally

### Interface Stability Levels

#### 🟢 Stable (Unlikely to change before v1.0)
- `BrokerClient` core methods (PlaceOrder, GetBalance, GetAccounts)
- `AuthClient` authentication methods
- `WebSocketClient` basic streaming operations

#### 🟡 Experimental (May change before v1.0)
- Extended trading methods (ModifyOrder, ClosePosition)
- Market data retrieval methods
- Advanced WebSocket subscriptions

#### 🔵 Planned (Not yet implemented)
- Margin calculation methods
- Risk analysis features
- Advanced charting data
- Multi-leg order support

### Path to v1.0.0 🎯

**Target:** Q2 2026 (after 6+ months of production validation)

We will release v1.0.0 when:
- Core interfaces stable for 6+ months
- Multiple production deployments validated
- Comprehensive test coverage (>80%)
- Complete documentation with examples
- Community feedback incorporated

**Roadmap:**
- `v0.3.0` (Dec 2025) - Stabilize core interfaces, add examples
- `v0.4.0` (Jan 2026) - Add margin calculation methods
- `v0.5.0` (Feb 2026) - Add risk analysis features
- `v0.6.0` (Mar 2026) - Feature freeze, stabilization period
- `v1.0.0-rc1` (Apr 2026) - Release candidate testing
- `v1.0.0` (May 2026) - Full stability guarantees begin

### 💡 Feature Requests Welcome!

We're actively gathering requirements for the v1.0 API. If you need specific broker operations:

**Please open an issue with:**
- Feature description (e.g., "Need margin calculation for futures")
- Use case (why you need it)
- Expected interface (how you'd like to use it)

This helps us design the right abstractions before locking down the API in v1.0.0.

**Open an issue:** https://github.com/bjoelf/saxo-adapter/issues/new

## Installation

```bash
go get github.com/bjoelf/saxo-adapter@latest
```

## Quick Start

```go
package main

import (
    "log"
    saxo "github.com/bjoelf/saxo-adapter/adapter"
)

func main() {
    logger := log.Default()
    
    // Create Saxo broker services (auth + broker client)
    authClient, brokerClient, err := saxo.CreateBrokerServices(logger)
    if err != nil {
        log.Fatal(err)
    }
    
    // Use the clients...
    _ = authClient
    _ = brokerClient
}
```

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

## Configuration

Set these environment variables:

```bash
# Environment (sim or live)
export SAXO_ENVIRONMENT=sim

# Credentials
export SAXO_CLIENT_ID=your_client_id
export SAXO_CLIENT_SECRET=your_secret
```

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
│   ├── saxo.go          # Main broker client (846 lines)
│   ├── market_data.go   # Market data client (375 lines)
│   ├── token_storage.go # Token persistence
│   └── websocket/       # WebSocket client (2,584 lines)
│       ├── saxo_websocket.go
│       ├── connection_manager.go
│       ├── subscription_manager.go
│       ├── message_handler.go
│       └── mocktesting/
└── docs/                # Documentation
```

## Dependencies

**External packages**:
- `golang.org/x/oauth2` - OAuth2 authentication
- `github.com/gorilla/websocket` - WebSocket client

**No internal dependencies** - This is a standalone adapter that can be imported by any Go project.

## Development

### Contributing

We welcome contributions! Since we're in pre-1.0 development, we're especially interested in:

**🎯 Feature Requests**
- What broker operations do you need?
- What use cases should we support?
- What would make the API more intuitive?

**🐛 Bug Reports**
- Issues with authentication
- WebSocket connection problems
- API incompatibilities

**📖 Documentation**
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
- **Issues & Feature Requests:** https://github.com/bjoelf/saxo-adapter/issues
- **Discussions:** https://github.com/bjoelf/saxo-adapter/discussions

## Acknowledgments

This adapter was extracted from the [pivot-web2](https://github.com/bjoelf/pivot-web2) trading platform to serve as a standalone, reusable library for the Go community.

## Changelog

See [CHANGELOG.md](CHANGELOG.md) for version history and breaking changes.

---

**Questions?** Open an issue or start a discussion!  
**Need a feature?** Tell us about your use case - we're designing the v1.0 API now!
