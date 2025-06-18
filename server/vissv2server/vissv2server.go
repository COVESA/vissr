/**
* (C) 2023 Ford Motor Company
* (C) 2022 Geotab Inc
* (C) 2020 Mitsubishi Electrics Automotive
* (C) 2019 Geotab Inc
* (C) 2019,2023 Volvo Cars
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE file in this repository.
*
**/

package main

import (
	"fmt"
	"os"
//	"bufio"

	"github.com/akamensky/argparse"
//	"github.com/gorilla/mux"

	"encoding/hex"
	"encoding/json"
	"crypto/sha1"
	"io"
//	"net/http"
//	"sort"
	"strconv"
	"strings"
//	"time"

	"github.com/covesa/vissr/server/vissv2server/atServer"
	"github.com/covesa/vissr/server/vissv2server/grpcMgr"
	"github.com/covesa/vissr/server/vissv2server/httpMgr"
	"github.com/covesa/vissr/server/vissv2server/mqttMgr"
	"github.com/covesa/vissr/server/vissv2server/serviceMgr"
	"github.com/covesa/vissr/server/vissv2server/wsMgr"
	"github.com/covesa/vissr/server/vissv2server/wsMgrFT"

	"github.com/covesa/vissr/utils"
)

// set to MAXFOUNDNODES in cparserlib.h
const MAXFOUNDNODES = 1500

// the server components started as threads by vissv2server. If a component is commented out, it will not be started
var serverComponents []string = []string{
	"serviceMgr",
	"httpMgr",
	"wsMgr",
	"wsMgrFT",
	"mqttMgr",
	"grpcMgr",
	"atServer",
}

/*
 * For communication between transport manager threads and vissv2server thread.
 * If support for new transport protocol is added, add element to channel
 */
 const NUMOFTRANSPORTMGRS = 4  // order assigned to channels: HTTP, WS, MQTT, gRPC
var transportMgrChannel []chan string
var transportDataChan []chan map[string]interface{}
//var transportDataChan []chan string
var backendChan []chan map[string]interface{}

var serviceMgrChannel []chan map[string]interface{}

var atsChannel []chan string

var serviceDataPortNum int = 8200 // port number interval [8200-]

// add element if support for new service manager is added
var serviceDataChan []chan map[string]interface{}

var ftChannel chan utils.FileTransferCache

var errorResponseMap = map[string]interface{}{}

func extractMgrId(routerId string) int { // "RouterId" : "mgrId?clientId"
	delim := strings.Index(routerId, "?")
	mgrId, _ := strconv.Atoi(routerId[:delim])
	return mgrId
}

func serviceDataSession(serviceMgrChannel chan map[string]interface{}, serviceDataChannel chan map[string]interface{}, backendChannel []chan map[string]interface{}) {
	for {
		select {

		case response := <-serviceMgrChannel:
//			utils.Info.Printf("Server core: Response from service mgr:%s", response)
			mgrIndex := extractMgrId(response["RouterId"].(string))
//			utils.Info.Printf("mgrIndex=%d", mgrIndex)
			backendChannel[mgrIndex] <- response
		case request := <-serviceDataChannel:
//			utils.Info.Printf("Server core: Request to service:%s", request)
			serviceMgrChannel <- request
		}
	}
}

func transportDataSession(transportMgrChannel chan string, transportDataChannel chan map[string]interface{}, backendChannel chan map[string]interface{}) {
	for {
		select {

		case msg := <-transportMgrChannel:
//			utils.Info.Printf("request: %s", msg)
			var msgMap map[string]interface{}
			utils.MapRequest(msg, &msgMap)
			transportDataChannel <- msgMap // send request to server hub
		case message := <-backendChannel:
			select {
				case transportMgrChannel <- utils.FinalizeMessage(message):
				default: 
					utils.Error.Printf("server hub: Event dropped")
			}
		}
	}
}

func searchTree(rootNode *utils.Node_t, path string, anyDepth bool, leafNodesOnly bool, listSize int, noScopeList []string, validation *int) (int, []utils.SearchData_t) {
//	utils.Info.Printf("searchTree(): path=%s, anyDepth=%t, leafNodesOnly=%t", path, anyDepth, leafNodesOnly)
	if len(path) > 0 {
		var searchData []utils.SearchData_t
		var matches int
		searchData, matches = utils.VSSsearchNodes(path, rootNode, MAXFOUNDNODES, anyDepth, leafNodesOnly, listSize, noScopeList, validation)
		return matches, searchData
	}
	return 0, nil
}

