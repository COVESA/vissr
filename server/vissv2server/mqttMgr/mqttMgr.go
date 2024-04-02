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
	"time"

	"github.com/covesa/vissr/utils"
	MQTT "github.com/eclipse/paho.mqtt.golang"
	//	"github.com/gorilla/websocket"
)

var mqttChannel chan string

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

func vissV2Receiver(transportMgrChan chan string, vissv2Channel chan string) {
	//	defer dataConn.Close()
	for {
		/*		_, response, err := dataConn.ReadMessage() // receive message from server core
				if err != nil {
					utils.Error.Println("Datachannel read error:" + err.Error())
					break
				}*/
		response := <-transportMgrChan
		utils.Info.Printf("MQTT mgr: Response from server core:%s\n", string(response))
		vissv2Channel <- string(response) // send message to hub
	}
}

// TODO add conf file
func getBrokerSocket(isSecure bool) string {
	//	FVTAddr := os.Getenv("MQTT_BROKER_ADDR")

	FVTAddr := "test.mosquitto.org"
	if FVTAddr == "" {
		FVTAddr = "127.0.0.1"
	}

	if isSecure == true {
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
		panic(token.Error())
	}
	if token := c.Subscribe(topic, 0, nil); token.Wait() && token.Error() != nil {
		utils.Error.Println(token.Error())
		os.Exit(1)
	}
	return c
}

func getTopic(topicId int) string {
	iterator := topicList.head
	for i := 0; i < topicList.nodes; i++ {
		if iterator.value.topicId == topicId {
			return iterator.value.topic
		}
		iterator = iterator.next
	}
	return ""
}

func pushTopic(topic string, topicId int) {
	var newNode Node
	newNode.value.topic = topic
	newNode.value.topicId = topicId
	newNode.next = nil

	if topicList.nodes == 0 {
		topicList.head = &newNode
		topicList.nodes++
		return
	}
	iterator := topicList.head
	for i := 0; i < topicList.nodes; i++ {
		if iterator.next == nil {
			iterator.next = &newNode
			break
		}
		iterator = iterator.next
	}
	topicList.nodes++
}

func popTopic(topicId int) { //TODO: to be used at unsubscribe, get, set responses from VISSv2
	if topicList.nodes > 0 && topicList.head.value.topicId == topicId {
		if topicList.nodes > 1 {
			topicList.head = topicList.head.next
		} else {
			topicList.head = nil
		}
		topicList.nodes--
	}
	iterator := topicList.head
	var previousNode *Node
	i := 0
	for i = 0; i < topicList.nodes; i++ {
		if iterator.value.topicId == topicId {
			break
		}
		previousNode = iterator
		iterator = iterator.next
	}
	if i == topicList.nodes {
		return
	}
	previousNode.next = iterator.next
	topicList.nodes--
}

func publishMessage(brokerSocket string, topic string, payload string) {
	utils.Info.Printf("publishMessage:Topic=%s, Payload=%s", topic, payload)
	opts := MQTT.NewClientOptions().AddBroker(brokerSocket)
	//    opts.SetClientID("VIN001")

	c := MQTT.NewClient(opts)
	if token := c.Connect(); token.Wait() && token.Error() != nil {
		utils.Error.Println(token.Error())
		os.Exit(1)
	}
	token := c.Publish(topic, 0, false, payload)
	token.Wait()
	c.Disconnect(250)
}

func getVissV2Topic(transportMgrChan chan string, mgrId int) string {
	vinRequest := "{\"RouterId\":\"" + strconv.Itoa(mgrId) + `?0", "action":"get", 
	"path":"Vehicle.VehicleIdentification.VIN", "requestId":"570415", "origin":"internal"}`
	/*	err := dataConn.WriteMessage(websocket.TextMessage, []byte(vinRequest))
		if err != nil {
			utils.Error.Println("Datachannel write error:" + err.Error())
		}
		_, response, err := dataConn.ReadMessage() // receive message from server core
		if err != nil {
			utils.Error.Println("Datachannel read error:" + err.Error())
			os.Exit(1)
		}*/
	transportMgrChan <- vinRequest
	response := <-transportMgrChan
	vin := extractVin(string(response))
	utils.Info.Printf("VIN=%s", vin)
	return "/" + vin + "/Vehicle"
}

func extractVin(response string) string {
	vinStartIndex := strings.Index(response, "value")
	if vinStartIndex == -1 {
		utils.Error.Printf("VIN cannot be extracted in %s", response)
		os.Exit(1)
	}
	vinStartIndex += 8 // value”:”
	vinEndIndex := utils.NextQuoteMark([]byte(response), vinStartIndex)
	return response[vinStartIndex:vinEndIndex]
}

func decomposeMqttPayload(mqttPayload string) (string, string) { // {"topic":"X", "request":"{...}"}
	var payloadMap = make(map[string]interface{})
	utils.MapRequest(mqttPayload, &payloadMap)
	payload, err := json.Marshal(payloadMap["request"])
	if err != nil {
		utils.Error.Printf("decomposeMqttPayload: cannot marshal request in response=%s", mqttPayload)
		os.Exit(1)
	}
	return payloadMap["topic"].(string), string(payload)
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
	serverSubscription := mqttSubscribe(brokerSocket, getVissV2Topic(transportMgrChan, mgrId))
	topicId := 0
	topicList.nodes = 0

	go vissV2Receiver(transportMgrChan, vissv2Channel) //message reception from server core

	utils.Info.Println("**** MQTT manager hub entering server loop... ****")

	for {
		select {

		case mqttPayload := <-mqttChannel:
			topic, payload := decomposeMqttPayload(mqttPayload)
			utils.Info.Printf("MQTT mgr hub: Message from broker:Topic=%s, Payload=%s\n", topic, payload)
			pushTopic(topic, topicId)
			AddRoutingInfoAndForward(payload, mgrId, topicId, transportMgrChan)
			topicId++

		case vissv2Message := <-vissv2Channel:
			utils.Info.Printf("MQTT hub: Message from VISSv2 server:%s\n", vissv2Message)
			// link routerId to topic, remove routerId from message, create mqtt message, send message to mqtt transport
			payload, topicHandle := utils.RemoveInternalData(string(vissv2Message))
			publishMessage(brokerSocket, getTopic(topicHandle), payload)

		default:
			time.Sleep(25 * time.Millisecond)
		}
	}

	serverSubscription.Disconnect(250)
}
