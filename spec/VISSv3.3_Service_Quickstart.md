# VISSv3.3alpha Services — Quickstart Guide

VISSv3.3 builds on VISSv3.2 and adds a **live service process model**: procedure code runs in
a separate process that connects to the VISS server over TCP. This guide covers both sides —
**client usage** (invoke/monitor/discover) and **service implementation** (the service process).

Read `VISSv3.2_Service_Quickstart.md` first if you haven't; the client-side protocol is
unchanged. This guide focuses on the v3.3 additions.

---

## What's new in v3.3?

| Feature | Summary |
|---|---|
| **Service SDK** | Go SDK (`vissServiceSDK`) for writing service processes |
| **TCP registration** | Service processes register on port 8300 |
| **Concurrent invocations** | Multiple clients can invoke the same procedure simultaneously |
| **Per-invocation timeout** | Default 30s; overridable per request |
| **Heartbeat** | Server sends ping every 15s; disconnects on missed pong |
| **Structured errors** | FAILED updates carry `{code, message}` |
| **Auth pass-through** | Client auth token forwarded to service process |
| **TLS on port 8300** | Optional mutual TLS for the service registration channel |
| **SSE helper** | HTTP monitoring can use Server-Sent Events |
| **Auto-reconnect SDK** | SDK reconnects on connection loss with exponential backoff |
| **Discover enrichment**<sup>†</sup> | Discover responses include live `serviceStatus` and `activeInvocations` |
| **Cancel propagation** | Server forwards cancel to service process; SDK exposes `ctx.Done()` |
| **Service versioning**<sup>†</sup> | Services declare a `version` string; appears in discover responses |
| **Progress percentage** | ONGOING updates carry optional `progress` 0-100 field |
| **Structured validation errors** | Missing Input fields listed by name on invoke failure |
| **Service health reporting**<sup>†</sup> | Services report health status; shown in discover responses |
| **Observability metrics**<sup>†</sup> | Per-path counters (`totalInvocations`, `successRate`, `avgDurationMs`) in discover |

<sup>†</sup> **Superseded:** discover's `metadata` is now produced by the same generic HIM-tree walk
used by the Data profile's metadata filter (`action:"get"`, `filter.variant:"metadata"`), which has
no notion of these live-status fields. The server-side code that computes them is retained,
unused, for potential future reuse as a post-processing step. `depth` (mandatory) replaces the
resource filter for narrowing discover's response - see the updated "Discover the service tree"
section in `VISSv3.2_Service_Quickstart.md`.

---

## Part 1: Writing a service process (Go SDK)

### 1.1 Install the SDK

```bash
go get github.com/covesa/vissr/server/vissv2server/vissServiceSDK
```

### 1.2 Minimal service process

```go
package main

import (
    "log"
    "time"

    "github.com/covesa/vissr/server/vissv2server/vissServiceSDK"
)

func main() {
    svc, err := vissServiceSDK.NewService("localhost:8300", "SeatingService.Car.Service.MoveSeat").
        WithInput("SeatId", "uint8").
        WithInput("Position", "uint8").
        WithOutput("Position", "uint8").
        OnInvoke(moveSeat).
        Register()
    if err != nil {
        log.Fatalf("register: %v", err)
    }
    defer svc.Close()
    svc.Run() // blocks
}

func moveSeat(ctx *vissServiceSDK.InvokeContext) {
    seatId := ctx.Input["SeatId"]
    target := ctx.Input["Position"]

    // Optional: check authorization (VISSv3.3 §21)
    if ctx.Authorization == "" {
        ctx.ReportError("UNAUTHORIZED", "authorization required", nil)
        return
    }

    // Simulate movement with periodic updates.
    for pos := 0; pos <= 40; pos += 10 {
        time.Sleep(500 * time.Millisecond)
        ctx.ReportProgress("ONGOING", map[string]interface{}{"Position": pos})
    }

    ctx.ReportProgress("SUCCESSFUL", map[string]interface{}{
        "SeatId": seatId, "Position": target,
    })
}
```

### 1.3 Builder API

| Method | Purpose |
|---|---|
| `NewService(addr, path)` | Create a new service |
| `.WithInput(name, datatype)` | Declare an input parameter |
| `.WithOutput(name, datatype)` | Declare an output parameter |
| `.OnInvoke(handler)` | Register the invocation handler |
| `.WithReconnect(maxRetries, delay)` | Enable auto-reconnect (§24) |
| `.Register()` | Connect and register with the server |
| `.Run()` | Block and dispatch invocations |
| `.Close()` | Deregister and disconnect |

### 1.4 Reporting errors (§20)

```go
ctx.ReportError("MOTOR_STALL", "seat motor blocked at position 25",
    map[string]interface{}{"Position": "25"})
```

This sends a FAILED update with a structured error payload. The client receives:

