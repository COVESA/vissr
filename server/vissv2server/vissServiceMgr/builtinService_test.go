/**
* (C) 2026 Matt Jones
*
* Unit + integration tests for the built-in MoveSeat simulation.
**/
package vissServiceMgr

import (
	"testing"
	"time"
)

func resetSeatState() {
	seatMu.Lock()
	seatState = map[string]int{}
	seatMu.Unlock()
}

// shrinkStepPeriod speeds up the simulation for tests and restores it after.
func shrinkStepPeriod(t *testing.T, d time.Duration) {
	t.Helper()
	old := moveSeatStepPeriod
	moveSeatStepPeriod = d
	t.Cleanup(func() { moveSeatStepPeriod = old })
}

// ---- procedureName ---------------------------------------------------------

func TestProcedureName(t *testing.T) {
	cases := map[string]string{
		"VehicleService.Seating.Row1.DriverSide.MoveSeat": "MoveSeat",
		"VehicleService.Seating.MoveSeat":                 "MoveSeat",
		"MoveSeat":                                        "MoveSeat",
		"":                                                "",
	}
	for in, want := range cases {
		if got := procedureName(in); got != want {
			t.Errorf("procedureName(%q) = %q, want %q", in, got, want)
		}
	}
}

// ---- instance-path resolution ---------------------------------------------

func TestIsInstanceGeneralization(t *testing.T) {
	split := func(s string) []string {
		out := []string{}
		cur := ""
		for _, r := range s {
			if r == '.' {
				out = append(out, cur)
				cur = ""
			} else {
				cur += string(r)
			}
		}
		return append(out, cur)
	}
	cases := []struct {
		tmpl, inst string
		want       bool
	}{
		{"A.B.MoveSeat", "A.B.Row1.DriverSide.MoveSeat", true},
		{"A.MoveSeat", "A.B.Row1.MoveSeat", true},
		{"A.B.MoveSeat", "A.B.MoveSeat", true},   // equal is trivially a subsequence
		{"X.B.MoveSeat", "A.B.Row1.MoveSeat", false}, // different root
		{"A.B.Other", "A.B.Row1.MoveSeat", false},    // different procedure
		{"A.C.MoveSeat", "A.B.Row1.MoveSeat", false},  // middle segment not present
	}
	for _, c := range cases {
		if got := isInstanceGeneralization(split(c.tmpl), split(c.inst)); got != c.want {
			t.Errorf("isInstanceGeneralization(%q,%q) = %v, want %v", c.tmpl, c.inst, got, c.want)
		}
	}
}

func TestResolveRegistration_ExactAndInstance(t *testing.T) {
	regMu.Lock()
	registrations = map[string]*serviceConn{}
	exact := &serviceConn{path: "A.B.Row1.MoveSeat"}
	tmpl := &serviceConn{path: "A.B.MoveSeat"}
	registrations["A.B.Row1.MoveSeat"] = exact
	registrations["A.B.MoveSeat"] = tmpl
	regMu.Unlock()
	t.Cleanup(func() {
		regMu.Lock()
		registrations = map[string]*serviceConn{}
		regMu.Unlock()
	})

	if got := resolveRegistration("A.B.Row1.MoveSeat"); got != exact {
		t.Errorf("exact path should match the exact registration")
	}
	// Instance path with no exact match resolves to the template registration.
	if got := resolveRegistration("A.B.Row2.PassengerSide.MoveSeat"); got != tmpl {
		t.Errorf("instance path should resolve to the template registration, got %v", got)
	}
	if got := resolveRegistration("A.B.Row2.Lighting"); got != nil {
		t.Errorf("unrelated path should not resolve, got %v", got)
	}
}

// ---- moveSeatBuiltin decision logic ---------------------------------------

