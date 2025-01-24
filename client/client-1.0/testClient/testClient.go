/**
* (C) 2023 Ford Motor Company
* (C) 2022 Geotab Inc
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE file in this repository.
*
**/

package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"flag"
	"io"
	"bytes"
	"net/http"
	"net/url"
	"os"
//	"strconv"
	"strings"

	"fmt"
	"time"

	"github.com/akamensky/argparse"
	"github.com/covesa/vissr/utils"
	"github.com/gorilla/websocket"
	MQTT "github.com/eclipse/paho.mqtt.golang"
	pb "github.com/covesa/vissr/grpc_pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// the server components started as threads by vissv2server. If a component is commented out, it will not be started
var testedProtocols []string = []string{
	"http",
	"ws",
	"grpc",
	"mqtt",
}

var clientCert tls.Certificate
var caCertPool x509.CertPool

const (
	address = "0.0.0.0"
	name    = "VISSv2-gRPC-client"
)

var doneChannel []chan bool

var httpCommandList []string
var wsCommandList []string
var mqttCommandList []string
var grpcCommandList []string

const http_url = "http://0.0.0.0:8888"

func pathToUrl(path string) string {
	var url string = strings.Replace(path, ".", "/", -1)
	return "/" + url
}

func jsonToStructList(jsonList string) int {
	protocols := 0
	var reqList map[string]interface{}
	err := json.Unmarshal([]byte(jsonList), &reqList)
	if err != nil {
		fmt.Printf("jsonToStructList:error jsonList=%s", jsonList)
		return 0
	}
	if reqList["http"] != nil {
		httpCommandList = createList(reqList["http"])
		protocols++
	}
	if reqList["ws"] != nil {
		wsCommandList = createList(reqList["ws"])
		protocols++
	}
	if reqList["mqtt"] != nil {
		mqttCommandList = createList(reqList["mqtt"])
		protocols++
	}
	if reqList["grpc"] != nil {
		grpcCommandList = createList(reqList["grpc"])
		protocols++
	}
	return protocols
}

func createList(commandMap interface{}) []string {
	var commandList []string
	switch vv := commandMap.(type) {
	case []interface{}:
//		fmt.Println(vv, "is an array:, len=",len(vv))
		commandList = make([]string, len(vv))
		for i := 0; i < len(vv); i++ {
			commandList[i] = retrieveRequest(vv[i].(map[string]interface{}))
		}
	case map[string]interface{}:
//		fmt.Println(vv, "is a map:")
		commandList = make([]string, 1)
		commandList[0] = retrieveRequest(vv)
	default:
		fmt.Println(vv, "is of an unknown type")
	}
	return commandList
}

func retrieveRequest(jsonRequest map[string]interface{}) string {
	request, err := json.Marshal(jsonRequest)
	if err != nil {
		fmt.Print("retrieveRequest(): JSON array encode failed. ", err)
		return ""
	}
	return string(request)
}

func createListFromFile(fname string) int {
	data, err := os.ReadFile(fname)
	if err != nil {
		fmt.Printf("Error reading file=%s", fname)
		return 0
	}
	return jsonToStructList(string(data))
}

func initVissV2WebSocket() *websocket.Conn {
	scheme := "ws"
	portNum := "8080"
	if secConfig.TransportSec == "yes" {
		scheme = "wss"
		portNum = secConfig.WsSecPort
		websocket.DefaultDialer.TLSClientConfig = &tls.Config{
			Certificates: []tls.Certificate{clientCert},
			RootCAs:      &caCertPool,
		}
	}
	var addr = flag.String("addr", "localhost:"+portNum, "http service address")
	dataSessionUrl := url.URL{Scheme: scheme, Host: *addr, Path: ""}
	subProtocol := make([]string, 1)
	subProtocol[0] = "VISSv2"
	dialer := websocket.Dialer{
		HandshakeTimeout: time.Second,
		ReadBufferSize:   1024,
		WriteBufferSize:  1024,
		Subprotocols:     subProtocol,
	}
	conn, _, err := dialer.Dial(dataSessionUrl.String(), nil)
	if err != nil {
		fmt.Printf("Data session dial error:%s\n", err)
		os.Exit(1)
	}
	return conn
}

func getWsResponse(conn *websocket.Conn, request []byte) string {
	err := conn.WriteMessage(websocket.BinaryMessage, request)
	if err != nil {
		fmt.Printf("Request error:%s\n", err)
		return ""
	}
	_, msg, err := conn.ReadMessage()
	if err != nil {
		fmt.Printf("Response error: %s\n", err)
		return ""
	}
	return string(msg)
}

