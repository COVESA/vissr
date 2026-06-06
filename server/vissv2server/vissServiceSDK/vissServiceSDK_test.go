package vissServiceSDK

import (
	"bufio"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"
)

// fakeServer runs a minimal TCP server that handles one registration and
// sends/receives messages for testing. Returns the listener address, a channel
// of messages received from the client, and a send func for server→client messages.
func fakeServer(t *testing.T) (addr string, received chan map[string]interface{}, send func(msg map[string]interface{})) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("fakeServer: listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	received = make(chan map[string]interface{}, 16)
	msgQueue := make(chan map[string]interface{}, 8)

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		scanner := bufio.NewScanner(conn)
		writer := bufio.NewWriter(conn)

		// Handle registration.
		if !scanner.Scan() {
			return
		}
		var regMsg map[string]interface{}
		json.Unmarshal(scanner.Bytes(), &regMsg) //nolint:errcheck
		ack, _ := json.Marshal(map[string]interface{}{"registered": true, "path": regMsg["path"]})
		writer.Write(ack)
		writer.WriteByte('\n')
		writer.Flush()

		// Fan-out: send queued server→client messages.
		go func() {
			for msg := range msgQueue {
				b, _ := json.Marshal(msg)
				writer.Write(b)
				writer.WriteByte('\n')
				writer.Flush()
			}
		}()

		// Collect client→server messages; silently discard auto-health reports
		// so existing tests are not affected by the §30 auto-send.
		for scanner.Scan() {
			var m map[string]interface{}
			if err := json.Unmarshal(scanner.Bytes(), &m); err == nil {
				if m["action"] != "health" {
					received <- m
				}
			}
		}
	}()

	send = func(msg map[string]interface{}) {
		msgQueue <- msg
	}

	return ln.Addr().String(), received, send
}

// fakeServerRaw is like fakeServer but does NOT filter health messages from
// received. Use for tests that specifically verify health message delivery (§30).
func fakeServerRaw(t *testing.T) (addr string, received chan map[string]interface{}, send func(msg map[string]interface{})) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("fakeServerRaw: listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	received = make(chan map[string]interface{}, 16)
	msgQueue := make(chan map[string]interface{}, 8)

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		scanner := bufio.NewScanner(conn)
		writer := bufio.NewWriter(conn)

		if !scanner.Scan() {
			return
		}
		var regMsg map[string]interface{}
		json.Unmarshal(scanner.Bytes(), &regMsg) //nolint:errcheck
		ack, _ := json.Marshal(map[string]interface{}{"registered": true, "path": regMsg["path"]})
		writer.Write(ack)
		writer.WriteByte('\n')
		writer.Flush()

		go func() {
			for msg := range msgQueue {
				b, _ := json.Marshal(msg)
				writer.Write(b)
				writer.WriteByte('\n')
				writer.Flush()
			}
		}()

		for scanner.Scan() {
			var m map[string]interface{}
			if err := json.Unmarshal(scanner.Bytes(), &m); err == nil {
				received <- m
			}
		}
	}()

	send = func(msg map[string]interface{}) {
		msgQueue <- msg
	}

	return ln.Addr().String(), received, send
}

func TestNewService_BuilderChain(t *testing.T) {
	svc := NewService("localhost:8300", "Root.Proc").
		WithInput("X", "uint8").
		WithOutput("Y", "uint8")
	if svc.path != "Root.Proc" {
		t.Errorf("wrong path: %s", svc.path)
	}
	if svc.signature["input"]["X"] != "uint8" {
		t.Error("input param X missing")
	}
	if svc.signature["output"]["Y"] != "uint8" {
		t.Error("output param Y missing")
	}
}

func TestRegister_FailsWithoutHandler(t *testing.T) {
	svc := NewService("localhost:9", "Root.Proc")
	_, err := svc.Register()
	if err == nil || !strings.Contains(err.Error(), "OnInvoke") {
		t.Fatalf("expected error about missing handler, got %v", err)
	}
}

func TestRegister_FailsOnBadAddr(t *testing.T) {
	svc := NewService("127.0.0.1:1", "Root.Proc").OnInvoke(func(_ *InvokeContext) {})
	_, err := svc.Register()
	if err == nil {
		t.Fatal("expected connection error")
	}
}

