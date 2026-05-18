# Testing

This document covers vissr's unit-test and fuzz-test infrastructure as
of the regression-test PR landed alongside the stability/security
batches (commit `ef639f0`, PRs #119, #120, #121, and the broader test-
debt fill-in shipped here).

## Running the test suite

```bash
go test \
    ./utils \
    ./server/vissv2server \
    ./server/vissv2server/atServer \
    ./server/vissv2server/serviceMgr \
    ./server/vissv2server/mqttMgr \
    ./server/vissv2server/wsMgr \
    ./server/vissv2server/wsMgrFT \
    ./server/vissv2server/udsMgr \
    ./server/vissv2server/grpcMgr \
    ./server/agt_server \
    ./client/client-1.0/filetransfer_client \
    ./feeder/feeder-rl \
    ./feeder/feeder-template/feederv4
```

`./...` from the repo root currently fails because of a long-standing
mixed-package layout in `server/vissv2server/grpcMgr/testprocess/`
unrelated to this PR. Use the explicit list above.

## Race-detector tests

Five tests target the mutex fixes shipped across the batches
(`WsClientIndexMu`, `sessionListMu`, `jtiCacheMu` in atServer and
agt_server, `udsClientIndexMu`, `grpcStateMu`). They're designed to
fail under `go test -race` if the mutex is removed or weakened. Run:

```bash
go test -race ./server/vissv2server/atServer \
              ./server/vissv2server/wsMgrFT \
              ./server/vissv2server/udsMgr \
              ./server/vissv2server/grpcMgr \
              ./server/agt_server \
              ./utils
```

## Fuzz tests

Seven native Go fuzz harnesses cover parsers that consume attacker-
controlled input. Run an individual fuzzer for ~30 s:

```bash
go test -run='^$' -fuzz=FuzzValidateTransferName -fuzztime=30s \
    ./server/vissv2server/wsMgrFT
```

All seven (suitable for a CI nightly job):

```bash
fuzzers=(
    'FuzzValidateTransferName   ./server/vissv2server/wsMgrFT'
    'FuzzSafeServerFilename     ./client/client-1.0/filetransfer_client'
    'FuzzExtractKeyValue        ./server/vissv2server/atServer'
    'FuzzProcessHistoryCtrl     ./server/vissv2server/serviceMgr'
    'FuzzProcessHistoryGet      ./server/vissv2server/serviceMgr'
    'FuzzMapRequest             ./utils'
    'FuzzJsonSchemaValidate     ./utils'
    'FuzzGetFileDescriptorData  ./server/vissv2server'
)
for entry in "${fuzzers[@]}"; do
    set -- $entry
    echo "==== $1 ===="
    go test -run='^$' -fuzz="$1" -fuzztime=30s "$2" || exit 1
done
```

Failures land as files under `testdata/fuzz/<FuzzerName>/`. Commit any
that surface real bugs as part of the fix; the file becomes a seed
corpus entry for that fuzzer.

## Coverage by area

### Regression coverage of the stability/security batches

| Fix area                                                       | Test file                                                                                  |
|----------------------------------------------------------------|--------------------------------------------------------------------------------------------|
| `validateTransferName` path traversal (wsMgrFT)                 | `server/vissv2server/wsMgrFT/wsMgrFT_test.go`                                              |
| `getDataSessionIndex` claim step + `sessionListMu` race         | `server/vissv2server/wsMgrFT/wsMgrFT_test.go`                                              |
| `safeServerFilename` client-side path traversal                 | `client/client-1.0/filetransfer_client/filetransfer_client_test.go`                        |
| `extractKeyValue` safe type-assert (atServer)                   | `server/vissv2server/atServer/atServer_fixes_test.go`                                      |
| `jtiCache` mutex (atServer, agt_server)                         | `server/vissv2server/atServer/atServer_fixes_test.go`, `server/agt_server/agt_server_test.go` |
| `processHistoryCtrl` panic + index + bufSize cap                | `server/vissv2server/serviceMgr/serviceMgr_test.go`                                        |
| `processHistoryGet` panic + index                               | `server/vissv2server/serviceMgr/serviceMgr_test.go`                                        |
| `getBrokerSocket` env var / fallback / TLS (mqttMgr)             | `server/vissv2server/mqttMgr/mqttMgr_test.go`                                              |
| `JsonSchemaValidate` nil-deref guard (utils)                    | `utils/common_fixes_test.go`                                                               |
| `WsClientIndexMu` race + `MaxBytesReader` POST body limit         | `utils/managerhandlers_test.go`                                                            |
| `udsClientIndexMu` race + -1 protection                         | `server/vissv2server/udsMgr/udsMgr_test.go`                                                |
| `grpcStateMu` race                                              | `server/vissv2server/grpcMgr/grpcMgr_test.go`                                              |
| `getFileDescriptorData` type-assert (vissv2server)              | `server/vissv2server/vissv2server_test.go`                                                 |
| `TouchFile` mode 0644 (feeder-rl)                               | `feeder/feeder-rl/feeder-rl_test.go`                                                       |
| `onNotificationList` lookup contract (feederv4)                 | `feeder/feeder-template/feederv4/feederv4_test.go`                                         |
| AGT POST body limit + 404 path                                  | `server/agt_server/agt_server_test.go`                                                     |
| AT POST body limit                                              | `server/vissv2server/atServer/atServer_fixes_test.go`                                      |
| HTTP VISS POST body limit                                       | `utils/managerhandlers_test.go`                                                            |
| `activateInterval`/`deactivateInterval` ticker leak             | `server/vissv2server/serviceMgr/serviceMgr_test.go`                                        |

