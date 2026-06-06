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
// sends/receives messages for testing. Returns the listener address and a
// channel of messages received from the client.
func fakeServer(t *testing.T) (addr string, received chan map[string]interface{}, sendInvoke func(msg map[string]interface{})) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("fakeServer: listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	received = make(chan map[string]interface{}, 16)
	invokeQueue := make(chan map[string]interface{}, 8)

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

		// Fan-out: send queued invoke messages, collect progress updates.
		go func() {
			for msg := range invokeQueue {
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

	sendInvoke = func(msg map[string]interface{}) {
		invokeQueue <- msg
	}

	return ln.Addr().String(), received, sendInvoke
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

	// Registration message has action:"register" and path.
	// The fake server consumed it; ack was received. No message in received yet.
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
	addr, received, sendInvoke := fakeServer(t)

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

	sendInvoke(map[string]interface{}{
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
	addr, received, sendInvoke := fakeServer(t)

	svc := NewService(addr, "Root.P").
		WithOutput("Y", "uint8").
		OnInvoke(func(ctx *InvokeContext) {
			ctx.ReportProgress("ONGOING", map[string]interface{}{"Y": "10"})  //nolint:errcheck
			ctx.ReportProgress("SUCCESSFUL", map[string]interface{}{"Y": "42"}) //nolint:errcheck
		})

	regSvc, err := svc.Register()
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	defer regSvc.Close()
	go regSvc.Run()

	sendInvoke(map[string]interface{}{
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