func performWsCommand(command string, conn *websocket.Conn) {
	fmt.Printf("Request: %s\n", command)
	jsonResponse := getWsResponse(conn, []byte(command))
	fmt.Printf("Response: %s\n", jsonResponse)
	if strings.Contains(command, `"subscribe"`) {
		events := 0
		for {
			_, event, err := conn.ReadMessage()
			if err != nil {
				fmt.Printf("Notification error: %s\n", err)
				return
			}
			fmt.Printf("Notification: %s\n", string(event))
			events++
			if events == 2 {
				subscriptionId := utils.ExtractSubscriptionId(jsonResponse)
				unsubReq := `{"action":"unsubscribe", "subscriptionId":"` + subscriptionId + `", "requestId":"123"}`
				fmt.Printf("Two events received. Terminating subscription session\n")
				performWsCommand(unsubReq, conn)
				return
			}
		}
	}
}

func performHttpCommand(command string) {
	var err error
	var req *http.Request
	var reqMap map[string]interface{}
	utils.MapRequest(command, &reqMap)
	url := http_url + "/" + reqMap["path"].(string)
	if reqMap["action"] == "set" {
		body := `{"value":"` + reqMap["value"].(string) + `"}`
		fmt.Printf("Request: POST: %s %s\n", url, body)
		req, err = http.NewRequest("POST", url, bytes.NewBuffer([]byte(body)))
	} else {
		if reqMap["filter"] != nil {
			filter, err2 := json.Marshal(reqMap["filter"])
			if err2 != nil {
				fmt.Printf("Filter syntax error: %s\n", err2)
				return
			}
			url += `?filter=`+ string(filter)
		}
		fmt.Printf("Request: GET: %s\n", url)
		req, err = http.NewRequest("GET", url, nil)
	}
	if err != nil {
		fmt.Printf("HTTP request init error: %s\n", err)
		return
	}

	req.Header.Add("Content-Type", "application/json")
	client := &http.Client{}
	var resp *http.Response
	resp, err = client.Do(req)
	if err != nil {
		fmt.Printf("HTTP request call error: %s\n", err)
		return
	}
	defer resp.Body.Close()

	var bdy []byte
	bdy, err = io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("HTTP response decode error: %s\n", err)
		return
	}
	fmt.Printf("Response: %s, %s\n", resp.Status, string(bdy))
}

func httpTesterRun(httpCommandList []string, doneChannel chan bool) {
	ok := true
	fmt.Printf("\n********** HTTP testing **************\n")
	for i := 0; i < len(httpCommandList); i++ {
		performHttpCommand(httpCommandList[i])
		fmt.Printf("\n\n")
		time.Sleep(1 * time.Second)
	}
	doneChannel <- ok
}

func wsTesterRun(wsCommandList []string, doneChannel chan bool) {
	ok := true
	conn := initVissV2WebSocket()
	fmt.Printf("\n********** Websocket testing **************\n")
	for i := 0; i < len(wsCommandList); i++ {
		performWsCommand(wsCommandList[i], conn)
		fmt.Printf("\n\n")
		time.Sleep(1 * time.Second)
	}
	doneChannel <- ok
}

func getBrokerSocket(isSecure bool) string {
	FVTAddr := "test.mosquitto.org"
	if isSecure == true {
		return "ssl://" + FVTAddr + ":8883"
	}
	return "tcp://" + FVTAddr + ":1883"
}

func mqttSubscribe(brokerSocket string, topic string) {
	fmt.Printf("mqttSubscribe:Topic=%s\n", topic)
	opts := MQTT.NewClientOptions().AddBroker(brokerSocket)
	opts.SetDefaultPublishHandler(publishHandler)

	c := MQTT.NewClient(opts)
	if token := c.Connect(); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}
	if token := c.Subscribe(`"` + topic + `"`, 0, nil); token.Wait() && token.Error() != nil {
		fmt.Println(token.Error())
		os.Exit(1)
	}
}

