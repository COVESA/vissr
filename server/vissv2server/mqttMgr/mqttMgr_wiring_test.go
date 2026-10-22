/**
* (C) 2026 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE
* file in this repository.
*
* Tests of MqttMgrInit wired to a model of the server core the way vissv2server wires them: the manager
* sends its requests to the core on one channel and receives the responses on another. The model of the core
* answers every request it receives. The manager is driven through the handler that mqttSubscribe installs
* on the (mock) broker client.
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

type wiringResult struct{ reached, answered, echoed, lost int }

// runWiring starts MqttMgrInit against a model of the server core that answers every request, delivers n
// client requests through the broker handler in bursts of perBurst, and reports how each one ended:
//
//	reached:  the server core received the request
//	answered: the reply topic of the client got a response
//	echoed:   the reply topic got the client's own REQUEST back, as if it were the response
//	lost:     neither answered nor echoed
func runWiring(t *testing.T, n, perBurst int, gap time.Duration) wiringResult {
	t.Helper()
	loadSchema(t)
	withFastStartup(t)
	t.Setenv("MQTT_VIN", "TESTVIN001")
	resetTopicList()
	t.Cleanup(resetTopicList)
	made := useMockClient(t, func(m *mockClient) { m.pubSignal = make(chan published, 8192) })

	requests := make(chan string, 32)  // manager -> core
	responses := make(chan string, 32) // core -> manager
	var reached atomic.Int32
	go func() { // the core: answers each request
		for req := range requests {
			reached.Add(1)
			var m map[string]interface{}
			utils.MapRequest(req, &m)
			resp := map[string]interface{}{"RouterId": m["RouterId"], "action": m["action"], "requestId": m["requestId"], "ts": "t"}
			go func() { time.Sleep(5 * time.Millisecond); responses <- utils.FinalizeMessage(resp) }()
		}
	}()
	go MqttMgrInit(2, requests, responses)
	mock := made.waitSubscribed(t, 10*time.Second)

	for i := 0; i < n; i += perBurst {
		for j := 0; j < perBurst && i+j < n; j++ {
			k := i + j
			mock.opts.DefaultPublishHandler(nil, &mockMessage{
				payload: []byte(fmt.Sprintf(`{"topic":"VISS/c%d","request":{"action":"get","path":"Vehicle.Speed","requestId":"%d"}}`, k, k)),
				topic:   "/TESTVIN001/Vehicle",
			})
		}
		time.Sleep(gap)
	}
	time.Sleep(700 * time.Millisecond)

	var res wiringResult
	answered := map[string]bool{}
	for _, p := range mock.pubs() {
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

// One request at a time: every request reaches the core and every response reaches its client.
func TestMqttMgrInit_RequestsOneAtATimeAreAllAnswered(t *testing.T) {
	r := runWiring(t, 40, 1, 30*time.Millisecond)
	if r.reached != 40 || r.answered != 40 || r.echoed != 0 || r.lost != 0 {
		t.Errorf("40 requests, one at a time: %+v; want all answered", r)
	}
}

// Concurrent requests: every one reaches the core and is answered, and a client's own request is never
// published back to it as the response. (It used to be, about every second one, when the requests and the
// responses shared one channel and the manager had a second receiver on it.)
func TestMqttMgrInit_ConcurrentRequestsAreAllAnswered(t *testing.T) {
	for _, burst := range []int{2, 3, 5} {
		t.Run(fmt.Sprintf("burst%d", burst), func(t *testing.T) {
			r := runWiring(t, 60, burst, 150*time.Millisecond)
			if r.reached != 60 || r.answered != 60 || r.echoed != 0 || r.lost != 0 {
				t.Errorf("60 requests in bursts of %d: %+v; want all answered", burst, r)
			}
		})
	}
}
