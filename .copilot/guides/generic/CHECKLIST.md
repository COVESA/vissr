# VISSR PR Review Checklist

Checklist for reviewing pull requests to the VISSR repository.

## Architecture & Design
- [ ] Single responsibility per package
- [ ] Minimal public API surface (< 5 exported symbols per new package)
- [ ] Follows Registry pattern (`RegisterServiceTree`) if providing trees
- [ ] No code duplication (flag if 3rd tree source introduces same logic)
- [ ] SOLID principles respected
- [ ] `--vdm` / `--him` mutual exclusivity preserved
- [ ] Correct package structure (vdmloader, webdash, vissServiceMgr, etc.)

## Security
- [ ] No hardcoded secrets, tokens, or credentials
- [ ] Secrets accessed via env vars only
- [ ] TLS not weakened (no InsecureSkipVerify, minVersion ≥ TLS 1.2)
- [ ] Auth tokens forwarded, not validated server-side
- [ ] No sensitive data in logs
- [ ] No `.env`, `.key`, or `.pem` files committed

## Concurrency
- [ ] Shared maps/slices protected by mutex
- [ ] No I/O inside mutex lock
- [ ] All goroutines have exit conditions
- [ ] Passes `go test -race`
- [ ] No goroutine leaks (confirmed exit paths)

## Testing
- [ ] All tests pass (`go test -race`)
- [ ] New public functions have tests
- [ ] Integration tests use `//go:build` tags
- [ ] Tests use net.Pipe/fixtures, not live external services
- [ ] Test overrides of timeout/interval vars use t.Cleanup
- [ ] No test requires real Redis, MQTT, or external process without build tag

## Code Quality
- [ ] Input validation for all user-provided data (JSON, GraphQL, flags)
- [ ] Atomic I/O frames (append newline before single Write)
- [ ] Proper error handling — no silent drops of errors
- [ ] Resource cleanup with `defer` on every conn/file open
- [ ] `utils.Info/Error` used (not `fmt.Println`)
- [ ] No `//nolint` directives suppressing real safety issues

## VDM / GraphQL (when applicable)
- [ ] All types have `@vspec(element:, fqn:)` directives
- [ ] `@range(min:, max:)` values are numerically valid (min ≤ max)
- [ ] Instance tag types follow `*_InstanceTag` convention
- [ ] Testdata updated for new directive patterns
- [ ] `go run ./tools/vdminfo ./testdata/` output reviewed

## Dependencies (when go.mod changes)
- [ ] New dependencies have compatible licenses (MIT, Apache-2.0, BSD)
- [ ] Versions pinned (not floating tags)
- [ ] Indirect deps reviewed for supply chain risk
- [ ] No unnecessary new dependencies (stdlib alternative exists?)

## PR & Repo Hygiene
- [ ] PR has single concern (not mixing features, fixes, refactors)
- [ ] PR size reasonable (< 500 lines, excluding testdata/generated)
- [ ] No dead code, commented-out blocks, or orphaned files
- [ ] No stale dependencies or unused imports
- [ ] Branch name follows convention (`feat/`, `fix/`, `chore/`)
- [ ] Commit messages follow `type(scope): description`

## Documentation
- [ ] Package comment on all new packages
- [ ] Doc comment on all exported functions
- [ ] Commit messages follow `type(scope): description` convention

## Error Handling
- [ ] Optional components (webdash, serviceReg TLS) fail gracefully
- [ ] HeartbeatTimeout < HeartbeatInterval
- [ ] Errors include human-readable description + machine reason code
- [ ] Service disconnections handled (invocations marked FAILED)
- [ ] Server continues on optional component failure

## Summary

**Approve if:** All items checked
**Request changes if:** Any items flagged
**Reject if:** Security, test failure, or architectural violation
