# Architecture & Design Patterns

## 🏗️ System Architecture Overview

GoSpin follows a modular, interface-driven architecture that prioritizes testability, maintainability, and extensibility.

### High-Level Architecture

```mermaid
graph TB
    subgraph "External Layer"
        UI[Web UI - Alpine.js]
        API[REST API - Gin]
        CLI[CLI Tools]
    end
    
    subgraph "Application Layer"
        CTL[Controllers]
        MW[Middleware]
        ROUTE[Routes]
    end
    
    subgraph "Business Layer"
        CACHE[Cache Store]
        SCHED[Scheduler]
        APP[App Container]
    end
    
    subgraph "Infrastructure Layer"
        REPO[Repository]
        RUNTIME[Runtime]
        CONFIG[Config]
        LOG[Logger]
    end
    
    subgraph "External Dependencies"
        DOCKER[Docker Daemon]
        FS[File System]
        TIME[System Clock]
    end
    
    UI --> API
    API --> CTL
    CTL --> CACHE
    CTL --> RUNTIME
    CACHE --> REPO
    SCHED --> CACHE
    SCHED --> RUNTIME
    RUNTIME --> DOCKER
    REPO --> FS
    SCHED --> TIME
    
    style UI fill:#e1f5fe
    style API fill:#f3e5f5
    style CACHE fill:#fff3e0
    style RUNTIME fill:#e8f5e8
```

## 🎯 Design Principles

### 1. Separation of Concerns
Each package has a single, well-defined responsibility:
- **Controllers**: Handle HTTP requests and responses
- **Cache**: Manage in-memory data with persistence tracking
- **Runtime**: Abstract container lifecycle operations
- **Repository**: Handle data persistence and file operations
- **Scheduler**: Time-based automation logic

### 2. Dependency Inversion
High-level modules don't depend on low-level modules. Both depend on abstractions.

```go
// High-level module (Controller) depends on abstraction
type ContainerController struct {
    store   cache.AppStore          // Interface
    runtime runtime.ContainerRuntime // Interface
}

// Low-level modules implement interfaces
type DockerRuntime struct{} // Implements ContainerRuntime
type MemoryRuntime struct{} // Implements ContainerRuntime
```

### 3. Interface Segregation
Interfaces are kept minimal and focused:

```go
// Focused interface for runtime operations
type ContainerRuntime interface {
    Start(name string) error
    Stop(name string) error
    IsRunning(name string) (bool, error)
}

// Separate interface for data operations
type Repository interface {
    Load() (*DataDocument, error)
    Save(*DataDocument) error
    StartWatching(callback func()) error
}
```

### 4. Single Responsibility
Each component has one reason to change:
- **Cache**: Data storage and dirty tracking
- **Scheduler**: Time-based execution
- **Runtime**: Container operations
- **Repository**: File persistence

## 🔄 Core Patterns

### 1. Repository Pattern

**Purpose**: Abstract data access and provide a uniform interface for data operations.

**Implementation**:
```go
type Repository interface {
    Load() (*DataDocument, error)
    Save(*DataDocument) error
    StartWatching(callback func()) error
    StopWatching() error
}

type JSONRepository struct {
    filePath string
    watcher  *fsnotify.Watcher
    // ...
}
```

**Benefits**:
- Easy to test with mock implementations
- Can switch storage backends (JSON → SQL → NoSQL)
- Centralizes data access logic
- Enables file watching for external changes

### 2. Cache-Aside Pattern

**Purpose**: Improve performance by keeping frequently accessed data in memory.

**Flow**:
```mermaid
sequenceDiagram
    participant C as Controller
    participant Cache as Cache Store
    participant R as Repository
    
    C->>Cache: GetContainer(name)
    Cache->>C: Container data
    
    C->>Cache: UpdateContainer(container)
    Cache->>Cache: Update in-memory
    Cache->>Cache: Mark as dirty
    
    Note over Cache: Async persistence
    Cache->>R: Save(document)
```

**Implementation**:
```go
type Store struct {
    document *DataDocument
    dirty    bool
    mu       sync.RWMutex
}

func (s *Store) UpdateContainer(container *Container) {
    s.mu.Lock()
    defer s.mu.Unlock()
    
    s.document.Containers[container.Name] = container
    s.dirty = true // Triggers async persistence
}
```

