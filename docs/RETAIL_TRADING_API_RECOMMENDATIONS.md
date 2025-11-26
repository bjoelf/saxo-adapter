# Saxo OpenAPI: Retail Trading Methods Recommendation

**Context**: Recommendations for saxo-adapter interface design targeting private traders with one account executing typical retail trading workflows.

**Analysis Date**: December 2024  
**Target Audience**: Retail traders (single account, self-directed trading)  
**API Version**: Saxo OpenAPI v1-v4 (various services)

---

## Executive Summary

Based on analysis of Saxo Bank's OpenAPI documentation and pivot-web's production requirements, this document provides a pragmatic implementation roadmap that balances immediate migration needs with long-term API design.

### Implementation Strategy

**Phase 1: pivot-web2 Migration Blockers (ASAP - v0.3.0)**
- Implement what pivot-web **requires now** using legacy code as reference
- 4 WebSocket subscriptions (already proven in production)
- Historical chart data endpoint
- Core HTTP methods for "the usual suspects" (balance, positions, orders, instruments)

**Phase 2: Joint Evolution (v0.4.0 - v0.6.0)**
- pivot-web2 and saxo-adapter evolve together
- Add additional subscriptions as needs emerge (easy once first 4 are debugged)
- Expand "the usual suspects" with real-world feedback

**Phase 3: Stabilization (v1.0.0 - Q2 2026)**
- Freeze stable core interface
- Document advanced features as post-v1.0 roadmap

### Three-Tier Categorization

- **🟢 Tier 1 - Migration Essentials**: 15 methods - pivot-web needs NOW
- **🟡 Tier 2 - The Usual Suspects**: 12 methods - common retail trading needs
- **🔵 Tier 3 - Advanced Features**: 20+ methods - post-v1.0 based on user requests

---

## Saxo OpenAPI Service Groups Overview

Saxo provides 15+ service groups. For retail traders, the following are relevant:

| Service Group | Retail Priority | Use Case |
|--------------|----------------|----------|
| **Trading** | CRITICAL | Order execution, position management, live prices |
| **Portfolio** | CRITICAL | Account balances, positions, order status |
| **Reference Data** | HIGH | Instrument details, exchange info, trading schedules |
| **Chart** | MEDIUM | Historical price data for strategy backtesting |
| **Account History** | LOW-MEDIUM | Performance tracking, closed positions |
| **Root Services** | MEDIUM | Session management, feature availability |
| **Market Overview** | LOW | Market movers, top gainers/losers (informational) |
| Corporate Actions | VERY LOW | Dividend tracking, stock splits |
| Client Services | VERY LOW | Institutional features |
| Ens (Event Notifications) | LOW | Alternative to WebSocket for some events |
| Value Add | LOW | Premium data services |

**Focus Recommendation**: Prioritize Trading + Portfolio + Reference Data (covers 90% of retail needs).

---

## Tier 1 - Migration Essentials (ASAP Implementation) 🟢

**Purpose**: Unblock pivot-web2 migration by implementing what production pivot-web **actually uses today**.

**Reference Implementation**: Legacy `pivot-web/broker/` package (proven in production since 2024)

**Stability Commitment**: These methods form the stable core. Will NOT change signature or behavior after v1.0.0.

### Critical Discovery: The 4 Production WebSocket Subscriptions

**Source**: `pivot-web/broker/broker_websocket.go` (lines 60-63)

```go
const priceSubscriptionPath = "/trade/v1/infoprices/subscriptions"
const orderStatusSubscriptionPath = "/port/v1/orders/subscriptions"
const sessionsSubscriptionPath = "/root/v1/sessions/events/subscriptions/active"  
const portfolioBalancePath = "/port/v1/balances/subscriptions"
```

**These 4 subscriptions are CRITICAL** - pivot-web2 cannot function without them. Once debugged, adding more subscriptions is trivial (same WebSocket pattern).

---

### Trading Service

#### 1. `PlaceOrder(request OrderRequest) (OrderResponse, error)` ✅
**Saxo Endpoint**: `POST /trade/v2/orders`  
**Use Case**: Place market, limit, stop, or stop-limit orders  
**Pivot-web Usage**: CRITICAL - `strategy_manager` places entry/stop orders daily at 22:00 UTC  
**Implementation Status**: ✅ Implemented in saxo-adapter v0.2.0
**Reference**: `pivot-web/broker/broker_http.go` - `PostBrokerData()`

```go
// Example: Place stop-limit order for EURUSD
request := OrderRequest{
    Uic: 21,  // EURUSD
    AssetType: "FxSpot",
    Amount: 10000,
    OrderType: "StopLimit",
    OrderPrice: 1.0850,
    StopLimitPrice: 1.0845,
    BuySell: "Buy",
}
```

#### 2. `CancelOrder(orderID string) error` ✅
**Saxo Endpoint**: `DELETE /trade/v2/orders/{OrderId}`  
**Use Case**: Cancel pending orders  
**Pivot-web Usage**: CRITICAL - Risk management, daily order cleanup  
**Implementation Status**: ✅ Implemented in saxo-adapter v0.2.0
**Reference**: `pivot-web/broker/broker_http.go` - `DeleteBrokerData()`

#### 3. `ModifyOrder(orderID string, updates OrderUpdate) error` ⚠️
**Saxo Endpoint**: `PATCH /trade/v2/orders/{OrderId}`  
**Use Case**: Update order price, size, or parameters  
**Pivot-web Usage**: HIGH - Trailing stop updates (`strategy_manager/trailingstop.go`)  
**Implementation Status**: ⚠️ Partially implemented (needs full parameter support)
**Reference**: `pivot-web/broker/broker_http.go` - `PutBrokerData()`

#### 4. `GetOrders(accountKey string) ([]Order, error)` ✅
**Saxo Endpoint**: `GET /port/v1/orders/me`  
**Use Case**: List all pending orders  
**Pivot-web Usage**: CRITICAL - Order recovery after crash (`strategies/order_recover.go`)  
**Implementation Status**: ✅ Implemented in saxo-adapter v0.2.0
**Reference**: `pivot-web/broker/broker_http.go` - `GetBrokerData()`

---

### WebSocket Subscriptions (THE CRITICAL 4) 🔥

These 4 subscriptions are what pivot-web **actually runs in production**. Implementing these unlocks pivot-web2 migration.

#### 5. `SubscribeToPrices(uic int, assetType string, callback PriceCallback) error` ✅
**Saxo Endpoint**: `POST /trade/v1/infoprices/subscriptions` (WebSocket)  
**Use Case**: Real-time price updates for threshold-based order triggering  
**Pivot-web Usage**: CRITICAL - `strategy_manager/streaming_prices.go` checks if `signal.Dist <= signal.ThresholdDist`  
**Implementation Status**: ✅ Implemented in saxo-adapter v0.2.0 (WebSocket)
**Reference**: `pivot-web/broker/broker_websocket.go:61` - `priceSubscriptionPath`

**Legacy Code Pattern**:
```go
// pivot-web subscribes to each instrument's price feed at market open
const priceSubscriptionPath = "/trade/v1/infoprices/subscriptions"
```

