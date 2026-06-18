/**
* (C) 2026 Matt Jones
*
* Unit tests for seatService (vissServiceMgr/example).
*
* handleMoveSeat drives the simulation via ctx.ReportProgress, which routes
* through the vissServiceSDK's unexported svc.sendJSON and so needs a live
* fake-server connection (covered end-to-end in vissServiceSDK_test.go, and the
* identical simulation logic is unit-tested against the server built-in in
* vissServiceMgr's builtinService_test.go). The request validation and stepping
* math are factored into pure helpers and tested directly below.
*
* Integration-only entry points — NOT unit-tested here:
*
*   main          — registers with live VISS server on localhost:8300
*   handleMoveSeat — calls ctx.ReportProgress which needs a live conn
**/
package main

import "testing"

func TestParseMoveSeatRequest(t *testing.T) {
	cases := []struct {
		name       string
		movement   string
		position   string
		wantTarget int
		wantErr    bool
	}{
		{"valid longitudinal", "longitudinal", "40", 40, false},
		{"valid vertical zero", "vertical", "0", 0, false},
		{"valid recline max", "recline", "100", 100, false},
		{"valid with whitespace", "longitudinal", " 55 ", 55, false},
		{"unknown movement type", "diagonal", "40", 0, true},
		{"empty movement type", "", "40", 0, true},
		{"position too high", "longitudinal", "101", 0, true},
		{"position negative", "longitudinal", "-1", 0, true},
		{"position non-numeric", "longitudinal", "abc", 0, true},
		{"position empty", "longitudinal", "", 0, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			target, errMsg := parseMoveSeatRequest(c.movement, c.position)
			if c.wantErr {
				if errMsg == "" {
					t.Fatalf("expected error for movement=%q position=%q", c.movement, c.position)
				}
				return
			}
			if errMsg != "" {
				t.Fatalf("unexpected error: %s", errMsg)
			}
			if target != c.wantTarget {
				t.Errorf("target = %d, want %d", target, c.wantTarget)
			}
		})
	}
}

func TestStepToward(t *testing.T) {
	cases := []struct{ current, target, want int }{
		{45, 46, 46}, // increment
		{45, 44, 44}, // decrement
		{40, 40, 40}, // already there
		{0, 1, 1},
		{100, 99, 99},
	}
	for _, c := range cases {
		if got := stepToward(c.current, c.target); got != c.want {
			t.Errorf("stepToward(%d,%d) = %d, want %d", c.current, c.target, got, c.want)
		}
	}
}

// TestStepTowardConverges checks that repeated stepping always reaches the
// target in exactly |target-current| steps, never overshooting.
func TestStepTowardConverges(t *testing.T) {
	for _, tc := range [][2]int{{0, 40}, {40, 0}, {0, 100}, {100, 0}, {37, 37}} {
		current, target := tc[0], tc[1]
		want := target - current
		if want < 0 {
			want = -want
		}
		steps := 0
		for current != target {
			current = stepToward(current, target)
			steps++
			if steps > 200 {
				t.Fatalf("did not converge from %d to %d", tc[0], tc[1])
			}
		}
		if steps != want {
			t.Errorf("converging %d->%d took %d steps, want %d", tc[0], tc[1], steps, want)
		}
	}
}

func TestOutput(t *testing.T) {
	out := output(42)
	if out["Position"] != "42" {
		t.Errorf("output Position = %v, want \"42\"", out["Position"])
	}
}
