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
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/akamensky/argparse"
	"github.com/covesa/vissr/utils"
	MQTT "github.com/eclipse/paho.mqtt.golang"
)

var uniqueTopicName string

// getBrokerSocket returns the MQTT broker URL. The broker address is read from
// the MQTT_BROKER_ADDR environment variable, defaulting to a local broker
// (127.0.0.1). This matches the behavior of the server's mqttMgr.
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
	fmt.Printf("Topic=%s\n", msg.Topic())
	fmt.Printf("Payload=%s\n", string(msg.Payload()))
}

// newMqttClient creates the MQTT client. It is a variable so that tests can supply a client that does not
// need a broker.
var newMqttClient = MQTT.NewClient

// mqttSubscribe connects to the broker and subscribes to topic. It returns the connected client, which the
// caller keeps for as long as it wants the subscription and then disconnects, or an error if the broker
// cannot be reached or the subscription fails (the client is then disconnected).
func mqttSubscribe(brokerSocket string, topic string) (MQTT.Client, error) {
	fmt.Printf("mqttSubscribe:Topic=%s\n", topic)
	opts := MQTT.NewClientOptions().AddBroker(brokerSocket)
	opts.SetDefaultPublishHandler(publishHandler)

	c := newMqttClient(opts)
	if token := c.Connect(); token.Wait() && token.Error() != nil {
		utils.Error.Printf("mqttSubscribe: broker connect failed: %v", token.Error())
		return nil, token.Error()
	}
	if token := c.Subscribe(topic, 0, nil); token.Wait() && token.Error() != nil {
		utils.Error.Println(token.Error())
		c.Disconnect(250)
		return nil, token.Error()
	}
	return c, nil
}

// publishMessage connects to the broker, publishes the payload on topic and disconnects. It returns an
// error if the broker cannot be reached or the publish fails.
func publishMessage(brokerSocket string, topic string, payload string) error {
	fmt.Printf("publishMessage:Topic=%s, Payload=%s\n", topic, payload)
	opts := MQTT.NewClientOptions().AddBroker(brokerSocket)

	c := newMqttClient(opts)
	if token := c.Connect(); token.Wait() && token.Error() != nil {
		utils.Error.Println(token.Error())
		return token.Error()
	}
	defer c.Disconnect(250)
	token := c.Publish(topic, 0, false, payload)
	token.Wait()
	if err := token.Error(); err != nil {
		utils.Error.Println(err)
		return err
	}
	return nil
}

// readLine returns the next line of r without the line ending and without surrounding white space. A line
// is read as a whole: a request is JSON and often has spaces in it, which fmt.Scanf("%s") would cut it at,
// leaving the rest to be read as the next request. ok is false at the end of the input.
func readLine(r *bufio.Reader) (line string, ok bool) {
	text, err := r.ReadString('\n')
	if err != nil && (err != io.EOF || text == "") {
		return "", false
	}
	return strings.TrimSpace(text), true
}

func subscribeVissV2Response(brokerSocket string) (MQTT.Client, error) {
	return mqttSubscribe(brokerSocket, uniqueTopicName)
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

func publishVissV2Request(brokerSocket string, vin string, request string) error {
	payload, ok := buildPublishPayload(uniqueTopicName, request)
	if !ok {
		return fmt.Errorf("the request is not valid JSON, or no reply topic is set")
	}
	return publishMessage(brokerSocket, "/"+vin+"/Vehicle", payload)
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

	input := bufio.NewReader(os.Stdin)
	i := 0
	continueLoop := true
	fmt.Printf("\nSet unique topic name:")
	var ok bool
	if uniqueTopicName, ok = readLine(input); !ok || uniqueTopicName == "" {
		fmt.Printf("No reply topic given. Goodbye.\n")
		os.Exit(1)
	}
	subscriber, err := subscribeVissV2Response(brokerSocket)
	if err != nil {
		fmt.Printf("Cannot subscribe to the reply topic on %s: %v\n", brokerSocket, err)
		os.Exit(1)
	}
	defer subscriber.Disconnect(250)
	for continueLoop {
		fmt.Printf("\nVISSv2-request (or q to quit):")
		request, ok := readLine(input)
		if !ok { // the input has ended
			break
		}
		if len(request) == 0 {
			continue
		}
		switch request[0] {
		case 'q':
			continueLoop = false
		default:
			if err := publishVissV2Request(brokerSocket, *vin, request); err != nil {
				fmt.Printf("Request not sent: %v\n", err)
			}
		}
		i++
		if i == 25 {
			fmt.Printf("Max number of requests reached. Goodbye.\n")
			continueLoop = false
		}
		time.Sleep(2 * time.Second)
	}
}
