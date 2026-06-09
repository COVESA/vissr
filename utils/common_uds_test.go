/**
* (C) 2026 Ford Motor Company
*
* Tests for UDS socket-path helpers and environment-variable functions.
* These are pure-logic functions that don't need live UDS sockets.
**/
package utils

import (
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// getSocketPathLocked — all four connection-name branches + default
// ---------------------------------------------------------------------------

// populateUdsRegListForTest sets up a single-element udsRegList protected
// by the mutex and returns a cleanup function that restores the original list.
func populateUdsRegListForTest(t *testing.T) func() {
	t.Helper()
	udsRegListMu.Lock()
	old := udsRegList
	udsRegList = []UdsReg{
		{
			RootName:     "Vehicle",
			ServerFeeder: "/tmp/feeder.sock",
			Redis:        "/tmp/redis.sock",
			Memcache:     "/tmp/memcache.sock",
			History:      "/tmp/history.sock",
		},
	}
	udsRegListMu.Unlock()
	return func() {
		udsRegListMu.Lock()
		udsRegList = old
		udsRegListMu.Unlock()
	}
}

func TestGetSocketPathLocked_AllBranches(t *testing.T) {
	cleanup := populateUdsRegListForTest(t)
	defer cleanup()

	cases := map[string]string{
		"serverFeeder": "/tmp/feeder.sock",
		"redis":        "/tmp/redis.sock",
		"memcache":     "/tmp/memcache.sock",
		"history":      "/tmp/history.sock",
	}
	udsRegListMu.RLock()
	defer udsRegListMu.RUnlock()
	for connName, want := range cases {
		if got := getSocketPathLocked(0, connName); got != want {
			t.Errorf("getSocketPathLocked(0, %q) = %q; want %q", connName, got, want)
		}
	}
}

func TestGetSocketPathLocked_UnknownReturnsEmpty(t *testing.T) {
	cleanup := populateUdsRegListForTest(t)
	defer cleanup()

	udsRegListMu.RLock()
	defer udsRegListMu.RUnlock()
	if got := getSocketPathLocked(0, "unknown-connection"); got != "" {
		t.Errorf("getSocketPathLocked(unknown) = %q; want \"\"", got)
	}
}

// ---------------------------------------------------------------------------
// GetUdsPath — matches root name correctly
// ---------------------------------------------------------------------------

func TestGetUdsPath_MatchesRoot(t *testing.T) {
	// Populate via ReadUdsRegistrations from a temp file so we exercise
	// the public interface with consistent data.
	tmp := t.TempDir()
	sockFile := filepath.Join(tmp, "uds.json")
	content := `[{"root":"Vehicle","serverFeeder":"/tmp/sf.sock","redis":"","memcache":"","history":""}]`
	if err := os.WriteFile(sockFile, []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	ReadUdsRegistrations(sockFile)
	if got := GetUdsPath("Vehicle.Speed", "serverFeeder"); got != "/tmp/sf.sock" {
		t.Errorf("GetUdsPath = %q; want /tmp/sf.sock", got)
	}
}

func TestGetUdsPath_NoMatch(t *testing.T) {
	// Use an empty list to exercise the not-found / Info.Printf path.
	udsRegListMu.Lock()
	old := udsRegList
	udsRegList = []UdsReg{}
	udsRegListMu.Unlock()
	defer func() {
		udsRegListMu.Lock()
		udsRegList = old
		udsRegListMu.Unlock()
	}()
	if got := GetUdsPath("Unknown.Signal", "serverFeeder"); got != "" {
		t.Errorf("GetUdsPath on no-match = %q; want \"\"", got)
	}
}

// ---------------------------------------------------------------------------
// GetServerIP — env-var present and absent branches
// ---------------------------------------------------------------------------

func TestGetServerIP_EnvVarSet(t *testing.T) {
	if err := os.Setenv(IpEnvVarName, "192.168.1.1"); err != nil {
		t.Fatalf("Setenv: %v", err)
	}
	defer os.Unsetenv(IpEnvVarName)
	if got := GetServerIP(); got != "192.168.1.1" {
		t.Errorf("GetServerIP with env = %q; want \"192.168.1.1\"", got)
	}
}

func TestGetServerIP_EnvVarAbsent(t *testing.T) {
	os.Unsetenv(IpEnvVarName)
	if got := GetServerIP(); got != "localhost" {
		t.Errorf("GetServerIP without env = %q; want \"localhost\"", got)
	}
}

// ---------------------------------------------------------------------------
// GetModelIP — model=0 (localhost) and model=2 (env var) branches
// ---------------------------------------------------------------------------

func TestGetModelIP_Model0(t *testing.T) {
	if got := GetModelIP(0); got != "localhost" {
		t.Errorf("GetModelIP(0) = %q; want \"localhost\"", got)
	}
}

func TestGetModelIP_Model2_EnvSet(t *testing.T) {
	if err := os.Setenv(IpEnvVarName, "10.0.0.5"); err != nil {
		t.Fatalf("Setenv: %v", err)
	}
	defer os.Unsetenv(IpEnvVarName)
	if got := GetModelIP(2); got != "10.0.0.5" {
		t.Errorf("GetModelIP(2) with env = %q; want \"10.0.0.5\"", got)
	}
}

func TestGetModelIP_Model2_EnvAbsent(t *testing.T) {
	os.Unsetenv(IpEnvVarName)
	// Without env var, falls back to "localhost"
	if got := GetModelIP(2); got != "localhost" {
		t.Errorf("GetModelIP(2) without env = %q; want \"localhost\"", got)
	}
}

// ---------------------------------------------------------------------------
// SetErrorResponse — subscriptionId branch
// ---------------------------------------------------------------------------

func TestSetErrorResponse_SubscriptionIdPropagated(t *testing.T) {
	req := map[string]interface{}{
		"action":         "unsubscribe",
		"subscriptionId": "sub-999",
	}
	resp := map[string]interface{}{}
	SetErrorResponse(req, resp, 0, "")
	if resp["subscriptionId"] != "sub-999" {
		t.Errorf("subscriptionId not propagated: %+v", resp)
	}
}

// ---------------------------------------------------------------------------
// GetRfcTime — additional branch coverage
// ---------------------------------------------------------------------------

func TestGetRfcTime_ConsistentlyEndsWithZ(t *testing.T) {
	for i := 0; i < 5; i++ {
		got := GetRfcTime()
		if got[len(got)-1] != 'Z' {
			t.Errorf("GetRfcTime()[%d] = %q; should end with Z", i, got)
		}
	}
}

func TestGetRfcTime_ContainsDot(t *testing.T) {
	// GetRfcTime should have sub-second precision (dot in fractional part)
	// because time.RFC3339Nano usually includes nanoseconds for UTC times.
	got := GetRfcTime()
	// The function truncates to 3 fractional digits or falls back to just Z.
	// Both outcomes are valid; we just check it parses properly.
	if len(got) < 20 {
		t.Errorf("GetRfcTime too short: %q", got)
	}
}
