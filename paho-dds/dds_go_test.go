// Package paho_dds contains transport-layer smoke tests for the go-DDS
// mock implementation. Mirrors paho-mqtt/paho_go_test.go, which validates
// paho MQTT connectivity against a live broker. Here the mock in-process
// broker is always available so no external infrastructure is needed.
package paho_dds

import (
	"testing"
	"time"

	dds "github.com/SoundMatt/go-DDS"
	"github.com/SoundMatt/go-DDS/mock"
)

// TestDDSMockLocalDomain verifies that two mock participants within the same
// process can exchange messages over the shared in-process broker.
func TestDDSMockLocalDomain(t *testing.T) {
	const topic = "vissr/dds/smoke/test"

	p1, err := mock.New(dds.Domain(0))
	if err != nil {
		t.Fatalf("mock.New p1: %v", err)
	}
	defer p1.Close()

	p2, err := mock.New(dds.Domain(0))
	if err != nil {
		t.Fatalf("mock.New p2: %v", err)
	}
	defer p2.Close()

	sub, err := p2.NewSubscriber(topic, dds.DefaultQoS)
	if err != nil {
		t.Fatalf("p2 NewSubscriber: %v", err)
	}
	defer sub.Close()

	pub, err := p1.NewPublisher(topic, dds.DefaultQoS)
	if err != nil {
		t.Fatalf("p1 NewPublisher: %v", err)
	}
	defer pub.Close()

	const want = `{"action":"get","path":"Vehicle.Speed"}`
	if err := pub.Write([]byte(want)); err != nil {
		t.Fatalf("Write: %v", err)
	}

	select {
	case sample := <-sub.C():
		if got := string(sample.Payload); got != want {
			t.Errorf("payload = %q; want %q", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("no sample received within 1s")
	}
}
