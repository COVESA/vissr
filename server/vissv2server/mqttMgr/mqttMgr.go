/**
* (C) 2022 Geotab
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE file in this repository.
*
**/
package mqttMgr

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/covesa/vissr/utils"
	MQTT "github.com/eclipse/paho.mqtt.golang"
)

var mqttChannel chan string

var errorResponseMap = map[string]interface{}{}

type NodeValue struct {
	topicId int
	topic   string
}

type Node struct {
	value NodeValue
	next  *Node
}

type TopicList struct {
	head  *Node
	nodes int
}

var topicList TopicList

// topicListMu guards topicList. pushTopic / getTopic / popTopic are
// all called from the main MqttMgrInit select loop today, so the
// mutex is largely defensive — but popTopic is documented as
// "TODO: to be used at unsubscribe..." which suggests it will
// eventually be invoked from another goroutine.
var topicListMu sync.Mutex

// isValidVin guards against MQTT topic-name injection. The VIN
// returned by the server core ultimately becomes part of the
// subscribed topic ("/<VIN>/Vehicle"); if a VIN contained MQTT
// wildcards ('+', '#') or path separators ('/'), the manager would
// end up subscribing to topics it shouldn't. VINs are alphanumeric
// per ISO 3779 but we allow '-' and '_' as well for legacy/test
// values.
func isValidVin(vin string) bool {
	if vin == "" || len(vin) > 64 {
		return false
	}
	for _, c := range vin {
		if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func vissV2Receiver(transportMgrChan chan string, vissv2Channel chan string) {
	//	defer dataConn.Close()
	for {
		/*		_, response, err := dataConn.ReadMessage() // receive message from server core
				if err != nil {
					utils.Error.Println("Datachannel read error:" + err.Error())
					break
				}*/
		response := <-transportMgrChan
		utils.Info.Printf("MQTT mgr: Response from server core:%s", string(response))
		vissv2Channel <- string(response) // send message to hub
	}
}

// TODO add conf file
func getBrokerSocket(isSecure bool) string {
	FVTAddr := os.Getenv("MQTT_BROKER_ADDR")
	if FVTAddr == "" {
		FVTAddr = "127.0.0.1"
	}

	if isSecure {
		return "ssl://" + FVTAddr + ":8883"
	}
	return "tcp://" + FVTAddr + ":1883"
}

var publishHandler MQTT.MessageHandler = func(client MQTT.Client, msg MQTT.Message) {
	//    mqttChannel <- msg.Topic()
	utils.Info.Printf("publishHandler:payload=%s", string(msg.Payload()))
	mqttChannel <- string(msg.Payload())
}

func mqttSubscribe(brokerSocket string, topic string) MQTT.Client {
	utils.Info.Printf("mqttSubscribe:Topic=%s", topic)
	opts := MQTT.NewClientOptions().AddBroker(brokerSocket)
	//    opts.SetClientID("VIN001")
	opts.SetDefaultPublishHandler(publishHandler)

	c := MQTT.NewClient(opts)
	if token := c.Connect(); token.Wait() && token.Error() != nil {
		utils.Error.Println(token.Error())
		return nil
	}
	if token := c.Subscribe(topic, 0, nil); token.Wait() && token.Error() != nil {
		// Previously this called os.Exit(1), tearing down the entire
		// vissv2server process on any transient broker subscribe
		// failure. Return nil instead and let the caller decide.
		utils.Error.Println("mqttSubscribe: subscribe failed:", token.Error())
		c.Disconnect(250)
		return nil
	}
	return c
}

func getTopic(topicId int) string {
	topicListMu.Lock()
	defer topicListMu.Unlock()
	for n := topicList.head; n != nil; n = n.next {
		if n.value.topicId == topicId {
			return n.value.topic
		}
	}
	return ""
}

func pushTopic(topic string, topicId int) {
	topicListMu.Lock()
	defer topicListMu.Unlock()
	newNode := &Node{value: NodeValue{topicId: topicId, topic: topic}}
	if topicList.head == nil {
		topicList.head = newNode
		topicList.nodes++
		return
	}
	// Walk to the tail and append. The old code mixed a counter-based
	// loop with `iterator.next == nil` and could leave the node count
	// inconsistent if the structure was ever modified concurrently.
	tail := topicList.head
	for tail.next != nil {
		tail = tail.next
	}
	tail.next = newNode
	topicList.nodes++
}

// popTopic removes the first node whose topicId matches and returns.
// The original implementation had three structural bugs (nil-deref on
// empty list, nil-deref after head removal, broken splice that could
// drop nodes). This is a clean single-pass rewrite.
func popTopic(topicId int) {
	topicListMu.Lock()
	defer topicListMu.Unlock()
	if topicList.head == nil {
		return
	}
	// Head case.
	if topicList.head.value.topicId == topicId {
		topicList.head = topicList.head.next
		topicList.nodes--
		return
	}
	// Walk with prev pointer; splice when found.
	for prev := topicList.head; prev.next != nil; prev = prev.next {
		if prev.next.value.topicId == topicId {
			prev.next = prev.next.next
			topicList.nodes--
			return
		}
	}
}

// publishMessage publishes payload on the given MQTT topic via the
// supplied already-connected client. The original implementation
// created a fresh MQTT.NewClient and connected on every call —
// generating a TCP storm against the broker under load — and called
// os.Exit(1) on any Connect failure, killing the entire vissv2server
// process for transient broker hiccups. Both problems are fixed by
// reusing the long-lived subscription client (which can also publish).
func publishMessage(client MQTT.Client, topic, payload string) {
	utils.Info.Printf("publishMessage:Topic=%s, Payload=%s", topic, payload)
	if client == nil {
		utils.Error.Printf("publishMessage: nil client, dropping message for topic=%s", topic)
		return
	}
	if topic == "" {
		utils.Error.Printf("publishMessage: empty topic, dropping payload=%s", payload)
		return
	}
	token := client.Publish(topic, 0, false, payload)
	token.Wait()
	if err := token.Error(); err != nil {
		utils.Error.Printf("publishMessage: publish failed for topic=%s: %s", topic, err)
	}
}

// getVissV2TopicFromEnv checks MQTT_VIN env var and returns the MQTT topic if set.
func getVissV2TopicFromEnv() string {
	vin := os.Getenv("MQTT_VIN")
	if vin == "" {
		return ""
	}
	if !isValidVin(vin) {
		utils.Error.Printf("getVissV2Topic: MQTT_VIN=%q is not a valid VIN (alphanumeric + - _)", vin)
		return ""
	}
	utils.Info.Printf("getVissV2Topic: using MQTT_VIN env var, topic=/%s/Vehicle", vin)
	return "/" + vin + "/Vehicle"
}

func getVissV2Topic(transportMgrChan chan string, mgrId int) string {
	// Fast path: MQTT_VIN env var bypasses the channel round-trip (useful for
	// local development where state storage has no VIN populated yet).
	if topic := getVissV2TopicFromEnv(); topic != "" {
		return topic
	}

	vinRequest := "{\"RouterId\":\"" + strconv.Itoa(mgrId) + `?0", "action":"get", "path":"Vehicle.VehicleIdentification.VIN", "requestId":"570415", "origin":"internal"}`
	transportMgrChan <- vinRequest

	// transportMgrChan is bidirectional (requests go server-ward, responses
	// come back on the same channel via transportDataSession). We rely on the
	// channel being unbuffered: if it were buffered our own send would sit in
	// the buffer and we'd echo-read it before transportDataSession consumes it.
	// vissv2server.initChannels explicitly keeps transportMgrChannel[2] unbuffered
	// for exactly this reason.
	select {
	case response := <-transportMgrChan:
		vin := extractVin(string(response))
		utils.Info.Printf("VIN=%s", vin)
		if !isValidVin(vin) {
			utils.Error.Printf("getVissV2Topic: refusing to build topic from invalid VIN=%q (set MQTT_VIN env var to override)", vin)
			return ""
		}
		return "/" + vin + "/Vehicle"
	case <-time.After(5 * time.Second):
		utils.Error.Printf("getVissV2Topic: timed out waiting for VIN response after 5s (set MQTT_VIN env var to override)")
		return ""
	}
}

// extractVin parses the VIN out of a server-core get response. The
// original implementation used strings.Index(response, "value") + 8
// and trusted that the literal sequence `value":"<VIN>"` appeared
// exactly that way — any pretty-printed JSON or whitespace shifted
// the +8 offset onto the wrong characters, and a malformed response
// caused either a wrong VIN or an OOB slice. Now we parse the
// response as JSON and look for the value field at the well-known
// nested location (data.dp.value) with a top-level fallback.
func extractVin(response string) string {
	var respMap map[string]interface{}
	if err := json.Unmarshal([]byte(response), &respMap); err != nil {
		utils.Error.Printf("extractVin: response not JSON: %s", response)
		return ""
	}
	// Preferred path: data.dp.value (matches getDataPackMap output).
	if data, ok := respMap["data"].(map[string]interface{}); ok {
		if dp, ok := data["dp"].(map[string]interface{}); ok {
			if v, ok := dp["value"].(string); ok {
				return v
			}
		}
		if v, ok := data["value"].(string); ok {
			return v
		}
	}
	// Fallback: top-level "value" (older formats / direct shape).
	if v, ok := respMap["value"].(string); ok {
		return v
	}
	utils.Error.Printf("extractVin: no value field found in response: %s", response)
	return ""
}

func decomposeMqttPayload(mqttPayload string) (string, string) { // {"topic":"X", "request":{...}}
	var payloadMap = make(map[string]interface{})
	utils.MapRequest(mqttPayload, &payloadMap)
	// Bug fix: the original used json.Marshal on payloadMap["topic"],
	// which returned `"X"` (with surrounding quotes), and that quoted
	// string was then handed to MQTT.Publish as the topic name —
	// producing literal `"X"` topics on the wire. Worse, a missing
	// "topic" key made Marshal return `"null"` (4 bytes), which
	// silently passed the len(topic)==0 check at the call site.
	// Type-assert to string instead so a missing/non-string topic
	// fails closed (empty return).
	topic, ok := payloadMap["topic"].(string)
	if !ok {
		utils.Error.Printf("decomposeMqttPayload: missing or non-string topic in: %s", mqttPayload)
		return "", ""
	}
	// The request field IS a JSON object that needs re-marshaling.
	payload, err := json.Marshal(payloadMap["request"])
	if err != nil {
		utils.Error.Printf("decomposeMqttPayload: cannot marshal request in:%s", mqttPayload)
		return topic, "corrupt request"
	}
	return topic, string(payload)
}

func AddRoutingInfoAndForward(reqMessage string, mgrId int, clientId int, transportMgrChan chan string) {
	newPrefix := "{ \"RouterId\":\"" + strconv.Itoa(mgrId) + "?" + strconv.Itoa(clientId) + "\", "
	request := strings.Replace(reqMessage, "{", newPrefix, 1)
	transportMgrChan <- request
}

func MqttMgrInit(mgrId int, transportMgrChan chan string) {
	mqttChannel = make(chan string)
	vissv2Channel := make(chan string)

	brokerSocket := getBrokerSocket(false)
	// wait for 2 seconds to allow the server and feeder to start
	time.Sleep(2 * time.Second)
	topic := getVissV2Topic(transportMgrChan, mgrId)
	if topic == "" {
		// Refused by isValidVin in getVissV2Topic — don't even try.
		utils.Error.Printf("MqttMgrInit: refusing to start; could not derive a valid VISS v2 topic")
		return
	}
	serverSubscription := mqttSubscribe(brokerSocket, topic)
	if serverSubscription == nil {
		utils.Error.Printf("MqttMgrInit: subscribe failed for topic=%s; manager not started", topic)
		return
	}
	topicId := 0
	topicList.nodes = 0

	utils.JsonSchemaInit()

	go vissV2Receiver(transportMgrChan, vissv2Channel) //message reception from server core

	utils.Info.Println("**** MQTT manager hub entering server loop... ****")

	// The original loop had a `default: time.Sleep(25 * time.Millisecond)`
	// arm that added 25ms of latency per message and woke this
	// goroutine 40 times per second forever. It's gone now: a select
	// with only blocking channel cases parks the goroutine until
	// something actually arrives. The Disconnect(250) call that lived
	// after this loop was also unreachable (the for has no break) and
	// has been removed.
	for {
		select {

		case mqttPayload := <-mqttChannel:
			topic, payload := decomposeMqttPayload(mqttPayload)
			if len(topic) == 0 {
				utils.Error.Printf("MQTT: Message from broker is corrupt:%s\nNot possible to respond to client", mqttPayload)
				continue
			}
			utils.Info.Printf("MQTT mgr hub: Message from broker:Topic=%s, Payload=%s", topic, payload)
			validationError := utils.JsonSchemaValidate(payload)
			if len(validationError) > 0 {
				// Bug fix: the original code published the error
				// response back to the client AND THEN fell through
				// to pushTopic + AddRoutingInfoAndForward, so the
				// schema-invalid payload was still routed inward to
				// the server core. `continue` short-circuits that
				// fall-through.
				var requestMap map[string]interface{}
				utils.MapRequest(payload, &requestMap)
				utils.SetErrorResponse(requestMap, errorResponseMap, 0, validationError) //bad_request
				publishMessage(serverSubscription, topic, utils.FinalizeMessage(errorResponseMap))
				continue
			}
			pushTopic(topic, topicId)
			AddRoutingInfoAndForward(payload, mgrId, topicId, transportMgrChan)
			topicId++

		case vissv2Message := <-vissv2Channel:
			utils.Info.Printf("MQTT hub: Message from VISSv2 server:%s", vissv2Message)
			// link routerId to topic, remove routerId from message, create mqtt message, send message to mqtt transport
			payload, topicHandle := utils.RemoveInternalData(string(vissv2Message))
			publishMessage(serverSubscription, getTopic(topicHandle), payload)
		}
	}
}
