/**
* (C) 2026 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE file in this repository.
*
* VISSv3.3-alpha Service Manager
*
* Handles the four service operations defined in VISSv3.2 SERVICES and
* extended by VISSv3.3:
*   invoke   – execute a service procedure (concurrent invocations supported)
*   monitor  – attach to an ongoing invocation
*   cancel   – cancel an invoke or monitor session
*   discover – retrieve service tree metadata (includes live service status)
*
* V3.3 additions over v3.2:
*   - Concurrent invocations: each invoke gets its own invocationState keyed
*     by serviceId; multiple calls to the same procedure can coexist.
*   - Per-invocation timeout watchdog: sessions that stay ONGOING past their
*     deadline receive a FAILED terminal event.
*   - Timebased filter: per-session ticker throttles monitoring events to
*     the requested period while always forwarding status-change events.
*   - Service registration: service processes connect via TCP and declare
*     the procedure paths they implement (see serviceReg.go).
*   - Structured error payload on FAILED: service processes may include an
*     error code and message; fans out in monitoring events.
*   - Authorization pass-through: client auth token forwarded to service.
*   - Discover enrichment: live serviceStatus and activeInvocations counts.
*   - SSE helper: FormatAsSSE encodes a monitoring event for HTTP streaming.
**/

package vissServiceMgr

import (
	"encoding/json"
	"fmt"
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

// DefaultTimeout is the maximum time an invocation may remain ONGOING before
// the server issues a FAILED terminal event. Overridable per-request via the
// "timeout" field (milliseconds).
const DefaultTimeout = 30 * time.Second

// ServiceError carries a structured error code and message on a FAILED update.
// It is included in monitoring events as {"error":{"code":"...","message":"..."}}
// (VISSv3.3 §20).
type ServiceError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// invocationState tracks one active procedure invocation.
type invocationState struct {
	serviceId string
	path      string
	status    ServiceStatus
	indata    map[string]interface{}
	outdata   map[string]interface{}
	startedAt time.Time
	deadline  time.Time
	cancelFn  func() // stops the timeout watchdog
}

// monitorSession represents one client watching an invocation.
type monitorSession struct {
	sessionId    string
	serviceId    string // which invocation is being watched
	path         string
	isInvoke     bool // true = session owner invoked; false = monitor-only
	routerIndex  int
	filterKind   string
	filterPeriod time.Duration // >0 for timebased
	lastEventAt  time.Time
	cancelTicker func() // stops the ticker goroutine, nil for non-timebased
}

var (
	mu sync.Mutex

	// invocations maps serviceId → invocationState.
	invocations = map[string]*invocationState{}

	// sessions maps sessionId → monitorSession.
	sessions = map[string]*monitorSession{}
)

// generateId produces a unique random numeric string.
func generateId() string {
	return strconv.Itoa(rand.Intn(900000) + 100000)
}

// getTimestamp returns the current time in RFC3339 format.
func getTimestamp() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// latestInvocationForPath returns the most recently started ONGOING invocation
// for path, or nil if none exists.
func latestInvocationForPath(path string) *invocationState {
	var latest *invocationState
	for _, inv := range invocations {
		if inv.path == path && inv.status == StatusOngoing {
			if latest == nil || inv.startedAt.After(latest.startedAt) {
				latest = inv
			}
		}
	}
	return latest
}

// startTimeoutWatchdog launches a goroutine that fires after deadline and
// terminates the invocation with FAILED if it is still ONGOING.
func startTimeoutWatchdog(inv *invocationState, backendChans []chan map[string]interface{}) func() {
	stopCh := make(chan struct{})
	go func() {
		remaining := time.Until(inv.deadline)
		if remaining <= 0 {
			remaining = time.Millisecond
		}
		select {
		case <-time.After(remaining):
			mu.Lock()
			current, ok := invocations[inv.serviceId]
			if !ok || current.status != StatusOngoing {
				mu.Unlock()
				return
			}
			mu.Unlock()
			UpdateServiceState(inv.serviceId, StatusFailed, nil, nil, backendChans)
		case <-stopCh:
		}
	}()
	return func() { close(stopCh) }
}

// startTimebasedTicker launches a goroutine that periodically pushes the
// current invocation state to the session's backend channel.
func startTimebasedTicker(sess *monitorSession, period time.Duration,
	backendChans []chan map[string]interface{}) func() {
	stopCh := make(chan struct{})
	go func() {
		ticker := time.NewTicker(period)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				mu.Lock()
				inv, ok := invocations[sess.serviceId]
				if !ok {
					mu.Unlock()
					return
				}
				event := map[string]interface{}{
					"action":    "monitoring",
					"path":      sess.path,
					"serviceId": sess.sessionId,
					"status":    string(inv.status),
					"ts":        getTimestamp(),
				}
				if inv.outdata != nil {
					event["outdata"] = copyMap(inv.outdata)
				}
				mu.Unlock()
				if sess.routerIndex < len(backendChans) {
					backendChans[sess.routerIndex] <- event
				}
				if inv.status != StatusOngoing {
					return
				}
			case <-stopCh:
				return
			}
		}
	}()
	return func() { close(stopCh) }
}