var events int = 0
var brokerSocket, clientTopic, serverTopic string
var publishHandler MQTT.MessageHandler = func(client MQTT.Client, msg MQTT.Message) {
//	fmt.Printf("Topic=%s\n", msg.Topic())
	if strings.Contains(string(msg.Payload()), `"subscribe"`) {
		fmt.Printf("Response=%s\n", string(msg.Payload()))
		events++
	} else 
	if strings.Contains(string(msg.Payload()), `subscription"`) {
		if events == 1 {
			fmt.Printf("\n")
		}
		fmt.Printf("Event=%s\n", string(msg.Payload()))
		events++
		if events == 3 {
				subscriptionId := utils.ExtractSubscriptionId(string(msg.Payload()))
				unsubReq := `{"action":"unsubscribe", "subscriptionId":"` + subscriptionId + `", "requestId":"123"}`
				fmt.Printf("Two events received. Unsubscribing to subscription session\n")
				publishVissV2Request(brokerSocket, unsubReq, clientTopic, serverTopic)
				events = 0
		}
	} else {
		fmt.Printf("Response=%s\n\n", string(msg.Payload()))
	}
}

func publishVissV2Request(brokerSocket string, request string, clientTopic string, serverTopic string) {
	payload := `{"topic":"` + clientTopic + `", "request":` + request + `}`
	publishMessage(brokerSocket, "/" + serverTopic, payload)
}

func publishMessage(brokerSocket string, topic string, payload string) {
	fmt.Printf("publishMessage:Topic=%s, Request=%s\n", topic, payload)
	opts := MQTT.NewClientOptions().AddBroker(brokerSocket)

	c := MQTT.NewClient(opts)
	if token := c.Connect(); token.Wait() && token.Error() != nil {
		fmt.Println(token.Error())
		os.Exit(1)
	}
	token := c.Publish(topic, 0, false, payload)
	token.Wait()
	c.Disconnect(250)
}

func mqttTesterRun(mqttCommandList []string, doneChannel chan bool) {
	ok := true
	fmt.Printf("\n********** MQTT testing **************\n")
	brokerSocket = getBrokerSocket(false)
	clientTopic = "VISSv3.0/testClient"
	mqttSubscribe(brokerSocket, clientTopic)
	serverTopic = "Data-not-found/Vehicle"  //Data-not-found is the value used by the serverr MQTT mgr if the tree does not have a default value set
	for i := 0; i < len(mqttCommandList); i++ {
		publishVissV2Request(brokerSocket, mqttCommandList[i], clientTopic, serverTopic)
		time.Sleep(2 * time.Second)
	}
	doneChannel <- ok
}

