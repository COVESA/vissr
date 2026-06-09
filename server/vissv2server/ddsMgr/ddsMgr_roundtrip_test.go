package ddsMgr

import (
	"encoding/json"
	"testing"
	"time"

	dds "github.com/SoundMatt/go-DDS"
	"github.com/SoundMatt/go-DDS/mock"
)

// TestDdsVissRoundTrip_Mock validates the complete VISSR-over-DDS
// request/response path within a single process using the in-process mock
// broker. Two mock.New participants share the process-global broker:
//
//	clientP  →  pub("/ROUNDTRIP01/Vehicle")  →  DdsMgrInit subscriber
//	DdsMgrInit  →  transportChan  →  vissV2Receiver  →  vissv2Chan
//	vissv2Chan case  →  pub(replyTopic)  →  clientP subscriber
//
// The response payload is the RouterId-stripped forward message (no real
// server core is running), but the full DDS transport path is exercised.
func TestDdsVissRoundTrip_Mock(t *testing.T) {
	origValidate := schemaValidate
	t.Cleanup(func() { schemaValidate = origValidate })
	schemaValidate = func(_ string) string { return "" }

	origNew := newParticipant
	t.Cleanup(func() { newParticipant = origNew })
	newParticipant = func() (dds.Participant, error) { return mock.New(ddsDomain) }

	t.Setenv("DDS_VIN", "ROUNDTRIP01")
	t.Cleanup(resetReplies)
	transportChan := make(chan string, 8)

	go DdsMgrInit(5, transportChan)
	time.Sleep(3 * time.Second)

	// Client participant on the same process-global mock broker.
	clientP, err := mock.New(ddsDomain)
	if err != nil {
		t.Fatalf("mock.New for client: %v", err)
	}
	defer clientP.Close()

	const replyTopic = "/roundtrip/reply/001"
	replySub, err := clientP.NewSubscriber(replyTopic, dds.DefaultQoS)
	if err != nil {
		t.Fatalf("client NewSubscriber: %v", err)
	}
	defer replySub.Close()

	reqPub, err := clientP.NewPublisher("/ROUNDTRIP01/Vehicle", dds.DefaultQoS)
	if err != nil {
		t.Fatalf("client NewPublisher: %v", err)
	}
	defer reqPub.Close()

	// Publish a VISS get request wrapped in the DDS envelope.
	env := makeEnvelope(replyTopic, "get", "Vehicle.Speed")
	if err := reqPub.Write([]byte(env)); err != nil {
		t.Fatalf("client publish: %v", err)
	}

	// DdsMgrInit processes the request, strips the RouterId, and publishes the
	// stripped payload to replyTopic. The client subscriber verifies receipt.
	select {
	case sample := <-replySub.C():
		var m map[string]interface{}
		if err := json.Unmarshal(sample.Payload, &m); err != nil {
			t.Fatalf("response is not valid JSON: %v — payload: %s", err, sample.Payload)
		}
		if _, ok := m["action"]; !ok {
			t.Errorf("response missing \"action\" field: %s", sample.Payload)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no DDS response received within 5s")
	}
}