func getPathLen(path string) int {
	for i := 0; i < len(path); i++ {
		if path[i] == 0x00 { // the path buffer defined in searchData_t is initiated with all zeros
			return i
		}
	}
	return len(path)
}

func getTokenErrorMessage(index int) string {
	switch index {
	case 1:
		return "Invalid Access Token. "
	case 2:
		return "Access Token not found. "
	case 5:
		return "Invalid Access Token Signature. "
	case 6:
		return "Invalid Access Token Signature Algorithm. "
	case 10:
		return "Invalid iat claim. Invalid time format. "
	case 11:
		return "Invalid iat claim. Future time. "
	case 15:
		return "Invalid exp claim. Invalid time format. "
	case 16:
		return "Invalid exp claim. Token Expired. "
	case 20:
		return "Invalid AUD. "
	case 21:
		return "Invalid Context. "
	case 30:
		return "Invalid Token: token revoked. "
	case 40, 41, 42:
		return "Internal error. "
	case 60:
		return "Permission denied. Purpose does not match signals requested. "
	case 61:
		return "Permission denied. Read only access mode trying to write. "
	}
	return "Unknown error. "
}

func setTokenErrorResponse(reqMap map[string]interface{}, errorCode int) {
	utils.SetErrorResponse(reqMap, errorResponseMap, 3, getTokenErrorMessage(errorCode))
}

// Sends a message to the Access Token Server to validate the Access Token paths and permissions
func verifyToken(token string, action string, paths string, validation int) (int, string, string) {
	handle := ""
	gatingId := ""
	request := `{"token":"` + token + `","paths":` + paths + `,"action":"` + action + `","validation":"` + strconv.Itoa(validation) + `"}`
	atsChannel[0] <- request
	body := <-atsChannel[0]
	var bdy map[string]interface{}
	var err error
	if err = json.Unmarshal([]byte(body), &bdy); err != nil {
		utils.Error.Print("verifyToken: Error unmarshalling ats response. ", err)
		return 41, handle, gatingId
	}
	if bdy["validation"] == nil {
		utils.Error.Print("verifyToken: Error reading validation claim. ")
		return 42, handle, gatingId
	}

	// Converts the validation claim to int
	var atsValidation int
	if atsValidation, err = strconv.Atoi(bdy["validation"].(string)); err != nil {
		utils.Error.Print("verifyToken: Error converting validation claim to int. ", err)
		return 42, handle, gatingId
	} else if atsValidation == 0 {
		if bdy["handle"] != nil {
			handle = bdy["handle"].(string)
		}
		if bdy["gatingId"] != nil {
			gatingId = bdy["gatingId"].(string)
		}
	}
	return atsValidation, handle, gatingId
}

// nativeCnodeDef.h: nodeTypes_t;
func nodeTypesToString(nodeType int) string {
	switch nodeType {
	case 1:
		return "sensor"
	case 2:
		return "actuator"
	case 3:
		return "attribute"
	case 4:
		return "branch"
	default:
		return ""
	}
}

// nativeCnodeDef.h: nodeDatatypes_t
func nodeDataTypesToString(nodeType int) string {
	switch nodeType {
	case 1:
		return "int8"
	case 2:
		return "uint8"
	case 3:
		return "int16"
	case 4:
		return "uint16"
	case 5:
		return "int32"
	case 6:
		return "uint32"
	case 7:
		return "double"
	case 8:
		return "float"
	case 9:
		return "boolean"
	case 10:
		return "string"
	default:
		return ""
	}
}