#### 6. `SubscribeToOrders(accountKey string, callback OrderUpdateCallback) error` ⚠️
**Saxo Endpoint**: `POST /port/v1/orders/subscriptions` (WebSocket)  
**Use Case**: Real-time order status changes (Working → Filled → Cancelled)  
**Pivot-web Usage**: CRITICAL - `strategy_manager/streaming_orders.go` updates signal state on fills  
**Implementation Status**: ⚠️ **BLOCKER** - Not yet in saxo-adapter, must implement ASAP
**Reference**: `pivot-web/broker/broker_websocket.go:62` - `orderStatusSubscriptionPath`

**Legacy Code Pattern**:
```go
// pivot-web tracks order lifecycle (entry filled → place stop, stop filled → close position)
const orderStatusSubscriptionPath = "/port/v1/orders/subscriptions"
```

**Why Critical**: Without this, pivot-web2 cannot detect when entry orders fill (needed to place stop-loss) or when stops trigger (needed to close positions).

#### 7. `SubscribeToBalance(accountKey string, callback BalanceCallback) error` ⚠️
**Saxo Endpoint**: `POST /port/v1/balances/subscriptions` (WebSocket)  
**Use Case**: Real-time margin/P&L updates  
**Pivot-web Usage**: HIGH - Live margin monitoring, prevents over-leveraging  
**Implementation Status**: ⚠️ **BLOCKER** - Not yet in saxo-adapter, must implement ASAP
**Reference**: `pivot-web/broker/broker_websocket.go:63` - `portfolioBalancePath`

**Legacy Code Pattern**:
```go
// pivot-web monitors margin to ensure sufficient funds for new orders
const portfolioBalancePath = "/port/v1/balances/subscriptions"
```

#### 8. `SubscribeToSessionEvents(callback SessionEventCallback) error` ⚠️
**Saxo Endpoint**: `POST /root/v1/sessions/events/subscriptions/active` (WebSocket)  
**Use Case**: Detect connection issues, token expiration, system maintenance  
**Pivot-web Usage**: MEDIUM - Connection robustness, graceful reconnection  
**Implementation Status**: ⚠️ **BLOCKER** - Not yet in saxo-adapter, must implement ASAP
**Reference**: `pivot-web/broker/broker_websocket.go:63` - `sessionsSubscriptionPath`

**Legacy Code Pattern**:
```go
// pivot-web uses session events to detect when reconnection is needed
const sessionsSubscriptionPath = "/root/v1/sessions/events/subscriptions/active"
```

**Implementation Note**: Once these 4 subscriptions are debugged, adding new subscriptions (positions, net positions, etc.) uses the same WebSocket pattern - very low effort.

---

### "The Usual Suspects" - HTTP Endpoints 📡

These are simple HTTP GET/POST requests - fast to write and test. Pivot-web uses all of these.

---

### Portfolio Service

#### 9. `GetBalance(accountKey string) (Balance, error)` ✅
**Saxo Endpoint**: `GET /port/v1/balances`  
**Use Case**: Check account cash, margin, total value  
**Pivot-web Usage**: CRITICAL - Position sizing (`strategies/pivot.go` calculates risk per trade)  
**Implementation Status**: ✅ Implemented in saxo-adapter v0.2.0
**Reference**: `pivot-web/broker/balance.go` - `GetBalance()`

```go
// Example balance response
Balance{
    CashBalance: 50000.00,
    MarginAvailable: 45000.00,
    TotalValue: 52500.00,
    Currency: "USD",
}
```

#### 10. `GetAccounts() ([]Account, error)` ✅
**Saxo Endpoint**: `GET /port/v1/accounts/me`  
**Use Case**: List accounts (typically one for retail)  
**Pivot-web Usage**: HIGH - Account selection at startup  
**Implementation Status**: ✅ Implemented in saxo-adapter v0.2.0
**Reference**: `pivot-web/broker/broker_http.go` - `GetAccounts()`

#### 11. `GetPositions(accountKey string) ([]Position, error)` ✅
**Saxo Endpoint**: `GET /port/v1/positions/me`  
**Use Case**: List all open positions  
**Pivot-web Usage**: CRITICAL - Position monitoring, recovery after restart  
**Implementation Status**: ✅ Implemented in saxo-adapter v0.2.0
**Reference**: `pivot-web/strategy_manager/positions_open.go` - `ProcessOpenPositions()`

#### 12. `GetNetPositions(accountKey string) ([]NetPosition, error)` ⚠️
**Saxo Endpoint**: `GET /port/v1/netpositions/me`  
**Use Case**: Aggregated positions (e.g., 3 long EURUSD orders = 1 net position)  
**Pivot-web Usage**: MEDIUM - Used in UI for simplified view  
**Implementation Status**: ⚠️ Not yet implemented - add to v0.3.0
**Reference**: `pivot-web/strategy_manager/composite.go` - displays net positions

#### 13. `GetClosedPositions(fromDate, toDate time.Time) ([]ClosedPosition, error)` ⚠️
**Saxo Endpoint**: `GET /port/v1/closedpositions/me`  
**Use Case**: Recent trade history  
**Pivot-web Usage**: HIGH - Daily P&L review  
**Implementation Status**: ⚠️ Not yet implemented - add to v0.3.0
**Reference**: `pivot-web/strategy_manager/positions_closed.go` - `ProcessClosedPositions()`

---

### Reference Data Service

#### 14. `GetInstrumentDetails(uic int, assetType string) (InstrumentDetails, error)` ✅
**Saxo Endpoint**: `GET /ref/v1/instruments/details/{Uic}/{AssetType}`  
**Use Case**: Tick size, decimals, lot sizes, trading schedule  
**Pivot-web Usage**: CRITICAL - Order validation, position sizing  
**Implementation Status**: ✅ Implemented in saxo-adapter v0.2.0
**Reference**: `pivot-web/strategies/portfolio.go` - `populateInstrumentDetails()`

```go
// Example instrument details
InstrumentDetails{
    Uic: 21,
    Symbol: "EURUSD",
    TickSize: 0.00001,
    MinLotSize: 1000,
    LotSize: 1000,
    TradingStatus: "Tradable",
    ExchangeId: "FOREX",
}
```

#### 15. `SearchInstruments(query string, assetTypes []string) ([]Instrument, error)` ⚠️
**Saxo Endpoint**: `GET /ref/v1/instruments` (with filters)  
**Use Case**: Find instruments by name/symbol  
**Pivot-web Usage**: LOW - Portfolio is static (loaded from `data/portfolio.json`)  
**Implementation Status**: ⚠️ Not yet implemented - add to v0.3.0 (nice-to-have)
**Reference**: Not used in pivot-web (portfolio is pre-configured)

#### 16. `GetExchangeInfo(exchangeID string) (Exchange, error)` ⚠️
**Saxo Endpoint**: `GET /ref/v1/exchanges/{ExchangeId}`  
**Use Case**: Trading hours, market status  
**Pivot-web Usage**: MEDIUM - Could replace hardcoded 22:00 UTC schedule  
**Implementation Status**: ⚠️ Not yet implemented - add to v0.4.0 (future enhancement)
**Reference**: `pivot-web/scheduling/scheduler.go` - hardcodes trading hours

---

### Market Data Service

