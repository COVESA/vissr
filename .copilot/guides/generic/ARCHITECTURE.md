# VISSR Architecture & Design Guide

Architectural patterns, design principles, and code organization standards for VISSR PRs.

## Service Manager Registry Pattern

All tree sources (static, dynamic, VDM) register via the unified registry:

```go
utils.RegisterServiceTree(rootName, domain, version, root)
utils.DeregisterServiceTree(rootName)
```

**Review checklist:**
- ✅ New components providing trees use `RegisterServiceTree()`
- ✅ `rootName` is unique across all sources
- ✅ Service trees have `.Service` domain suffix
- ✅ Cleanup calls `DeregisterServiceTree()` on shutdown
- ❌ Red flag: same `rootName` registered from two sources

## HIM Forest vs Dynamic Registration

| Use Case | Mechanism | File |
|----------|-----------|------|
| Static VSS tree | `viss.him` file | `server/vissv2server/viss.him` |
| VDM SDL files (v4.0) | `--vdm` flag | `vdmloader.LoadDir()` |
| Dynamic service procedures | TCP port 8300 | `vissServiceMgr/serviceReg.go` |

**Key constraint:** `--vdm` and `--him` are mutually exclusive — validate early!

## Package Organization

```
server/vissv2server/
├── vdmloader/         → VDM-specific parsing logic
├── webdash/           → Web UI/dashboard components
├── vissServiceMgr/    → Service registration protocol
├── wsMgr/             → WebSocket transport
├── httpMgr/           → HTTP transport
└── smoketest/         → Integration tests (//go:build smoke)
```

**Review checklist:**
- ✅ Single responsibility per package
- ✅ Public APIs minimal (< 5 exported symbols)
- ✅ Integration tests use `//go:build` tags
- ✅ Implementation details private (lowercase names)

## Interface-Driven Design

```go
// ✅ GOOD: Focused, stateless function
func Start(addr string) error

// ✅ GOOD: Fluent builder
svc, err := NewService(addr, path).
    WithInput("Param", "uint32").
    WithOutput("Result", "string").
    OnInvoke(handler).
    Register()

// ❌ BAD: Large, god-like interface
func NewManager(conf Config, logger Logger, registry Registry, ...) *Manager
```

## SOLID Principles

| Principle | Application | Red Flags |
|-----------|-------------|-----------|
| **SRP** | Each package has one reason to change | Multiple concerns mixed |
| **OCP** | Extensible for new tree sources | Hardcoded sources, no registry |
| **LSP** | Subtypes can replace supertypes | Type assertions on interfaces |
| **ISP** | Minimal, focused public APIs | Large interfaces with many methods |
| **DIP** | Depends on abstractions, not concretions | Direct dependencies on implementations |

## Common Issues

### Issue: Missing Input Validation

```go
// ❌ BAD
sig, _ := msg["signature"].(map[string]interface{})
root := buildTree(sig)  // Could fail silently

// ✅ GOOD
sig, ok := msg["signature"].(map[string]interface{})
if !ok || len(sig) == 0 {
    return fmt.Errorf("signature must be non-empty map")
}
```

**Flag:** Any user-provided data (JSON, GraphQL, files) processed without validation.

### Issue: Non-Atomic I/O Operations

```go
// ❌ BAD: Multiple writes (partial write risk)
w.Write(jsonBytes)
w.WriteByte('\n')
w.Flush()

// ✅ GOOD: Atomic frame
frame := append(jsonBytes, '\n')
w.Write(frame)
w.Flush()
```

**Flag:** Network I/O in protocol handlers, especially line-delimited JSON.

### Issue: Missing Mutual Exclusivity Checks

```go
// ❌ BAD: Could specify both flags
if *vdmDir != "" { vdmloader.LoadDir(*vdmDir) }
if *himFile != "" { loadHIM(*himFile) }

// ✅ GOOD: Validates exclusivity
if *vdmDir != "" && *himFile != "" {
    return fmt.Errorf("--vdm and --him are mutually exclusive")
}
```

**Flag:** Multiple configuration options that could conflict.

### Issue: Tree Building Code Duplication (Preventive)

Currently tree building is in 2 places: `serviceReg.go` and `vdmloader.go`.  
**Do NOT flag** unless a **third** tree source appears — then extract shared logic.

## Configuration Management

### Flag Validation

```go
// ✅ GOOD: Explicit mutual exclusivity at startup
if *vdmDir != "" && *himFile != "" {
    utils.Error.Printf("fatal: --vdm and --him are mutually exclusive")
    os.Exit(1)
}
```

