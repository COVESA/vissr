/**
* (C) 2026 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE
* file in this repository.
*
* Tests for the lifecycle of the reply-topic list (registerRequest /
* routeResponse). Every client request adds an entry that routes the server
* core's response(s) back to the client's reply topic. The entry must be
* released when the request is finished, otherwise the list grows for as long
* as the server runs, and every lookup walks it.
*
*   get / set / discover      one response -> released with it
*   subscribe                 kept for the events; the subscriptionId of the
*                             subscribe response is recorded on the entry
*   subscription (error)      the subscription is ended by the server -> released
*   unsubscribe               released together with the subscription it ended
*   unknown / not parseable   kept: dropping an entry would lose the route
*
* The message shapes are those that serviceMgr produces.
**/
package mqttMgr

import (
	"strconv"
	"testing"

	"github.com/covesa/vissr/utils"
)

const replyTopic = "VISS/client1"

// routerMsg wraps a server core message the way the server core sends it to the
// transport manager: with a RouterId of the form "mgrId?clientId".
func routerMsg(topicId int, body string) string {
	return `{"RouterId":"2?` + strconv.Itoa(topicId) + `",` + body + `}`
}

func listLen() int {
	topicListMu.Lock()
	defer topicListMu.Unlock()
	return topicList.nodes
}

// walkLen counts the entries by walking the list, to cross-check the counter.
func walkLen() int {
	topicListMu.Lock()
	defer topicListMu.Unlock()
	n := 0
	for it := topicList.head; it != nil; it = it.next {
		n++
	}
	return n
}

func assertListLen(t *testing.T, want int) {
	t.Helper()
	if got := listLen(); got != want {
		t.Fatalf("topic list nodes = %d; want %d", got, want)
	}
	if got := walkLen(); got != want {
		t.Fatalf("topic list walked length = %d; want %d (counter out of step)", got, want)
	}
}

// A get/set/discover request is one request, one response.
func TestTopicList_OneShotRequestsAreReleasedWithTheirResponse(t *testing.T) {
	for _, action := range []string{"get", "set", "discover"} {
		t.Run(action, func(t *testing.T) {
			resetTopicList()
			defer resetTopicList()
			registerRequest(replyTopic, 7, `{"action":"`+action+`","path":"Vehicle.Speed","requestId":"1"}`)
			assertListLen(t, 1)

			if got := routeResponse(7, routerMsg(7, `"action":"`+action+`","requestId":"1","ts":"x"`)); got != replyTopic {
				t.Fatalf("response routed to %q; want %q", got, replyTopic)
			}
			assertListLen(t, 0)
		})
	}
}

// An error response ends a request of any kind that has no subscription yet.
func TestTopicList_ErrorResponseReleasesTheEntry(t *testing.T) {
	for _, action := range []string{"get", "set", "subscribe", "unsubscribe"} {
		t.Run(action, func(t *testing.T) {
			resetTopicList()
			defer resetTopicList()
			registerRequest(replyTopic, 3, `{"action":"`+action+`","requestId":"1"}`)
			got := routeResponse(3, routerMsg(3, `"action":"`+action+`","requestId":"1","error":{"number":"400","reason":"bad_request","description":"x"},"ts":"x"`))
			if got != replyTopic {
				t.Fatalf("error response routed to %q; want %q", got, replyTopic)
			}
			assertListLen(t, 0)
		})
	}
}

// The reply must reach the client, and the entry must survive, for a
// subscription as long as it is running.
func TestTopicList_SubscribeEntryLivesAcrossEvents(t *testing.T) {
	resetTopicList()
	defer resetTopicList()
	registerRequest(replyTopic, 5, `{"action":"subscribe","path":"Vehicle.Speed","filter":{},"requestId":"2"}`)

	subResp := routerMsg(5, `"action":"subscribe","requestId":"2","subscriptionId":"11","ts":"x"`)
	if got := routeResponse(5, subResp); got != replyTopic {
		t.Fatalf("subscribe response routed to %q; want %q", got, replyTopic)
	}
	assertListLen(t, 1)

	event := routerMsg(5, `"action":"subscription","subscriptionId":"11","ts":"x","data":[{"path":"Vehicle.Speed","dp":{"value":"1","ts":"x"}}]`)
	for i := 0; i < 20; i++ {
		if got := routeResponse(5, event); got != replyTopic {
			t.Fatalf("event %d routed to %q; want %q", i, got, replyTopic)
		}
	}
	assertListLen(t, 1)
}