func jsonifyTreeNode(nodeHandle *utils.Node_t, jsonBuffer string, depth int, maxDepth int) string {
	if depth >= maxDepth {
		return jsonBuffer
	}
	depth++
	var newJsonBuffer string
	nodeName := utils.VSSgetName(nodeHandle)
	newJsonBuffer += `"` + nodeName + `":{`
	nodeType := int(utils.VSSgetType(nodeHandle))
	newJsonBuffer += `"type":"` + nodeTypesToString(nodeType) + `",`
	nodeDefault := utils.VSSgetDefault(nodeHandle)
	if len(nodeDefault) > 0 {
		if nodeDefault[0] == '{' || nodeDefault[0] == '[' {
			newJsonBuffer += `"default":` + singleToDoubleQuote(nodeDefault) + `,`
		} else {
			newJsonBuffer += `"default":"`+ nodeDefault + `",`
		}
	}
	nodeDescr := utils.VSSgetDescr(nodeHandle)
	newJsonBuffer += `"description":"` + nodeDescr + `",`
	nodeNumofChildren := utils.VSSgetNumOfChildren(nodeHandle)
	switch nodeType {
	case 4: // branch
	case 1: // sensor
		fallthrough
	case 2: // actuator
		fallthrough
	case 3: // attribute
		// TODO Look for other metadata, unit, enum, ...
		nodeDatatype := utils.VSSgetDatatype(nodeHandle)
		newJsonBuffer += `"datatype":"` + nodeDatatype + `",`
	default:
		return ""

	}
	if depth < maxDepth {
		if nodeNumofChildren > 0 {
			newJsonBuffer += `"children":{`
		}
		for i := 0; i < nodeNumofChildren; i++ {
			childNode := utils.VSSgetChild(nodeHandle, i)
			newJsonBuffer += jsonifyTreeNode(childNode, jsonBuffer, depth, maxDepth)
		}
		if nodeNumofChildren > 0 {
			newJsonBuffer = newJsonBuffer[:len(newJsonBuffer)-1] // remove comma after curly bracket
			newJsonBuffer += "}"
		}
	}
	if newJsonBuffer[len(newJsonBuffer)-1] == ',' && newJsonBuffer[len(newJsonBuffer)-2] != '}' {
		newJsonBuffer = newJsonBuffer[:len(newJsonBuffer)-1]
	}
	newJsonBuffer += "},"
	return jsonBuffer + newJsonBuffer
}

func singleToDoubleQuote(jsonObject string) string {
	for i := 0; i < len(jsonObject); i++ {
		if jsonObject[i] == '\'' {
			jsonObject = jsonObject[:i] + `"` + jsonObject[i+1:]
		}
	}
	return jsonObject
}

func countPathSegments(path string) int {
	return strings.Count(path, ".") + 1
}

func getNoScopeList(tokenContext string) ([]string, int) {
	// call ATS to get noscope list
	request := `{"context":"` + tokenContext + `"}`
	atsChannel[0] <- request
	body := <-atsChannel[0]
	var noScopeMap map[string]interface{}
	err := json.Unmarshal([]byte(body), &noScopeMap)
	if err != nil {
		utils.Error.Printf("initPurposelist:error body=%s, err=%s", body, err)
		return nil, 0
	}
	return extractNoScopeElementsLevel1(noScopeMap)
}

func extractNoScopeElementsLevel1(noScopeMap map[string]interface{}) ([]string, int) {
	for k, v := range noScopeMap {
		switch vv := v.(type) {
		case string:
//			utils.Info.Println(k, "is string", vv)
			noScopeList := make([]string, 1)
			noScopeList[0] = vv
			return noScopeList, 1
		case []interface{}:
//			utils.Info.Println(k, "is an array:, len=", strconv.Itoa(len(vv)))
			return extractNoScopeElementsLevel2(vv)
		default:
			utils.Info.Println(k, "is of an unknown type")
		}
	}
	return nil, 0
}

func extractNoScopeElementsLevel2(noScopeMap []interface{}) ([]string, int) {
	noScopeList := make([]string, len(noScopeMap))
	i := 0
	for k, v := range noScopeMap {
		switch vv := v.(type) {
		case string:
//			utils.Info.Println(k, "is string", vv)
			noScopeList[i] = vv
		default:
			utils.Info.Println(k, "is of an unknown type")
		}
		i++
	}
	return noScopeList, i
}

