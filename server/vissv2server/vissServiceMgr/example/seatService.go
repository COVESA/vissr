/**
* (C) 2026 Ford Motor Company
*
* SPDX-License-Identifier: MPL-2.0
*
* VISSv3.3-alpha — Example Service: VehicleService.Seating.MoveSeat
*
* Reference implementation of the MoveSeat service procedure using the
* vissServiceSDK. It simulates real seat-actuator behaviour:
*
*   - A Position state (0-100 %) is kept per MovementType. The supported
*     MovementTypes are "longitudinal", "vertical" and "recline". State is
*     initialised to 0 at start-up (no persistence across lifetimes).
*   - When the requested Position differs from the saved state, the state is
*     stepped one percentage point per second towards the request, emitting a
*     monitoring event per step, until it reaches the target -> SUCCESSFUL.
*   - When the requested Position already equals the saved state, the response
*     is SUCCESSFUL with no events.
*   - When the requested Position is outside [0,100] (or non-numeric, or the
*     MovementType is unknown), a FAILED error response is returned and no
*     events are issued.
*
* The same simulation is built into the server (vissServiceMgr) so the demo
* works whether or not this external service process is running; this binary
* is the reference for implementing a real service.
*
* Run as a standalone binary alongside the VISS server:
*
*   go run ./server/vissv2server/vissServiceMgr/example/seatService.go
*
* It registers with the server on localhost:8300 (the default ServiceRegPort).
**/

package main

import (
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	vissServiceSDK "github.com/covesa/vissr/server/vissv2server/vissServiceSDK"
)

// stepPeriod is the interval between one-percentage-point position changes.
var stepPeriod = time.Second

var supportedMovementTypes = map[string]bool{
	"longitudinal": true,
	"vertical":     true,
	"recline":      true,
}

// positions holds the simulated seat position per MovementType, initialised to
// 0 and guarded by mu because invocations are handled concurrently.
var (
	mu        sync.Mutex
	positions = map[string]int{
		"longitudinal": 0,
		"vertical":     0,
		"recline":      0,
	}
)

func main() {
	svc, err := vissServiceSDK.NewService("localhost:8300", "VehicleService.Seating.MoveSeat").
		WithInput("MovementType", "string").
		WithInput("Position", "uint8").
		WithOutput("Position", "uint8").
		OnInvoke(handleMoveSeat).
		Register()
	if err != nil {
		log.Fatalf("seatService: register failed: %v", err)
	}
	defer svc.Close()

	log.Println("seatService: registered, waiting for invocations...")
	svc.Run()
}

// handleMoveSeat implements the simulation described in the file header.
func handleMoveSeat(ctx *vissServiceSDK.InvokeContext) {
	movementType, _ := ctx.Input["MovementType"].(string)
	posStr, _ := ctx.Input["Position"].(string)
	target, errMsg := parseMoveSeatRequest(movementType, posStr)
	if errMsg != "" {
		ctx.ReportError("400", errMsg, nil) //nolint:errcheck
		return
	}

	mu.Lock()
	current := positions[movementType]
	mu.Unlock()

	if current == target {
		// Already at the requested position: SUCCESSFUL, no events.
		ctx.ReportProgress("SUCCESSFUL", output(current)) //nolint:errcheck
		return
	}

	ticker := time.NewTicker(stepPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done(): // server cancelled the invocation (§26)
			return
		case <-ticker.C:
			mu.Lock()
			cur := stepToward(positions[movementType], target)
			positions[movementType] = cur
			mu.Unlock()

			if cur == target {
				ctx.ReportProgress("SUCCESSFUL", output(cur)) //nolint:errcheck
				return
			}
			ctx.ReportProgress("ONGOING", output(cur)) //nolint:errcheck
		}
	}
}

// parseMoveSeatRequest validates the MovementType and Position inputs. It
// returns the target position, or a non-empty error message describing why the
// request is rejected (unknown MovementType, or Position non-numeric / outside
// [0,100]).
func parseMoveSeatRequest(movementType, posStr string) (target int, errMsg string) {
	if !supportedMovementTypes[movementType] {
		return 0, "MovementType must be one of: longitudinal, vertical, recline"
	}
	target, err := strconv.Atoi(strings.TrimSpace(posStr))
	if err != nil || target < 0 || target > 100 {
		return 0, "Position must be an integer percentage between 0 and 100"
	}
	return target, ""
}

// stepToward returns current moved one percentage point towards target.
func stepToward(current, target int) int {
	switch {
	case current < target:
		return current + 1
	case current > target:
		return current - 1
	default:
		return current
	}
}

func output(position int) map[string]interface{} {
	return map[string]interface{}{"Position": strconv.Itoa(position)}
}