func TestMoveSeatBuiltin_ValidationErrors(t *testing.T) {
	resetSeatState()
	cases := []struct {
		name  string
		input map[string]interface{}
	}{
		{"unknown movement type", map[string]interface{}{"MovementType": "diagonal", "Position": "40"}},
		{"missing movement type", map[string]interface{}{"Position": "40"}},
		{"position too high", map[string]interface{}{"MovementType": "longitudinal", "Position": "101"}},
		{"position negative", map[string]interface{}{"MovementType": "longitudinal", "Position": "-5"}},
		{"position non-numeric", map[string]interface{}{"MovementType": "longitudinal", "Position": "x"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := moveSeatBuiltin("VehicleService.Seating.MoveSeat", c.input)
			if d.errNum != "400" {
				t.Errorf("want errNum 400, got %q (decision %+v)", d.errNum, d)
			}
			if d.run != nil || d.immediate != "" {
				t.Errorf("error decision must not move or complete: %+v", d)
			}
		})
	}
}

func TestMoveSeatBuiltin_AlreadyAtTarget(t *testing.T) {
	resetSeatState()
	path := "VehicleService.Seating.Row1.DriverSide.MoveSeat"
	seatMu.Lock()
	seatState[seatKey(path, "longitudinal")] = 40
	seatMu.Unlock()

	d := moveSeatBuiltin(path, map[string]interface{}{"MovementType": "longitudinal", "Position": "40"})
	if d.immediate != StatusSuccessful {
		t.Fatalf("want immediate SUCCESSFUL, got %q", d.immediate)
	}
	if d.run != nil {
		t.Error("already-at-target must not start a driver (no events)")
	}
	if d.outdata["Position"] != "40" {
		t.Errorf("outdata Position = %v, want 40", d.outdata["Position"])
	}
}

func TestMoveSeatBuiltin_MovingReturnsRunner(t *testing.T) {
	resetSeatState()
	shrinkStepPeriod(t, time.Second) // keep default for the minDuration assertion
	d := moveSeatBuiltin("VehicleService.Seating.MoveSeat",
		map[string]interface{}{"MovementType": "vertical", "Position": "40"})
	if d.run == nil {
		t.Fatal("moving request must return a driver")
	}
	if d.immediate != "" || d.errNum != "" {
		t.Errorf("moving request must not be immediate/error: %+v", d)
	}
	// 0->40 is 40 steps; deadline budget must exceed DefaultTimeout so the
	// watchdog does not kill the move (the bug Ulf saw at ~30 s).
	if d.minDuration <= DefaultTimeout {
		t.Errorf("minDuration %v should exceed DefaultTimeout %v for a 40-step move", d.minDuration, DefaultTimeout)
	}
}

// TestMoveSeatBuiltin_DriverStepsToTarget runs the async driver against a
// seeded invocation + an "all"-filter session (so every ONGOING update is
// delivered) and verifies the event stream Ulf specified: one event per
// percentage point, each with outdata.output.Position, ending SUCCESSFUL.
func TestMoveSeatBuiltin_DriverStepsToTarget(t *testing.T) {
	resetState()
	resetSeatState()
	shrinkStepPeriod(t, 2*time.Millisecond)

	const sid = "drv-1"
	const path = "VehicleService.Seating.Row1.DriverSide.MoveSeat"
	mu.Lock()
	invocations[sid] = &invocationState{serviceId: sid, path: path, status: StatusOngoing, startedAt: time.Now()}
	sessions["sess-1"] = &monitorSession{sessionId: "sess-1", serviceId: sid, path: path, filterKind: "all", routerIndex: 0}
	mu.Unlock()

	bc := make(chan map[string]interface{}, 64)
	bcs := []chan map[string]interface{}{bc}

	d := moveSeatBuiltin(path, map[string]interface{}{"MovementType": "longitudinal", "Position": "5"})
	if d.run == nil {
		t.Fatal("expected a driver")
	}
	done := make(chan struct{})
	go func() { d.run(sid, bcs); close(done) }()

	var statuses []string
	var lastPos string
	timeout := time.After(2 * time.Second)
	for {
		select {
		case ev := <-bc:
			if ev["action"] != "monitoring" {
				t.Errorf("event action = %v, want monitoring", ev["action"])
			}
			if ev["path"] != path {
				t.Errorf("event path = %v, want %v", ev["path"], path)
			}
			if ev["serviceId"] != "sess-1" {
				t.Errorf("event serviceId = %v, want sess-1 (the session id)", ev["serviceId"])
			}
			outdata, ok := ev["outdata"].(map[string]interface{})
			if !ok {
				t.Fatalf("event missing outdata: %v", ev)
			}
			out, ok := outdata["output"].(map[string]interface{})
			if !ok {
				t.Fatalf("outdata missing output: %v", outdata)
			}
			if outdata["ts"] == nil {
				t.Error("outdata missing ts")
			}
			lastPos, _ = out["Position"].(string)
			statuses = append(statuses, ev["status"].(string))
			if ev["status"] == string(StatusSuccessful) {
				goto finished
			}
		case <-timeout:
			t.Fatalf("driver did not complete; statuses so far=%v", statuses)
		}
	}
finished:
	<-done
	// 0->5: four ONGOING events then one SUCCESSFUL.
	if len(statuses) != 5 {
		t.Errorf("want 5 events, got %d (%v)", len(statuses), statuses)
	}
	for i := 0; i < len(statuses)-1; i++ {
		if statuses[i] != string(StatusOngoing) {
			t.Errorf("event %d status = %q, want ONGOING", i, statuses[i])
		}
	}
	if lastPos != "5" {
		t.Errorf("final Position = %q, want 5", lastPos)
	}
	seatMu.Lock()
	if seatState[seatKey(path, "longitudinal")] != 5 {
		t.Errorf("saved state = %d, want 5", seatState[seatKey(path, "longitudinal")])
	}
	seatMu.Unlock()
	// Terminal removes the invocation.
	mu.Lock()
	_, alive := invocations[sid]
	mu.Unlock()
	if alive {
		t.Error("invocation should be removed after SUCCESSFUL")
	}
}

