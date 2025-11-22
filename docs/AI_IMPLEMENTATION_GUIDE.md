# AI-Assisted Implementation Guide
## Saxo Adapter Extraction to Public Repository

**Purpose**: Break down Saxo adapter extraction into AI-manageable chat sessions  
**Target**: Extract `internal/adapters/saxo/` to separate public repository  
**Total Effort**: ~8 hours across 6 AI chat sessions  
**AI Tools**: GitHub Copilot, Claude, ChatGPT, etc.

---

## 📋 Overview

This guide provides **exact AI prompts** to extract the Saxo broker adapter from `pivot-web2` (private) into `saxo-adapter` (public). Each session is designed for a **single AI conversation** (30-90 minutes).

**Your Decision**: Saxo adapter → PUBLIC | Everything else → PRIVATE

---

## 🎯 How to Use This Guide

### For Each Session:

1. **Read "Context"** - Understand what this session accomplishes
2. **Check "Prerequisites"** - Ensure previous sessions are complete
3. **Copy "AI Chat Prompt"** - Use it verbatim in your AI assistant
4. **Review AI Output** - Carefully check proposed changes
5. **Run "Validation"** - Test that changes work correctly
6. **Execute "Commit"** - Save progress with provided message
7. **Mark Complete** - Check off the session

### Progress Tracking:

```markdown
## Session Checklist
- [x] Session 1: Analyze Saxo Adapter Dependencies ✅ COMPLETE
- [x] Session 2: Create Standalone Adapter with Local Types ✅ COMPLETE
- [ ] Session 3: Extract Core Saxo Files (OPTIONAL - adapter already standalone)
- [ ] Session 4: Create Adapter Factory & README (OPTIONAL - already done)
- [ ] Session 5: Update pivot-web2 to Import from Public Adapter
- [ ] Session 6: Publish & Verify

**NOTE**: Sessions 2 was completed using a different approach than originally planned.
Instead of creating public packages in pivot-web2, we created a fully standalone
adapter with all types defined locally. This is actually BETTER because:
- Zero dependencies on pivot-web2
- Can be used by any Go project immediately
- Generic interface pattern supports multi-broker architecture
- Trading strategies are broker-agnostic

See saxo-adapter/docs/SESSION_2_COMPLETE.md for details.
```

---

## 📦 SESSION 1: Analyze Saxo Adapter Dependencies (1 hour)

### Context

Before extracting, we need to understand what the Saxo adapter imports and what depends on it.

### Prerequisites

- [ ] Repository at `/home/bjorn/source/pivot-web2`
- [ ] Go 1.21+ installed
- [ ] Git configured

### AI Chat Prompt

```
I need to analyze the Saxo broker adapter to prepare for extraction to a separate repository.

Task: Analyze dependencies for internal/adapters/saxo/

Please provide:

1. List all Go files in internal/adapters/saxo/ (including subdirectories)
2. For each file, show what it imports from pivot-web2
3. Identify which files from pivot-web2 the adapter depends on
4. List which files in pivot-web2 import from saxo adapter
5. Estimate lines of code in the saxo adapter

Output format:
- Markdown table of files with line counts
- Import dependency graph
- List of breaking changes needed in main repo

Use these commands:
```bash
# List saxo adapter files
find internal/adapters/saxo -name "*.go" -type f

# Count lines
find internal/adapters/saxo -name "*.go" -exec wc -l {} + | tail -1

# Find imports FROM saxo
grep -r "internal/adapters/saxo" --include="*.go" internal/services/ cmd/