```json
{
  "action": "monitoring",
  "status": "FAILED",
  "error": {"code": "MOTOR_STALL", "message": "seat motor blocked at position 25"},
  "outdata": {"output": {"Position": "25"}, "ts": "..."},
  "ts": "..."
}
```

### 1.5 Auto-reconnect (§24)

```go
svc, err := vissServiceSDK.NewService("localhost:8300", "My.Proc").
    OnInvoke(handler).
    WithReconnect(5, time.Second). // max 5 retries, starting at 1s backoff
    Register()
```

Backoff doubles on each failure, capped at 2 minutes. Pass `maxRetries=0` for unlimited retries.

---

## Part 2: Client changes in v3.3

### 2.1 Concurrent invocations (§10)

Multiple clients can invoke the same procedure simultaneously. Each gets its own `serviceId`
and independent state machine. Monitoring sessions attach to a specific invocation by path
(the server picks the most recently started ONGOING invocation).

### 2.2 Per-request timeout (§11)

```json
{
  "action": "invoke",
  "path": "SeatingService.Car.Service.MoveSeat",
  "input": {"SeatId": "1", "Position": "40"},
  "filter": {"variant": "all"},
  "timeout": 10000,
  "requestId": "r-1"
}
```

`timeout` is milliseconds. Omitting it uses the server default (30s). Setting it to `0`
disables the timeout for this invocation.

### 2.3 Discover enrichment (§25) — superseded

> **Superseded:** the fields described in this subsection are no longer present in discover
> responses; see the note under "What's new in v3.3?" above. `discover` now requires a mandatory
> `depth` field and returns the addressed subtree in the same nested/`children`-keyed shape as the
> Data profile's metadata filter - see `VISSv3.2_Service_Quickstart.md`'s "Discover the service
> tree" section for the current shape.

The `discover` response previously included live fields per procedure:

```json
{
  "action": "discover",
  "metadata": {
    "MoveSeat": {
      "type": "procedure",
      "Input": {"SeatId": {...}, "Position": {...}},
      "Output": {"Position": {...}},
      "serviceStatus": "registered",
      "activeInvocations": 2
    }
  },
  "ts": "..."
}
```

| Field | Values | Meaning |
|---|---|---|
| `serviceStatus` | `"registered"` / `"disconnected"` | Is a service process connected? |
| `activeInvocations` | integer ≥ 0 | How many ONGOING invocations right now |

---

## Part 3: The registration protocol (§12)

Service processes connect to **TCP port 8300** (or 8300/TLS with §22). The protocol is
line-delimited JSON.

### Handshake

```
Service → Server:  {"action":"register","path":"Root.Proc","signature":{"input":{...},"output":{...}}}
Server  → Service:  {"registered":true,"path":"Root.Proc"}
```

If the path is already registered:

```
Server → Service: {"registered":false,"reason":"path already registered"}
```

### Heartbeat (§19)

Every 15 seconds the server sends:

```
Server → Service: {"action":"ping"}
```

The service must reply within 5 seconds:

```
Service → Server: {"action":"pong"}
```

Missed pong → server closes the connection and marks all invocations for that path as FAILED.

### Invocation forwarding

When a client calls `invoke`, the server forwards to the service process:

```json
{"action":"invoke","sessionId":"543210","input":{"SeatId":"1","Position":"40"},"authorization":"Bearer ..."}
```

The `authorization` field is omitted if the client did not provide one.

### Progress updates

```json
{"sessionId":"543210","status":"ONGOING","output":{"Position":"25"}}
{"sessionId":"543210","status":"SUCCESSFUL","output":{"Position":"40"}}
```

Error:

```json
{"sessionId":"543210","status":"FAILED","error":{"code":"MOTOR_STALL","message":"blocked"}}
```

---

## Part 4: TLS on port 8300 (§22)

Start the registration server with TLS in your server binary:

```go
err := vissServiceMgr.StartServiceRegServerTLS(backendChans, "cert.pem", "key.pem")
```

The SDK connects via plain TCP by default. To use TLS, dial manually with `tls.Dial` and
pass the connection to a custom service implementation (TLS client support in the SDK is
a planned future extension).

---

## Part 5: HTTP Server-Sent Events (§23)

For browser clients, monitoring events can be served as SSE:

```go
func monitorHandler(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "text/event-stream")
    // ... start monitoring session, then for each event:
    frame, err := vissServiceMgr.FormatAsSSE(event)
    if err == nil {
        fmt.Fprint(w, frame)
        w.(http.Flusher).Flush()
    }
}
```

Each SSE frame is `data: <json>\n\n`.

---

## Quick-reference: v3.3 vs v3.2 differences

