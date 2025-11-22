# Saxo Bank Adapter for Go

**Standalone Saxo Bank OpenAPI adapter for Go** - OAuth2 authentication, REST API client, and WebSocket streaming.

## Status

✅ **Session 2 Complete** - Standalone repository with no external dependencies

**What Works**:
- ✅ OAuth2 authentication with automatic token refresh
- ✅ RESTful API client for orders, positions, and market data
- ✅ WebSocket streaming for real-time price feeds
- ✅ Fully self-contained - no imports from pivot-web2
- ✅ All core types and interfaces defined locally

**What's Next**:
- Fix remaining test failures
- Re-enable and fix `instrument_adapter.go` (optional)
- Add usage examples
- Publish first release

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

# SIM credentials
export SIM_CLIENT_ID=your_sim_client_id
export SIM_CLIENT_SECRET=your_sim_secret

# LIVE credentials (use with caution!)
export LIVE_CLIENT_ID=your_live_client_id  
export LIVE_CLIENT_SECRET=your_live_secret

# Optional
export TOKEN_STORAGE_PATH=./data  # Default: data/
export PROVIDER=saxo  # Default: saxo
```

## Architecture

This adapter is designed to be imported by trading applications. It provides:

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
go test ./adapter -v -run Integration
```

## License

MIT License - See LICENSE file

## Notes

- `instrument_adapter.go` is temporarily disabled - will be re-enabled in future updates
- This adapter is extracted from the pivot-web2 trading platform
- Designed to be a general-purpose Saxo Bank adapter for any Go application

- `github.com/gorilla/websocket` - WebSocket client
- `github.com/stretchr/testify` - Testing framework

**Note**: Currently contains references to `pivot-web2` internal packages which will be replaced with local types in upcoming sessions.

## Usage

Documentation will be added as the extraction progresses. This adapter is being extracted from a private trading platform to become a reusable public package.

## Extraction Progress

- [x] **Session 1**: Analyze dependencies & copy code ✅
- [x] **Session 2**: Create repository structure ✅
- [ ] **Session 3**: Extract core files & update imports
- [ ] **Session 4**: Create adapter factory & README
- [ ] **Session 5**: Update pivot-web2 to use public adapter
- [ ] **Session 6**: Publish & verify

## License

MIT License - See [LICENSE](LICENSE) file

## References

- Implementation guide: [AI_IMPLEMENTATION_GUIDE.md](https://github.com/bjoelf/pivot-web2/blob/main/docs/workflows/refactoring-best-practice/AI_IMPLEMENTATION_GUIDE.md)
- Analysis document: [saxo-extraction-analysis.md](https://github.com/bjoelf/pivot-web2/blob/main/docs/workflows/refactoring-best-practice/saxo-extraction-analysis.md)