// A subscription that the server ends with an error event is finished.
func TestTopicList_SubscriptionErrorEventReleasesTheEntry(t *testing.T) {
	resetTopicList()
	defer resetTopicList()
	registerRequest(replyTopic, 5, `{"action":"subscribe","requestId":"2"}`)
	routeResponse(5, routerMsg(5, `"action":"subscribe","requestId":"2","subscriptionId":"11","ts":"x"`))
	assertListLen(t, 1)

	got := routeResponse(5, routerMsg(5, `"action":"subscription","subscriptionId":"11","error":{"number":"401","reason":"invalid_token","description":"Token expired or consent cancelled."},"ts":"x"`))
	if got != replyTopic {
		t.Fatalf("error event routed to %q; want %q", got, replyTopic)
	}
	assertListLen(t, 0)
}

// The unsubscribe response ends the subscription, so the subscribe entry and the
// unsubscribe entry are both released.
func TestTopicList_UnsubscribeReleasesSubscribeAndItsOwnEntry(t *testing.T) {
	resetTopicList()
	defer resetTopicList()
	registerRequest(replyTopic, 5, `{"action":"subscribe","requestId":"2"}`)
	routeResponse(5, routerMsg(5, `"action":"subscribe","requestId":"2","subscriptionId":"11","ts":"x"`))
	registerRequest(replyTopic, 6, `{"action":"unsubscribe","subscriptionId":"11","requestId":"3"}`)
	assertListLen(t, 2)

	got := routeResponse(6, routerMsg(6, `"action":"unsubscribe","requestId":"3","ts":"x"`))
	if got != replyTopic {
		t.Fatalf("unsubscribe response routed to %q; want %q", got, replyTopic)
	}
	assertListLen(t, 0)

	// Nothing is routed for the ended subscription any more.
	if got := routeResponse(5, routerMsg(5, `"action":"subscription","subscriptionId":"11","ts":"x"`)); got != "" {
		t.Errorf("event for an ended subscription routed to %q; want no route", got)
	}
}

// Ending one subscription must not touch another one, also not another client's.
func TestTopicList_UnsubscribeOnlyReleasesTheNamedSubscription(t *testing.T) {
	resetTopicList()
	defer resetTopicList()
	registerRequest("VISS/a", 1, `{"action":"subscribe","requestId":"1"}`)
	routeResponse(1, routerMsg(1, `"action":"subscribe","requestId":"1","subscriptionId":"10","ts":"x"`))
	registerRequest("VISS/b", 2, `{"action":"subscribe","requestId":"1"}`)
	routeResponse(2, routerMsg(2, `"action":"subscribe","requestId":"1","subscriptionId":"11","ts":"x"`))
	registerRequest("VISS/a", 3, `{"action":"unsubscribe","subscriptionId":"10","requestId":"2"}`)
	assertListLen(t, 3)

	routeResponse(3, routerMsg(3, `"action":"unsubscribe","requestId":"2","ts":"x"`))
	assertListLen(t, 1)
	if got := routeResponse(2, routerMsg(2, `"action":"subscription","subscriptionId":"11","ts":"x"`)); got != "VISS/b" {
		t.Errorf("the other subscription's event routed to %q; want VISS/b", got)
	}
}

// If the unsubscribe fails (e.g. unknown subscriptionId) the subscription is
// still running, so only the unsubscribe entry itself is released.
func TestTopicList_FailedUnsubscribeKeepsTheSubscription(t *testing.T) {
	resetTopicList()
	defer resetTopicList()
	registerRequest(replyTopic, 5, `{"action":"subscribe","requestId":"2"}`)
	routeResponse(5, routerMsg(5, `"action":"subscribe","requestId":"2","subscriptionId":"11","ts":"x"`))
	registerRequest(replyTopic, 6, `{"action":"unsubscribe","subscriptionId":"99","requestId":"3"}`)

	routeResponse(6, routerMsg(6, `"action":"unsubscribe","requestId":"3","error":{"number":"400","reason":"invalid_data","description":""},"subscriptionId":"99","ts":"x"`))
	assertListLen(t, 1)
	if got := routeResponse(5, routerMsg(5, `"action":"subscription","subscriptionId":"11","ts":"x"`)); got != replyTopic {
		t.Errorf("running subscription's event routed to %q; want %q", got, replyTopic)
	}
}

