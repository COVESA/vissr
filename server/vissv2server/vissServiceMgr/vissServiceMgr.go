/**
* (C) 2026 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE file in this repository.
*
* VISSv3.2 Service Manager
*
* Handles the four service operations defined in the VISSv3.2 SERVICES specification:
*   invoke   – execute a service procedure
*   monitor  – attach to an ongoing service execution
*   cancel   – cancel an ongoing invoke or monitor session
*   discover – retrieve service tree metadata
*
* A "service tree" is any HIM tree whose domain name ends in ".Service".
* Procedure nodes in the tree have node type 'procedure'; I/O containers have
* type 'iostruct'; parameters have type 'property' or 'symlink'.
*
* Service state machine per procedure path:
*   UNKNOWN → ONGOING → SUCCESSFUL | CANCELED | FAILED
**/

package vissServiceMgr

import (
	"encoding/json"
	"math/rand"
	"strconv"
	"sync"
	"time"

	"github.com/covesa/vissr/utils"
)

// ServiceStatus is the set of allowed status values from VISSv3.2 §2.
type ServiceStatus string

const (
	StatusUnknown    ServiceStatus = "UNKNOWN"
	StatusOngoing    ServiceStatus = "ONGOING"
	StatusSuccessful ServiceStatus = "SUCCESSFUL"
	StatusCanceled   ServiceStatus = "CANCELED"
	StatusFailed     ServiceStatus = "FAILED"
)

// serviceSession represents one active invoke or monitor session.
type serviceSession struct {
	serviceId   string
	path        string
	isInvoke    bool // true = invoke session, false = monitor session
	routerIndex int  // which backendChan[i] to send events on
	indata      map[string]interface{}
	outdata     map[string]interface{}
	status      ServiceStatus
	startedAt   time.Time
}

// serviceState tracks per-procedure persistent state.
type serviceState struct {
	status  ServiceStatus
	indata  map[string]interface{}
	outdata map[string]interface{}
}

var (
	mu sync.Mutex

	// sessions maps serviceId → session.
	sessions = map[string]*serviceSession{}

	// states maps procedure path → last known state.
	states = map[string]*serviceState{}
)

// generateServiceId produces a random numeric string ID.
func generateServiceId() string {
	return strconv.Itoa(rand.Intn(900000) + 100000)
}

// getTimestamp returns the current time in RFC3339 format.
func getTimestamp() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// getState returns the current serviceState for path, creating UNKNOWN if absent.
func getState(path string) *serviceState {
	if s, ok := states[path]; ok {
		return s
	}
	s := &serviceState{status: StatusUnknown}
	states[path] = s
	return s
}

// HandleInvoke processes an "invoke" action request.
//
// requestMap keys consumed: path, input, filter, requestId, authorization
// On success it writes to backendChan; on error it builds and returns an error map.
func HandleInvoke(requestMap map[string]interface{}, backendChan chan map[string]interface{}) {
	path, _ := requestMap["path"].(string)
	requestId, _ := requestMap["requestId"].(string)

	// Validate: path must exist and address a procedure node.
	node := utils.SetRootNodePointer(path)
	if node == nil || utils.VSSgetType(node) != utils.PROCEDURE {
		sendServiceError(backendChan, "invoke", requestId, "", StatusFailed,
			"400", "bad_request", "path must address a procedure node")
		return
	}

	// Extract and validate input against the procedure signature.
	inputParams, _ := requestMap["input"].(map[string]interface{})
	if !validateInputSignature(node, inputParams) {
		sendServiceError(backendChan, "invoke", requestId, "", StatusFailed,
			"400", "bad_request", "input does not conform to service signature")
		return
	}

	mu.Lock()
	state := getState(path)

	ts := getTimestamp()
	indataWrapped := map[string]interface{}{
		"input": inputParams,
		"ts":    ts,
	}

	// Synchronous response: the service starts as ONGOING when invoked.
	// A real implementation would call the underlying service here.
	state.status = StatusOngoing
	state.indata = indataWrapped
	state.outdata = nil

	// Determine filter variant to decide whether to start a monitoring session.
	filterVariant := extractFilterVariant(requestMap["filter"])
	var sessionId string
	if filterVariant != "none" {
		sessionId = generateServiceId()
		sess := &serviceSession{
			serviceId:   sessionId,
			path:        path,
			isInvoke:    true,
			routerIndex: extractRouterIndex(requestMap),
			indata:      indataWrapped,
			status:      StatusOngoing,
			startedAt:   time.Now(),
		}
		sessions[sessionId] = sess
	}
	mu.Unlock()

	response := map[string]interface{}{
		"action":    "invoke",
		"path":      path,
		"status":    string(StatusOngoing),
		"requestId": requestId,
		"ts":        ts,
	}
	if sessionId != "" {
		response["serviceId"] = sessionId
	}
	// outdata is omitted on initial invoke when service is starting.
	copyRouteFields(requestMap, response)
	backendChan <- response
}

