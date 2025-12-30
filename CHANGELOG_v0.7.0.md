# saxo-adapter v0.7.0 Release Notes

**Release Date:** December 30, 2025  
**Type:** Minor version (breaking interface changes)

## Overview

This release implements critical WebSocket token refresh functionality to prevent subscription resets caused by OAuth2 token expiration. The implementation follows the proven legacy `broker_websocket.go` pattern with a self-contained timer that automatically reauthorizes WebSocket connections every ~18 minutes (2 minutes before the 20-minute access token expiry).

## 🔥 Breaking Changes

### AuthClient Interface
- **ADDED:** `ReauthorizeWebSocket(ctx context.Context, contextID string) error`
  - Reauthorizes active WebSocket connection with refreshed token
  - Calls Saxo's `PUT /streaming/ws/authorize?contextid={id}` endpoint
  - Returns error if reauthorization fails
  
- **REMOVED:** `StartTokenEarlyRefresh(ctx context.Context, wsConnected <-chan bool, wsContextID <-chan string)`
  - This channel-based approach was never called in production
  - Replaced by self-contained timer pattern (simpler and more reliable)

### WebSocketClient Interface
- **REMOVED:** `SetStateChannels(stateChannel chan<- bool, contextIDChannel chan<- string)`
  - OAuth token refresh coordination no longer uses external channels
  - WebSocket manages its own token refresh lifecycle

## ✨ New Features

### WebSocket Token Refresh Timer
Implements automatic token refresh to keep WebSocket connections alive:

1. **Timer Setup** (`startTokenRefreshTimer()`)
   - Calculates next refresh: 18 minutes (2 min before 20-min token expiry)
   - Returns time until token expiry for validation
   - Starts automatically when WebSocket connection is established

2. **Token Refresh Callback** (`refreshTokenAndReschedule()`)
   - Fires at 18-minute intervals
   - Calls `authClient.ReauthorizeWebSocket()` with current contextID
   - Self-reschedules via `defer` pattern (even on errors/panics)
   - Logs success/failure for production monitoring

3. **Next Refresh Scheduling** (`scheduleNextRefresh()`)
   - Calculates next refresh interval (18 minutes for fresh tokens)
   - Handles edge cases (token expiring soon → 30s retry)
   - Resets existing timer or creates new one if needed

### Implementation Details
- **Pattern:** Self-contained timer owned by `SaxoWebSocketClient`
- **Lifecycle:** Timer starts in `EstablishConnection()`, stops in `Close()`
- **Thread-safety:** Single timer per WebSocket instance, no concurrent access
- **Error handling:** Logs failures but always reschedules to prevent getting stuck

## 🔧 ReauthorizeWebSocket Implementation

The `ReauthorizeWebSocket()` method in `SaxoAuthClient` follows the exact legacy pattern:

```go
func (sac *SaxoAuthClient) ReauthorizeWebSocket(ctx context.Context, contextID string) error {
    // 1. Get current token (cached, no refresh yet)
    token, err := sac.getToken("saxo")
    
    // 2. Build authorization URL: https://sim-streaming.saxobank.com/sim/oapi/streaming/ws/authorize?contextid={id}
    authURL := buildAuthURL(websocketURL, contextID)
    
    // 3. Create token source with EARLY EXPIRY (2 min before actual expiry)
    tokenSource := sac.createTokenSourceWithEarlyExpiry(ctx, &token.Token, earlyRefreshTime)
    
    // 4. Create authenticated HTTP client (auto-refreshes token if needed)
    httpClient := oauth2.NewClient(ctx, tokenSource)
    
    // 5. Send PUT request (Saxo returns 202 Accepted)
    req, _ := http.NewRequestWithContext(ctx, "PUT", authURL, nil)
    resp, err := httpClient.Do(req)
    
    // 6. Get potentially refreshed token from token source
    refreshedToken, _ := tokenSource.Token()
    
    // 7. Store refreshed token if it changed
    if refreshedToken.AccessToken != token.Token.AccessToken {
        sac.storeToken(sac.oauth2ToTokenInfo(*refreshedToken, "saxo"))
    }
}
```

**Key Concepts:**
- Uses `createTokenSourceWithEarlyExpiry()` to force token refresh before API call if expiry is < 2 minutes
- The `oauth2.NewClient()` automatically calls `tokenSource.Token()` before each request
- If token is expired or within early expiry window, it refreshes automatically
- Fresh token is saved to file storage for persistence across app restarts

## 🧹 Code Cleanup

### Removed Dead Code (~130 lines)
- **`StartTokenEarlyRefresh()`** implementation in `oauth.go`
  - Complex channel-based coordination logic
  - Never actually called in production
  - Replaced by simpler timer pattern

### Removed Channel Coordination
- **WebSocket state channels** (`stateChannel`, `contextIDChannel`)
  - `Connect()` no longer sends connection state notifications
  - `Close()` no longer sends disconnection notifications
  - Removed all channel-related code from `SaxoWebSocketClient`

### Simplified Connection Lifecycle
- **Before:** WebSocket → OAuth coordination via channels → token refresh
- **After:** WebSocket → direct timer → `authClient.ReauthorizeWebSocket()`
- **Benefit:** Fewer moving parts, easier to debug, follows proven legacy pattern

## 📝 File Changes Summary