| Topic | v3.2 | v3.3 |
|---|---|---|
| Service implementation | In-process | Separate process via TCP |
| Concurrent invocations | One per path | Unlimited per path |
| Timeout | None | 30s default, per-request override |
| Service status in discover<sup>†</sup> | No | (was: `serviceStatus`, `activeInvocations`) |
| Structured errors | No | Yes (`error.code` + `error.message`) |
| Auth forwarding | Not specified | Client token forwarded to service |
| TLS on service channel | No | Yes (port 8300 TLS) |
| Heartbeat | No | Ping every 15s, pong within 5s |
| Auto-reconnect | No | SDK built-in with backoff |
| Cancel propagation | No | Server forwards cancel; `ctx.Done()` in SDK |
| Service versioning<sup>†</sup> | No | `WithVersion()` (was: shown in discover) |
| Progress percentage | No | `ReportProgressPct()` + `progress` 0-100 field |
| Validation error details | Generic string | `fields` array with missing field names |
| Health reporting<sup>†</sup> | No | `ReportHealth()` (was: `serviceHealth` in discover) |
| Observability metrics<sup>†</sup> | No | (was: `totalInvocations`, `successRate`, `avgDurationMs` in discover) |
| Discover depth limiting | No | `depth` mandatory; replaces the old resource filter |

<sup>†</sup> Superseded - see the note under "What's new in v3.3?" above.

---

## Part 6: New features in detail (§26–§31)

### 6.1 Cancel propagation (§26)

When a client cancels an invocation, the server notifies your service process.
Listen on `ctx.Done()` to stop early:

```go
OnInvoke(func(ctx *vissServiceSDK.InvokeContext) {
    for i := 0; i < 100; i++ {
        select {
        case <-ctx.Done():
            return // client cancelled — stop immediately
        default:
        }
        time.Sleep(100 * time.Millisecond)
        ctx.ReportProgressPct(i, "ONGOING", nil)
    }
    ctx.ReportProgress("SUCCESSFUL", map[string]interface{}{"done": true})
})
```

### 6.2 Service versioning (§27)

Declare a version during registration:

```go
vissServiceSDK.NewService(serverAddr, "Root.Proc").
    WithVersion("2.1.0").
    OnInvoke(handler).
    Register()
```

> **Superseded:** the server stores this version but no longer surfaces it in the discover
> response - see the note under "What's new in v3.3?" above.

### 6.3 Progress percentage (§28)

Report granular progress with `ReportProgressPct`:

```go
ctx.ReportProgressPct(25, "ONGOING", map[string]interface{}{"phase": "init"})
ctx.ReportProgressPct(75, "ONGOING", map[string]interface{}{"phase": "executing"})
ctx.ReportProgress("SUCCESSFUL", finalResult)
```

Monitoring clients receive `"progress": 25` and `"progress": 75` in events.
Values outside [0, 100] are silently clamped.

### 6.4 Structured validation errors (§29)

If an invoke request is missing required Input fields the server now returns
the field names, not just a generic string:

```json
{
  "action": "invoke",
  "status": "FAILED",
  "error": {
    "number": "400",
    "reason": "bad_request",
    "description": "input does not conform to service signature",
    "fields": ["SeatId", "Position"]
  }
}
```

### 6.5 Health reporting (§30)

The SDK automatically sends `healthy:true` after registration. Update health
status at any time:

```go
svc.ReportHealth(false, "seat motor overheated — maintenance required")
```

> **Superseded:** the server stores the latest health report but no longer surfaces it in the
> discover response as `serviceHealth` - see the note under "What's new in v3.3?" above.

### 6.6 Observability metrics (§31)

The server accumulates per-path stats (`totalInvocations`, `successRate`, `avgDurationMs`) as
requests are processed; these reset when the server restarts.

> **Superseded:** these counters are no longer surfaced in the discover response - see the note
> under "What's new in v3.3?" above.

---

## Running the example

A complete example service process (`seatService.go`) is in
`server/vissv2server/vissServiceMgr/example/`. It registers the procedure
`VehicleService.Seating.MoveSeat` and simulates seat movement with periodic
position updates. To run it:

```bash
# Terminal 1: start the VISS server
cd server/vissv2server && go run . --him viss.him

# Terminal 2: start the service process
go run ./server/vissv2server/vissServiceMgr/example/seatService.go

# Terminal 3: invoke the service
wscat -c ws://localhost:8080 <<'MSG'
{"action":"invoke","path":"VehicleService.Seating.MoveSeat","input":{"SeatId":"1","Position":"40"},"filter":{"variant":"all"},"requestId":"r1"}
MSG
```

---

## Running the test suite

Unit tests (with race detector) for the service packages:

```bash
go test -race -count=1 ./server/vissv2server/vissServiceMgr/...
go test -race -count=1 ./server/vissv2server/vissServiceSDK/...
```

MQTT integration tests require the mosquitto broker container running locally.
Use `docker-compose.test.yml` at the repo root:

```bash
# Start broker (first-time: trust the CA cert — see docker-compose.test.yml header)
docker compose -f docker-compose.test.yml up -d

# Run MQTT tests
go test -v -count=1 ./paho-mqtt/...

# Tear down
docker compose -f docker-compose.test.yml down
```

CI runs all of the above automatically on every push/PR via
`.github/workflows/test.yml`.
