# VISSv4.0 VDM Integration — Quickstart Guide

This guide gets you from a COVESA VDM `.graphql` file to a running vissr
server in five minutes. For the full specification see `VISSv4.0_VDM.html`.

---

## What is new in v4.0?

VISSv4.0 adds native support for the COVESA **VDM (Vehicle Data Model)**
GraphQL SDL format. Instead of compiling a VSS YAML catalog into a binary tree
and listing it in `viss.him`, you can point the server directly at a directory
of `.graphql` files:

```
vissv2server --vdm ./my-vdm-signals/
```

The server parses the SDL at startup, builds the equivalent signal trees in
memory, and registers them — no binary compilation step required.

---

## Prerequisites

| Tool | Why |
|---|---|
| Go 1.22+ | Build the server |
| vissr source | `git clone https://github.com/covesa/vissr && cd vissr` |
| A `.graphql` VDM file | Your signal model |

---

## Step 1: Write a VDM SDL file

Create `my-vdm/vehicle.graphql`:

```graphql
type Vehicle @vspec(element: BRANCH, fqn: "Vehicle", description: "Top-level vehicle object") {
  Speed: Float         @vspec(element: SENSOR,    fqn: "Vehicle.Speed",    description: "Current speed in km/h") @range(min: 0, max: 250)
  ADAS:  VehicleADAS   @vspec(element: BRANCH,    fqn: "Vehicle.ADAS")
  Cabin: VehicleCabin  @vspec(element: BRANCH,    fqn: "Vehicle.Cabin")
}

type VehicleADAS @vspec(element: BRANCH, fqn: "Vehicle.ADAS", description: "Advanced driver-assistance") {
  ActiveAutonomyLevel: String @vspec(element: ATTRIBUTE, fqn: "Vehicle.ADAS.ActiveAutonomyLevel", description: "Current autonomy level")
  ABS: VehicleADASABS         @vspec(element: BRANCH,    fqn: "Vehicle.ADAS.ABS")
}

type VehicleADASABS @vspec(element: BRANCH, fqn: "Vehicle.ADAS.ABS", description: "Antilock braking") {
  IsEnabled: Boolean @vspec(element: ACTUATOR, fqn: "Vehicle.ADAS.ABS.IsEnabled", description: "Enable or disable ABS")
  IsActive:  Boolean @vspec(element: SENSOR,   fqn: "Vehicle.ADAS.ABS.IsActive",  description: "ABS active")
}

type VehicleCabin @vspec(element: BRANCH, fqn: "Vehicle.Cabin", description: "Vehicle cabin") {
  Temperature: Float @vspec(element: SENSOR, fqn: "Vehicle.Cabin.Temperature", description: "Cabin temp °C") @range(min: -40, max: 100)
}
```

**Key rules:**
- Every type needs `@vspec(element: BRANCH, fqn: "...")` on the `type` declaration.
- Every signal field needs `@vspec(element: SENSOR|ACTUATOR|ATTRIBUTE, fqn: "...")`.
- The `fqn` values define the tree; the GraphQL type hierarchy is ignored.
- You do **not** need to include the directive preamble — the loader adds it automatically.

---

## Step 2: Build and start the server

```bash
# From the vissr repo root
go build ./server/vissv2server/

cd server/vissv2server/
./vissv2server --vdm ../../my-vdm/
```

You should see a log line like:

```
VDM loader: registered 1 tree(s) from ../../my-vdm/
```

The server is now accepting WebSocket connections on port 8080 (default).

---

## Step 3: Query a signal

Connect with any VISS WebSocket client and send a `get` request:

```json
{"action":"get","path":"Vehicle.Speed","requestId":"req-1"}
```

Expected response (value depends on your state-storage backend):

```json
{
  "action": "get",
  "path": "Vehicle.Speed",
  "data": {"dp": {"value": "0", "ts": "2026-06-06T12:00:00Z"}},
  "requestId": "req-1",
  "ts": "2026-06-06T12:00:00Z"
}
```

---

## Step 4: Set an actuator signal

```json
{"action":"set","path":"Vehicle.ADAS.ABS.IsEnabled","value":"true","requestId":"req-2"}
```

---

## Step 5: Add a service procedure with @viss_service

If you want to expose a callable procedure using VISSv3.2 service semantics,
add `@viss_service` to a field:

```graphql
type SeatingService @vspec(element: BRANCH, fqn: "SeatingService", description: "Seating control") {
  MoveSeat: Boolean
    @vspec(element: SENSOR, fqn: "SeatingService.MoveSeat", description: "Adjust seat position")
    @viss_service
}
```

The loader creates a `PROCEDURE` node for `SeatingService.MoveSeat`.
Clients can now invoke it using the VISSv3.2 `invoke` action:

```json
{
  "action": "invoke",
  "path": "SeatingService.MoveSeat",
  "input": {"SeatId": "1", "Position": "40"},
  "filter": {"variant": "all"},
  "requestId": "inv-1"
}
```

See `VISSv3.2_Service_Quickstart.md` for the full invoke/monitor/cancel flow.

---

## Using multiple SDL files

You can split your VDM across multiple `.graphql` files in the same directory:

```
my-vdm/
  vehicle.graphql      # Vehicle.* signals
  seating.graphql      # SeatingService.* procedures
```

All files are loaded at startup. Each distinct root FQN becomes an independent
tree in the HIM forest.

---

## Validating SDL with s2dm (recommended for CI)

The COVESA **s2dm** toolchain enforces naming conventions and structural rules
beyond what the GraphQL parser checks.  Add it as a CI gate:

```bash
# Install uv (if not already available)
curl -LsSf https://astral.sh/uv/install.sh | sh

# Validate all SDL files
uv run poe validate --input ./my-vdm/
```

s2dm is a Python tool and is **not** required at runtime — it is a CI-only
validation step.  The vissr loader will accept any syntactically valid SDL that
contains `@vspec` annotations; s2dm additionally checks semantic compliance
with COVESA VDM authoring rules.

---

## HIM vs VDM: when to use each

| Scenario | Use |
|---|---|
| Existing VSS YAML catalog compiled to binary | `viss.him` (default) |
| Authoring a new signal model with VDM tooling | `--vdm <dir>` |
| Mixing legacy HIM trees with new VDM trees | Not yet supported; use one or the other |

---

## Running the test suite

Unit tests for the VDM loader:

```bash
go test -race -count=1 ./server/vissv2server/vdmloader/...
```

Full CI (unit + MQTT integration):

```bash
# Start the mosquitto broker container
docker compose -f docker-compose.test.yml up -d

# Run all tests
go test -race -count=1 ./server/vissv2server/vissServiceMgr/...
go test -race -count=1 ./server/vissv2server/vdmloader/...
go test -v       -count=1 ./paho-mqtt/...

# Tear down
docker compose -f docker-compose.test.yml down
```

CI runs all of the above automatically on every push/PR via
`.github/workflows/test.yml`.

---

## Known limitations (v4.0alpha)

| Limitation | Planned fix |
|---|---|
| Instance tag expansion (Row × Side multi-instance) not yet implemented | v4.1 |
| `@viss_service` is a vissr extension, not part of upstream VDM | Upstreaming TBD |
| `--vdm` and `viss.him` are mutually exclusive at startup | Mixed mode planned |
| STRUCT / PROPERTY element types are parsed but not fully handled | v4.1 |