// TestMoveSeatBuiltin_DriverStopsWhenInvocationRemoved verifies the driver
// exits promptly when the invocation is cancelled/removed (the cancel path).
func TestMoveSeatBuiltin_DriverStopsWhenInvocationRemoved(t *testing.T) {
	resetState()
	resetSeatState()
	shrinkStepPeriod(t, 5*time.Millisecond)

	const sid = "drv-2"
	const path = "VehicleService.Seating.MoveSeat"
	mu.Lock()
	invocations[sid] = &invocationState{serviceId: sid, path: path, status: StatusOngoing, startedAt: time.Now()}
	mu.Unlock()

	bc := make(chan map[string]interface{}, 64)
	d := moveSeatBuiltin(path, map[string]interface{}{"MovementType": "recline", "Position": "100"})
	done := make(chan struct{})
	go func() { d.run(sid, []chan map[string]interface{}{bc}); close(done) }()

	// Remove the invocation as a cancel would, then the driver must return.
	time.Sleep(15 * time.Millisecond)
	mu.Lock()
	delete(invocations, sid)
	mu.Unlock()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("driver did not stop after invocation removed")
	}
}

func TestMoveSeatBuiltin_PerMovementTypeIndependent(t *testing.T) {
	resetSeatState()
	path := "VehicleService.Seating.MoveSeat"
	seatMu.Lock()
	seatState[seatKey(path, "longitudinal")] = 30
	seatMu.Unlock()

	// vertical is still 0, so a request for vertical=0 is already-at-target,
	// independent of longitudinal=30.
	d := moveSeatBuiltin(path, map[string]interface{}{"MovementType": "vertical", "Position": "0"})
	if d.immediate != StatusSuccessful {
		t.Errorf("vertical state should be independent (0), got decision %+v", d)
	}
}

// ---- HandleInvoke integration (built-in path; needs the service tree) ------

func TestHandleInvoke_BuiltinMoveSeatOutOfRange(t *testing.T) {
	resetState()
	resetSeatState()
	loadVehicleServiceTree(t)
	t.Cleanup(stopServiceGoroutines)

	bc := make(chan map[string]interface{}, 8)
	bcs := []chan map[string]interface{}{bc}
	req := map[string]interface{}{
		"action":      "invoke",
		"path":        moveSeatPath,
		"input":       map[string]interface{}{"MovementType": "longitudinal", "Position": "150"},
		"requestId":   "8756",
		"routerIndex": 0,
		"RouterId":    "1?0",
	}
	HandleInvoke(req, bcs)

	select {
	case resp := <-bc:
		if _, ok := resp["error"]; !ok {
			t.Fatalf("out-of-range invoke should error, got %v", resp)
		}
		if resp["RouterId"] != "1?0" {
			t.Errorf("error response dropped RouterId: %v", resp)
		}
	case <-time.After(time.Second):
		t.Fatal("no response")
	}
	// No invocation/session created, so no events follow.
	mu.Lock()
	n := len(invocations)
	mu.Unlock()
	if n != 0 {
		t.Errorf("out-of-range request must not create an invocation, have %d", n)
	}
}

