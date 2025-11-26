# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.3.0] - 2025-11-26 🎉

### Added
- ✅ **SubscribeToSessionEvents()** - WebSocket subscription for session events via HTTP POST to `/root/v1/sessions/events/subscriptions/active`
  - Added to `WebSocketClient` interface
  - Implements Saxo session monitoring (connection status, auth changes)
  - Includes automatic resubscription on reconnection
  - **Location**: `adapter/websocket/subscription_manager.go`, `adapter/websocket/saxo_websocket.go`

- ✅ **GetNetPositions()** - Retrieve aggregated net positions
  - Endpoint: `GET /port/v1/netpositions/me`
  - Aggregates multiple individual positions of same instrument
  - Example: 3 long EURUSD positions → 1 net position showing total exposure
  - **Location**: `adapter/saxo.go`

- ✅ **GetClosedPositions()** - Retrieve closed positions for P&L reporting
  - Endpoint: `GET /port/v1/closedpositions/me`
  - Returns trade history with P&L details
  - **Location**: `adapter/saxo.go`

### Stabilized (Moved from Experimental to Stable)
- 🟢 **ModifyOrder()** - Complete HTTP PATCH implementation for order modifications
  - Endpoint: `PATCH /trade/v2/orders/{OrderID}`
  - Supports trailing stop updates and market order conversions
  - Used by pivot strategies for trailing stop management
  - **Location**: `adapter/saxo.go:288-360`

- 🟢 **GetHistoricalData()** - Historical chart data with intelligent caching
  - Endpoint: `GET /chart/v3/charts`
  - 1-hour cache TTL for performance
  - FxSpot: Averages bid/ask spreads for OHLC
  - ContractFutures: Direct OHLC values
  - **Location**: `adapter/market_data.go:197-330`

- 🟢 **SubscribeToPrices()** - Real-time price feed subscription
  - Endpoint: `POST /trade/v1/infoprices/subscriptions`
  - Already implemented in v0.2.0, now marked as Stable
  - **Location**: `adapter/websocket/subscription_manager.go`

- 🟢 **SubscribeToOrders()** - Order status update subscription
  - Endpoint: `POST /port/v1/orders/subscriptions`
  - Already implemented in v0.2.0, now marked as Stable
  - **Location**: `adapter/websocket/subscription_manager.go`

- 🟢 **SubscribeToPortfolio()** - Portfolio balance subscription
  - Endpoint: `POST /port/v1/balances/subscriptions`
  - Already implemented in v0.2.0, now marked as Stable
  - **Location**: `adapter/websocket/subscription_manager.go`

### Fixed
- Removed duplicate `SubscribeToSessionEvents()` implementations that used incorrect WebSocket WriteJSON pattern
  - Saxo API requires HTTP POST for subscriptions, not WebSocket writes
  - Cleaned up 2 duplicate methods in `subscription_manager.go` and `saxo_websocket.go`
  - All subscriptions now correctly use HTTP POST pattern

### Documentation
- Updated README.md with v0.3.0 status
- Moved core features from Experimental to Stable category
- Updated roadmap to reflect v0.3.0 completion
- Added comprehensive interface stability matrix
- Updated directory structure with accurate line counts

### Technical Details

#### WebSocket Subscription Pattern
All 4 WebSocket subscriptions now follow the correct Saxo API pattern:
1. **HTTP POST** to subscription endpoint (not WebSocket write)
2. Include `ContextId`, `ReferenceId`, `RefreshRate`, `Arguments` in payload
3. WebSocket **reads** subscription data messages
4. Automatic resubscription on reconnection

#### Interface Stability
The following interfaces are now **locked for v1.0**:
- `BrokerClient`: PlaceOrder, ModifyOrder, GetBalance, GetAccounts, GetOpenOrders
- `AuthClient`: Login, GetAccessToken, IsAuthenticated, token refresh
- `WebSocketClient`: All 4 subscription methods + connection management
- `MarketDataClient`: GetHistoricalData, GetTradingSchedule

### Migration Guide from v0.2.x

**No breaking changes** - v0.3.0 is fully backward compatible with v0.2.x.

**New features to adopt**:
```go
// New: Subscribe to session events
err := wsClient.SubscribeToSessionEvents(ctx)

// Now stable: Modify orders (was experimental in v0.2.x)
resp, err := brokerClient.ModifyOrder(ctx, modReq)

// Now stable: Get historical data (was experimental in v0.2.x)
data, err := marketClient.GetHistoricalData(ctx, instrument, days)
```

### Testing
- All existing tests passing
- ModifyOrder tested against SIM environment
- WebSocket subscriptions validated in production
- Historical data caching verified
- GetNetPositions returns aggregated position view
- GetClosedPositions returns P&L data

### Known Limitations
- No examples/ directory yet (planned for v0.4.0)

### Upgrade Instructions

```bash
# Update go.mod
go get github.com/bjoelf/saxo-adapter@v0.3.0

# Or pin to minor version for automatic patch updates
require github.com/bjoelf/saxo-adapter v0.3.x
```

---

## [0.2.0-dev] - 2025-11-01 (Pre-release)

### Added
- OAuth2 authentication with automatic token refresh
- WebSocket streaming for prices, orders, and portfolio
- REST API client for orders and positions
- Token persistence with file-based storage
- Automatic reconnection with exponential backoff
- Generic broker-agnostic interface pattern

### Initial Release
- Extracted from pivot-web2 project
- Standalone library with no external dependencies on pivot-web2
- Core interfaces defined: BrokerClient, AuthClient, WebSocketClient

---

## Release Notes

### v0.3.0 - Production-Ready Core 🎯

This release marks the **stabilization of core interfaces** for the saxo-adapter library. All critical features needed for automated FX trading are now implemented and tested in production.

**What's New:**
- All 4 WebSocket subscriptions complete (prices, orders, portfolio, session events)
- Order modification for trailing stops fully functional
- Historical chart data with performance caching
- All core interfaces locked for v1.0 compatibility

**Migration Impact:** None - fully backward compatible

**Next Steps:**
- v0.4.0: Add GetNetPositions, GetClosedPositions, comprehensive examples
- v0.5.0: Margin calculation and risk analysis features
- v1.0.0: Full stability guarantees (Q2 2026)

**Production Status:** ✅ Ready for production use

---

[0.3.0]: https://github.com/bjoelf/saxo-adapter/compare/v0.2.0-dev...v0.3.0
[0.2.0-dev]: https://github.com/bjoelf/saxo-adapter/releases/tag/v0.2.0-dev
