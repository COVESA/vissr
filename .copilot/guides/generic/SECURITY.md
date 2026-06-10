# VISSR Security & Secrets Management Guide

Security best practices, secret management, and authorization patterns for VISSR PRs.

## Environment Variables

VISSR requires these env vars in production. Flag PRs that mishandle them:

| Variable | Purpose | Risk if Missing |
|----------|---------|----------------|
| `VISSR_AT_SECRET` | Access token signing key | Ephemeral key (restarts invalidate tokens) |
| `VISSR_ECF_SECRET` | ECF consent HMAC key | HMAC verification disabled |
| `VISSR_ECF_CERT_PATH` / `VISSR_ECF_KEY_PATH` | TLS cert for ECF WebSocket | Plaintext connections accepted |
| `VISSR_ECF_ALLOWED_ORIGIN` | CORS origin allowlist | Any origin accepted |

**Review checklist:**
- ✅ No hardcoded secrets, tokens, or passwords in code
- ✅ Secrets accessed only via `os.Getenv()` or config
- ✅ Warning logs emitted when env vars are unset
- ✅ Test fixtures use dummy/ephemeral secrets only
- ✅ Scan diff: `git diff | grep -iE 'secret|apikey|password|token\s*='`
- ❌ Red flag: any `.env`, `.key`, or `.pem` file committed

## TLS & Transport Security

```go
// ✅ GOOD: Strict TLS config
cfg := &tls.Config{
    Certificates: []tls.Certificate{cert},
    MinVersion:   tls.VersionTLS12,
    // InsecureSkipVerify omitted (defaults false)
}

// ❌ BAD: Disabled verification
cfg := &tls.Config{InsecureSkipVerify: true}
```

**Review checklist:**
- ✅ `StartServiceRegServerTLS` used in production paths (not plain TCP)
- ✅ Minimum TLS version is 1.2 (`tls.VersionTLS12`)
- ✅ Certificate validation not disabled (`InsecureSkipVerify: false`)
- ✅ Cipher suite restrictions appropriate for use case

## Authorization in Service Invocations

```go
// ✅ GOOD: Auth token forwarded, service decides
msg := map[string]interface{}{
    "action":        "invoke",
    "sessionId":     serviceId,
    "input":         input,
    "authorization": authToken, // ← forwarded, not validated server-side
}
```

**Review checklist:**
- ✅ `authorization` token forwarded from client to service
- ✅ Service can reject unauthorized invocations
- ✅ Error messages don't expose internal paths or system state
- ✅ JWT/token contents not logged at info level

## Common Security Violations to Flag

### Hardcoded Secrets
```go
// ❌ NEVER DO THIS
const SecretKey = "my-super-secret-key-12345"
password := "admin123"
apiToken := "ghp_xxxxxxxxxxxxxxxxxxxx"
```

**Fix:** Move to environment variables:
```go
secretKey := os.Getenv("VISSR_AT_SECRET")
if secretKey == "" {
    utils.Warning.Printf("VISSR_AT_SECRET not set; using ephemeral key")
    // generate ephemeral key for tests only
}
```

### Disabled TLS Verification
```go
// ❌ NEVER DO THIS
cfg := &tls.Config{InsecureSkipVerify: true}
```

**Impact:** Man-in-the-middle attacks possible. Always validate certificates.

### Logging Secrets
```go
// ❌ NEVER DO THIS
token := os.Getenv("AUTH_TOKEN")
utils.Info.Printf("Connecting with token: %s", token)  // ← LEAKED

// ✅ GOOD: Log without sensitive data
utils.Info.Printf("Connecting to service")  // ← Safe
```

**Rule:** Never log tokens, keys, passwords, or full request bodies at any log level.

### Exposed Error Details
```go
// ❌ BAD: Exposes internal paths
return fmt.Errorf("config file not found at /etc/vissr/secret.key")

// ✅ GOOD: Generic error
return fmt.Errorf("configuration error")
```

**Impact:** Don't leak filesystem paths, internal IPs, or system state in error messages to clients.

## Service Connection Security

For heartbeat/health protocol patterns and timeout configuration, see [ARCHITECTURE.md](ARCHITECTURE.md#timeout--interval-values).

**Key security checks:**
- Missed heartbeat pong closes connection (triggers deregister)
- Connection loss detected in finite time (no indefinite waits)
- All ONGOING invocations marked FAILED on disconnect
- Service path deregistered from HIM forest on disconnect

## Path Traversal

File-loading flags (`--vdm`, `--him`) accept user-supplied paths.

```go
// ❌ BAD: No validation — can escape intended directory
root := vdmloader.LoadDir(userSuppliedPath)

// ✅ GOOD: Resolve and verify path stays within allowed base
absPath, _ := filepath.Abs(userSuppliedPath)
if !strings.HasPrefix(absPath, allowedBase) {
    return fmt.Errorf("path %q escapes allowed directory", userSuppliedPath)
}
```

**Review checklist:**
- ✅ File paths cleaned with `filepath.Clean` / `filepath.Abs`
- ✅ Resolved path checked against allowed base directory
- ✅ Symlinks resolved before check (`filepath.EvalSymlinks`)
- ❌ Red flag: user-supplied path used directly in `os.Open` or `os.ReadDir`

## Input Size Limits

Unbounded reads enable denial-of-service.

```go
// ❌ BAD: Unlimited read
body, _ := io.ReadAll(r.Body)

// ✅ GOOD: Bounded read
body, _ := io.ReadAll(io.LimitReader(r.Body, maxBodySize))
```

**Review checklist:**
- ✅ HTTP/WebSocket payloads bounded (`io.LimitReader`, `MaxBytesReader`)
- ✅ GraphQL SDL files checked for size before parsing
- ✅ Recursive structures (instance tag expansion) bounded by depth/count limit
- ❌ Red flag: `io.ReadAll` without size cap on network input

## Secrets Scanning

Before committing, scan for secrets:

```bash
# Scan diff for common patterns
git diff | grep -iE 'secret|apikey|password|token\s*=|insecureskipverify'

# Scan staged files
git diff --cached | grep -iE 'secret|apikey|password|token\s*='
```

**Red flags to block:**
- `SECRET = "..."`
- `PASSWORD: "..."`
- `API_KEY = "..."`
- `InsecureSkipVerify: true`
- `.env` files
- `.key` or `.pem` files
- Private key contents

## Test Fixtures & Dummy Secrets

For tests, use ephemeral/dummy values only:

```go
// ✅ GOOD: Test-only dummy secret
const testSecret = "dummy-test-key-do-not-use"

// ✅ GOOD: Ephemeral key for each test
func TestWithAuth(t *testing.T) {
    secret := generateEphemeralKey()  // New key per test
    defer func() { secret = nil }()   // Cleanup
    // ... test code
}
```

Never commit real secrets or use production credentials in tests.
