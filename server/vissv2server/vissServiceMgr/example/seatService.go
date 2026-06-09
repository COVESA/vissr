/**
* (C) 2026 Ford Motor Company
*
* VISSv3.3-alpha — Example Service: VehicleService.Seating.MoveSeat
*
* Demonstrates how to implement a VISS service procedure using the
* vissServiceSDK. This example simulates moving a vehicle seat, reporting
* position updates every 500 ms until the target position is reached.
*
* Run as a standalone binary alongside the VISS server:
*
*   go run ./server/vissv2server/vissServiceMgr/example/seatService.go
*
* The service registers with the server on localhost:8300 (the default
* ServiceRegPort) and handles invoke requests for MoveSeat.
**/

package main

import (
	"log"
	"strconv"
	"time"

	vissServiceSDK "github.com/covesa/vissr/server/vissv2server/vissServiceSDK"
)

func main() {
	svc, err := vissServiceSDK.NewService("localhost:8300", "VehicleService.Seating.MoveSeat").
		WithInput("SeatId", "string").
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

// handleMoveSeat simulates moving a vehicle seat from its current position
// to the requested target position, reporting progress every 500 ms.
func handleMoveSeat(ctx *vissServiceSDK.InvokeContext) {
	targetStr, _ := ctx.Input["Position"].(string)
	target, err := strconv.Atoi(targetStr)
	if err != nil || target < 0 || target > 100 {
		ctx.ReportProgress("FAILED", nil) //nolint:errcheck
		return
	}

	// Simulate current position starting at 0.
	current := 0
	step := 10
	if target < current {
		step = -10
	}

	for current != target {
		time.Sleep(500 * time.Millisecond)
		current += step
		if step > 0 && current > target {
			current = target
		} else if step < 0 && current < target {
			current = target
		}
		output := map[string]interface{}{"Position": strconv.Itoa(current)}
		if current == target {
			ctx.ReportProgress("SUCCESSFUL", output) //nolint:errcheck
			return
		}
		ctx.ReportProgress("ONGOING", output) //nolint:errcheck
	}

	// Already at target.
	ctx.ReportProgress("SUCCESSFUL", map[string]interface{}{"Position": strconv.Itoa(current)}) //nolint:errcheck
}