### 3. Factory Pattern

**Purpose**: Create runtime implementations based on configuration.

**Implementation**:
```go
func NewRuntimeFromConfig(runtimeType string, store cache.AppStore) (ContainerRuntime, error) {
    switch runtimeType {
    case "docker":
        client, err := client.NewClientWithOpts(
            client.FromEnv,
            client.WithAPIVersionNegotiation(),
        )
        if err != nil {
            return nil, fmt.Errorf("failed to create Docker client: %w", err)
        }
        return &DockerRuntime{client: client, store: store}, nil
        
    case "memory":
        return &MemoryRuntime{store: store}, nil
        
    default:
        return nil, fmt.Errorf("unsupported runtime type: %s", runtimeType)
    }
}
```

**Benefits**:
- Easy to add new runtime implementations
- Configuration-driven runtime selection
- Enables testing without Docker dependency

### 4. Observer Pattern (File Watching)

**Purpose**: Automatically reload configuration when external changes occur.

**Implementation**:
```go
func (r *JSONRepository) StartWatching(callback func()) error {
    watcher, err := fsnotify.NewWatcher()
    if err != nil {
        return err
    }
    
    go func() {
        for {
            select {
            case event := <-watcher.Events:
                if event.Op&fsnotify.Write == fsnotify.Write {
                    callback() // Notify of changes
                }
            case err := <-watcher.Errors:
                log.WithError(err).Error("File watcher error")
            }
        }
    }()
    
    return watcher.Add(r.filePath)
}
```

### 5. Strategy Pattern (Runtime Implementations)

**Purpose**: Switch between different container runtime strategies.

```go
// Strategy interface
type ContainerRuntime interface {
    Start(name string) error
    Stop(name string) error
    IsRunning(name string) (bool, error)
}

// Docker strategy
type DockerRuntime struct {
    client docker.APIClient
    store  cache.AppStore
}

// Memory strategy (for testing)
type MemoryRuntime struct {
    containers map[string]bool
    store      cache.AppStore
}
```

## 🧩 Component Deep Dive

### Cache Store Architecture

```go
type Store struct {
    // Core data
    document *DataDocument
    
    // State tracking
    dirty    bool
    lastSave time.Time
    
    // Concurrency control
    mu sync.RWMutex
    
    // Dependencies
    repository Repository
    logger     *logrus.Entry
}
```

**Key Features**:
- **Thread-safe**: Uses RWMutex for concurrent access
- **Dirty tracking**: Only persists when changes occur
- **Optimistic locking**: Uses timestamps to detect conflicts
- **Event-driven**: Reloads automatically on file changes

**Dirty Tracking Flow**:
```mermaid
stateDiagram-v2
    [*] --> Clean
    Clean --> Dirty: Data Modified
    Dirty --> Persisting: Background Save
    Persisting --> Clean: Save Complete
    Persisting --> Error: Save Failed
    Error --> Dirty: Retry
```

### Scheduler Architecture

```go
type PollingScheduler struct {
    store        cache.AppStore
    runtime      runtime.ContainerRuntime
    pollInterval time.Duration
    timezone     *time.Location
    enabled      bool
    stopCh       chan bool
    logger       *logrus.Entry
}
```

**Scheduling Algorithm**:
```mermaid
flowchart TD
    Start([Start Scheduler]) --> Wait[Wait Poll Interval]
    Wait --> Check{Enabled?}
    Check -->|No| Wait
    Check -->|Yes| GetTime[Get Current Time]
    GetTime --> GetSchedules[Get Active Schedules]
    GetSchedules --> Loop{For Each Schedule}
    Loop --> CheckTimer[Check Timer Match]
    CheckTimer --> Action{Action Needed?}
    Action -->|Start| StartContainer[Start Container]
    Action -->|Stop| StopContainer[Stop Container]
    Action -->|None| Loop
    StartContainer --> Loop
    StopContainer --> Loop
    Loop -->|Done| Wait
```

