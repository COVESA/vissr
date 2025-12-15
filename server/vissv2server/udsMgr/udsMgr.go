/**
* (C) 2024 Ford Motor Company
* (C) 2022 Geotab Inc
* (C) 2019 Volvo Cars
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE file in this repository.
*
**/
package udsMgr

import (
	utils "github.com/covesa/vissr/utils"
	"net"
	"os"
	"strings"
	"strconv"
	"sort"
	"time"
)

const backendTermination = "internal-backend-termination"

// the number of channel array elements sets the limit for max number of parallel WS app clients
const NUMOFUDSCLIENTS = 20
var udsClientChan []chan string
var clientBackendChan []chan string
var UdsClientIndexList []bool
var udsClientIndex int
const isClientLocal = false

var errorResponseMap = map[string]interface{}{}
/*
* responseHandling values, instructs server about possible path compression:
1: compress path, delete cache entry (get on single path)
2. do not compress path, delete cache entry (get on multiple paths) <- This shall not be saved in cache.
3. compress path, do not delete cache entry (subscribe, single path, but also for multiple paths after first time)
4. do not compress path, do not delete cache entry (subscribe on multiple paths) <- This is changed to 3 after first time
*/
type CompressionType struct {
	Pc  uint8
	Tsc uint8
}

type DataCompressionElement struct {
	PayloadId string
	Dc CompressionType
	ResponseHandling int  //possible values 1..4, see description
	SortedList []string
}
var dataCompressionCache []DataCompressionElement
const DCCACHESIZE = 20

func initChannels() {
	udsClientChan = make([]chan string, NUMOFUDSCLIENTS)
	clientBackendChan = make([]chan string, NUMOFUDSCLIENTS)
	UdsClientIndexList = make([]bool, NUMOFUDSCLIENTS)
	for i := 0; i < NUMOFUDSCLIENTS; i++ {
		udsClientChan[i] = make(chan string)
		clientBackendChan[i] = make(chan string)
		UdsClientIndexList[i] = true
	}
}
func RemoveRoutingForwardResponse(response string, transportMgrChan chan string) {
	trimmedResponse, clientId := utils.RemoveInternalData(response)
	if strings.Contains(trimmedResponse, "\"subscription\"") {
		select {
		case clientBackendChan[clientId] <- trimmedResponse: //subscription notification
		default: 
			utils.Error.Printf("wsmgr:Event dropped")
		}
	} else {
		udsClientChan[clientId] <- trimmedResponse
	}
}

func checkCompressionRequest(reqMessage string) {
	if strings.Contains(reqMessage, `"dc"`) {
		dcValue, payloadId, singleResponse, singlePath := getDcConfig(reqMessage)
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
	payloadId = getValueForKey(reqMessage, `"requestId"`)
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
			if setDcValue(dcValue, i) {
				dataCompressionCache[i].ResponseHandling = responseHandling
				dataCompressionCache[i].PayloadId = payloadId
			}
			return
		}
	}
}

func setDcValue(dcValue string, cacheIndex int) bool {
	isCached := false
	plusIndex := strings.Index(dcValue, "+")
	if plusIndex != -1 {
		pc, err := strconv.Atoi(dcValue[:plusIndex])
		if err == nil && (pc == 2 || pc == 0) {  // only request local compression is supported
			dataCompressionCache[cacheIndex].Dc.Pc = uint8(pc)
			tsc, err := strconv.Atoi(dcValue[plusIndex+1:])
			if err == nil && (tsc == 1 || tsc == 0) {  // message local ts compression supported
				dataCompressionCache[cacheIndex].Dc.Tsc = uint8(tsc)
				isCached = true
			}
		}
	}
	return isCached
}

