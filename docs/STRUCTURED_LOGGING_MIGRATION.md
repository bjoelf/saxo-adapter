# Structured Logging Migration - Saxo-Adapter

## TL;DR

Migrate saxo-adapter from `*log.Logger` to `*slog.Logger` directly. **Breaking change** - requires consumer updates. Clean, simple migration.

## Timeline: 8-12 days

| Phase | Duration |
|-------|----------|
| 1. Core Adapter | 2-3 days |
| 2. WebSocket | 3-4 days |
| 3. Examples/Docs | 2-3 days |
| 4. Testing | 2-3 days |

---

## Phase 1: Core Adapter (2-3 days)

## Phase 1: Core Adapter (2-3 days)

### Update `adapter/saxo.go` and `adapter/oauth.go`

**Changes**:
1. Change all `logger *log.Logger` → `logger *slog.Logger` in structs
2. Update all constructors: `NewSaxoBrokerClient(authClient, baseURL, *slog.Logger)`
3. Convert ~30-40 log statements in saxo.go to structured format
4. Convert ~20-25 log statements in oauth.go to structured format

**Pattern**:
```go
// Before
sbc.logger.Printf("PlaceOrder: Processing order for %s", ticker)

// After
sbc.logger.Info("Processing order",
    "function", "PlaceOrder",
    "ticker", ticker,
    "order_type", orderType)
```

---

## Phase 2: WebSocket (3-4 days)

### Update `adapter/websocket/*.go`

**Files**:
- `connection_manager.go` - ~50-60 statements
- `subscription_manager.go` - ~30-40 statements
- `message_handler.go` - ~20-30 statements
- `saxo_websocket.go` - Change `logger *log.Logger` → `logger *slog.Logger`

**Log Levels**:
- DEBUG: Heartbeats, message parsing, cache hits
- INFO: Connection established, subscriptions created
- WARN: Reconnection attempts
- ERROR: Connection failures, auth failures

---

## Phase 3: Examples/Docs (2-3 days)

### Update all examples + create docs

**Examples** (4 files):
- `basic_auth/main.go`
- `websocket_prices/main.go`
- `place_order/main.go`
- `historical_data/main.go`

**Documentation**:
- `docs/LOGGING.md` - Main guide (how to configure LOG_LEVEL, LOG_FORMAT)
- `docs/MIGRATION_GUIDE.md` - Consumer breaking changes + migration steps
- Update `README.md` - Show new slog usage
- Update `docs/ARCHITECTURE.md` - Logging patterns

---

## Phase 4: Testing (2-3 days)

**Unit tests** (≥85% coverage):
- slog integration tests
- Level parsing
- Output format validation

**Integration tests**:
- Full broker operations with slog
- WebSocket lifecycle logging
- pivot-web2 consumer update

**Performance benchmarks**:
- Verify DEBUG logs = zero cost when disabled
- Overhead < 10% for enabled logs

---

## API Design (Breaking Changes)

```go
// OLD (v0.4.x)
import saxo "github.com/bjoelf/saxo-adapter/adapter"

logger := log.New(os.Stdout, "[SAXO] ", log.LstdFlags)
brokerClient := saxo.NewSaxoBrokerClient(authClient, baseURL, logger)

// NEW (v0.5.0+)
import saxo "github.com/bjoelf/saxo-adapter/adapter"

logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
    Level: slog.LevelInfo,
}))
brokerClient := saxo.NewSaxoBrokerClient(authClient, baseURL, logger)

// Or use default logger
slog.SetDefault(logger)
brokerClient := saxo.NewSaxoBrokerClient(authClient, baseURL, slog.Default())
```

---

## Log Level Guidelines

| Level | Use For |
|-------|---------|
| DEBUG | Cache hits, message parsing, function traces (SILENT in prod) |
| INFO | Orders placed, connections established, successful ops |
| WARN | Reconnection attempts, token expiry warnings |
| ERROR | Order failures, connection failures, auth errors |

---

## Breaking Changes

**All constructors change from `*log.Logger` to `*slog.Logger`:**

- `NewSaxoBrokerClient(authClient, baseURL, *slog.Logger)`
- `NewSaxoWebSocketClient(authClient, *slog.Logger)`
- `CreateBrokerServices(authClient, *slog.Logger)`

**Consumer Impact:**
- pivot-web2 must update all saxo-adapter instantiations
- Replace `log.New()` with `slog.New()` or `slog.Default()`
- One-time migration when upgrading to v0.5.0

**Version Strategy:**
- v0.4.x - Current with `*log.Logger`
- v0.5.0 - Breaking change to `*slog.Logger`

---

## Success Criteria

- ✅ All `*log.Logger` replaced with `*slog.Logger`
- ✅ Test coverage ≥ 85%
- ✅ Performance overhead < 10%
- ✅ All examples working with slog
- ✅ pivot-web2 migration completed
- ✅ Migration guide created for consumers