**Time Handling**:
```go
func (s *PollingScheduler) shouldExecuteTimer(timer *Timer) (action string) {
    now := time.Now().In(s.timezone)
    weekday := int(now.Weekday())
    
    // Check if today is scheduled
    if !contains(timer.Days, weekday) {
        return "none"
    }
    
    // Parse start and stop times
    startTime, _ := time.Parse("15:04", timer.StartTime)
    stopTime, _ := time.Parse("15:04", timer.StopTime)
    
    // Create today's schedule times
    todayStart := time.Date(now.Year(), now.Month(), now.Day(),
        startTime.Hour(), startTime.Minute(), 0, 0, s.timezone)
    todayStop := time.Date(now.Year(), now.Month(), now.Day(),
        stopTime.Hour(), stopTime.Minute(), 0, 0, s.timezone)
    
    // Determine action based on current time
    if now.After(todayStart) && now.Before(todayStop) {
        return "start"
    } else {
        return "stop"
    }
}
```

### Runtime Abstraction

**Docker Runtime Implementation**:
```go
type DockerRuntime struct {
    client docker.APIClient
    store  cache.AppStore
    logger *logrus.Entry
}

func (d *DockerRuntime) Start(name string) error {
    // 1. Get container from store
    container := d.store.GetContainer(name)
    if container == nil {
        return ErrContainerNotFound
    }
    
    // 2. Check if already running
    running, err := d.IsRunning(name)
    if err != nil {
        return err
    }
    if running {
        return nil // Already running
    }
    
    // 3. Start container via Docker API
    err = d.client.ContainerStart(context.Background(), name, types.ContainerStartOptions{})
    if err != nil {
        return fmt.Errorf("failed to start container %s: %w", name, err)
    }
    
    // 4. Update store state
    container.Running = true
    d.store.UpdateContainer(container)
    
    d.logger.WithField("container", name).Info("Container started")
    return nil
}
```

**Memory Runtime (Testing)**:
```go
type MemoryRuntime struct {
    containers map[string]bool
    store      cache.AppStore
    mu         sync.Mutex
}

func (m *MemoryRuntime) Start(name string) error {
    m.mu.Lock()
    defer m.mu.Unlock()
    
    container := m.store.GetContainer(name)
    if container == nil {
        return ErrContainerNotFound
    }
    
    // Simulate container start
    m.containers[name] = true
    container.Running = true
    m.store.UpdateContainer(container)
    
    return nil
}
```

### Ghostboard Store

**Design** (`internal/ghostboard`):
- Boards live in one `Store` guarded by a mutex. Each board keeps its text, a revision
  counter, the last update time, its open event streams and the pending idle-clear timer.
- `Update` is a no-op when the text is unchanged, so a typing pause does not bump the revision.
  Otherwise it stores the text, increments the revision and fans an `Event` out to every
  subscriber. Subscribers get a buffered channel (16 events): a slow stream drops intermediate
  events and converges on the next full snapshot, which always carries the whole text.
- The text is dropped `ghostboard.clear_delay_secs` after the last write **or** the last
  subscriber leaving, whichever happens last. Every schedule increments a generation counter
  captured by the timer, so a tick that fires while a write re-arms the clear cannot empty the
  fresh text.
- `StartPersistence` flushes dirty boards to a dedicated JSON file every
  `ghostboard.persist_interval_secs` (empty boards are pruned) and flushes once more on shutdown.

**HTTP surface** (`internal/api/route/ghostboard_route.go` and
`internal/api/controller/ghostboard_controller.go`):
- `GET`/`POST /ghostboard/:board` — JSON board state; a write accepts a JSON body, a form field
  or the raw text.
- `GET /ghostboard/:board/events` — Server-Sent Events stream with the initial snapshot, every
  change and a keep-alive comment; it bypasses gzip and the per-request timeout.
- `GET`/`POST /api/boards/:board` — plain-text surface for CLIs; `POST` stores the raw body byte
  for byte, rejects invalid UTF-8 with `400` and answers `204 No Content`.
- `ui/ghostboard.html` and `ui/assets/ghostboard.js` render one independent board per
  `/ui/ghostboard/<board>` path.

**CLI** (`cmd/board`, client in `internal/boardclient`):
- `gb-cli` is a thin HTTP client of `/api/boards/:board`, so it works locally, in a
  container or against a remote host without touching the persistence file. `add` is a read
  followed by a write; `set`, `read`, `upload` and `download` map to one request each.
