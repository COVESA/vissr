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
	"sync/atomic"
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
//     (with outdata or outdataByResource); create no invocation/session, emit
//     no events.
//   - run != nil          -> proceed with the normal ONGOING invocation,
//     session and ticker, then start run() as the async driver. minDuration
//     extends the timeout-watchdog deadline so a long move is not killed before
//     it completes.
//
// outdataByResource, when non-nil, is used instead of outdata for a
// multi-resource immediate decision (i.e. every addressed resource happened
// to already be at its target — the "all immediate" case); a mix of
// immediate and driven resources is handled by run() reporting per-resource
// via UpdateServiceState's resourceKey parameter instead.
type builtinDecision struct {
	immediate         ServiceStatus
	outdata           map[string]interface{}
	outdataByResource map[string]map[string]interface{}
	errNum            string
	errReason         string
	errDesc           string
	minDuration       time.Duration
	run               func(serviceId string, backendChans []chan map[string]interface{})
}

// builtinHandler simulates a procedure. resourceKeys is nil/empty for a
// single-resource procedure; for a multiplexed procedure it holds the
// concrete resource-instance keys addressed by this invocation (resolved
// from the request's "resource" filter, or every resource if none given).
type builtinHandler func(path string, resourceKeys []string, input map[string]interface{}) builtinDecision

// builtinServices maps a procedure name (the last path segment) to its
// in-process handler. Lookup is by procedure name so a built-in serves every
// instance path that ends in that procedure.
var builtinServices = map[string]builtinHandler{
	"MoveSeat":        moveSeatBuiltin,
	"ActivateMassage": activateMassageBuiltin,
	"GetCapabilities": getCapabilitiesBuiltin,
}

// procedureName returns the last "."-separated segment of a service path.
func procedureName(path string) string {
	if i := strings.LastIndex(path, "."); i >= 0 {
		return path[i+1:]
	}
	return path
}

// ── MoveSeat ────────────────────────────────────────────────────────────────

// moveSeatStepPeriodNs is the wall-clock interval (nanoseconds) between
// one-percentage-point position changes (VISSv3.3 service example), stored
// atomically since it is read concurrently by every per-resource driver
// goroutine spawned by moveSeatBuiltin (one per addressed resource — §4) and
// written by tests via setMoveSeatStepPeriod/shrinkStepPeriod. A plain
// package-level var here previously raced under `go test -race`: a test's
// t.Cleanup restoring the value could run concurrently with a driver
// goroutine from an earlier invocation still reading it.
var moveSeatStepPeriodNs = int64(time.Second)

// moveSeatStepPeriod returns the current step period.
func moveSeatStepPeriod() time.Duration {
	return time.Duration(atomic.LoadInt64(&moveSeatStepPeriodNs))
}

// setMoveSeatStepPeriod sets the step period. Exposed for tests
// (shrinkStepPeriod); not used by production code otherwise.
func setMoveSeatStepPeriod(d time.Duration) {
	atomic.StoreInt64(&moveSeatStepPeriodNs, int64(d))
}

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

// ── GetCapabilities ─────────────────────────────────────────────────────────

// getCapabilitiesBuiltin implements VehicleService.Seating.GetCapabilities
// (issue #198's requirement to return real capability data instead of timing
// out) per the HIM Types.Struct.Service.Capability/CapabilityData struct
// shapes defined in resources/ServiceTypes-example.vspec:
// Capability{Name, Data: CapabilityData{Name, Description}[]}.
//
// GetCapabilities is single-resource (§1.1: it describes the Seating service
// group as a whole, not per-seat), so resourceKeys is ignored; it responds
// immediately with no ticker/session, matching the "core capabilities do not
// change at runtime" nature of the data.
func getCapabilitiesBuiltin(path string, resourceKeys []string, input map[string]interface{}) builtinDecision {
	capability := func(name string, data ...map[string]interface{}) map[string]interface{} {
		return map[string]interface{}{"Name": name, "Data": data}
	}
	capData := func(name, description string) map[string]interface{} {
		return map[string]interface{}{"Name": name, "Description": description}
	}
	return builtinDecision{
		immediate: StatusSuccessful,
		outdata: map[string]interface{}{
			"Capabilities": []map[string]interface{}{
				capability("MoveSeat",
					capData("longitudinal", "Forward/backward seat movement."),
					capData("vertical", "Up/down seat movement."),
					capData("recline", "Backrest recline angle adjustment."),
				),
				capability("ActivateMassage",
					capData("roll", "Rolling massage pattern."),
					capData("lumbar", "Lumbar-support massage pattern."),
				),
			},
		},
	}
}

