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

## Inspecting your signal tree with vdminfo

Before starting the server, verify what the loader will produce:

```bash
go run ./tools/vdminfo ./my-vdm/
```

Example output:
```
=== Vehicle  (domain: Vehicle, version: 1.0) ===
Vehicle  [branch, 6 children]
  Speed  (sensor, float, km/h, 0..250)
  CurrentGear  (sensor, vehiclegearposition, default=Park, allowed=[Park|Reverse|Neutral|Drive])
  Cabin  [branch, 1 children]
    Seat  [branch, 2 children]
      Row1  [branch, 2 children]
        DriverSide  [branch, 3 children]
          IsOccupied  (sensor, bool)
          Position  (actuator, uint8, 0..100)
        PassengerSide  [branch, 3 children]
          ...
```

Use it in CI to diff tree output between VDM versions:
```bash
go run ./tools/vdminfo ./my-vdm/ > tree-new.txt
diff tree-baseline.txt tree-new.txt
```

---

## Web dashboard

Start the server with `--web-addr` to open the embedded signal-tree dashboard:

```bash
vissv2server --vdm ./my-vdm/ --web-addr :8090
```

Then open **http://localhost:8090** in a browser. The dashboard shows:

- A collapsible signal tree on the left (searchable)
- Node details on the right: path, type, datatype, unit, range, default, allowed values
- Switch between loaded trees via the dropdown at the top

The dashboard reads from `/api/forest` and `/api/tree/{rootName}` — you can
also query these endpoints directly with `curl` for scripting.

---

## REST+SSE transport

An optional REST+SSE transport is available alongside WebSocket and HTTP.
Start it with `--restenable` (port defaults to 8081; override with `--restport`):

```bash
vissv2server --vdm ./my-vdm/ --restenable --restport 8081
```

### Read a signal

```bash
curl http://localhost:8081/viss/v2/Vehicle.Speed
```

Response (VISS JSON):
```json
{"action":"get","path":"Vehicle.Speed","data":{"dp":{"value":"42","ts":"2026-06-08T12:00:00Z"}},"requestId":"rest-1","ts":"..."}
```

### Write a signal

```bash
curl -X PUT http://localhost:8081/viss/v2/Vehicle.ADAS.ABS.IsEnabled \
     -H 'Content-Type: application/json' \
     -d '{"value":"true"}'
```

### Stream signal updates (SSE)

```bash
curl -N http://localhost:8081/viss/v2/Vehicle.Speed/subscribe
```

Each SSE event has `event: notification` and VISS JSON in the data line.
Cancel with `DELETE /viss/v2/{path}/subscribe?subscriptionId=<id>`.

### Metadata

```bash
curl http://localhost:8081/viss/v2/metadata/Vehicle.Speed
```

### Path format

Signals may be specified as dot-separated VISS paths (`Vehicle.Speed`) or
slash-separated URL paths (`Vehicle/Speed`) — both are accepted.

### CORS

All responses include `Access-Control-Allow-Origin: *` and the transport
handles `OPTIONS` pre-flight requests, making it suitable for browser use.

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

## Units, default values, and allowed values

### Units

Add `@unit(value: "…")` to any signal field. Pass a standard string or a
SCREAMING_SNAKE_CASE name — the loader normalises both:

```graphql
Speed: Float @vspec(element: SENSOR, fqn: "Vehicle.Speed") @unit(value: "km/h")
Temp:  Float @vspec(element: SENSOR, fqn: "Vehicle.Cabin.Temperature") @unit(value: "DEGREES_CELSIUS")
# → Node_t.Unit = "celsius"
```

Common normalised names: `km/h`, `m/s`, `mph`, `celsius`, `degrees`, `radians`,
`percent`, `m`, `km`, `kW`, `kWh`, `V`, `A`, `Nm`, `rpm`, `s`, `ms`, `Hz`.

### Default values

```graphql
IsEnabled: Boolean @vspec(element: ACTUATOR, fqn: "…") @defaultValue(value: "false")
CurrentGear: GearEnum @vspec(element: SENSOR, fqn: "…") @defaultValue(value: "Park")
```

The value is stored as a string in `Node_t.DefaultValue` and used by the server
to seed the state-storage backend at startup.

### Allowed values (enum signals)

Type a signal field as a GraphQL enum and the loader automatically populates
`Node_t.AllowedDef`:

```graphql
enum GearPosition {
  PARK    @vspec(originalName: "Park")
  REVERSE @vspec(originalName: "Reverse")
  NEUTRAL @vspec(originalName: "Neutral")
  DRIVE   @vspec(originalName: "Drive")
}

type Vehicle … {
  CurrentGear: GearPosition
    @vspec(element: SENSOR, fqn: "Vehicle.CurrentGear")
    @defaultValue(value: "Park")
}
```

The server will reject `set` requests with values outside the allowed list.

---

## Multi-instance signals (instance tags)

VDM's instance tag mechanism lets one signal definition expand to cover many
physical instances — for example one seat template that becomes
`Row1.DriverSide`, `Row1.PassengerSide`, `Row2.DriverSide`, etc.

Add an `_InstanceTag` type and its dimension enums to your SDL:

```graphql
type VehicleCabinSeat @vspec(element: BRANCH, fqn: "Vehicle.Cabin.Seat") {
  IsOccupied: Boolean @vspec(element: SENSOR,   fqn: "Vehicle.Cabin.Seat.IsOccupied")
  Position:   UInt8   @vspec(element: ACTUATOR, fqn: "Vehicle.Cabin.Seat.Position") @range(min: 0, max: 100)
}

type VehicleCabinSeat_InstanceTag @instanceTag @vspec(element: BRANCH, fqn: "Vehicle.Cabin.Seat") {
  dimension1: VehicleCabinSeat_InstanceTag_Dimension1
  dimension2: VehicleCabinSeat_InstanceTag_Dimension2
}

enum VehicleCabinSeat_InstanceTag_Dimension1 {
  ROW1 @vspec(originalName: "Row1")
  ROW2 @vspec(originalName: "Row2")
}

enum VehicleCabinSeat_InstanceTag_Dimension2 {
  DRIVER_SIDE    @vspec(originalName: "DriverSide")
  PASSENGER_SIDE @vspec(originalName: "PassengerSide")
}
```

The loader expands this automatically into:
```
Vehicle.Cabin.Seat.Row1.DriverSide.IsOccupied
Vehicle.Cabin.Seat.Row1.DriverSide.Position
Vehicle.Cabin.Seat.Row1.PassengerSide.IsOccupied
Vehicle.Cabin.Seat.Row1.PassengerSide.Position
Vehicle.Cabin.Seat.Row2.DriverSide.IsOccupied
...
```

Query any instance directly:
```json
{"action":"get","path":"Vehicle.Cabin.Seat.Row1.DriverSide.Position","requestId":"req-3"}
```

Up to N dimensions are supported; VDM today uses 1 or 2.

---

## Known limitations (v4.0)

| Limitation | Planned fix |
|---|---|
| `@viss_service` is a vissr extension, not part of upstream VDM | Upstreaming TBD |
| `--vdm` and `viss.him` are mutually exclusive at startup | Mixed mode planned |
| STRUCT / PROPERTY element types are parsed but not expanded | v4.1 |
