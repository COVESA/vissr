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
	"sync"

	"github.com/akamensky/argparse"

	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"gopkg.in/yaml.v3"
	"io"
	"strconv"
	"strings"

	"github.com/covesa/vissr/server/vissv2server/atServer"
	"github.com/covesa/vissr/server/vissv2server/grpcMgr"
	"github.com/covesa/vissr/server/vissv2server/httpMgr"
	"github.com/covesa/vissr/server/vissv2server/mqttMgr"
	"github.com/covesa/vissr/server/vissv2server/serviceMgr"
	"github.com/covesa/vissr/server/vissv2server/wsMgr"
	"github.com/covesa/vissr/server/vissv2server/wsMgrFT"
	"github.com/covesa/vissr/server/vissv2server/udsMgr"

	"github.com/covesa/vissr/utils"
)

// atsChannelMu serializes the request/response RPC over atsChannel[0].
// Without it, two concurrent serveRequest goroutines (one per transport)
// can interleave their request and response on the shared channel and
// one will receive the other's reply. Same shape applies to ftChannel.
var atsChannelMu sync.Mutex
var ftChannelMu sync.Mutex

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
	"udsMgr",
	"atServer",
}

/*
 * For communication between transport manager threads and vissv2server thread.
 * If support for new transport protocol is added, add element to channel
 */
 const NUMOFTRANSPORTMGRS = 5  // order assigned to channels: HTTP, WS, MQTT, gRPC, UDS
var transportMgrChannel []chan string
var transportDataChan []chan map[string]interface{}
var backendChan []chan map[string]interface{}

var serviceMgrChannel []chan map[string]interface{}

var atsChannel []chan string

var serviceDataPortNum int = 8200 // port number interval [8200-]

// add element if support for new service manager is added
var serviceDataChan []chan map[string]interface{}

var ftChannel chan utils.FileTransferCache

var errorResponseMap = map[string]interface{}{}

// extractMgrId parses a RouterId of the form "mgrId?clientId" and returns
// the mgrId, or -1 if the string is malformed (missing '?' or non-numeric
// mgrId). The previous version did `routerId[:delim]` with delim == -1 on a
// missing delimiter and silently routed parse failures to channel 0.
func extractMgrId(routerId string) int {
	delim := strings.Index(routerId, "?")
	if delim <= 0 {
		return -1
	}
	mgrId, err := strconv.Atoi(routerId[:delim])
	if err != nil {
		return -1
	}
	return mgrId
}

func serviceDataSession(serviceMgrChannel chan map[string]interface{}, serviceDataChannel chan map[string]interface{}, backendChannel []chan map[string]interface{}) {
	for {
		select {

		case response := <-serviceMgrChannel:
			routerId, ok := response["RouterId"].(string)
			if !ok {
				utils.Error.Printf("serviceDataSession: missing/invalid RouterId in response; dropping")
				continue
			}
			mgrIndex := extractMgrId(routerId)
			if mgrIndex < 0 || mgrIndex >= len(backendChannel) {
				utils.Error.Printf("serviceDataSession: invalid mgrIndex=%d from RouterId=%q; dropping", mgrIndex, routerId)
				continue
			}
			backendChannel[mgrIndex] <- response
		case request := <-serviceDataChannel:
			serviceMgrChannel <- request
		}
	}
}

