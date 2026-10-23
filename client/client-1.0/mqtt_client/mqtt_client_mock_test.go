/**
* (C) 2026 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE
* file in this repository.
*
* Tests of the MQTT broker functions of mqtt_client against a mock MQTT client, so no broker is needed.
**/
package main

import (
	"bufio"
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

// useMock makes the client functions create mock clients, and returns the ones made.
func useMock(t *testing.T, configure func(*mockClient)) *[]*mockClient {
	t.Helper()
	var made []*mockClient
	old := newMqttClient
	newMqttClient = func(opts *MQTT.ClientOptions) MQTT.Client {
		m := &mockClient{opts: opts}
		if configure != nil {
			configure(m)
		}
		made = append(made, m)
		return m
	}
	t.Cleanup(func() { newMqttClient = old })
	return &made
}

func TestMqttSubscribe_SuccessReturnsTheConnectedClient(t *testing.T) {
	made := useMock(t, nil)
	c, err := mqttSubscribe("tcp://10.0.0.1:1883", "VISS/reply")
	if err != nil || c == nil {
		t.Fatalf("got (%v, %v); want a client and no error", c, err)
	}
	m := (*made)[0]
	if len(m.subscribed) != 1 || m.subscribed[0] != "VISS/reply" {
		t.Errorf("subscriptions = %v; want [VISS/reply]", m.subscribed)
	}
	if m.disconnects != 0 {
		t.Errorf("the client was disconnected %d times; the caller keeps it for the subscription", m.disconnects)
	}
	if got := m.opts.Servers; len(got) != 1 || got[0].String() != "tcp://10.0.0.1:1883" {
		t.Errorf("broker = %v", got)
	}
	if m.opts.DefaultPublishHandler == nil {
		t.Error("no handler for the messages from the broker")
	}
}

func TestMqttSubscribe_ConnectErrorIsReturned(t *testing.T) {
	made := useMock(t, func(m *mockClient) { m.connectErr = errors.New("connection refused") })
	c, err := mqttSubscribe("tcp://x:1883", "VISS/reply")
	if err == nil || c != nil {
		t.Fatalf("got (%v, %v); want no client and the error", c, err)
	}
	if n := len((*made)[0].subscribed); n != 0 {
		t.Errorf("subscribed %d times although the connect failed", n)
	}
}

// A failed subscription must not leave a connected client behind.
func TestMqttSubscribe_SubscribeErrorDisconnectsAndIsReturned(t *testing.T) {
	made := useMock(t, func(m *mockClient) { m.subscribeErr = errors.New("not authorized") })
	c, err := mqttSubscribe("tcp://x:1883", "VISS/reply")
	if err == nil || c != nil {
		t.Fatalf("got (%v, %v); want no client and the error", c, err)
	}
	if d := (*made)[0].disconnects; d != 1 {
		t.Errorf("disconnects = %d; want 1", d)
	}
}

func TestPublishMessage_PublishesAndDisconnects(t *testing.T) {
	made := useMock(t, nil)
	if err := publishMessage("tcp://x:1883", "/V/Vehicle", `{"a":1}`); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := (*made)[0]
	if len(m.published) != 1 || m.published[0] != (published{"/V/Vehicle", `{"a":1}`}) {
		t.Errorf("published = %+v", m.published)
	}
	if m.disconnects != 1 {
		t.Errorf("disconnects = %d; want 1", m.disconnects)
	}
}

func TestPublishMessage_ConnectErrorIsReturned(t *testing.T) {
	made := useMock(t, func(m *mockClient) { m.connectErr = errors.New("connection refused") })
	if err := publishMessage("tcp://x:1883", "/V/Vehicle", "{}"); err == nil {
		t.Fatal("want the connect error")
	}
	if n := len((*made)[0].published); n != 0 {
		t.Errorf("published %d messages without a connection", n)
	}
}

// A publish that fails is an error too: it was ignored before, and the caller thought the request was sent.
func TestPublishMessage_PublishErrorIsReturnedAndClientDisconnected(t *testing.T) {
	made := useMock(t, func(m *mockClient) { m.publishErr = errors.New("not connected") })
	if err := publishMessage("tcp://x:1883", "/V/Vehicle", "{}"); err == nil {
		t.Fatal("want the publish error")
	}
	if d := (*made)[0].disconnects; d != 1 {
		t.Errorf("disconnects = %d; want 1", d)
	}
}

func TestPublishVissV2Request_SendsTheEnvelopeToTheVinTopic(t *testing.T) {
	made := useMock(t, nil)
	uniqueTopicName = "VISS/reply"
	if err := publishVissV2Request("tcp://x:1883", "VIN123", `{"action":"get","path":"Vehicle.Speed"}`); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	p := (*made)[0].published[0]
	if p.topic != "/VIN123/Vehicle" {
		t.Errorf("topic = %q; want /VIN123/Vehicle", p.topic)
	}
	if !strings.Contains(p.payload, `"topic":"VISS/reply"`) || !strings.Contains(p.payload, `"path":"Vehicle.Speed"`) {
		t.Errorf("payload = %s", p.payload)
	}
}

// A request that cannot be sent is reported, not silently dropped.
func TestPublishVissV2Request_InvalidRequestIsAnErrorAndNothingIsPublished(t *testing.T) {
	made := useMock(t, nil)
	uniqueTopicName = "VISS/reply"
	if err := publishVissV2Request("tcp://x:1883", "VIN123", `not json`); err == nil {
		t.Error("want an error for a request that is not JSON")
	}
	uniqueTopicName = ""
	if err := publishVissV2Request("tcp://x:1883", "VIN123", `{"action":"get"}`); err == nil {
		t.Error("want an error when no reply topic is set")
	}
	if n := len(*made); n != 0 {
		t.Errorf("%d clients were created for requests that cannot be sent", n)
	}
}

func TestSubscribeVissV2Response_SubscribesToTheUniqueTopic(t *testing.T) {
	made := useMock(t, nil)
	uniqueTopicName = "VISS/mine"
	c, err := subscribeVissV2Response("tcp://x:1883")
	if err != nil || c == nil {
		t.Fatalf("got (%v, %v)", c, err)
	}
	if got := (*made)[0].subscribed; len(got) != 1 || got[0] != "VISS/mine" {
		t.Errorf("subscriptions = %v; want [VISS/mine]", got)
	}
}

// A request is a JSON text and often has spaces in it. It is read as one line, not cut at the first space.
func TestReadLine_ReadsWholeLines(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("VISS/mine\n{\"action\":\"get\", \"path\":\"Vehicle.Speed\"}\n  q  \n\nlast without newline"))
	want := []string{"VISS/mine", `{"action":"get", "path":"Vehicle.Speed"}`, "q", "", "last without newline"}
	for i, w := range want {
		got, ok := readLine(r)
		if !ok || got != w {
			t.Fatalf("line %d: got (%q, %v); want (%q, true)", i, got, ok, w)
		}
	}
	if got, ok := readLine(r); ok {
		t.Errorf("a line after the end of the input: %q", got)
	}
}

func TestReadLine_WindowsLineEndings(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("abc\r\ndef\r\n"))
	if got, _ := readLine(r); got != "abc" {
		t.Errorf("got %q; want abc", got)
	}
	if got, _ := readLine(r); got != "def" {
		t.Errorf("got %q; want def", got)
	}
}

func TestReadLine_EmptyInput(t *testing.T) {
	if got, ok := readLine(bufio.NewReader(strings.NewReader(""))); ok {
		t.Errorf("got (%q, true) from an empty input", got)
	}
}