// Gets node and childs data as string from VSS tree
func synthesizeJsonTree(path string, depth int, tokenContext string, VSSTreeRoot *utils.Node_t) string {
	var jsonBuffer string
	var searchData []utils.SearchData_t
	var matches int
	noScopeList, numOfListElem := getNoScopeList(tokenContext)
//	utils.Info.Printf("noScopeList[0]=%s", noScopeList[0])
	matches, searchData = searchTree(VSSTreeRoot, path+".*", true, false, numOfListElem, noScopeList, nil)
	subTreeRoot := getSubTreeNodeHandle(path, searchData, matches)
	if subTreeRoot == nil {
		return ""
	}
//	utils.Info.Printf("synthesizeJsonTree:subTreeRoot-name=%s", utils.VSSgetName(subTreeRoot))
	if depth == 0 {
		depth = 100
	}
	jsonBuffer = jsonifyTreeNode(subTreeRoot, jsonBuffer, 0, depth)
	if len(jsonBuffer) > 0 {
		return "{" + jsonBuffer[:len(jsonBuffer)-1] + "}" // remove comma
	}
	return ""
}

func getSubTreeNodeHandle(path string, searchData []utils.SearchData_t, matches int) *utils.Node_t {
	for i := 0; i < matches; i++ {
		if searchData[i].NodePath == path {
			return searchData[i].NodeHandle
		}
	}
	return nil
}

func getTokenContext(reqMap map[string]interface{}) string {
	if reqMap["authorization"] != nil {
		return utils.ExtractFromToken(reqMap["authorization"].(string), "clx")
	}
	return ""
}

func serveRequest(requestMap map[string]interface{}, tDChanIndex int, sDChanIndex int) {
	if requestMap["path"] != nil {
		requestMap["path"] = utils.UrlToPath(requestMap["path"].(string)) // replace slash with dot
	}
	if requestMap["action"] == "unsubscribe" {
		serviceDataChan[sDChanIndex] <- requestMap
		return
	}
	issueServiceRequest(requestMap, tDChanIndex, sDChanIndex)
}