#### 17. `GetHistoricalBars(uic int, assetType string, horizon int, count int) ([]OHLC, error)` ⚠️
**Saxo Endpoint**: `GET /chart/v1/charts`  
**Use Case**: Backtest strategies, calculate pivots  
**Pivot-web Usage**: CRITICAL BLOCKER - Fetches 25 days of 1440-min bars daily for pivot calculation  
**Implementation Status**: ⚠️ **MUST IMPLEMENT ASAP** - pivot-web2 cannot migrate without this
**Reference**: `pivot-web/strategies/strategy.go:227` - `fetchHistoricBars()`

```go
// Example: Get last 25 days of daily bars
bars, _ := client.GetHistoricalBars(21, "FxSpot", 1440, 25)
// Returns: [{Open: 1.0850, High: 1.0920, Low: 1.0830, Close: 1.0900, Time: ...}, ...]
```

**Why Critical**: Pivot strategies (`pivot.go`, `pivot_extra.go`) calculate entry/stop levels from daily high/low pivots. Without historical data, strategies cannot generate signals.

---

### Authentication (Already Implemented) ✅

#### 18. `Authenticate(code string) (Token, error)` ✅
**Saxo Endpoint**: OAuth2 flow with `POST /sim/openapi/token`  
**Use Case**: Initial broker connection  
**Pivot-web Usage**: CRITICAL - System prerequisite  
**Implementation Status**: ✅ Implemented in saxo-adapter v0.2.0
**Reference**: `pivot-web/broker/oauth.go` - `Callback()`

#### 19. `RefreshToken() (Token, error)` ✅
**Saxo Endpoint**: OAuth2 refresh flow  
**Use Case**: Maintain session without re-login  
**Pivot-web Usage**: CRITICAL - Persistent connection (7-day refresh token)  
**Implementation Status**: ✅ Implemented in saxo-adapter v0.2.0
**Reference**: `pivot-web/broker/oauth.go` - `refreshToken()`, `StartAuthenticationKeeper()`

---

## Summary: Tier 1 Implementation Checklist

### ✅ Already Implemented in saxo-adapter v0.2.0 (11/19 methods)
1. ✅ `PlaceOrder()` - Order placement
2. ✅ `CancelOrder()` - Order cancellation
3. ✅ `GetOrders()` - List pending orders
4. ✅ `GetBalance()` - Account balance
5. ✅ `GetAccounts()` - Account list
6. ✅ `GetPositions()` - Open positions
7. ✅ `GetInstrumentDetails()` - Instrument metadata
8. ✅ `Authenticate()` - OAuth2 login
9. ✅ `RefreshToken()` - Token refresh
10. ✅ `SubscribeToPrices()` - Price WebSocket subscription
11. ✅ `ModifyOrder()` - Order modification (partial)

### ⚠️ CRITICAL BLOCKERS - Must Implement for pivot-web2 Migration (5 methods)
12. ⚠️ `GetHistoricalBars()` - **BLOCKER** - Daily pivot calculation requires this
13. ⚠️ `SubscribeToOrders()` - **BLOCKER** - Order status tracking requires this
14. ⚠️ `SubscribeToBalance()` - **BLOCKER** - Margin monitoring requires this
15. ⚠️ `SubscribeToSessionEvents()` - **BLOCKER** - Connection robustness requires this
16. ⚠️ `ModifyOrder()` - Complete implementation (trailing stops require full parameter support)

### 📋 Nice-to-Have - Can Add During/After Migration (3 methods)
17. 📋 `GetNetPositions()` - UI convenience (can work around with `GetPositions()`)
18. 📋 `GetClosedPositions()` - P&L reporting (pivot-web uses CSV logs as backup)
19. 📋 `SearchInstruments()` - Discovery (pivot-web uses static portfolio)

**Migration Impact**: Cannot start pivot-web2 migration until the 5 critical blockers are implemented. Estimated effort: **6-8 hours** (4 WebSocket subscriptions + 1 HTTP endpoint + polish `ModifyOrder()`).

---

## Tier 2 - The Usual Suspects (Joint Evolution) 🟡

**Purpose**: Common retail trading features that emerge during real-world usage. Implement based on pivot-web2 feedback.

**Development Approach**: pivot-web2 and saxo-adapter evolve together. Add methods as needs are proven, not speculation.

**Stability Commitment**: Signatures may change in v0.x based on real usage patterns. Stabilize by v1.0.

---

### Additional WebSocket Subscriptions (Easy Once First 4 Are Debugged)

Once the 4 critical subscriptions are working, adding more follows the same pattern. Low effort, high value.

#### 20. `SubscribeToPositions(accountKey string, callback PositionCallback) error` 📋
**Saxo Endpoint**: `POST /port/v1/positions/subscriptions` (WebSocket)  
**Use Case**: Real-time position updates (P&L changes, position opened/closed)  
**Pivot-web Usage**: Not currently used (polls `GetPositions()` instead)  
**Priority**: LOW-MEDIUM - Optimization, not requirement  
**Effort**: 1-2 hours (copy pattern from `SubscribeToOrders()`)

#### 21. `SubscribeToNetPositions(accountKey string, callback NetPositionCallback) error` 📋
**Saxo Endpoint**: `POST /port/v1/netpositions/subscriptions` (WebSocket)  
**Use Case**: Real-time aggregated position updates  
**Pivot-web Usage**: Not currently used  
**Priority**: LOW - Nice-to-have for UI  
**Effort**: 1-2 hours (copy pattern from `SubscribeToOrders()`)

---

### "The Usual Suspects" - HTTP Endpoints

These are common requests that traders make frequently. Simple HTTP GETs, fast to implement.

#### 22. `GetInfoPrices(uics []int, assetTypes []string) ([]InfoPrice, error)` 📋
**Saxo Endpoint**: `GET /trade/v1/infoprices/list` (polling, non-tradable)  
**Use Case**: Watchlist prices (multiple instruments without WebSocket)  
**Pivot-web Usage**: Not used (subscribes to individual prices via WebSocket)  
**Priority**: MEDIUM - Useful for UI watchlists  
**Effort**: 2 hours (simple HTTP GET with array parameters)