// Unsubscribing a subscription id that belongs to nothing must not release any
// other entry, in particular not one that has no subscriptionId yet.
func TestTopicList_UnsubscribeOfUnknownIdReleasesNothingElse(t *testing.T) {
	resetTopicList()
	defer resetTopicList()
	registerRequest(replyTopic, 5, `{"action":"subscribe","requestId":"2"}`) // response not yet received
	registerRequest(replyTopic, 6, `{"action":"unsubscribe","subscriptionId":"","requestId":"3"}`)

	routeResponse(6, routerMsg(6, `"action":"unsubscribe","requestId":"3","ts":"x"`))
	assertListLen(t, 1)
}

// Anything that cannot be understood must keep its route: losing a route silently
// drops every later event of a subscription, a leaked entry only costs memory.
func TestTopicList_UnparseableMessagesKeepTheEntry(t *testing.T) {
	cases := map[string]string{
		"not json":                   `{"RouterId":"2?9", not json`,
		"missing action":             routerMsg(9, `"requestId":"1"`),
		"non-string action":          routerMsg(9, `"action":42,"requestId":"1"`),
		"unknown action":             routerMsg(9, `"action":"frobnicate","requestId":"1"`),
		"multi-event service action": routerMsg(9, `"action":"invoke","status":"ONGOING","requestId":"1"`),
	}
	for name, msg := range cases {
		t.Run(name, func(t *testing.T) {
			resetTopicList()
			defer resetTopicList()
			registerRequest(replyTopic, 9, `{"action":"get","requestId":"1"}`)
			if got := routeResponse(9, msg); got != replyTopic {
				t.Fatalf("routed to %q; want %q", got, replyTopic)
			}
			assertListLen(t, 1)
		})
	}
}

func TestTopicList_UnknownTopicIdHasNoRoute(t *testing.T) {
	resetTopicList()
	defer resetTopicList()
	registerRequest(replyTopic, 1, `{"action":"get"}`)
	if got := routeResponse(2, routerMsg(2, `"action":"get","requestId":"1"`)); got != "" {
		t.Errorf("routed to %q; want no route", got)
	}
	assertListLen(t, 1)
}

// The regression this file exists for: a long run of requests must not grow the list.
func TestTopicList_DoesNotGrowOverManyRequests(t *testing.T) {
	resetTopicList()
	defer resetTopicList()
	for id := 0; id < 5000; id++ {
		registerRequest(replyTopic, id, `{"action":"get","path":"Vehicle.Speed","requestId":"1"}`)
		if got := routeResponse(id, routerMsg(id, `"action":"get","requestId":"1","ts":"x"`)); got != replyTopic {
			t.Fatalf("request %d: response routed to %q; want %q", id, got, replyTopic)
		}
	}
	assertListLen(t, 0)
}

// Many subscribe/unsubscribe cycles must not grow the list either.
func TestTopicList_DoesNotGrowOverSubscribeUnsubscribeCycles(t *testing.T) {
	resetTopicList()
	defer resetTopicList()
	id := 0
	for i := 0; i < 1000; i++ {
		subId := strconv.Itoa(i)
		registerRequest(replyTopic, id, `{"action":"subscribe","requestId":"1"}`)
		routeResponse(id, routerMsg(id, `"action":"subscribe","requestId":"1","subscriptionId":"`+subId+`","ts":"x"`))
		subTopicId := id
		id++
		registerRequest(replyTopic, id, `{"action":"unsubscribe","subscriptionId":"`+subId+`","requestId":"2"}`)
		if got := routeResponse(id, routerMsg(id, `"action":"unsubscribe","requestId":"2","ts":"x"`)); got != replyTopic {
			t.Fatalf("cycle %d: unsubscribe response routed to %q; want %q", i, got, replyTopic)
		}
		id++
		if got := getTopic(subTopicId); got != "" {
			t.Fatalf("cycle %d: subscribe entry still present", i)
		}
	}
	assertListLen(t, 0)
}