func issueServiceRequest(requestMap map[string]interface{}, tDChanIndex int, sDChanIndex int) {
	if requestMap["action"] == "internal-killsubscriptions" || requestMap["action"] == "internal-cancelsubscription" {
		serviceDataChan[sDChanIndex] <- requestMap // internal message
		return
	}
	rootPath := requestMap["path"].(string)
	VSSTreeRoot := utils.SetRootNodePointer(rootPath)
	if VSSTreeRoot == nil {
		utils.SetErrorResponse(requestMap, errorResponseMap, 0, "") //bad_request
		backendChan[tDChanIndex] <- errorResponseMap
		return
	}
	var searchPath []string
	var filterList []utils.FilterObject // variant + parameter

	// Manages Filter Request
	if requestMap["filter"] != nil {
		utils.UnpackFilter(requestMap["filter"], &filterList)
		// Iterates all the filters
		for i := 0; i < len(filterList); i++ {
//			utils.Info.Printf("filterList[%d].Type=%s, filterList[%d].Parameter=%s", i, filterList[i].Type, i, filterList[i].Parameter)
			// PATH FILTER
			if filterList[i].Type == "paths" {
				if strings.Contains(filterList[i].Parameter, "[") { // Various paths to search
					err := json.Unmarshal([]byte(filterList[i].Parameter), &searchPath) // Writes in search path all values in filter
					if err != nil {
						utils.Error.Printf("Unmarshal filter path array failed.")
						utils.SetErrorResponse(requestMap, errorResponseMap, 0, "") //bad_request
						backendChan[tDChanIndex] <- errorResponseMap
						return
					}
					for i := 0; i < len(searchPath); i++ {
						searchPath[i] = rootPath + "." + utils.UrlToPath(searchPath[i]) // replaces slash with dot
					}
				} else { // Single path to search
					searchPath = make([]string, 1)
					searchPath[0] = rootPath + "." + utils.UrlToPath(filterList[i].Parameter) // replaces slash with dot
				}
				break // only one paths object is allowed
			}

			// METADATA FILTER
			if filterList[i].Type == "metadata" {
				tokenContext := getTokenContext(requestMap) // Gets the client context from the token in the request
				if len(tokenContext) == 0 {
					tokenContext = "Undefined+Undefined+Undefined"
				}
//utils.Info.Printf("filterList[%d].Type=%s, filterList[%d].Parameter=%s", i, filterList[i].Type, i, filterList[i].Parameter)
				treedepth, err := strconv.Atoi(filterList[i].Parameter)
				if err == nil {
				metadata := ""
				metadata = synthesizeJsonTree(requestMap["path"].(string), treedepth, tokenContext, VSSTreeRoot)
				if len(metadata) > 0 {
					delete(requestMap, "path")
					delete(requestMap, "filter")
					requestMap["ts"] = utils.GetRfcTime()
					requestMap["metadata"] = metadata
					backendChan[tDChanIndex] <- requestMap
					return
				}
				}
				utils.Error.Printf("Metadata error")
				utils.SetErrorResponse(requestMap, errorResponseMap, 0, "") //bad_request
				backendChan[tDChanIndex] <- errorResponseMap
				return
			}
		}
	}
	if requestMap["filter"] == nil || len(searchPath) == 0 {
		searchPath = make([]string, 1)
		searchPath[0] = rootPath
	}
	var searchData []utils.SearchData_t
	var matches int
	totalMatches := 0
	paths := ""
	maxValidation := -1
	for i := 0; i < len(searchPath); i++ {
		anyDepth := true
		validation := -1
		matches, searchData = searchTree(VSSTreeRoot, searchPath[i], anyDepth, true, 0, nil, &validation)
		utils.Info.Printf("Matches=%d. Max validation from search=%d", matches, int(validation))
		for i := 0; i < matches; i++ {
			pathLen := getPathLen(string(searchData[i].NodePath[:]))
			paths += "\"" + string(searchData[i].NodePath[:pathLen]) + "\", "
		}
		if matches == 0 {
			utils.SetErrorResponse(requestMap, errorResponseMap, 6, "") //unavailable_data
			backendChan[tDChanIndex] <- errorResponseMap
			return
		}
		totalMatches += matches
		maxValidation = utils.GetMaxValidation(int(validation), maxValidation)
	}
	if strings.Contains (utils.VSSgetDatatype(searchData[0].NodeHandle), ".FileDescriptor") {  // path to struct def
		response := initiateFileTransfer(requestMap, utils.VSSgetType(searchData[0].NodeHandle), searchPath[0])
		backendChan[tDChanIndex] <- response
		return
	}
	errorIndex, errorDescription := validateData(requestMap, searchData, filterList)
	if errorIndex != -1 {
		utils.SetErrorResponse(requestMap, errorResponseMap, errorIndex, errorDescription)
		backendChan[tDChanIndex] <- errorResponseMap
		return
	}
	paths = paths[:len(paths)-2]
	if totalMatches > 1 {
		paths = "[" + paths + "]"
	}

	if requestMap["origin"] == "internal" { // internal message, no validation needed
		maxValidation = 0
	}

	tokenHandle := ""
	gatingId := ""
	switch maxValidation % 10 {
	case 0: // validation not required
	case 1:
		fallthrough
	case 2:
		errorCode := 0
		if requestMap["authorization"] == nil {
			errorCode = 2
		} else {
			if requestMap["action"] == "set" || maxValidation%10 == 2 { // no validation for get/subscribe when validation is 1 (write-only)
				// checks if requestmap authorization is a string
				if authToken, ok := requestMap["authorization"].(string); !ok {
					errorCode = 1
				} else {
					errorCode, tokenHandle, gatingId = verifyToken(authToken, requestMap["action"].(string), paths, maxValidation)
				}
			}
		}
		if errorCode != 0 {
			setTokenErrorResponse(requestMap, errorCode)
			backendChan[tDChanIndex] <- errorResponseMap
			return
		}
	default: // should not be possible...
		utils.SetErrorResponse(requestMap, errorResponseMap, 7, "") //service_unavailable
		backendChan[tDChanIndex] <- errorResponseMap
		return
	}
	if totalMatches == 1 {
		paths = paths[1 : len(paths)-1] // remove hyphens
	}
	requestMap["path"] = paths
	if tokenHandle != "" {
		requestMap["handle"] = tokenHandle
	}
	if gatingId != "" {
		requestMap["gatingId"] = gatingId
	}
	serviceDataChan[sDChanIndex] <- requestMap
}

