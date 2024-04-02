/**
* (C) 2022 Geotab Inc
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE file in this repository.
*
**/

package main

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/akamensky/argparse"
	"github.com/covesa/vissr/utils"
	"github.com/gorilla/websocket"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

var commandNumber string

var clientCert tls.Certificate
var caCertPool x509.CertPool

var vissv2Url string
var protocol string
var compression string

type RequestList struct {
	Request []string
}

var requestList RequestList

func pathToUrl(path string) string {
	var url string = strings.Replace(path, ".", "/", -1)
	return "/" + url
}

func jsonToStructList(jsonList string) int {
	var reqList map[string]interface{}
	err := json.Unmarshal([]byte(jsonList), &reqList)
	if err != nil {
		utils.Error.Printf("jsonToStructList:error jsonList=%s", jsonList)
		return 0
	}
	switch vv := reqList["request"].(type) {
	case []interface{}:
		//        utils.Info.Println(jsonList, "is an array:, len=",strconv.Itoa(len(vv)))
		requestList.Request = make([]string, len(vv))
		for i := 0; i < len(vv); i++ {
			requestList.Request[i] = retrieveRequest(vv[i].(map[string]interface{}))
		}
	case map[string]interface{}:
		//        utils.Info.Println(jsonList, "is a map:")
		requestList.Request = make([]string, 1)
		requestList.Request[0] = retrieveRequest(vv)
	default:
		//        utils.Info.Println(vv, "is of an unknown type")
	}
	return len(requestList.Request)
}

func retrieveRequest(jsonRequest map[string]interface{}) string {
	request, err := json.Marshal(jsonRequest)
	if err != nil {
		utils.Error.Print("retrieveRequest(): JSON array encode failed. ", err)
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

func initVissV2WebSocket(compression string) *websocket.Conn {
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
	var addr = flag.String("addr", vissv2Url+":"+portNum, "http service address")
	dataSessionUrl := url.URL{Scheme: scheme, Host: *addr, Path: ""}
	subProtocol := make([]string, 1)
	subProtocol[0] = "VISSv2" + compression
	dialer := websocket.Dialer{
		HandshakeTimeout: time.Second,
		ReadBufferSize:   1024,
		WriteBufferSize:  1024,
		Subprotocols:     subProtocol,
	}
	conn, _, err := dialer.Dial(dataSessionUrl.String(), nil)
	if err != nil {
		fmt.Printf("Data session dial error:%s\n", err)
		os.Exit(-1)
	}
	return conn
}

func getResponse(conn *websocket.Conn, request []byte) []byte {
	err := conn.WriteMessage(websocket.BinaryMessage, request)
	if err != nil {
		fmt.Printf("Request error:%s\n", err)
		return nil
	}
	_, msg, err := conn.ReadMessage()
	if err != nil {
		fmt.Printf("Response error: %s\n", err)
		return nil
	}
	return msg
}

func performCommand(commandNumber int, conn *websocket.Conn, optionChannel chan string, unsubChannel chan string) {
	fmt.Printf("Request: %s\n", requestList.Request[commandNumber])
	if compression == "pbl1" || compression == "pbl2" {
		performPbCommand(commandNumber, conn, optionChannel)
	} else if compression == "prop" {
		performPropCommand(commandNumber, conn, optionChannel)
	} else {
		performNoneCommand(commandNumber, conn, optionChannel, unsubChannel)
	}
}

func performNoneCommand(commandNumber int, conn *websocket.Conn, optionChannel chan string, unsubChannel chan string) {
	fmt.Printf("Compression is not applied. If subscribe request is issued, response data will be saved in the file data.csv\n")

	jsonResponse := string(getResponse(conn, []byte(requestList.Request[commandNumber])))
	fmt.Printf("Response: %s\n", jsonResponse)
	if strings.Contains(requestList.Request[commandNumber], "subscribe") == true {
		subscriptionId := utils.ExtractSubscriptionId(jsonResponse)
		unsubReq := `{"action":"unsubscribe", "subscriptionId":"` + subscriptionId + `"}`
		unsubChannel <- unsubReq
		maxArrayLen := 10000
		valArray := make([]string, maxArrayLen)
		tsArray := make([]string, maxArrayLen)
		sessionDone := false
		arrayIndex := 0
		finalIterations := -1
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				fmt.Printf("Notification error: %s\n", err)
				return
			}
			jsonNotification := string(msg)
			if strings.Contains(jsonNotification, "unsubscribe") == true {
				fmt.Printf("Response: %s\n", jsonNotification)
				sessionDone = true
				finalIterations = 1 // 2 more iteration if last buffer has saved dp(s). If not, change to 1.
			} else {
				finalIterations--
				fmt.Printf("Notification: %s\n", jsonNotification)
				if arrayIndex < maxArrayLen {
					arrayIndex = storeinArrays(jsonNotification, valArray, tsArray, arrayIndex)
				} else {
					fmt.Printf("Maximum number of datapoints are saved. Recording terminated.\n")
				}
			}
			if sessionDone == true && finalIterations == 0 {
				fmt.Printf("Number of datapoints saved: %d\n", arrayIndex)
				saveInCsv(valArray, tsArray, arrayIndex)
				return
			}
		}
	} else { // get request
		fmt.Printf("Response: %s\n", jsonResponse)
	}
}

