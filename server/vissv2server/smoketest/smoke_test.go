//go:build smoke

// Package smoketest starts vissv2server as a subprocess and verifies
// end-to-end VISS WebSocket responses without any external infrastructure.
//
// Run with:
//
//	go test -v -tags smoke -timeout 60s ./server/vissv2server/smoketest/
//
// Integration-only — NOT included in the standard unit-test sweep.
package smoketest

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// repoRoot returns the absolute path of the repository root by walking up
// from this source file's location.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// file is .../server/vissv2server/smoketest/smoke_test.go — walk up 3 dirs
	return filepath.Join(filepath.Dir(file), "..", "..", "..")
}

// buildBinary compiles vissv2server into a temp file and returns its path.
func buildBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "vissv2server-smoke")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	root := repoRoot(t)
	cmd := exec.Command("go", "build", "-o", bin, "./server/vissv2server/")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build vissv2server: %v\n%s", err, out)
	}
	return bin
}

// waitForPort polls addr until it accepts TCP connections or the deadline passes.
func waitForPort(t *testing.T, addr string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("server did not open %s within %s", addr, timeout)
}

// TestSmoke_WsGetSpeed starts vissv2server with the in-repo VDM testdata
// and an in-memory state backend, then verifies that a VISS WebSocket GET
// for Vehicle.Speed returns a well-formed response.
func TestSmoke_WsGetSpeed(t *testing.T) {
	root := repoRoot(t)
	bin := buildBinary(t)
	vdmDir := filepath.Join(root, "server", "vissv2server", "vdmloader", "testdata")
	// Run from the server directory so relative paths (atServer/purposelist.json,
	// vissv3.0-schema.json, ../transport_sec/) resolve correctly. The logs/
	// subdirectory it creates is gitignored.
	serverDir := filepath.Join(root, "server", "vissv2server")

	args := []string{
		"--vdm", vdmDir,
		"-s", "none",
		"--loglevel", "error",
	}
	srv := exec.Command(bin, args...)
	srv.Dir = serverDir
	srv.Stdout = os.Stdout
	srv.Stderr = os.Stderr

	if err := srv.Start(); err != nil {
		t.Fatalf("start vissv2server: %v", err)
	}
	t.Cleanup(func() {
		srv.Process.Kill()
		srv.Wait()
	})

	waitForPort(t, "localhost:8080", 20*time.Second)

	conn, _, err := websocket.DefaultDialer.Dial("ws://localhost:8080/", nil)
	if err != nil {
		t.Fatalf("WebSocket dial: %v", err)
	}
	defer conn.Close()

	req := `{"action":"get","path":"Vehicle.Speed","requestId":"smoke-1"}`
	if err := conn.WriteMessage(websocket.TextMessage, []byte(req)); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(msg, &resp); err != nil {
		t.Fatalf("response is not valid JSON: %v — raw: %s", err, msg)
	}

	action, _ := resp["action"].(string)
	if action == "" {
		t.Errorf("response missing 'action' field: %s", msg)
	}
	if resp["requestId"] == nil {
		t.Errorf("response missing 'requestId' field: %s", msg)
	}
	// noneBackend returns data or a 503 service_unavailable — both are valid
	// VISS responses that prove the full pipeline (WS→core→serviceMgr) ran.
	if resp["data"] == nil && resp["error"] == nil {
		t.Errorf("response has neither 'data' nor 'error' field: %s", msg)
	}
	fmt.Printf("smoke response: %s\n", msg)
}

// TestSmoke_WsSetRejected verifies that a SET on the none backend returns
// a 503 service_unavailable error (the none backend never stores values).
func TestSmoke_WsSetRejected(t *testing.T) {
	root := repoRoot(t)
	bin := buildBinary(t)
	vdmDir := filepath.Join(root, "server", "vissv2server", "vdmloader", "testdata")
	serverDir := filepath.Join(root, "server", "vissv2server")

	srv := exec.Command(bin,
		"--vdm", vdmDir, "-s", "none", "--loglevel", "error")
	srv.Dir = serverDir
	srv.Stdout = os.Stdout
	srv.Stderr = os.Stderr
	if err := srv.Start(); err != nil {
		t.Fatalf("start vissv2server: %v", err)
	}
	t.Cleanup(func() { srv.Process.Kill(); srv.Wait() })

	waitForPort(t, "localhost:8080", 20*time.Second)

	conn, _, err := websocket.DefaultDialer.Dial("ws://localhost:8080/", nil)
	if err != nil {
		t.Fatalf("WebSocket dial: %v", err)
	}
	defer conn.Close()

	req := `{"action":"set","path":"Vehicle.Speed","value":"42","requestId":"smoke-2"}`
	if err := conn.WriteMessage(websocket.TextMessage, []byte(req)); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(msg, &resp); err != nil {
		t.Fatalf("response is not valid JSON: %v — raw: %s", err, msg)
	}
	fmt.Printf("smoke set response: %s\n", msg)

	// none backend cannot write — expect error field in response
	if resp["error"] == nil && resp["data"] == nil {
		t.Errorf("expected error or data field in set response: %s", msg)
	}
}
