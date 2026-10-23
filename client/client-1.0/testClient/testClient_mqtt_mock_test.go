/**
* (C) 2026 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE
* file in this repository.
*
* Tests of the MQTT part of testClient against a mock MQTT client, so no broker is needed.
**/
package main

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	MQTT "github.com/eclipse/paho.mqtt.golang"
)

type mockToken struct{ err error }

func (t *mockToken) Wait() bool                     { return true }
func (t *mockToken) WaitTimeout(time.Duration) bool { return true }
func (t *mockToken) Done() <-chan struct{}          { c := make(chan struct{}); close(c); return c }
func (t *mockToken) Error() error                   { return t.err }

type published struct{ topic, payload string }

// mockClient implements MQTT.Client. Embedding the interface leaves the methods that are not used
// unimplemented, so a call to one of them fails the test loudly.
type mockClient struct {
	MQTT.Client
	opts *MQTT.ClientOptions

	mu           sync.Mutex
	connectErr   error
	subscribeErr error
	publishErr   error
	subscribed   []string
	published    []published
	disconnects  int
}

func (m *mockClient) Connect() MQTT.Token { return &mockToken{err: m.connectErr} }

func (m *mockClient) Subscribe(topic string, qos byte, cb MQTT.MessageHandler) MQTT.Token {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subscribed = append(m.subscribed, topic)
	return &mockToken{err: m.subscribeErr}
}

func (m *mockClient) Publish(topic string, qos byte, retained bool, payload interface{}) MQTT.Token {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.published = append(m.published, published{topic, payload.(string)})
	return &mockToken{err: m.publishErr}
}

func (m *mockClient) Disconnect(quiesce uint) {
	m.mu.Lock()
	m.disconnects++
	m.mu.Unlock()
}

type madeClients struct {
	mu      sync.Mutex
	clients []*mockClient
}

func (m *madeClients) all() []*mockClient {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*mockClient(nil), m.clients...)
}

// useMock makes the client functions create mock clients, and returns the record of them.
func useMock(t *testing.T, configure func(*mockClient)) *madeClients {
	t.Helper()
	made := &madeClients{}
	old := newMqttClient
	newMqttClient = func(opts *MQTT.ClientOptions) MQTT.Client {
		m := &mockClient{opts: opts}
		if configure != nil {
			configure(m)
		}
		made.mu.Lock()
		made.clients = append(made.clients, m)
		made.mu.Unlock()
		return m
	}
	t.Cleanup(func() { newMqttClient = old })
	return made
}

func TestMqttSubscribe_SuccessReturnsTheConnectedClient(t *testing.T) {
	made := useMock(t, nil)
	c, err := mqttSubscribe("tcp://10.0.0.1:1883", "VISS/testClient")
	if err != nil || c == nil {
		t.Fatalf("got (%v, %v); want a client and no error", c, err)
	}
	m := made.all()[0]
	if len(m.subscribed) != 1 || m.subscribed[0] != "VISS/testClient" {
		t.Errorf("subscriptions = %v; want [VISS/testClient], without quote characters around it", m.subscribed)
	}
	if m.disconnects != 0 {
		t.Errorf("disconnects = %d; the caller keeps the client for the subscription", m.disconnects)
	}
	if m.opts.DefaultPublishHandler == nil {
		t.Error("no handler for the messages from the broker")
	}
}

func TestMqttSubscribe_ConnectErrorIsReturned(t *testing.T) {
	made := useMock(t, func(m *mockClient) { m.connectErr = errors.New("connection refused") })
	if c, err := mqttSubscribe("tcp://x:1883", "VISS/testClient"); err == nil || c != nil {
		t.Fatalf("got (%v, %v); want no client and the error", c, err)
	}
	if n := len(made.all()[0].subscribed); n != 0 {
		t.Errorf("subscribed %d times although the connect failed", n)
	}
}

func TestMqttSubscribe_SubscribeErrorDisconnectsAndIsReturned(t *testing.T) {
	made := useMock(t, func(m *mockClient) { m.subscribeErr = errors.New("not authorized") })
	if c, err := mqttSubscribe("tcp://x:1883", "VISS/testClient"); err == nil || c != nil {
		t.Fatalf("got (%v, %v); want no client and the error", c, err)
	}
	if d := made.all()[0].disconnects; d != 1 {
		t.Errorf("disconnects = %d; want 1", d)
	}
}

func TestPublishMessage_PublishesAndDisconnects(t *testing.T) {
	made := useMock(t, nil)
	if err := publishMessage("tcp://x:1883", "/V/Vehicle", `{"a":1}`); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := made.all()[0]
	if len(m.published) != 1 || m.published[0] != (published{"/V/Vehicle", `{"a":1}`}) {
		t.Errorf("published = %+v", m.published)
	}
	if m.disconnects != 1 {
		t.Errorf("disconnects = %d; want 1", m.disconnects)
	}
}