- `follow` consumes `/ghostboard/:board/events` with a client that carries no overall timeout (the
  stream stays open until Ctrl-C) and prints every revision, so the CLI shows the same live
  updates as the web page. The stream is read line by line without a fixed buffer: an event carries
  the whole board, whose size the server already bounds with `ghostboard.max_text_bytes`, so a
  client-side cap could only disagree with that limit.
- The configuration is resolved in `internal/boardclient` (defaults, YAML file next to the
  executable or in `$XDG_CONFIG_HOME/ghostboard/config.yaml` / `~/.config/ghostboard/config.yaml`,
  `GO_SPIN_BOARD_*` environment) and the command-line flags are applied on top by `cmd/board`.
  `Config.Validate` runs on the effective values, so a flag always corrects a mistyped file entry;
  `--no-config` skips every file and `ConfigPaths` lists the candidates.
- `config` prints the effective configuration, the file that was read and (with `--paths`) every
  candidate, without contacting the server, which makes a wrong URL or an unused file visible
  immediately. It also reports the keys the CLI does not understand and the environment variables
  that replaced a file value, so a mistyped key never looks like a default. `version`, `completion`
  and `config` deliberately run before the validation and never touch the network.
- `read --stat` and `read --json` use the JSON board API (`/ghostboard/:board`) because only that
  route carries the revision and the update time; every other read uses the plain-text route. The
  CLI turns the zero update time of a board that was never written into an absent field, so a
  consumer never sees a timestamp in year 1.
- Text and JSON never share stdout: `download -` and `follow` reject `--json`, `read --json`
  rejects `--stat`, and `download` rejects `-o` together with a file argument. These are reported
  as misuse (exit code `2`), the same code the CLI uses for a negative `--retry`, a non-positive
  `--timeout` or a text given both as an argument and with `-f`.
- The client retries transient failures (`429`, `502`, `503`, `504` and transport errors) with an
  exponential backoff and can send extra headers (`--header`, `GO_SPIN_BOARD_TOKEN` as a Bearer
  token), for a GoSpin instance behind an authenticating proxy.
- Failures are typed: `EndpointError` (a 404 on a board route), `BoardNameError` and `misuseError`
  map to the dedicated exit codes `3`, `4` and `2`, so a script can tell a wrong URL, a wrong board
  name and a command line mistake from a generic error.
- `edit` round-trips the board through `$VISUAL`/`$EDITOR` and writes only when the temporary file
  changed; `completion` renders bash, zsh and fish scripts from the command and flag tables in
  `cmd/board/completion.go`.
- Board names are validated with `ghostboard.ValidateBoardName` before a request is made, so a
  malformed name never reaches the server.

## 🔌 Extension Points

### Adding New Runtime Types

1. **Create Runtime Implementation**:
```go
type KubernetesRuntime struct {
    client kubernetes.Interface
    store  cache.AppStore
}

func (k *KubernetesRuntime) Start(name string) error {
    // Kubernetes-specific implementation
}
```

2. **Update Factory**:
```go
func NewRuntimeFromConfig(runtimeType string, store cache.AppStore) (ContainerRuntime, error) {
    switch runtimeType {
    case "docker":
        return NewDockerRuntime(store)
    case "memory":
        return NewMemoryRuntime(store)
    case "kubernetes":
        return NewKubernetesRuntime(store)
    // ...
    }
}
```

### Adding New Storage Backends

1. **Implement Repository Interface**:
```go
type PostgresRepository struct {
    db *sql.DB
}

func (p *PostgresRepository) Load() (*DataDocument, error) {
    // Load from PostgreSQL
}

func (p *PostgresRepository) Save(*DataDocument) error {
    // Save to PostgreSQL
}
```

2. **Update Configuration**:
```yaml
data:
  backend: "postgres"  # json, postgres, mongodb, etc.
  connection_string: "postgres://..."
```

### Adding New Middleware

```go
func AuthMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        // Authentication logic
        token := c.GetHeader("Authorization")
        if !isValidToken(token) {
            c.AbortWithStatus(http.StatusUnauthorized)
            return
        }
        c.Next()
    }
}

// In route setup
r.Use(middleware.AuthMiddleware())
```

## 🧪 Testing Architecture

### Test Strategy Layers