// HandleMonitor processes a "monitor" action request.
func HandleMonitor(requestMap map[string]interface{}, backendChan chan map[string]interface{}) {
	path, _ := requestMap["path"].(string)
	requestId, _ := requestMap["requestId"].(string)

	node := utils.SetRootNodePointer(path)
	if node == nil || utils.VSSgetType(node) != utils.PROCEDURE {
		sendServiceError(backendChan, "monitor", requestId, "", StatusFailed,
			"400", "bad_request", "path must address a procedure node")
		return
	}

	mu.Lock()
	state := getState(path)
	currentStatus := state.status
	indataCopy := copyMap(state.indata)
	outdataCopy := copyMap(state.outdata)

	filterVariant := extractFilterVariant(requestMap["filter"])
	var sessionId string
	if currentStatus == StatusOngoing && filterVariant != "none" {
		sessionId = generateServiceId()
		sess := &serviceSession{
			serviceId:   sessionId,
			path:        path,
			isInvoke:    false,
			routerIndex: extractRouterIndex(requestMap),
			indata:      indataCopy,
			status:      StatusOngoing,
			startedAt:   time.Now(),
		}
		sessions[sessionId] = sess
	}
	mu.Unlock()

	ts := getTimestamp()
	response := map[string]interface{}{
		"action":    "monitor",
		"path":      path,
		"status":    string(currentStatus),
		"requestId": requestId,
		"ts":        ts,
	}
	if indataCopy != nil {
		response["indata"] = indataCopy
	}
	if outdataCopy != nil {
		response["outdata"] = outdataCopy
	}
	if sessionId != "" {
		response["serviceId"] = sessionId
	}
	copyRouteFields(requestMap, response)
	backendChan <- response
}

// HandleCancel processes a "cancel" action request.
func HandleCancel(requestMap map[string]interface{}, backendChan chan map[string]interface{}) {
	serviceId, _ := requestMap["serviceId"].(string)
	if serviceId == "" {
		sendServiceError(backendChan, "cancel", "", serviceId, StatusFailed,
			"400", "bad_request", "serviceId is required for cancel")
		return
	}

	mu.Lock()
	sess, ok := sessions[serviceId]
	if !ok {
		mu.Unlock()
		sendServiceError(backendChan, "cancel", "", serviceId, StatusFailed,
			"400", "bad_request", "serviceId not found")
		return
	}

	path := sess.path
	isInvoke := sess.isInvoke
	delete(sessions, serviceId)

	state := getState(path)
	outdataCopy := copyMap(state.outdata)
	if isInvoke {
		// Canceling the invoke session cancels the service execution.
		state.status = StatusCanceled
	}
	// Canceling a monitor session leaves service state unchanged.
	mu.Unlock()

	ts := getTimestamp()
	response := map[string]interface{}{
		"action":    "cancel",
		"status":    string(StatusCanceled),
		"serviceId": serviceId,
		"ts":        ts,
	}
	if outdataCopy != nil {
		response["outdata"] = outdataCopy
	}
	copyRouteFields(requestMap, response)
	backendChan <- response
}