func TestHandleInvoke_BuiltinMoveSeatAlreadyThere(t *testing.T) {
	resetState()
	resetSeatState()
	loadVehicleServiceTree(t)
	t.Cleanup(stopServiceGoroutines)

	seatMu.Lock()
	seatState[seatKey(moveSeatPath, "longitudinal")] = 40
	seatMu.Unlock()

	bc := make(chan map[string]interface{}, 8)
	bcs := []chan map[string]interface{}{bc}
	req := map[string]interface{}{
		"action":      "invoke",
		"path":        moveSeatPath,
		"input":       map[string]interface{}{"MovementType": "longitudinal", "Position": "40"},
		"requestId":   "8756",
		"routerIndex": 0,
		"RouterId":    "1?0",
		"filter":      map[string]interface{}{"variant": "timebased", "parameter": map[string]interface{}{"period": "1000"}},
	}
	HandleInvoke(req, bcs)

	select {
	case resp := <-bc:
		if resp["status"] != string(StatusSuccessful) {
			t.Errorf("already-at-target should respond SUCCESSFUL, got %v", resp["status"])
		}
		if resp["RouterId"] != "1?0" {
			t.Errorf("response dropped RouterId: %v", resp)
		}
	case <-time.After(time.Second):
		t.Fatal("no response")
	}
	mu.Lock()
	n := len(invocations)
	mu.Unlock()
	if n != 0 {
		t.Errorf("already-at-target must not create an invocation, have %d", n)
	}
}

func TestHandleInvoke_BuiltinMoveSeatMovesAndCompletes(t *testing.T) {
	resetState()
	resetSeatState()
	shrinkStepPeriod(t, 3*time.Millisecond)
	loadVehicleServiceTree(t)
	t.Cleanup(stopServiceGoroutines)

	bc := make(chan map[string]interface{}, 64)
	bcs := []chan map[string]interface{}{bc}
	req := map[string]interface{}{
		"action":      "invoke",
		"path":        moveSeatPath,
		"input":       map[string]interface{}{"MovementType": "longitudinal", "Position": "3"},
		"requestId":   "8756",
		"routerIndex": 0,
		"RouterId":    "1?0",
		"filter":      map[string]interface{}{"variant": "all"},
	}
	HandleInvoke(req, bcs)

	// First the ONGOING invoke response, then monitoring events ending SUCCESSFUL.
	gotInvoke := false
	gotSuccess := false
	timeout := time.After(2 * time.Second)
	for !gotSuccess {
		select {
		case msg := <-bc:
			switch msg["action"] {
			case "invoke":
				gotInvoke = true
				if msg["status"] != string(StatusOngoing) {
					t.Errorf("invoke response status = %v, want ONGOING", msg["status"])
				}
			case "monitoring":
				if msg["status"] == string(StatusSuccessful) {
					gotSuccess = true
					od, _ := msg["outdata"].(map[string]interface{})
					out, _ := od["output"].(map[string]interface{})
					if out["Position"] != "3" {
						t.Errorf("final Position = %v, want 3", out["Position"])
					}
				}
			}
		case <-timeout:
			t.Fatal("did not reach SUCCESSFUL")
		}
	}
	if !gotInvoke {
		t.Error("never received the ONGOING invoke response")
	}
	seatMu.Lock()
	if seatState[seatKey(moveSeatPath, "longitudinal")] != 3 {
		t.Errorf("saved state = %d, want 3", seatState[seatKey(moveSeatPath, "longitudinal")])
	}
	seatMu.Unlock()
}