/* TODO: Current impl only supports notifications that do not contain data arrays ([]data).
* To support data[] (multiple signals in the notification), arrayIndex would need to be an array, and tsArray/valArray would have to be 2-dim arrays
 */
func storeinArrays(jsonNotification string, valArray []string, tsArray []string, arrayIndex int) int { // can be any of the four response formats that VISSv2 specifies...
	var notificationMap = make(map[string]interface{})
	utils.MapRequest(jsonNotification, &notificationMap)
	return processDataLevel1(notificationMap["data"], valArray, tsArray, arrayIndex)
}

func processDataLevel1(dataObject interface{}, valArray []string, tsArray []string, arrayIndex int) int { // data or []data level
	switch vv := dataObject.(type) {
	case []interface{}: // []data
		//		utils.Info.Println(dataObject, "is an array:, len=", strconv.Itoa(len(vv)))
		arrayIndex = processDataLevel2(vv, valArray, tsArray, arrayIndex)
	case map[string]interface{}:
		//		utils.Info.Println(dataObject, "is a map:")
		arrayIndex = processDataLevel3(vv, valArray, tsArray, arrayIndex)
	default:
		utils.Info.Println(dataObject, "is of an unknown type")
	}
	return arrayIndex
}

func processDataLevel2(dataArray []interface{}, valArray []string, tsArray []string, arrayIndex int) int { // []data level
	for k, v := range dataArray {
		switch vv := v.(type) {
		case map[string]interface{}:
			//			utils.Info.Println(k, "is a map:")
			arrayIndex = processDataLevel3(vv, valArray, tsArray, arrayIndex)
		default:
			utils.Info.Println(k, "is of an unknown type")
		}
	}
	return arrayIndex
}

func processDataLevel3(data map[string]interface{}, valArray []string, tsArray []string, arrayIndex int) int { // inside data, dp or []dp level
	for k, v := range data {
		switch vv := v.(type) {
		case []interface{}: // []dp
			//			utils.Info.Println(vv, "is an array:, len=", strconv.Itoa(len(vv)))
			arrayIndex = processDataLevel4(vv, valArray, tsArray, arrayIndex)
		case map[string]interface{}:
			//			utils.Info.Println(k, "is a map:")
			arrayIndex = processDataLevel5(vv, valArray, tsArray, arrayIndex)
		case string: // path
			//			utils.Info.Println(k, "is string", vv)
		default:
			utils.Info.Println(k, "is of an unknown type")
		}
	}
	return arrayIndex
}

func processDataLevel4(dpArray []interface{}, valArray []string, tsArray []string, arrayIndex int) int { // []dp level
	for k, v := range dpArray {
		switch vv := v.(type) {
		case map[string]interface{}:
			//			utils.Info.Println(k, "is a map:")
			arrayIndex = processDataLevel5(vv, valArray, tsArray, arrayIndex)
		default:
			utils.Info.Println(k, "is of an unknown type")
		}
	}
	return arrayIndex
}

