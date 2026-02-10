# Development Guide

## 🛠️ Setup Development Environment

### Prerequisites

1. **Go 1.25.6+**
   ```bash
   # Check version
   go version
   
   # Install if needed (Linux/macOS)
   wget https://go.dev/dl/go1.25.6.linux-amd64.tar.gz
   sudo tar -C /usr/local -xzf go1.25.6.linux-amd64.tar.gz
   export PATH=$PATH:/usr/local/go/bin
   ```

2. **Docker** (for runtime operations)
   ```bash
   # Install Docker (Ubuntu/Debian)
   sudo apt-get update
   sudo apt-get install docker.io
   sudo usermod -aG docker $USER
   # Logout and login again
   ```

3. **Development Tools**
   ```bash
   # Install air for hot-reload
   go install github.com/air-verse/air@latest
   
   # Install golangci-lint
   curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin v1.55.2
   ```

### Project Setup

1. **Clone and Setup**
   ```bash
   git clone https://github.com/bassista/gospin.git
   cd gospin
   
   # Install dependencies
   go mod tidy
   
   ```

## 🔄 Development Workflow

### Daily Development

```bash
# 1. Start with clean state
go vet ./...
go fmt ./...

# 2. Run tests
go test ./...

# 3. Start development server
air -c .air.toml    # Linux/macOS
air -c .air_win.toml # Windows

# 4. Make changes (auto-reload with air)

# 5. Before commit
golangci-lint run
go test ./... -cover
go mod tidy
```


### Testing Strategy

#### Unit Tests
```bash
# Run all tests
go test ./...

# Run with coverage
go test ./... -coverprofile=coverage.out
go tool cover -html=coverage.out -o coverage.html

# Run specific package
go test ./internal/cache/...

# Run with race detection
go test -race ./...

# Verbose output
go test -v ./...
```


#### Testing Patterns

**Interface Testing:**
```go
func TestContainerRuntime(t *testing.T) {
    tests := []struct {
        name     string
        runtime  runtime.ContainerRuntime
        want     bool
    }{
        {"docker", runtime.NewDockerRuntime(), true},
        {"memory", runtime.NewMemoryRuntime(), true},
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Test both implementations
        })
    }
}
```

**Mock Usage:**
```go
type MockRuntime struct {
    StartFunc func(string) error
}

func (m *MockRuntime) Start(name string) error {
    if m.StartFunc != nil {
        return m.StartFunc(name)
    }
    return nil
}
```

## 📁 Project Structure Deep Dive

### Core Components

#### 1. **Application Container** (`internal/app/`)
Dependency injection and application lifecycle management.

```go
type App struct {
    Config     *config.Config
    Store      cache.AppStore  
    Runtime    runtime.ContainerRuntime
    Repository repository.Repository
    Scheduler  *scheduler.PollingScheduler
}
```

#### 2. **Cache Layer** (`internal/cache/`)
In-memory data store with dirty tracking and async persistence.

**Key Files:**
- `store.go` - Main cache implementation
- `interfaces.go` - Store contracts
- `scheduler.go` - Persistence scheduler

**Pattern:**
```go
// Controllers don't persist directly
func (c *Controller) UpdateContainer(ctx *gin.Context) {
    // 1. Update in cache
    store.UpdateContainer(container)
    
    // 2. Mark dirty (triggers async save)
    store.MarkDirty()
    
    // 3. Return immediately
    ctx.JSON(200, container)
}
```

#### 3. **Runtime Abstraction** (`internal/runtime/`)
Pluggable container runtime implementations.

**Structure:**
```
runtime/
├── runtime.go          # Interface definitions
├── factory.go          # Factory pattern implementation  
├── docker_runtime.go   # Docker implementation
├── memory_runtime.go   # Test/mock implementation
└── *_test.go          # Tests
```

**Usage:**
```go
// Factory creates appropriate runtime
runtime := runtime.NewRuntimeFromConfig(cfg.RuntimeType, store)

// Uniform interface regardless of implementation
err := runtime.Start("container-name")
```

#### 4. **Repository Pattern** (`internal/repository/`)
Data persistence with file watching and optimistic locking.

