/**
* (C) 2023 Ford Motor Company
* (C) 2022 Geotab Inc
* (C) 2020 Mitsubishi Electrics Automotive
* (C) 2019 Geotab Inc
* (C) 2019,2023 Volvo Cars
*
* All files and artifacts in the repository at https://github.com/w3c/automotive-viss2
* are licensed under the provisions of the license provided by the LICENSE file in this repository.
*
**/

package main

import (
	//   "fmt"

	"fmt"
	"log"
	"os"

	"github.com/akamensky/argparse"
	"github.com/gorilla/mux"

	"bytes"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/w3c/automotive-viss2/server/vissv2server/atServer"
	"github.com/w3c/automotive-viss2/server/vissv2server/grpcMgr"
	"github.com/w3c/automotive-viss2/server/vissv2server/httpMgr"
	"github.com/w3c/automotive-viss2/server/vissv2server/serviceMgr"
	"github.com/w3c/automotive-viss2/server/vissv2server/wsMgr"
	"github.com/w3c/automotive-viss2/server/vissv2server/mqttMgr"

	gomodel "github.com/COVESA/vss-tools/binary/go_parser/datamodel"
	golib "github.com/COVESA/vss-tools/binary/go_parser/parserlib"
	"github.com/w3c/automotive-viss2/utils"
)

var VSSTreeRoot *gomodel.Node_t

// set to MAXFOUNDNODES in cparserlib.h
const MAXFOUNDNODES = 1500

// the server components started as threads by vissv2server. If a component is commented out, it will not be started
var serverComponents []string = []string{
	"serviceMgr",
	"httpMgr",
	"wsMgr",
//	"mqttMgr",  //to avoid calls to the mosquitto broker if not used anyway
	"grpcMgr",
	"atServer",
}

/*
 * For communication between transport manager threads and vissv2server thread.
 * If support for new transport protocol is added, add element to channel
 */
var transportMgrChannel = []chan string{
	make(chan string), // HTTP
	make(chan string), // WS
	make(chan string), // MQTT
	make(chan string), // gRPC
}

var serviceMgrChannel = []chan string{
	make(chan string), // Vehicle service
}

var atsChannel = []chan string{
	make(chan string),  // access token verification
	make(chan string),  // token cancellation
}

// add element to both channels if support for new transport protocol is added
var transportDataChan = []chan string{
	make(chan string),
	make(chan string),
	make(chan string),
	make(chan string),
}

var backendChan = []chan string{
	make(chan string),
	make(chan string),
	make(chan string),
	make(chan string),
}

var serviceDataPortNum int = 8200 // port number interval [8200-]

// add element if support for new service manager is added
var serviceDataChan = []chan string{
	make(chan string),
	//	make(chan string),
}

var errorResponseMap = map[string]interface{}{
	"RouterId":  0,
	"action":    "unknown",
	"requestId": "XXX",
	"error":     `{"number":AAA, "reason": "BBB", "message": "CCC"}`,
	"ts":        1234,
}

func extractMgrId(routerId string) int { // "RouterId" : "mgrId?clientId"
	delim := strings.Index(routerId, "?")
	mgrId, _ := strconv.Atoi(routerId[:delim])
	return mgrId
}

func getRouterId(response string) string { // "RouterId" : "mgrId?clientId",
	afterRouterIdKey := strings.Index(response, "RouterId")
	if afterRouterIdKey == -1 {
		return ""
	}
	afterRouterIdKey += 8 + 1 // points to after quote
	routerIdValStart := utils.NextQuoteMark([]byte(response), afterRouterIdKey) + 1
	routerIdValStop := utils.NextQuoteMark([]byte(response), routerIdValStart)
	utils.Info.Printf("getRouterId: %s", response[routerIdValStart:routerIdValStop])
	return response[routerIdValStart:routerIdValStop]
}

func serviceDataSession(serviceMgrChannel chan string, serviceDataChannel chan string, backendChannel []chan string) {
	for {
		select {

		case response := <-serviceMgrChannel:
			utils.Info.Printf("Server core: Response from service mgr:%s", string(response))
			mgrIndex := extractMgrId(getRouterId(string(response)))
			utils.Info.Printf("mgrIndex=%d", mgrIndex)
			backendChannel[mgrIndex] <- string(response)
		case request := <-serviceDataChannel:
			utils.Info.Printf("Server core: Request to service:%s", request)
			serviceMgrChannel <- request
		}
	}
}

