package ddsMgr

import (
	"encoding/json"
	"testing"
	"time"

	dds "github.com/SoundMatt/go-DDS"
	"github.com/SoundMatt/go-DDS/mock"
)

// TestDdsServiceActionRoundTrip_Mock confirms that a VISSv3.2 service request
// (here an "invoke") transits the DDS transport unchanged: the action and path
// survive the publish → DdsMgrInit → reply round trip.
//
// Service dispatch itself (routing invoke/monitor/discover to vissServiceMgr)
// happens in the transport-agnostic server core, identically for every
// transport, and is covered by the vissServiceMgr tests — not here. This test
// only asserts that DDS carries service actions to the core like the other
// transports, so the service profile works over DDS for free.
func TestDdsServiceActionRoundTrip_Mock(t *testing.T) {
	origValidate := schemaValidate
	t.Cleanup(func() { schemaValidate = origValidate })
	schemaValidate = func(_ string) string { return "" }

	origNew := newParticipant
	t.Cleanup(func() { newParticipant = origNew })
	newParticipant = func() (dds.Participant, error) { return mock.New(ddsDomain) }

	t.Setenv("DDS_VIN", "SVCDDS01")
	t.Cleanup(resetReplies)
	transportChan := make(chan string, 8)

	go DdsMgrInit(5, transportChan)
	time.Sleep(3 * time.Second)

	clientP, err := mock.New(ddsDomain)
	if err != nil {
		t.Fatalf("mock.New for client: %v", err)
	}
	defer clientP.Close()

	const replyTopic = "/roundtrip/reply/svc-001"
	replySub, err := clientP.NewSubscriber(replyTopic, dds.DefaultQoS)
	if err != nil {
		t.Fatalf("client NewSubscriber: %v", err)
	}
	defer replySub.Close()

	reqPub, err := clientP.NewPublisher("/SVCDDS01/Vehicle", dds.DefaultQoS)
	if err != nil {
		t.Fatalf("client NewPublisher: %v", err)
	}
	defer reqPub.Close()

	const servicePath = "VehicleService.Seating.Row1.DriverSide.MoveSeat"
	env := makeEnvelope(replyTopic, "invoke", servicePath)
	if err := reqPub.Write([]byte(env)); err != nil {
		t.Fatalf("client publish: %v", err)
	}

	select {
	case sample := <-replySub.C():
		var m map[string]interface{}
		if err := json.Unmarshal(sample.Payload, &m); err != nil {
			t.Fatalf("response is not valid JSON: %v — payload: %s", err, sample.Payload)
		}
		if m["action"] != "invoke" {
			t.Errorf("action = %v, want invoke (service action not preserved over DDS)", m["action"])
		}
		if m["path"] != servicePath {
			t.Errorf("path = %v, want %q", m["path"], servicePath)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no DDS response received within 5s")
	}
}
