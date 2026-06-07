/**
* (C) 2026 Matt Jones
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE file in this repository.
*
**/

// Package ddsMgr implements the VISS-over-DDS transport manager.
//
// Protocol envelope (mirrors VISS over MQTT):
//
//	Request  topic : /<VIN>/Vehicle
//	Payload        : {"replyTopic":"<unique>","request":{...VISS JSON...}}
//	Response topic : <unique>
//
// The mock DDS implementation is used by default (no system library needed).
// Switch to CycloneDDS by rebuilding vissr with -tags cyclone and
// libcyclonedds-dev installed.
package ddsMgr

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	dds "github.com/SoundMatt/go-DDS"
	"github.com/covesa/vissr/utils"
)

// ddsDomain is the DDS domain used for VISS traffic.
const ddsDomain = dds.Domain(0)

var errorResponseMap = map[string]interface{}{}

// replyEntry maps a DDS reply-topic to its RouterId handle so that
// server-core responses can be published to the correct client topic.
type replyEntry struct {
	replyTopic string
	topicId    int
}

type replyList struct {
	mu      sync.Mutex
	entries []replyEntry
}

func (r *replyList) push(replyTopic string, topicId int) {
	r.mu.Lock()
	r.entries = append(r.entries, replyEntry{replyTopic, topicId})
	r.mu.Unlock()
}

func (r *replyList) get(topicId int) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.entries {
		if e.topicId == topicId {
			return e.replyTopic
		}
	}
	return ""
}

func (r *replyList) pop(topicId int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, e := range r.entries {
		if e.topicId == topicId {
			r.entries = append(r.entries[:i], r.entries[i+1:]...)
			return
		}
	}
}

var replies replyList

// isValidVin mirrors the MQTT manager's VIN validation.
// VINs are alphanumeric per ISO 3779; we additionally allow '-' and '_'.
func isValidVin(vin string) bool {
	if vin == "" || len(vin) > 64 {
		return false
	}
	for _, c := range vin {
		if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
			(c >= '0' && c <= '9') || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func getVehicleTopic(transportMgrChan chan string, mgrId int) string {
	if vin := os.Getenv("DDS_VIN"); vin != "" {
		if !isValidVin(vin) {
			utils.Error.Printf("ddsMgr: DDS_VIN=%q is not a valid VIN", vin)
			return ""
		}
		utils.Info.Printf("ddsMgr: using DDS_VIN env var, topic=/%s/Vehicle", vin)
		return "/" + vin + "/Vehicle"
	}

	vinRequest := `{"RouterId":"` + strconv.Itoa(mgrId) + `?0","action":"get","path":"Vehicle.VehicleIdentification.VIN","requestId":"570416","origin":"internal"}`
	transportMgrChan <- vinRequest

	select {
	case response := <-transportMgrChan:
		vin := extractVin(response)
		if !isValidVin(vin) {
			utils.Error.Printf("ddsMgr: invalid VIN %q in server response (set DDS_VIN to override)", vin)
			return ""
		}
		return "/" + vin + "/Vehicle"
	case <-time.After(5 * time.Second):
		utils.Error.Printf("ddsMgr: timed out waiting for VIN (set DDS_VIN to override)")
		return ""
	}
}

func extractVin(response string) string {
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(response), &m); err != nil {
		return ""
	}
	if data, ok := m["data"].(map[string]interface{}); ok {
		if dp, ok := data["dp"].(map[string]interface{}); ok {
			if v, ok := dp["value"].(string); ok {
				return v
			}
		}
		if v, ok := data["value"].(string); ok {
			return v
		}
	}
	if v, ok := m["value"].(string); ok {
		return v
	}
	return ""
}

// decomposeDdsPayload unpacks {"replyTopic":"X","request":{...}} into its parts.
func decomposeDdsPayload(payload string) (replyTopic, request string) {
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(payload), &m); err != nil {
		utils.Error.Printf("ddsMgr: cannot parse payload: %s", payload)
		return "", ""
	}
	rt, ok := m["replyTopic"].(string)
	if !ok || rt == "" {
		utils.Error.Printf("ddsMgr: missing replyTopic in: %s", payload)
		return "", ""
	}
	reqBytes, err := json.Marshal(m["request"])
	if err != nil {
		utils.Error.Printf("ddsMgr: cannot marshal request in: %s", payload)
		return rt, ""
	}
	return rt, string(reqBytes)
}

