# VISSR Reference & Examples

Quick reference for commands, code patterns, useful examples, and resources.

## Quick Commands

### Verification
```bash
# Quick check
go build ./...
go vet ./...

# All unit tests (excludes paho-mqtt + smoketest)
go list ./... | grep -v 'paho-mqtt' | grep -v 'smoketest' | xargs go test -race -count=1 -timeout 5m

# Specific package
go test -race ./server/vissv2server/...
```

### Smoke & Integration Tests
```bash
# End-to-end WebSocket test
mkdir -p /var/tmp/vissv2
go test -v -tags smoke -timeout 120s ./server/vissv2server/smoketest/

# VDM loader validation
go run ./tools/vdminfo ./server/vissv2server/vdmloader/testdata/
```

### Local Development
```bash
# MQTT broker (local testing)
docker compose -f docker-compose.test.yml up -d
docker compose -f docker-compose.test.yml ps

# Server startup
go build -o /tmp/vissv2server ./server/vissv2server/
/tmp/vissv2server --vdm ./server/vissv2server/vdmloader/testdata/

# Connect WebSocket client
curl http://localhost:8200
```

### Scanning & Analysis
```bash
# Code coverage
go test -cover ./...

# Scan for common issues
go vet ./...
staticcheck ./... 2>/dev/null || true

# Scan for secrets in diff
git diff | grep -iE 'secret|apikey|password|token\s*=|insecureskipverify'
```

## Code Patterns

### Registry Pattern (All Tree Sources)
```go
// Providing a tree? Register it.
import "github.com/covesa/vissr/utils"

func (m *VDMManager) Start() error {
    trees, err := vdmloader.LoadDir(vdmPath)
    if err != nil {
        return err
    }
    
    for name, root := range trees {
        domain := "Vehicle.VDM.Service"
        version := "1.0"
        utils.RegisterServiceTree(name, domain, version, root)
        
        // On shutdown:
        defer utils.DeregisterServiceTree(name)
    }
    
    return nil
}
```

### Mutex Protection (Shared State)

See [ARCHITECTURE.md](ARCHITECTURE.md#concurrency--synchronization) for full concurrency patterns.

### Goroutine Lifecycle

See [ARCHITECTURE.md](ARCHITECTURE.md#goroutine-lifecycle) for goroutine exit patterns.

For input validation, atomic I/O, and error handling patterns, see [ARCHITECTURE.md](ARCHITECTURE.md#common-issues).

### Environment Variable Secrets
```go
// GOOD: Load from env var
secret := os.Getenv("VISSR_AT_SECRET")
if secret == "" {
    utils.Warning.Printf("VISSR_AT_SECRET not set; using ephemeral key")
    secret = generateEphemeralKey()
}

// BAD: Hardcoded
const secret = "super-secret-12345"
```

### Logging Patterns
```go
// GOOD: Structured, contextual
utils.Info.Printf("Service registered: path=%s domain=%s", path, domain)
utils.Error.Printf("Connection failed: %v", err)

// BAD: Too verbose or leaks secrets
utils.Info.Printf("Connecting with token: %s", token)  // LEAK!
fmt.Println("Service started")  // Should use utils.Info
```

## Commit Message Examples

### Feature
```
feat(vdmloader): add support for @unit directive parsing

Adds parsing of @unit(value: "km/h") annotations on leaf nodes,
mapping to Node_t unit metadata. Integrates with treeutils.NewSignalNode.

Closes #42
```

### Bug Fix
```
fix(serviceReg): validate signature before building tree

Service registration now validates that signature maps are non-empty
before constructing procedure trees. Prevents silent creation of
unreachable services with no input/output parameters.

Closes #84
```

### Test
```
test(vdmloader): add instance tag expansion test cases

Adds comprehensive tests for multi-dimensional instance tag expansion
(Seat: Row × Side) and single-dimension cases (Door: Row).
Validates path generation and leaf node cloning.
```

### Refactor
```
refactor(common): extract tree building validation

Extracts common validation logic from serviceReg and vdmloader
into a shared utils.ValidateSignature() helper. No behavior change.
```

## Resource Links

- **Specification**: `spec/VISSv4.0_VDM.html`, `spec/VISSv3.3_Service_Quickstart.md`
- **Service Protocol**: Top of `server/vissv2server/vissServiceMgr/serviceReg.go`
- **Code Examples**: Merged PRs #157–#159 (service integration patterns)
- **Testing Guide**: `TESTING.md`
- **MQTT Setup**: `docker-compose.test.yml`
- **CI Pipeline**: `.github/workflows/test.yml`
- **HIM Forest**: `server/vissv2server/viss.him` + `utils/treeutils.go`

## Common Questions

**Q: When should I use mutex vs channel?**  
A: Use **mutex** for shared state (maps, slices). Use **channel** for signaling/event distribution.

**Q: How do I test goroutines without live services?**  
A: Use `net.Pipe()` for bidirectional connection tests, or mock interfaces and channels for behavioral tests.

**Q: Can I use `fmt.Println` in my code?**  
A: No. Use `utils.Info.Printf()` for normal events or `utils.Error.Printf()` for failures.

**Q: What's the difference between `--vdm` and `--him`?**  
A: `--vdm` loads GraphQL SDL files (VISSv4.0). `--him` uses static HIM forest file. Mutually exclusive.

**Q: Do I need to worry about tree duplication?**  
A: Not until we have a 3rd tree source (beyond serviceReg + vdmloader). Current status: OK.
