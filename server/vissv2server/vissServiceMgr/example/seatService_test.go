/**
* (C) 2026 Matt Jones
*
* Unit tests for seatService (vissServiceMgr/example).
*
* handleMoveSeat calls ctx.ReportProgress which routes through the
* vissServiceSDK's unexported svc.sendJSON. Testing it end-to-end
* requires a live fake-server fixture identical to the one in
* vissServiceSDK_test.go; that coverage is provided there.
*
* Integration-only entry points — NOT unit-tested here:
*
*   main          — registers with live VISS server on localhost:8300
*   handleMoveSeat — calls ctx.ReportProgress which needs a live conn
**/
package main

import "testing"

// TestPackageCompiles is a compile-only sentinel — confirms that the
// example binary builds cleanly without importing the live server.
func TestPackageCompiles(t *testing.T) {}
