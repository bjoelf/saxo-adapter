# Saxo Adapter - Authentication Guide

## Overview

The saxo-adapter provides **automatic CLI-friendly OAuth authentication** with zero manual token generation. Just call `Login()` and your browser opens automatically!

## Authentication Flow

### 1. **First-Time Authentication (CLI)**

```go
authClient, err := saxo.CreateSaxoAuthClient(logger)
if err != nil {
    log.Fatal(err)
}

ctx := context.Background()
if err := authClient.Login(ctx); err != nil {
    log.Fatal(err)
}
// Browser opens automatically, you login, token saved to data/saxo_token.bin
```

**What happens:**
1. ✅ Temporary HTTP server starts on `localhost:8080`
2. ✅ Browser opens with Saxo Bank login page
3. ✅ You authenticate with Saxo Bank
4. ✅ OAuth callback captured automatically
5. ✅ Token saved to `data/saxo_token.bin`
6. ✅ Server shuts down
7. ✅ Token refresh starts automatically

**Output:**
```
🔐 Starting CLI OAuth authentication flow...
📍 OAuth callback URL: http://localhost:8080/oauth/callback
🌐 Starting temporary callback server on http://localhost:8080
🌐 Opening browser for authentication...
📋 If browser doesn't open, visit this URL manually:
   https://sim.logonvalidation.net/authorize?client_id=...
⏳ Waiting for authentication callback...
✅ Authorization code received
🔄 Exchanging authorization code for access token...
✅ Authentication successful! Token saved.
🔄 Token refresh manager started (auto-refresh every 58 minutes)
```

### 2. **Subsequent Runs (Automatic)**

```go
authClient, err := saxo.CreateSaxoAuthClient(logger)
// authClient is already authenticated from saved token!

if err := authClient.Login(ctx); err != nil {
    log.Fatal(err)
}
// Detects existing token, returns immediately
```

**Output:**
```
✅ Already authenticated with valid token
```

### 3. **Token Refresh (Automatic)**

The adapter automatically refreshes tokens in the background:

```go
// Start token refresh (called automatically by CreateBrokerServices)
authClient.StartAuthenticationKeeper("saxo")
// Refreshes every 58 minutes (before refresh token expires)

// For WebSocket apps, also start early refresh
authClient.StartTokenEarlyRefresh(ctx, wsConnected, wsContextID)
// Refreshes every 18 minutes (before access token expires)
```

**No action required** - tokens refresh automatically forever!

## Deployment Scenarios

### **Local Development**

```bash
cd examples/basic_auth
go run main.go
# Browser opens, login, token saved
```

### **Remote VM/Server (Headless)**

#### **Option 1: Pre-authenticate Locally**
```bash
# On your laptop
cd fx-collector
go run cmd/collector/main.go
# Login via browser, token saved to data/saxo_token.bin

# Upload token to VM
scp data/saxo_token.bin user@vm:/app/fx-collector/data/

# Run on VM (uses existing token)
ssh user@vm
cd /app/fx-collector
./fx-collector  # No authentication needed - runs forever!
```

#### **Option 2: SSH with X11 Forwarding**
```bash
ssh -X user@vm
cd /app/fx-collector
./fx-collector
# Browser opens on your local machine via X11
```

#### **Option 3: Manual URL Copy/Paste**
```bash
ssh user@vm
cd /app/fx-collector
./fx-collector
# Copy URL from terminal output
# Paste into browser on your laptop
# After login, copy authorization code from redirect URL
# Paste code back into terminal
```

### **Production Deployment**

```bash
# 1. Authenticate locally (one-time setup)
./fx-collector
# Token saved to data/saxo_token.bin

# 2. Deploy with token file
scp data/saxo_token.bin deploy@prod-server:/app/data/
scp .env deploy@prod-server:/app/

# 3. Run as systemd service
sudo systemctl start fx-collector
# Runs 24/7, tokens auto-refresh
```

## Environment Variables

```bash
# Required
export SAXO_ENVIRONMENT=sim        # or "live"
export SAXO_CLIENT_ID=your_id
export SAXO_CLIENT_SECRET=your_secret

# Optional
export PROVIDER=saxo               # Default: "saxo"
```

## Token Storage

Tokens are stored in:
```
data/saxo_token.bin
```

**Security Notes:**
- ✅ Binary format (not plain text)
- ✅ Contains access_token, refresh_token, expiry timestamps
- ✅ Automatically refreshed before expiration
- ⚠️  **Keep this file secure** - treat like a password!
- ⚠️  Add to `.gitignore`

## Token Lifecycle

