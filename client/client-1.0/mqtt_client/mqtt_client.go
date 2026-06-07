/**
* (C) 2021 Mitsubishi Electrics Automotive
* (C) 2021 Geotab
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE file in this repository.
*
**/
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/akamensky/argparse"
	"github.com/covesa/vissr/utils"
	MQTT "github.com/eclipse/paho.mqtt.golang"
)

var uniqueTopicName string

func getBrokerSocket(isSecure bool) string {
	FVTAddr := "test.mosquitto.org"
	if FVTAddr == "" {
		FVTAddr = "127.0.0.1"
	}
	if isSecure {
		return "ssl://" + FVTAddr + ":8883"
	}
	return "tcp://" + FVTAddr + ":1883"
}

var publishHandler MQTT.MessageHandler = func(client MQTT.Client, msg MQTT.Message) {
	fmt.Printf("Topic=%s\n", msg.Topic())
	fmt.Printf("Payload=%s\n", string(msg.Payload()))
}

func mqttSubscribe(brokerSocket string, topic string) {
	fmt.Printf("mqttSubscribe:Topic=%s\n", topic)
	opts := MQTT.NewClientOptions().AddBroker(brokerSocket)
	opts.SetDefaultPublishHandler(publishHandler)

	c := MQTT.NewClient(opts)
	if token := c.Connect(); token.Wait() && token.Error() != nil {
		utils.Error.Printf("mqttSubscribe: broker connect failed: %v", token.Error())
		os.Exit(1)
	}
	if token := c.Subscribe(topic, 0, nil); token.Wait() && token.Error() != nil {
		utils.Error.Println(token.Error())
		os.Exit(1)
	}
}

func publishMessage(brokerSocket string, topic string, payload string) {
	fmt.Printf("publishMessage:Topic=%s, Payload=%s\n", topic, payload)
	opts := MQTT.NewClientOptions().AddBroker(brokerSocket)

	c := MQTT.NewClient(opts)
	if token := c.Connect(); token.Wait() && token.Error() != nil {
		utils.Error.Println(token.Error())
		os.Exit(1)
	}
	token := c.Publish(topic, 0, false, payload)
	token.Wait()
	c.Disconnect(250)
}

func subscribeVissV2Response(brokerSocket string) {
	mqttSubscribe(brokerSocket, uniqueTopicName)
}

// buildPublishPayload wraps a raw VISS JSON request in the MQTT transport
// envelope: {"topic":"<replyTopic>","request":{...}}.
// Returns ("", false) when replyTopic is empty or request is not valid JSON.
func buildPublishPayload(replyTopic, request string) (string, bool) {
	if replyTopic == "" {
		return "", false
	}
	if !json.Valid([]byte(request)) {
		utils.Error.Printf("mqtt_client: request is not valid JSON: %s", request)
		return "", false
	}
	envelope := map[string]interface{}{
		"topic":   replyTopic,
		"request": json.RawMessage(request),
	}
	b, _ := json.Marshal(envelope)
	return string(b), true
}

func publishVissV2Request(brokerSocket string, vin string, request string) {
	payload, ok := buildPublishPayload(uniqueTopicName, request)
	if !ok {
		return
	}
	publishMessage(brokerSocket, "/"+vin+"/Vehicle", payload)
}

func main() {
	parser := argparse.NewParser("print", "mqtt client")
	logFile := parser.Flag("", "logfile", &argparse.Options{Required: false, Help: "outputs to logfile in ./logs folder"})
	logLevel := parser.Selector("", "loglevel", []string{"trace", "debug", "info", "warn", "error", "fatal", "panic"}, &argparse.Options{
		Required: false,
		Help:     "changes log output level",
		Default:  "info"})
	vin := parser.String("", "vin", &argparse.Options{
		Required: true,
		Help:     "VIN Number",
		Default:  "ULFB0"})
	if err := parser.Parse(os.Args); err != nil {
		fmt.Print(parser.Usage(err))
		os.Exit(1)
	}

	utils.InitLog("mqtt-client-log.txt", "./logs", *logFile, *logLevel)

	brokerSocket := getBrokerSocket(false)

	var request string
	i := 0
	continueLoop := true
	fmt.Printf("\nSet unique topic name:")
	fmt.Scanf("%s", &uniqueTopicName)
	subscribeVissV2Response(brokerSocket)
	for continueLoop {
		fmt.Printf("\nVISSv2-request (or q to quit):")
		fmt.Scanf("%s", &request)
		if len(request) == 0 {
			continue
		}
		switch request[0] {
		case 'q':
			continueLoop = false
		default:
			publishVissV2Request(brokerSocket, *vin, request)
		}
		i++
		if i == 25 {
			fmt.Printf("Max number of requests reached. Goodbye.\n")
			continueLoop = false
		}
		time.Sleep(2 * time.Second)
	}
}
