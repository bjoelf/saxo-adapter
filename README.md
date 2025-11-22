# Saxo Bank Adapter for Go

**Status**: 🚧 **Session 1 Complete** - Code copied from pivot-web2, ready for Session 2

## Overview

This repository will contain the Saxo Bank OpenAPI adapter extracted from the pivot-web2 trading platform. The adapter provides OAuth2 authentication, RESTful API client, and WebSocket streaming functionality for Saxo Bank's trading API.

## Current State

✅ **Session 1 Complete** (Nov 22, 2025)
- All adapter code copied from pivot-web2 (6,317 lines)
- Directory structure preserved
- Ready for Go module initialization

⏳ **Next: Session 2** - Repository Structure Creation
- Initialize Go module
- Create LICENSE (MIT)
- Set up .gitignore
- Create proper README

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
└── docs/                # Documentation (to be created)
```

## Dependencies (Current)

**Note**: These imports currently reference pivot-web2 and will be updated in Sessions 3-4:

- `github.com/bjoelf/pivot-web2/internal/domain` → Will become local types
- `github.com/bjoelf/pivot-web2/internal/ports` → Will become local interfaces
- `github.com/bjoelf/pivot-web2/internal/adapters/storage` → Will be abstracted

**External packages** (will remain):
- `golang.org/x/oauth2` - OAuth2 authentication
- `github.com/gorilla/websocket` - WebSocket client
- `github.com/stretchr/testify` - Testing

## Features (Planned)

Once extraction is complete, this adapter will provide:

- ✅ OAuth2 authentication flow with automatic token refresh
- ✅ RESTful API client for Saxo Bank OpenAPI
- ✅ WebSocket streaming for real-time price feeds
- ✅ Order placement and management
- ✅ Position and portfolio tracking
- ✅ Instrument data enrichment
- ✅ Comprehensive test coverage with mock servers

## Extraction Progress

- [x] **Session 1**: Analyze dependencies & copy code ✅
- [ ] **Session 2**: Create repository structure
- [ ] **Session 3**: Extract core files & update imports
- [ ] **Session 4**: Create adapter factory & README
- [ ] **Session 5**: Update pivot-web2 to use public adapter
- [ ] **Session 6**: Publish & verify

## License

To be added in Session 2 (MIT License planned)

## References

- Implementation guide: `pivot-web2/docs/workflows/refactoring-best-practice/AI_IMPLEMENTATION_GUIDE.md`
- Analysis document: `pivot-web2/docs/saxo-extraction-analysis.md`
- Session log: `SESSION_1_COMPLETE.md`