// HandleDiscover processes a "discover" action request.
func HandleDiscover(requestMap map[string]interface{}, backendChan chan map[string]interface{}) {
	path, _ := requestMap["path"].(string)
	requestId, _ := requestMap["requestId"].(string)

	node := utils.SetRootNodePointer(path)
	if node == nil {
		sendServiceError(backendChan, "discover", requestId, "", StatusUnknown,
			"400", "bad_request", "path not found in service tree")
		return
	}

	nodeType := utils.VSSgetType(node)
	if nodeType != utils.BRANCH && nodeType != utils.PROCEDURE {
		sendServiceError(backendChan, "discover", requestId, "", StatusUnknown,
			"400", "bad_request", "path must address a branch or procedure node")
		return
	}

	// Build metadata JSON by walking the subtree.
	metadata := buildServiceMetadata(node, path)
	ts := getTimestamp()
	response := map[string]interface{}{
		"action":    "discover",
		"metadata":  metadata,
		"requestId": requestId,
		"ts":        ts,
	}
	copyRouteFields(requestMap, response)
	backendChan <- response
}

// UpdateServiceState is called by the underlying service implementation to report
// progress. It updates state and fans out monitoring events to subscribed sessions.
func UpdateServiceState(path string, status ServiceStatus, outdata map[string]interface{},
	backendChans []chan map[string]interface{}) {

	ts := getTimestamp()
	outdataWrapped := map[string]interface{}{
		"output": outdata,
		"ts":     ts,
	}

	mu.Lock()
	state := getState(path)
	state.status = status
	if outdata != nil {
		state.outdata = outdataWrapped
	}

	// Collect sessions watching this path.
	var toNotify []*serviceSession
	var toRemove []string
	for id, sess := range sessions {
		if sess.path == path {
			toNotify = append(toNotify, sess)
			if status != StatusOngoing {
				toRemove = append(toRemove, id)
			}
		}
	}
	for _, id := range toRemove {
		delete(sessions, id)
	}
	mu.Unlock()

	for _, sess := range toNotify {
		event := map[string]interface{}{
			"action":    "monitoring",
			"path":      path,
			"serviceId": sess.serviceId,
			"status":    string(status),
			"ts":        ts,
		}
		if outdata != nil {
			event["outdata"] = outdataWrapped
		}
		if sess.routerIndex < len(backendChans) {
			backendChans[sess.routerIndex] <- event
		}
	}
}

// buildServiceMetadata walks the subtree rooted at node and returns a map
// describing the service structure (procedure names and their I/O signatures).
func buildServiceMetadata(node *utils.Node_t, path string) map[string]interface{} {
	result := map[string]interface{}{}
	numChildren := utils.VSSgetNumOfChildren(node)
	for i := 0; i < numChildren; i++ {
		child := utils.VSSgetChild(node, i)
		if child == nil {
			continue
		}
		childName := utils.VSSgetName(child)
		childType := utils.VSSgetType(child)
		switch childType {
		case utils.PROCEDURE:
			result[childName] = buildProcedureMetadata(child)
		case utils.BRANCH:
			result[childName] = buildServiceMetadata(child, path+"."+childName)
		}
	}
	return result
}

// buildProcedureMetadata returns the I/O signature of a procedure node.
func buildProcedureMetadata(node *utils.Node_t) map[string]interface{} {
	meta := map[string]interface{}{"type": "procedure"}
	numChildren := utils.VSSgetNumOfChildren(node)
	for i := 0; i < numChildren; i++ {
		child := utils.VSSgetChild(node, i)
		if child == nil {
			continue
		}
		childName := utils.VSSgetName(child)
		childType := utils.VSSgetType(child)
		if childType == utils.IOSTRUCT {
			meta[childName] = buildIoStructMetadata(child)
		}
	}
	return meta
}

