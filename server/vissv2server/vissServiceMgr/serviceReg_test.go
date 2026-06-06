package vissServiceMgr

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"testing"
	"time"

	"github.com/covesa/vissr/utils"
)

func init() {
	// Initialise loggers so utils.Error/Info are non-nil for all tests.
	utils.InitLog("serviceReg_test.log", os.TempDir(), false, "error")
}

// serviceReg_test.go — unit tests for serviceReg.go.
//
// Functions that write to the live HIM forest (RegisterServiceTree,
// DeregisterServiceTree) are integration-only and are not unit-tested here.
// Tests for handleRegister cover the registration protocol logic using
// net.Pipe() so no real socket is needed.

// ---- helpers ---------------------------------------------------------------

// jsonRead wraps a net.Conn as a line-delimited JSON reader.
func jsonRead(c net.Conn) func() map[string]interface{} {
	sc := bufio.NewScanner(c)
	return func() map[string]interface{} {
		if !sc.Scan() {
			return nil
		}
		var m map[string]interface{}
		json.Unmarshal(sc.Bytes(), &m) //nolint:errcheck
		return m
	}
}

// jsonWrite wraps a net.Conn as a line-delimited JSON writer.
func jsonWrite(c net.Conn) func(v interface{}) {
	w := bufio.NewWriter(c)
	return func(v interface{}) {
		b, _ := json.Marshal(v)
		w.Write(b)
		w.WriteByte('\n')
		w.Flush() //nolint:errcheck
	}
}

// cleanReg removes a path from the registrations map (test teardown).
func cleanReg(path string) {
	regMu.Lock()
	delete(registrations, path)
	regMu.Unlock()
}

// ---- buildTreeFromSignature ------------------------------------------------

func TestBuildTreeFromSignature_BasicStructure(t *testing.T) {
	sig := map[string]interface{}{
		"input":  map[string]interface{}{"SeatId": "uint8", "Position": "uint8"},
		"output": map[string]interface{}{"Position": "uint8"},
	}
	root := buildTreeFromSignature("VehicleService.Seating.MoveSeat", sig)
	if root == nil {
		t.Fatal("buildTreeFromSignature returned nil")
	}
}

func TestBuildTreeFromSignature_NilSignature(t *testing.T) {
	root := buildTreeFromSignature("S.P", nil)
	if root == nil {
		t.Fatal("nil signature should still produce a root node")
	}
}

func TestBuildTreeFromSignature_EmptySignature(t *testing.T) {
	root := buildTreeFromSignature("S.P", map[string]interface{}{})
	if root == nil {
		t.Fatal("empty signature should produce a root node")
	}
}

func TestBuildTreeFromSignature_NonStringDatatypeDefaultsToString(t *testing.T) {
	// A malformed client that sends a numeric datatype should not produce an
	// empty datatype field — it should default to "string".
	sig := map[string]interface{}{
		"input": map[string]interface{}{"X": float64(42)}, // non-string value
	}
	// The function must not panic; it should use "string" as the default.
	root := buildTreeFromSignature("S.P", sig)
	if root == nil {
		t.Fatal("should not return nil for malformed datatype")
	}
}

func TestBuildTreeFromSignature_SingleSegmentPath(t *testing.T) {
	root := buildTreeFromSignature("P", map[string]interface{}{})
	if root == nil {
		t.Fatal("single-segment path should work")
	}
}

// ---- handleProgress --------------------------------------------------------