### Broader coverage (independent of recent fixes)

| Area                                                           | Test file                                                            |
|----------------------------------------------------------------|----------------------------------------------------------------------|
| `utils.IsNumber`, `IsBoolean` predicates                        | `utils/common_broader_test.go`                                       |
| `utils.PathToUrl` ⇄ `UrlToPath` round-trip                      | `utils/common_broader_test.go`                                       |
| `utils.GetMaxValidation`                                        | `utils/common_broader_test.go`                                       |
| `utils.ExtractRootName`                                         | `utils/common_broader_test.go`                                       |
| `utils.GenerateHmac` / `VerifyTokenSignature` round-trip + tamper | `utils/common_broader_test.go`                                       |
| `utils.AddKeyValue` no-Marshal value injection                  | `utils/common_broader_test.go`                                       |
| `utils.MapRequest` fuzz                                         | `utils/common_fixes_test.go`                                         |
| `wsMgr.getValueForKey` JSON value extractor                     | `server/vissv2server/wsMgr/wsMgr_test.go`                            |
| `wsMgr.getSortedPaths` deterministic ordering                   | `server/vissv2server/wsMgr/wsMgr_test.go`                            |
| `wsMgr` data-compression cache (insert / lookup / reset)         | `server/vissv2server/wsMgr/wsMgr_test.go`                            |

## Still not covered (open follow-ups)

The fixes below would all be testable but require either non-trivial
refactoring of production code (to expose seams) or integration-style
harnesses that are out of scope for this PR:

- `mqttMgr::createSubscribeClient` / `publishMessage` `os.Exit` removal
  (PR #121). The fix is "absence of process exit"; to test it, the
  helpers need to be refactored to accept a token-getter / client
  factory.
- `vissv2server.go::issueServiceRequest` `dt[:5]` panic fix (PR #121).
  The check is inline in the long message-dispatch goroutine; tests
  here are integration-style and live in `runtest.sh`.
- `vissv2server.go::initiateFileTransfer` `[utils.UIDLEN]byte` panic
  fix (PR #121). Same shape as above; deeply embedded in the channel
  flow.

## Broader vissr test debt — areas with no automated coverage

These are not covered by this PR's tests. Listed roughly in order of
attack surface and review value, for future PRs:

| Subsystem                                  | Surface          |
|--------------------------------------------|------------------|
| `server/vissv2server/vissv2server.go` core | ~700 LOC, only `getFileDescriptorData` is tested. Most of the message-dispatch loop, routing, and helper functions have no unit tests. |
| `server/vissv2server/wsMgr` beyond pure helpers | The WS upgrade path, message handling goroutine, and dispatch logic. Currently covered only by `runtest.sh` integration. |
| `server/vissv2server/httpMgr` top-level    | HTTP transport init and dispatch. No unit tests. |
| `server/vissv2server/grpcMgr` streaming    | The `SubscribeRequest` streaming RPC. Only the index-allocator helpers are tested. |
| All client subdirs (compress, csv, mqtt)   | Only `filetransfer_client/safeServerFilename` is tested. The transports and request/response loops are untested. |
| `feeder/feeder-template/feederv4` main loop | Only the `onNotificationList` lookup. The dispatch/update/redis paths are untested. |
| `feeder/feeder-evic`                       | No tests at all. |
| `paho-mqtt` integration                    | Existing `paho_go_test.go` only; no expansion. |
| `tools/DomainConversionTool`               | No tests at all. |

These represent multi-PR work; addressing them comprehensively is a
multi-session effort that should be planned as a sequence of focused
PRs rather than one large drop.

## Note on `.gitignore`

The repo's `.gitignore` has unanchored patterns `feeder` and
`agt_server` (lines 30, 53) that silently swallow any path with those
names — including three of the test files in this PR. Three files
required `git add -f`. The patterns should be anchored
(`/feeder/feeder-template/feederv4/feeder` for the built binary, etc.)
in a separate cleanup PR.
