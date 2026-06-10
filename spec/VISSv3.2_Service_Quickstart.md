# VISSv3.2 Services — Quickstart Guide

This guide gets you from zero to a working service invocation using the VISSv3.2 reference
server. For the full specification see `VISSv3.2_Service.html`.

---

## What is a "service" in VISSv3.2?

A **service** is a named procedure exposed through the VISS server. Clients can:

| Action | Description |
|---|---|
| **invoke** | Execute a procedure and receive a result |
| **monitor** | Attach to an ongoing execution and receive streaming updates |
| **cancel** | Cancel an active invoke or monitor session |
| **discover** | Query the service tree for available procedures and their I/O signatures |

Services live in the **HIM forest** on a tree whose `domain` field ends in `.Service`.

---

## Step 1: Configure a service tree in `viss.him`

Add an entry to your HIM forest file (default: `server/vissv2server/viss.him`):

```yaml
HIM.SeatingService:
type: direct
domain: SeatingService.Car.Service
version: 1.0
local: forest/seating-service.v1.0.binary
description: Vehicle seating service tree.
```

The `domain` value **must** end in `.Service`. The server detects this suffix and routes
`invoke`, `monitor`, `cancel`, and `discover` requests through `vissServiceMgr`.

---

## Step 2: Add procedure nodes to the binary tree

Your `.binary` file (generated from a VSpec or HIM source) must contain procedure nodes.
A minimal tree looks like:

```
SeatingService.Car.Service (branch)
  └─ MoveSeat (procedure)
       ├─ Input (iostruct)
       │    ├─ SeatId    (property, uint8)
       │    └─ Position  (property, uint8)
       └─ Output (iostruct)
            └─ Position  (property, uint8)
```

Node types `procedure`, `iostruct`, `property`, and `symlink` are all valid within a
service tree.

---

## Step 3: Invoke a service (WebSocket example)

Connect to the server's WebSocket interface and send an **invokeRequest**:

```json
{
  "action": "invoke",
  "path": "SeatingService.Car.Service.MoveSeat",
  "input": {
    "SeatId": "1",
    "Position": "40"
  },
  "filter": {"variant": "all"},
  "requestId": "abc-123"
}
```

The server responds immediately with an **invokeSuccessResponse**:

```json
{
  "action": "invoke",
  "path": "SeatingService.Car.Service.MoveSeat",
  "status": "ONGOING",
  "serviceId": "543210",
  "requestId": "abc-123",
  "ts": "2026-06-06T12:00:00Z"
}
```

The `serviceId` is your handle for monitoring or cancelling this execution.

---

## Step 4: Receive monitoring events

While the invocation is ONGOING the server sends **monitoring events** on the same
connection:

```json
{
  "action": "monitoring",
  "path": "SeatingService.Car.Service.MoveSeat",
  "serviceId": "543210",
  "status": "ONGOING",
  "outdata": {"output": {"Position": "25"}, "ts": "2026-06-06T12:00:01Z"},
  "ts": "2026-06-06T12:00:01Z"
}
```

When the invocation finishes:

```json
{
  "action": "monitoring",
  "path": "SeatingService.Car.Service.MoveSeat",
  "serviceId": "543210",
  "status": "SUCCESSFUL",
  "outdata": {"output": {"Position": "40"}, "ts": "2026-06-06T12:00:05Z"},
  "ts": "2026-06-06T12:00:05Z"
}
```

---

## Step 5: Monitor an existing invocation

If you connect after an invocation is already running, send a **monitorRequest**:

```json
{
  "action": "monitor",
  "path": "SeatingService.Car.Service.MoveSeat",
  "filter": {"variant": "status"},
  "requestId": "xyz-456"
}
```

The server returns the current state and (if ONGOING) assigns you a `serviceId` for
further monitoring events.

---

## Step 6: Cancel an invocation or monitoring session

```json
{
  "action": "cancel",
  "serviceId": "543210"
}
```

Cancelling the **invoke session** (the original invoker's `serviceId`) cancels the
execution itself. Cancelling a **monitor session** only stops updates to that client.

---

## Step 7: Discover the service tree

```json
{
  "action": "discover",
  "path": "SeatingService.Car.Service",
  "requestId": "disc-1"
}
```

Response:

```json
{
  "action": "discover",
  "metadata": {
    "MoveSeat": {
      "type": "procedure",
      "Input": {"SeatId": {"type": 4, "datatype": "uint8"}, "Position": {"type": 4, "datatype": "uint8"}},
      "Output": {"Position": {"type": 4, "datatype": "uint8"}}
    }
  },
  "requestId": "disc-1",
  "ts": "2026-06-06T12:00:00Z"
}
```

---

## Filter variants

The `filter.variant` field controls which monitoring events you receive:

| Variant | Behaviour |
|---|---|
| `all` | Every progress update is delivered |
| `status` | Only delivered when status changes |
| `timebased` | Delivered at the interval in `filter.parameter.period` (ms) |
| `none` | No monitoring events; response is synchronous-only |

---

## Service states

```
UNKNOWN → ONGOING → SUCCESSFUL
                  → CANCELED
                  → FAILED
```

A service starts at `UNKNOWN`. When a client invokes it, the server transitions it to
`ONGOING`. The service process (or a timeout) terminates it as `SUCCESSFUL`, `CANCELED`,
or `FAILED`.

---

## HIM configuration reference

See **Appendix B** of `VISSv3.2_Service.html` for the full rules on the `.Service`
domain suffix and the `viss.him` file format.

---

## Error responses

All four actions return a consistent error object on failure:

```json
{
  "action": "invoke",
  "status": "FAILED",
  "error": {
    "number": "400",
    "reason": "bad_request",
    "description": "path must address a procedure node"
  },
  "ts": "2026-06-06T12:00:00Z"
}
```

Common error numbers: `400` (bad request), `404` (path not found), `503` (service unavailable).

---

## Request validation

Service requests (`invoke`, `monitor`, `cancel`, `discover`) are validated against
`server/vissv2server/vissv3.2-service-schema.json` before they reach the service
manager. Data requests (`get`, `set`, `subscribe`, …) continue to be validated
against the base `vissv3.0-schema.json`. The server selects the schema from the
request's `action` field, so a malformed service request — e.g. an `invoke`
missing the required `path` or `filter` — is rejected with a `400` schema error
rather than being forwarded. No configuration is needed; both schema files ship
alongside the server binary.

---

## Running the test suite

Unit tests (with race detector) for the service manager:

```bash
go test -race -count=1 ./server/vissv2server/vissServiceMgr/...
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