**Review checklist:**
- ✅ Mutually exclusive flags validated early
- ✅ Required flags checked (not just defaulted to empty)
- ✅ Flag names follow conventions (`--vdm`, `--web-addr`)
- ✅ Help strings explain mutual exclusivity

### Timeout & Interval Values

```go
// ✅ GOOD: Exported, overridable in tests
var HeartbeatInterval = 15 * time.Second
var HeartbeatTimeout  =  5 * time.Second
```

**Review checklist:**
- ✅ Timeout variables exported (not hardcoded)
- ✅ Default values reasonable for production
- ✅ Documented with comments explaining purpose

## Error Handling & Recovery

### Graceful Degradation

```go
// ✅ GOOD: Optional component failure is non-fatal
if *webAddr != "" {
    if err := webdash.Start(*webAddr); err != nil {
        utils.Error.Printf("webdash: failed to start: %v", err)
        // Do NOT os.Exit(1) — server continues
    }
}

// ❌ BAD: Optional component kills server
if err := webdash.Start(*webAddr); err != nil {
    log.Fatalf("webdash: %v", err)
}
```

**Mandatory components** (fatal on failure): transport managers, atServer  
**Optional components** (log + continue): webdash, serviceReg TLS, vdminfo CLI

### Error Response Format

VISS responses use standard error objects:

```json
{
  "error": {
    "number": "404",
    "reason": "unavailable_data",
    "description": "The requested data was not found."
  }
}
```

**Review checklist:**
- ✅ Errors use `number` + `reason` + `description` format
- ✅ `reason` is machine-readable (snake_case)
- ✅ `description` is human-readable
- ✅ Internal paths/types not leaked

## Logging Standards

| Level | Usage |
|-------|-------|
| `utils.Error` | Unexpected failures requiring attention |
| `utils.Info` | Normal lifecycle events |
| `utils.Warning` | Degraded but functional |

**Review checklist:**
- ✅ `utils.Error` not used for expected cases
- ✅ `utils.Info` used for service lifecycle
- ✅ No sensitive data (tokens, secrets) logged
- ✅ Log messages include relevant context
- ❌ Red flag: `fmt.Println` used instead of `utils.Info/Error`

## VDM / GraphQL SDL

### Directive Correctness

**Review checklist:**
- ✅ `@vspec(element: BRANCH|SENSOR|ACTUATOR|ATTRIBUTE)` on all types/fields
- ✅ `@vspec(fqn: "Dot.Separated.Path")` matches expected signal path
- ✅ `@range(min:, max:)` values numerically valid (min ≤ max)
- ✅ `@unit(value: ...)` uses correct VSS unit strings
- ✅ `@defaultValue(value: ...)` valid for field type

### Instance Tag Expansion

**Review checklist:**
- ✅ Types named `*_InstanceTag` have `@instanceTag` directive
- ✅ Each dimension is an enum with `@vspec(originalName: ...)` on values
- ✅ Expanded paths are unique (no collisions)
- ✅ Deep clone of signal subtree correct (test: leaf nodes × instance count)

## DRY (Don't Repeat Yourself)

**Pattern to watch:**
- Tree building logic in multiple packages → extract
- Configuration parsing in multiple places → centralize
- Error handling patterns → use helpers

## Concurrency & Synchronization

### Mutex vs Channel

Use **mutex** for shared state (maps, slices). Use **channels** for event signaling/fan-out.

```go
// Mutex: protect shared state
var regMu sync.Mutex
var registrations = map[string]*serviceConn{}

regMu.Lock()
registrations[path] = sc
regMu.Unlock()

// Channel: communicate between goroutines
backendChans []chan map[string]interface{}
```

**Review checklist:**
- Use `sync.Mutex` for maps/slices shared across goroutines
- Use channels for event fan-out (e.g., `backendChans`)
- Lock held for minimum duration (no I/O inside lock)
- Red flag: `map` accessed without mutex from concurrent goroutines
- Red flag: mutex held across network calls

### Goroutine Lifecycle

Every goroutine must have a known exit condition:

```go
// GOOD: Exits when connection closes
func startHeartbeat(sc *serviceConn) {
    for {
        time.Sleep(HeartbeatInterval)
        if err := sc.sendJSON(ping); err != nil {
            return  // connection gone — goroutine exits
        }
    }
}

// GOOD: Respects context cancellation
func (m *Manager) run(ctx context.Context) {
    for {
        select {
        case <-ctx.Done():
            return
        case msg := <-m.events:
            m.process(msg)
        }
    }
}
```

**Review checklist:**
- Every goroutine has a known exit condition
- Long-running goroutines respect `context.Context` or `done` chan
- Red flag: `go func() { for { ... } }()` with no exit