func validateData(requestMap map[string]interface{}, searchData []utils.SearchData_t, filterList []utils.FilterObject) (int, string) { // -1, "" means valid data
	if requestMap["action"] == "set" && utils.VSSgetType(searchData[0].NodeHandle) != utils.ACTUATOR {
		return 1, "Forbidden to write to read-only resource"  //invalid_data
	}
	if requestMap["filter"] != nil {
		for i := 0; i < len(filterList); i++ {
			var paramMap map[string]interface{}
			if filterList[i].Type == "range" { //parameter:[{"logic-op":"a", "boundary": "x", "combination-op":"b"},{"logic-op":"c", "boundary": "y"}]
				utils.MapRequest(filterList[i].Parameter, &paramMap)
				bVal1, bVal2 := getRangeBoundaries(paramMap)
				if !utils.IsNumber(bVal1) || (len(bVal2) != 0 && !utils.IsNumber(bVal2)) {  // number ok, one or two boundaries
					return 1, "Invalid range boundary datatype"
				}
			} else if filterList[i].Type == "change" { //parameter:{"logic-op":"a", "diff": "x"}
				utils.MapRequest(filterList[i].Parameter, &paramMap)
				if !utils.IsNumber(paramMap["diff"].(string)) && !utils.IsBoolean(paramMap["diff"].(string)) { // number or boolean ok
					return 1, "Invalid change diff datatype"
				}
			} else if filterList[i].Type == "curvelog" { //parameter:{"maxerr":"a", "bufsize": "x"}
				utils.MapRequest(filterList[i].Parameter, &paramMap)
				if !utils.IsNumber(paramMap["maxerr"].(string)) {  // number ok
					return 1, "Invalid curve log maxerr datatype"
				}
			}
		}
	}
	return -1, ""
}

func getRangeBoundaries(paramMap interface{}) (string, string) {
	var bVal []string = []string{"", ""}
	switch pMap := paramMap.(type) {
		case interface{}:
			bVal[0] = getRangeBoundary(pMap.(map[string]interface{}))
		case []interface{}:
			for i := 0; i < len(pMap); i++ {
				if i > 1 {
					utils.Error.Printf("Range array size too big. Len=%d", len(pMap))
					break
				}
				bVal[i] = getRangeBoundary(pMap[i].(map[string]interface{}))
			}
		default:
			utils.Info.Println(pMap, "is of an unknown type")
	}
//utils.Info.Printf("bVal1=%s, bVal2=%s", bVal[0], bVal[1])
	return bVal[0], bVal[1]
}

func getRangeBoundary(pMap map[string]interface{}) string {
	bVal := ""
	for k, v := range pMap {
		switch vv := v.(type) {
		case string:
//			utils.Info.Println(k, "is string", vv)
			if k == "boundary" {
				bVal = vv
			}
		default:
			utils.Info.Println(k, "is of an unknown type")
		}
	}
	return bVal
}

func initiateFileTransfer(requestMap map[string]interface{}, nodeType utils.NodeTypes_t, path string) map[string]interface{} {
//utils.Info.Printf("initiateFileTransfer: requestMap[action]=%s, nodeType=%d", requestMap["action"], nodeType)
	var ftInitData utils.FileTransferCache
	var responseMap = map[string]interface{}{}
	if requestMap["action"] == "set" && nodeType == utils.ACTUATOR { // download
		var uidString string
		ftInitData.UploadTransfer = false
		ftInitData.Name, ftInitData.Hash, uidString = getFileDescriptorData(requestMap["value"].(interface{}))
		uidByte, _ := hex.DecodeString(uidString)
		ftInitData.Uid = [utils.UIDLEN]byte(uidByte)
		ftInitData.Path = "./"  //get it from statestorage when vss-tools have updated.
		ftChannel <- ftInitData
		ftInitData = <- ftChannel
		if ftInitData.Status == 0 {
			responseMap["RouterId"] = requestMap["RouterId"].(string)
			responseMap["action"] = "set"
			responseMap["ts"] = utils.GetRfcTime()
			return responseMap
		} else {
			utils.SetErrorResponse(requestMap, errorResponseMap, 7, "") //service_unavailable
			return errorResponseMap
		}
		
	} else if requestMap["action"] == "get" && nodeType == utils.SENSOR { //upload
		if requestMap["path"] != nil {
			path := requestMap["path"].(string)
			ftInitData.UploadTransfer = true
			ftInitData.Path, ftInitData.Name = getInternalFileName(path)
			ftInitData.Hash = calculateHash(ftInitData.Path + ftInitData.Name)
			uidByte, _ := hex.DecodeString("2d878213")  //TODO: random generation
			ftInitData.Uid = [utils.UIDLEN]byte(uidByte)
			ftChannel <- ftInitData
			_ = <- ftChannel
			responseMap["RouterId"] = requestMap["RouterId"].(string)
			responseMap["action"] = "get"
			responseMap["path"] = path
			responseMap["value"] = `{"name": "` + ftInitData.Name + `", "hash":"` + ftInitData.Hash + `","uid":"` + hex.EncodeToString(ftInitData.Uid[:]) + `"}`
			responseMap["ts"] = utils.GetRfcTime()
			return responseMap
/*			return `{"RouterId": "` + requestMap["RouterId"].(string) + `"action": "get", "path":"` + path +
				`", "value":{"name": "` + ftInitData.Name + `", "hash":"` + ftInitData.Hash + `","uid":"` + hex.EncodeToString(ftInitData.Uid[:]) + `"}, ` +
				`"ts": "` + utils.GetRfcTime() + `"}`*/
		}
	}
	utils.SetErrorResponse(requestMap, errorResponseMap, 1, "") //invalid_data
	return errorResponseMap
}

