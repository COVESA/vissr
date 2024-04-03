/**
* (C) 2023 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE file in this repository.
*
**/

package main

import (
	"encoding/json"
	"fmt"
	"time"
	"strings"
	"net/http"
	"net/url"
	"flag"
	"github.com/gorilla/websocket"
)

var muxServer = []*http.ServeMux{
	http.NewServeMux(),
}

var Upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

var statusIndex int
var replyStatus [3]string = [3]string{`“status”: “200-OK”`, `“status”: “401-Bad request”`, `“status”: “404-Not found”`}
var postponeTicker *time.Ticker
var postponedRequest string
var cancelTicker *time.Ticker
var cancelRequest string

func initEcfComm(receiveChan chan string, sendChan chan string, muxServer *http.ServeMux) {
	scheme := "ws"
	portNum := "8445"
	var addr = flag.String("addr", "localhost:"+portNum, "http service address")
	dataSessionUrl := url.URL{Scheme: scheme, Host: *addr, Path: ""}
	dialer := websocket.Dialer{
		HandshakeTimeout: time.Second,
		ReadBufferSize:   1024,
		WriteBufferSize:  1024,
	}
	conn := reDialer(dialer, dataSessionUrl)
	if conn != nil {
		go ecfClient(conn, sendChan)
		go ecfReceiver(conn, receiveChan)
	}
}

func reDialer(dialer websocket.Dialer, sessionUrl url.URL) *websocket.Conn {
	for i := 0; i < 15; i++ {
		conn, _, err := dialer.Dial(sessionUrl.String(), nil)
		if err != nil {
			fmt.Printf("Data session dial error:%s\n", err)
			time.Sleep(2 * time.Second)
		} else {
			fmt.Printf("ECF dial success.\n")
			return conn
		}
	}
	fmt.Printf("ECF dial failure.\n")
	return nil
}

func ecfClient(conn *websocket.Conn, sendChan chan string) {
	defer conn.Close()
	for {
		ecfRequest := <-sendChan
		err := conn.WriteMessage(websocket.TextMessage, []byte(ecfRequest))
		if err != nil {
			fmt.Printf("ecfClient:Request write error:%s\n", err)
			return
		}
	}
}

func ecfReceiver(conn *websocket.Conn, ecfReceiveChan chan string) {
	defer conn.Close()
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			fmt.Printf("ecfReceiver read error: %s\n", err)
			break
		}
		message := string(msg)
		fmt.Printf("ECF message: %s\n", message)
		ecfReceiveChan <- message
	}
}

func dispatchResponse(request string, sendChan chan string) {
fmt.Printf("dispatchResponse: request=%s\n", request)
	var requestMap map[string]interface{}
	errorIndex := statusIndex
	err := json.Unmarshal([]byte(request), &requestMap)
	if err != nil {
		fmt.Printf("dispatchResponse:Request unmarshal error=%s", err)
		errorIndex = 1 //bad request
	}
	response := `{"action":"` + requestMap["action"].(string) + `", "status":"` + replyStatus[errorIndex] + `"}`
	sendChan <- response
}

func uiDialogue(request string) string {
	var actionNum string
	var newstatusIndex int
	fmt.Printf("\nCurrent response to all requests=%s\n", replyStatus[statusIndex])
	fmt.Printf("Change to 0:200-OK / 1:401-Bad request / 2:404-Not found / 3:Keep current response: ")
	fmt.Scanf("%d", &newstatusIndex)
	if newstatusIndex >= 0 && newstatusIndex <= 2 {
		statusIndex = newstatusIndex
	}
	fmt.Printf("\natServer request=%s\n", request)
	fmt.Printf("Select action: 0:Consent reply=YES / 1:Consent reply=NO / 2: Postpone consent reply: ")
	fmt.Scanf("%s", &actionNum)
	switch actionNum {
	case "0":
		if prepareCancelRequest(request) {
			var cancelSecs int
			fmt.Printf("Time to activate event to cancel request in seconds: ")
			fmt.Scanf("%d", &cancelSecs)
			cancelTicker.Reset(time.Duration(cancelSecs) * time.Second)
			cancelRequest = request
		}
		return createReply(request, true)
	case "1":
		return createReply(request, false)
	case "2":
		var postponSecs int
		fmt.Printf("Time to postpone in seconds: ")
		fmt.Scanf("%d", &postponSecs)
		postponeTicker.Reset(time.Duration(postponSecs) * time.Second)
		postponedRequest = request
		return ""
	default:
		fmt.Printf("Invalid action.")
		return ""
	}
	return ""
}

func prepareCancelRequest(request string) bool {
	var cancelDecision string
	fmt.Printf("Request= %s", request)
	fmt.Printf("Activate event for allowing cancelling of this request (yes/no): ")
	fmt.Scanf("%s", &cancelDecision)
	if cancelDecision == "yes" {
		return true
	}
	return false
}

func extractMessageId(request string) string {
	var requestMap map[string]interface{}
	err := json.Unmarshal([]byte(request), &requestMap)
	if err != nil {
		fmt.Printf("extractMessageId:Request unmarshal error=%s", err)
		return ""
	}
	if requestMap["messageId"] == nil {
		fmt.Printf("extractMessageId:Missing messageId key in request=%s", request)
		return ""
	}
	return requestMap["messageId"].(string)
}

func createReply(request string, consent bool) string {
	var requestMap map[string]interface{}
	yesNo := "NO"
	if consent {
		yesNo = "YES"
	}
	err := json.Unmarshal([]byte(request), &requestMap)
	if err != nil {
		fmt.Printf("createReply:Request unmarshal error=%s", err)
		return ""
	} else {
		return `{"action":"consent-reply", "consent":"` + yesNo + `", "messageId":"` + requestMap["messageId"].(string) + `"}`
	}
}

func main() {
	receiveChan := make(chan string)
	sendChan := make(chan string)
	statusIndex = 0
	postponeTicker = time.NewTicker(24 * time.Hour)
	cancelTicker = time.NewTicker(24 * time.Hour)

	go initEcfComm(receiveChan, sendChan, muxServer[0])
	fmt.Printf("ECF simulator started. Waiting for request from Access Token server...")

	for {
		select {
		case message := <-receiveChan:
			fmt.Printf("Message received=%s\n", message)
			if !strings.Contains(message, "status\":") {
				dispatchResponse(message, sendChan)
				reply := uiDialogue(message)
				if reply != "" {
					fmt.Printf("Reply to atServer=%s\n", reply)
					sendChan <- reply
				}
			}
		case <-postponeTicker.C:
			fmt.Printf("postpone ticker triggered")
			reply := uiDialogue(postponedRequest)
			if reply != "" {
				fmt.Printf("Postponed reply to atServer=%s\n", reply)
				sendChan <- reply
			}
		case <-cancelTicker.C:
			fmt.Printf("Cancel ticker triggered")
			messageId := extractMessageId(cancelRequest)
			if messageId != "" {
				request := `{"action":"consent-cancel", "messageId":"` + messageId + `"}`
				fmt.Printf("Cancel request to atServer=%s\n", request)
				sendChan <- request
			}
		}
	}
}