// buildIoStructMetadata returns the parameters of an Input or Output iostruct node.
func buildIoStructMetadata(node *utils.Node_t) map[string]interface{} {
	params := map[string]interface{}{}
	numChildren := utils.VSSgetNumOfChildren(node)
	for i := 0; i < numChildren; i++ {
		child := utils.VSSgetChild(node, i)
		if child == nil {
			continue
		}
		name := utils.VSSgetName(child)
		params[name] = map[string]interface{}{
			"type":     utils.VSSgetType(child),
			"datatype": utils.VSSgetDatatype(child),
		}
	}
	return params
}

// validateInputSignature checks that inputParams matches the Input children
// declared under the procedure node. Returns true if valid (or if the
// procedure has no Input iostruct).
func validateInputSignature(procedureNode *utils.Node_t, inputParams map[string]interface{}) bool {
	numChildren := utils.VSSgetNumOfChildren(procedureNode)
	for i := 0; i < numChildren; i++ {
		child := utils.VSSgetChild(procedureNode, i)
		if child == nil {
			continue
		}
		if utils.VSSgetName(child) == "Input" && utils.VSSgetType(child) == utils.IOSTRUCT {
			return validateIoParams(child, inputParams)
		}
	}
	return true // no Input iostruct means no input required
}

// validateIoParams checks that every declared parameter is present in params.
func validateIoParams(iostructNode *utils.Node_t, params map[string]interface{}) bool {
	numChildren := utils.VSSgetNumOfChildren(iostructNode)
	for i := 0; i < numChildren; i++ {
		child := utils.VSSgetChild(iostructNode, i)
		if child == nil {
			continue
		}
		paramName := utils.VSSgetName(child)
		if _, ok := params[paramName]; !ok {
			return false
		}
	}
	return true
}

// extractFilterVariant returns the "variant" string from a filter object.
// Defaults to "all" if absent or malformed.
func extractFilterVariant(filter interface{}) string {
	if filter == nil {
		return "all"
	}
	switch f := filter.(type) {
	case map[string]interface{}:
		if v, ok := f["variant"].(string); ok {
			return v
		}
	case string:
		var m map[string]interface{}
		if json.Unmarshal([]byte(f), &m) == nil {
			if v, ok := m["variant"].(string); ok {
				return v
			}
		}
	}
	return "all"
}

// extractRouterIndex retrieves the transport router index stashed in the request.
func extractRouterIndex(requestMap map[string]interface{}) int {
	if v, ok := requestMap["routerIndex"].(int); ok {
		return v
	}
	return 0
}

// copyRouteFields copies routing metadata (RouterId, etc.) needed by the transport
// manager to route the response back to the originating client.
func copyRouteFields(src, dst map[string]interface{}) {
	for _, k := range []string{"RouterId", "routerId", "routerIndex"} {
		if v, ok := src[k]; ok {
			dst[k] = v
		}
	}
}

// copyMap performs a shallow copy of a map. Returns nil if src is nil.
func copyMap(src map[string]interface{}) map[string]interface{} {
	if src == nil {
		return nil
	}
	dst := make(map[string]interface{}, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// sendServiceError builds and sends a service error response to backendChan.
func sendServiceError(backendChan chan map[string]interface{},
	action, requestId, serviceId string,
	status ServiceStatus,
	errNum, errReason, errDesc string) {

	errMap := map[string]interface{}{
		"action": action,
		"status": string(status),
		"error": map[string]interface{}{
			"number":      errNum,
			"reason":      errReason,
			"description": errDesc,
		},
		"ts": getTimestamp(),
	}
	if requestId != "" {
		errMap["requestId"] = requestId
	}
	if serviceId != "" {
		errMap["serviceId"] = serviceId
	}
	backendChan <- errMap
}