func calculateHash(fileName string) string {
	f, err := os.Open(fileName)
	if err != nil {
		utils.Error.Printf("calculateHash: failed to open %s, err=%s", fileName, err)
		return ""
	}
	defer f.Close()

	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		utils.Info.Printf("calculateHash: failed to read %s, err=%s", fileName, err)
	}
//	utils.Info.Printf("SHA-1 hash=%x", h.Sum(nil))
	return hex.EncodeToString(h.Sum(nil))
}

func getInternalFileName(path string) (string, string) {  // maps between tree paths and files in the vehicle file system
	switch path {
		case "Vehicle.UploadFile": return "", "upload.txt"
	}
	return "", "upload.txt"
}

func getFileDescriptorData(value interface{}) (string, string, string) { // {"name": "xxx","hash": "yyy","uid": "zzz"}
	var name, hash, uid string
	for k, v := range value.(map[string]interface{}) {
		switch vv := v.(type) {
		case string:
//			utils.Info.Println(k, "is string", vv)
			if k == "name" {
				name = vv
			} else if k == "hash" {
				hash = vv
			} else if k == "uid" {
				uid = vv
			} else {
				utils.Info.Println(k, "is of an unknown type")
				return "", "", ""
			}
		default:
			utils.Info.Println(k, "is of an unknown type")
			return "", "", ""
		}
	}
	return name, hash, uid
}

func initChannels() {
	ftChannel = make(chan utils.FileTransferCache)
	serviceMgrChannel = make([]chan map[string]interface{}, 1)
	serviceMgrChannel[0] = make(chan map[string]interface{})
	serviceDataChan = make([]chan map[string]interface{}, 1)
	serviceDataChan[0] = make(chan map[string]interface{})
	transportMgrChannel = make([]chan string, NUMOFTRANSPORTMGRS)
	transportDataChan = make([]chan map[string]interface{}, NUMOFTRANSPORTMGRS)
	backendChan = make([]chan map[string]interface{}, NUMOFTRANSPORTMGRS)
	for i := 0; i < NUMOFTRANSPORTMGRS; i++ {
		transportMgrChannel[i] = make(chan string)
		transportDataChan[i] = make(chan map[string]interface{})
		backendChan[i] = make(chan map[string]interface{})
	}
	atsChannel = make([]chan string, 2)
	atsChannel[0] = make(chan string) // access token verification
	atsChannel[1] = make(chan string) // token cancellation
}

/*type CoreInterface interface {
	vssPathListHandler(w http.ResponseWriter, r *http.Request)
}*/