#### 23. `GetTradingSchedule(exchangeID string, date time.Time) (TradingSchedule, error)` 📋
**Saxo Endpoint**: Part of `/ref/v1/exchanges` or `/ref/v1/instruments/tradingschedule`  
**Use Case**: Market open/close times  
**Pivot-web Usage**: Not used (hardcodes 22:00 UTC in `scheduling/scheduler.go`)  
**Priority**: MEDIUM - Future automation enhancement  
**Effort**: 3 hours (need to parse Saxo's schedule format)

**Future Use Case**: Replace hardcoded `AdjustUpdateTime()` DST logic with dynamic schedule fetching.

#### 24. `GetCurrencyPairs() ([]CurrencyPair, error)` 📋
**Saxo Endpoint**: `GET /ref/v1/currencypairs`  
**Use Case**: List all tradable FX pairs  
**Pivot-web Usage**: Not used (portfolio is static)  
**Priority**: LOW - Instrument discovery  
**Effort**: 1 hour (simple HTTP GET)

---

### Position Management Convenience Methods

These wrap existing functionality for common use cases.

#### 25. `ClosePosition(positionID string) error` 📋
**Saxo Endpoint**: Part of `POST /trade/v2/orders` (market order to close)  
**Use Case**: Emergency exit (close all at market)  
**Pivot-web Usage**: Not used (places manual market order instead)  
**Priority**: MEDIUM - Convenience wrapper  
**Effort**: 1 hour (wrapper around `PlaceOrder()` with opposite side)

```go
// Convenience wrapper example
func (c *Client) ClosePosition(ctx context.Context, positionID string) error {
    // Get position details
    position, err := c.getPositionByID(ctx, positionID)
    if err != nil {
        return err
    }
    
    // Place market order on opposite side
    return c.PlaceOrder(ctx, OrderRequest{
        Uic: position.Uic,
        AssetType: position.AssetType,
        Amount: position.Amount,
        OrderType: "Market",
        BuySell: oppositeSide(position.BuySell),
    })
}
```

#### 26. `UpdatePosition(positionID string, updates PositionUpdate) error` 📋
**Saxo Endpoint**: `PATCH /trade/v1/positions/{PositionId}`  
**Use Case**: Update stop-loss/take-profit on existing position  
**Pivot-web Usage**: Not used (modifies orders directly via `ModifyOrder()`)  
**Priority**: LOW - May merge with `ModifyOrder()`  
**Effort**: 2 hours (HTTP PATCH with position ID)

**Note**: Different brokers handle this differently. Saxo allows both position-level and order-level updates. Pivot-web uses order-level (`ModifyOrder()`), which is more flexible.

---

### Analytics & Performance Tracking

#### 27. `GetPerformance(accountKey string, period string) (Performance, error)` 📋
**Saxo Endpoint**: `GET /hist/v4/performance`  
**Use Case**: Sharpe ratio, win rate, drawdown  
**Pivot-web Usage**: Not used (could calculate from closed positions)  
**Priority**: LOW-MEDIUM - Strategy evaluation  
**Effort**: 3 hours (need to parse Saxo's performance metrics)

#### 28. `GetExposure(accountKey string) (Exposure, error)` 📋
**Saxo Endpoint**: `GET /port/v1/exposure/me`  
**Use Case**: Risk analysis (currency exposure, sector concentration)  
**Pivot-web Usage**: Not used  
**Priority**: LOW - Advanced risk management  
**Effort**: 2 hours (HTTP GET with exposure breakdown)

---

### Root Services

#### 29. `GetSessionCapabilities() (Capabilities, error)` 📋
**Saxo Endpoint**: `GET /root/v1/sessions/capabilities`  
**Use Case**: Discover available features (e.g., does account support options?)  
**Pivot-web Usage**: Not used  
**Priority**: LOW - Feature detection  
**Effort**: 2 hours (HTTP GET, parse capabilities JSON)

---

## Summary: Tier 2 "Usual Suspects"

### WebSocket Subscriptions (2 additional - easy once first 4 work)
- 📋 `SubscribeToPositions()` - Real-time P&L updates
- 📋 `SubscribeToNetPositions()` - Aggregated position updates

### HTTP Endpoints (7 methods - simple GETs)
- 📋 `GetInfoPrices()` - Multi-instrument watchlist polling
- 📋 `GetTradingSchedule()` - Market hours lookup
- 📋 `GetCurrencyPairs()` - FX pair discovery
- 📋 `ClosePosition()` - Emergency exit wrapper
- 📋 `UpdatePosition()` - Position-level stop/target updates
- 📋 `GetPerformance()` - Analytics metrics
- 📋 `GetExposure()` - Risk exposure breakdown
- 📋 `GetSessionCapabilities()` - Feature availability check

**Total Tier 2**: 9 methods, estimated **15-20 hours** total effort

**Implementation Strategy**: 
1. Add these incrementally during pivot-web2 usage (v0.4.0 - v0.6.0)
2. Only implement when actual need emerges (not speculation)
3. Mark as "Experimental" until validated by real usage
4. Stabilize signatures based on feedback before v1.0

---

## Tier 3 - Advanced Features (Post-v1.0 Roadmap) 🔵

**Purpose**: Document for future consideration. Implement only if users request.

**Stability Commitment**: Document as "Planned" in README roadmap. Add GitHub issues to collect user feedback.

### Options Trading (LOW Priority for FX traders)

- `GetOptionsChain(underlyingUic int) ([]Option, error)` - `/trade/v1/optionschain`
- `ExerciseOption(positionID string) error` - `/trade/v1/positions/{PositionId}/exercise`

**Why Tier 3**: pivot-web trades FX/indices (no options). Add only if users request.

---

### Corporate Actions (VERY LOW for FX)

- `GetCorporateActions(instrumentID int) ([]CorporateAction, error)` - Various endpoints
- `AcceptCorporateAction(actionID string, decision string) error`

**Why Tier 3**: Not applicable to FX/CFD trading.

---

### Advanced Charting

- `SubscribeToCharts(params ChartParams, callback ChartCallback) error` - `/chart/v3/charts/subscriptions` (streaming OHLC)
- `GetIntradayBars(uic int, interval string, count int) ([]OHLC, error)` - High-frequency bars

**Why Tier 3**: Tier 2 covers daily bars. Streaming charts are heavy (bandwidth). Add if users build charting UIs.

---

### Market Overview (Informational)

- `GetMarketMovers(exchange string) ([]Mover, error)` - Top gainers/losers
- `GetTopTrades(assetType string) ([]Trade, error)` - Most traded instruments

**Why Tier 3**: Nice-to-have for dashboards, not trading functionality.

---

### Client Services (Institutional)

- `GetClientInfo(clientKey string) (Client, error)` - Account details
- `GetUserInfo(userKey string) (User, error)` - User profile

**Why Tier 3**: Retail traders don't manage multiple clients.

---

### Algorithmic Strategies

- `GetAlgoStrategies() ([]AlgoStrategy, error)` - `/ref/v1/algostrategies`
- `PlaceAlgoOrder(strategy AlgoStrategy, params map[string]interface{}) error`

**Why Tier 3**: Saxo's built-in algos (TWAP, VWAP, etc.). Custom strategies use basic orders.

---

### Unsettled Amounts (Beta)

- `GetUnsettledAmounts(accountKey string) ([]UnsettledAmount, error)` - `/hist/v1/unsettledamounts`

**Why Tier 3**: Beta feature for institutional cash management.

---

## Recommended Interface Organization

Based on tiered analysis, organize saxo-adapter interfaces as follows:

### File: `adapter/interfaces.go`

```go
package adapter

// ============================================================================
// STABLE CORE INTERFACE (v1.0 Commitment)
// ============================================================================

// BrokerClient defines essential retail trading operations.
// Stability: STABLE - Will not change after v1.0.0
type BrokerClient interface {
	// Order Management (Tier 1)
	PlaceOrder(ctx context.Context, request OrderRequest) (OrderResponse, error)
	CancelOrder(ctx context.Context, orderID string) error
	ModifyOrder(ctx context.Context, orderID string, updates OrderUpdate) error
	GetOrders(ctx context.Context, accountKey string) ([]Order, error)

	// Account & Balance (Tier 1)
	GetBalance(ctx context.Context, accountKey string) (Balance, error)
	GetAccounts(ctx context.Context) ([]Account, error)
	
	// Positions (Tier 1)
	GetPositions(ctx context.Context, accountKey string) ([]Position, error)
	GetNetPositions(ctx context.Context, accountKey string) ([]NetPosition, error)
	GetClosedPositions(ctx context.Context, fromDate, toDate time.Time) ([]ClosedPosition, error)

	// Instruments (Tier 1)
	GetInstrumentDetails(ctx context.Context, uic int, assetType string) (InstrumentDetails, error)
	SearchInstruments(ctx context.Context, query string, assetTypes []string) ([]Instrument, error)
}

// AuthClient handles OAuth2 authentication flow.
// Stability: STABLE - Will not change after v1.0.0
type AuthClient interface {
	Authenticate(ctx context.Context, code string) (Token, error)
	RefreshToken(ctx context.Context) (Token, error)
	GetValidToken(ctx context.Context) (Token, error)
}

// WebSocketClient manages real-time data subscriptions.
// Stability: STABLE - Core methods frozen, callbacks may add optional fields
type WebSocketClient interface {
	Connect(ctx context.Context) error
	Close() error
	
	// Tier 1 subscriptions
	SubscribeToPrices(ctx context.Context, uic int, assetType string, callback PriceCallback) error
	UnsubscribeFromPrices(ctx context.Context, uic int, assetType string) error
}

// ============================================================================
// EXPERIMENTAL EXTENSIONS (v0.x - May Change)
// ============================================================================

// MarketDataExtensions adds advanced market data features.
// Stability: EXPERIMENTAL - Signatures may change before v1.0
type MarketDataExtensions interface {
	// Tier 2 - Historical data
	GetHistoricalBars(ctx context.Context, uic int, assetType string, horizon int, count int) ([]OHLC, error)
	
	// Tier 2 - Multi-instrument polling (watchlists)
	GetInfoPrices(ctx context.Context, uics []int, assetTypes []string) ([]InfoPrice, error)
}

// WebSocketExtensions adds real-time features beyond prices.
// Stability: EXPERIMENTAL - Callback signatures may evolve
type WebSocketExtensions interface {
	// Tier 2 subscriptions
	SubscribeToBalance(ctx context.Context, accountKey string, callback BalanceCallback) error
	SubscribeToOrders(ctx context.Context, accountKey string, callback OrderUpdateCallback) error
	SubscribeToSessionEvents(ctx context.Context, callback SessionEventCallback) error
}

// PositionManagementExtensions adds convenience wrappers.
// Stability: EXPERIMENTAL - May merge with BrokerClient in v1.0
type PositionManagementExtensions interface {
	ClosePosition(ctx context.Context, positionID string) error
	UpdatePosition(ctx context.Context, positionID string, updates PositionUpdate) error
}

// ReferenceDataExtensions adds instrument discovery features.
// Stability: EXPERIMENTAL - May add new filter options
type ReferenceDataExtensions interface {
	GetTradingSchedule(ctx context.Context, exchangeID string, date time.Time) (TradingSchedule, error)
	GetExchangeInfo(ctx context.Context, exchangeID string) (Exchange, error)
	GetCurrencyPairs(ctx context.Context) ([]CurrencyPair, error)
}

// AnalyticsExtensions adds performance tracking.
// Stability: EXPERIMENTAL - Fields in Performance struct may expand
type AnalyticsExtensions interface {
	GetPerformance(ctx context.Context, accountKey string, period string) (Performance, error)
	GetExposure(ctx context.Context, accountKey string) (Exposure, error)
}

// ============================================================================
// PLANNED FEATURES (Post-v1.0)
// ============================================================================

// Document in README.md roadmap, implement based on user requests:
// - OptionsTrading interface (GetOptionsChain, ExerciseOption)
// - ChartingExtensions (SubscribeToCharts, GetIntradayBars)
// - MarketOverview (GetMarketMovers, GetTopTrades)
// - AlgoTrading (GetAlgoStrategies, PlaceAlgoOrder)
```

---

## Implementation Recommendations

### Phase 1: Unblock pivot-web2 Migration (CRITICAL - Target: v0.3.0, December 2024)

**Goal**: Implement the 5 critical blockers using pivot-web legacy code as reference.

**Timeline**: 6-8 hours over 1 week

**Tasks**:

#### Task 1: Add `GetHistoricalBars()` HTTP Endpoint (2-3 hours)
**Reference**: `pivot-web/strategies/strategy.go:227` - `fetchHistoricBars()`

```go
// Legacy implementation pattern
func fetchHistoricBars(instrument Instrument, horizonMinutes, count int) ([]HistoricBar, error) {
    endpoint := "/chart/v1/charts"
    params := url.Values{}
    params.Add("Uic", strconv.Itoa(instrument.Uic))
    params.Add("AssetType", instrument.AssetType)
    params.Add("Horizon", strconv.Itoa(horizonMinutes))
    params.Add("Count", strconv.Itoa(count))
    
    data, err := broker.GetBrokerData(endpoint + "?" + params.Encode())
    // ... parse response
}
```

**Implementation**:
1. Add method to `adapter/market_data.go`
2. Parse Saxo's chart response format (OHLC data)
3. Write unit tests with mock data
4. Integration test with SIM environment

**Priority**: HIGHEST - pivot-web2 cannot calculate pivots without this

---

#### Task 2: Add 4 WebSocket Subscriptions (4-5 hours)

**Reference**: `pivot-web/broker/broker_websocket.go:60-63`

All 4 follow the same pattern - once first is working, others are quick:

**2.1. `SubscribeToOrders()` (2 hours - first is slowest)**

```go
// Legacy endpoint
const orderStatusSubscriptionPath = "/port/v1/orders/subscriptions"

// Subscription message format
{
    "ContextId": "20241126-162101-12345",
    "ReferenceId": "orders-20241126-162101",
    "Arguments": {
        "AccountKey": "Cf4xZWiYL6W1nMKpygBLLA==",
        "FieldGroups": ["DisplayAndFormat"]
    }
}
```

**Implementation**:
1. Add `SubscribeToOrders()` to `adapter/websocket/saxo_websocket.go`
2. Create `OrderUpdateCallback` type
3. Handle order status messages (Working, Placed, Filled, Cancelled, etc.)
4. Test with SIM environment (place order, watch for fill notification)

**Reference**: `pivot-web/strategy_manager/streaming_orders.go` - callback handler

**2.2. `SubscribeToBalance()` (1 hour - copy pattern)**

```go
// Legacy endpoint
const portfolioBalancePath = "/port/v1/balances/subscriptions"
```

**Implementation**: Copy pattern from `SubscribeToOrders()`, different callback type

**2.3. `SubscribeToSessionEvents()` (1 hour - copy pattern)**

```go
// Legacy endpoint  
const sessionsSubscriptionPath = "/root/v1/sessions/events/subscriptions/active"
```

**Implementation**: Copy pattern from `SubscribeToOrders()`, handles session warnings/errors

**2.4. Polish Existing `SubscribeToPrices()` (30 mins)**

Already implemented, but verify against legacy:
```go
// Legacy endpoint
const priceSubscriptionPath = "/trade/v1/infoprices/subscriptions"
```

**Testing**: Subscribe to all 4 simultaneously, verify no conflicts

---

#### Task 3: Complete `ModifyOrder()` Implementation (1 hour)

**Reference**: `pivot-web/broker/broker_http.go` - `PutBrokerData()` for order updates

**Current Status**: Partially implemented, needs full parameter support

**Requirements**:
- Update order price
- Update order size
- Update stop/limit levels
- Update order duration

**Priority**: HIGH - Trailing stops require this

---

### Phase 2: Add "Usual Suspects" During pivot-web2 Usage (v0.4.0 - v0.6.0, Jan-Apr 2025)

**Goal**: Expand interface based on real-world feedback from pivot-web2 deployment.

**Approach**: Implement Tier 2 methods **only when needed**, not speculatively.

**Timeline**: 15-20 hours over 3-4 months (incremental)

**Candidates** (prioritize based on actual usage):
1. `GetNetPositions()` - If UI needs aggregated view
2. `GetClosedPositions()` - If CSV logs prove insufficient
3. `GetInfoPrices()` - If watchlist feature is added
4. `SubscribeToPositions()` - If real-time P&L monitoring is added
5. `ClosePosition()` - If emergency exit feature is added
6. `GetTradingSchedule()` - If dynamic scheduling is added

**Decision Point**: Each method added should solve a **proven need**, not a theoretical one.

---

### Phase 3: Stabilize for v1.0.0 (Q2 2026)

**Goal**: Freeze all implemented interfaces, commit to semantic versioning.

**Tasks**:
1. Review all Tier 1 methods for stability (no breaking changes allowed after v1.0)
2. Promote heavily-used Tier 2 methods to Tier 1 (stable core)
3. Document remaining Tier 2 methods as experimental extensions
4. Create GitHub issues for Tier 3 features (collect user votes)

**Stability Commitment**:
- **Tier 1 (Stable Core)**: No breaking changes ever (semver MAJOR bump required)
- **Tier 2 (Extensions)**: Can add new optional parameters, mark old ones deprecated
- **Tier 3 (Planned)**: No commitment, implement if requested

---

## Retail Trading Workflow Coverage

### Typical Retail Trading Day (FX)

**Scenario**: Trader wakes up, checks positions, places new orders, monitors intraday, closes trades.

| Workflow Step | Required Methods | Tier |
|--------------|------------------|------|
| 1. Login to broker | `Authenticate()`, `RefreshToken()` | 🟢 Tier 1 |
| 2. Check account balance | `GetBalance()` | 🟢 Tier 1 |
| 3. View open positions | `GetPositions()`, `GetNetPositions()` | 🟢 Tier 1 |
| 4. Review yesterday's trades | `GetClosedPositions()` | 🟢 Tier 1 |
| 5. Research new instruments | `SearchInstruments()`, `GetInstrumentDetails()` | 🟢 Tier 1 |
| 6. Calculate pivot points | `GetHistoricalBars()` | 🟡 Tier 2 |
| 7. Place limit order | `PlaceOrder()` | 🟢 Tier 1 |
| 8. Set stop-loss | `PlaceOrder()` (stop order linked to position) | 🟢 Tier 1 |
| 9. Monitor live prices | `SubscribeToPrices()` | 🟢 Tier 1 |
| 10. Adjust stop-loss (trailing) | `ModifyOrder()` or `UpdatePosition()` | 🟢 Tier 1 / 🟡 Tier 2 |
| 11. Cancel unfilled orders | `CancelOrder()` | 🟢 Tier 1 |
| 12. Close position at market | `ClosePosition()` or `PlaceOrder()` | 🟡 Tier 2 / 🟢 Tier 1 |
| 13. Review performance | `GetPerformance()` | 🟡 Tier 2 |

**Coverage**: Tier 1 alone covers 90% of the workflow. Tier 2 adds convenience (historical bars, position close wrapper, performance metrics).

---

### Automated Trading (pivot-web)

**Scenario**: Bot runs daily at 22:00 UTC, calculates pivots, places orders, monitors via WebSocket.

| Workflow Step | Required Methods | Tier |
|--------------|------------------|------|
| 1. Fetch historic bars (25 days) | `GetHistoricalBars()` | 🟡 Tier 2 |
| 2. Calculate pivot levels | (App logic using bars) | N/A |
| 3. Get account balance | `GetBalance()` | 🟢 Tier 1 |
| 4. Size positions (risk-based) | (App logic using balance) | N/A |
| 5. Place entry orders (stop-limit) | `PlaceOrder()` | 🟢 Tier 1 |
| 6. Place stop-loss orders | `PlaceOrder()` | 🟢 Tier 1 |
| 7. Subscribe to prices | `SubscribeToPrices()` | 🟢 Tier 1 |
| 8. Subscribe to order updates | `SubscribeToOrders()` | 🟡 Tier 2 |
| 9. Subscribe to balance changes | `SubscribeToBalance()` | 🟡 Tier 2 |
| 10. Detect entry fill | (Order update callback) | 🟡 Tier 2 |
| 11. Update trailing stop | `ModifyOrder()` | 🟢 Tier 1 |
| 12. Close WebSocket at 21:00 UTC | `Close()` | 🟢 Tier 1 |

**Coverage**: Tier 1 covers core functionality. Tier 2 adds critical automation features (`GetHistoricalBars`, `SubscribeToOrders`, `SubscribeToBalance`).

**Recommendation**: Implement Tier 2 methods during pivot-web2 migration (Phase 2).

---

## Technical Implementation Notes

### 1. Version-Specific Endpoints

Saxo has multiple API versions for some services:

| Service | Available Versions | Recommendation |
|---------|-------------------|----------------|
| Orders | v1, v2 | Use **v2** (latest, more parameters) |
| Performance | v3, v4 | Use **v4** (enhanced metrics) |
| Charts | v1, v3 | Use **v1** for HTTP polling, **v3** for WebSocket streaming |
| Trades | v1, v2 | Use **v2** (includes deal capture enhancements) |

**Implementation**: Use latest stable version by default, document version in method comments.

---

### 2. WebSocket vs HTTP Polling

| Data Type | WebSocket Endpoint | HTTP Polling Endpoint | Recommendation |
|-----------|-------------------|----------------------|----------------|
| Prices (single) | `/trade/v1/prices/subscriptions` | `/trade/v1/infoprices/list` | **WebSocket** (real-time) |
| Prices (multi) | N/A | `/trade/v1/infoprices/list` | **HTTP** (watchlists) |
| Orders | `/port/v1/orders/subscriptions` | `/port/v1/orders/me` | **WebSocket** (automation) |
| Balance | `/port/v1/balances/subscriptions` | `/port/v1/balances` | **HTTP** for manual, **WebSocket** for automation |
| Charts (daily) | `/chart/v3/charts/subscriptions` | `/chart/v1/charts` | **HTTP** (once per day) |
| Charts (intraday) | `/chart/v3/charts/subscriptions` | N/A | **WebSocket** (streaming) |

**Current saxo-adapter WebSocket**: Implements price subscriptions. Add order/balance subscriptions in Tier 2.

---

### 3. Asset Type Coverage

Saxo supports 30+ asset types. For retail FX/indices trading, prioritize:

| Asset Type | Saxo Code | Priority | Notes |
|-----------|-----------|----------|-------|
| FX Spot | `FxSpot` | CRITICAL | 90% of pivot-web volume |
| CFD Indices | `CfdIndexOption` | HIGH | OMXS30, DAX, etc. |
| Stocks | `Stock` | MEDIUM | Some retail traders |
| FX Forwards | `FxForwards` | LOW | Advanced FX traders |
| Options | `StockOption`, `StockIndexOption` | VERY LOW | Complex, Tier 3 |
| Futures | `StockFuture`, `CommodityFuture` | VERY LOW | Institutional |

**Testing**: Ensure Tier 1 methods work with `FxSpot` and `CfdIndexOption` (cover 95% of pivot-web use cases).

---

### 4. Rate Limiting

Saxo enforces rate limits:

- **HTTP**: ~100 requests/second per token
- **WebSocket**: Max 200 subscriptions per connection
- **Chart data**: Max 1200 bars per request

**Implementation**: Add rate limiting middleware in saxo-adapter, expose configuration:
```go
type Config struct {
    HTTPRateLimit int // requests per second (default: 50)
    MaxWebSocketSubscriptions int // default: 100
}
```

---

### 5. Error Handling

Saxo returns structured errors:
```json
{
  "ErrorCode": "InsufficientFunds",
  "Message": "Account does not have sufficient funds",
  "ModelState": {
    "Amount": ["Amount exceeds available margin"]
  }
}
```

**Recommendation**: Parse `ErrorCode` into typed errors:
```go
type SaxoError struct {
    Code    string
    Message string
    Fields  map[string][]string
}

func (e *SaxoError) Error() string {
    return fmt.Sprintf("Saxo API error [%s]: %s", e.Code, e.Message)
}

// Usage
if err != nil {
    var saxoErr *SaxoError
    if errors.As(err, &saxoErr) {
        if saxoErr.Code == "InsufficientFunds" {
            // Handle margin call
        }
    }
}
```

---

## Migration Impact for pivot-web2

### Current Situation

**pivot-web2 branch**: `7-spawn-saxo-adapter-to-separate-repo`  
**Migration document**: `docs/SAXO_ADAPTER_MIGRATION.md` (8 decision points awaiting approval)  
**Blocker status**: Cannot migrate until 5 critical methods are implemented

### What pivot-web Actually Uses Today

Analysis of production `pivot-web/` codebase reveals actual usage:

#### ✅ Already Available in saxo-adapter (11 methods)
1. ✅ `PlaceOrder()` - Used by `strategy_manager` to place entry/stop orders
2. ✅ `CancelOrder()` - Used for daily order cleanup
3. ✅ `ModifyOrder()` - Used for trailing stops (needs completion)
4. ✅ `GetOrders()` - Used for crash recovery
5. ✅ `GetBalance()` - Used for position sizing
6. ✅ `GetAccounts()` - Used for account selection
7. ✅ `GetPositions()` - Used for position monitoring
8. ✅ `GetInstrumentDetails()` - Used for tick size validation
9. ✅ `Authenticate()` - OAuth2 login
10. ✅ `RefreshToken()` - Token refresh
11. ✅ `SubscribeToPrices()` - Price updates (already works)

#### ⚠️ Critical Blockers (5 methods)
12. ⚠️ **`GetHistoricalBars()`** - **CRITICAL** - `strategies/strategy.go:227` fetches 25 days of bars daily
13. ⚠️ **`SubscribeToOrders()`** - **CRITICAL** - `strategy_manager/streaming_orders.go` tracks order fills
14. ⚠️ **`SubscribeToBalance()`** - **CRITICAL** - Monitors margin to prevent over-leveraging
15. ⚠️ **`SubscribeToSessionEvents()`** - **CRITICAL** - Connection robustness
16. ⚠️ **Complete `ModifyOrder()`** - Trailing stops need full parameter support

#### 📋 Optional Nice-to-Haves (3 methods)
17. 📋 `GetNetPositions()` - Used in UI, can work around
18. 📋 `GetClosedPositions()` - Used for P&L, has CSV backup
19. 📋 `SearchInstruments()` - Not used (static portfolio)

### Revised Migration Timeline

**Original Plan** (from migration doc): 8-13 hours over 2 sessions

**Revised Plan with Blockers**:

#### Step 1: Implement Blockers (6-8 hours - 1 week)
- Add `GetHistoricalBars()` - 2-3 hours
- Add 4 WebSocket subscriptions - 4-5 hours
- Complete `ModifyOrder()` - 1 hour
- **Output**: saxo-adapter v0.3.0 with all pivot-web requirements

#### Step 2: Migration Execution (8-13 hours - 1 week)
- Follow 7-phase plan from migration doc
- Use updated saxo-adapter v0.3.0
- **Output**: pivot-web2 fully migrated, internal Saxo code deleted

**Total Timeline**: 14-21 hours over 2 weeks

---

### Decision Point #10: Implementation Approach

**Question**: How should we sequence the work?

**Option A: Implement Blockers First (Recommended)**
1. **Week 1**: Implement 5 blockers in saxo-adapter
2. **Week 2**: Execute pivot-web2 migration using complete saxo-adapter
3. **Benefit**: Clean migration, no workarounds, full feature parity from day 1

**Option B: Parallel Development**
1. **Week 1**: Start migration, implement blockers in parallel
2. **Risk**: Context switching, potential rework if blocker signatures change

**Option C: Workaround Approach**
1. Migrate now, keep internal HTTP client for historical bars temporarily
2. Use polling instead of WebSocket subscriptions
3. Refactor to saxo-adapter later
4. **Downside**: Technical debt, duplicate code, delayed cleanup

**Recommendation**: **Option A** - The 6-8 hour investment to complete saxo-adapter ensures:
- No technical debt
- No duplicate code paths
- Clean separation of concerns from day 1
- Reference implementation (pivot-web) available for testing

---

### Joint Evolution Strategy

After migration completes:

1. **Both projects evolve together**: pivot-web2 discovers needs → saxo-adapter implements methods
2. **Feedback loop**: Real usage validates interface design
3. **Incremental additions**: Add Tier 2 "usual suspects" only when proven necessary
4. **No speculation**: Don't implement methods "because they might be useful"

**Example**:
- pivot-web2 adds UI watchlist feature → needs `GetInfoPrices()`
- Implement in saxo-adapter as experimental
- Test in production for 2-3 months
- Stabilize signature based on feedback
- Promote to stable core in v1.0

This approach ensures saxo-adapter serves **real needs**, not imagined ones.

---

## Summary & Next Steps

### Key Findings

1. **Production Requirements Are Clear**: pivot-web reveals exactly what's needed (not speculation)
2. **5 Critical Blockers Identified**: Must implement before migration can start
3. **WebSocket Pattern Is Reusable**: Once first 4 subscriptions work, adding more is trivial
4. **HTTP Endpoints Are Fast**: "The usual suspects" are simple GETs, 1-2 hours each
5. **Joint Evolution Works**: saxo-adapter and pivot-web2 should grow together based on real usage

### Implementation Roadmap

**v0.3.0 (December 2024) - Unblock Migration** 🔥
- ⚠️ Add `GetHistoricalBars()` - 2-3 hours
- ⚠️ Add `SubscribeToOrders()` - 2 hours (first WebSocket, sets pattern)
- ⚠️ Add `SubscribeToBalance()` - 1 hour (copy pattern)
- ⚠️ Add `SubscribeToSessionEvents()` - 1 hour (copy pattern)
- ⚠️ Complete `ModifyOrder()` - 1 hour
- 📋 Add `GetNetPositions()` - 1 hour (nice-to-have)
- 📋 Add `GetClosedPositions()` - 1 hour (nice-to-have)
- **Total**: 6-8 hours (critical), 9-10 hours (with nice-to-haves)
- **Output**: saxo-adapter is ready for pivot-web2 migration

**v0.4.0 - v0.6.0 (Jan-Apr 2025) - Joint Evolution**
- Add Tier 2 methods **only as needs emerge** from pivot-web2 usage
- Examples: `GetInfoPrices()`, `ClosePosition()`, `SubscribeToPositions()`, `GetTradingSchedule()`
- Mark all as "Experimental" - allow signature changes based on feedback
- **Total**: 15-20 hours over 3-4 months (incremental)

**v1.0.0 (Q2 2026) - Stabilization**
- Freeze Tier 1 stable core (no breaking changes allowed)
- Promote heavily-used Tier 2 methods to Tier 1
- Document Tier 3 as post-v1.0 roadmap
- Commit to semantic versioning

---

### Decision Point #10: Ready to Implement?

**Question**: Should we proceed with implementing the 5 critical blockers in saxo-adapter v0.3.0?

**Scope**:
- `GetHistoricalBars()` - HTTP GET with chart parsing
- `SubscribeToOrders()` - WebSocket subscription (first of 3)
- `SubscribeToBalance()` - WebSocket subscription
- `SubscribeToSessionEvents()` - WebSocket subscription
- Complete `ModifyOrder()` - Full parameter support

**Effort**: 6-8 hours

**Benefit**: Unlocks pivot-web2 migration, validates saxo-adapter design

**Reference**: Legacy `pivot-web/broker/` package provides proven implementation patterns

**Your decision**: Implement now, or defer to after migration with workarounds?

---

### Lessons Learned from This Analysis

1. **Start with Production Reality**: pivot-web showed what's actually needed vs theoretical retail trader
2. **WebSocket Is Critical for Automation**: 4 subscriptions are non-negotiable for pivot strategies
3. **Historical Data Is Essential**: Can't calculate pivots without bars
4. **Simple HTTP Methods Are Easy**: Most "usual suspects" are 1-2 hour implementations
5. **Interface Evolution Works**: Extension pattern allows growth without breaking consumers

### Pragmatic API Design Principles

✅ **DO**:
- Implement what production code **actually uses**
- Use legacy code as reference (it's proven in production)
- Add methods when **needs are proven**, not speculated
- Group related functionality (e.g., 4 WebSocket subscriptions use same pattern)
- Allow signature changes in v0.x based on real feedback

❌ **DON'T**:
- Implement methods because "retail traders might want them"
- Add features before consumers request them
- Freeze interfaces before validating with real usage
- Ignore production legacy code (it has lessons learned)
- Over-engineer for imagined future needs

**Bottom line**: Build for **today's proven needs**, evolve for **tomorrow's real feedback**.

---

**End of Document**

---

## Appendices

### Appendix A: Legacy pivot-web WebSocket Implementation Details

**File**: `pivot-web/broker/broker_websocket.go` (1524 lines)

**Architecture**:
- Singleton WebSocket instance (`GetWebSocketInstance()`)
- Separated reader/processor goroutines with buffered channels
- Reconnection logic with max 3 attempts
- Subscription monitoring with 70-second timeout detection
- Thread-safe operations with mutexes

**4 Production Subscriptions**:
```go
const priceSubscriptionPath = "/trade/v1/infoprices/subscriptions"
const orderStatusSubscriptionPath = "/port/v1/orders/subscriptions"
const sessionsSubscriptionPath = "/root/v1/sessions/events/subscriptions/active"
const portfolioBalancePath = "/port/v1/balances/subscriptions"
```

**Callback Pattern**:
```go
ws.callbackHandlers["ReferenceId"] = func(payload []byte) {
    // Handle subscription messages
}
```

**Key Insights for saxo-adapter**:
1. Use buffered channels (100 messages, 10 errors, 5 reconnection requests)
2. Track last message timestamp per subscription (staleness detection)
3. Separate reader (WebSocket I/O) from processor (business logic)
4. Graceful shutdown with goroutine tracking (wait for exit)
5. Reconnection throttling (don't hammer server)

**Reference for Implementation**: Study this file before implementing WebSocket subscriptions in saxo-adapter.

---

### Appendix B: Saxo API Version Strategy

| Service | Versions Available | Use in saxo-adapter | Rationale |
|---------|-------------------|---------------------|-----------|
| Orders | v1, v2 | **v2** | More parameters, better error handling |
| Positions | v1 | **v1** | Only version available |
| Prices | v1 | **v1** | Stable, proven |
| InfoPrices | v1 | **v1** | Stable, proven |
| Balances | v1 | **v1** | Only version available |
| Charts | v1, v3 | **v1** (HTTP), **v3** (WebSocket) | v1 for polling, v3 for streaming |
| Performance | v3, v4 | **v4** | Latest, enhanced metrics |
| Instruments | v1 | **v1** | Only version available |
| Exchanges | v1 | **v1** | Only version available |

**Strategy**: Use latest stable version by default. Document version in method comments. Allow version override via config if needed.

---

### Appendix C: Full Saxo Service Group Breakdown

For reference, all 15 Saxo service groups:

| Service Group | Priority for Retail | Methods Count | Notes |
|--------------|---------------------|---------------|-------|
| Trading | CRITICAL | 20+ | Orders, positions, prices, trades |
| Portfolio | CRITICAL | 15+ | Balances, accounts, positions (read-only) |
| Reference Data | HIGH | 25+ | Instruments, exchanges, currencies |
| Chart | MEDIUM | 5+ | Historical/streaming OHLC data |
| Account History | MEDIUM | 8+ | Performance, closed positions |
| Root Services | MEDIUM | 10+ | Sessions, capabilities, features |
| Market Overview | LOW | 5+ | Market movers, top trades |
| Client Services | VERY LOW | 5+ | Institutional client management |
| Corporate Actions | VERY LOW | 5+ | Dividends, splits, rights issues |
| Ens (Event Notifications) | LOW | 3+ | Alternative to WebSocket for some events |
| Disclaimer Management | VERY LOW | 2+ | Legal disclaimer tracking |
| Client Reporting | VERY LOW | 3+ | Regulatory reporting |
| Regulatory Services | VERY LOW | 2+ | MiFID II compliance |
| Partner Integration | VERY LOW | 5+ | White-label features |
| Value Add | LOW | 5+ | Premium data services (news, research) |

**Total**: ~120+ endpoints. Retail traders need ~20-30.

---

**End of Document**
