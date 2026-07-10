/**
* (C) 2026 Ford Motor Company
*
* Coverage tests for the utils/managerhandlers.go pure-logic helpers
* and utils/logger.go utilities:
*   - RemoveInternalData — strips RouterId and returns clientId
*   - createRouterIdProperty — builds the RouterId JSON property
*   - AddRoutingForwardRequest — wraps a request with routing prefix
*   - CloseLogFile — safe even when Logfile is nil
*   - TrimLogFile — exercises the small-file (no-trim) branch
*   - getSocketPath — public entry-point wrapping getSocketPathLocked
**/
package utils

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// RemoveInternalData — strips RouterId and extracts clientId
// ---------------------------------------------------------------------------

func TestRemoveInternalData_HappyPath(t *testing.T) {
	// Canonical shape produced by AddRoutingForwardRequest:
	// {"RouterId":"0?1", "origin":"external", "action":"get","path":"Vehicle.Speed"}
	response := `{"RouterId":"0?1", "origin":"external", "action":"get","path":"Vehicle.Speed"}`
	trimmed, clientId := RemoveInternalData(response)
	if clientId != 1 {
		t.Errorf("clientId = %d; want 1", clientId)
	}
	if strings.Contains(trimmed, "RouterId") {
		t.Errorf("RouterId not removed: %q", trimmed)
	}
	if !strings.Contains(trimmed, "Vehicle.Speed") {
		t.Errorf("payload not preserved: %q", trimmed)
	}
}

func TestRemoveInternalData_ClientId0(t *testing.T) {
	response := `{"RouterId":"2?0", "action":"subscribe","path":"Vehicle.RPM"}`
	_, clientId := RemoveInternalData(response)
	if clientId != 0 {
		t.Errorf("clientId = %d; want 0", clientId)
	}
}

// A RouterId that is the last field (no trailing comma) is still valid: the
// clientId must be returned and the property removed.
func TestRemoveInternalData_RouterIdLastField(t *testing.T) {
	response := `{"action":"get","path":"Vehicle.Speed","RouterId":"3?7"}`
	trimmed, clientId := RemoveInternalData(response)
	if clientId != 7 {
		t.Errorf("clientId = %d; want 7", clientId)
	}
	if strings.Contains(trimmed, "RouterId") {
		t.Errorf("RouterId not removed: %q", trimmed)
	}
}

// Regression: a response that carries no RouterId must not panic. A service
// "monitoring" event emitted without a RouterId used to make routerIdStart
// == -2 and response[-2:] crash the whole server (reported against v3.2).
// RemoveInternalData must now return the response unchanged with clientId -1
// so the transport managers can drop it.
func TestRemoveInternalData_NoRouterId_NoPanic(t *testing.T) {
	// Exact shape from the crash report's server log.
	response := `{"action":"monitoring","path":"VehicleService.Seating.Row1.DriverSide.MoveSeat","serviceId":"769516","status":"ONGOING","ts":"2026-06-16T11:13:07Z"}`
	trimmed, clientId := RemoveInternalData(response)
	if clientId != -1 {
		t.Errorf("clientId = %d; want -1 for missing RouterId", clientId)
	}
	if trimmed != response {
		t.Errorf("response should be returned unchanged when no RouterId; got %q", trimmed)
	}
}

