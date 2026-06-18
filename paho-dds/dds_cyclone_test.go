//go:build cyclone

package paho_dds

import (
	"testing"
	"time"

	dds "github.com/SoundMatt/go-DDS"
	"github.com/SoundMatt/go-DDS/cyclone"
)

// TestDDSCycloneLocalDomain verifies that two CycloneDDS participants on the
// same domain can exchange messages over real RTPS/UDP. Run with:
//
//	go test -tags cyclone ./paho-dds/...
//
// Requires libcyclonedds-dev (apt install libcyclonedds-dev / brew install cyclonedds).
func TestDDSCycloneLocalDomain(t *testing.T) {
	const topic = "vissr/dds/cyclone/smoke/test"

	p1, err := cyclone.New(dds.Domain(0))
	if err != nil {
		t.Fatalf("cyclone.New p1: %v", err)
	}
	defer p1.Close()

	p2, err := cyclone.New(dds.Domain(0))
	if err != nil {
		t.Fatalf("cyclone.New p2: %v", err)
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

	// Allow CycloneDDS participant discovery (SPDP) before publishing.
	time.Sleep(time.Second)

	const want = `{"action":"get","path":"Vehicle.Speed"}`
	if err := pub.Write([]byte(want)); err != nil {
		t.Fatalf("Write: %v", err)
	}

	select {
	case sample := <-sub.C():
		if got := string(sample.Payload); got != want {
			t.Errorf("payload = %q; want %q", got, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no sample received within 5s (is CycloneDDS installed?)")
	}
}
