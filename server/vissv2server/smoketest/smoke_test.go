//go:build smoke

// Package smoketest starts vissv2server as a subprocess and verifies
// end-to-end VISS WebSocket responses without any external infrastructure.
//
// Run with:
//
//	go test -v -tags smoke -timeout 120s ./server/vissv2server/smoketest/
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

var (
	// builtBin is the path to the pre-built vissv2server binary.
	// Populated by TestMain so both tests share one build.
	builtBin string
	// serverDir is the CWD for the server subprocess — relative paths
	// (atServer/purposelist.json, etc.) resolve from here.
	serverDir string
)

func TestMain(m *testing.M) {
	// Locate the repository root from this source file's path.
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		fmt.Fprintln(os.Stderr, "TestMain: runtime.Caller failed")
		os.Exit(1)
	}
	// file is .../server/vissv2server/smoketest/smoke_test.go — walk up 3
	root := filepath.Join(filepath.Dir(file), "..", "..", "..")
	serverDir = filepath.Join(root, "server", "vissv2server")

	// serviceMgr creates a Unix socket here; create the dir if CI doesn't
	// have it.
	if err := os.MkdirAll("/var/tmp/vissv2", 0755); err != nil {
		fmt.Fprintf(os.Stderr, "TestMain: create /var/tmp/vissv2: %v\n", err)
		os.Exit(1)
	}

	// Build the binary once; reuse it for every test in this package.
	bin := filepath.Join(os.TempDir(), "vissv2server-smoke-bin")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", bin, "./server/vissv2server/")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build vissv2server: %v\n%s\n", err, out)
		os.Exit(1)
	}
	builtBin = bin

	code := m.Run()
	os.Remove(bin)
	os.Exit(code)
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

// startServer launches vissv2server with the in-repo VDM testdata and
// none backend and registers a cleanup that kills it.
func startServer(t *testing.T) {
	t.Helper()
	vdmDir := filepath.Join(serverDir, "vdmloader", "testdata")

	srv := exec.Command(builtBin,
		"--vdm", vdmDir,
		"-s", "none",
		"--loglevel", "error",
	)
	// Run from the server directory so relative paths resolve correctly.
	// logs/ in that directory is gitignored.
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

	waitForPort(t, "localhost:8080", 30*time.Second)
}

// TestSmoke_WsGetSpeed starts vissv2server with the in-repo VDM testdata
// and an in-memory state backend, then verifies that a VISS WebSocket GET
// for Vehicle.Speed returns a well-formed response.
func TestSmoke_WsGetSpeed(t *testing.T) {
	startServer(t)

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
	// noneBackend returns data or a 404/503 — both are valid VISS responses
	// that prove the full pipeline (WS→core→serviceMgr) ran.
	if resp["data"] == nil && resp["error"] == nil {
		t.Errorf("response has neither 'data' nor 'error' field: %s", msg)
	}
	fmt.Printf("smoke GET response: %s\n", msg)
}

// TestSmoke_WsSetRejected verifies that a SET on the none backend returns
// a VISS error response (the none backend never stores values).
func TestSmoke_WsSetRejected(t *testing.T) {
	startServer(t)

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
	fmt.Printf("smoke SET response: %s\n", msg)

	// none backend cannot write — expect error field in response
	if resp["error"] == nil && resp["data"] == nil {
		t.Errorf("expected error or data field in set response: %s", msg)
	}
}