// resourceScopedPath returns the state-namespacing key for one resource
// instance of a (possibly multiplexed) procedure: path alone for a
// single-resource procedure (resourceKey == ""), or path+resourceKey so that
// each resource instance (e.g. Row1.DriverSide vs Row1.PassengerSide) keeps
// independent simulated state despite sharing the same stable procedure path
// (§4.1: with the tree rebuilt to the HIM-canonical multiplexed shape, "path"
// is always the same procedure path regardless of which seat is addressed).
func resourceScopedPath(path, resourceKey string) string {
	if resourceKey == "" {
		return path
	}
	return path + "." + resourceKey
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
//
// resourceKeys is nil/empty for a single-resource invocation (unchanged
// behaviour, keyed by path alone). For a multiplexed MoveSeat invocation it
// holds every addressed resource instance (e.g. ["Row1.DriverSide",
// "Row1.PassengerSide"]); each resource is driven independently — one seat
// already at the target position and another needing to move is supported
// simultaneously (§4.2 "partial-immediate + partial-run").
func moveSeatBuiltin(path string, resourceKeys []string, input map[string]interface{}) builtinDecision {
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

	// A single-resource invocation (no resource filter, or a non-multiplexed
	// procedure) is represented internally as one implicit "" resource key so
	// the same per-resource loop below handles both cases uniformly.
	keys := resourceKeys
	if len(keys) == 0 {
		keys = []string{""}
	}

	type resourceState struct {
		key     string // resource key ("" for single-resource)
		seatKey string // seatState map key
		current int
	}
	states := make([]resourceState, len(keys))
	maxSteps := 0
	allImmediate := true
	seatMu.Lock()
	for i, rk := range keys {
		sk := seatKey(resourceScopedPath(path, rk), movementType)
		cur := seatState[sk]
		states[i] = resourceState{key: rk, seatKey: sk, current: cur}
		if cur != target {
			allImmediate = false
			steps := target - cur
			if steps < 0 {
				steps = -steps
			}
			if steps > maxSteps {
				maxSteps = steps
			}
		}
	}
	seatMu.Unlock()

	if allImmediate {
		// Every addressed resource is already at the requested position:
		// terminal response, no movement, no invocation created (§4.2 fast path).
		if len(keys) > 1 {
			byResource := make(map[string]map[string]interface{}, len(states))
			for _, s := range states {
				byResource[s.key] = seatOutput(s.current)
			}
			return builtinDecision{immediate: StatusSuccessful, outdataByResource: byResource}
		}
		return builtinDecision{immediate: StatusSuccessful, outdata: seatOutput(states[0].current)}
	}

	// Allow one step per second plus a small buffer so the timeout watchdog
	// does not kill the longest move (0->100 takes 100 s, well past DefaultTimeout).
	minDuration := time.Duration(maxSteps+2) * moveSeatStepPeriod()

	return builtinDecision{
		minDuration: minDuration,
		run: func(serviceId string, backendChans []chan map[string]interface{}) {
			var wg sync.WaitGroup
			for _, s := range states {
				if s.current == target {
					// This particular resource is already at target; report its
					// terminal state without a ticker (§4.2 partial-immediate).
					UpdateServiceState(serviceId, StatusSuccessful, s.key, seatOutput(s.current), nil, nil, backendChans)
					continue
				}
				wg.Add(1)
				go func(s resourceState) {
					defer wg.Done()
					ticker := time.NewTicker(moveSeatStepPeriod())
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
						cur := seatState[s.seatKey]
						if cur < target {
							cur++
						} else if cur > target {
							cur--
						}
						seatState[s.seatKey] = cur
						seatMu.Unlock()

						if cur == target {
							UpdateServiceState(serviceId, StatusSuccessful, s.key, seatOutput(cur), nil, nil, backendChans)
							return
						}
						UpdateServiceState(serviceId, StatusOngoing, s.key, seatOutput(cur), nil, nil, backendChans)
					}
				}(s)
			}
			wg.Wait()
		},
	}
}

