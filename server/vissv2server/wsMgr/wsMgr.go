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
	"strconv"
	"sort"
)

// the number of channel array elements sets the limit for max number of parallel WS app clients
const NUMOFWSCLIENTS = 20
var wsClientChan []chan string
var clientBackendChan []chan string

var wsClientIndex int
const isClientLocal = false

/*
* responseHandling values, instructs server about possible path compression:
1: compress path, delete cache entry (get on single path)
2. do not compress path, delete cache entry (get on multiple paths) <- This shall not be saved in cache.
3. compress path, do not delete cache entry (subscribe, single path, but also for multiple paths after first time)
4. do not compress path, do not delete cache entry (subscribe on multiple paths) <- This is changed to 3 after first time
*/
type DataCompressionElement struct {
	PayloadId string
	Dc string
	ResponseHandling int  //possible values 1..4, see description
	SortedList []string
}
var dataCompressionCache []DataCompressionElement
const DCCACHESIZE = 20

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

func checkForCompression(reqMessage string) {
	if strings.Contains(reqMessage, `"dc"`) {
		dcValue, payloadId, singleResponse, singlePath := getDcConfig(reqMessage)
utils.Info.Printf("checkForCompression:dcValue=%s, payloadId=%s, singleResponse=%d, singlePath=%d", dcValue, payloadId, singleResponse, singlePath)
		if len(dcValue) > 0 {
			responseHandling := 1  // singleResponse && singlePath
			if singleResponse && !singlePath {
				responseHandling = 2
			} else if !singleResponse && singlePath {
				responseHandling = 3
			} else if !singleResponse && !singlePath {
				responseHandling = 4
			}
			dcCacheInsert(payloadId, dcValue, responseHandling)
		}
	}
}

func getDcConfig(reqMessage string) (string, string, bool, bool) {
	var dcValue, payloadId string
	isGet := false
	singlePath := false
	dcValue = getValueForKey(reqMessage, `"dc"`)
	isGet = strings.Contains(reqMessage, `"get"`)
	singlePath = !strings.Contains(reqMessage, `"paths"`)
	if isGet {
		payloadId = getValueForKey(reqMessage, `"requestId"`)
	} else {
		payloadId = getValueForKey(reqMessage, `"subscriptionId"`)
	}
	return dcValue, payloadId, isGet, singlePath
}

func getValueForKey(reqMessage string, key string) string {
	var keyValue string
	keyIndex := strings.Index(reqMessage, key) + len(key)
	hyphenIndex1 := strings.Index(reqMessage[keyIndex:], `"`)
	if hyphenIndex1 != -1 {
		hyphenIndex2 := strings.Index(reqMessage[keyIndex+hyphenIndex1+1:], `"`)
		if hyphenIndex2 != -1 {
			keyValue = reqMessage[keyIndex+hyphenIndex1+1:keyIndex+hyphenIndex1+1+hyphenIndex2]
		}
	}
	return keyValue
}

func initDcCache() {
	dataCompressionCache = make([]DataCompressionElement, DCCACHESIZE)
	for i := 0; i < DCCACHESIZE; i++ {
		dataCompressionCache[i].ResponseHandling = -1
	}
}

func dcCacheInsert(payloadId string, dcValue string, responseHandling int) {
	for i := 0; i < DCCACHESIZE; i++ {
		if dataCompressionCache[i].ResponseHandling == -1 {
			dataCompressionCache[i].ResponseHandling = responseHandling
			dataCompressionCache[i].PayloadId = payloadId
			dataCompressionCache[i].Dc = dcValue
		}
	}
}

func getDcCacheIndex(payloadId string) int {
	for i := 0; i < DCCACHESIZE; i++ {
		if dataCompressionCache[i].PayloadId == payloadId {
			return i
		}
	}
	return -1
}

func resetDcCache(cacheIndex int) {
	dataCompressionCache[cacheIndex].ResponseHandling = -1
	dataCompressionCache[cacheIndex].SortedList = nil
}

