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
| **Discover enrichment** | Discover responses include live `serviceStatus` and `activeInvocations` |
| **Cancel propagation** | Server forwards cancel to service process; SDK exposes `ctx.Done()` |
| **Service versioning** | Services declare a `version` string; appears in discover responses |
| **Progress percentage** | ONGOING updates carry optional `progress` 0-100 field |
| **Structured validation errors** | Missing Input fields listed by name on invoke failure |
| **Service health reporting** | Services report health status; shown in discover responses |
| **Observability metrics** | Per-path counters (`totalInvocations`, `successRate`, `avgDurationMs`) in discover |

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

### 2.3 Discover enrichment (§25)

The `discover` response now includes live fields per procedure:

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
| Service status in discover | No | Yes (`serviceStatus`, `activeInvocations`) |
| Structured errors | No | Yes (`error.code` + `error.message`) |
| Auth forwarding | Not specified | Client token forwarded to service |
| TLS on service channel | No | Yes (port 8300 TLS) |
| Heartbeat | No | Ping every 15s, pong within 5s |
| Auto-reconnect | No | SDK built-in with backoff |
| Cancel propagation | No | Server forwards cancel; `ctx.Done()` in SDK |
| Service versioning | No | `WithVersion()` + shows in discover |
| Progress percentage | No | `ReportProgressPct()` + `progress` 0-100 field |
| Validation error details | Generic string | `fields` array with missing field names |
| Health reporting | No | `ReportHealth()` + `serviceHealth` in discover |
| Observability metrics | No | `totalInvocations`, `successRate`, `avgDurationMs` in discover |

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

Declare a version to make upgrades visible in discover responses:

```go
vissServiceSDK.NewService(serverAddr, "Root.Proc").
    WithVersion("2.1.0").
    OnInvoke(handler).
    Register()
```

Clients see `"version":"2.1.0"` in the discover response alongside
`serviceStatus`, allowing them to validate compatibility before invoking.

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

Clients see this in discover:

```json
"serviceHealth": {
  "healthy": false,
  "detail": "seat motor overheated — maintenance required",
  "updatedAt": "2026-01-15T10:03:00Z"
}
```

### 6.6 Observability metrics (§31)

After the service has processed some requests, discover shows cumulative stats:

```json
"totalInvocations": 124,
"successRate": 0.98,
"avgDurationMs": 1240
```

These reset when the server restarts. Use them to surface dashboards or
orchestration health checks without a separate metrics endpoint.

---

## Running the example

A complete example service process is in
`server/vissv2server/vissServiceMgr/example/`. To run it:

```bash
# Terminal 1: start the VISS server
cd server/vissv2server && go run . --him viss.him

# Terminal 2: start the service process
cd server/vissv2server/vissServiceMgr/example && go run .

# Terminal 3: invoke the service
wscat -c ws://localhost:8080 <<'MSG'
{"action":"invoke","path":"My.Service.Proc","input":{},"filter":{"variant":"all"},"requestId":"r1"}
MSG
```