func initGrpcSession() *grpc.ClientConn {
	var conn *grpc.ClientConn
	var err error
	if secConfig.TransportSec == "yes" {
		config := &tls.Config{
			Certificates: []tls.Certificate{clientCert},
			RootCAs:      &caCertPool,
		}
		tlsCredentials := credentials.NewTLS(config)
		portNo := secConfig.GrpcSecPort
		conn, err = grpc.Dial(address+portNo, grpc.WithTransportCredentials(tlsCredentials), grpc.WithBlock())
	} else {
		// grpc.Dial

//		fmt.Printf("connecting to port = 8887")
		conn, err = grpc.Dial(address+":8887", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	}
	if err != nil {
		fmt.Printf("did not connect: %v", err)
		return nil
	}
	return conn
}

func performGrpcCommand(vssRequest string, client pb.VISSClient) {
	var reqMap map[string]interface{}
	utils.MapRequest(vssRequest, &reqMap)
	fmt.Printf("Request=:%s\n", vssRequest)
	ctx, _ := context.WithTimeout(context.Background(), 6*time.Second)
	switch reqMap["action"].(string) {
	case "get":
		pbRequest := utils.GetRequestJsonToPb(vssRequest)
		pbResponse, err := client.GetRequest(ctx, pbRequest)
		if err == nil {
			vssResponse := utils.GetResponsePbToJson(pbResponse)
			fmt.Printf("Response:%s\n\n", vssResponse)
		} else {
			fmt.Printf("Response error:%s\n", err)
		}
	case "set":
		pbRequest := utils.SetRequestJsonToPb(vssRequest)
		pbResponse, err := client.SetRequest(ctx, pbRequest)
		if err == nil {
			vssResponse := utils.SetResponsePbToJson(pbResponse)
			fmt.Printf("Response:%s\n\n", vssResponse)
		} else {
			fmt.Printf("Response error:%s\n", err)
		}
	case "subscribe":
		pbRequest := utils.SubscribeRequestJsonToPb(vssRequest)
		stream, err := client.SubscribeRequest(ctx, pbRequest)
		if err == nil {
			events := 0
			for {
				pbResponse, err := stream.Recv()
				if err != nil {
					fmt.Printf("Error=%v when issuing request=:%s", err, vssRequest)
					break
				}
				events++
				vssResponse := utils.SubscribeStreamPbToJson(pbResponse)
				fmt.Printf("Received response:%s\n", vssResponse)
				if events == 3 {
					subscriptionId := utils.ExtractSubscriptionId(vssResponse)
					unsubReq := `{"action":"unsubscribe", "subscriptionId":"` + subscriptionId + `", "requestId":"123"}`
					fmt.Printf("Two events received. Terminating subscription session\n")
					performGrpcCommand(unsubReq, client)
					break
				}
			}
//			fmt.Printf("Response:%s\n\n", vssResponse)
		} else {
			fmt.Printf("Response error:%s\n", err)
		}
	case "unsubscribe":
		pbRequest := utils.UnsubscribeRequestJsonToPb(vssRequest)
		pbResponse, err := client.UnsubscribeRequest(ctx, pbRequest)
		if err == nil {
			vssResponse := utils.UnsubscribeResponsePbToJson(pbResponse)
			fmt.Printf("Response:%s\n\n", vssResponse)
		} else {
			fmt.Printf("Response error:%s\n", err)
		}
	}
}

func grpcTesterRun(grpcCommandList []string, doneChannel chan bool) {
	ok := true
	fmt.Printf("\n********** gRPC testing **************\n")
	conn := initGrpcSession()
	if conn == nil {
		fmt.Printf("Failed to connect to server. Exiting gRPC tests.\n")
		doneChannel <- ok
		return
	}
	defer conn.Close()
	client := pb.NewVISSClient(conn)
	for i := 0; i < len(grpcCommandList); i++ {
		performGrpcCommand(grpcCommandList[i], client)
		time.Sleep(1 * time.Second)
	}
	doneChannel <- ok
}

func waitForKey(message string) {
	var anyKey string
	fmt.Printf(message)
	fmt.Scanf("%s", &anyKey)
}

func main() {
	// Create new parser object
	parser := argparse.NewParser("print", "Test client")

	// Create flags
//	prot := parser.Selector("p", "protocol", []string{"http", "ws"}, &argparse.Options{Required: false,
//		Help: "Protocol must be either http or websocket", Default: "ws"})
	logFile := parser.Flag("", "logfile", &argparse.Options{Required: false, Help: "outputs to logfile in ./logs folder"})
	logLevel := parser.Selector("", "loglevel", []string{"trace", "debug", "info", "warn", "error", "fatal", "panic"}, &argparse.Options{
		Required: false,
		Help:     "changes log output level",
		Default:  "info"})

	// Parse input
	err := parser.Parse(os.Args)
	if err != nil {
		fmt.Print(parser.Usage(err))
		//exits due to required info not provided by user
		os.Exit(1)
	}

	utils.InitLog("testClient-log.txt", "./logs", *logFile, *logLevel)  //used in utils functions...

	readTransportSecConfig()
	fmt.Printf("secConfig.TransportSec=%s", secConfig.TransportSec)
	if secConfig.TransportSec == "yes" {
		caCertPool = *prepareTransportSecConfig()
	}

	doneChannel = make([]chan bool, 4)
	for i := 0; i < 4; i++ {
		doneChannel[i] = make(chan bool)
	}

	if createListFromFile("testRequests.json") == 0 {
		fmt.Printf("Failed in creating list from testRequests.json")
		os.Exit(1)
	}

	time.Sleep(2 * time.Second)  //give the server a few secs to startup...

	var protocolId int
	for _, testedProtocols := range testedProtocols {
		switch testedProtocols {
		case "http":
			if len(httpCommandList ) > 0 {
				protocolId = 0
				go httpTesterRun(httpCommandList, doneChannel[protocolId])
			}
		case "ws":
			if len(wsCommandList ) > 0 {
				protocolId = 1
				go wsTesterRun(wsCommandList, doneChannel[protocolId])
			}
		case "mqtt":
			if len(mqttCommandList ) > 0 {
				protocolId = 2
				go mqttTesterRun(mqttCommandList, doneChannel[protocolId])
			}
		case "grpc":
			if len(grpcCommandList ) > 0 {
				protocolId = 3
				go grpcTesterRun(grpcCommandList, doneChannel[protocolId])
			}
		}
		ok := <- doneChannel[protocolId] // wait until each protocol tester is done
		waitForKey("Hit return key to continue:") // provide time to look at results before continue
		if !ok {
			break
		}
	}

//	waitForKey("Hit return key to terminate:")
}