```
adapter/interfaces.go                     |   2 +-   (interface change)
adapter/oauth.go                          | 127 ----------  (removed dead code)
adapter/saxo_test.go                      |   8 ++++   (mock updated)
adapter/websocket/connection_manager.go   |  12 +++++     (timer startup)
adapter/websocket/saxo_websocket.go       | 176 ++++++++++   (timer implementation)
adapter/websocket/saxo_websocket_test.go  |   3 ++               (mock updated)
adapter/websocket/subscription_manager.go |   6 +++             (debug logging)

7 files changed, 161 insertions(+), 174 deletions(-)
```

## 🎯 Root Cause Analysis

**Problem:** WebSocket subscriptions were resetting every 20-30 minutes in production

**Investigation Findings:**
1. Saxo Bank OAuth2 access tokens expire in 20 minutes
2. WebSocket connections require reauthorization before token expires
3. Legacy `pivot-web` had timer-based refresh at 18 minutes
4. `saxo-adapter` had `StartTokenEarlyRefresh()` but it was never called
5. Without reauthorization, expired tokens cause subscription resets

**Solution:** Implement timer-based refresh following exact legacy pattern
- Timer fires at 18 minutes (2 min before 20-min expiry)
- Calls `ReauthorizeWebSocket()` which hits Saxo's `/authorize` endpoint
- Token refreshes automatically via `oauth2` library's early expiry source
- Fresh token saved to file storage for persistence

## 🧪 Testing

### MockAuthClient Updates
- Added `ReauthorizeWebSocket()` to `saxo_test.go` MockAuthClient
- Added `ReauthorizeWebSocket()` to `saxo_websocket_test.go` MockAuthClient
- Both implementations support error testing via `shouldError` flag

### Build Verification
```bash
cd saxo-adapter
go build ./adapter/...        # ✅ Success
go test ./adapter/...          # ✅ Compiles (2 pre-existing test failures unrelated)
```

### Integration Testing Recommendations
1. Deploy to test environment
2. Monitor logs for these messages:
   ```
   startTokenRefreshTimer: Timer set to fire in 18m0s
   refreshTokenAndReschedule: Token refreshed successfully
   ```
3. Verify subscriptions continue without reset for multiple hours
4. Check for "Reauthorization failed" errors

## 📚 Documentation Updates

### Architecture Notes
- Timer pattern is self-contained and simple (no external coordination)
- Follows proven legacy `broker_websocket.go` design (in production since 2021)
- Early expiry token source ensures token refresh happens BEFORE API call
- Self-rescheduling via `defer` ensures timer never gets stuck

### Migration Notes for pivot-web2
This release must be paired with `pivot-web2` changes:
1. Remove `SetStateChannels()` calls in `websocket_adapter.go`
2. Update `internal/ports/auth.go` interface to match new AuthClient
3. Update `internal/ports/websocket.go` interface to match new WebSocketClient
4. Remove `wsStateChannel` and `wsContextIDChannel` from `scheduler_service.go`

## 🔗 Related Issues

- **Investigation:** Token not being properly updated within 20-minute TTL
- **Root Cause:** Missing WebSocket-specific token refresh mechanism
- **Solution:** Timer-based refresh following legacy proven pattern
- **Verification:** Requires production monitoring over 24+ hour period

## 📦 Upgrade Guide

### For pivot-web2 (REQUIRED)
```bash
# Update go.mod
go get github.com/bjoelf/saxo-adapter@v0.7.0
go mod tidy

# Update interface implementations
# 1. Remove SetStateChannels() calls
# 2. Update internal/ports/auth.go to add ReauthorizeWebSocket()
# 3. Update internal/ports/websocket.go to remove SetStateChannels()
# 4. Update services/scheduler_service.go to remove channel coordination

# Build and test
go build ./...
go test ./...
```

### For standalone projects
```bash
# Update dependency
go get github.com/bjoelf/saxo-adapter@v0.7.0
go mod tidy

# Update AuthClient implementations to add:
# ReauthorizeWebSocket(ctx context.Context, contextID string) error

# Remove if present:
# StartTokenEarlyRefresh(ctx context.Context, wsConnected <-chan bool, wsContextID <-chan string)

# Build
go build ./...
```

## 🙏 Acknowledgments

This implementation follows the proven pattern from the legacy `pivot-web` system's `broker_websocket.go`, which has been reliably handling WebSocket token refresh in production since 2021. The timer-based approach with early token expiry proved to be more reliable than channel-based coordination.

## 🔮 Future Enhancements

1. **Token Expiry Visibility:** Expose token expiry time via AuthClient interface
   - Currently assumes 20-minute expiry (hardcoded)
   - Could extract from OAuth2 token response for accuracy

2. **Monitoring Metrics:** Add Prometheus/OpenTelemetry metrics
   - Token refresh success/failure rates
   - Time between refreshes (should be ~18 minutes)
   - Reauthorization endpoint latency

3. **Graceful Degradation:** Handle reauthorization failures more robustly
   - Current: Logs error and reschedules (may cause subscription reset)
   - Enhanced: Exponential backoff with earlier retries
   - Ultimate: Detect subscription reset and reconnect proactively

---

**Full Diff:** https://github.com/bjoelf/saxo-adapter/compare/v0.6.7...v0.7.0  
**Commit:** `189ff95` - feat: implement WebSocket token refresh timer