func updatepayloadId(payloadId1 string, payloadId2 string) {
	for i := 0; i < DCCACHESIZE; i++ {
		if dataCompressionCache[i].PayloadId == payloadId1 {
			dataCompressionCache[i].PayloadId = payloadId2
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

func checkCompressionResponse(respMessage string) string {
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
		case "subscribe":
			payloadId1 := getValueForKey(respMessage, `"requestId"`)
			payloadId2 := getValueForKey(respMessage, `"subscriptionId"`)
			updatepayloadId(payloadId1, payloadId2)

		case "subscription":
			payloadId = getValueForKey(respMessage, `"subscriptionId"`)
		default: return respMessage
	}
	cacheIndex := getDcCacheIndex(payloadId)
	if cacheIndex == -1 {
		return respMessage
	}
	if isUnsubscribe {
		resetDcCache(cacheIndex)
		return respMessage
	}
	switch dataCompressionCache[cacheIndex].ResponseHandling {
		case 1:
			if dataCompressionCache[cacheIndex].Dc.Pc == 2 {
				dataCompressionCache[cacheIndex].SortedList = getSortedPaths(respMessage)
				respMessage = compressPaths(respMessage, dataCompressionCache[cacheIndex].SortedList)
			}
			if dataCompressionCache[cacheIndex].Dc.Tsc == 1 {
				respMessage = compressTs(respMessage)
			}
			resetDcCache(cacheIndex)
		case 2: return respMessage
		case 3:
			if dataCompressionCache[cacheIndex].Dc.Pc == 2 {
				respMessage = compressPaths(respMessage, dataCompressionCache[cacheIndex].SortedList)
			}
			if dataCompressionCache[cacheIndex].Dc.Tsc == 1 {
				respMessage = compressTs(respMessage)
			}
		case 4:
			dataCompressionCache[cacheIndex].SortedList = getSortedPaths(respMessage)
			dataCompressionCache[cacheIndex].ResponseHandling = 3
			if dataCompressionCache[cacheIndex].Dc.Tsc == 1 {
				respMessage = compressTs(respMessage)
			}
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
	dataIf := respMap["data"]
	switch data := dataIf.(type) {
		case []interface{}:
//			utils.Info.Println(data, "is []interface{}")
			for i := 0; i < len(data); i++ {
				for k, v := range data[i].(map[string]interface{}) {
					if k == "path" {
						paths = append(paths, v.(string))
					}
				}
			}
		case interface{}:
//			utils.Info.Println(data, "is interface{}")
			for k, v := range data.(map[string]interface{}) {
				if k == "path" {
					paths = append(paths, v.(string))
				}
			}
		default:
			utils.Info.Println(data, "is of an unknown type")
	}
	sort.Strings(paths)
	return paths
}

func compressTs(respMessage string) string {
utils.Info.Printf("compressTs()")
	respMap := make(map[string]interface{})
	if utils.MapRequest(respMessage, &respMap) != 0 {
		utils.Error.Printf("compressTs():invalid JSON format=%s", respMessage)
		return respMessage
	}
	var tsList []string
	messageTs := respMap["ts"].(string)
	dataIf := respMap["data"]
	switch data := dataIf.(type) {
		case []interface{}:
//			utils.Info.Println(data, "is []interface{}")
			for i := 0; i < len(data); i++ {
				for k, v := range data[i].(map[string]interface{}) {
					if k == "dp" {
						tsList = append(tsList, getDpTsList(v)...)
					}
				}
			}
		case interface{}:
//			utils.Info.Println(data, "is interface{}")
			for k, v := range data.(map[string]interface{}) {
				if k == "dp" {
						tsList = getDpTsList(v)
				}
			}
		default:
			utils.Info.Println(data, "is of an unknown type")
	}
	respMessage = replaceTs(respMessage, messageTs, tsList)
	return respMessage
}

func getDpTsList(dpMap interface{}) []string {
	var tsList []string
	switch dp := dpMap.(type) {
		case []interface{}:
//			utils.Info.Println(dp, "is []interface{}")
			for i := 0; i < len(dp); i++ {
				for k, v := range dp[i].(map[string]interface{}) {
					if k == "ts" {
						tsList = append(tsList, v.(string))
					}
				}
			}
		case interface{}:
//			utils.Info.Println(dp, "is interface{}")
			for k, v := range dp.(map[string]interface{}) {
				if k == "ts" {
						tsList = append(tsList, v.(string))
				}
			}
		default:
			utils.Info.Println(dp, "is of an unknown type")
	}
	return tsList
}

func replaceTs(respMessage string, messageTs string, tsList []string) string {
	tsRef, _ := time.Parse(time.RFC3339, messageTs)
	refMs := tsRef.UnixMilli()
	preIndex := 0
	postIndex := len(respMessage)
	var respFraction string
	messageTsIndex := strings.Index(respMessage, messageTs)
	if strings.Count(respMessage[:messageTsIndex], "{") == 1 {
		preIndex = messageTsIndex
		respFraction = respMessage[:preIndex]
	} else {
		messageTsIndex = strings.LastIndex(respMessage, messageTs)
		postIndex = messageTsIndex
		respFraction = respMessage[postIndex:]
	}
	for i := 0; i < len(tsList); i++ {
		tsDp, _ := time.Parse(time.RFC3339, tsList[i])
		dpMs := tsDp.UnixMilli()
		diffMs := refMs - dpMs
		if diffMs > 999999999 || diffMs < -999999999 {
			continue  // keep iso time
		}
		signedTimeDiffStr := signedTimeDiff(strconv.Itoa(int(diffMs)), diffMs)
		if preIndex == 0 {
			respMessage = strings.Replace(respMessage[:postIndex], tsList[i], signedTimeDiffStr, 1) + respFraction
			postIndex -= len(tsList[i]) - len(signedTimeDiffStr)
		} else {
			respMessage = respFraction + strings.Replace(respMessage[preIndex:], tsList[i], signedTimeDiffStr, 1)
		}
	}
	return respMessage
}

func signedTimeDiff(diffMsStr string, diffMs int64) string {
	if diffMs > 0 {
		return "-" + diffMsStr
	} else if diffMs == 0 {
		return "+" + diffMsStr
	} else {
		return "+" + diffMsStr[1:]
	}
}

func compressPaths(respMessage string, sortedList []string) string {
	for i := 0; i < len(sortedList); i++ {
		respMessage = strings.Replace(respMessage, sortedList[i], strconv.Itoa(i), 1)
	}
	return respMessage
}

func initClientServer(mgrId int, clientIndex *int) {
	*clientIndex = 0
	os.Remove("/var/tmp/vissv2/udsMgr.sock")
	listener, err := net.Listen("unix", "/var/tmp/vissv2/udsMgr.sock")
	if err != nil {
		utils.Error.Printf("UdsMgrInit:UDS listen failed, err = %s", err)
		return
	}
	for {
		conn, err := listener.Accept()
		if err != nil {
			utils.Error.Printf("UdsMgrInit:UDS accept failed, err = %s", err)
			continue
		}
		*clientIndex = getUdsClientIndex()
		go udsReader(conn, udsClientChan[*clientIndex], clientBackendChan[*clientIndex], *clientIndex)
		go udsWriter(conn, clientBackendChan[*clientIndex])
	}
}


func getUdsClientIndex() int {
	freeIndex := -1
	for i := range UdsClientIndexList {
		if UdsClientIndexList[i] == true {
			UdsClientIndexList[i] = false
			freeIndex = i
			break
		}
	}
	return freeIndex
}

func returnUdsClientIndex(index int) {
	UdsClientIndexList[index] = true
}

func UdsMgrInit(mgrId int, transportMgrChan chan string) {
	var reqMessage string
	var clientId int
	utils.ReadTransportSecConfig()
	initChannels()
	initDcCache()
	utils.JsonSchemaInit()
	go initClientServer(mgrId, &udsClientIndex)
	utils.Info.Println("UDS manager data session initiated.")

	for {
		select {
		case respMessage := <-transportMgrChan:
			utils.Info.Printf("UDS mgr hub: Response from server core:%s", respMessage)
			respMessage = checkCompressionResponse(respMessage)
			RemoveRoutingForwardResponse(respMessage, transportMgrChan)
			continue
		case reqMessage = <-udsClientChan[0]: clientId = 0
		case reqMessage = <-udsClientChan[1]: clientId = 1
		case reqMessage = <-udsClientChan[2]: clientId = 2
		case reqMessage = <-udsClientChan[3]: clientId = 3
		case reqMessage = <-udsClientChan[4]: clientId = 4
		case reqMessage = <-udsClientChan[5]: clientId = 5
		case reqMessage = <-udsClientChan[6]: clientId = 6
		case reqMessage = <-udsClientChan[7]: clientId = 7
		case reqMessage = <-udsClientChan[8]: clientId = 8
		case reqMessage = <-udsClientChan[9]: clientId = 9
		case reqMessage = <-udsClientChan[10]: clientId = 10
		case reqMessage = <-udsClientChan[11]: clientId = 11
		case reqMessage = <-udsClientChan[12]: clientId = 12
		case reqMessage = <-udsClientChan[13]: clientId = 13
		case reqMessage = <-udsClientChan[14]: clientId = 14
		case reqMessage = <-udsClientChan[15]: clientId = 15
		case reqMessage = <-udsClientChan[16]: clientId = 16
		case reqMessage = <-udsClientChan[17]: clientId = 17
		case reqMessage = <-udsClientChan[18]: clientId = 18
		case reqMessage = <-udsClientChan[19]: clientId = 19
		}
		if !strings.Contains(reqMessage, `"internal-killsubscriptions"`) {
			validationError := utils.JsonSchemaValidate(reqMessage)
			if len(validationError) > 0 {
				requestMap := make(map[string]interface{})
				requestMap["action"] = utils.ExtractFromRequest(reqMessage, "action")
				requestMap["requestId"] = utils.ExtractFromRequest(reqMessage, "requestId")
				utils.SetErrorResponse(requestMap, errorResponseMap, 0, validationError) //bad_request
				udsClientChan[clientId] <- utils.FinalizeMessage(errorResponseMap)
				continue
			}
			checkCompressionRequest(reqMessage)
		}
		utils.AddRoutingForwardRequest(reqMessage, mgrId, clientId, transportMgrChan)
	}
}

func udsReader(conn net.Conn, clientChannel chan string, clientBackendChannel chan string, clientId int) {
	defer conn.Close()
	buf := make([]byte, 8192)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			utils.Error.Printf("udsReader:Read failed, err = %s", err)
			clientChannel <- `{"action":"internal-killsubscriptions"}`
			clientBackendChannel <- backendTermination
			returnUdsClientIndex(clientId)
			return
		}
		utils.Info.Printf("udsReader:Message from server: %s", string(buf[:n]))
		if n > 8192 {
			utils.Error.Printf("udsReader:Max message size of 8192 chars exceeded. Message dropped")
			continue
		}
		clientChannel <- string(buf[:n])    // forward to mgr hub,
		response := <-clientChannel //  and wait for response

		clientBackendChannel <- response // Forwards the response to the backendWSAppSession
	}
}

func udsWriter(conn net.Conn, clientBackendChannel chan string) {
	defer conn.Close()
	for {
		message := <-clientBackendChannel
		utils.Info.Printf("udsWriter: Message received=%s", message)
		if message == backendTermination {
			utils.Error.Print("udsWriter:App client websocket session error.")
			break
		}
		_, err := conn.Write([]byte(message))
		if err != nil {
			utils.Error.Print("udsWriter:App client write error:", err)
		}
	}
}
