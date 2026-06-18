/**
* (C) 2026 Ford Motor Company
*
* SPDX-License-Identifier: MPL-2.0
*
* Built-in (in-process) service simulations.
**/

package vissServiceMgr

import (
	"strconv"
	"strings"
	"sync"
	"time"
)

// Built-in services provide an in-process simulation for demo/test procedures
// when no external service process (serviceReg.go) has registered for the
// addressed path. Without one, an invoke to e.g.
// VehicleService.Seating.Row1.DriverSide.MoveSeat would create an invocation
// that nothing ever drives via UpdateServiceState: the time-based ticker emits
// content-less ONGOING monitoring events until the timeout watchdog fires
// FAILED ~30 s later. The built-in supplies realistic state and terminates the
// session, matching the VISSv3.2 Service spec event example.

// builtinDecision is what a builtin returns synchronously to HandleInvoke.
//
//   - errNum != ""        -> send an error invoke response; create nothing.
//   - immediate != ""     -> send that terminal status as the invoke response
//     (with outdata); create no invocation/session, emit no events.
//   - run != nil          -> proceed with the normal ONGOING invocation,
//     session and ticker, then start run() as the async driver. minDuration
//     extends the timeout-watchdog deadline so a long move is not killed before
//     it completes.
type builtinDecision struct {
	immediate   ServiceStatus
	outdata     map[string]interface{}
	errNum      string
	errReason   string
	errDesc     string
	minDuration time.Duration
	run         func(serviceId string, backendChans []chan map[string]interface{})
}

type builtinHandler func(path string, input map[string]interface{}) builtinDecision

// builtinServices maps a procedure name (the last path segment) to its
// in-process handler. Lookup is by procedure name so a built-in serves every
// instance path that ends in that procedure.
var builtinServices = map[string]builtinHandler{
	"MoveSeat": moveSeatBuiltin,
}

// procedureName returns the last "."-separated segment of a service path.
func procedureName(path string) string {
	if i := strings.LastIndex(path, "."); i >= 0 {
		return path[i+1:]
	}
	return path
}

// ── MoveSeat ────────────────────────────────────────────────────────────────

// moveSeatStepPeriod is the wall-clock interval between one-percentage-point
// position changes (VISSv3.3 service example). A var so tests can shrink it.
var moveSeatStepPeriod = time.Second

var moveSeatMovementTypes = map[string]bool{
	"longitudinal": true,
	"vertical":     true,
	"recline":      true,
}

// seatState stores the simulated position (0-100) per (path, MovementType),
// initialised to 0 on first use and persisted for the process lifetime. Keyed
// by the full instance path so each seat instance has independent state.
var (
	seatMu    sync.Mutex
	seatState = map[string]int{}
)

func seatKey(path, movementType string) string { return path + "\x00" + movementType }

func seatOutput(position int) map[string]interface{} {
	return map[string]interface{}{"Position": strconv.Itoa(position)}
}

// moveSeatBuiltin simulates VehicleService...MoveSeat per Ulf's specification:
//
//   - MovementType must be one of longitudinal, vertical, recline.
//   - Position is a percentage restricted to [0,100].
//   - If the requested Position equals the saved state -> SUCCESSFUL, no events.
//   - If out of range (or non-numeric / unknown MovementType) -> error response,
//     no events.
//   - Otherwise the saved state is incremented/decremented by one percentage
//     point per second, emitting monitoring events, until it reaches the
//     requested Position, at which point the status becomes SUCCESSFUL and
//     events stop.
func moveSeatBuiltin(path string, input map[string]interface{}) builtinDecision {
	movementType, _ := input["MovementType"].(string)
	if !moveSeatMovementTypes[movementType] {
		return builtinDecision{
			errNum: "400", errReason: "bad_request",
			errDesc: "MovementType must be one of: longitudinal, vertical, recline",
		}
	}

	posStr, _ := input["Position"].(string)
	target, err := strconv.Atoi(strings.TrimSpace(posStr))
	if err != nil || target < 0 || target > 100 {
		return builtinDecision{
			errNum: "400", errReason: "bad_request",
			errDesc: "Position must be an integer percentage between 0 and 100",
		}
	}

	key := seatKey(path, movementType)
	seatMu.Lock()
	current := seatState[key]
	seatMu.Unlock()

	if current == target {
		// Already at the requested position: terminal response, no movement.
		return builtinDecision{immediate: StatusSuccessful, outdata: seatOutput(current)}
	}

	steps := target - current
	if steps < 0 {
		steps = -steps
	}
	// Allow one step per second plus a small buffer so the timeout watchdog
	// does not kill a long move (0->100 takes 100 s, well past DefaultTimeout).
	minDuration := time.Duration(steps+2) * moveSeatStepPeriod

	return builtinDecision{
		minDuration: minDuration,
		run: func(serviceId string, backendChans []chan map[string]interface{}) {
			ticker := time.NewTicker(moveSeatStepPeriod)
			defer ticker.Stop()
			for range ticker.C {
				// Stop promptly if the invocation was cancelled or removed.
				mu.Lock()
				_, alive := invocations[serviceId]
				mu.Unlock()
				if !alive {
					return
				}

				seatMu.Lock()
				cur := seatState[key]
				if cur < target {
					cur++
				} else if cur > target {
					cur--
				}
				seatState[key] = cur
				seatMu.Unlock()

				if cur == target {
					UpdateServiceState(serviceId, StatusSuccessful, seatOutput(cur), nil, nil, backendChans)
					return
				}
				UpdateServiceState(serviceId, StatusOngoing, seatOutput(cur), nil, nil, backendChans)
			}
		},
	}
}