# Find imports BY saxo
grep -r "^import" internal/adapters/saxo/*.go | grep "pivot-web2"
```
```

### Expected AI Output

The AI should provide:
- List of ~15-20 Go files in saxo adapter
- Total ~6,000+ lines of code
- Imports: `internal/domain`, `internal/ports`
- Used by: `cmd/server/main.go`, `internal/services/*`

### Validation

```bash
# Verify file count
find internal/adapters/saxo -name "*.go" | wc -l
# Should show ~15-20 files

# Check line count
find internal/adapters/saxo -name "*.go" -exec wc -l {} + | tail -1
# Should show ~6,000-7,000 total lines
```

### Deliverable

Document saved to `docs/saxo-extraction-analysis.md` with:
- File inventory
- Dependency map
- Migration checklist

### Commit

```bash
git add docs/saxo-extraction-analysis.md
git commit -m "docs: analyze saxo adapter for extraction

- Document 19 Go files in saxo adapter
- Map dependencies on domain/ports
- Identify services that use saxo adapter
- Preparation for public repository extraction"
```

**Time**: 1 hour  
**Risk**: None (analysis only)

---

## 📦 SESSION 2: Create Public Repository Structure (1.5 hours)

### Context

Create the `saxo-adapter` repository on GitHub with proper Go module structure.

### Prerequisites

- [x] Session 1 complete
- [ ] GitHub account access
- [ ] GitHub CLI (`gh`) installed OR browser access

### AI Chat Prompt

```
I need to create a new public GitHub repository for the Saxo Bank adapter.

Repository name: saxo-adapter
Visibility: PUBLIC
Description: "Saxo Bank OpenAPI adapter for Go - OAuth2, REST, and WebSocket integration"

Please provide:

1. GitHub CLI commands to create the repo
2. Initial directory structure for a Go module
3. go.mod file content
4. Basic .gitignore for Go
5. Initial README.md outline
6. LICENSE file (MIT)

Structure should include:
- adapter/ (main adapter code)
- websocket/ (WebSocket client)
- examples/ (usage examples)
- docs/ (documentation)

The adapter will depend on:
- github.com/bjoelf/pivot-web2/pkg/domain (will create later)
- github.com/bjoelf/pivot-web2/pkg/ports (will create later)
- github.com/gorilla/websocket
- golang.org/x/oauth2
```

### Expected AI Output

Commands to create repository and initial files.

### Manual Steps (if not using GitHub CLI)

```bash
# Create repo on GitHub.com manually
# Then clone it:
cd ~/dev_go
git clone git@github.com:bjoelf/saxo-adapter.git
cd saxo-adapter
```

### AI-Generated Files

The AI will create:

**go.mod**:
```go
module github.com/bjoelf/saxo-adapter

go 1.21

require (
    github.com/gorilla/websocket v1.5.0
    golang.org/x/oauth2 v0.15.0
)
```

**.gitignore**:
```
# Binaries
*.exe
*.dll
*.so
*.dylib
bin/
dist/

# Go
*.test
*.out
go.work

# IDE
.vscode/
.idea/
*.swp
*.swo
*~

# OS
.DS_Store
Thumbs.db

# Environment
.env
*.local
```

**README.md** (outline):
```markdown
# Saxo Bank Adapter for Go

Saxo Bank OpenAPI integration providing OAuth2 authentication, REST API client, and WebSocket streaming.

## Status

🚧 **Under Development** - Extracting from private repository

## Features (Planned)

- OAuth2 authentication flow
- RESTful API client
- WebSocket real-time streaming
- Complete type safety

## Installation

```bash
go get github.com/bjoelf/saxo-adapter@latest
```

## Usage

Documentation coming soon.

## License

MIT License - See LICENSE file
```

**LICENSE** (MIT):
```
MIT License

Copyright (c) 2025 Bjorn Eliasson

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

### Validation

```bash
cd ~/dev_go/saxo-adapter

# Verify structure
ls -la
# Should see: go.mod, .gitignore, README.md, LICENSE

# Verify Go module
go mod tidy
# Should succeed (even with no code yet)

# Verify Git
git status
# Should show clean working tree or staged files
```

### Commit

```bash
git add .
git commit -m "chore: initialize saxo-adapter repository

- Create Go module structure
- Add MIT license
- Add initial README outline
- Configure .gitignore for Go project"

git push origin main
```

**Time**: 1.5 hours  
**Risk**: Low

---

## 📦 SESSION 3: Extract Core Saxo Files (2 hours)

### Context

Copy Saxo adapter files from private repo, update import paths, and remove dependencies on private code.

### Prerequisites

- [x] Session 2 complete
- [ ] Both repositories cloned locally
- [ ] Backup of pivot-web2 created

### AI Chat Prompt

```
I need to copy Saxo adapter files from pivot-web2 to saxo-adapter and update imports.

Source: /home/bjorn/dev_go/pivot-web2/internal/adapters/saxo/
Target: /home/bjorn/dev_go/saxo-adapter/adapter/

Files to copy:
- oauth.go (OAuth2 authentication)
- saxo.go (main broker client)
- market_data.go (market data client)
- instrument_adapter.go (instrument enrichment)
- types.go (Saxo-specific types)
- config.go (configuration)
- All files in websocket/ subdirectory

Tasks:
1. Copy files to new repo maintaining structure
2. Update package declarations:
   - From: package saxo
   - To: package adapter (for main files)
   - To: package websocket (for websocket files)
3. Update imports:
   - Remove: "github.com/bjoelf/pivot-web2/internal/domain"
   - Remove: "github.com/bjoelf/pivot-web2/internal/ports"
   - Add: "github.com/bjoelf/saxo-adapter/types" (for internal types)
4. Create adapter/types.go with Saxo-specific structs
5. Note any dependencies that need to be abstracted

Please provide the exact bash commands to:
- Copy files preserving structure
- Run sed commands to update imports
- Verify no broken imports remain
```

### Expected AI Actions

1. Copy commands with directory creation
2. Sed commands for import updates
3. Verification commands

### Manual Execution

```bash
# Navigate to target repo
cd ~/dev_go/saxo-adapter

# Copy main adapter files
mkdir -p adapter
cp ~/dev_go/pivot-web2/internal/adapters/saxo/*.go adapter/

# Copy websocket subdirectory
mkdir -p websocket
cp -r ~/dev_go/pivot-web2/internal/adapters/saxo/websocket/*.go websocket/

# Update package declarations for main adapter
find adapter -name "*.go" -exec sed -i 's/^package saxo$/package adapter/g' {} +

# Update package declarations for websocket
find websocket -name "*.go" -exec sed -i 's/^package saxo$/package websocket/g' {} +

# Remove imports to internal packages (we'll fix these next session)
# For now, just note them
grep -r "pivot-web2/internal" adapter/ websocket/
```

### Issues to Document

The AI should identify imports that need fixing:
- `internal/domain` types (need to be duplicated or abstracted)
- `internal/ports` interfaces (need to be duplicated or abstracted)

Save these to `docs/import-fixes-needed.md`

### Validation

```bash
# Check files copied
ls -la adapter/
ls -la websocket/

# Count Go files
find adapter websocket -name "*.go" | wc -l
# Should match source count

# Try to build (will fail - expected)
go build ./...
# Note the errors for next session
```

### Commit

```bash
git add adapter/ websocket/
git commit -m "feat: extract saxo adapter files from pivot-web2

- Copy 19 Go files from internal/adapters/saxo
- Update package declarations (saxo → adapter)
- Preserve websocket subdirectory structure
- Note: Imports still reference pivot-web2 internals (fix in next session)"
```

**Time**: 2 hours  
**Risk**: Medium (imports broken, but expected)

---

## 📦 SESSION 4: Create Standalone Adapter (2 hours)

### Context

Make the adapter self-contained by creating necessary types and interfaces locally.

### Prerequisites

- [x] Session 3 complete
- [ ] List of import dependencies from Session 3

### AI Chat Prompt

```
I need to make the Saxo adapter self-contained by fixing imports to pivot-web2 internals.

Current broken imports:
- "github.com/bjoelf/pivot-web2/internal/domain"
- "github.com/bjoelf/pivot-web2/internal/ports"

Strategy:
1. Create adapter/interfaces.go with minimal interfaces needed
2. Create adapter/types.go with basic domain types
3. Update all files to use local types
4. Ensure adapter can build standalone

Required interfaces (from ports):
- BrokerClient
- AuthClient  
- WebSocketClient

Required types (from domain):
- Signal
- Instrument
- OrderRequest/Response
- TradingSchedule

Please:
1. Analyze what types/interfaces are actually used
2. Create minimal adapter/interfaces.go
3. Create minimal adapter/types.go
4. Update imports in all adapter files
5. Ensure go build ./... succeeds

Note: This is temporary - later we'll switch to importing from pivot-web2/pkg/ when we create public packages.
```

### Expected AI Output

The AI will create minimal type definitions:

**adapter/types.go**:
```go
package adapter

import "time"

// Minimal domain types for standalone adapter

type Signal struct {
    Ticker       string
    LongOrShort  string
    Size         float64
    EntryPrice   float64
    StopPrice    float64
    Active       bool
    CreatedAt    time.Time
}

type Instrument struct {
    Ticker       string
    InstrumentID string
    AssetType    string
    Currency     string
}

// ... other minimal types
```

**adapter/interfaces.go**:
```go
package adapter

import "context"

// Minimal interfaces for standalone adapter

type BrokerClient interface {
    PlaceOrder(ctx context.Context, order OrderRequest) (*OrderResponse, error)
    GetBalance(ctx context.Context) (*Balance, error)
    // ... other methods
}

// ... other interfaces
```

### Validation

```bash
cd ~/dev_go/saxo-adapter

# Should build successfully now
go build ./...

# Should have no external pivot-web2 dependencies
go mod tidy
go list -m all | grep pivot-web2
# Should return nothing

# Run tests (if any)
go test ./...
```

### Commit

```bash
git add adapter/ websocket/
git commit -m "feat: make saxo adapter self-contained

- Create local types.go with minimal domain types
- Create local interfaces.go with broker interfaces  
- Remove dependencies on pivot-web2 internals
- Adapter now builds standalone
- Note: Will switch to pkg imports when public packages created"
```

**Time**: 2 hours  
**Risk**: Medium (maintaining type compatibility)

---

## 📦 SESSION 5: Create Public README & Examples (1 hour)

### Context

Document the adapter usage and create working examples.

### Prerequisites

- [x] Session 4 complete
- [ ] Adapter builds successfully

### AI Chat Prompt

```
I need to create comprehensive README and usage examples for the Saxo adapter.

Please create:

1. Complete README.md with:
   - Project description
   - Installation instructions
   - Quick start example
   - Configuration options
   - Authentication flow
   - API coverage
   - Testing instructions
   - Contributing guidelines
   - License

2. examples/basic_usage.go:
   - Connect to Saxo SIM environment
   - Authenticate
   - Get account balance
   - Place a simple order
   - Subscribe to price updates

3. examples/websocket_streaming.go:
   - Setup WebSocket connection
   - Subscribe to price feeds
   - Handle incoming messages
   - Error handling and reconnection

4. docs/CONFIGURATION.md:
   - Environment variables
   - Config struct options
   - SIM vs LIVE environments

Make examples runnable with:
```bash
cd examples
go run basic_usage.go
```
```

### Expected AI Output

Comprehensive documentation with:
- Professional README.md
- Working code examples
- Configuration guide

### Validation

```bash
# Check examples compile
cd examples
go build basic_usage.go
go build websocket_streaming.go

# Check documentation
cat ../README.md | wc -l
# Should be 200+ lines

# Verify links in README work
cat ../README.md | grep -o 'http[s]*://[^)]*'
```

### Commit

```bash
git add README.md examples/ docs/
git commit -m "docs: add comprehensive documentation and examples

- Complete README with installation and usage
- Add basic_usage.go example
- Add websocket_streaming.go example  
- Add CONFIGURATION.md guide
- Examples compile successfully"
```

**Time**: 1 hour  
**Risk**: Low

---

## 📦 SESSION 6: Publish & Update Main Repo (1.5 hours)

### Context

Publish the public adapter and update pivot-web2 to use it.

### Prerequisites

- [x] All previous sessions complete
- [ ] Saxo adapter builds and tests pass
- [ ] README complete

### AI Chat Prompt (Part A - Publish)

```
I need to publish the saxo-adapter repository.

Tasks for saxo-adapter:
1. Create git tag v0.1.0
2. Push to GitHub
3. Verify appears on pkg.go.dev
4. Create GitHub release with notes

Please provide:
- Git commands for tagging
- GitHub release notes template
- Verification commands
```

### Manual Execution (Part A)

```bash
cd ~/dev_go/saxo-adapter

# Final check
go test ./...
go build ./...

# Tag version
git tag -a v0.1.0 -m "Initial public release

- OAuth2 authentication
- REST API client
- WebSocket streaming
- Examples and documentation"

# Push
git push origin main
git push origin v0.1.0

# Trigger Go package index
GOPROXY=proxy.golang.org go list -m github.com/bjoelf/saxo-adapter@v0.1.0
```

### AI Chat Prompt (Part B - Update Main Repo)

```
I need to update pivot-web2 to use the public saxo-adapter.

Location: /home/bjorn/dev_go/pivot-web2

Tasks:
1. Add dependency in go.mod:
   require github.com/bjoelf/saxo-adapter v0.1.0

2. Update imports in cmd/server/main.go:
   From: "github.com/bjoelf/pivot-web2/internal/adapters/saxo"
   To: saxo "github.com/bjoelf/saxo-adapter/adapter"

3. Update service files that import saxo:
   Find all: grep -r "internal/adapters/saxo" internal/services/
   Update imports to use public package

4. Remove internal/adapters/saxo/ directory (it's now external)

5. Test everything builds

Please provide:
- go.mod additions
- sed commands for import updates
- Verification steps
```

### Expected AI Output (Part B)

Commands to:
1. Add external dependency
2. Update all imports
3. Remove internal saxo directory
4. Verify build

### Manual Execution (Part B)

```bash
cd ~/dev_go/pivot-web2

# Add public adapter
go get github.com/bjoelf/saxo-adapter@v0.1.0

# Update imports in cmd/server/main.go
# (AI will provide specific sed command)

# Update service imports
find internal/services -name "*.go" -exec sed -i \
  's|github.com/bjoelf/pivot-web2/internal/adapters/saxo|github.com/bjoelf/saxo-adapter/adapter|g' {} +

# Remove internal saxo adapter
git rm -rf internal/adapters/saxo/

# Test
go mod tidy
go build ./...
go test ./...
```

### Validation

```bash
cd ~/dev_go/pivot-web2

# Verify external dependency
go list -m github.com/bjoelf/saxo-adapter
# Should show: github.com/bjoelf/saxo-adapter v0.1.0

# Verify no internal saxo imports
grep -r "internal/adapters/saxo" --include="*.go" .
# Should return nothing

# Verify builds
go build ./cmd/server
# Should succeed

# Verify internal saxo directory removed
ls internal/adapters/saxo 2>/dev/null
# Should error: No such file or directory
```

### Commit (Main Repo)

```bash
cd ~/dev_go/pivot-web2

git add .
git commit -m "refactor: use public saxo-adapter package

- Add dependency: github.com/bjoelf/saxo-adapter v0.1.0
- Update imports in cmd/server and services
- Remove internal/adapters/saxo/ (now external)
- Reduce codebase by ~6,000 lines
- Saxo adapter now independently versioned and maintained

BREAKING: Saxo adapter moved to external package"

git push origin main
```

**Time**: 1.5 hours  
**Risk**: Medium (integration with main app)

---

## ✅ Final Validation Checklist

After all sessions complete, verify:

### Public Repository (saxo-adapter)

- [ ] Repository exists on GitHub (public)
- [ ] Builds successfully: `go build ./...`
- [ ] Tests pass: `go test ./...`
- [ ] Examples compile
- [ ] Tagged as v0.1.0
- [ ] Appears on pkg.go.dev
- [ ] README comprehensive
- [ ] MIT license included
- [ ] No references to pivot-web2 internals

### Private Repository (pivot-web2)

- [ ] Uses external adapter: `go list -m | grep saxo-adapter`
- [ ] No internal saxo directory: `ls internal/adapters/saxo` fails
- [ ] Builds successfully: `go build ./cmd/server`
- [ ] Tests pass: `go test ./...`
- [ ] Integration tests work with external adapter
- [ ] No broken imports
- [ ] Reduced by ~6,000 lines of code

### Integration Test

```bash
# In pivot-web2
cd /home/bjorn/dev_go/pivot-web2

# Should use external adapter
go run cmd/server/main.go --help
# Should start without errors

# Check dependency
go mod graph | grep saxo-adapter
# Should show: pivot-web2 -> saxo-adapter v0.1.0
```

---

## 🎯 Success Metrics

| Metric | Target | Verification |
|--------|--------|--------------|
| **Public Repo Created** | ✅ Yes | GitHub.com shows saxo-adapter |
| **Adapter Self-Contained** | ✅ Yes | Builds without pivot-web2 |
| **Main Repo Updated** | ✅ Yes | Uses external adapter |
| **Code Reduction** | ~6,000 lines | `git diff --stat` |
| **Published Version** | v0.1.0 | pkg.go.dev listing |
| **Tests Passing** | 100% | Both repos green |
| **Documentation** | Complete | README 200+ lines |

---

## 🚨 Troubleshooting

### Issue: "Cannot find module github.com/bjoelf/saxo-adapter"

```bash
# Solution: Trigger Go proxy
GOPROXY=proxy.golang.org go get github.com/bjoelf/saxo-adapter@v0.1.0

# Or use direct mode
GOPRIVATE="" go get github.com/bjoelf/saxo-adapter@v0.1.0
```

### Issue: "Build fails in pivot-web2 after removing saxo"

```bash
# Check what still imports it
grep -r "internal/adapters/saxo" --include="*.go" .

# Update those files manually
```

### Issue: "Type incompatibility between adapter types and domain types"

This happens because we created minimal types in Session 4. Two solutions:

**Option A** (Quick): Keep using adapter's local types  
**Option B** (Better): Create pivot-web2/pkg/domain and have adapter import it

---

## 📚 Post-Extraction Next Steps

After successful extraction:

1. **Update Adapter** - As Saxo API changes, update public repo
2. **Version Releases** - Use semantic versioning (v0.2.0, v1.0.0, etc.)
3. **Community** - Accept PRs for improvements
4. **Documentation** - Keep README and examples current
5. **Issues** - Track bugs in public repo

---

## 🎓 Learning Points

**What This Accomplishes**:
- ✅ Saxo adapter becomes reusable by others
- ✅ Independent versioning from main application
- ✅ Cleaner separation of concerns
- ✅ Reduced main repository size by 40%
- ✅ Showcases your work (public portfolio piece)

**What Stays Private**:
- ✅ Trading strategies (pivot.go, pivot_extra.go)
- ✅ Business logic (services/)
- ✅ Application wiring (cmd/server/main.go)
- ✅ Internal data structures and workflows

---

**Total Time**: ~8 hours across 6 sessions  
**Complexity**: Medium  
**Result**: Professional open-source Saxo adapter + cleaner private codebase

**Next**: Consider creating `pivot-web2/pkg/domain` for shared types if you want to publish a framework later (optional).