func processDataLevel5(dp map[string]interface{}, valArray []string, tsArray []string, arrayIndex int) int { // inside dp level
	for k, v := range dp {
		switch vv := v.(type) {
		case string:
			//			utils.Info.Println(k, "is string", vv)
			if k == "value" {
				valArray[arrayIndex] = vv
			} else if k == "ts" {
				tsArray[arrayIndex] = vv
			}
		default:
			utils.Info.Println(k, "is of an unknown type")
		}
	}
	return arrayIndex + 1
}

func saveInCsv(valArray []string, tsArray []string, arrayIndex int) {
	treeFp, err := os.OpenFile("data.csv", os.O_RDWR|os.O_CREATE, 0755)
	if err != nil {
		fmt.Printf("Could not open data.csv for writing.\n")
		return
	}
	for i := 0; i < arrayIndex; i++ {
		treeFp.Write([]byte(tsArray[i]))
		treeFp.Write([]byte(", "))
	}
	treeFp.Write([]byte("\n"))

	for i := 0; i < arrayIndex; i++ {
		treeFp.Write([]byte(valArray[i]))
		treeFp.Write([]byte(", "))
	}
	treeFp.Write([]byte("\n"))

	treeFp.Close()
}

func performPropCommand(commandNumber int, conn *websocket.Conn, optionChannel chan string) {
	compressedRequest := utils.CompressMessage([]byte(requestList.Request[commandNumber]))
	fmt.Printf("JSON request size= %d, Compressed request size=%d\n", len(requestList.Request[commandNumber]), len(compressedRequest))
	fmt.Printf("Compression= %d%\n", (100*len(requestList.Request[commandNumber]))/len(compressedRequest))
	compressedResponse := getResponse(conn, compressedRequest)
	jsonResponse := string(utils.DecompressMessage([]byte(compressedResponse)))
	fmt.Printf("Response: %s\n", jsonResponse)
	fmt.Printf("JSON response size= %d, Compressed response size=%d\n", len(jsonResponse), len(compressedResponse))
	fmt.Printf("Compression= %d%\n", (100*len(jsonResponse))/len(compressedResponse))
	if strings.Contains(requestList.Request[commandNumber], "subscribe") == true {
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				fmt.Printf("Notification error: %s\n", err)
				return
			}
			jsonNotification := string(utils.DecompressMessage(msg))
			fmt.Printf("Notification: %s\n", jsonNotification)
			fmt.Printf("JSON notification size= %d, Compressed notification size=%d\n", len(jsonNotification), len(msg))
			fmt.Printf("Compression= %d%\n", (100*len(jsonNotification))/len(msg))
			select {
			case <-optionChannel:
				// issue unsubscribe request
				subscriptionId := utils.ExtractSubscriptionId(jsonResponse)
				unsubReq := `{"action":"unsubscribe", "subscriptionId":"` + subscriptionId + `"}`
				compressedUnsubReq := utils.CompressMessage([]byte(unsubReq))
				getResponse(conn, compressedUnsubReq)
				return
			default:
			}
		}
	}
}

