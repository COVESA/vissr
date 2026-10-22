/**
* (C) 2026 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE
* file in this repository.
*
* Tests for the broker facing part of the MQTT manager: mqttSubscribe, the server loop
* (runHub, handleBrokerMessage, handleServerCoreMessage) and MqttMgrInit. A mock
* MQTT client takes the place of the paho client, so no broker is needed.
**/
package mqttMgr

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/covesa/vissr/utils"
	MQTT "github.com/eclipse/paho.mqtt.golang"
)

// ---------------------------------------------------------------------------
// mock client
// ---------------------------------------------------------------------------

type mockToken struct{ err error }

func (t *mockToken) Wait() bool                     { return true }
func (t *mockToken) WaitTimeout(time.Duration) bool { return true }
func (t *mockToken) Done() <-chan struct{}          { c := make(chan struct{}); close(c); return c }
func (t *mockToken) Error() error                   { return t.err }

type published struct {
	topic   string
	payload string
}

type subscription struct {
	topic string
	qos   byte
}

// mockClient implements MQTT.Client. Embedding the interface leaves the methods that
// the manager does not use unimplemented; calling one of them panics, so a test fails
// loudly if the manager starts using something that is not mocked.
type mockClient struct {
	MQTT.Client
	opts *MQTT.ClientOptions

	mu            sync.Mutex
	connectErr    error
	subscribeErr  error
	publishErr    error
	connected     int
	subscriptions []subscription
	publications  []published
	disconnects   int
	pubSignal     chan published // receives each publication, if not nil
}

func (m *mockClient) Connect() MQTT.Token {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connected++
	return &mockToken{err: m.connectErr}
}

func (m *mockClient) Subscribe(topic string, qos byte, cb MQTT.MessageHandler) MQTT.Token {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subscriptions = append(m.subscriptions, subscription{topic, qos})
	return &mockToken{err: m.subscribeErr}
}

func (m *mockClient) Publish(topic string, qos byte, retained bool, payload interface{}) MQTT.Token {
	m.mu.Lock()
	p := published{topic: topic, payload: payload.(string)}
	m.publications = append(m.publications, p)
	sig := m.pubSignal
	err := m.publishErr
	m.mu.Unlock()
	if sig != nil {
		sig <- p
	}
	return &mockToken{err: err}
}

func (m *mockClient) Disconnect(quiesce uint) {
	m.mu.Lock()
	m.disconnects++
	m.mu.Unlock()
}

func (m *mockClient) IsConnected() bool { return true }

func (m *mockClient) pubs() []published {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]published(nil), m.publications...)
}

func (m *mockClient) subs() []subscription {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]subscription(nil), m.subscriptions...)
}

// madeClients records the mock clients that mqttSubscribe has created. The manager creates them in its
// own goroutine, so access goes through the lock.
type madeClients struct {
	mu      sync.Mutex
	clients []*mockClient
}

func (m *madeClients) add(c *mockClient) {
	m.mu.Lock()
	m.clients = append(m.clients, c)
	m.mu.Unlock()
}

func (m *madeClients) count() int { m.mu.Lock(); defer m.mu.Unlock(); return len(m.clients) }

// first returns the first client made, or nil if there is none yet.
func (m *madeClients) first() *mockClient {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.clients) == 0 {
		return nil
	}
	return m.clients[0]
}

// waitSubscribed waits until the first client has subscribed, and returns it.
func (m *madeClients) waitSubscribed(t *testing.T, timeout time.Duration) *mockClient {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if c := m.first(); c != nil && len(c.subs()) > 0 {
			return c
		}
		if time.Now().After(deadline) {
			t.Fatal("the manager did not subscribe")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// useMockClient makes mqttSubscribe create mock clients and returns the record of them.
func useMockClient(t *testing.T, configure func(*mockClient)) *madeClients {
	t.Helper()
	made := &madeClients{}
	old := newMqttClient
	newMqttClient = func(opts *MQTT.ClientOptions) MQTT.Client {
		m := &mockClient{opts: opts}
		if configure != nil {
			configure(m)
		}
		made.add(m)
		return m
	}
	t.Cleanup(func() { newMqttClient = old })
	return made
}

// loadSchema makes utils.JsonSchemaInit find the VISS schema. The schema is loaded
// once per test binary, whatever the working directory of the later tests.
var schemaOnce sync.Once

func loadSchema(t *testing.T) {
	t.Helper()
	schemaOnce.Do(func() {
		data, err := os.ReadFile(filepath.Join("..", "vissv3.0-schema.json"))
		if err != nil {
			t.Fatalf("cannot read the schema: %v", err)
		}
		orig, _ := os.Getwd()
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "vissv3.0-schema.json"), data, 0644); err != nil {
			t.Fatal(err)
		}
		os.Chdir(dir)
		utils.JsonSchemaInit()
		os.Chdir(orig)
	})
}