**Features:**
- JSON-based storage
- File system watching (`fsnotify`)
- Optimistic locking with timestamps
- Auto-reload on external changes

#### 5. **Scheduler Engine** (`internal/scheduler/`)
Time-based container lifecycle management.

**Algorithm:**
```
Every N seconds (configurable):
1. Get current time in configured timezone
2. For each active schedule:
   a. Check if current time matches any timer
   b. Determine if container should start/stop
   c. Execute runtime operation if needed
3. Log operations and errors
```

### API Layer Architecture

#### Controllers (`internal/api/controller/`)
Business logic and request handling.

**Patterns:**
- Each entity (Container, Group, Schedule) has dedicated controller
- CRUD operations follow consistent patterns
- Runtime operations separated from data operations
- Validation using struct tags

#### Middleware (`internal/api/middleware/`)
Cross-cutting concerns.

**Current Middleware:**
- CORS handling
- Request timeout
- Logging (via gin middleware)

#### Routing (`internal/api/route/`)
HTTP route configuration and mounting.

**Structure:**
```go
func SetupRoutes(app *app.App) *gin.Engine {
    r := gin.New()
    
    // Global middleware
    r.Use(middleware.CORSMiddleware(app.Config.CORSAllowedOrigins))
    
    // Route groups
    api := r.Group("/")
    setupContainerRoutes(api, app)
    setupGroupRoutes(api, app)
    // ...
}
```

## 🧪 Testing Guidelines

### Test Coverage Requirements

- **Minimum**: 80% overall coverage
- **Critical paths**: 90%+ coverage (runtime, persistence)
- **Controllers**: Test all HTTP endpoints
- **Business logic**: Test all edge cases

### Mock Strategy

**When to Mock:**
- External dependencies (Docker API, file system)
- Slow operations (network, disk I/O)
- Non-deterministic behavior (time, randomness)

**When NOT to Mock:**
- Pure functions
- Simple data structures  
- Configuration loading

### Integration Tests

**Docker Integration:**
```bash
# Run with real Docker (requires Docker daemon)
GO_SPIN_MISC_RUNTIME_TYPE=docker go test ./internal/runtime/

# Run with mock runtime (no Docker required)
GO_SPIN_MISC_RUNTIME_TYPE=memory go test ./internal/runtime/
```

**File System Integration:**
```go
func TestFileWatching(t *testing.T) {
    // Create temporary directory
    tmpDir := t.TempDir()
    
    // Setup repository with temp file
    repo := repository.NewJSONRepository(filepath.Join(tmpDir, "config.json"))
    
    // Test file watching behavior
}
```

## 🚀 Build & Deployment

### Build Process

```bash
# Development build
go build -o .build/main ./cmd/server/main.go

# Production build (optimized)
CGO_ENABLED=0 go build -ldflags="-w -s" -o .build/main ./cmd/server/main.go

# Cross-compilation examples
GOOS=linux GOARCH=amd64 go build -o .build/main-linux ./cmd/server/main.go
GOOS=windows GOARCH=amd64 go build -o .build/main.exe ./cmd/server/main.go
```

### Docker Development

**Development with hot-reload:**
```bash
docker-compose -f dev.docker-compose.yml up
```

**Production build:**
```bash
docker build -t go-spin:latest .
docker run -p 8084:8084 -v /var/run/docker.sock:/var/run/docker.sock go-spin:latest
```

### Release Checklist

- [ ] All tests passing: `go test ./...`
- [ ] Linting clean: `golangci-lint run`
- [ ] Coverage >80%: `go test -cover ./...`
- [ ] Dependencies updated: `go mod tidy`
- [ ] Version bumped in appropriate files
- [ ] CHANGELOG.md updated
- [ ] Documentation updated
- [ ] Docker image builds successfully
- [ ] Manual smoke testing completed

## 🔍 Debugging

### Debug Mode

**Enable verbose logging:**
```yaml
# config.yaml
misc:
  gin_mode: debug
```

**Environment variable:**
```bash
GO_SPIN_MISC_GIN_MODE=debug ./main
```

