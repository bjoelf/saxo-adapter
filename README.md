# Saxo Bank Adapter for Go

Saxo Bank OpenAPI integration providing OAuth2 authentication, REST API client, and WebSocket streaming for Go applications.

## Status

🚧 **Under Development** - Session 2 Complete

**Current State**: Repository structure initialized, code copied from pivot-web2. Next steps will update imports to make this a standalone package.

## Features

- OAuth2 authentication flow with automatic token refresh
- RESTful API client for Saxo Bank OpenAPI
- WebSocket streaming for real-time price feeds
- Order placement and management
- Position and portfolio tracking
- Instrument data enrichment
- Comprehensive test coverage with mock servers

## Installation

```bash
go get github.com/bjoelf/saxo-adapter@latest
```

## Directory Structure

```
saxo-adapter/
├── adapter/              # Main Saxo adapter implementation
│   ├── oauth.go         # OAuth2 authentication (672 lines)
│   ├── saxo.go          # Main broker client (846 lines)
│   ├── market_data.go   # Market data client (375 lines)
│   ├── types.go         # Saxo-specific types (385 lines)
│   ├── config.go        # Configuration (58 lines)
│   ├── instrument_adapter.go  # Instrument enrichment (254 lines)
│   └── websocket/       # WebSocket client (2,584 lines)
│       ├── saxo_websocket.go
│       ├── connection_manager.go
│       ├── subscription_manager.go
│       ├── message_handler.go
│       └── ...
└── docs/                # Documentation
```

## Dependencies

**External packages**:
- `golang.org/x/oauth2` - OAuth2 authentication
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
