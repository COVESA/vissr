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

// handleHttpClientRequest processes a single inbound HTTP request from
// HttpClientChan. Validates against the JSON schema; on failure,
// returns the schema error back to the client via the same channel.
// On success, forwards the request to the manager hub via
// AddRoutingForwardRequest. Extracted from HttpMgrInit's for/select
// loop so the validation + forwarding behaviour can be unit-tested
// independently of the goroutine machinery — see
// httpMgr_dispatch_test.go.
func handleHttpClientRequest(reqMessage string, mgrId int, transportMgrChan chan string) {
	utils.Info.Printf("HTTP mgr hub: Request from client:%s", reqMessage)
	validationError := utils.JsonSchemaValidate(reqMessage)
	if len(validationError) > 0 {
		var requestMap map[string]interface{}
		utils.MapRequest(reqMessage, &requestMap)
		utils.SetErrorResponse(requestMap, errorResponseMap, 0, validationError) //bad_request
		HttpClientChan[0] <- utils.FinalizeMessage(errorResponseMap)
		return
	}
	utils.AddRoutingForwardRequest(reqMessage, mgrId, 0, transportMgrChan)
}

// handleHttpTransportResponse processes a single response from the
// server core. Extracted from HttpMgrInit so the response path can be
// unit-tested.
func handleHttpTransportResponse(respMessage string, transportMgrChan chan string) {
	utils.Info.Printf("HTTP mgr hub: Response from server core:%s", respMessage)
	RemoveRoutingForwardResponse(respMessage, transportMgrChan)
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
			handleHttpClientRequest(reqMessage, mgrId, transportMgrChan)
		case respMessage := <-transportMgrChan:
			handleHttpTransportResponse(respMessage, transportMgrChan)
		}
	}
}