### Debugging Tools

**Delve Debugger:**
```bash
# Install
go install github.com/go-delve/delve/cmd/dlv@latest

# Debug main application
dlv debug ./cmd/server/main.go

# Debug specific test
dlv test ./internal/cache -- -test.run TestStore
```

**VS Code Debug Configuration (`.vscode/launch.json`):**
```json
{
    "version": "0.2.0",
    "configurations": [
        {
            "name": "Debug gospin",
            "type": "go",
            "request": "launch",
            "mode": "debug",
            "program": "./cmd/server/main.go",
            "env": {
                "GO_SPIN_MISC_GIN_MODE": "debug",
                "GO_SPIN_MISC_RUNTIME_TYPE": "memory"
            },
            "args": []
        },
        {
            "name": "Debug Tests",
            "type": "go", 
            "request": "launch",
            "mode": "test",
            "program": "${workspaceFolder}/internal/cache",
            "buildFlags": "-race"
        }
    ]
}
```

### Logging Best Practices

**Structured Logging:**
```go
import "github.com/bassista/go_spin/internal/logger"

// Component-specific logger
log := logger.WithComponent("scheduler")

// Context logging
log.WithFields(logrus.Fields{
    "container": containerName,
    "operation": "start",
    "duration": elapsed,
}).Info("Container operation completed")

// Error context
log.WithError(err).Error("Failed to start container")
```

**Log Levels:**
- `Debug`: Detailed internal information
- `Info`: General operational messages
- `Warn`: Potential issues, recoverable errors
- `Error`: Serious problems requiring attention

## 🤝 Contributing Guidelines

### Code Style

**Follow Go conventions:**
```bash
# Format code
go fmt ./...

# Lint code  
golangci-lint run

# Vet code
go vet ./...
```

**Naming Conventions:**
- Packages: lowercase, single word
- Types: PascalCase (exported), camelCase (internal)
- Functions: PascalCase (exported), camelCase (internal)  
- Constants: PascalCase or UPPER_SNAKE_CASE
- Interfaces: Usually end with -er (Reader, Writer)

**Comment Standards:**
```go
// Package cache provides in-memory storage with dirty tracking
package cache

// AppStore defines the interface for application data storage
type AppStore interface {
    // GetDocument returns the current data document
    GetDocument() *DataDocument
}

// NewStore creates a new store instance with the given document
func NewStore(doc *DataDocument) *Store {
    // Implementation...
}
```

### Commit Message Format

Follow [Conventional Commits](https://conventionalcommits.org/):

```
<type>[optional scope]: <description>

[optional body]

[optional footer(s)]
```

**Examples:**
```
feat(api): add container group management endpoints
fix(scheduler): resolve timezone handling for schedules  
docs: update API documentation with new endpoints
test(cache): add unit tests for dirty tracking
refactor(runtime): extract common Docker operations
```

### Pull Request Process

1. **Branch Naming:**
   - `feature/description` - New features
   - `fix/description` - Bug fixes  
   - `docs/description` - Documentation
   - `refactor/description` - Code improvements

2. **PR Checklist:**
   - [ ] Tests added/updated
   - [ ] Documentation updated
   - [ ] Code formatted and linted
   - [ ] No breaking changes (or documented)
   - [ ] Performance implications considered

3. **Review Requirements:**
   - At least one approving review
   - All CI checks passing
   - No merge conflicts

## 📚 Resources

### Go Resources
- [Effective Go](https://golang.org/doc/effective_go)
- [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments)
- [Standard Go Project Layout](https://github.com/golang-standards/project-layout)

### Docker API
- [Moby Client Documentation](https://pkg.go.dev/github.com/docker/docker/client)
- [Docker Engine API](https://docs.docker.com/engine/api/)

### Testing
- [Go Testing Package](https://pkg.go.dev/testing)
- [Testify Assertions](https://github.com/stretchr/testify)

### Tools
- [Air - Hot Reload](https://github.com/air-verse/air)
- [Delve - Debugger](https://github.com/go-delve/delve)
- [golangci-lint](https://golangci-lint.run/)