func transportDataSession(transportMgrChannel chan string, transportDataChannel chan map[string]interface{}, backendChannel chan map[string]interface{}) {
	for {
		select {

		case msg := <-transportMgrChannel:
			var msgMap map[string]interface{}
			utils.MapRequest(msg, &msgMap)
			// Bug fix: previously unconditional send could wedge the per-transport
			// goroutine if the central dispatcher was slow. Non-blocking with
			// drop-and-log on overflow.
			select {
			case transportDataChannel <- msgMap:
			default:
				utils.Error.Printf("transportDataSession: transportDataChannel full, dropping request")
			}
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
	case 22:
		return "Invalid ISS. "
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

// Sends a message to the Access Token Server to validate the Access Token paths and permissions.
// Uses atsChannelMu to serialize the request/response over the shared atsChannel[0]; without
// it, two concurrent serveRequest goroutines would race and one might receive the other's reply.
func verifyToken(token string, action string, paths string, validation int) (int, string, string) {
	handle := ""
	gatingId := ""
	request := `{"token":"` + token + `","paths":` + paths + `,"action":"` + action + `","validation":"` + strconv.Itoa(validation) + `"}`

	atsChannelMu.Lock()
	atsChannel[0] <- request
	body := <-atsChannel[0]
	atsChannelMu.Unlock()

	var bdy map[string]interface{}
	if err := json.Unmarshal([]byte(body), &bdy); err != nil {
		utils.Error.Print("verifyToken: Error unmarshalling ats response. ", err)
		return 41, handle, gatingId
	}
	validationStr, ok := bdy["validation"].(string)
	if !ok {
		utils.Error.Print("verifyToken: missing/invalid validation claim in ats response")
		return 42, handle, gatingId
	}
	atsValidation, err := strconv.Atoi(validationStr)
	if err != nil {
		utils.Error.Print("verifyToken: Error converting validation claim to int. ", err)
		return 42, handle, gatingId
	}
	if atsValidation == 0 {
		if h, ok := bdy["handle"].(string); ok {
			handle = h
		}
		if g, ok := bdy["gatingId"].(string); ok {
			gatingId = g
		}
	}
	return atsValidation, handle, gatingId
}

func jsonifyTreeNode(nodeHandle *utils.Node_t, jsonBuffer string, depth int, maxDepth int) string {
	if depth >= maxDepth {
		return jsonBuffer
	}
	depth++
	var newJsonBuffer string
	nodeName := utils.VSSgetName(nodeHandle)
	newJsonBuffer += `"` + nodeName + `":{`
	nodeType := utils.VSSgetType(nodeHandle)
	newJsonBuffer += `"type":"` + nodeType + `",`
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
	// TODO Look for other metadata: unit, enum, ...
	nodeDatatype := utils.VSSgetDatatype(nodeHandle)
	if nodeDatatype != "" {
		newJsonBuffer += `"datatype":"` + nodeDatatype + `",`
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
	// Bug fix: previous code indexed [len-1] and [len-2] without bounds
	// check. Worst-case the accumulator is at least 6 chars (`":{`,`"type":"` etc.)
	// but be defensive.
	if len(newJsonBuffer) >= 2 &&
		newJsonBuffer[len(newJsonBuffer)-1] == ',' &&
		newJsonBuffer[len(newJsonBuffer)-2] != '}' {
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
	atsChannelMu.Lock()
	atsChannel[0] <- request
	body := <-atsChannel[0]
	atsChannelMu.Unlock()

	var noScopeMap map[string]interface{}
	err := json.Unmarshal([]byte(body), &noScopeMap)
	if err != nil {
		utils.Error.Printf("getNoScopeList: error body=%s, err=%v", body, err)
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
	if auth, ok := reqMap["authorization"].(string); ok {
		return utils.ExtractFromToken(auth, "clx")
	}
	return ""
}

// isInternalAction reports whether the request is one of the
// internal-only control actions (kill-subscriptions, cancel-subscription)
// that bypass the normal validation/auth path and go straight to
// serviceDataChan. Extracted from issueServiceRequest in PR #128 so the
// predicate can be unit-tested. See vissv2server_dispatch_test.go.
func isInternalAction(action string) bool {
	return action == "internal-killsubscriptions" || action == "internal-cancelsubscription"
}

// isUnsubscribeRequest reports whether the request action is the
// client-driven "unsubscribe" — these are forwarded directly to
// serviceDataChan from serveRequest without going through the
// validation/auth pipeline in issueServiceRequest. Extracted in PR #128.
func isUnsubscribeRequest(action string) bool {
	return action == "unsubscribe"
}

// setErrorAndForward fills errorResponseMap with the given error code
// and description (using the request fields from requestMap) and pushes
// it to the backend channel for the transport manager identified by
// tDChanIndex. Extracted in PR #128 — the SetErrorResponse → backendChan
// → return pattern was duplicated inline six times inside
// issueServiceRequest, and the function-level deduplication makes the
// happy path much easier to read.
func setErrorAndForward(requestMap map[string]interface{}, tDChanIndex int, errorIndex int, description string) {
	utils.SetErrorResponse(requestMap, errorResponseMap, errorIndex, description)
	backendChan[tDChanIndex] <- errorResponseMap
}

// expandPathFilter takes the Parameter from a "paths" filter and the
// request's rootPath, and returns the fully-qualified search paths.
// The parameter is either a single path string or a JSON array of path
// strings. Returns (paths, false) on malformed JSON so the caller can
// emit the appropriate bad-request response. Extracted from
// issueServiceRequest's filter loop in PR #128.
func expandPathFilter(parameter string, rootPath string) ([]string, bool) {
	if strings.Contains(parameter, "[") {
		var searchPath []string
		if err := json.Unmarshal([]byte(parameter), &searchPath); err != nil {
			return nil, false
		}
		for i := range searchPath {
			searchPath[i] = rootPath + "." + utils.UrlToPath(searchPath[i])
		}
		return searchPath, true
	}
	return []string{rootPath + "." + utils.UrlToPath(parameter)}, true
}

// authorizeAccess runs the per-request authorization check used by
// issueServiceRequest. It encapsulates the (slightly tricky) logic of
// when the auth token is actually consulted:
//   - no authorization header  → errorCode 2 (auth required)
//   - non-set request and the resource is write-only (validation%10==2
//     would mean read-also-restricted) → skip the check entirely
//   - authorization header present but not a string → errorCode 1
//   - otherwise → delegate to verifyToken
//
// Extracted from issueServiceRequest in PR #128 so the dispatch
// branches can be table-tested without the atsChannel goroutine.
// (The verifyToken delegation path itself still requires a live ats
// goroutine and is not covered by the new tests — same as before.)
func authorizeAccess(requestMap map[string]interface{}, paths string, maxValidation int) (errorCode int, tokenHandle string, gatingId string) {
	if requestMap["authorization"] == nil {
		return 2, "", ""
	}
	action, _ := requestMap["action"].(string)
	if action != "set" && maxValidation%10 != 2 { // no validation for get/sub when resource is write-only
		return 0, "", ""
	}
	authToken, ok := requestMap["authorization"].(string)
	if !ok {
		return 1, "", ""
	}
	return verifyToken(authToken, action, paths, maxValidation)
}

func serveRequest(requestMap map[string]interface{}, tDChanIndex int, sDChanIndex int) {
	if p, ok := requestMap["path"].(string); ok {
		requestMap["path"] = utils.UrlToPath(p) // replace slash with dot
	}
	if action, _ := requestMap["action"].(string); isUnsubscribeRequest(action) {
		serviceDataChan[sDChanIndex] <- requestMap
		return
	}
	issueServiceRequest(requestMap, tDChanIndex, sDChanIndex)
}

func issueServiceRequest(requestMap map[string]interface{}, tDChanIndex int, sDChanIndex int) {
	if action, _ := requestMap["action"].(string); isInternalAction(action) {
		serviceDataChan[sDChanIndex] <- requestMap // internal message
		return
	}
	rootPath, ok := requestMap["path"].(string)
	if !ok {
		utils.SetErrorResponse(requestMap, errorResponseMap, 1, "missing/invalid path")
		backendChan[tDChanIndex] <- errorResponseMap
		return
	}
	var VSSTreeRoot *utils.Node_t
	if !(rootPath == "HIM" && requestMap["action"] == "get" && requestMap["filter"] != nil) {
		VSSTreeRoot = utils.SetRootNodePointer(rootPath)
		if VSSTreeRoot == nil {
			setErrorAndForward(requestMap, tDChanIndex, 0, "") //bad_request
			return
		}
		requestMap["infoType"] = utils.GetInfoType(VSSTreeRoot)
//utils.Info.Printf("requestMap[infoType]=%s", requestMap["infoType"].(string))
	}
	var searchPath []string
	var filterList []utils.FilterObject // variant + parameter

	if requestMap["filter"] != nil {
		utils.UnpackFilter(requestMap["filter"], &filterList)
		// Iterates all the filters
		for i := 0; i < len(filterList); i++ {
//			utils.Info.Printf("filterList[%d].Type=%s, filterList[%d].Parameter=%s", i, filterList[i].Type, i, filterList[i].Parameter)
			// PATH FILTER
			if filterList[i].Type == "paths" {
				expanded, ok := expandPathFilter(filterList[i].Parameter, rootPath)
				if !ok {
					utils.Error.Printf("Unmarshal filter path array failed.")
					setErrorAndForward(requestMap, tDChanIndex, 0, "") //bad_request
					return
				}
				searchPath = expanded
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
				if rootPath ==  "HIM" {
					metadata = himJsonify()
				} else {
					metadata = synthesizeJsonTree(rootPath, treedepth, tokenContext, VSSTreeRoot)
				}
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
				setErrorAndForward(requestMap, tDChanIndex, 0, "") //bad_request
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
			setErrorAndForward(requestMap, tDChanIndex, 6, "") //unavailable_data
			return
		}
		if requestMap["action"] == "set" {
			// Bug fix: previous code did `VSSgetDatatype(...)[:5]` which
			// panicked when the datatype string was shorter than 5 chars
			// (e.g. "int", "bool"). Use strings.HasPrefix instead.
			datatype := utils.VSSgetDatatype(searchData[0].NodeHandle)
			if strings.HasPrefix(datatype, "Types") {
				val, ok := requestMap["value"].(map[string]interface{})
				if !ok {
					utils.SetErrorResponse(requestMap, errorResponseMap, 1, "struct value must be an object")
					backendChan[tDChanIndex] <- errorResponseMap
					return
				}
				res := verifyStruct(val, datatype, 0)
				if res != "ok" {
					utils.SetErrorResponse(requestMap, errorResponseMap, 1, res) //invalid_data
					backendChan[tDChanIndex] <- errorResponseMap
					return
				}
			}
		}
		totalMatches += matches
		maxValidation = utils.GetMaxValidation(int(validation), maxValidation)
	}
	if strings.Contains(utils.VSSgetDatatype(searchData[0].NodeHandle), ".FileDescriptor") {  // path to struct def
		response := initiateFileTransfer(requestMap, utils.VSSgetType(searchData[0].NodeHandle), searchPath[0])
		backendChan[tDChanIndex] <- response
		return
	}
	errorIndex, errorDescription := validateData(requestMap, searchData, filterList)
	if errorIndex != -1 {
		setErrorAndForward(requestMap, tDChanIndex, errorIndex, errorDescription)
		return
	}
	// Defensive: paths is built ", "-separated from at least one match (matches==0
	// returned early above). Trim only when there's content.
	if len(paths) >= 2 {
		paths = paths[:len(paths)-2]
	}
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
		errorCode, handle, gId := authorizeAccess(requestMap, paths, maxValidation)
		tokenHandle = handle
		gatingId = gId
		if errorCode != 0 {
			setTokenErrorResponse(requestMap, errorCode)
			backendChan[tDChanIndex] <- errorResponseMap
			return
		}
	default: // should not be possible...
		setErrorAndForward(requestMap, tDChanIndex, 7, "") //service_unavailable
		return
	}
	if totalMatches == 1 && len(paths) >= 2 {
		paths = paths[1 : len(paths)-1] // remove surrounding quotes
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

func verifyStruct(value map[string]interface{}, typePath string, depth int) string {
	typeDefRoot := utils.SetRootNodePointer(typePath[:utils.GetFirstDotIndex(typePath)])
	if typeDefRoot == nil {
		return "Struct cannot be verified"
	}
	matches, typeSearch := searchTree(typeDefRoot, typePath + ".*", true, true, 0, nil, nil)
	if matches == 0 || !verifyStructMembers(value, typeSearch, matches, depth) || depth > 5 {
		return "Incorrect struct data"
	}
	return "ok"
}

func verifyStructMembers(value map[string]interface{}, typeSearch []utils.SearchData_t, matches int, depth int) bool {  // {"memName1": "data1", ..., "memNameN": "dataN"}
	for memName, memValue := range value {
		if !verifyStructMember(memName, typeSearch, matches) {
			return false
		}
		switch memValue.(type) {
			case map[string]interface{}: //inline struct
				if verifyStruct(memValue.(map[string]interface{}), 
						utils.VSSgetDatatype(typeSearch[getIndex(typeSearch, matches, memName)].NodeHandle), depth+1) != "ok" {
					return false
				}
		}
	}
	return true
}

func getIndex(typeSearch []utils.SearchData_t, matches int , memName string) int {
	for i := 0; i < matches; i++ {
		if utils.GetLastDotSegment(typeSearch[i].NodePath) == memName {
			return i
		}
	}
	return 0
}

func verifyStructMember(memberName string, typeSearch []utils.SearchData_t, matches int) bool {
	for i := 0; i < matches; i++ {
		if strings.ToLower(memberName) == strings.ToLower(utils.GetLastDotSegment(typeSearch[i].NodePath)) {
			return true
		}
	}
	return false
}

func validateData(requestMap map[string]interface{}, searchData []utils.SearchData_t, filterList []utils.FilterObject) (int, string) { // -1, "" means valid data
	if requestMap["action"] == "set" && utils.VSSgetType(searchData[0].NodeHandle) != utils.ACTUATOR {
		return 1, "Forbidden to write to read-only resource" //invalid_data
	}
	if requestMap["filter"] == nil {
		return -1, ""
	}
	for i := 0; i < len(filterList); i++ {
		var paramMap map[string]interface{}
		switch filterList[i].Type {
		case "range": //parameter:[{"logic-op":"a", "boundary": "x", "combination-op":"b"},{"logic-op":"c", "boundary": "y"}]
			utils.MapRequest(filterList[i].Parameter, &paramMap)
			bVal1, bVal2 := getRangeBoundaries(paramMap)
			if !utils.IsNumber(bVal1) || (len(bVal2) != 0 && !utils.IsNumber(bVal2)) { // number ok, one or two boundaries
				return 1, "Invalid range boundary datatype"
			}
		case "change": //parameter:{"logic-op":"a", "diff": "x"}
			utils.MapRequest(filterList[i].Parameter, &paramMap)
			diff, ok := paramMap["diff"].(string)
			if !ok {
				return 1, "change filter missing/invalid diff"
			}
			if !utils.IsNumber(diff) && !utils.IsBoolean(diff) { // number or boolean ok
				return 1, "Invalid change diff datatype"
			}
		case "curvelog": //parameter:{"maxerr":"a", "bufsize": "x"}
			utils.MapRequest(filterList[i].Parameter, &paramMap)
			maxerr, ok := paramMap["maxerr"].(string)
			if !ok {
				return 1, "curvelog filter missing/invalid maxerr"
			}
			if !utils.IsNumber(maxerr) { // number ok
				return 1, "Invalid curve log maxerr datatype"
			}
		}
	}
	return -1, ""
}

func himJsonify() string {
	var himMap map[string]interface{}

	data, err := os.ReadFile("viss.him")
	if err != nil {
		utils.Error.Printf("error reading viss.him")
		return ""
	}
	err = yaml.Unmarshal([]byte(data), &himMap)
	if err != nil {
		utils.Error.Printf("him file unmarshal error: %v", err)
		return ""
	}
	himMap = removeLocalProperty(himMap)
	data, err = json.Marshal(himMap)
	if err != nil {
		fmt.Printf("HIM JSON marshall error=%s\n", err)
		return ""
	}
	return string(data)
}

func removeLocalProperty(himMap map[string]interface{}) map[string]interface{} {
	for _, v1 := range himMap {
		// Bug fix: previous code did `v1.(map[string]interface{})` without
		// check - any non-map value in the parsed viss.him (string, number,
		// array, null) panicked the whole server at startup.
		inner, ok := v1.(map[string]interface{})
		if !ok {
			continue
		}
		delete(inner, "local")
	}
	return himMap
}

func getRangeBoundaries(paramMap interface{}) (string, string) {
	var bVal = []string{"", ""}
	switch pMap := paramMap.(type) {
	case []interface{}:
		for i := 0; i < len(pMap); i++ {
			if i > 1 {
				utils.Error.Printf("Range array size too big. Len=%d", len(pMap))
				break
			}
			// Bug fix: defensive .(map[string]interface{}) - array elements
			// could be strings/numbers from malformed JSON.
			elem, ok := pMap[i].(map[string]interface{})
			if !ok {
				utils.Error.Printf("getRangeBoundaries: element %d not an object", i)
				continue
			}
			bVal[i] = getRangeBoundary(elem)
		}
	case map[string]interface{}:
		bVal[0] = getRangeBoundary(pMap)
	default:
		utils.Info.Println(pMap, "is of an unknown type")
	}
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

func initiateFileTransfer(requestMap map[string]interface{}, nodeType string, path string) map[string]interface{} {
	var ftInitData utils.FileTransferCache
	var responseMap = map[string]interface{}{}
	routerId, _ := requestMap["RouterId"].(string)
	if requestMap["action"] == "set" && nodeType == utils.ACTUATOR { // download
		// requestMap["value"] arrives from client JSON. If the client
		// sends a non-object value (e.g. a bare string) for a
		// FileDescriptor actuator, the previous direct
		// .(interface{}) cast plus getFileDescriptorData's internal
		// .(map[string]interface{}) panic-walked into a daemon
		// crash. Type-check up front and reject malformed inputs.
		if requestMap["value"] == nil {
			utils.SetErrorResponse(requestMap, errorResponseMap, 1, "missing value for FileDescriptor set")
			return errorResponseMap
		}
		var uidString string
		ftInitData.UploadTransfer = false
		ftInitData.Name, ftInitData.Hash, uidString = getFileDescriptorData(requestMap["value"])
		if ftInitData.Name == "" {
			utils.SetErrorResponse(requestMap, errorResponseMap, 1, "invalid file descriptor")
			return errorResponseMap
		}
		// Bug fix: hex.DecodeString error discarded; conversion to fixed array panicked on length mismatch.
		uidByte, err := hex.DecodeString(uidString)
		if err != nil || len(uidByte) != utils.UIDLEN {
			utils.SetErrorResponse(requestMap, errorResponseMap, 1, "invalid uid")
			return errorResponseMap
		}
		copy(ftInitData.Uid[:], uidByte)
		ftInitData.Path = "./" //get it from statestorage when vss-tools have updated.

		// Bug fix: serialize the shared-channel RPC over ftChannel to prevent
		// two concurrent file transfers from interleaving their responses.
		ftChannelMu.Lock()
		ftChannel <- ftInitData
		ftInitData = <-ftChannel
		ftChannelMu.Unlock()

		if ftInitData.Status == 0 {
			responseMap["RouterId"] = routerId
			responseMap["action"] = "set"
			responseMap["ts"] = utils.GetRfcTime()
			return responseMap
		}
		utils.SetErrorResponse(requestMap, errorResponseMap, 7, "") //service_unavailable
		return errorResponseMap

	} else if requestMap["action"] == "get" && nodeType == utils.SENSOR { //upload
		reqPath, ok := requestMap["path"].(string)
		if !ok {
			utils.SetErrorResponse(requestMap, errorResponseMap, 1, "missing/invalid path")
			return errorResponseMap
		}
		ftInitData.UploadTransfer = true
		ftInitData.Path, ftInitData.Name = getInternalFileName(reqPath)
		if ftInitData.Name == "" {
			// Bug fix: previously every unknown path silently mapped to
			// "upload.txt"; that's a path-injection hazard. Reject unknown paths.
			utils.SetErrorResponse(requestMap, errorResponseMap, 1, "unknown upload path")
			return errorResponseMap
		}
		ftInitData.Hash = calculateHash(ftInitData.Path + ftInitData.Name)
		// Bug fix: previous code used a hardcoded uid ("2d878213"); now generate a random one.
		if _, err := rand.Read(ftInitData.Uid[:]); err != nil {
			utils.SetErrorResponse(requestMap, errorResponseMap, 7, "")
			return errorResponseMap
		}

		ftChannelMu.Lock()
		ftChannel <- ftInitData
		_ = <-ftChannel
		ftChannelMu.Unlock()

		responseMap["RouterId"] = routerId
		responseMap["action"] = "get"
		responseMap["path"] = reqPath
		responseMap["value"] = `{"name": "` + ftInitData.Name + `", "hash":"` + ftInitData.Hash + `","uid":"` + hex.EncodeToString(ftInitData.Uid[:]) + `"}`
		responseMap["ts"] = utils.GetRfcTime()
		return responseMap
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

// getInternalFileName maps between tree paths and files in the vehicle file
// system. Unknown paths return ("", "") so the caller rejects the request
// rather than silently writing/reading "upload.txt" for any input path.
func getInternalFileName(path string) (string, string) {
	switch path {
	case "Vehicle.UploadFile":
		return "", "upload.txt"
	}
	return "", ""
}

func getFileDescriptorData(value interface{}) (string, string, string) { // {"name": "xxx","hash": "yyy","uid": "zzz"}
	m, ok := value.(map[string]interface{})
	if !ok {
		return "", "", ""
	}
	var name, hash, uid string
	for k, v := range m {
		vv, ok := v.(string)
		if !ok {
			return "", "", ""
		}
		switch k {
		case "name":
			name = vv
		case "hash":
			hash = vv
		case "uid":
			uid = vv
		default:
			// Unknown keys: log and ignore rather than fail outright.
			utils.Info.Printf("getFileDescriptorData: unknown key %q", k)
		}
	}
	return name, hash, uid
}

func initChannels() {
	// Bug fix: all pipeline channels are now buffered so a slow consumer
	// (slow client, slow backend, slow access-token server) cannot wedge the
	// dispatcher loop. atsChannel[0] and ftChannel stay unbuffered because
	// they're explicitly serialized request/response by atsChannelMu /
	// ftChannelMu.
	const pipelineBuf = 32
	ftChannel = make(chan utils.FileTransferCache)
	serviceMgrChannel = make([]chan map[string]interface{}, 1)
	serviceMgrChannel[0] = make(chan map[string]interface{}, pipelineBuf)
	serviceDataChan = make([]chan map[string]interface{}, 1)
	serviceDataChan[0] = make(chan map[string]interface{}, pipelineBuf)
	transportMgrChannel = make([]chan string, NUMOFTRANSPORTMGRS)
	transportDataChan = make([]chan map[string]interface{}, NUMOFTRANSPORTMGRS)
	backendChan = make([]chan map[string]interface{}, NUMOFTRANSPORTMGRS)
	for i := 0; i < NUMOFTRANSPORTMGRS; i++ {
		transportMgrChannel[i] = make(chan string, pipelineBuf)
		transportDataChan[i] = make(chan map[string]interface{}, pipelineBuf)
		backendChan[i] = make(chan map[string]interface{}, pipelineBuf)
	}
	// MQTT's channel must stay unbuffered. MqttMgrInit performs a synchronous
	// VIN-fetch by sending on and receiving from the same channel in one
	// goroutine. A buffered channel causes an echo: the goroutine reads its own
	// send before transportDataSession can consume it. Unbuffered forces the
	// send to block until transportDataSession reads, preserving ordering.
	transportMgrChannel[2] = make(chan string)
	atsChannel = make([]chan string, 2)
	atsChannel[0] = make(chan string) // access token verification (serialized by atsChannelMu)
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
		os.Exit(1)
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
		case "udsMgr":
			go udsMgr.UdsMgrInit(4, transportMgrChannel[4])
			go transportDataSession(transportMgrChannel[4], transportDataChan[4], backendChan[4])
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
		case request := <-transportDataChan[4]: // request from UDS mgr
			serveRequest(request, 4, 0)
		case gatingId := <-atsChannel[1]:
//			request := `{"action": "internal-cancelsubscription", "gatingId":"` + gatingId + `"}`
			request := map[string]interface{}{}
			request["action"] = "internal-cancelsubscription"
			request["gatingId"] = gatingId
			serveRequest(request, 0, 0)
			//  case request := <- transportDataChan[X]:  // implement when there is a Xth transport protocol mgr
		}
	}
}