// A client that subscribes and then disappears leaves its entry behind; this is
// the known limitation (the broker gives no per-client disconnect signal here).
// The test pins that such an entry is retained and still routes, so a later
// change that adds expiry has to update it deliberately.
func TestTopicList_AbandonedSubscriptionIsRetained(t *testing.T) {
	resetTopicList()
	defer resetTopicList()
	registerRequest(replyTopic, 5, `{"action":"subscribe","requestId":"2"}`)
	routeResponse(5, routerMsg(5, `"action":"subscribe","requestId":"2","subscriptionId":"11","ts":"x"`))
	assertListLen(t, 1)
	if got := routeResponse(5, routerMsg(5, `"action":"subscription","subscriptionId":"11","ts":"x"`)); got != replyTopic {
		t.Errorf("event routed to %q; want %q", got, replyTopic)
	}
}

// fromServerCore mirrors what MqttMgrInit does with a message from the server core: the
// message is a map that has been through utils.FinalizeMessage, and the router id is
// removed by utils.RemoveInternalData before the topic is looked up.
func fromServerCore(msg map[string]interface{}) string {
	payload, topicId := utils.RemoveInternalData(utils.FinalizeMessage(msg))
	return routeResponse(topicId, payload)
}

// The whole life of a subscription with messages in the form the server core produces them.
func TestTopicList_RealMessageShapesThroughRemoveInternalData(t *testing.T) {
	resetTopicList()
	defer resetTopicList()

	registerRequest(replyTopic, 4, `{"action":"subscribe","path":"Vehicle.Speed","filter":{"variant":"timebased","parameter":{"period":"1000"}},"requestId":"246"}`)
	if got := fromServerCore(map[string]interface{}{"RouterId": "2?4", "action": "subscribe", "requestId": "246", "subscriptionId": "3", "ts": "t"}); got != replyTopic {
		t.Fatalf("subscribe response routed to %q; want %q", got, replyTopic)
	}
	for i := 0; i < 3; i++ {
		ev := map[string]interface{}{"RouterId": "2?4", "action": "subscription", "subscriptionId": "3", "ts": "t",
			"data": []interface{}{map[string]interface{}{"path": "Vehicle.Speed", "dp": map[string]interface{}{"value": "1", "ts": "t"}}}}
		if got := fromServerCore(ev); got != replyTopic {
			t.Fatalf("event %d routed to %q; want %q", i, got, replyTopic)
		}
	}
	assertListLen(t, 1)

	registerRequest(replyTopic, 5, `{"action":"unsubscribe","subscriptionId":"3","requestId":"247"}`)
	if got := fromServerCore(map[string]interface{}{"RouterId": "2?5", "action": "unsubscribe", "requestId": "247", "ts": "t"}); got != replyTopic {
		t.Fatalf("unsubscribe response routed to %q; want %q", got, replyTopic)
	}
	assertListLen(t, 0)

	registerRequest(replyTopic, 6, `{"action":"get","path":"Vehicle.Speed","requestId":"1"}`)
	if got := fromServerCore(map[string]interface{}{"RouterId": "2?6", "action": "get", "requestId": "1", "ts": "t",
		"data": map[string]interface{}{"path": "Vehicle.Speed", "dp": map[string]interface{}{"value": "1", "ts": "t"}}}); got != replyTopic {
		t.Fatalf("get response routed to %q; want %q", got, replyTopic)
	}
	assertListLen(t, 0)
}

// A request that the server core rejects with the error shape of utils.SetErrorResponse.
func TestTopicList_RealErrorResponseShape(t *testing.T) {
	resetTopicList()
	defer resetTopicList()
	registerRequest(replyTopic, 8, `{"action":"get","path":"Nope","requestId":"9"}`)
	errMap := map[string]interface{}{}
	utils.SetErrorResponse(map[string]interface{}{"RouterId": "2?8", "action": "get", "requestId": "9"}, errMap, 6, "")
	if got := fromServerCore(errMap); got != replyTopic {
		t.Fatalf("error response routed to %q; want %q", got, replyTopic)
	}
	assertListLen(t, 0)
}