func TestRemoveInternalData_Malformed(t *testing.T) {
	cases := []struct {
		name     string
		response string
	}{
		{"empty", ``},
		{"routerId at index 0 (no opening quote)", `RouterId":"0?1","action":"get"}`},
		{"no question mark", `{"RouterId":"01", "action":"get"}`},
		{"no closing quote on clientId", `{"RouterId":"0?1, "action":"get"}`},
		{"non-numeric clientId", `{"RouterId":"0?abc", "action":"get"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			trimmed, clientId := RemoveInternalData(tc.response)
			if clientId != -1 {
				t.Errorf("clientId = %d; want -1 for malformed input", clientId)
			}
			if trimmed != tc.response {
				t.Errorf("malformed input should be returned unchanged; got %q", trimmed)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// createRouterIdProperty — internal helper
// ---------------------------------------------------------------------------

func TestCreateRouterIdProperty(t *testing.T) {
	got := createRouterIdProperty(3, 7)
	if !strings.Contains(got, "RouterId") {
		t.Errorf("missing RouterId: %q", got)
	}
	if !strings.Contains(got, "3?7") {
		t.Errorf("missing mgrId?clientId: %q", got)
	}
}

// ---------------------------------------------------------------------------
// AddRoutingForwardRequest — wraps request with RouterId prefix
// ---------------------------------------------------------------------------

func TestAddRoutingForwardRequest_ForwardsWithPrefix(t *testing.T) {
	ch := make(chan string, 1)
	req := `{"action":"get","path":"Vehicle.Speed"}`
	AddRoutingForwardRequest(req, 1, 0, ch)
	got := <-ch
	if !strings.Contains(got, "RouterId") {
		t.Errorf("RouterId prefix missing: %q", got)
	}
	if !strings.Contains(got, "origin") {
		t.Errorf("origin field missing: %q", got)
	}
	if !strings.Contains(got, "Vehicle.Speed") {
		t.Errorf("original payload missing: %q", got)
	}
}

// ---------------------------------------------------------------------------
// getSocketPath — public wrapper that acquires its own RLock
// ---------------------------------------------------------------------------

func TestGetSocketPath_AllConnections(t *testing.T) {
	cleanup := populateUdsRegListForTest(t)
	defer cleanup()

	cases := map[string]string{
		"serverFeeder": "/tmp/feeder.sock",
		"redis":        "/tmp/redis.sock",
		"memcache":     "/tmp/memcache.sock",
		"history":      "/tmp/history.sock",
	}
	for connName, want := range cases {
		if got := getSocketPath(0, connName); got != want {
			t.Errorf("getSocketPath(0, %q) = %q; want %q", connName, got, want)
		}
	}
}

func TestGetSocketPath_UnknownReturnsEmpty(t *testing.T) {
	cleanup := populateUdsRegListForTest(t)
	defer cleanup()
	if got := getSocketPath(0, "unknown"); got != "" {
		t.Errorf("getSocketPath(unknown) = %q; want \"\"", got)
	}
}

// ---------------------------------------------------------------------------
// CloseLogFile — safe when Logfile is nil
// ---------------------------------------------------------------------------

func TestCloseLogFile_NilLogfile(t *testing.T) {
	old := Logfile
	Logfile = nil
	defer func() { Logfile = old }()
	// Must not panic
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("CloseLogFile panicked with nil Logfile: %v", r)
		}
	}()
	CloseLogFile()
}

func TestCloseLogFile_OpenLogfile(t *testing.T) {
	tmp := t.TempDir()
	f, err := os.CreateTemp(tmp, "logtest*.log")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	old := Logfile
	Logfile = f
	defer func() { Logfile = old }()
	// Must not panic; file will be closed
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("CloseLogFile panicked: %v", r)
		}
	}()
	CloseLogFile()
}

// ---------------------------------------------------------------------------
// TrimLogFile — small file (< 10MB) → no-op
// ---------------------------------------------------------------------------

func TestTrimLogFile_SmallFile(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "small.log")
	f, err := os.Create(logPath)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer f.Close()
	// Write less than 10MB
	if _, err := f.WriteString("small log content\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Must not panic or do anything harmful on a small file
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("TrimLogFile panicked: %v", r)
		}
	}()
	TrimLogFile(f)
}

// ---------------------------------------------------------------------------
// InitLog — logFile=true branch (creates log file)
// ---------------------------------------------------------------------------

func TestInitLog_WithLogFile(t *testing.T) {
	tmp := t.TempDir()
	// Call with logFile=true: creates the log directory and file
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("InitLog panicked: %v", r)
		}
	}()
	InitLog("test-coverage.log", tmp, true, "error")
	// Verify the log file was created
	logPath := filepath.Join(tmp, "test-coverage.log")
	if _, err := os.Stat(logPath); err != nil {
		t.Errorf("log file not created: %v", err)
	}
	// Close it so we don't leak the file
	if Logfile != nil {
		Logfile.Close()
		Logfile = nil
	}
	// Re-init to stdout so subsequent tests still have working loggers
	InitLog("test.log", os.TempDir(), false, "error")
}
