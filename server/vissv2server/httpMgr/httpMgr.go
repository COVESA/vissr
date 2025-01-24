/**
* (C) 2022 Geotab
* (C) 2019 Volvo Cars
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE file in this repository.
*
**/
package httpMgr

import (
	"github.com/covesa/vissr/utils"
)

var errorResponseMap = map[string]interface{}{}

// All HTTP app clients share same channel
var HttpClientChan = []chan string{
	make(chan string),
}

func RemoveRoutingForwardResponse(response string, transportMgrChan chan string) {
	trimmedResponse, clientId := utils.RemoveInternalData(response)
	HttpClientChan[clientId] <- trimmedResponse
}

func HttpMgrInit(mgrId int, transportMgrChan chan string) {
	utils.ReadTransportSecConfig()
	utils.JsonSchemaInit()

	go utils.HttpServer{}.InitClientServer(utils.MuxServer[0], HttpClientChan) // go routine needed due to listenAndServe call...
	utils.Info.Println("HTTP manager data session initiated.")

	utils.Info.Println("**** HTTP manager entering server loop... ****")
	for {
		select {
		case reqMessage := <-HttpClientChan[0]:
			utils.Info.Printf("HTTP mgr hub: Request from client:%s", reqMessage)
			validationError := utils.JsonSchemaValidate(reqMessage)
			if len(validationError) > 0 {
				var requestMap map[string]interface{}
				utils.MapRequest(reqMessage, &requestMap)
				utils.SetErrorResponse(requestMap, errorResponseMap, 0, validationError) //bad_request
				HttpClientChan[0] <- utils.FinalizeMessage(errorResponseMap)
				continue
			}
			utils.AddRoutingForwardRequest(reqMessage, mgrId, 0, transportMgrChan)
		case respMessage := <-transportMgrChan:
			utils.Info.Printf("HTTP mgr hub: Response from server core:%s", respMessage)
			RemoveRoutingForwardResponse(respMessage, transportMgrChan)
		}
	}
}