func performPbCommand(commandNumber int, conn *websocket.Conn, optionChannel chan string) {
	compressedRequest := utils.JsonToProtobuf(requestList.Request[commandNumber], convertToCompression(compression))
	fmt.Printf("JSON request size= %d, Protobuf request size=%d\n", len(requestList.Request[commandNumber]), len(compressedRequest))
	fmt.Printf("Compression= %d%\n", (100*len(requestList.Request[commandNumber]))/len(compressedRequest))
	compressedResponse := getResponse(conn, compressedRequest)
	jsonResponse := utils.ProtobufToJson(compressedResponse, convertToCompression(compression))
	fmt.Printf("Response: %s\n", jsonResponse)
	fmt.Printf("JSON response size= %d, Protobuf response size=%d\n", len(jsonResponse), len(compressedResponse))
	fmt.Printf("Compression= %d%\n", (100*len(jsonResponse))/len(compressedResponse))
	if strings.Contains(requestList.Request[commandNumber], "subscribe") == true {
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				fmt.Printf("Notification error: %s\n", err)
				return
			}
			jsonNotification := utils.ProtobufToJson(msg, convertToCompression(compression))
			fmt.Printf("Notification: %s\n", jsonNotification)
			fmt.Printf("JSON notification size= %d, Protobuf notification size=%d\n", len(jsonNotification), len(msg))
			fmt.Printf("Compression= %d%\n", (100*len(jsonNotification))/len(msg))
			select {
			case <-optionChannel:
				// issue unsubscribe request
				subscriptionId := utils.ExtractSubscriptionId(jsonResponse)
				unsubReq := `{"action":"unsubscribe", "subscriptionId":"` + subscriptionId + `"}`
				pbUnsubReq := utils.JsonToProtobuf(unsubReq, convertToCompression(compression))
				getResponse(conn, pbUnsubReq)
				return
			default:
			}
		}
	}
}

func convertToCompression(compression string) utils.Compression {
	switch compression {
	case "prop":
		return utils.PROPRIETARY
	case "pbl1":
		return utils.PB_LEVEL1
	case "pbl2":
		return utils.PB_LEVEL2
	}
	return utils.NONE
}

func displayOptions() {
	fmt.Printf("\n\nSelect one of the following numbers:\n")
	fmt.Printf("0: Exit program\n")
	for i := 0; i < len(requestList.Request); i++ {
		fmt.Printf("%d: %s\n", i+1, requestList.Request[i])
	}
	fmt.Printf("In the case of an ongoing subscription session, to unsubscribe please input the character 'q'.\n")
	fmt.Printf("\nOption number selected: ")
}

func readOption(conn *websocket.Conn, optionChannel chan string, unsubChannel chan string) {
	for {
		fmt.Scanf("%s", &commandNumber)
		if commandNumber == "q" {
			unsubReq := <-unsubChannel
			fmt.Printf("Unsubscribe request: %s\n", unsubReq)
			getResponse(conn, []byte(unsubReq))

		}
		optionChannel <- commandNumber
	}
}

func main() {
	// Create new parser object
	parser := argparse.NewParser("print", "Prints provided string to stdout")

	// Create flags
	url_vissv2 := parser.String("v", "vissv2Url", &argparse.Options{Required: true, Help: "IP/url to VISSv2 server"})
	prot := parser.Selector("p", "protocol", []string{"http", "ws"}, &argparse.Options{Required: false,
		Help: "Protocol must be either http or websocket", Default: "ws"})
	comp := parser.Selector("c", "compression", []string{"none", "prop", "pbl1", "pbl2"}, &argparse.Options{Required: false,
		Help: "Compression must be either proprietary or protobuf level 1 or 2", Default: "none"})
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

	//conversion since parsed flags are of *string type and not string
	vissv2Url = *url_vissv2
	protocol = *prot
	compression = *comp

	utils.InitLog("pb_client-log.txt", "./logs", *logFile, *logLevel)

	readTransportSecConfig()
	utils.Info.Printf("secConfig.TransportSec=%s", secConfig.TransportSec)
	if secConfig.TransportSec == "yes" {
		caCertPool = *prepareTransportSecConfig()
	}

	if createListFromFile("requests.json") == 0 {
		fmt.Printf("Failed in creating list from requests.json")
		os.Exit(1)
	}

	if compression != "none" && utils.InitCompression("vsspathlist.json") != true {
		fmt.Printf("Failed in creating list from vsspathlist.json")
		os.Exit(1)
	}

	conn := initVissV2WebSocket(compression)

	unsubChannel := make(chan string, 1)
	optionChannel := make(chan string)
	go readOption(conn, optionChannel, unsubChannel)

	for {
		displayOptions()
		select {
		case commandNumber = <-optionChannel:
			if commandNumber == "0" {
				return
			}
		}
		cNo, err := strconv.Atoi(commandNumber)
		if err != nil {
			fmt.Printf("Selected option not supported\n")
			continue
		}
		performCommand(cNo-1, conn, optionChannel, unsubChannel)
	}
}
