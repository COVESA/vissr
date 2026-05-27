# Testing

This document describes vissr's unit-test and fuzz-test infrastructure
as of the broad-coverage push landed alongside the stability/security
batches (commit `ef639f0` plus PRs #119, #120, #121, #122 and the
present comprehensive-coverage PR).

## Running the test suite

```bash
go test ./...
```
runs the entire test suite.

The paths to the different unit tests can be explicitly provided, enabling test of a reduced set of unit tests.
Shown below are the paths to the complete set of unit tests as of 27-05-2026.
```
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
    ./client/client-1.0/compress_client \
    ./client/client-1.0/csv_client \
    ./client/client-1.0/filetransfer_client \
    ./client/client-1.0/grpc_client \
    ./client/client-1.0/testClient \
    ./feeder/feeder-rl \
    ./feeder/feeder-template/feederv4 \
    ./feeder/feeder-evic \
    ./feeder/feeder-evic/evicSim
```

## Race-detector tests

Tests for the mutex fixes shipped across the security batches
(`WsClientIndexMu`, `sessionListMu`, `jtiCacheMu` in atServer and
agt_server, `udsClientIndexMu`, `grpcStateMu`) are designed to fail
under `go test -race` if the mutex protecting their shared state is
removed. Run:

```bash
go test -race ./server/vissv2server/atServer \
              ./server/vissv2server/wsMgrFT \
              ./server/vissv2server/udsMgr \
              ./server/vissv2server/grpcMgr \
              ./server/agt_server \
              ./utils
```

## Fuzz tests

Native Go fuzz harnesses cover parsers that consume attacker-
controlled input. Run an individual fuzzer for ~30s:

```bash
go test -run='^$' -fuzz=FuzzValidateTransferName -fuzztime=30s \
    ./server/vissv2server/wsMgrFT
```

Run all of them in turn (CI nightly):

```bash
fuzzers=(
    'FuzzValidateTransferName   ./server/vissv2server/wsMgrFT'
    'FuzzSafeServerFilename     ./client/client-1.0/filetransfer_client'
    'FuzzEncodeDlRequest        ./client/client-1.0/filetransfer_client'
    'FuzzExtractKeyValue        ./server/vissv2server/atServer'
    'FuzzProcessHistoryCtrl     ./server/vissv2server/serviceMgr'
    'FuzzProcessHistoryGet      ./server/vissv2server/serviceMgr'
    'FuzzMapRequest             ./utils'
    'FuzzJsonSchemaValidate     ./utils'
    'FuzzGetFileDescriptorData  ./server/vissv2server'
    'FuzzGetRangeBoundaries     ./server/vissv2server'
    'FuzzGetValueForKey         ./server/vissv2server/wsMgr'
    'FuzzCompressTs             ./server/vissv2server/wsMgr'
)
for entry in "${fuzzers[@]}"; do
    set -- $entry
    echo "==== $1 ===="
    go test -run='^$' -fuzz="$1" -fuzztime=30s "$2" || exit 1
done
```

Failures land under `testdata/fuzz/<FuzzerName>/`. Commit any that
surface real bugs as part of the fix; the file then becomes a seed
corpus entry.

## Coverage by area

### Regression coverage of the stability/security batches (PR #122)

| Fix area                                                       | Test file                                                                                  |
|----------------------------------------------------------------|--------------------------------------------------------------------------------------------|
| `validateTransferName` path traversal (wsMgrFT)                 | `server/vissv2server/wsMgrFT/wsMgrFT_test.go`                                              |
| `getDataSessionIndex` claim step + `sessionListMu` race         | `server/vissv2server/wsMgrFT/wsMgrFT_test.go`                                              |
| `safeServerFilename` client-side path traversal                 | `client/client-1.0/filetransfer_client/filetransfer_client_test.go`                        |
| `extractKeyValue` safe type-assert (atServer)                   | `server/vissv2server/atServer/atServer_fixes_test.go`                                      |
| `jtiCache` mutex (atServer, agt_server)                         | `server/vissv2server/atServer/atServer_fixes_test.go`, `server/agt_server/agt_server_test.go` |
| AGT / AT / HTTP POST body limits (`MaxBytesReader`)              | `server/agt_server/agt_server_test.go`, `server/vissv2server/atServer/atServer_fixes_test.go`, `utils/managerhandlers_test.go` |
| `processHistoryCtrl` panic + index + bufSize cap                | `server/vissv2server/serviceMgr/serviceMgr_test.go`                                        |
| `processHistoryGet` panic + index                               | `server/vissv2server/serviceMgr/serviceMgr_test.go`                                        |
| `activate/deactivateInterval` ticker-leak                       | `server/vissv2server/serviceMgr/serviceMgr_test.go`                                        |
| `getBrokerSocket` env-var / fallback / TLS (mqttMgr)             | `server/vissv2server/mqttMgr/mqttMgr_test.go`                                              |
| `JsonSchemaValidate` nil-deref guard                            | `utils/common_fixes_test.go`                                                               |
| `WsClientIndexMu` race                                          | `utils/managerhandlers_test.go`                                                            |
| `udsClientIndexMu` race + `-1` pool-exhaustion guard             | `server/vissv2server/udsMgr/udsMgr_test.go`                                                |
| `grpcStateMu` race                                              | `server/vissv2server/grpcMgr/grpcMgr_test.go`                                              |
| `getFileDescriptorData` type-assert (vissv2server)              | `server/vissv2server/vissv2server_test.go`                                                 |
| `TouchFile` mode 0644                                           | `feeder/feeder-rl/feeder-rl_test.go`                                                       |
| `onNotificationList` lookup contract                            | `feeder/feeder-template/feederv4/feederv4_test.go`                                         |