func main() {
	// Create new parser object
	parser := argparse.NewParser("print", "VISS v3.0 Server")
	// Create string flag
	logFile := parser.Flag("", "logfile", &argparse.Options{Required: false, Help: "outputs to logfile in ./logs folder"})
	logLevel := parser.Selector("", "loglevel", []string{"trace", "debug", "info", "warn", "error", "fatal", "panic"}, &argparse.Options{
		Required: false,
		Help:     "changes log output level",
		Default:  "info"})
	dPop := parser.Flag("d", "dpop", &argparse.Options{Required: false, Help: "Populate tree defaults", Default: false})
	pList := parser.Flag("p", "pathlist", &argparse.Options{Required: false, Help: "Generate pathlist file", Default: false})
	pListPath := parser.String("", "pListPath", &argparse.Options{Required: false, Help: "path to pathlist file", Default: "../"})
	stateDB := parser.Selector("s", "statestorage", []string{"sqlite", "redis", "memcache", "apache-iotdb", "none"}, &argparse.Options{Required: false,
		Help: "Statestorage must be either sqlite, redis, memcache, apache-iotdb, or none", Default: "redis"})
	historySupport := parser.Flag("j", "history", &argparse.Options{Required: false, Help: "Support for historic data requests", Default: false})
	dbFile := parser.String("", "dbfile", &argparse.Options{
		Required: false,
		Help:     "statestorage database filename",
		Default:  "serviceMgr/statestorage.db"})
	consentSupport := parser.Flag("c", "consentsupport", &argparse.Options{Required: false, Help: "try to connect to ECF", Default: false})
	mqttEnable := parser.Flag("m", "mqttenable", &argparse.Options{Required: false, Help: "enable MQTT usage", Default: false})

	// Parse input
	err := parser.Parse(os.Args)
	if err != nil {
		fmt.Print(parser.Usage(err))
	}

	utils.InitLog("servercore-log.txt", "./logs", *logFile, *logLevel)

	if !utils.InitForest("viss.him") {
		utils.Error.Printf("Failed to initialize viss.him")
		return
	}

	if *dPop {
		utils.PopulateDefault()
	}
	if *pList {
		utils.CreatePathListFile(*pListPath)
	}

	initChannels()

/*	router := mux.NewRouter()
	router.HandleFunc("/vsspathlist", pathList.VssPathListHandler).Methods("GET")

	srv := &http.Server{
		Addr:         "0.0.0.0:8081",
		WriteTimeout: time.Second * 15,
		ReadTimeout:  time.Second * 15,
		IdleTimeout:  time.Second * 60,
		Handler:      router,
	}

	// Active wait for 3 seconds to allow the server to start
	time.Sleep(3 * time.Second)

	go func() {
		utils.Info.Printf("Server is listening on %s", srv.Addr)
		if err := srv.ListenAndServe(); err != nil {
			log.Fatal("ListenAndServe: ", err)
		}
	}()*/

	for _, serverComponent := range serverComponents {
		switch serverComponent {
		case "httpMgr":
			go httpMgr.HttpMgrInit(0, transportMgrChannel[0])
			go transportDataSession(transportMgrChannel[0], transportDataChan[0], backendChan[0])
		case "wsMgr":
			go wsMgr.WsMgrInit(1, transportMgrChannel[1])
			go transportDataSession(transportMgrChannel[1], transportDataChan[1], backendChan[1])
		case "wsMgrFT":
			go wsMgrFT.WsMgrFTInit(ftChannel)
		case "mqttMgr":
			if *mqttEnable {
				go mqttMgr.MqttMgrInit(2, transportMgrChannel[2])
				go transportDataSession(transportMgrChannel[2], transportDataChan[2], backendChan[2])
			}
		case "grpcMgr":
			go grpcMgr.GrpcMgrInit(3, transportMgrChannel[3])
			go transportDataSession(transportMgrChannel[3], transportDataChan[3], backendChan[3])
		case "serviceMgr":
			go serviceMgr.ServiceMgrInit(0, serviceMgrChannel[0], *stateDB, *historySupport, *dbFile)
			go serviceDataSession(serviceMgrChannel[0], serviceDataChan[0], backendChan)
		case "atServer":
			go atServer.AtServerInit(atsChannel[0], atsChannel[1], *consentSupport)
		}
	}

	utils.Info.Printf("main():starting loop for channel receptions...")
	for {
		select {
		case request := <-transportDataChan[0]: // request from HTTP/HTTPS mgr
			serveRequest(request, 0, 0)
		case request := <-transportDataChan[1]: // request from WS/WSS mgr
			serveRequest(request, 1, 0)
		case request := <-transportDataChan[2]: // request from MQTT mgr
			serveRequest(request, 2, 0)
		case request := <-transportDataChan[3]: // request from gRPC mgr
			serveRequest(request, 3, 0)
		case gatingId := <-atsChannel[1]:
//			request := `{"action": "internal-cancelsubscription", "gatingId":"` + gatingId + `"}`
			var request map[string]interface{}
			request["action"] = "internal-cancelsubscription"
			request["gatingId"] = gatingId
			serveRequest(request, 0, 0)
			//  case request := <- transportDataChan[X]:  // implement when there is a Xth transport protocol mgr
		}
	}
}