func TestHandleProgress_OngoingUpdate(t *testing.T) {
	resetState()
	ch := make(chan map[string]interface{}, 4)
	bcs := []chan map[string]interface{}{ch}

	invocations["prog-1"] = &invocationState{serviceId: "prog-1", path: "S.PP", status: StatusOngoing}
	sessions["ps-1"] = &monitorSession{sessionId: "ps-1", serviceId: "prog-1", routerIndex: 0, filterKind: "all"}

	handleProgress(map[string]interface{}{
		"sessionId": "prog-1",
		"status":    "ONGOING",
		"output":    map[string]interface{}{"Position": "25"},
	}, bcs)

	select {
	case event := <-ch:
		if event["status"] != "ONGOING" {
			t.Errorf("want ONGOING, got %v", event["status"])
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func TestHandleProgress_StructuredErrorExtracted(t *testing.T) {
	resetState()
	ch := make(chan map[string]interface{}, 4)
	bcs := []chan map[string]interface{}{ch}

	invocations["prog-2"] = &invocationState{serviceId: "prog-2", path: "S.PE2", status: StatusOngoing}
	sessions["ps-2"] = &monitorSession{sessionId: "ps-2", serviceId: "prog-2", routerIndex: 0, filterKind: "all"}

	handleProgress(map[string]interface{}{
		"sessionId": "prog-2",
		"status":    "FAILED",
		"error": map[string]interface{}{
			"code":    "MOTOR_STALL",
			"message": "seat motor stalled",
		},
	}, bcs)

	select {
	case event := <-ch:
		if event["status"] != "FAILED" {
			t.Errorf("want FAILED, got %v", event["status"])
		}
		errField, ok := event["error"].(map[string]interface{})
		if !ok {
			t.Fatalf("missing error field: %v", event)
		}
		if errField["code"] != "MOTOR_STALL" {
			t.Errorf("wrong code: %v", errField["code"])
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func TestHandleProgress_InvalidStatusDropped(t *testing.T) {
	resetState()
	ch := make(chan map[string]interface{}, 4)
	bcs := []chan map[string]interface{}{ch}

	handleProgress(map[string]interface{}{
		"sessionId": "prog-bad",
		"status":    "BOGUS_STATUS",
	}, bcs)

	select {
	case <-ch:
		t.Error("invalid status should be dropped")
	case <-time.After(30 * time.Millisecond):
	}
}

// ---- handleRegister (protocol-level via net.Pipe) --------------------------

// runHandleRegister launches handleRegister in a goroutine and returns a channel
// that receives the ack (the first JSON line written to the pipe's read end).
func runHandleRegister(t *testing.T, msg map[string]interface{}, bcs []chan map[string]interface{}) <-chan map[string]interface{} {
	t.Helper()
	srvConn, cliConn := net.Pipe()
	t.Cleanup(func() { srvConn.Close(); cliConn.Close() })

	writer := bufio.NewWriter(srvConn)
	ackCh := make(chan map[string]interface{}, 1)

	go func() {
		handleRegister(msg, srvConn, writer, bcs)
	}()

	go func() {
		sc := bufio.NewScanner(cliConn)
		if sc.Scan() {
			var m map[string]interface{}
			json.Unmarshal(sc.Bytes(), &m) //nolint:errcheck
			ackCh <- m
		} else {
			close(ackCh)
		}
	}()

	return ackCh
}

func TestHandleRegister_MissingPath(t *testing.T) {
	ackCh := runHandleRegister(t, map[string]interface{}{}, nil)
	select {
	case ack := <-ackCh:
		if reg, _ := ack["registered"].(bool); reg {
			t.Error("missing path should be rejected")
		}
		if reason, _ := ack["reason"].(string); reason == "" {
			t.Error("rejection should include a reason")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for ack")
	}
}

func TestHandleRegister_DuplicateRejected(t *testing.T) {
	const dupPath = "Test.DupProc"
	regMu.Lock()
	registrations[dupPath] = &serviceConn{path: dupPath}
	regMu.Unlock()
	defer cleanReg(dupPath)

	ackCh := runHandleRegister(t, map[string]interface{}{"path": dupPath}, nil)
	select {
	case ack := <-ackCh:
		if reg, _ := ack["registered"].(bool); reg {
			t.Error("duplicate path should be rejected")
		}
		if reason, _ := ack["reason"].(string); reason != "path already registered" {
			t.Errorf("unexpected reason: %q", reason)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for ack")
	}
}

// ---- forwardInvokeToService ------------------------------------------------

func TestForwardInvokeToService_NoRegistration(t *testing.T) {
	// Must not panic when no service is registered for the path.
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("panic: %v", r)
		}
	}()
	forwardInvokeToService("No.Such.Path", "sid-x", nil, "")
}

func TestForwardInvokeToService_DeliversMessage(t *testing.T) {
	const fwdPath = "Test.FwdProc"

	srvConn, cliConn := net.Pipe()
	defer srvConn.Close()
	defer cliConn.Close()

	sc := &serviceConn{
		path:   fwdPath,
		conn:   srvConn,
		writer: bufio.NewWriter(srvConn),
	}
	regMu.Lock()
	registrations[fwdPath] = sc
	regMu.Unlock()
	defer cleanReg(fwdPath)

	readCli := jsonRead(cliConn)
	msgCh := make(chan map[string]interface{}, 1)
	go func() { msgCh <- readCli() }()

	forwardInvokeToService(fwdPath, "sid-fwd", map[string]interface{}{"X": "1"}, "Bearer tok")

	select {
	case msg := <-msgCh:
		if msg == nil {
			t.Fatal("no message received by service")
		}
		if msg["action"] != "invoke" {
			t.Errorf("want action=invoke, got %v", msg["action"])
		}
		if msg["sessionId"] != "sid-fwd" {
			t.Errorf("wrong sessionId: %v", msg["sessionId"])
		}
		if msg["authorization"] != "Bearer tok" {
			t.Errorf("missing authorization: %v", msg["authorization"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for invoke message")
	}
}

func TestForwardInvokeToService_NoAuthWhenEmpty(t *testing.T) {
	const fwdPath2 = "Test.FwdProc2"

	srvConn, cliConn := net.Pipe()
	defer srvConn.Close()
	defer cliConn.Close()

	sc := &serviceConn{
		path:   fwdPath2,
		conn:   srvConn,
		writer: bufio.NewWriter(srvConn),
	}
	regMu.Lock()
	registrations[fwdPath2] = sc
	regMu.Unlock()
	defer cleanReg(fwdPath2)

	readCli := jsonRead(cliConn)
	msgCh := make(chan map[string]interface{}, 1)
	go func() { msgCh <- readCli() }()

	forwardInvokeToService(fwdPath2, "sid-noauth", map[string]interface{}{}, "")

	select {
	case msg := <-msgCh:
		if msg == nil {
			t.Fatal("no message received")
		}
		if _, ok := msg["authorization"]; ok {
			t.Error("authorization field should be absent when token is empty")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

// ---- startHeartbeat --------------------------------------------------------

func TestStartHeartbeat_ClosesOnMissedPong(t *testing.T) {
	// Use tight timers. Override globals before goroutines start; restore after
	// all goroutines that read them are guaranteed to have finished.
	HeartbeatInterval = 10 * time.Millisecond
	HeartbeatTimeout = 20 * time.Millisecond

	srvConn, cliConn := net.Pipe()
	defer cliConn.Close()

	sc := &serviceConn{
		path:     "Test.HBProc",
		conn:     srvConn,
		writer:   bufio.NewWriter(srvConn),
		lastPong: time.Now(),
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		startHeartbeat(sc)
	}()

	// Drain/discard pings so the heartbeat goroutine can send them.
	go func() {
		buf := make([]byte, 256)
		for {
			if _, err := cliConn.Read(buf); err != nil {
				return
			}
		}
	}()

	// heartbeat must close the connection within a generous window.
	select {
	case <-done:
		// Heartbeat goroutine exited — means it closed the connection.
	case <-time.After(500 * time.Millisecond):
		t.Error("heartbeat did not close connection on missed pong")
		srvConn.Close()
	}

	// Restore globals AFTER the heartbeat goroutine is confirmed done.
	HeartbeatInterval = 15 * time.Second
	HeartbeatTimeout = 5 * time.Second
}

func TestStartHeartbeat_PongRenewsConnection(t *testing.T) {
	HeartbeatInterval = 20 * time.Millisecond
	HeartbeatTimeout = 20 * time.Millisecond

	srvConn, cliConn := net.Pipe()

	sc := &serviceConn{
		path:     "Test.HBPong",
		conn:     srvConn,
		writer:   bufio.NewWriter(srvConn),
		lastPong: time.Now(),
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		startHeartbeat(sc)
	}()

	// Simulate a service that always responds to pings with pong.
	cliScanner := bufio.NewScanner(cliConn)
	cliWriter := bufio.NewWriter(cliConn)
	go func() {
		for cliScanner.Scan() {
			var msg map[string]interface{}
			if json.Unmarshal(cliScanner.Bytes(), &msg) == nil && msg["action"] == "ping" {
				sc.mu.Lock()
				sc.lastPong = time.Now()
				sc.mu.Unlock()
				b, _ := json.Marshal(map[string]interface{}{"action": "pong"})
				cliWriter.Write(b)   //nolint:errcheck
				cliWriter.WriteByte('\n') //nolint:errcheck
				cliWriter.Flush()    //nolint:errcheck
			}
		}
	}()

	// After two full heartbeat cycles the goroutine should still be running.
	time.Sleep(3 * HeartbeatInterval)
	select {
	case <-done:
		t.Error("heartbeat goroutine exited despite timely pongs")
	default:
	}

	// Close everything to unblock the goroutine before restoring globals.
	srvConn.Close()
	cliConn.Close()
	<-done

	HeartbeatInterval = 15 * time.Second
	HeartbeatTimeout = 5 * time.Second
}