```
┌─────────────────────────────────────────────────────────┐
│ First Run: authClient.Login()                           │
├─────────────────────────────────────────────────────────┤
│ 1. No token found                                       │
│ 2. Start localhost:8080 callback server                │
│ 3. Open browser → Saxo Bank login                      │
│ 4. User authenticates                                   │
│ 5. Callback → Exchange code for token                  │
│ 6. Save to data/saxo_token.bin                         │
│ 7. Start token refresh background process              │
└─────────────────────────────────────────────────────────┘
                         ↓
┌─────────────────────────────────────────────────────────┐
│ Subsequent Runs: authClient.Login()                     │
├─────────────────────────────────────────────────────────┤
│ 1. Token found in data/saxo_token.bin                  │
│ 2. Check if expired                                     │
│ 3. Auto-refresh if needed                              │
│ 4. Return immediately (no browser needed)              │
└─────────────────────────────────────────────────────────┘
                         ↓
┌─────────────────────────────────────────────────────────┐
│ Runtime: Automatic Token Refresh                        │
├─────────────────────────────────────────────────────────┤
│ StartAuthenticationKeeper():                            │
│   - Runs in background goroutine                        │
│   - Refreshes every 58 minutes                          │
│   - Uses refresh_token (valid 60 minutes)              │
│                                                          │
│ StartTokenEarlyRefresh() [WebSocket apps]:             │
│   - Runs when WebSocket connected                       │
│   - Refreshes every 18 minutes                          │
│   - Uses access_token (valid 20 minutes)               │
│   - Re-authorizes WebSocket via HTTP                    │
└─────────────────────────────────────────────────────────┘
```

## WebSocket Re-Authorization

For WebSocket applications (fx-collector, streaming examples):

```go
// Setup state channels for WebSocket coordination
wsStateChannel := make(chan bool, 1)
wsContextIDChannel := make(chan string, 1)
wsClient.SetStateChannels(wsStateChannel, wsContextIDChannel)

// Start WebSocket-aware token refresh
authClient.StartTokenEarlyRefresh(ctx, wsStateChannel, wsContextIDChannel)
```

**What happens automatically:**
1. ✅ Token refreshes every 18 minutes (before expiration)
2. ✅ WebSocket re-authorization via HTTP PUT to `/streaming/ws/authorize`
3. ✅ WebSocket connection stays alive 24/7
4. ✅ No reconnection needed

## Troubleshooting

### **"Browser doesn't open"**

**Solution:** URL is printed in terminal - copy and paste into browser manually.

### **"Port 8080 already in use"**

**Solution:** Stop other services using port 8080, or modify `callbackPort` in code.

### **"Token refresh failed"**

**Causes:**
- Network connectivity issues
- Invalid refresh token (expired after 60 minutes of inactivity)
- Saxo Bank API issues

**Solution:** Delete `data/saxo_token.bin` and re-authenticate.

### **"Authentication timeout (5 minutes)"**

**Cause:** No callback received within 5 minutes.

**Solution:** 
1. Check firewall allows `localhost:8080`
2. Check browser didn't block redirect
3. Re-run authentication

### **Remote VM: "Failed to connect to X11"**

**Cause:** No X11 forwarding enabled.

**Solutions:**
1. Use pre-authenticated token upload (Option 1 above)
2. Enable X11: `ssh -X user@vm`
3. Use manual URL copy/paste (Option 3 above)

## Examples

### **CLI Application (basic_auth)**
```bash
cd examples/basic_auth
export SAXO_ENVIRONMENT=sim
export SAXO_CLIENT_ID=your_id
export SAXO_CLIENT_SECRET=your_secret
go run main.go
# Browser opens automatically → Login → Done!
```

### **WebSocket Streaming (fx-collector)**
```bash
cd fx-collector
cp .env.example .env
# Edit .env with your credentials
go run cmd/collector/main.go
# Authenticates once, runs 24/7 with auto token refresh
```

### **Order Placement (place_order)**
```bash
cd examples/place_order
go run main.go
# Reuses token from previous authentication
# No browser needed if already authenticated
```

## Security Best Practices

1. ✅ **Never commit** `data/saxo_token.bin` to version control
2. ✅ **Use SIM environment** for testing (`SAXO_ENVIRONMENT=sim`)
3. ✅ **Secure token file** permissions: `chmod 600 data/saxo_token.bin`
4. ✅ **Rotate credentials** periodically
5. ✅ **Use LIVE only in production** with proper access controls

## Architecture

```
┌──────────────────────┐
│   Your Application   │
│  (CLI/Web/Service)   │
└──────────┬───────────┘
           │
           │ authClient.Login()
           ↓
┌──────────────────────┐
│  SaxoAuthClient      │
│  (oauth.go)          │
├──────────────────────┤
│ loginCLI()           │  → Temporary HTTP server on localhost:8080
│ openBrowser()        │  → Opens browser with OAuth URL
│ ExchangeCode()       │  → Exchanges code for token
│ StartAuthKeeper()    │  → Auto-refresh every 58min
│ StartTokenRefresh()  │  → Auto-refresh every 18min (WebSocket)
│ ReauthorizeWS()      │  → HTTP PUT re-authorization
└──────────┬───────────┘
           │
           │ Token Storage
           ↓
┌──────────────────────┐
│ data/saxo_token.bin  │
│ (Binary file)        │
└──────────────────────┘
```

## Summary

**Key Features:**
- ✅ **Zero manual work** - browser opens automatically
- ✅ **Cross-platform** - Linux, macOS, Windows
- ✅ **Token persistence** - authenticate once, run forever
- ✅ **Auto-refresh** - tokens refresh in background
- ✅ **WebSocket support** - automatic re-authorization
- ✅ **Deployment-friendly** - works locally and on remote servers
- ✅ **Junior-friendly** - just call `Login()` and it works!

**No web server needed at runtime** - temporary server only runs during initial authentication.
