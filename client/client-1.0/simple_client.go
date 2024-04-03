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
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"flag"
	//"io/ioutil"
	//	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"fmt"
	"time"

	"github.com/akamensky/argparse"
	"github.com/covesa/vissr/utils"
	"github.com/gorilla/websocket"
)

var commandNumber string

var clientCert tls.Certificate
var caCertPool x509.CertPool

var vissv2Url string
var protocol string

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
	var addr = flag.String("addr", vissv2Url+":"+portNum, "http service address")
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

func getResponse(conn *websocket.Conn, request []byte) (string, int) {
	start := time.Now()
	err := conn.WriteMessage(websocket.BinaryMessage, request)
	if err != nil {
		fmt.Printf("Request error:%s\n", err)
		return "", 0
	}
	_, msg, err := conn.ReadMessage()
	stop := time.Now()
	elapsed := stop.Sub(start)
	roundtripTime := int(elapsed / 1000) // nsec -> usec
	if err != nil {
		fmt.Printf("Response error: %s\n", err)
		return "", 0
	}
	return string(msg), roundtripTime
}

func performCommand(commandNumber int, conn *websocket.Conn, optionChannel chan string) int {
	if commandNumber > len(requestList.Request) {
		fmt.Printf("Termination due to invalid command.\n")
		os.Exit(1)
	}
	fmt.Printf("Request: %s\n", requestList.Request[commandNumber])
	request := []byte(requestList.Request[commandNumber])
	jsonResponse, roundtripTime := getResponse(conn, request)
	fmt.Printf("Response: %s\n", jsonResponse)
	if strings.Contains(requestList.Request[commandNumber], "subscribe") == true {
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				fmt.Printf("Notification error: %s\n", err)
				return 0
			}
			jsonNotification := string(msg)
			fmt.Printf("Notification: %s\n", jsonNotification)
			select {
			case <-optionChannel:
				// issue unsubscribe request
				subscriptionId := utils.ExtractSubscriptionId(jsonResponse)
				unsubReq := `{"action":"unsubscribe", "subscriptionId":"` + subscriptionId + `"}`
				_, roundtripTime = getResponse(conn, []byte(unsubReq))
				return roundtripTime
			default:
			}
		}
	}
	return roundtripTime
}

func displayOptions() {
	fmt.Printf("\n\nSelect one of the following numbers:\n")
	fmt.Printf("0: Exit program\n")
	for i := 0; i < len(requestList.Request); i++ {
		fmt.Printf("%d: %s\n", i+1, requestList.Request[i])
	}
	fmt.Printf("In the case of an ongoing subscription session, a RETURN key input will lead to unsubscribe.\n")
	fmt.Printf("\nOption number selected: ")
}

func readOption(optionChannel chan string) {
	for {
		fmt.Scanf("%s", &commandNumber)
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

	/*	if (utils.InitCompression("vsspathlist.json") != true) {
		fmt.Printf("Failed in creating list from vsspathlist.json")
		os.Exit(1)
	}*/

	conn := initVissV2WebSocket()

	optionChannel := make(chan string)
	go readOption(optionChannel)

	iterations := 0
	sumRT := 0
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
		roundtripTime := performCommand(cNo-1, conn, optionChannel)
		sumRT += roundtripTime
		iterations++
		fmt.Printf("Request-response roundtrip time=%d usec\n", roundtripTime)
		fmt.Printf("Average roundtrip time=%d usec, %d iterations\n", sumRT/iterations, iterations)
	}
}