func checkForDecompression(respMessage string) string {
	var payloadId string
	isUnsubscribe := false
	if strings.Contains(respMessage, `"error"`) {
		return respMessage
	}
	switch getValueForKey(respMessage, `"action"`) {
		case "unsubscribe":
			isUnsubscribe = true
			fallthrough
		case "get":
			payloadId = getValueForKey(respMessage, `"requestId"`)
		case "subscription":
			payloadId = getValueForKey(respMessage, `"subscriptionId"`)
		default: return respMessage
	}
	cacheIndex := getDcCacheIndex(payloadId)
utils.Info.Printf("checkForDecompression:getValueForKey(respMessage, `action`)=%s, payloadId=%s, cacheIndex=%d", getValueForKey(respMessage, `"action"`), payloadId, cacheIndex)
	if cacheIndex == -1 {
		return respMessage
	}
	if isUnsubscribe {
		resetDcCache(cacheIndex)
		return respMessage
	}
	switch dataCompressionCache[cacheIndex].ResponseHandling {
		case 1:
			dataCompressionCache[cacheIndex].SortedList = getSortedPaths(respMessage)
			respMessage = compressPaths(respMessage, dataCompressionCache[cacheIndex].SortedList)
			resetDcCache(cacheIndex)
		case 2: return respMessage
		case 3:
			dataCompressionCache[cacheIndex].SortedList = getSortedPaths(respMessage)
			respMessage = compressPaths(respMessage, dataCompressionCache[cacheIndex].SortedList)
		case 4:
			dataCompressionCache[cacheIndex].ResponseHandling = 3
		default: return respMessage
	}
	return respMessage
}

func getSortedPaths(respMessage string) []string {
	respMap := make(map[string]interface{})
	if utils.MapRequest(respMessage, &respMap) != 0 {
		utils.Error.Printf("getSortedPaths():invalid JSON format=%s", respMessage)
		return nil
	}
	var paths []string
	switch data := respMap["data"].(type) {
		case interface{}:
			utils.Info.Println(data, "is interface{}")
			for k, v := range data.(map[string]interface{}) {
				if k == "path" {
					paths = append(paths, v.(string))
				}
			}
		case []interface{}:
			utils.Info.Println(data, "is []interface{}")
			for i := 0; i < len(data); i++ {
				for k, v := range data[i].(map[string]interface{}) {
					if k == "path" {
						paths = append(paths, v.(string))
					}
				}
			}
		default:
			utils.Info.Println(data, "is of an unknown type")
	}
	sort.Strings(paths)
	return paths
}

func compressPaths(respMessage string, sortedList []string) string {
	for i := 0; i < len(sortedList); i++ {
		respMessage = strings.Replace(respMessage, sortedList[i], strconv.Itoa(i), 1)
	}
	return respMessage
}

func WsMgrInit(mgrId int, transportMgrChan chan string) {
	var reqMessage string
	var clientId int
	utils.ReadTransportSecConfig()
	initChannels()
	initDcCache()
	go utils.WsServer{ClientBackendChannel: clientBackendChan}.InitClientServer(utils.MuxServer[1], wsClientChan, mgrId, &wsClientIndex)
	utils.Info.Println("WS manager data session initiated.")

	for {
		select {
		case respMessage := <-transportMgrChan:
			utils.Info.Printf("WS mgr hub: Response from server core:%s", respMessage)
			respMessage = checkForDecompression(respMessage)
			RemoveRoutingForwardResponse(respMessage, transportMgrChan)
			continue
		case reqMessage = <-wsClientChan[0]: clientId = 0
		case reqMessage = <-wsClientChan[1]: clientId = 1
		case reqMessage = <-wsClientChan[2]: clientId = 2
		case reqMessage = <-wsClientChan[3]: clientId = 3
		case reqMessage = <-wsClientChan[4]: clientId = 4
		case reqMessage = <-wsClientChan[5]: clientId = 5
		case reqMessage = <-wsClientChan[6]: clientId = 6
		case reqMessage = <-wsClientChan[7]: clientId = 7
		case reqMessage = <-wsClientChan[8]: clientId = 8
		case reqMessage = <-wsClientChan[9]: clientId = 9
		case reqMessage = <-wsClientChan[10]: clientId = 10
		case reqMessage = <-wsClientChan[11]: clientId = 11
		case reqMessage = <-wsClientChan[12]: clientId = 12
		case reqMessage = <-wsClientChan[13]: clientId = 13
		case reqMessage = <-wsClientChan[14]: clientId = 14
		case reqMessage = <-wsClientChan[15]: clientId = 15
		case reqMessage = <-wsClientChan[16]: clientId = 16
		case reqMessage = <-wsClientChan[17]: clientId = 17
		case reqMessage = <-wsClientChan[18]: clientId = 18
		case reqMessage = <-wsClientChan[19]: clientId = 19
		}
		checkForCompression(reqMessage)
		utils.AddRoutingForwardRequest(reqMessage, mgrId, clientId, transportMgrChan)
	}
}