// HandleInvoke processes an "invoke" action request per VISSv3.2 §6.1 /
// VISSv3.3 §10 (concurrent invocations).
func HandleInvoke(requestMap map[string]interface{}, backendChans []chan map[string]interface{}) {
	path, _ := requestMap["path"].(string)
	requestId, _ := requestMap["requestId"].(string)
	tDChanIndex := extractRouterIndex(requestMap)
	bc := backendChans[tDChanIndex]

	node := utils.SetRootNodePointer(path)
	if node == nil || utils.VSSgetType(node) != utils.PROCEDURE {
		sendServiceError(bc, "invoke", requestId, "", StatusFailed,
			"400", "bad_request", "path must address a procedure node")
		return
	}

	inputParams, _ := requestMap["input"].(map[string]interface{})
	if !validateInputSignature(node, inputParams) {
		sendServiceError(bc, "invoke", requestId, "", StatusFailed,
			"400", "bad_request", "input does not conform to service signature")
		return
	}

	authToken, _ := requestMap["authorization"].(string)

	deadline := time.Now().Add(timeoutFromRequest(requestMap))

	mu.Lock()
	ts := getTimestamp()
	indataWrapped := map[string]interface{}{"input": inputParams, "ts": ts}

	serviceId := generateId()
	inv := &invocationState{
		serviceId: serviceId,
		path:      path,
		status:    StatusOngoing,
		indata:    indataWrapped,
		startedAt: time.Now(),
		deadline:  deadline,
	}
	invocations[serviceId] = inv

	filterVariant := extractFilterVariant(requestMap["filter"])
	var sessionId string
	if filterVariant != "none" {
		sessionId = generateId()
		sess := &monitorSession{
			sessionId:   sessionId,
			serviceId:   serviceId,
			path:        path,
			isInvoke:    true,
			routerIndex: tDChanIndex,
			filterKind:  filterVariant,
		}
		if filterVariant == "timebased" {
			period := periodFromFilter(requestMap["filter"])
			sess.filterPeriod = period
			sess.cancelTicker = startTimebasedTicker(sess, period, backendChans)
		}
		sessions[sessionId] = sess
	}
	inv.cancelFn = startTimeoutWatchdog(inv, backendChans)
	mu.Unlock()

	forwardInvokeToService(path, serviceId, inputParams, authToken)

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
	copyRouteFields(requestMap, response)
	bc <- response
}