// ── ActivateMassage ──────────────────────────────────────────────────────────

// massageTypes are the supported MassageType values, matching the
// "ActivateMassage" capability tokens returned by getCapabilitiesBuiltin
// (roll, lumbar).
var massageTypes = map[string]bool{
	"roll":   true,
	"lumbar": true,
}

// activateMassageBuiltin simulates VehicleService.Seating.ActivateMassage.
// Without this builtin (the bug reported against PR #201), an invoke to
// ActivateMassage created an invocation that nothing ever drove via
// UpdateServiceState — like the general MoveSeat problem described at the top
// of this file — so it always ran into the timeout watchdog and finished
// FAILED after DefaultTimeout, even though nothing about the request was
// actually wrong.
//
//   - MassageType must be one of roll, lumbar (the two tokens GetCapabilities
//     advertises for ActivateMassage).
//   - Duration is a non-negative integer number of seconds. Duration == 0
//     completes immediately (SUCCESSFUL) — there is nothing to simulate.
//   - Otherwise the session runs for `Duration` simulated seconds — one tick
//     per moveSeatStepPeriod() interval, reusing MoveSeat's test-shrinkable
//     clock so tests stay fast — reporting completion percentage via the
//     "progress" field (VISSv3.3 §28) on each ONGOING tick, then SUCCESSFUL.
//
// ActivateMassage's Output iostruct only declares Status/ServiceId (both
// already conveyed by the envelope's top-level "status"/"serviceId" fields,
// per resources/ServiceSpecification-example.vspec), so unlike MoveSeat's
// Position there is no extra per-tick data to report as outdata.
//
// resourceKeys is handled exactly like moveSeatBuiltin: nil/empty for a
// single-resource invocation, or every addressed resource instance for a
// multiplexed one (ActivateMassage is multiplexed the same way MoveSeat is —
// §1), each driven independently via one goroutine per resource.
func activateMassageBuiltin(path string, resourceKeys []string, input map[string]interface{}) builtinDecision {
	massageType, _ := input["MassageType"].(string)
	if !massageTypes[massageType] {
		return builtinDecision{
			errNum: "400", errReason: "bad_request",
			errDesc: "MassageType must be one of: roll, lumbar",
		}
	}

	durStr, _ := input["Duration"].(string)
	duration, err := strconv.Atoi(strings.TrimSpace(durStr))
	if err != nil || duration < 0 {
		return builtinDecision{
			errNum: "400", errReason: "bad_request",
			errDesc: "Duration must be a non-negative integer number of seconds",
		}
	}

	if duration == 0 {
		// Nothing to simulate: succeed immediately, no invocation/session
		// created — matches moveSeatBuiltin's already-at-target fast path.
		return builtinDecision{immediate: StatusSuccessful}
	}

	keys := resourceKeys
	if len(keys) == 0 {
		keys = []string{""}
	}

	// Allow one tick per second plus a small buffer so the timeout watchdog
	// does not kill a massage session whose Duration exceeds DefaultTimeout
	// (e.g. the 300 s session in appclient_service_commands.txt).
	minDuration := time.Duration(duration+2) * moveSeatStepPeriod()

	return builtinDecision{
		minDuration: minDuration,
		run: func(serviceId string, backendChans []chan map[string]interface{}) {
			var wg sync.WaitGroup
			for _, key := range keys {
				wg.Add(1)
				go func(resourceKey string) {
					defer wg.Done()
					ticker := time.NewTicker(moveSeatStepPeriod())
					defer ticker.Stop()
					elapsed := 0
					for range ticker.C {
						// Stop promptly if the invocation was cancelled or removed.
						mu.Lock()
						_, alive := invocations[serviceId]
						mu.Unlock()
						if !alive {
							return
						}

						elapsed++
						if elapsed >= duration {
							UpdateServiceState(serviceId, StatusSuccessful, resourceKey, nil, nil, nil, backendChans)
							return
						}
						progress := elapsed * 100 / duration
						UpdateServiceState(serviceId, StatusOngoing, resourceKey, nil, nil, &progress, backendChans)
					}
				}(key)
			}
			wg.Wait()
		},
	}
}