func transportDataSession(transportMgrChannel chan string, transportDataChannel chan string, backendChannel chan string) {
	for {
		select {

		case msg := <-transportMgrChannel:
			utils.Info.Printf("request: %s", msg)
			transportDataChannel <- msg // send request to server hub
		case message := <-backendChannel:
			utils.Info.Printf("Transport mgr server: message= %s", message)
			transportMgrChannel <- message
		}
	}
}

func initVssFile() bool {
	filePath := "vss_vissv2.binary"
	VSSTreeRoot = golib.VSSReadTree(filePath)

	if VSSTreeRoot == nil {
		//		utils.Error.Println("Tree file not found")
		return false
	}

	return true
}

func searchTree(rootNode *gomodel.Node_t, path string, anyDepth bool, leafNodesOnly bool, listSize int, noScopeList []string, validation *int) (int, []golib.SearchData_t) {
	utils.Info.Printf("searchTree(): path=%s, anyDepth=%t, leafNodesOnly=%t", path, anyDepth, leafNodesOnly)
	if len(path) > 0 {
		var searchData []golib.SearchData_t
		var matches int
		searchData, matches = golib.VSSsearchNodes(path, rootNode, MAXFOUNDNODES, anyDepth, leafNodesOnly, listSize, noScopeList, validation)
		//		searchData, matches = golib.VSSsearchNodes(path, rootNode, MAXFOUNDNODES, anyDepth, leafNodesOnly, validation)
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
	utils.SetErrorResponse(reqMap, errorResponseMap, "400", "Bad Request", getTokenErrorMessage(errorCode))
}

// Sends a message to the Access Token Server to validate the Access Token paths and permissions
func verifyToken(token string, action string, paths string, validation int) (int, string, string) {
	handle := ""
	gatingId := ""
	request := `{"token":"` + token + `","paths":"` + paths + `","action":"` + action + `","validation":"` + strconv.Itoa(validation) + `"}`
	atsChannel[0] <- request
	body := <- atsChannel[0]
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

func jsonifyTreeNode(nodeHandle *gomodel.Node_t, jsonBuffer string, depth int, maxDepth int) string {
	if depth >= maxDepth {
		return jsonBuffer
	}
	depth++
	var newJsonBuffer string
	nodeName := golib.VSSgetName(nodeHandle)
	newJsonBuffer += `"` + nodeName + `":{`
	nodeType := int(golib.VSSgetType(nodeHandle))
	utils.Info.Printf("nodeType=%d", nodeType)
	newJsonBuffer += `"type":` + `"` + nodeTypesToString(nodeType) + `",`
	nodeDescr := golib.VSSgetDescr(nodeHandle)
	newJsonBuffer += `"description":` + `"` + nodeDescr + `",`
	nodeNumofChildren := golib.VSSgetNumOfChildren(nodeHandle)
	switch nodeType {
	case 4: // branch
	case 1: // sensor
		fallthrough
	case 2: // actuator
		fallthrough
	case 3: // attribute
		// TODO Look for other metadata, unit, enum, ...
		nodeDatatype := golib.VSSgetDatatype(nodeHandle)
		newJsonBuffer += `"datatype":` + `"` + nodeDatatype + `",`
	default:
		return ""

	}
	if depth < maxDepth {
		if nodeNumofChildren > 0 {
			newJsonBuffer += `"children":` + "{"
		}
		for i := 0; i < nodeNumofChildren; i++ {
			childNode := golib.VSSgetChild(nodeHandle, i)
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

func countPathSegments(path string) int {
	return strings.Count(path, ".") + 1
}

func getNoScopeList(tokenContext string) ([]string, int) {
	// call ATS to get list
	hostIp := utils.GetServerIP()
	url := "http://" + hostIp + ":8600/atserver"

	data := []byte(`{"context":"` + tokenContext + `"}`)

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(data))
	if err != nil {
		utils.Error.Print("getNoScopeList: Error reading request. ", err)
		return nil, 0
	}

	// Set headers
	req.Header.Set("Access-Control-Allow-Origin", "*")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Host", hostIp+":8600")

	// Set client timeout
	client := &http.Client{Timeout: time.Second * 10}

	// Send request
	resp, err := client.Do(req)
	if err != nil {
		utils.Error.Print("getNoScopeList: Error sending request. ", err)
		return nil, 0
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		utils.Error.Print("getNoScopeList:Error reading response. ", err)
		return nil, 0
	}
	var noScopeMap map[string]interface{}
	err = json.Unmarshal(body, &noScopeMap)
	if err != nil {
		utils.Error.Printf("initPurposelist:error data=%s, err=%s", data, err)
		return nil, 0
	}
	return extractNoScopeElementsLevel1(noScopeMap)
}

func extractNoScopeElementsLevel1(noScopeMap map[string]interface{}) ([]string, int) {
	for k, v := range noScopeMap {
		switch vv := v.(type) {
		case string:
			utils.Info.Println(k, "is string", vv)
			noScopeList := make([]string, 1)
			noScopeList[0] = vv
			return noScopeList, 1
		case []interface{}:
			utils.Info.Println(k, "is an array:, len=", strconv.Itoa(len(vv)))
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
			utils.Info.Println(k, "is string", vv)
			noScopeList[i] = vv
		default:
			utils.Info.Println(k, "is of an unknown type")
		}
		i++
	}
	return noScopeList, i
}

// Gets node and childs data as string from VSS tree
func synthesizeJsonTree(path string, depth int, tokenContext string) string {
	var jsonBuffer string
	var searchData []golib.SearchData_t
	var matches int
	noScopeList, numOfListElem := getNoScopeList(tokenContext)
	//utils.Info.Printf("noScopeList[0]=%s", noScopeList[0])
	matches, searchData = searchTree(VSSTreeRoot, path, false, false, numOfListElem, noScopeList, nil)
	if matches < countPathSegments(path) {
		return ""
	}
	subTreeRoot := searchData[matches-1].NodeHandle
	utils.Info.Printf("synthesizeJsonTree:subTreeRoot-name=%s", golib.VSSgetName(subTreeRoot))
	if depth == 0 {
		depth = 100
	}
	jsonBuffer = jsonifyTreeNode(subTreeRoot, jsonBuffer, 0, depth)
	if len(jsonBuffer) > 0 {
		return "{" + jsonBuffer[:len(jsonBuffer)-1] + "}" // remove comma
	}
	return ""
}

func getTokenContext(reqMap map[string]interface{}) string {
	if reqMap["authorization"] != nil {
		return utils.ExtractFromToken(reqMap["authorization"].(string), "clx")
	}
	return ""
}

func validRequest(request string, action string) bool {
	switch action {
	case "get":
		return isValidGetParams(request)
	case "set":
		return isValidSetParams(request)
	case "subscribe":
		return isValidSubscribeParams(request)
	case "unsubscribe":
		return isValidUnsubscribeParams(request)
	case "internal-killsubscriptions":
		return true
	case "internal-cancelsubscription":
		return true
	}
	return false
}

func isValidGetParams(request string) bool {
	if strings.Contains(request, "path") == false {
		return false
	}
	if strings.Contains(request, "filter") == true {
		return isValidGetFilter(request)
	}
	return true
}

func isValidGetFilter(request string) bool { // paths, history,static-metadata, dynamic-metadata supported
	if strings.Contains(request, "paths") == true {
		if strings.Contains(request, "parameter") == true {
			return true
		}
	}
	if strings.Contains(request, "history") == true {
		if strings.Contains(request, "parameter") == true {
			return true
		}
	}
	if strings.Contains(request, "static-metadata") == true {
		if strings.Contains(request, "parameter") == true {
			return true
		}
	}
	if strings.Contains(request, "dynamic-metadata") == true {
		if strings.Contains(request, "parameter") == true {
			return true
		}
	}
	return false
}

func isValidSetParams(request string) bool {
	return strings.Contains(request, "path") && strings.Contains(request, "value")
}

func isValidSubscribeParams(request string) bool {
	if strings.Contains(request, "path") == false {
		return false
	}
	if strings.Contains(request, "filter") == true {
		return isValidSubscribeFilter(request)
	}
	return true
}

func isValidSubscribeFilter(request string) bool { // paths, history, timebased, range, change, curvelog, static-metadata, dynamic-metadata supported
	if isValidGetFilter(request) == true {
		return true
	}
	if strings.Contains(request, "timebased") == true {
		if strings.Contains(request, "parameter") == true && strings.Contains(request, "period") == true {
			return true
		}
	}
	if strings.Contains(request, "range") == true {
		if strings.Contains(request, "parameter") == true && strings.Contains(request, "logic-op") == true &&
			strings.Contains(request, "boundary") == true {
			return true
		}
	}
	if strings.Contains(request, "change") == true {
		if strings.Contains(request, "parameter") == true && strings.Contains(request, "logic-op") == true &&
			strings.Contains(request, "diff") == true {
			return true
		}
	}
	if strings.Contains(request, "curvelog") == true {
		if strings.Contains(request, "parameter") == true && strings.Contains(request, "maxerr") == true &&
			strings.Contains(request, "bufsize") == true {
			return true
		}
	}
	return false
}

func isValidUnsubscribeParams(request string) bool {
	return strings.Contains(request, "subscriptionId")
}

// Receives a request (json containing path, action, token, routerId....) and calls
// the appropriate function to handle the request
func serveRequest(request string, tDChanIndex int, sDChanIndex int) {
	utils.Info.Printf("serveRequest():request=%s", request)
	var requestMap = make(map[string]interface{})
	if utils.MapRequest(request, &requestMap) != 0 {
		utils.Error.Printf("serveRequest():invalid JSON format=%s", request)
		utils.SetErrorResponse(requestMap, errorResponseMap, "400", "invalid request syntax", "See VISSv2 spec and JSON RFC for valid request syntax.")
		backendChan[tDChanIndex] <- utils.FinalizeMessage(errorResponseMap)
		return
	}
	if requestMap["action"] == nil || validRequest(request, requestMap["action"].(string)) == false {
		utils.Error.Printf("serveRequest():invalid action params=%s", requestMap["action"])
		utils.SetErrorResponse(requestMap, errorResponseMap, "400", "invalid request syntax", "Request parameter invalid.")
		backendChan[tDChanIndex] <- utils.FinalizeMessage(errorResponseMap)
		return
	}
	if requestMap["path"] != nil && strings.Contains(requestMap["path"].(string), "*") == true {
		utils.Error.Printf("serveRequest():path contained wildcard=%s", requestMap["path"])
		utils.SetErrorResponse(requestMap, errorResponseMap, "400", "invalid request syntax", "Wildcard must be in filter expression.")
		backendChan[tDChanIndex] <- utils.FinalizeMessage(errorResponseMap)
		return
	}
	if requestMap["path"] != nil {
		requestMap["path"] = utils.UrlToPath(requestMap["path"].(string)) // replace slash with dot
	}
	if requestMap["action"] == "set" && requestMap["filter"] != nil {
		utils.Error.Printf("serveRequest():Set request combined with filtering.")
		utils.SetErrorResponse(requestMap, errorResponseMap, "400", "invalid request", "Set request must not contain filtering.")
		backendChan[tDChanIndex] <- utils.FinalizeMessage(errorResponseMap)
		return
	}
	if requestMap["action"] == "unsubscribe" {
		serviceDataChan[sDChanIndex] <- request
		return
	}
	issueServiceRequest(requestMap, tDChanIndex, sDChanIndex)
}

func issueServiceRequest(requestMap map[string]interface{}, tDChanIndex int, sDChanIndex int) {
	if requestMap["action"] == "internal-killsubscriptions" || requestMap["action"] == "internal-cancelsubscription" {
		serviceDataChan[sDChanIndex] <- utils.FinalizeMessage(requestMap) // internal message
		return
	}
	rootPath := requestMap["path"].(string)
	var searchPath []string

	// Manages Filter Request
	if requestMap["filter"] != nil {
		var filterList []utils.FilterObject // type + parameter
		utils.UnpackFilter(requestMap["filter"], &filterList)
		// Iterates all the filters
		for i := 0; i < len(filterList); i++ {
			utils.Info.Printf("filterList[%d].Type=%s, filterList[%d].Parameter=%s", i, filterList[i].Type, i, filterList[i].Parameter)
			// PATH FILTER
			if filterList[i].Type == "paths" {
				if strings.Contains(filterList[i].Parameter, "[") { // Various paths to search
					err := json.Unmarshal([]byte(filterList[i].Parameter), &searchPath) // Writes in search path all values in filter
					if err != nil {
						utils.Error.Printf("Unmarshal filter path array failed.")
						utils.SetErrorResponse(requestMap, errorResponseMap, "400", "Internal error.", "Unmarshall failed on array of paths.")
						backendChan[tDChanIndex] <- utils.FinalizeMessage(errorResponseMap)
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

			// STATIC METADATA FILTER
			if filterList[i].Type == "static-metadata" {
				tokenContext := getTokenContext(requestMap) // Gets the client context from the token in the request
				if len(tokenContext) == 0 {
					tokenContext = "Undefined+Undefined+Undefined"
				}
				metadata := ""
				metadata = synthesizeJsonTree(requestMap["path"].(string), 2, tokenContext) // TODO: depth setting via filtering?
				if len(metadata) > 0 {
					delete(requestMap, "path")
					delete(requestMap, "filter")
					requestMap["ts"] = utils.GetRfcTime()
					backendChan[tDChanIndex] <- utils.AddKeyValue(utils.FinalizeMessage(requestMap), "metadata", metadata)
					return
				}
				utils.Error.Printf("Metadata not available.")
				utils.SetErrorResponse(requestMap, errorResponseMap, "400", "Bad request", "Metadata not available.")
				backendChan[tDChanIndex] <- utils.FinalizeMessage(errorResponseMap)
				return
			}
			// DYNAMIC METADATA FILTER
			if filterList[i].Type == "dynamic-metadata" && filterList[i].Parameter == "server_capabilities" {
				serviceDataChan[sDChanIndex] <- utils.FinalizeMessage(requestMap) // no further verification
				return
			}
		}
	}
	if requestMap["filter"] == nil || len(searchPath) == 0 {
		searchPath = make([]string, 1)
		searchPath[0] = rootPath
	}
	var searchData []golib.SearchData_t
	var matches int
	totalMatches := 0
	paths := ""
	maxValidation := -1
	for i := 0; i < len(searchPath); i++ {
		anyDepth := true
		validation := -1
		matches, searchData = searchTree(VSSTreeRoot, searchPath[i], anyDepth, true, 0, nil, &validation)
		//utils.Info.Printf("Path=%s, Matches=%d. Max validation from search=%d", searchPath[i], matches, int(validation))
		utils.Info.Printf("Matches=%d. Max validation from search=%d", matches, int(validation))
		for i := 0; i < matches; i++ {
			pathLen := getPathLen(string(searchData[i].NodePath[:]))
			paths += "\"" + string(searchData[i].NodePath[:pathLen]) + "\", "
		}
		totalMatches += matches
		maxValidation = utils.GetMaxValidation(int(validation), maxValidation)
	}
	if totalMatches == 0 {
		utils.SetErrorResponse(requestMap, errorResponseMap, "400", "No signals matching path.", "")
		backendChan[tDChanIndex] <- utils.FinalizeMessage(errorResponseMap)
		return
	}
	if requestMap["action"] == "set" && golib.VSSgetType(searchData[0].NodeHandle) != gomodel.ACTUATOR {
		utils.SetErrorResponse(requestMap, errorResponseMap, "400", "Illegal command", "Only the actuator node type can be set.")
		backendChan[tDChanIndex] <- utils.FinalizeMessage(errorResponseMap)
		return
	}
	paths = paths[:len(paths)-2]
	if totalMatches == 1 {
		paths = paths[1 : len(paths)-1] // remove hyphens
	} else if totalMatches > 1 {
		paths = "[" + paths + "]"
	}

	if requestMap["origin"] == "internal" { // internal message, no validation needed
		maxValidation = 0
	}

	tokenHandle := ""
	gatingId := ""
	switch maxValidation%10 {
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
			backendChan[tDChanIndex] <- utils.FinalizeMessage(errorResponseMap)
			return
		}
	default: // should not be possible...
		utils.SetErrorResponse(requestMap, errorResponseMap, "400", "Access control tag invalid.", "See VISSv2 spec for access control tagging")
		backendChan[tDChanIndex] <- utils.FinalizeMessage(errorResponseMap)
		return
	}
	requestMap["path"] = paths
	if tokenHandle != "" {
		requestMap["handle"] = tokenHandle
	}
	if gatingId != "" {
		requestMap["gatingId"] = gatingId
	}
	serviceDataChan[sDChanIndex] <- utils.FinalizeMessage(requestMap)
}

type CoreInterface interface {
	vssPathListHandler(w http.ResponseWriter, r *http.Request)
}

type PathList struct {
	LeafPaths []string
}

var pathList PathList

func sortPathList(listFname string) {
	data, err := ioutil.ReadFile(listFname)
	if err != nil {
		utils.Error.Printf("Error reading %s: %s\n", listFname, err)
		return
	}
	err = json.Unmarshal([]byte(data), &pathList)
	if err != nil {
		utils.Error.Printf("Error unmarshal json=%s\n", err)
		return
	}
	sort.Strings(pathList.LeafPaths)
	file, _ := json.Marshal(pathList)
	_ = ioutil.WriteFile(listFname, file, 0644)
}

func createPathListFile(listFname string) {
	// call int VSSGetLeafNodesList(long rootNode, char* leafNodeList);
	golib.VSSGetLeafNodesList(VSSTreeRoot, listFname)
	sortPathList(listFname)
}

func main() {
	// Create new parser object
	parser := argparse.NewParser("print", "Server Core")
	// Create string flag
	logFile := parser.Flag("", "logfile", &argparse.Options{Required: false, Help: "outputs to logfile in ./logs folder"})
	logLevel := parser.Selector("", "loglevel", []string{"trace", "debug", "info", "warn", "error", "fatal", "panic"}, &argparse.Options{
		Required: false,
		Help:     "changes log output level",
		Default:  "info"})
	dryRun := parser.Flag("", "dryrun", &argparse.Options{Required: false, Help: "dry run to generate vsspathlist file", Default: false})
	vssJson := parser.String("", "vssJson", &argparse.Options{Required: false, Help: "path and name vssPathlist json file", Default: "../vsspathlist.json"})
	stateDB := parser.Selector("s", "statestorage", []string{"sqlite", "redis", "apache-iotdb", "none"}, &argparse.Options{Required: false,
		Help: "Statestorage must be either sqlite, redis, apache-iotdb, or none", Default: "redis"})
	udsPath := parser.String("", "uds", &argparse.Options{
		Required: false,
		Help:     "Set UDS path and file",
		Default:  "/var/tmp/vissv2/histctrlserver.sock"})
	dbFile := parser.String("", "dbfile", &argparse.Options{
		Required: false,
		Help:     "statestorage database filename",
		Default:  "serviceMgr/statestorage.db"})
	consentSupport := parser.Flag("c", "consentsupport", &argparse.Options{Required: false, Help: "try to connect to ECF", Default: false})

	// Parse input
	err := parser.Parse(os.Args)
	if err != nil {
		fmt.Print(parser.Usage(err))
	}

	utils.InitLog("servercore-log.txt", "./logs", *logFile, *logLevel)

	if !initVssFile() {
		utils.Error.Fatal(" Tree file not found")
		return
	}

	createPathListFile(*vssJson) // save in server directory, where transport managers will expect it to be
	if *dryRun {
		utils.Info.Printf("vsspathlist.json created. Job done.")
		return
	}

	router := mux.NewRouter()
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
	}()

	for _, serverComponent := range serverComponents {
		switch serverComponent {
		case "httpMgr":
			go httpMgr.HttpMgrInit(0, transportMgrChannel[0])
			go transportDataSession(transportMgrChannel[0], transportDataChan[0], backendChan[0])
		case "wsMgr":
			go wsMgr.WsMgrInit(1, transportMgrChannel[1])
			go transportDataSession(transportMgrChannel[1], transportDataChan[1], backendChan[1])
		case "mqttMgr":
			go mqttMgr.MqttMgrInit(2, transportMgrChannel[2])
			go transportDataSession(transportMgrChannel[2], transportDataChan[2], backendChan[2])
		case "grpcMgr":
			go grpcMgr.GrpcMgrInit(3, transportMgrChannel[3])
			go transportDataSession(transportMgrChannel[3], transportDataChan[3], backendChan[3])
		case "serviceMgr":
			go serviceMgr.ServiceMgrInit(0, serviceMgrChannel[0], *stateDB, *udsPath, *dbFile)
			go serviceDataSession(serviceMgrChannel[0], serviceDataChan[0], backendChan)
		case "atServer":
			go atServer.AtServerInit(atsChannel[0], atsChannel[1], VSSTreeRoot, *consentSupport)
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
		case gatingId := <- atsChannel[1]:
			request := `{"action": "internal-cancelsubscription", "gatingId":"` + gatingId + `"}`
			serveRequest(request, 0, 0)
			//  case request := <- transportDataChan[X]:  // implement when there is a Xth transport protocol mgr
		}
	}
}