// ---------------------------------------------------------------------------
// mqttSubscribe
// ---------------------------------------------------------------------------

func TestMqttSubscribe_SuccessReturnsConnectedSubscribedClient(t *testing.T) {
	made := useMockClient(t, nil)
	c := mqttSubscribe("tcp://127.0.0.1:1883", "/WVWZZZ1KZEW000000/Vehicle")
	if c == nil {
		t.Fatal("mqttSubscribe returned nil on success")
	}
	if made.count() != 1 {
		t.Fatalf("clients created = %d; want 1", made.count())
	}
	m := made.first()
	if m.connected != 1 {
		t.Errorf("Connect called %d times; want 1", m.connected)
	}
	subs := m.subs()
	if len(subs) != 1 || subs[0].topic != "/WVWZZZ1KZEW000000/Vehicle" || subs[0].qos != 0 {
		t.Errorf("subscriptions = %+v; want one, qos 0, on the server topic", subs)
	}
	if m.disconnects != 0 {
		t.Errorf("a successful client was disconnected %d times", m.disconnects)
	}
}

func TestMqttSubscribe_UsesTheBrokerSocketAndTheDefaultHandler(t *testing.T) {
	made := useMockClient(t, nil)
	mqttSubscribe("tcp://10.1.2.3:1883", "/V/Vehicle")
	m := made.first()
	if got := m.opts.Servers; len(got) != 1 || got[0].String() != "tcp://10.1.2.3:1883" {
		t.Errorf("broker = %v; want tcp://10.1.2.3:1883", got)
	}
	if m.opts.DefaultPublishHandler == nil {
		t.Error("no default publish handler: messages from the broker would be dropped")
	}
}