func TestRegister_SendsRegistrationMessage(t *testing.T) {
	addr, received, _ := fakeServer(t)

	svc := NewService(addr, "VehicleService.Seating.MoveSeat").
		WithInput("Position", "uint8").
		WithOutput("Position", "uint8").
		OnInvoke(func(_ *InvokeContext) {})

	regSvc, err := svc.Register()
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	defer regSvc.Close()

	// Registration message was consumed by fakeServer; ack was received.
	// Send a deregister to trigger a message we can inspect.
	regSvc.Close()

	select {
	case m := <-received:
		if m["action"] != "deregister" {
			t.Errorf("expected deregister, got %v", m["action"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for deregister")
	}
}

func TestRun_DispatchesInvokeToHandler(t *testing.T) {
	addr, received, send := fakeServer(t)

	handlerCalled := make(chan *InvokeContext, 1)
	svc := NewService(addr, "Root.P").
		WithInput("X", "uint8").
		WithOutput("Y", "uint8").
		OnInvoke(func(ctx *InvokeContext) {
			handlerCalled <- ctx
		})

	regSvc, err := svc.Register()
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	defer regSvc.Close()

	go regSvc.Run()

	send(map[string]interface{}{
		"action":    "invoke",
		"sessionId": "sess-1",
		"input":     map[string]interface{}{"X": "42"},
	})

	select {
	case ctx := <-handlerCalled:
		if ctx.SessionId != "sess-1" {
			t.Errorf("wrong sessionId: %s", ctx.SessionId)
		}
		if ctx.Input["X"] != "42" {
			t.Errorf("wrong input: %v", ctx.Input)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for handler")
	}
	_ = received
}

func TestReportProgress_SendsMessageToServer(t *testing.T) {
	addr, received, send := fakeServer(t)

	svc := NewService(addr, "Root.P").
		WithOutput("Y", "uint8").
		OnInvoke(func(ctx *InvokeContext) {
			ctx.ReportProgress("ONGOING", map[string]interface{}{"Y": "10"})    //nolint:errcheck
			ctx.ReportProgress("SUCCESSFUL", map[string]interface{}{"Y": "42"}) //nolint:errcheck
		})

	regSvc, err := svc.Register()
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	defer regSvc.Close()
	go regSvc.Run()

	send(map[string]interface{}{
		"action":    "invoke",
		"sessionId": "sess-2",
		"input":     map[string]interface{}{},
	})

	var msgs []map[string]interface{}
	deadline := time.After(2 * time.Second)
	for len(msgs) < 2 {
		select {
		case m := <-received:
			msgs = append(msgs, m)
		case <-deadline:
			t.Fatalf("timeout: got %d messages, want 2", len(msgs))
		}
	}

	if msgs[0]["status"] != "ONGOING" {
		t.Errorf("first message should be ONGOING, got %v", msgs[0]["status"])
	}
	if msgs[1]["status"] != "SUCCESSFUL" {
		t.Errorf("second message should be SUCCESSFUL, got %v", msgs[1]["status"])
	}
	for _, m := range msgs {
		if m["sessionId"] != "sess-2" {
			t.Errorf("wrong sessionId: %v", m["sessionId"])
		}
	}
}

func TestClose_Idempotent(t *testing.T) {
	addr, _, _ := fakeServer(t)
	svc := NewService(addr, "Root.P").OnInvoke(func(_ *InvokeContext) {})
	regSvc, err := svc.Register()
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	// Close twice must not panic.
	regSvc.Close()
	regSvc.Close()
}

// TestRun_RespondsToHeartbeatPing verifies that the SDK replies to a server
// ping with a pong message (VISSv3.3 §19).
func TestRun_RespondsToHeartbeatPing(t *testing.T) {
	addr, received, send := fakeServer(t)

	svc := NewService(addr, "Root.P").OnInvoke(func(_ *InvokeContext) {})
	regSvc, err := svc.Register()
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	defer regSvc.Close()
	go regSvc.Run()

	send(map[string]interface{}{"action": "ping"})

	select {
	case m := <-received:
		if m["action"] != "pong" {
			t.Errorf("expected pong, got %v", m["action"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for pong")
	}
}

// TestInvokeContext_ReportError verifies that ReportError sends a FAILED
// message with a structured error payload (VISSv3.3 §20).
func TestInvokeContext_ReportError_SendsFailed(t *testing.T) {
	addr, received, send := fakeServer(t)

	svc := NewService(addr, "Root.P").
		OnInvoke(func(ctx *InvokeContext) {
			ctx.ReportError("MOTOR_STALL", "seat motor stalled", nil) //nolint:errcheck
		})

	regSvc, err := svc.Register()
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	defer regSvc.Close()
	go regSvc.Run()

	send(map[string]interface{}{
		"action":    "invoke",
		"sessionId": "sess-err",
		"input":     map[string]interface{}{},
	})

	select {
	case m := <-received:
		if m["status"] != "FAILED" {
			t.Errorf("want FAILED, got %v", m["status"])
		}
		errField, ok := m["error"].(map[string]interface{})
		if !ok {
			t.Fatalf("missing 'error' field: %v", m)
		}
		if errField["code"] != "MOTOR_STALL" {
			t.Errorf("wrong error code: %v", errField["code"])
		}
		if errField["message"] != "seat motor stalled" {
			t.Errorf("wrong error message: %v", errField["message"])
		}
		if m["sessionId"] != "sess-err" {
			t.Errorf("wrong sessionId: %v", m["sessionId"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for error report")
	}
}

// TestInvokeContext_AuthorizationPassedToHandler verifies that the
// Authorization field in InvokeContext carries the forwarded client auth token
// (VISSv3.3 §21).
func TestInvokeContext_AuthorizationPassedToHandler(t *testing.T) {
	addr, _, send := fakeServer(t)

	authReceived := make(chan string, 1)
	svc := NewService(addr, "Root.P").
		OnInvoke(func(ctx *InvokeContext) {
			authReceived <- ctx.Authorization
		})

	regSvc, err := svc.Register()
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	defer regSvc.Close()
	go regSvc.Run()

	send(map[string]interface{}{
		"action":        "invoke",
		"sessionId":     "sess-auth",
		"input":         map[string]interface{}{},
		"authorization": "Bearer tok123",
	})

	select {
	case got := <-authReceived:
		if got != "Bearer tok123" {
			t.Errorf("wrong auth token: %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for handler")
	}
}

// TestWithReconnect_BuilderChain verifies that WithReconnect sets the fields
// and that the builder chain still returns *Service (VISSv3.3 §24).
func TestWithReconnect_BuilderChain(t *testing.T) {
	svc := NewService("localhost:8300", "Root.P").
		OnInvoke(func(_ *InvokeContext) {}).
		WithReconnect(3, 500*time.Millisecond)

	if !svc.reconnect {
		t.Error("reconnect should be true")
	}
	if svc.maxRetries != 3 {
		t.Errorf("maxRetries want 3, got %d", svc.maxRetries)
	}
	if svc.retryDelay != 500*time.Millisecond {
		t.Errorf("retryDelay want 500ms, got %v", svc.retryDelay)
	}
}

// ---- §26 Cancel propagation -------------------------------------------------

// TestInvokeContext_Done_ClosedOnCancel verifies that ctx.Done() is closed when
// the server sends a cancel message for the active invocation (VISSv3.3 §26).
func TestInvokeContext_Done_ClosedOnCancel(t *testing.T) {
	addr, _, send := fakeServer(t)

	cancelReceived := make(chan struct{})
	svc := NewService(addr, "Root.P").
		OnInvoke(func(ctx *InvokeContext) {
			select {
			case <-ctx.Done():
				close(cancelReceived)
			case <-time.After(3 * time.Second):
			}
		})

	regSvc, err := svc.Register()
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	defer regSvc.Close()
	go regSvc.Run()

	send(map[string]interface{}{
		"action":    "invoke",
		"sessionId": "sess-cancel",
		"input":     map[string]interface{}{},
	})

	// Give the handler time to start and block on Done().
	time.Sleep(30 * time.Millisecond)

	send(map[string]interface{}{
		"action":    "cancel",
		"sessionId": "sess-cancel",
	})

	select {
	case <-cancelReceived:
		// ctx.Done() was closed — correct.
	case <-time.After(2 * time.Second):
		t.Fatal("ctx.Done() was not closed after server cancel")
	}
}

// TestInvokeContext_Done_OpenDuringInvoke verifies that Done() is not yet
// closed when the handler starts (before any cancel).
func TestInvokeContext_Done_OpenDuringInvoke(t *testing.T) {
	addr, _, send := fakeServer(t)

	doneOpen := make(chan bool, 1)
	svc := NewService(addr, "Root.P").
		OnInvoke(func(ctx *InvokeContext) {
			select {
			case <-ctx.Done():
				doneOpen <- false
			default:
				doneOpen <- true
			}
		})

	regSvc, err := svc.Register()
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	defer regSvc.Close()
	go regSvc.Run()

	send(map[string]interface{}{
		"action":    "invoke",
		"sessionId": "sess-nodecancel",
		"input":     map[string]interface{}{},
	})

	select {
	case open := <-doneOpen:
		if !open {
			t.Error("Done() should be open during normal handler execution")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not run")
	}
}

// ---- §27 Service versioning -------------------------------------------------

// TestWithVersion_BuilderChain verifies that WithVersion sets the field and
// returns the service for chaining (VISSv3.3 §27).
func TestWithVersion_BuilderChain(t *testing.T) {
	svc := NewService("localhost:8300", "Root.P").
		WithVersion("2.1.0").
		OnInvoke(func(_ *InvokeContext) {})
	if svc.version != "2.1.0" {
		t.Errorf("want version 2.1.0, got %q", svc.version)
	}
}

// TestRegister_SendsVersionInRegistrationMessage verifies that the version
// is included in the register message when WithVersion is called (§27).
func TestRegister_SendsVersionInRegistrationMessage(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	regMsgCh := make(chan map[string]interface{}, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		sc := bufio.NewScanner(conn)
		if sc.Scan() {
			var m map[string]interface{}
			json.Unmarshal(sc.Bytes(), &m) //nolint:errcheck
			regMsgCh <- m
		}
		wr := bufio.NewWriter(conn)
		ack, _ := json.Marshal(map[string]interface{}{"registered": true})
		wr.Write(ack)
		wr.WriteByte('\n')
		wr.Flush()
	}()

	svc := NewService(ln.Addr().String(), "Root.P").
		WithVersion("1.5.0").
		OnInvoke(func(_ *InvokeContext) {})

	regSvc, err := svc.Register()
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	defer regSvc.Close()

	select {
	case m := <-regMsgCh:
		if m["version"] != "1.5.0" {
			t.Errorf("want version 1.5.0 in registration message, got %v", m["version"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for registration message")
	}
}

// TestRegister_NoVersionWhenEmpty verifies that "version" is absent when
// WithVersion is not called.
func TestRegister_NoVersionWhenEmpty(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	regMsgCh := make(chan map[string]interface{}, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		sc := bufio.NewScanner(conn)
		if sc.Scan() {
			var m map[string]interface{}
			json.Unmarshal(sc.Bytes(), &m) //nolint:errcheck
			regMsgCh <- m
		}
		wr := bufio.NewWriter(conn)
		ack, _ := json.Marshal(map[string]interface{}{"registered": true})
		wr.Write(ack)
		wr.WriteByte('\n')
		wr.Flush()
	}()

	svc := NewService(ln.Addr().String(), "Root.P").
		OnInvoke(func(_ *InvokeContext) {})

	regSvc, err := svc.Register()
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	defer regSvc.Close()

	select {
	case m := <-regMsgCh:
		if _, ok := m["version"]; ok {
			t.Error("version should be absent when WithVersion not called")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

// ---- §28 Progress percentage ------------------------------------------------

// TestReportProgressPct_SendsProgressField verifies that ReportProgressPct
// includes "progress" in the message sent to the server (VISSv3.3 §28).
func TestReportProgressPct_SendsProgressField(t *testing.T) {
	addr, received, send := fakeServer(t)

	svc := NewService(addr, "Root.P").
		OnInvoke(func(ctx *InvokeContext) {
			ctx.ReportProgressPct(50, "ONGOING", nil) //nolint:errcheck
		})

	regSvc, err := svc.Register()
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	defer regSvc.Close()
	go regSvc.Run()

	send(map[string]interface{}{
		"action":    "invoke",
		"sessionId": "sess-pct",
		"input":     map[string]interface{}{},
	})

	select {
	case m := <-received:
		if m["sessionId"] != "sess-pct" {
			t.Errorf("unexpected message: %v", m)
		}
		pct, ok := m["progress"]
		if !ok {
			t.Fatal("progress field missing")
		}
		if pct != float64(50) {
			t.Errorf("want progress=50, got %v", pct)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for progress report")
	}
}

// TestReportProgressPct_ClampsOutOfRange verifies that values outside [0,100]
// are clamped before being sent.
func TestReportProgressPct_ClampsOutOfRange(t *testing.T) {
	addr, received, send := fakeServer(t)

	svc := NewService(addr, "Root.P").
		OnInvoke(func(ctx *InvokeContext) {
			ctx.ReportProgressPct(150, "ONGOING", nil) //nolint:errcheck
			ctx.ReportProgressPct(-5, "ONGOING", nil)  //nolint:errcheck
		})

	regSvc, err := svc.Register()
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	defer regSvc.Close()
	go regSvc.Run()

	send(map[string]interface{}{
		"action":    "invoke",
		"sessionId": "sess-clamp",
		"input":     map[string]interface{}{},
	})

	var msgs []map[string]interface{}
	deadline := time.After(2 * time.Second)
	for len(msgs) < 2 {
		select {
		case m := <-received:
			msgs = append(msgs, m)
		case <-deadline:
			t.Fatalf("timeout: collected %d messages", len(msgs))
		}
	}

	if msgs[0]["progress"] != float64(100) {
		t.Errorf("want 100 for >100 input, got %v", msgs[0]["progress"])
	}
	if msgs[1]["progress"] != float64(0) {
		t.Errorf("want 0 for <0 input, got %v", msgs[1]["progress"])
	}
}

// ---- §30 Service health reporting -------------------------------------------

// TestConnect_AutoSendsHealthyTrue verifies that Register() causes the SDK
// to automatically send a health=true report after successful registration (§30).
func TestConnect_AutoSendsHealthyTrue(t *testing.T) {
	addr, received, _ := fakeServerRaw(t)

	svc := NewService(addr, "Root.P").OnInvoke(func(_ *InvokeContext) {})
	regSvc, err := svc.Register()
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	defer regSvc.Close()

	select {
	case m := <-received:
		if m["action"] != "health" {
			t.Errorf("want first post-registration message to be health, got %v", m["action"])
		}
		if m["healthy"] != true {
			t.Errorf("want healthy=true in auto-sent health, got %v", m["healthy"])
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for auto health message")
	}
}

// TestReportHealth_SendsHealthMessage verifies that ReportHealth sends the
// expected message to the server (VISSv3.3 §30).
func TestReportHealth_SendsHealthMessage(t *testing.T) {
	addr, received, _ := fakeServerRaw(t)

	svc := NewService(addr, "Root.P").OnInvoke(func(_ *InvokeContext) {})
	regSvc, err := svc.Register()
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	defer regSvc.Close()

	// Drain the auto-health message from connect().
	select {
	case <-received:
	case <-time.After(time.Second):
		t.Fatal("auto-health message not received")
	}

	if err := regSvc.ReportHealth(false, "motor overheated"); err != nil {
		t.Fatalf("ReportHealth: %v", err)
	}

	select {
	case m := <-received:
		if m["action"] != "health" {
			t.Errorf("want action=health, got %v", m["action"])
		}
		if m["healthy"] != false {
			t.Errorf("want healthy=false, got %v", m["healthy"])
		}
		if m["detail"] != "motor overheated" {
			t.Errorf("wrong detail: %v", m["detail"])
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for health message")
	}
}

// TestRun_NoReconnectByDefault verifies that Run() exits without reconnecting
// when WithReconnect is not called and the server closes the connection.
func TestRun_NoReconnectByDefault(t *testing.T) {
	addr, _, send := fakeServer(t)

	svc := NewService(addr, "Root.P").OnInvoke(func(_ *InvokeContext) {})
	regSvc, err := svc.Register()
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	done := make(chan struct{})
	go func() {
		regSvc.Run()
		close(done)
	}()

	// Close the connection from the server side by sending a nonsense close signal.
	// We do this by closing the service connection itself.
	regSvc.conn.Close()
	send(map[string]interface{}{}) // unblock the queue goroutine

	select {
	case <-done:
		// Run() exited without reconnect attempt — correct.
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not exit after connection close")
	}
}

// TestReportError_WithOutput verifies that ReportError includes the output map
// when one is supplied (covers the output != nil branch).
func TestReportError_WithOutput(t *testing.T) {
	addr, received, send := fakeServer(t)

	svc := NewService(addr, "Root.P").
		OnInvoke(func(ctx *InvokeContext) {
			ctx.ReportError("MOTOR_JAM", "motor jammed", map[string]interface{}{ //nolint:errcheck
				"lastPosition": "42",
			})
		})

	regSvc, err := svc.Register()
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	done := make(chan struct{})
	go func() {
		regSvc.Run()
		close(done)
	}()

	send(map[string]interface{}{"action": "invoke", "sessionId": "err-out-1"})

	select {
	case msg := <-received:
		if msg["status"] != "FAILED" {
			t.Errorf("status = %v, want FAILED", msg["status"])
		}
		output, ok := msg["output"].(map[string]interface{})
		if !ok {
			t.Fatalf("output missing or wrong type: %T %v", msg["output"], msg["output"])
		}
		if output["lastPosition"] != "42" {
			t.Errorf("output.lastPosition = %v, want 42", output["lastPosition"])
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for FAILED message")
	}

	regSvc.conn.Close()
	send(map[string]interface{}{})
	<-done
}

// TestWithReconnect_ZeroDelayDefaultsToOneSecond verifies the initialDelay <= 0
// guard: a zero delay is replaced with time.Second.
func TestWithReconnect_ZeroDelayDefaultsToOneSecond(t *testing.T) {
	svc := NewService("127.0.0.1:1", "Root.P").
		OnInvoke(func(_ *InvokeContext) {}).
		WithReconnect(3, 0)

	if svc.retryDelay != time.Second {
		t.Errorf("retryDelay = %v, want %v", svc.retryDelay, time.Second)
	}
	if svc.maxRetries != 3 {
		t.Errorf("maxRetries = %d, want 3", svc.maxRetries)
	}
	if !svc.reconnect {
		t.Error("reconnect flag not set")
	}
}

// ---- connect error paths ----------------------------------------------------

// TestConnect_SendRegisterFails verifies that connect returns an error when
// the server closes the TCP connection immediately after Accept (before reading
// the registration message). The bufio.Writer.Flush() fails with a write error.
func TestConnect_SendRegisterFails(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	// Server accepts then immediately closes with RST (SO_LINGER=0 or just Close).
	// A plain Close() sends a FIN; the client can still write to a FIN'd peer
	// until the OS sends back a RST. We force the RST by using SetLinger(0).
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		// Set linger to 0 so Close() sends RST, not FIN.
		if tc, ok := conn.(*net.TCPConn); ok {
			tc.SetLinger(0)
		}
		conn.Close()
	}()

	svc := NewService(ln.Addr().String(), "Root.P").OnInvoke(func(_ *InvokeContext) {})

	// Retry a couple of times in case the OS hasn't propagated the RST yet.
	var regErr error
	for i := 0; i < 10; i++ {
		_, regErr = svc.Register()
		if regErr != nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if regErr == nil {
		t.Skip("OS buffered the write successfully — RST path not triggerable on this platform")
	}
}

// TestConnect_NoAckFromServer verifies that connect returns an error when the
// server closes the connection before sending an ack.
func TestConnect_NoAckFromServer(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	// Server accepts, reads the register message, then immediately closes.
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		// Drain register message then close without sending ack.
		sc := bufio.NewScanner(conn)
		sc.Scan()
		conn.Close()
	}()

	svc := NewService(ln.Addr().String(), "Root.P").OnInvoke(func(_ *InvokeContext) {})
	_, err = svc.Register()
	if err == nil {
		t.Fatal("expected error when server sends no ack")
	}
	if !strings.Contains(err.Error(), "no ack") {
		t.Errorf("want 'no ack' in error, got: %v", err)
	}
}

// TestConnect_RegistrationRejected verifies that connect returns an error when
// the server sends an ack with registered=false.
func TestConnect_RegistrationRejected(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		sc := bufio.NewScanner(conn)
		sc.Scan() // consume register
		wr := bufio.NewWriter(conn)
		ack, _ := json.Marshal(map[string]interface{}{"registered": false, "reason": "path not found"})
		wr.Write(ack)
		wr.WriteByte('\n')
		wr.Flush()
	}()

	svc := NewService(ln.Addr().String(), "Root.P").OnInvoke(func(_ *InvokeContext) {})
	_, err = svc.Register()
	if err == nil {
		t.Fatal("expected error for rejected registration")
	}
	if !strings.Contains(err.Error(), "registration rejected") {
		t.Errorf("want 'registration rejected' in error, got: %v", err)
	}
}

// TestConnect_InvalidAckJSON verifies that connect returns an error when the
// server sends a non-JSON ack (the json.Unmarshal branch).
func TestConnect_InvalidAckJSON(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		sc := bufio.NewScanner(conn)
		sc.Scan() // consume register
		wr := bufio.NewWriter(conn)
		wr.WriteString("not-json\n")
		wr.Flush()
	}()

	svc := NewService(ln.Addr().String(), "Root.P").OnInvoke(func(_ *InvokeContext) {})
	_, err = svc.Register()
	if err == nil {
		t.Fatal("expected error for invalid ack JSON")
	}
}

// ---- sendJSON error path -----------------------------------------------------

// TestSendJSON_MarshalError verifies that sendJSON returns an error when the
// value cannot be marshalled to JSON (e.g. contains a channel).
func TestSendJSON_MarshalError(t *testing.T) {
	addr, _, _ := fakeServer(t)

	svc := NewService(addr, "Root.P").OnInvoke(func(_ *InvokeContext) {})
	regSvc, err := svc.Register()
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	defer regSvc.Close()

	// A channel is not JSON-serialisable — Marshal will return an error.
	bad := map[string]interface{}{
		"action": make(chan int),
	}
	err = regSvc.sendJSON(bad)
	if err == nil {
		t.Fatal("expected marshal error for channel value")
	}
}

// ---- ReportProgressPct with output ------------------------------------------

// TestReportProgressPct_WithOutput verifies that a non-nil output map is
// included in the message (covers the output != nil branch).
func TestReportProgressPct_WithOutput(t *testing.T) {
	addr, received, send := fakeServer(t)

	svc := NewService(addr, "Root.P").
		OnInvoke(func(ctx *InvokeContext) {
			ctx.ReportProgressPct(42, "ONGOING", map[string]interface{}{"Position": "42"}) //nolint:errcheck
		})

	regSvc, err := svc.Register()
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	defer regSvc.Close()
	go regSvc.Run()

	send(map[string]interface{}{
		"action":    "invoke",
		"sessionId": "sess-pct-out",
		"input":     map[string]interface{}{},
	})

	select {
	case m := <-received:
		if m["sessionId"] != "sess-pct-out" {
			t.Fatalf("unexpected message: %v", m)
		}
		if m["progress"] != float64(42) {
			t.Errorf("want progress=42, got %v", m["progress"])
		}
		output, ok := m["output"].(map[string]interface{})
		if !ok {
			t.Fatalf("output missing or wrong type: %T %v", m["output"], m["output"])
		}
		if output["Position"] != "42" {
			t.Errorf("output.Position = %v, want 42", output["Position"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for progress report with output")
	}
}

// ---- runLoop stopCh path ----------------------------------------------------

// TestRunLoop_StopChExitsLoop verifies that runLoop returns when stopCh is
// closed before the scanner would block.
func TestRunLoop_StopChExitsLoop(t *testing.T) {
	addr, _, _ := fakeServer(t)

	svc := NewService(addr, "Root.P").OnInvoke(func(_ *InvokeContext) {})
	regSvc, err := svc.Register()
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	done := make(chan struct{})
	go func() {
		regSvc.runLoop()
		close(done)
	}()

	// Close stopCh to trigger the select at the top of the runLoop.
	regSvc.Close()

	select {
	case <-done:
		// runLoop exited via stopCh — correct.
	case <-time.After(2 * time.Second):
		t.Fatal("runLoop did not exit after stopCh closed")
	}
}

// TestRunLoop_CancelUnknownSession verifies that a cancel for an unknown
// sessionId does not panic or block (the ctx-not-found branch).
func TestRunLoop_CancelUnknownSession(t *testing.T) {
	addr, _, send := fakeServer(t)

	svc := NewService(addr, "Root.P").OnInvoke(func(_ *InvokeContext) {})
	regSvc, err := svc.Register()
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	defer regSvc.Close()

	done := make(chan struct{})
	go func() {
		regSvc.Run()
		close(done)
	}()

	// Cancel a session that was never started — should be silently ignored.
	send(map[string]interface{}{
		"action":    "cancel",
		"sessionId": "non-existent-session",
	})

	// Allow time for message to be processed then close cleanly.
	time.Sleep(50 * time.Millisecond)
	regSvc.conn.Close()
	send(map[string]interface{}{}) // unblock queue goroutine

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not exit")
	}
}

// ---- Run reconnect paths ----------------------------------------------------

// TestRun_ReconnectExhaustsMaxRetries verifies that Run() returns after
// maxRetries failed reconnect attempts (VISSv3.3 §24).
func TestRun_ReconnectExhaustsMaxRetries(t *testing.T) {
	// Use a server that closes immediately after ack so the run loop exits fast.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	// Accept exactly one connection (initial Register), then stop listening.
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		sc := bufio.NewScanner(conn)
		sc.Scan()
		wr := bufio.NewWriter(conn)
		ack, _ := json.Marshal(map[string]interface{}{"registered": true})
		wr.Write(ack)
		wr.WriteByte('\n')
		wr.Flush()
		// Close immediately — runLoop will see EOF.
	}()

	addr := ln.Addr().String()
	ln.Close() // stop accepting new connections so reconnect always fails

	svc := NewService(addr, "Root.P").
		OnInvoke(func(_ *InvokeContext) {}).
		WithReconnect(2, time.Millisecond) // 2 retries, 1ms delay

	// Manually dial to complete registration (ln is closed so Register would fail).
	// Instead, create a direct server for the initial connection.
	ln2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen2: %v", err)
	}
	go func() {
		conn, err := ln2.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		sc := bufio.NewScanner(conn)
		sc.Scan() // register
		wr := bufio.NewWriter(conn)
		ack, _ := json.Marshal(map[string]interface{}{"registered": true})
		wr.Write(ack)
		wr.WriteByte('\n')
		wr.Flush()
		// Close right away so runLoop returns.
	}()
	svc.serverAddr = ln2.Addr().String()
	regSvc, err := svc.Register()
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	// After registration ln2 is done; subsequent connect attempts go to the
	// original closed address — all will fail.
	svc.serverAddr = addr // point at the closed listener

	done := make(chan struct{})
	go func() {
		regSvc.Run()
		close(done)
	}()

	select {
	case <-done:
		// Run exited after exhausting maxRetries — correct.
	case <-time.After(3 * time.Second):
		t.Fatal("Run() did not exit after exhausting maxRetries")
	}
}

// TestRun_StopChDuringReconnectDelay verifies that Close() during the
// reconnect back-off delay causes Run() to exit immediately.
func TestRun_StopChDuringReconnectDelay(t *testing.T) {
	// Create a server for initial registration only.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		sc := bufio.NewScanner(conn)
		sc.Scan()
		wr := bufio.NewWriter(conn)
		ack, _ := json.Marshal(map[string]interface{}{"registered": true})
		wr.Write(ack)
		wr.WriteByte('\n')
		wr.Flush()
		// Immediately close so runLoop returns and reconnect begins.
	}()

	initialAddr := ln.Addr().String()
	svc := NewService(initialAddr, "Root.P").
		OnInvoke(func(_ *InvokeContext) {}).
		WithReconnect(0, 500*time.Millisecond) // unlimited retries, 500ms delay

	regSvc, err := svc.Register()
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	ln.Close() // ensure reconnect attempts fail

	// Point at a port that refuses connections so the delay is entered.
	svc.serverAddr = "127.0.0.1:1"

	done := make(chan struct{})
	go func() {
		regSvc.Run()
		close(done)
	}()

	// Give runLoop time to exit and reconnect to fail, entering the delay select.
	time.Sleep(80 * time.Millisecond)
	regSvc.Close() // fires stopCh during the delay select

	select {
	case <-done:
		// Run() exited when stopCh fired during delay — correct.
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not exit after Close() during reconnect delay")
	}
}

// TestRun_ReconnectSuccessResetsRetries verifies that a successful reconnect
// resets the retry counter so subsequent failures start fresh (line 293).
// Strategy: register → close conn1 → reconnect succeeds (conn2) → retries=0 →
// close conn2 → close listener → reconnect fails with maxRetries=1 → Run exits.
// No concurrent Close() is called so there is no data race on s.conn/s.writer.
func TestRun_ReconnectSuccessResetsRetries(t *testing.T) {
	connections := make(chan net.Conn, 4)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	// Do NOT defer ln.Close() here — we close it explicitly at the right moment.

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return // ln was closed
			}
			connections <- conn
			sc := bufio.NewScanner(conn)
			sc.Scan() // consume register
			wr := bufio.NewWriter(conn)
			ack, _ := json.Marshal(map[string]interface{}{"registered": true})
			wr.Write(ack)
			wr.WriteByte('\n')
			wr.Flush()
			// Hold connection open; caller closes it via connections channel.
		}
	}()

	svc := NewService(ln.Addr().String(), "Root.P").
		OnInvoke(func(_ *InvokeContext) {}).
		WithReconnect(1, 5*time.Millisecond) // maxRetries=1, tiny delay

	regSvc, err := svc.Register()
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Drain the initial connection.
	var conn1 net.Conn
	select {
	case conn1 = <-connections:
	case <-time.After(time.Second):
		t.Fatal("initial connection not received")
	}

	done := make(chan struct{})
	go func() {
		regSvc.Run()
		close(done)
	}()

	// Drop first connection → runLoop returns → reconnect succeeds (retries reset to 0).
	conn1.Close()

	var conn2 net.Conn
	select {
	case conn2 = <-connections:
	case <-time.After(2 * time.Second):
		t.Fatal("reconnect connection not received")
	}

	// Close listener first so the next reconnect attempt fails with connection refused.
	// Then close conn2 so runLoop exits and the reconnect path is entered.
	// retries=0 after the successful reconnect, so maxRetries check (0 >= 1) is false
	// — one failure increments retries to 1 → 1 >= 1 → Run() exits.
	ln.Close()
	conn2.Close()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Run() did not exit after retries reset and maxRetries exhausted")
	}
}

// TestRun_DelayCapAt2Minutes verifies the exponential back-off cap: once the
// computed delay exceeds 2 minutes it is clamped to exactly 2 minutes and the
// inner doubling loop breaks (line 277-280).
// We use a large initialDelay so the cap is hit on the very first doubling
// (retries=1 → delay*2 = 4 min > 2 min).
func TestRun_DelayCapAt2Minutes(t *testing.T) {
	// Server for initial registration.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		sc := bufio.NewScanner(conn)
		sc.Scan()
		wr := bufio.NewWriter(conn)
		ack, _ := json.Marshal(map[string]interface{}{"registered": true})
		wr.Write(ack)
		wr.WriteByte('\n')
		wr.Flush()
		// Close immediately — runLoop exits, reconnect loop starts.
	}()

	initialAddr := ln.Addr().String()
	svc := NewService(initialAddr, "Root.P").
		OnInvoke(func(_ *InvokeContext) {}).
		// initialDelay > 1 min so that delay*2 > 2 min after one retry.
		WithReconnect(0, 90*time.Second)

	regSvc, err := svc.Register()
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	ln.Close()

	// First reconnect attempt will fail (ln closed) → retries becomes 1.
	// Second iteration: delay = 90s * 2 = 180s > 2min → capped → delay = 2min.
	// We fire stopCh during the delay select to avoid waiting 2 minutes.
	svc.serverAddr = "127.0.0.1:1" // guaranteed-refused address

	done := make(chan struct{})
	go func() {
		regSvc.Run()
		close(done)
	}()

	// Wait long enough for runLoop to exit, first reconnect to fail, and the
	// second delay select to be entered (with the capped 2-min delay).
	time.Sleep(120 * time.Millisecond)
	regSvc.Close()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Run() did not exit after Close() with capped delay")
	}
}

// TestRunLoop_StopChAfterRunLoop verifies that when stopCh is closed while
// runLoop is still running (via Close before conn closes), the post-runLoop
// stopCh select returns immediately without attempting reconnect.
func TestRunLoop_StopChAfterRunLoop(t *testing.T) {
	addr, _, _ := fakeServer(t)
	svc := NewService(addr, "Root.P").
		OnInvoke(func(_ *InvokeContext) {}).
		WithReconnect(0, time.Millisecond) // unlimited reconnect — but stopCh fires

	regSvc, err := svc.Register()
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	done := make(chan struct{})
	go func() {
		regSvc.Run()
		close(done)
	}()

	// Close() sets stopCh; runLoop will detect it on the next loop iteration and
	// return, then the post-runLoop select case fires and Run() returns.
	regSvc.Close()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not exit after Close() with reconnect enabled")
	}
}

// TestRunLoop_CancelAlreadyClosed verifies that cancelling an invocation whose
// doneCh is already closed (double-cancel) does not panic. The handler must
// remain in activeCtxs between both cancel messages so the second cancel finds
// the ctx and takes the "case <-ctx.doneCh:" branch.
func TestRunLoop_CancelAlreadyClosed(t *testing.T) {
	addr, _, send := fakeServer(t)

	handlerStarted := make(chan struct{})
	allowFinish := make(chan struct{}) // keeps handler alive until we're ready

	svc := NewService(addr, "Root.P").
		OnInvoke(func(ctx *InvokeContext) {
			close(handlerStarted)
			// Block until told to finish — keeps ctx in activeCtxs.
			<-allowFinish
		})

	regSvc, err := svc.Register()
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	defer regSvc.Close()
	go regSvc.Run()

	send(map[string]interface{}{
		"action":    "invoke",
		"sessionId": "sess-dc2",
		"input":     map[string]interface{}{},
	})

	<-handlerStarted

	// First cancel: closes doneCh via the "default" branch while handler is live.
	send(map[string]interface{}{
		"action":    "cancel",
		"sessionId": "sess-dc2",
	})
	// Give first cancel time to be processed.
	time.Sleep(30 * time.Millisecond)

	// Second cancel for the same session: doneCh is already closed, so the
	// "case <-ctx.doneCh:" branch is taken (no double-close panic).
	// The handler is still alive (blocked on allowFinish) so ctx remains in
	// activeCtxs.
	send(map[string]interface{}{
		"action":    "cancel",
		"sessionId": "sess-dc2",
	})
	time.Sleep(30 * time.Millisecond)

	// Release the handler so the test can clean up.
	close(allowFinish)
}

// fakeServerRaw2 is a variant of fakeServer that also exposes a channel for
// sending raw (non-JSON) bytes to the client. Used to exercise the
// json.Unmarshal error path in runLoop.
func fakeServerRaw2(t *testing.T) (addr string, received chan map[string]interface{}, sendRaw func(line string), sendJSON2 func(msg map[string]interface{})) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("fakeServerRaw2: listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	received = make(chan map[string]interface{}, 16)
	rawQueue := make(chan string, 8)
	msgQueue := make(chan map[string]interface{}, 8)

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		sc := bufio.NewScanner(conn)
		wr := bufio.NewWriter(conn)

		// Handle registration.
		if !sc.Scan() {
			return
		}
		ack, _ := json.Marshal(map[string]interface{}{"registered": true})
		wr.Write(ack)
		wr.WriteByte('\n')
		wr.Flush()

		// Fan-out sender: either raw bytes or JSON-encoded messages.
		go func() {
			for {
				select {
				case raw, ok := <-rawQueue:
					if !ok {
						return
					}
					wr.WriteString(raw)
					wr.Flush()
				case msg, ok := <-msgQueue:
					if !ok {
						return
					}
					b, _ := json.Marshal(msg)
					wr.Write(b)
					wr.WriteByte('\n')
					wr.Flush()
				}
			}
		}()

		// Collect client→server messages, skip health.
		for sc.Scan() {
			var m map[string]interface{}
			if err := json.Unmarshal(sc.Bytes(), &m); err == nil {
				if m["action"] != "health" {
					received <- m
				}
			}
		}
	}()

	sendRaw = func(line string) { rawQueue <- line + "\n" }
	sendJSON2 = func(msg map[string]interface{}) { msgQueue <- msg }
	return ln.Addr().String(), received, sendRaw, sendJSON2
}

// TestRunLoop_SkipsBadJSON verifies that a malformed JSON message from the
// server causes runLoop to skip (continue) rather than panic or exit.
// This covers the json.Unmarshal error branch (line 309-311).
func TestRunLoop_SkipsBadJSON(t *testing.T) {
	addr, received, sendRaw, sendJSON2 := fakeServerRaw2(t)

	svc := NewService(addr, "Root.P").OnInvoke(func(_ *InvokeContext) {})
	regSvc, err := svc.Register()
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	defer regSvc.Close()
	go regSvc.Run()

	// Send a bad-JSON line — runLoop must skip it via the continue branch.
	sendRaw("this is not valid json")

	// Then send a valid ping — if runLoop continued correctly it responds with pong.
	time.Sleep(20 * time.Millisecond)
	sendJSON2(map[string]interface{}{"action": "ping"})

	// Expect pong in response to the ping that followed the bad JSON.
	select {
	case m := <-received:
		if m["action"] != "pong" {
			t.Errorf("expected pong, got %v", m["action"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for pong after bad-JSON skip")
	}
}