```mermaid
pyramid TB
    title Testing Pyramid
    
    subgraph "E2E Tests"
        API["API Integration Tests"]
        UI["UI Tests (Future)"]
    end
    
    subgraph "Integration Tests"
        HTTP["HTTP Handler Tests"]
        DOCKER["Docker Runtime Tests"]
        FILE["File System Tests"]
    end
    
    subgraph "Unit Tests"
        LOGIC["Business Logic"]
        UTILS["Utilities"]
        MOCK["Mock Implementations"]
    end
```

### Test Patterns

**Table-Driven Tests**:
```go
func TestContainerController_CreateContainer(t *testing.T) {
    tests := []struct {
        name           string
        input          Container
        existingData   *DataDocument
        expectedStatus int
        expectedError  string
    }{
        {
            name:           "valid new container",
            input:          Container{Name: "nginx", Active: true},
            existingData:   &DataDocument{Containers: make(map[string]*Container)},
            expectedStatus: 201,
        },
        {
            name:           "duplicate container name",
            input:          Container{Name: "nginx", Active: true},
            existingData:   &DataDocument{Containers: map[string]*Container{"nginx": {}}},
            expectedStatus: 400,
            expectedError:  "container already exists",
        },
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Test implementation
        })
    }
}
```

**Mock Interfaces**:
```go
type MockRuntime struct {
    StartFunc     func(string) error
    StopFunc      func(string) error
    IsRunningFunc func(string) (bool, error)
}

func (m *MockRuntime) Start(name string) error {
    if m.StartFunc != nil {
        return m.StartFunc(name)
    }
    return nil
}
```

**Integration Test Setup**:
```go
func setupTestApp(t *testing.T) *app.App {
    // Create temp directory for test data
    tmpDir := t.TempDir()
    
    // Create test configuration
    cfg := &config.Config{
        Data: config.DataConfig{
            FilePath: filepath.Join(tmpDir, "test.json"),
        },
        Misc: config.MiscConfig{
            RuntimeType: "memory", // Use memory runtime for tests
        },
    }
    
    // Initialize app with test dependencies
    return app.NewApp(cfg)
}
```

## 🔄 Data Flow Architecture

### Request Processing Flow

```mermaid
sequenceDiagram
    participant Client
    participant Gin
    participant Controller
    participant Cache
    participant Repository
    participant Runtime
    participant Docker
    
    Client->>Gin: HTTP Request
    Gin->>Controller: Route to Handler
    Controller->>Cache: Read/Write Data
    Cache->>Cache: Mark Dirty (if write)
    Controller->>Runtime: Execute Action (if needed)
    Runtime->>Docker: Docker API Call
    Docker-->>Runtime: Response
    Runtime-->>Controller: Result
    Controller-->>Gin: JSON Response
    Gin-->>Client: HTTP Response
    
    Note over Cache,Repository: Async Persistence
    Cache->>Repository: Save (if dirty)
    Repository->>Repository: Write to File
```

### Event Flow Architecture

```mermaid
stateDiagram-v2
    [*] --> Idle
    
    Idle --> HTTPRequest: API Call
    HTTPRequest --> ProcessRequest: Route to Controller
    ProcessRequest --> UpdateCache: Modify Data
    UpdateCache --> MarkDirty: Set Dirty Flag
    MarkDirty --> Idle: Return Response
    
    Idle --> ScheduleTick: Timer Event
    ScheduleTick --> CheckSchedules: Evaluate Timers
    CheckSchedules --> ExecuteAction: Action Needed
    ExecuteAction --> UpdateCache: Update State
    
    Idle --> FileChanged: External File Change
    FileChanged --> ReloadData: File Watcher Event
    ReloadData --> UpdateCache: Refresh Cache
    UpdateCache --> Idle: Cache Updated
    
    Idle --> PersistTick: Persistence Timer
    PersistTick --> CheckDirty: Dirty Check
    CheckDirty --> SaveData: Data Changed
    SaveData --> ClearDirty: Reset Flag
    ClearDirty --> Idle: Save Complete
    CheckDirty --> Idle: No Changes
```

This architecture documentation provides a comprehensive view of the system's design patterns, component interactions, and extension points. It serves as a guide for developers to understand the codebase structure and make informed decisions when adding new features or modifications.