func addRoutingAndForward(request string, mgrId, clientId int, transportMgrChan chan string) {
	prefix := `{"RouterId":"` + strconv.Itoa(mgrId) + "?" + strconv.Itoa(clientId) + `", `
	transportMgrChan <- strings.Replace(request, "{", prefix, 1)
}

func vissV2Receiver(transportMgrChan chan string, vissv2Chan chan string) {
	for {
		vissv2Chan <- <-transportMgrChan
	}
}

// newParticipant is set by the build-tag-conditional backend file (ddsMgr_mock.go
// or ddsMgr_cyclone.go). Tests may override it to inject a fake participant.
var newParticipant func() (dds.Participant, error)

// schemaValidate wraps utils.JsonSchemaValidate so tests can override it
// to return "" (schema passes) and exercise the happy path without needing
// a real vissv3.0-schema.json file present in the test working directory.
var schemaValidate = utils.JsonSchemaValidate

// DdsMgrInit is the transport manager entry point called by vissv2server.
// mgrId is the channel-slot index assigned to this manager (slot 5).
// transportMgrChan is the shared bidirectional channel to the server core;
// it must be unbuffered (same requirement as the MQTT manager).
func DdsMgrInit(mgrId int, transportMgrChan chan string) {
	participant, err := newParticipant()
	if err != nil {
		utils.Error.Printf("ddsMgr: cannot create DDS participant: %v", err)
		return
	}
	defer participant.Close()

	// Allow the feeder and state storage to start before requesting the VIN.
	time.Sleep(2 * time.Second)
	vehicleTopic := getVehicleTopic(transportMgrChan, mgrId)
	if vehicleTopic == "" {
		utils.Error.Printf("ddsMgr: could not derive vehicle topic; manager not started")
		return
	}

	sub, err := participant.NewSubscriber(vehicleTopic, dds.DefaultQoS)
	if err != nil {
		utils.Error.Printf("ddsMgr: subscribe to %q failed: %v", vehicleTopic, err)
		return
	}
	defer sub.Close()

	pub, err := participant.NewPublisher(vehicleTopic, dds.DefaultQoS)
	if err != nil {
		utils.Error.Printf("ddsMgr: create publisher failed: %v", err)
		return
	}
	defer pub.Close()

	utils.JsonSchemaInit()

	vissv2Chan := make(chan string)
	go vissV2Receiver(transportMgrChan, vissv2Chan)

	utils.Info.Println("**** DDS manager hub entering server loop... ****")

	topicId := 0
	for {
		select {
		case sample := <-sub.C():
			payload := string(sample.Payload)
			replyTopic, request := decomposeDdsPayload(payload)
			if replyTopic == "" {
				utils.Error.Printf("ddsMgr: dropping malformed sample: %s", payload)
				continue
			}
			utils.Info.Printf("ddsMgr: request on %s: %s", vehicleTopic, request)

			if errStr := schemaValidate(request); errStr != "" {
				var reqMap map[string]interface{}
				utils.MapRequest(request, &reqMap)
				utils.SetErrorResponse(reqMap, errorResponseMap, 0, errStr)
				replyPub, perr := participant.NewPublisher(replyTopic, dds.DefaultQoS)
				if perr == nil {
					replyPub.Write([]byte(utils.FinalizeMessage(errorResponseMap)))
					replyPub.Close()
				}
				continue
			}

			replies.push(replyTopic, topicId)
			addRoutingAndForward(request, mgrId, topicId, transportMgrChan)
			topicId++

		case vissv2Msg := <-vissv2Chan:
			utils.Info.Printf("ddsMgr: response from server core: %s", vissv2Msg)
			responsePayload, topicHandle := utils.RemoveInternalData(vissv2Msg)
			replyTopic := replies.get(topicHandle)
			if replyTopic == "" {
				utils.Error.Printf("ddsMgr: no reply topic for handle %d", topicHandle)
				continue
			}
			replies.pop(topicHandle)
			replyPub, err := participant.NewPublisher(replyTopic, dds.DefaultQoS)
			if err != nil {
				utils.Error.Printf("ddsMgr: cannot publish reply to %q: %v", replyTopic, err)
				continue
			}
			replyPub.Write([]byte(responsePayload))
			replyPub.Close()
		}
	}
}
