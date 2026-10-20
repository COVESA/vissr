/**
* (C) 2026 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE
* file in this repository.
*
* Tests of MqttMgrInit wired to the server core the way vissv2server wires them: one UNBUFFERED
* channel (transportMgrChannel[2]) that the manager sends its requests on, that the server core's
* transportDataSession receives them from, and that transportDataSession sends the core's responses
* back on (without blocking: a response is dropped if nobody is waiting to receive it).
**/
package mqttMgr

import (
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/covesa/vissr/utils"
)

// transportDataSessionModel has the shape of vissv2server.transportDataSession. It is a copy: the real
// function is in package main and cannot be imported, so keep the two in step.
//
// blockingResponses selects how a response from the core is handed to the manager. false is what the
// server does today: a send that is dropped unless the manager is receiving at that very moment.
// true waits up to a second for the manager.
func transportDataSessionModel(transportMgrChannel chan string, transportDataChannel chan map[string]interface{}, backendChannel chan map[string]interface{}, blockingResponses bool) {
	for {
		select {
		case msg := <-transportMgrChannel:
			var msgMap map[string]interface{}
			utils.MapRequest(msg, &msgMap)
			select {
			case transportDataChannel <- msgMap:
			default:
			}
		case message := <-backendChannel:
			resp := utils.FinalizeMessage(message)
			if blockingResponses {
				select {
				case transportMgrChannel <- resp:
				case <-time.After(time.Second):
				}
				continue
			}
			select {
			case transportMgrChannel <- resp:
			default:
			}
		}
	}
}

// wiringResult tells how the n requests ended: reached is how many the server core received.
type wiringResult struct{ reached, answered, echoed, lost int }

// runWiring starts MqttMgrInit against a model of the server core that answers every request, delivers
// n client requests through the broker handler in bursts of perBurst, and reports how each one ended:
//
//	answered: the reply topic of the client got a response
//	echoed:   the reply topic got the client's own REQUEST back, as if it were the response
//	lost:     neither
func runWiring(t *testing.T, n, perBurst int, gap time.Duration, blockingResponses bool) wiringResult {
	t.Helper()
	loadSchema(t)
	withFastStartup(t)
	t.Setenv("MQTT_VIN", "TESTVIN001")
	resetTopicList()
	t.Cleanup(resetTopicList)
	made := useMockClient(t, func(m *mockClient) { m.pubSignal = make(chan published, 8192) })

	transport := make(chan string) // unbuffered, like transportMgrChannel[2]
	toCore := make(chan map[string]interface{}, 64)
	backend := make(chan map[string]interface{}, 64)
	go transportDataSessionModel(transport, toCore, backend, blockingResponses)
	var reached atomic.Int32
	go func() { // the core: answers each request
		for req := range toCore {
			reached.Add(1)
			resp := map[string]interface{}{"RouterId": req["RouterId"], "action": req["action"], "requestId": req["requestId"], "ts": "t"}
			go func() { time.Sleep(5 * time.Millisecond); backend <- resp }()
		}
	}()
	time.Sleep(50 * time.Millisecond) // the core is receiving before the manager starts, as in the server
	go MqttMgrInit(2, transport)
	m := made.waitSubscribed(t, 10*time.Second)

	for i := 0; i < n; i += perBurst {
		for j := 0; j < perBurst && i+j < n; j++ {
			k := i + j
			m.opts.DefaultPublishHandler(nil, &mockMessage{
				payload: []byte(fmt.Sprintf(`{"topic":"VISS/c%d","request":{"action":"get","path":"Vehicle.Speed","requestId":"%d"}}`, k, k)),
				topic:   "/TESTVIN001/Vehicle",
			})
		}
		time.Sleep(gap)
	}
	time.Sleep(700 * time.Millisecond)

	var res wiringResult
	answered := map[string]bool{}
	for _, p := range m.pubs() {
		switch {
		case strings.Contains(p.payload, `"path":"Vehicle.Speed"`):
			res.echoed++
		case strings.Contains(p.payload, `"requestId"`):
			answered[p.topic] = true
		}
	}
	res.reached = int(reached.Load())
	res.answered = len(answered)
	res.lost = n - res.answered - res.echoed
	return res
}

// One request at a time, with the core's response send as it is in the server (non blocking). That send
// is dropped if the manager is not parked in a receive at that very instant, which the scheduler can
// cause now and then (more often under the race detector), so a lost response is not a failure of the
// manager and is not asserted on here. What the manager controls is checked: every request reaches the
// core, and none is published back to the client.
func TestMqttMgrInit_RequestsOneAtATimeAllReachTheCore(t *testing.T) {
	r := runWiring(t, 40, 1, 30*time.Millisecond, false)
	if r.reached != 40 || r.echoed != 0 {
		t.Errorf("40 requests, one at a time: %+v; want all 40 to reach the core and none echoed", r)
	}
	if r.answered < 36 {
		t.Errorf("only %d of 40 requests answered: %+v", r.answered, r)
	}
}

// Same, with the core handing over responses reliably: all are answered.
func TestMqttMgrInit_RequestsOneAtATimeAreAllAnsweredWhenTheCoreDoesNotDropResponses(t *testing.T) {
	r := runWiring(t, 40, 1, 30*time.Millisecond, true)
	if r.reached != 40 || r.answered != 40 || r.echoed != 0 || r.lost != 0 {
		t.Errorf("40 requests, one at a time: %+v; want all answered", r)
	}
}

// Concurrent requests. What the manager itself has to guarantee, whatever the core does: every request
// reaches the core, and a client's own request is never published back to it as the response. (It used
// to be: the manager had a second goroutine receiving on the shared channel, and took about half of its
// own requests when two were in flight.)
func TestMqttMgrInit_ConcurrentRequestsAllReachTheCoreAndAreNeverEchoed(t *testing.T) {
	for _, burst := range []int{2, 3, 5} {
		t.Run(fmt.Sprintf("burst%d", burst), func(t *testing.T) {
			r := runWiring(t, 60, burst, 150*time.Millisecond, false)
			if r.echoed != 0 {
				t.Errorf("%d of 60 requests were published back to the client as the response: %+v", r.echoed, r)
			}
			if r.reached != 60 {
				t.Errorf("only %d of 60 requests reached the core: %+v", r.reached, r)
			}
			t.Logf("with the core's non blocking response send, as in the server today: %+v", r)
		})
	}
}

// When the core hands its responses over reliably (it waits for the manager instead of dropping), every
// concurrent request is answered: nothing is lost on the manager's side.
func TestMqttMgrInit_ConcurrentRequestsAreAllAnsweredWhenTheCoreDoesNotDropResponses(t *testing.T) {
	for _, burst := range []int{2, 3, 5} {
		t.Run(fmt.Sprintf("burst%d", burst), func(t *testing.T) {
			r := runWiring(t, 60, burst, 150*time.Millisecond, true)
			if r.answered != 60 || r.echoed != 0 || r.lost != 0 {
				t.Errorf("60 requests in bursts of %d: %+v; want all answered", burst, r)
			}
		})
	}
}
