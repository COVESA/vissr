//go:build cyclone

package ddsMgr

import (
	"encoding/json"
	"testing"
	"time"

	dds "github.com/SoundMatt/go-DDS"
	"github.com/SoundMatt/go-DDS/cyclone"
)

// TestDdsVissRoundTrip_Cyclone validates the complete VISSR-over-DDS path
// using real CycloneDDS participants. Run with:
//
//	go test -tags cyclone ./server/vissv2server/ddsMgr/...
//
// Requires libcyclonedds-dev (apt install libcyclonedds-dev / brew install cyclonedds).
// The test mirrors TestDdsVissRoundTrip_Mock but uses CycloneDDS so that
// real RTPS/UDP discovery and message exchange are exercised.
func TestDdsVissRoundTrip_Cyclone(t *testing.T) {
	origValidate := schemaValidate
	t.Cleanup(func() { schemaValidate = origValidate })
	schemaValidate = func(_ string) string { return "" }

	origNew := newParticipant
	t.Cleanup(func() { newParticipant = origNew })
	newParticipant = func() (dds.Participant, error) { return cyclone.New(ddsDomain) }

	t.Setenv("DDS_VIN", "CYCLONE01")
	t.Cleanup(resetReplies)
	transportChan := make(chan string, 8)

	go DdsMgrInit(5, transportChan)
	// 2s startup sleep + extra time for CycloneDDS participant discovery.
	time.Sleep(4 * time.Second)

	clientP, err := cyclone.New(ddsDomain)
	if err != nil {
		t.Fatalf("cyclone.New for client: %v", err)
	}
	defer clientP.Close()

	const replyTopic = "/cyclone/reply/001"
	replySub, err := clientP.NewSubscriber(replyTopic, dds.DefaultQoS)
	if err != nil {
		t.Fatalf("client NewSubscriber: %v", err)
	}
	defer replySub.Close()

	reqPub, err := clientP.NewPublisher("/CYCLONE01/Vehicle", dds.DefaultQoS)
	if err != nil {
		t.Fatalf("client NewPublisher: %v", err)
	}
	defer reqPub.Close()

	// Allow CycloneDDS discovery (SPDP) before publishing.
	time.Sleep(time.Second)

	env := makeEnvelope(replyTopic, "get", "Vehicle.Speed")
	if err := reqPub.Write([]byte(env)); err != nil {
		t.Fatalf("client publish: %v", err)
	}

	select {
	case sample := <-replySub.C():
		var m map[string]interface{}
		if err := json.Unmarshal(sample.Payload, &m); err != nil {
			t.Fatalf("response is not valid JSON: %v — payload: %s", err, sample.Payload)
		}
		if _, ok := m["action"]; !ok {
			t.Errorf("response missing \"action\" field: %s", sample.Payload)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no DDS response received within 10s (is CycloneDDS installed?)")
	}
}