func TestPublishMessage_ErrorsAreReturned(t *testing.T) {
	made := useMock(t, func(m *mockClient) { m.connectErr = errors.New("connection refused") })
	if err := publishMessage("tcp://x:1883", "/V/Vehicle", "{}"); err == nil {
		t.Error("want the connect error")
	}
	if n := len(made.all()[0].published); n != 0 {
		t.Errorf("published %d messages without a connection", n)
	}
	useMock(t, func(m *mockClient) { m.publishErr = errors.New("not connected") })
	if err := publishMessage("tcp://x:1883", "/V/Vehicle", "{}"); err == nil {
		t.Error("want the publish error: it was ignored before, and the request counted as sent")
	}
}

func TestPublishVissV2Request_PublishesTheEnvelopeToTheServerTopicAsIs(t *testing.T) {
	made := useMock(t, nil)
	if err := publishVissV2Request("tcp://x:1883", `{"action":"get","path":"Vehicle.Speed"}`, "VISS/testClient", "/VIN123/Vehicle"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	p := made.all()[0].published[0]
	if p.topic != "/VIN123/Vehicle" {
		t.Errorf("topic = %q; want /VIN123/Vehicle (a single leading slash)", p.topic)
	}
	if !strings.Contains(p.payload, `"topic":"VISS/testClient"`) || !strings.Contains(p.payload, `"path":"Vehicle.Speed"`) {
		t.Errorf("payload = %s", p.payload)
	}
}

// With the broker down only the MQTT test fails: the function must return, signal "not ok", and not end
// the process, and it must not try to publish.
func TestMqttTesterRun_BrokerDownSignalsFailureAndReturns(t *testing.T) {
	made := useMock(t, func(m *mockClient) { m.connectErr = errors.New("connection refused") })
	done := make(chan bool, 1)
	finished := make(chan struct{})
	go func() { mqttTesterRun([]string{`{"action":"get","path":"Vehicle.Speed"}`}, done); close(finished) }()
	select {
	case ok := <-done:
		if ok {
			t.Error("the MQTT test reported success with the broker down")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("mqttTesterRun did not signal that it was done")
	}
	<-finished
	for _, m := range made.all() {
		if len(m.published) != 0 {
			t.Errorf("published %d messages with the broker down", len(m.published))
		}
	}
}

// A request that cannot be sent makes the MQTT test fail, but the rest of the requests are still tried
// and the test still signals that it is done.
func TestMqttTesterRun_FailedPublishFailsTheTestButStillSignals(t *testing.T) {
	t.Setenv("MQTT_VIN", "")
	created := 0
	var mu sync.Mutex
	old := newMqttClient
	newMqttClient = func(opts *MQTT.ClientOptions) MQTT.Client {
		mu.Lock()
		created++
		first := created == 1 // the first client is the subscriber; the later ones publish
		mu.Unlock()
		m := &mockClient{opts: opts}
		if !first {
			m.publishErr = errors.New("not connected")
		}
		return m
	}
	t.Cleanup(func() { newMqttClient = old })

	done := make(chan bool, 1)
	go mqttTesterRun([]string{`{"action":"get","path":"A"}`}, done)
	select {
	case ok := <-done:
		if ok {
			t.Error("the MQTT test reported success although the request could not be published")
		}
	case <-time.After(20 * time.Second):
		t.Fatal("mqttTesterRun did not signal that it was done")
	}
}

// The happy path: subscribe to the reply topic first, then publish each request, and disconnect the
// subscriber when done.
func TestMqttTesterRun_SubscribesThenPublishesEveryRequest(t *testing.T) {
	t.Setenv("MQTT_VIN", "TESTVIN001")
	made := useMock(t, nil)
	done := make(chan bool, 1)
	go mqttTesterRun([]string{`{"action":"get","path":"A"}`, `{"action":"get","path":"B"}`}, done)
	select {
	case ok := <-done:
		if !ok {
			t.Error("the MQTT test reported failure")
		}
	case <-time.After(30 * time.Second):
		t.Fatal("mqttTesterRun did not finish")
	}
	clients := made.all()
	if len(clients) != 3 { // the subscriber and one publisher per request
		t.Fatalf("clients created = %d; want 3", len(clients))
	}
	if len(clients[0].subscribed) != 1 || clients[0].subscribed[0] != "VISS/testClient" {
		t.Errorf("the first client subscribed to %v; want [VISS/testClient]", clients[0].subscribed)
	}
	for i, path := range []string{`"path":"A"`, `"path":"B"`} {
		c := clients[i+1]
		if len(c.published) != 1 || c.published[0].topic != "/TESTVIN001/Vehicle" || !strings.Contains(c.published[0].payload, path) {
			t.Errorf("publisher %d published %+v", i+1, c.published)
		}
	}
	time.Sleep(50 * time.Millisecond)
	clients[0].mu.Lock()
	d := clients[0].disconnects
	clients[0].mu.Unlock()
	if d != 1 {
		t.Errorf("the subscriber was disconnected %d times; want 1", d)
	}
}

// The kind of a message from the server comes from its action. A response to something else that has the
// word "subscribe" in a value is not a subscribe response and not an event.
func TestMessageKind(t *testing.T) {
	cases := map[string]string{
		`{"action":"subscribe","requestId":"1","subscriptionId":"7","ts":"t"}`:                                              "subscribe",
		`{"action":"subscription","subscriptionId":"7","data":[{"path":"Vehicle.Speed","dp":{"value":"5","ts":"t"}}]}`:      "subscription",
		`{"action":"subscribe","error":{"number":"400","reason":"bad_request","description":"x"},"requestId":"1","ts":"t"}`: "subscribe",
		`{"action":"subscription","error":{"number":"401","reason":"invalid_token","description":"x"},"ts":"t"}`:            "subscription",
		`{"action":"get","requestId":"1","data":{"path":"Vehicle.X","dp":{"value":"subscribe","ts":"t"}}}`:                  "",
		`{"action":"get","requestId":"1","data":{"path":"Vehicle.X","dp":{"value":"my subscription","ts":"t"}}}`:            "",
		`{"action":"unsubscribe","requestId":"123","ts":"t"}`:                                                               "",
		`{"action":"set","requestId":"1","ts":"t"}`:                                                                         "",
		`{"action":"discover","metadata":{"subscribe":"x"},"ts":"t"}`:                                                       "",
		`{"requestId":"1"}`: "",
		`{"action":5}`:      "",
		`not json`:          "",
		``:                  "",
	}
	for payload, want := range cases {
		if got := messageKind([]byte(payload)); got != want {
			t.Errorf("messageKind(%s) = %q; want %q", payload, got, want)
		}
	}
}

// The handler counts the subscribe response and the events, and only those, so that the unsubscribe is sent
// after the right number of events.
func TestPublishHandler_OnlyRealEventsAreCounted(t *testing.T) {
	made := useMock(t, nil)
	oldEvents, oldMax, oldBroker, oldCT, oldST := events, maxEvents, brokerSocket, clientTopic, serverTopic
	t.Cleanup(func() {
		events, maxEvents, brokerSocket, clientTopic, serverTopic = oldEvents, oldMax, oldBroker, oldCT, oldST
	})
	events, maxEvents = 0, 2
	brokerSocket, clientTopic, serverTopic = "tcp://x:1883", "VISS/testClient", "/VIN123/Vehicle"

	send := func(payload string) { publishHandler(nil, &mockMessage{payload: []byte(payload)}) }
	send(`{"action":"get","requestId":"1","data":{"path":"V.X","dp":{"value":"subscribe","ts":"t"}}}`)
	send(`{"action":"get","requestId":"2","data":{"path":"V.X","dp":{"value":"a subscription","ts":"t"}}}`)
	if events != 0 {
		t.Fatalf("responses to get requests counted as %d events", events)
	}
	send(`{"action":"subscribe","requestId":"3","subscriptionId":"7","ts":"t"}`)
	send(`{"action":"subscription","subscriptionId":"7","ts":"t"}`)
	if n := len(made.all()); n != 0 {
		t.Fatalf("the unsubscribe was sent after only %d of the %d events", events-1, maxEvents)
	}
	send(`{"action":"subscription","subscriptionId":"7","ts":"t"}`)
	clients := made.all()
	if len(clients) != 1 || len(clients[0].published) != 1 || !strings.Contains(clients[0].published[0].payload, `"action":"unsubscribe"`) ||
		!strings.Contains(clients[0].published[0].payload, `"subscriptionId":"7"`) {
		t.Errorf("after %d events the unsubscribe request was not sent as expected: %+v", maxEvents, clients)
	}
}

type mockMessage struct{ payload []byte }

func (m *mockMessage) Duplicate() bool   { return false }
func (m *mockMessage) Qos() byte         { return 0 }
func (m *mockMessage) Retained() bool    { return false }
func (m *mockMessage) Topic() string     { return "VISS/testClient" }
func (m *mockMessage) MessageID() uint16 { return 0 }
func (m *mockMessage) Payload() []byte   { return m.payload }
func (m *mockMessage) Ack()              {}