// HandleMonitor processes a "monitor" action request per VISSv3.2 §6.2.
// Attaches to the most recent ONGOING invocation for path; if none, returns
// the last known state without starting a monitoring session.
func HandleMonitor(requestMap map[string]interface{}, backendChans []chan map[string]interface{}) {
	path, _ := requestMap["path"].(string)
	requestId, _ := requestMap["requestId"].(string)
	tDChanIndex := extractRouterIndex(requestMap)
	bc := backendChans[tDChanIndex]

	node := utils.SetRootNodePointer(path)
	if node == nil || utils.VSSgetType(node) != utils.PROCEDURE {
		sendServiceError(bc, "monitor", requestId, "", StatusFailed,
			"400", "bad_request", "path must address a procedure node")
		return
	}

	mu.Lock()
	inv := latestInvocationForPath(path)

	var currentStatus ServiceStatus
	var indataCopy, outdataCopy map[string]interface{}
	var watchedServiceId string

	if inv != nil {
		currentStatus = inv.status
		indataCopy = copyMap(inv.indata)
		outdataCopy = copyMap(inv.outdata)
		watchedServiceId = inv.serviceId
	} else {
		currentStatus = StatusUnknown
	}

	filterVariant := extractFilterVariant(requestMap["filter"])
	var sessionId string
	if inv != nil && currentStatus == StatusOngoing && filterVariant != "none" {
		sessionId = generateId()
		sess := &monitorSession{
			sessionId:   sessionId,
			serviceId:   watchedServiceId,
			path:        path,
			isInvoke:    false,
			routerIndex: tDChanIndex,
			filterKind:  filterVariant,
		}
		if filterVariant == "timebased" {
			period := periodFromFilter(requestMap["filter"])
			sess.filterPeriod = period
			sess.cancelTicker = startTimebasedTicker(sess, period, backendChans)
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
	bc <- response
}

// HandleCancel processes a "cancel" action per VISSv3.2 §6.3.
// If the sessionId was from an Invoke session, the invocation is cancelled.
// If from a Monitor session, only the monitoring is cancelled.
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

	if sess.cancelTicker != nil {
		sess.cancelTicker()
	}
	delete(sessions, serviceId)

	var outdataCopy map[string]interface{}
	if sess.isInvoke {
		inv, invOk := invocations[sess.serviceId]
		if invOk {
			if inv.cancelFn != nil {
				inv.cancelFn()
			}
			outdataCopy = copyMap(inv.outdata)
			inv.status = StatusCanceled
			// Remove all other sessions watching this invocation.
			for id, s := range sessions {
				if s.serviceId == sess.serviceId {
					if s.cancelTicker != nil {
						s.cancelTicker()
					}
					delete(sessions, id)
				}
			}
			delete(invocations, sess.serviceId)
		}
	}
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

// HandleDiscover processes a "discover" action per VISSv3.2 §6.4.
// The response includes live serviceStatus and activeInvocations for each
// procedure node (VISSv3.3 §8 / §24).
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

// UpdateServiceState is called by a registered service process (via
// serviceReg.go) to report execution progress. It updates the invocation
// state and fans out monitoring events to all watching sessions, respecting
// each session's filter settings.
//
// svcErr, when non-nil, is included in monitoring events as
// {"error":{"code":"...","message":"..."}} (VISSv3.3 §20).
func UpdateServiceState(serviceId string, status ServiceStatus,
	outdata map[string]interface{}, svcErr *ServiceError,
	backendChans []chan map[string]interface{}) {

	ts := getTimestamp()
	var outdataWrapped map[string]interface{}
	if outdata != nil {
		outdataWrapped = map[string]interface{}{"output": outdata, "ts": ts}
	}

	mu.Lock()
	inv, ok := invocations[serviceId]
	if !ok {
		mu.Unlock()
		return
	}
	prevStatus := inv.status
	inv.status = status
	if outdataWrapped != nil {
		inv.outdata = outdataWrapped
	}

	statusChanged := prevStatus != status

	type eventTarget struct {
		sess          *monitorSession
		shouldDeliver bool
	}
	var targets []eventTarget
	var toRemove []string
	for id, sess := range sessions {
		if sess.serviceId != serviceId {
			continue
		}
		deliver := false
		switch sess.filterKind {
		case "status":
			deliver = statusChanged
		case "all":
			deliver = true
		case "timebased":
			// timebased ticker handles delivery; only deliver here on status change.
			deliver = statusChanged
		case "none":
			deliver = false
		default:
			deliver = true
		}
		targets = append(targets, eventTarget{sess: sess, shouldDeliver: deliver})
		if status != StatusOngoing {
			if sess.cancelTicker != nil {
				sess.cancelTicker()
			}
			toRemove = append(toRemove, id)
		}
	}
	for _, id := range toRemove {
		delete(sessions, id)
	}
	if status != StatusOngoing {
		if inv.cancelFn != nil {
			inv.cancelFn()
		}
		delete(invocations, serviceId)
	}
	mu.Unlock()

	for _, t := range targets {
		if !t.shouldDeliver {
			continue
		}
		event := map[string]interface{}{
			"action":    "monitoring",
			"path":      t.sess.path,
			"serviceId": t.sess.sessionId,
			"status":    string(status),
			"ts":        ts,
		}
		if outdataWrapped != nil {
			event["outdata"] = outdataWrapped
		}
		if svcErr != nil {
			event["error"] = map[string]interface{}{
				"code":    svcErr.Code,
				"message": svcErr.Message,
			}
		}
		if t.sess.routerIndex < len(backendChans) {
			backendChans[t.sess.routerIndex] <- event
		}
	}
}

// FormatAsSSE encodes a monitoring event as a Server-Sent Events data frame
// for use in HTTP streaming responses (VISSv3.3 §23).
// The returned string is ready to write directly to an http.ResponseWriter.
func FormatAsSSE(event map[string]interface{}) (string, error) {
	b, err := json.Marshal(event)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("data: %s\n\n", b), nil
}

// ---- tree helpers ----------------------------------------------------------

// buildServiceMetadata walks the HIM tree rooted at node, returning a metadata
// map. basePath is the dot-separated path of node in the service tree (used to
// look up live registration and invocation status per procedure).
func buildServiceMetadata(node *utils.Node_t, basePath string) map[string]interface{} {
	result := map[string]interface{}{}
	numChildren := utils.VSSgetNumOfChildren(node)
	for i := 0; i < numChildren; i++ {
		child := utils.VSSgetChild(node, i)
		if child == nil {
			continue
		}
		childName := utils.VSSgetName(child)
		childPath := basePath + "." + childName
		switch utils.VSSgetType(child) {
		case utils.PROCEDURE:
			result[childName] = buildProcedureMetadata(child, childPath)
		case utils.BRANCH:
			result[childName] = buildServiceMetadata(child, childPath)
		}
	}
	return result
}

// buildProcedureMetadata returns HIM metadata for a procedure node, enriched
// with live serviceStatus ("registered" | "disconnected") and activeInvocations
// count (VISSv3.3 §24).
func buildProcedureMetadata(node *utils.Node_t, path string) map[string]interface{} {
	meta := map[string]interface{}{"type": "procedure"}
	numChildren := utils.VSSgetNumOfChildren(node)
	for i := 0; i < numChildren; i++ {
		child := utils.VSSgetChild(node, i)
		if child == nil {
			continue
		}
		if utils.VSSgetType(child) == utils.IOSTRUCT {
			meta[utils.VSSgetName(child)] = buildIoStructMetadata(child)
		}
	}

	// Live service status from registrations (serviceReg.go, same package).
	regMu.Lock()
	_, connected := registrations[path]
	regMu.Unlock()
	if connected {
		meta["serviceStatus"] = "registered"
	} else {
		meta["serviceStatus"] = "disconnected"
	}

	// Count ONGOING invocations for this path.
	mu.Lock()
	count := 0
	for _, inv := range invocations {
		if inv.path == path && inv.status == StatusOngoing {
			count++
		}
	}
	mu.Unlock()
	meta["activeInvocations"] = count

	return meta
}

func buildIoStructMetadata(node *utils.Node_t) map[string]interface{} {
	params := map[string]interface{}{}
	numChildren := utils.VSSgetNumOfChildren(node)
	for i := 0; i < numChildren; i++ {
		child := utils.VSSgetChild(node, i)
		if child == nil {
			continue
		}
		params[utils.VSSgetName(child)] = map[string]interface{}{
			"type":     utils.VSSgetType(child),
			"datatype": utils.VSSgetDatatype(child),
		}
	}
	return params
}

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
	return true
}

func validateIoParams(iostructNode *utils.Node_t, params map[string]interface{}) bool {
	numChildren := utils.VSSgetNumOfChildren(iostructNode)
	for i := 0; i < numChildren; i++ {
		child := utils.VSSgetChild(iostructNode, i)
		if child == nil {
			continue
		}
		if _, ok := params[utils.VSSgetName(child)]; !ok {
			return false
		}
	}
	return true
}

// ---- filter helpers --------------------------------------------------------

func extractFilterVariant(filter interface{}) string {
	m := filterToMap(filter)
	if m == nil {
		return "all"
	}
	if v, ok := m["variant"].(string); ok {
		return v
	}
	return "all"
}

func periodFromFilter(filter interface{}) time.Duration {
	m := filterToMap(filter)
	if m == nil {
		return time.Second
	}
	param, _ := m["parameter"].(map[string]interface{})
	if param == nil {
		return time.Second
	}
	periodStr, _ := param["period"].(string)
	ms, err := strconv.Atoi(periodStr)
	if err != nil || ms <= 0 {
		return time.Second
	}
	return time.Duration(ms) * time.Millisecond
}

func filterToMap(filter interface{}) map[string]interface{} {
	switch f := filter.(type) {
	case map[string]interface{}:
		return f
	case string:
		var m map[string]interface{}
		if json.Unmarshal([]byte(f), &m) == nil {
			return m
		}
	}
	return nil
}

// timeoutFromRequest reads the optional "timeout" key (milliseconds) from
// the request map. Falls back to DefaultTimeout.
func timeoutFromRequest(requestMap map[string]interface{}) time.Duration {
	switch v := requestMap["timeout"].(type) {
	case float64:
		if v > 0 {
			return time.Duration(v) * time.Millisecond
		}
	case string:
		if ms, err := strconv.Atoi(v); err == nil && ms > 0 {
			return time.Duration(ms) * time.Millisecond
		}
	}
	return DefaultTimeout
}

// ---- routing helpers -------------------------------------------------------

func extractRouterIndex(requestMap map[string]interface{}) int {
	if v, ok := requestMap["routerIndex"].(int); ok {
		return v
	}
	return 0
}

func copyRouteFields(src, dst map[string]interface{}) {
	for _, k := range []string{"RouterId", "routerId", "routerIndex"} {
		if v, ok := src[k]; ok {
			dst[k] = v
		}
	}
}

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

func sendServiceError(backendChan chan map[string]interface{},
	action, requestId, serviceId string,
	status ServiceStatus, errNum, errReason, errDesc string) {

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
