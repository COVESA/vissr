/**
* (C) 2022 Geotab Inc
* (C) 2019 Volvo Cars
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE file in this repository.
*
**/
package wsMgr

import (
	utils "github.com/covesa/vissr/utils"
	"strings"
)

// the number of channel array elements sets the limit for max number of parallel WS app clients
const NUMOFWSCLIENTS = 20
var wsClientChan []chan string
var clientBackendChan []chan string

var wsClientIndex int
const isClientLocal = false

func initChannels() {
	wsClientChan = make([]chan string, NUMOFWSCLIENTS)
	clientBackendChan = make([]chan string, NUMOFWSCLIENTS)
	for i := 0; i < NUMOFWSCLIENTS; i++ {
	wsClientChan[i] = make(chan string)
	clientBackendChan[i] = make(chan string)
	}
}
func RemoveRoutingForwardResponse(response string, transportMgrChan chan string) {
	trimmedResponse, clientId := utils.RemoveInternalData(response)
	if strings.Contains(trimmedResponse, "\"subscription\"") {
		clientBackendChan[clientId] <- trimmedResponse //subscription notification
	} else {
		wsClientChan[clientId] <- trimmedResponse
	}
}

func WsMgrInit(mgrId int, transportMgrChan chan string) {
	utils.ReadTransportSecConfig()
	initChannels()

	go utils.WsServer{ClientBackendChannel: clientBackendChan}.InitClientServer(utils.MuxServer[1], wsClientChan, mgrId, &wsClientIndex) // go routine needed due to listenAndServe call...

	utils.Info.Println("WS manager data session initiated.")

	for {
		select {
		case respMessage := <-transportMgrChan:
			utils.Info.Printf("WS mgr hub: Response from server core:%s", respMessage)
			RemoveRoutingForwardResponse(respMessage, transportMgrChan)
		case reqMessage := <-wsClientChan[0]:
			utils.AddRoutingForwardRequest(reqMessage, mgrId, 0, transportMgrChan)
		case reqMessage := <-wsClientChan[1]:
			utils.AddRoutingForwardRequest(reqMessage, mgrId, 1, transportMgrChan)
		case reqMessage := <-wsClientChan[2]:
			utils.AddRoutingForwardRequest(reqMessage, mgrId, 2, transportMgrChan)
		case reqMessage := <-wsClientChan[3]:
			utils.AddRoutingForwardRequest(reqMessage, mgrId, 3, transportMgrChan)
		case reqMessage := <-wsClientChan[4]:
			utils.AddRoutingForwardRequest(reqMessage, mgrId, 4, transportMgrChan)
		case reqMessage := <-wsClientChan[5]:
			utils.AddRoutingForwardRequest(reqMessage, mgrId, 5, transportMgrChan)
		case reqMessage := <-wsClientChan[6]:
			utils.AddRoutingForwardRequest(reqMessage, mgrId, 6, transportMgrChan)
		case reqMessage := <-wsClientChan[7]:
			utils.AddRoutingForwardRequest(reqMessage, mgrId, 7, transportMgrChan)
		case reqMessage := <-wsClientChan[8]:
			utils.AddRoutingForwardRequest(reqMessage, mgrId, 8, transportMgrChan)
		case reqMessage := <-wsClientChan[9]:
			utils.AddRoutingForwardRequest(reqMessage, mgrId, 9, transportMgrChan)
		case reqMessage := <-wsClientChan[10]:
			utils.AddRoutingForwardRequest(reqMessage, mgrId, 10, transportMgrChan)
		case reqMessage := <-wsClientChan[11]:
			utils.AddRoutingForwardRequest(reqMessage, mgrId, 11, transportMgrChan)
		case reqMessage := <-wsClientChan[12]:
			utils.AddRoutingForwardRequest(reqMessage, mgrId, 12, transportMgrChan)
		case reqMessage := <-wsClientChan[13]:
			utils.AddRoutingForwardRequest(reqMessage, mgrId, 13, transportMgrChan)
		case reqMessage := <-wsClientChan[14]:
			utils.AddRoutingForwardRequest(reqMessage, mgrId, 14, transportMgrChan)
		case reqMessage := <-wsClientChan[15]:
			utils.AddRoutingForwardRequest(reqMessage, mgrId, 15, transportMgrChan)
		case reqMessage := <-wsClientChan[16]:
			utils.AddRoutingForwardRequest(reqMessage, mgrId, 16, transportMgrChan)
		case reqMessage := <-wsClientChan[17]:
			utils.AddRoutingForwardRequest(reqMessage, mgrId, 17, transportMgrChan)
		case reqMessage := <-wsClientChan[18]:
			utils.AddRoutingForwardRequest(reqMessage, mgrId, 18, transportMgrChan)
		case reqMessage := <-wsClientChan[19]:
			utils.AddRoutingForwardRequest(reqMessage, mgrId, 19, transportMgrChan)
		}
	}
}
