# VISSR Testing Requirements Guide

Mandatory tests, conditional tests, and testing best practices for VISSR PRs.

## Mandatory Tests

Every PR must pass:

### 1. Build Check
```bash
go build ./...
```
- Compile all packages successfully
- No missing imports or syntax errors

### 2. Vet Check
```bash
go vet ./...
```
- No obvious bugs or type errors
- No unused variables or imports
- Correct format strings

### 3. Unit Tests with Race Detector
```bash
go list ./... | grep -v 'paho-mqtt' | grep -v 'smoketest' | xargs go test -race -count=1 -timeout 5m
```
- All tests pass
- **No race detector warnings**
- Race detector is non-optional

## Conditional Tests (Run When Relevant)

### Smoke Test

**Run when:** PRs touching server startup, WebSocket, or serviceReg  
**Command:**
```bash
mkdir -p /var/tmp/vissv2
go test -v -tags smoke -timeout 120s ./server/vissv2server/smoketest/
```

**Verifies:** End-to-end WebSocket communication without external services

### VDM Loader Validation

**Run when:** PRs touching `vdmloader/` or `.graphql` files  
**Command:**
```bash
go run ./tools/vdminfo ./server/vissv2server/vdmloader/testdata/
```

**Verifies:** GraphQL SDL parsing, instance tag expansion, tree structure

### MQTT Tests

**Run when:** PRs touching `paho-mqtt` or `mqttMgr`  
**Command:**
```bash
docker compose -f docker-compose.test.yml up -d
go test -race -count=1 ./paho-mqtt/...
docker compose -f docker-compose.test.yml down
```

**Verifies:** MQTT integration with live Mosquitto broker

## Race Detector

All tests **must** pass `-race`. When a race is reported:

1. **Identify** the unsynchronized map/slice/variable
2. **Protect** reads AND writes with the same mutex
3. **Verify** the fix doesn't introduce deadlocks
4. Never use `go test -race` as optional

```bash
# If race detected, investigate with verbose output
go test -race -v -run TestName ./package/
```

## Testing Best Practices

### Use Fixtures, Not Live Services

```go
// GOOD: net.Pipe for isolated tests
client, server := net.Pipe()
go handleConnection(server)
// ... test client behavior

// BAD: Requires live external service
conn, err := net.Dial("tcp", "localhost:8300")  // What if port taken?
```

### Isolation with //go:build Tags

```go
//go:build smoke

package smoketest

// This test only runs with: go test -tags smoke
// Excluded from standard test suite: go test ./...
```

**Review checklist:**
- Integration tests use `//go:build` tags
- Smoke tests don't run in CI by default
- Standard `go test ./...` only runs unit tests

### Test-Only Configuration

```go
// GOOD: Exported vars allow test overrides
var HeartbeatInterval = 15 * time.Second

func TestFastHeartbeat(t *testing.T) {
    oldInterval := HeartbeatInterval
    HeartbeatInterval = 100 * time.Millisecond
    t.Cleanup(func() { HeartbeatInterval = oldInterval })
    
    // ... test code runs with fast heartbeat
}
```

**Review checklist:**
- Timeout/interval variables exported (not hardcoded)
- Tests override with small values for speed (< 100ms)
- `t.Cleanup()` resets after test

### All New Public Functions Have Tests

```go
// If PR adds this public function:
func NewService(addr string, path string) (*Service, error)

// There must be tests like:
func TestNewService_Valid(t *testing.T)
func TestNewService_InvalidAddr(t *testing.T)
func TestNewService_InvalidPath(t *testing.T)
```

### No Real External Dependencies

```go
// BAD: Requires real Redis
func TestCacheWrite(t *testing.T) {
    client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
    // ... test fails if Redis not running
}

// GOOD: Mock or fixture
func TestCacheWrite(t *testing.T) {
    cache := &MockCache{}
    // ... test runs anywhere
}
```

**Red flags:**
- Tests require real Redis, MQTT, or external process without build tag
- Tests hardcode localhost ports
- Integration tests in standard test suite (should use `//go:build`)

## Coverage Report

Generate coverage to identify gaps:

```bash
# Unit test coverage (excludes paho-mqtt, smoketest)
go list ./... | grep -v 'paho-mqtt' | grep -v 'smoketest' | xargs go test -cover ./

# Full coverage with HTML report
go test -coverprofile=coverage.out ./... && go tool cover -html=coverage.out
```

**Goal:** New public functions and critical paths should have test coverage.

## Common Test Issues to Flag

| Issue | Pattern | Fix |
|-------|---------|-----|
| **Missing validation** | No error checks in test | Add test cases for error paths |
| **Hardcoded ports** | `localhost:8300` in tests | Use `net.Pipe()` or `net.Listener` |
| **Race conditions** | Works locally but fails in CI | Run with `-race` flag |
| **Flaky tests** | Time-dependent assertions | Use channels or mocks, not `time.Sleep()` |
| **Incomplete cleanup** | Goroutines left running | Use `t.Cleanup()` or context cancellation |

For all test commands, see [REFERENCE.md](REFERENCE.md#quick-commands).