// The handler that mqttSubscribe installs is the one that feeds the server loop.
func TestMqttSubscribe_DefaultHandlerFeedsTheServerLoop(t *testing.T) {
	made := useMockClient(t, nil)
	oldCh := mqttChannel
	mqttChannel = make(chan string, 1)
	defer func() { mqttChannel = oldCh }()
	mqttSubscribe("tcp://127.0.0.1:1883", "/V/Vehicle")
	h := made.first().opts.DefaultPublishHandler
	h(nil, &mockMessage{payload: []byte(`{"topic":"r","request":{}}`), topic: "/V/Vehicle"})
	select {
	case got := <-mqttChannel:
		if got != `{"topic":"r","request":{}}` {
			t.Errorf("payload = %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the handler did not forward the message")
	}
}

func TestMqttSubscribe_ConnectErrorReturnsNil(t *testing.T) {
	made := useMockClient(t, func(m *mockClient) { m.connectErr = errors.New("connection refused") })
	if c := mqttSubscribe("tcp://127.0.0.1:1883", "/V/Vehicle"); c != nil {
		t.Fatalf("got a client on a connect error")
	}
	if n := len(made.first().subs()); n != 0 {
		t.Errorf("subscribed %d times although the connect failed", n)
	}
}

// A failed subscription must not leave a connected client behind: it is disconnected.
func TestMqttSubscribe_SubscribeErrorReturnsNilAndDisconnects(t *testing.T) {
	made := useMockClient(t, func(m *mockClient) { m.subscribeErr = errors.New("not authorized") })
	if c := mqttSubscribe("tcp://127.0.0.1:1883", "/V/Vehicle"); c != nil {
		t.Fatalf("got a client on a subscribe error")
	}
	if d := made.first().disconnects; d != 1 {
		t.Errorf("disconnects = %d; want 1", d)
	}
}

// fwd turns a channel into the forward function that handleBrokerMessage takes.
func fwd(ch chan string) func(string) {
	return func(request string) { ch <- request }
}

// ---------------------------------------------------------------------------
// handleBrokerMessage
// ---------------------------------------------------------------------------

func TestHandleBrokerMessage_ValidRequestIsRegisteredAndForwarded(t *testing.T) {
	loadSchema(t)
	resetTopicList()
	defer resetTopicList()
	mc := &mockClient{}
	ch := make(chan string, 4)

	next := handleBrokerMessage(`{"topic":"VISS/c1","request":{"action":"get","path":"Vehicle.Speed","requestId":"1"}}`, 2, 7, fwd(ch), mc)

	if next != 8 {
		t.Errorf("next topicId = %d; want 8", next)
	}
	select {
	case got := <-ch:
		if !strings.Contains(got, `"RouterId":"2?7"`) || !strings.Contains(got, `"path":"Vehicle.Speed"`) {
			t.Errorf("forwarded = %q; want the request with RouterId 2?7", got)
		}
	default:
		t.Fatal("nothing forwarded to the server core")
	}
	if got := getTopic(7); got != "VISS/c1" {
		t.Errorf("reply topic registered for 7 = %q; want VISS/c1", got)
	}
	if n := len(mc.pubs()); n != 0 {
		t.Errorf("published %d messages for a valid request", n)
	}
}

func TestHandleBrokerMessage_CorruptMessageIsDropped(t *testing.T) {
	resetTopicList()
	defer resetTopicList()
	mc := &mockClient{}
	ch := make(chan string, 4)
	for _, msg := range []string{`not json`, `{"request":{"action":"get"}}`, `{"topic":42,"request":{}}`, ``} {
		if next := handleBrokerMessage(msg, 2, 3, fwd(ch), mc); next != 3 {
			t.Errorf("%q: next topicId = %d; want 3 (unchanged)", msg, next)
		}
	}
	if len(ch) != 0 {
		t.Errorf("%d messages forwarded for corrupt input", len(ch))
	}
	if n := len(mc.pubs()); n != 0 {
		t.Errorf("published %d messages; with no reply topic there is nobody to answer", n)
	}
	if listLen() != 0 {
		t.Errorf("topic list has %d entries", listLen())
	}
}

// A request that fails the schema is answered on its reply topic and is NOT forwarded.
func TestHandleBrokerMessage_SchemaInvalidRequestGetsAnErrorAndIsNotForwarded(t *testing.T) {
	loadSchema(t)
	resetTopicList()
	defer resetTopicList()
	mc := &mockClient{}
	ch := make(chan string, 4)

	next := handleBrokerMessage(`{"topic":"VISS/c1","request":{"action":"get","requestId":"9"}}`, 2, 4, fwd(ch), mc) // no path

	if next != 4 {
		t.Errorf("next topicId = %d; want 4 (unchanged)", next)
	}
	if len(ch) != 0 {
		t.Fatal("a schema invalid request was forwarded to the server core")
	}
	pubs := mc.pubs()
	if len(pubs) != 1 || pubs[0].topic != "VISS/c1" {
		t.Fatalf("publications = %+v; want one error response on VISS/c1", pubs)
	}
	if !strings.Contains(pubs[0].payload, `"error"`) || !strings.Contains(pubs[0].payload, `"bad_request"`) ||
		!strings.Contains(pubs[0].payload, `"requestId":"9"`) {
		t.Errorf("error response = %s; want bad_request with requestId 9", pubs[0].payload)
	}
	if listLen() != 0 {
		t.Errorf("a rejected request was registered (%d entries)", listLen())
	}
}

// Consecutive error responses must not carry fields over from the previous request.
func TestHandleBrokerMessage_ErrorResponsesDoNotLeakFieldsBetweenRequests(t *testing.T) {
	loadSchema(t)
	resetTopicList()
	defer resetTopicList()
	mc := &mockClient{}
	ch := make(chan string, 4)

	handleBrokerMessage(`{"topic":"VISS/a","request":{"action":"get","requestId":"first-secret"}}`, 2, 0, fwd(ch), mc)
	handleBrokerMessage(`{"topic":"VISS/b","request":{"action":"get"}}`, 2, 0, fwd(ch), mc)

	pubs := mc.pubs()
	if len(pubs) != 2 {
		t.Fatalf("publications = %d; want 2", len(pubs))
	}
	if strings.Contains(pubs[1].payload, "first-secret") {
		t.Errorf("the second error response (to VISS/b) contains the requestId of the first request: %s", pubs[1].payload)
	}
}

// ---------------------------------------------------------------------------
// handleServerCoreMessage
// ---------------------------------------------------------------------------

func TestHandleServerCoreMessage_PublishesOnTheReplyTopicWithoutRouterId(t *testing.T) {
	resetTopicList()
	defer resetTopicList()
	registerRequest("VISS/c1", 6, `{"action":"get"}`)
	mc := &mockClient{}

	handleServerCoreMessage(`{"RouterId":"2?6", "action":"get","requestId":"1","ts":"t","data":{"path":"Vehicle.Speed","dp":{"value":"5","ts":"t"}}}`, mc)

	pubs := mc.pubs()
	if len(pubs) != 1 || pubs[0].topic != "VISS/c1" {
		t.Fatalf("publications = %+v; want one on VISS/c1", pubs)
	}
	if strings.Contains(pubs[0].payload, "RouterId") {
		t.Errorf("the router id was not removed: %s", pubs[0].payload)
	}
	if !strings.Contains(pubs[0].payload, `"requestId":"1"`) {
		t.Errorf("payload = %s", pubs[0].payload)
	}
}

func TestHandleServerCoreMessage_NoRouteDropsTheMessage(t *testing.T) {
	resetTopicList()
	defer resetTopicList()
	mc := &mockClient{}
	handleServerCoreMessage(`{"RouterId":"2?99", "action":"get","requestId":"1"}`, mc)
	if n := len(mc.pubs()); n != 0 {
		t.Errorf("published %d messages for an unknown router id", n)
	}
}

// ---------------------------------------------------------------------------
// runHub
// ---------------------------------------------------------------------------

// A request from the broker, an answer from the server core and the request after it all go
// through the loop in order, and the topic ids count up.
func TestRunHub_RoutesRequestsAndResponses(t *testing.T) {
	loadSchema(t)
	resetTopicList()
	defer resetTopicList()
	mc := &mockClient{pubSignal: make(chan published, 8)}
	mqttCh := make(chan string)
	transport := make(chan string, 8) // requests to the core
	responses := make(chan string, 8) // responses from the core
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() { runHub(2, transport, responses, mqttCh, mc, stop); close(done) }()

	mqttCh <- `{"topic":"VISS/c1","request":{"action":"get","path":"Vehicle.Speed","requestId":"1"}}`
	mqttCh <- `{"topic":"VISS/c2","request":{"action":"get","path":"Vehicle.Speed","requestId":"2"}}`
	var forwarded []string
	for i := 0; i < 2; i++ {
		select {
		case m := <-transport:
			forwarded = append(forwarded, m)
		case <-time.After(5 * time.Second):
			t.Fatal("request not forwarded")
		}
	}
	if !strings.Contains(forwarded[0], `"RouterId":"2?0"`) || !strings.Contains(forwarded[1], `"RouterId":"2?1"`) {
		t.Errorf("topic ids not counted up from 0: %q, %q", forwarded[0], forwarded[1])
	}

	// the server core answers on the same channel
	responses <- `{"RouterId":"2?1", "action":"get","requestId":"2","ts":"t"}`
	select {
	case p := <-mc.pubSignal:
		if p.topic != "VISS/c2" {
			t.Errorf("response published on %q; want VISS/c2", p.topic)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("response not published")
	}

	close(stop)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("runHub did not return after stop was closed")
	}
}

// ---------------------------------------------------------------------------
// reply topic validation and resubscription
// ---------------------------------------------------------------------------

// The reply topic is chosen by the client. A topic name with a wildcard is not allowed in a publish, and
// a broker answers such a publish by closing the connection of the publisher, here the manager's one
// connection to the broker, which then has to reconnect, for every client. The request must be dropped.
func TestHandleBrokerMessage_ReplyTopicWithWildcardOrOtherInvalidIsDropped(t *testing.T) {
	loadSchema(t)
	for _, replyTopic := range []string{"a/#", "#", "a/+/b", "+", "x+y", "a\u0000b"} {
		t.Run(replyTopic, func(t *testing.T) {
			resetTopicList()
			defer resetTopicList()
			mc := &mockClient{}
			ch := make(chan string, 4)
			msg := `{"topic":` + jsonString(replyTopic) + `,"request":{"action":"get","path":"Vehicle.Speed","requestId":"1"}}`
			if next := handleBrokerMessage(msg, 2, 5, fwd(ch), mc); next != 5 {
				t.Errorf("next topicId = %d; want 5 (unchanged)", next)
			}
			if len(ch) != 0 {
				t.Errorf("the request was forwarded to the server core")
			}
			if n := len(mc.pubs()); n != 0 {
				t.Errorf("published %d messages: %+v", n, mc.pubs())
			}
			if listLen() != 0 {
				t.Errorf("the request was registered (%d entries)", listLen())
			}
		})
	}
}

// Topic names that are valid must still work, including ones that look unusual.
func TestHandleBrokerMessage_ValidReplyTopicsAreAccepted(t *testing.T) {
	loadSchema(t)
	for _, replyTopic := range []string{"VISS/testClient", "/leading/slash", "a b", "a-b_c.d", "ünïcode", "a/b/c/d/e"} {
		t.Run(replyTopic, func(t *testing.T) {
			resetTopicList()
			defer resetTopicList()
			ch := make(chan string, 4)
			msg := `{"topic":` + jsonString(replyTopic) + `,"request":{"action":"get","path":"Vehicle.Speed","requestId":"1"}}`
			if next := handleBrokerMessage(msg, 2, 5, fwd(ch), &mockClient{}); next != 6 {
				t.Errorf("next topicId = %d; want 6", next)
			}
			if len(ch) != 1 {
				t.Errorf("the request was not forwarded")
			}
		})
	}
}

// The connection to the broker is lost and restored by the client library, with a clean session, so the
// broker has forgotten the manager's subscription. Without a subscribe on every reconnect the manager
// goes silent: it stays connected and receives nothing.
func TestMqttSubscribe_SubscriptionIsRestoredWhenTheConnectionIsRestored(t *testing.T) {
	made := useMockClient(t, nil)
	mqttSubscribe("tcp://127.0.0.1:1883", "/V/Vehicle")
	m := made.first()
	if m.opts.OnConnect == nil {
		t.Fatal("no OnConnect handler: the subscription is not restored after a reconnect")
	}
	if n := len(m.subs()); n != 1 {
		t.Fatalf("after the first connect: %d subscriptions; want 1", n)
	}
	// the library calls the handler at the first connect too, and at every reconnect
	m.opts.OnConnect(m)
	if n := len(m.subs()); n != 1 {
		t.Errorf("the handler at the first connect subscribed again (%d subscriptions); want 1", n)
	}
	m.opts.OnConnect(m) // a reconnect
	subs := m.subs()
	if len(subs) != 2 || subs[1].topic != "/V/Vehicle" || subs[1].qos != 0 {
		t.Errorf("subscriptions after a reconnect = %+v; want the server topic subscribed again", subs)
	}
	m.opts.OnConnect(m) // and another one
	if n := len(m.subs()); n != 3 {
		t.Errorf("after a second reconnect: %d subscriptions; want 3", n)
	}
}

// A failed re-subscribe must not panic or stop anything; it is only logged.
func TestMqttSubscribe_FailedResubscribeIsHarmless(t *testing.T) {
	made := useMockClient(t, nil)
	mqttSubscribe("tcp://127.0.0.1:1883", "/V/Vehicle")
	m := made.first()
	if m.opts.OnConnect == nil {
		t.Skip("no OnConnect handler")
	}
	m.opts.OnConnect(m)
	m.mu.Lock()
	m.subscribeErr = errors.New("broker unavailable")
	m.mu.Unlock()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	m.opts.OnConnect(m)
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// ---------------------------------------------------------------------------
// MqttMgrInit
// ---------------------------------------------------------------------------

func withFastStartup(t *testing.T) {
	t.Helper()
	old := startupDelay
	startupDelay = 0
	t.Cleanup(func() { startupDelay = old })
}

func TestMqttMgrInit_WithoutAValidTopicItDoesNotConnect(t *testing.T) {
	withFastStartup(t)
	t.Setenv("MQTT_VIN", "bad/vin#")
	made := useMockClient(t, nil)
	// the VIN is invalid, so the manager must not try the server core either: the env var path
	// returns "" and the code then asks the core, which nobody answers here
	transport := make(chan string, 4)
	responses := make(chan string, 4)
	done := make(chan struct{})
	go func() { MqttMgrInit(2, transport, responses); close(done) }()
	select {
	case req := <-transport: // the VIN request to the core; answer with something unusable
		if !strings.Contains(req, "Vehicle.VehicleIdentification.VIN") {
			t.Errorf("unexpected request to the core: %q", req)
		}
		responses <- `{"RouterId":"2?0","action":"get","data":{"path":"Vehicle.VehicleIdentification.VIN","dp":{"value":"a/b+c","ts":"t"}}}`
	case <-time.After(10 * time.Second):
		t.Fatal("MqttMgrInit did not ask the server core for the VIN")
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("MqttMgrInit did not return for an invalid VIN")
	}
	if n := made.count(); n != 0 {
		t.Errorf("%d MQTT clients created for an invalid topic", n)
	}
}

func TestMqttMgrInit_SubscribeFailureReturnsInsteadOfRunning(t *testing.T) {
	withFastStartup(t)
	t.Setenv("MQTT_VIN", "TESTVIN001")
	useMockClient(t, func(m *mockClient) { m.connectErr = errors.New("refused") })
	done := make(chan struct{})
	go func() { MqttMgrInit(2, make(chan string, 4), make(chan string, 4)); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("MqttMgrInit did not return when the broker could not be reached")
	}
}

// Start the manager against the mock client, let a request through, answer it, and check
// that the right topic was subscribed.
func TestMqttMgrInit_EndToEnd(t *testing.T) {
	loadSchema(t)
	withFastStartup(t)
	t.Setenv("MQTT_VIN", "TESTVIN001")
	resetTopicList()
	defer resetTopicList()
	made := useMockClient(t, func(m *mockClient) { m.pubSignal = make(chan published, 8) })
	// the manager sends its requests to the core on one channel and receives the responses on another,
	// as in the server
	core := make(chan string, 8)
	responses := make(chan string, 8)
	go MqttMgrInit(2, core, responses)

	deadline := time.After(10 * time.Second)
	m := made.waitSubscribed(t, 10*time.Second)
	if subs := m.subs(); subs[0].topic != "/TESTVIN001/Vehicle" {
		t.Errorf("subscribed to %q; want /TESTVIN001/Vehicle", subs[0].topic)
	}
	// the broker delivers a client request through the handler mqttSubscribe installed
	m.opts.DefaultPublishHandler(nil, &mockMessage{
		payload: []byte(`{"topic":"VISS/c1","request":{"action":"get","path":"Vehicle.Speed","requestId":"7"}}`),
		topic:   "/TESTVIN001/Vehicle",
	})
	var req string
	select {
	case req = <-core:
	case <-deadline:
		t.Fatal("the request did not reach the server core")
	}
	if !strings.Contains(req, `"RouterId":"2?0"`) {
		t.Fatalf("request to the core = %q", req)
	}
	// ... and the core answers on the response channel
	responses <- `{"RouterId":"2?0", "action":"get","requestId":"7","ts":"t"}`
	select {
	case p := <-m.pubSignal:
		if p.topic != "VISS/c1" {
			t.Errorf("answer published on %q; want VISS/c1", p.topic)
		}
	case <-deadline:
		t.Fatal("the answer was not published on the reply topic")
	}
}