### Broader coverage of pure helpers (PR #122)

| Area                                                           | Test file                                                            |
|----------------------------------------------------------------|----------------------------------------------------------------------|
| `utils.IsNumber`, `IsBoolean` predicates                        | `utils/common_broader_test.go`                                       |
| `utils.PathToUrl` ⇄ `UrlToPath` round-trip                      | `utils/common_broader_test.go`                                       |
| `utils.GetMaxValidation`                                        | `utils/common_broader_test.go`                                       |
| `utils.ExtractRootName`                                         | `utils/common_broader_test.go`                                       |
| `utils.GenerateHmac` / `VerifyTokenSignature` round-trip + tamper | `utils/common_broader_test.go`                                       |
| `utils.AddKeyValue` no-Marshal value injection                  | `utils/common_broader_test.go`                                       |
| `utils.MapRequest` fuzz                                         | `utils/common_fixes_test.go`                                         |
| `wsMgr.getValueForKey`, `getSortedPaths`, dcCache               | `server/vissv2server/wsMgr/wsMgr_test.go`                            |

### Whole-subsystem coverage (this PR)

#### vissv2server core

| Helper                                  | Test file                                              |
|-----------------------------------------|--------------------------------------------------------|
| `extractMgrId` (`mgrId?clientId` parser) | `server/vissv2server/vissv2server_helpers_test.go`     |
| `getPathLen` (NUL-aware buffer length)   | `server/vissv2server/vissv2server_helpers_test.go`     |
| `countPathSegments`                      | `server/vissv2server/vissv2server_helpers_test.go`     |
| `getTokenErrorMessage` (all indices)     | `server/vissv2server/vissv2server_helpers_test.go`     |
| `setTokenErrorResponse`                  | `server/vissv2server/vissv2server_helpers_test.go`     |
| `singleToDoubleQuote`                    | `server/vissv2server/vissv2server_helpers_test.go`     |
| `extractNoScopeElementsLevel1` / `Level2`| `server/vissv2server/vissv2server_helpers_test.go`     |
| `getTokenContext` (safe defaults)        | `server/vissv2server/vissv2server_helpers_test.go`     |
| `removeLocalProperty`                    | `server/vissv2server/vissv2server_helpers_test.go`     |
| `getInternalFileName`                    | `server/vissv2server/vissv2server_helpers_test.go`     |
| `calculateHash` (SHA-1)                  | `server/vissv2server/vissv2server_helpers_test.go`     |
| `getRangeBoundary` / `getRangeBoundaries`| `server/vissv2server/vissv2server_helpers_test.go`     |
| `FuzzGetRangeBoundaries`                 | `server/vissv2server/vissv2server_helpers_test.go`     |

#### wsMgr (beyond pure helpers covered in PR #122)

| Helper                                  | Test file                                              |
|-----------------------------------------|--------------------------------------------------------|
| `getDcConfig`                            | `server/vissv2server/wsMgr/wsMgr_helpers_test.go`      |
| `setDcValue` (pc + tsc parsing)          | `server/vissv2server/wsMgr/wsMgr_helpers_test.go`      |
| `dcCacheInsert` / `getDcCacheIndex` / `resetDcCache` / `updatepayloadId` lifecycle | `server/vissv2server/wsMgr/wsMgr_helpers_test.go` |
| `signedTimeDiff`                         | `server/vissv2server/wsMgr/wsMgr_helpers_test.go`      |
| `compressTs` malformed-JSON rejection    | `server/vissv2server/wsMgr/wsMgr_helpers_test.go`      |
| `getDpTsList` (single / array / unknown) | `server/vissv2server/wsMgr/wsMgr_helpers_test.go`      |
| `FuzzGetValueForKey`, `FuzzCompressTs`   | `server/vissv2server/wsMgr/wsMgr_helpers_test.go`      |

#### grpcMgr (beyond mutex tests covered in PR #122)

| Helper                                  | Test file                                              |
|-----------------------------------------|--------------------------------------------------------|
| `getSubscriptionId` (valid / missing / malformed JSON) | `server/vissv2server/grpcMgr/grpcMgr_helpers_test.go` |
| `extractClientId` (valid + malformed parse) | `server/vissv2server/grpcMgr/grpcMgr_helpers_test.go` |

