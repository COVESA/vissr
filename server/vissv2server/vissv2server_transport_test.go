/**
* (C) 2026 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE
* file in this repository.
*
* Tests of the link between the server core and a transport manager, transportDataSession: requests go from
* the manager to the core, and responses and events go from the core to the manager. The two directions must
* never be mixed up: a response that the manager does not take at once must not be read back by the core as
* a request.
*
* The manager is modelled by a loop that receives a message, works for a while (a write to a client), and
* receives the next one, as the loops of the managers do.
**/
package main

import (
	"encoding/json"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

// session is one transportDataSession with the channels around it.
type session struct {
	requests  chan string                 // manager -> core, what the manager sends
	responses chan string                 // core -> manager, what the manager receives
	toCore    chan map[string]interface{} // what the core's dispatcher receives (transportDataChan)
	backend   chan map[string]interface{} // what the core sends to the manager (backendChan)
}

// startSession starts the real transportDataSession.
func startSession() *session {
	s := &session{
		requests:  make(chan string, 32),
		responses: make(chan string, 32),
		toCore:    make(chan map[string]interface{}, 1024),
		backend:   make(chan map[string]interface{}, 32),
	}
	go transportDataSession(s.requests, s.responses, s.toCore, s.backend)
	return s
}

// event is a subscription event for client 0 of manager 0.
func event(n int) map[string]interface{} {
	return map[string]interface{}{"RouterId": "0?0", "action": "subscription", "subscriptionId": "1", "n": float64(n)}
}

// runManager receives from the response channel, works workPerMessage for each message, and counts them.
func runManager(s *session, workPerMessage time.Duration, stop chan struct{}) *atomic.Int32 {
	var got atomic.Int32
	go func() {
		for {
			select {
			case <-s.responses:
				time.Sleep(workPerMessage)
				got.Add(1)
			case <-stop:
				return
			}
		}
	}()
	return &got
}

// Whatever the core hands to the manager must never come back to the core as a request, however busy
// the manager is. (When the manager was busy, the core's own receive took the message back, and the core
// answered the client with an error, or treated an unsubscribe response as an unsubscribe request.)
func TestTransportDataSession_ResponsesAreNeverReadBackAsRequests(t *testing.T) {
	for _, c := range []struct {
		name  string
		n     int
		every time.Duration
		work  time.Duration
	}{
		{"steady, manager keeps up", 100, 5 * time.Millisecond, time.Millisecond},
		{"steady, manager is slow", 50, 5 * time.Millisecond, 20 * time.Millisecond},
		{"burst, manager is fast", 200, 0, time.Millisecond},
		{"burst, manager is slow", 200, 0, 5 * time.Millisecond},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := startSession()
			stop := make(chan struct{})
			defer close(stop)
			runManager(s, c.work, stop)
			time.Sleep(20 * time.Millisecond)
			for i := 0; i < c.n; i++ {
				s.backend <- event(i)
				if c.every > 0 {
					time.Sleep(c.every)
				}
			}
			time.Sleep(c.work*time.Duration(c.n) + 300*time.Millisecond)
			if n := len(s.toCore); n != 0 {
				var m map[string]interface{}
				select {
				case m = <-s.toCore:
				default:
				}
				b, _ := json.Marshal(m)
				t.Errorf("%d of %d responses were read back by the core as requests, e.g. %s", n, c.n, b)
			}
		})
	}
}

// A manager that keeps up gets every message, also when they come in a steady stream.
func TestTransportDataSession_ManagerThatKeepsUpReceivesEverything(t *testing.T) {
	s := startSession()
	stop := make(chan struct{})
	defer close(stop)
	got := runManager(s, time.Millisecond, stop)
	time.Sleep(20 * time.Millisecond)
	for i := 0; i < 200; i++ {
		s.backend <- event(i)
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond)
	if n := int(got.Load()); n != 200 {
		t.Errorf("the manager received %d of 200 messages", n)
	}
}

// Requests still go from the manager to the core.
func TestTransportDataSession_RequestsReachTheCore(t *testing.T) {
	s := startSession()
	s.requests <- `{"RouterId":"0?0","origin":"external","action":"get","path":"Vehicle.Speed","requestId":"1"}`
	select {
	case m := <-s.toCore:
		if m["path"] != "Vehicle.Speed" || m["requestId"] != "1" {
			t.Errorf("the core received %v", m)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the request did not reach the core")
	}
}

// A request that arrives while the core has responses to hand over is not lost, and a response is not
// taken for it: both directions are served at the same time.
func TestTransportDataSession_RequestsAndResponsesTogether(t *testing.T) {
	s := startSession()
	stop := make(chan struct{})
	defer close(stop)
	got := runManager(s, time.Millisecond, stop)
	time.Sleep(20 * time.Millisecond)
	const n = 100
	for i := 0; i < n; i++ {
		s.backend <- event(i)
		s.requests <- `{"RouterId":"0?0","origin":"external","action":"get","path":"Vehicle.Speed","requestId":"` + strconv.Itoa(i) + `"}`
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(500 * time.Millisecond)
	requests := 0
	for len(s.toCore) > 0 {
		m := <-s.toCore
		if m["action"] == "get" {
			requests++
		} else {
			t.Errorf("a message that is not a request reached the core: %v", m)
			break
		}
	}
	if requests != n {
		t.Errorf("%d of %d requests reached the core", requests, n)
	}
	if r := int(got.Load()); r < n-5 {
		t.Errorf("the manager received only %d of %d responses", r, n)
	}
}

// The dispatcher of the core being full must not stop the manager from receiving responses.
func TestTransportDataSession_FullDispatcherDoesNotStopResponses(t *testing.T) {
	s := startSession()
	for len(s.toCore) < cap(s.toCore) {
		s.toCore <- map[string]interface{}{"filler": true}
	}
	for i := 0; i < 5; i++ {
		s.requests <- `{"action":"get","path":"X"}` // dropped by the full dispatcher, must not block
	}
	stop := make(chan struct{})
	defer close(stop)
	got := runManager(s, 0, stop)
	time.Sleep(20 * time.Millisecond)
	for i := 0; i < 20; i++ {
		s.backend <- event(i)
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond)
	if n := int(got.Load()); n != 20 {
		t.Errorf("the manager received %d of 20 messages with the dispatcher full", n)
	}
}