#### Clients (`client/client-1.0/`)

| Client                  | Test file                                                                  |
|-------------------------|----------------------------------------------------------------------------|
| `compress_client`        | `client/client-1.0/compress_client/compress_client_test.go`                |
| `csv_client`             | `client/client-1.0/csv_client/csv_client_test.go`                          |
| `filetransfer_client`    | `client/client-1.0/filetransfer_client/filetransfer_client_helpers_test.go` |
| `grpc_client`            | `client/client-1.0/grpc_client/grpc_client_test.go`                        |
| `testClient`             | `client/client-1.0/testClient/testClient_test.go`                          |

Each covers the pure helpers in that client (path conversion, JSON
parsing, request marshalling, file reading). Network-driven entry
points are tested via runtest.sh integration.

#### Feeders

| Feeder                    | Test file                                                       |
|---------------------------|-----------------------------------------------------------------|
| `feeder-rl`                | `feeder/feeder-rl/feeder-rl_test.go` (PR #122)                  |
| `feeder-template/feederv4` | `feeder/feeder-template/feederv4/feederv4_helpers_test.go`      |
| `feeder-evic`              | `feeder/feeder-evic/evicFeeder_test.go`                         |
| `feeder-evic/evicSim`      | `feeder/feeder-evic/evicSim/evicSim_test.go`                    |

Covers `fileExists`, `deSerializeUInt` (1/2/4-byte), `inFeederScope`,
`splitToDomainDataAndTs`, `convertToDomainData`, `makeDataPoint`,
`calcInputValue`, `incDpIndex`, `enumConversion`, `linearConversion`.

## Functions that need code refactoring to be unit-testable

Documented inline as `TODO(testing):` comments in each test file.
Listed here for visibility:

| Function                                                       | Why not testable                                                                            | Refactor approach                                                                       |
|----------------------------------------------------------------|---------------------------------------------------------------------------------------------|-----------------------------------------------------------------------------------------|
| `mqttMgr::createSubscribeClient`, `publishMessage` (post-`os.Exit` removal) | The fix is "absence of process exit". Helpers are wrapped around `MQTT.NewClient`.   | Extract a client-factory function so a mock `MQTT.Client` can be injected.              |
| `vissv2server::issueServiceRequest` `dt[:5]` panic fix          | Inline in the main message-dispatch goroutine.                                              | Extract the SET-on-struct-Type branch into a function that takes (`requestMap`, `dt`).  |
| `vissv2server::initiateFileTransfer` uid bounds check           | Embedded in channel-driven flow.                                                            | Extract `validateFileDescriptorUid([]byte) error`; call from `initiateFileTransfer`.    |
| `feederv4::feederRegister`, `statestorageSet`                   | Coupled to live UDS / SQLite / Redis / memcache.                                            | Inject a storage interface; mock for tests.                                             |
| `feederv4::udsReader`, `udsWriter`                              | Inline goroutine loops driving channels.                                                    | Extract per-message dispatch function; test it directly.                                |
| `feederv4::simulateInput`, `selectRandomInput`, `getRandomVssfMapIndex` | Non-deterministic (`math/rand`).                                                       | Inject `*rand.Rand`; seed in tests.                                                     |
| `feeder-evic::statestorageSet`, `udsReader/Writer`, `canDriverClient`, `serverSession`, `reDialer` | Coupled to live network / DB.                                                | Same shape: inject interfaces / extract per-message handlers.                           |
| All client `initWebSocket*`, `controlClient`, `dataClient`, `downloadFile`, `uploadFile` | Need a running vissv2server.                                                  | Already integration-tested via runtest.sh.                                              |
| `grpcMgr::GetRequest`, `SetRequest`, `UnsubscribeRequest`, `SubscribeRequest` | Streaming gRPC handlers; need a full gRPC fixture.                                   | Extract per-request body into a function taking plain Go values; test the helper.       |
| `httpMgr` (entire package)                                     | Two functions, both giant goroutine loops with no extractable pure-function surface.        | Refactor `HttpMgrInit` to call out to a `handleRequest(msg) string` helper.             |

## Subsystems with genuinely no .go code

- `server/vissv2server/forest/` — VSS-tree binary data files only.
- `server/transport_sec/` — TLS certs and `transportSec.json` config only.
- `client/client-1.0/Javascript/`, `mqtt_client/`, `transport_sec/` — no Go code (JS client, certs, sample configs).

## Note on `.gitignore`

The repo's `.gitignore` has unanchored patterns `feeder` and
`agt_server` (lines 30, 53) that silently swallow any path with those
names — multiple test files in this PR and PR #122 needed
`git add -f`. The patterns should be anchored
(`/feeder/feeder-template/feederv4/feeder` for the actual built
binary, etc.) in a separate cleanup PR.

## Note on dependency between PRs

This test PR depends on PRs #119, #120, #121, and #122 being merged
first. The tests reference helpers and mutexes added across those
batches; without them, `go test` against this branch alone will not
build. Once the four prior PRs land, the full suite builds and
passes.
