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
	"strings"
	"sync"
	"time"

	"github.com/covesa/vissr/utils"
)

// maxServiceNodes caps the result set when resolving a service path. A service
// request addresses a single procedure (or, for discover, a single branch), so
// a small cap is sufficient.
const maxServiceNodes = 50

// resolveServiceNode walks the HIM forest to the node addressed by the full
// dot-delimited path and returns it, or nil if the path does not resolve to
// exactly one node.
//
// utils.SetRootNodePointer only returns the *tree root* (it matches on the
// first path segment), so it cannot address a procedure node deeper in the
// tree. Callers that need the addressed node (invoke/monitor/discover) must
// walk the full path from the root — using SetRootNodePointer alone made every
// multi-segment service path resolve to the root branch, which then failed the
// "must address a procedure node" check.
func resolveServiceNode(path string) *utils.Node_t {
	root := utils.SetRootNodePointer(path)
	if root == nil {
		return nil
	}
	// VSSsearchNodes (with leafNodesOnly=false) records every node along the
	// matched path — root, intermediate branches, and the addressed node — so
	// we pick the entry whose full path equals the request path exactly. This
	// resolves both procedure targets (invoke/monitor) and branch targets
	// (discover), unlike SetRootNodePointer which only ever returns the root.
	searchData, matches := utils.VSSsearchNodes(path, root, maxServiceNodes, true, false, 0, nil, nil)
	for i := 0; i < matches; i++ {
		if searchData[i].NodePath == path {
			return searchData[i].NodeHandle
		}
	}
	return nil
}

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
	progress  *int   // latest progress percentage 0-100 (§28); nil until first report
}

// monitorSession represents one client watching an invocation.
type monitorSession struct {
	sessionId    string
	serviceId    string // which invocation is being watched
	path         string
	isInvoke     bool   // true = session owner invoked; false = monitor-only
	routerIndex  int    // transport-manager channel index (which transport)
	routerId     string // originating "mgrId?clientId" (which client within it)
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

// pathMetrics accumulates per-path invocation statistics (VISSv3.3 §31).
type pathMetrics struct {
	total      int64
	successes  int64
	cancels    int64
	failures   int64
	totalDurMs int64
}

var (
	metricsMu sync.Mutex
	metrics   = map[string]*pathMetrics{}
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
			UpdateServiceState(inv.serviceId, StatusFailed, nil, nil, nil, backendChans)
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
				if sess.routerId != "" {
					event["RouterId"] = sess.routerId // address the event back to the requesting client
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
	if tDChanIndex < 0 || tDChanIndex >= len(backendChans) {
		utils.Error.Printf("vissServiceMgr: HandleInvoke: routerIndex %d out of range (%d chans)", tDChanIndex, len(backendChans))
		return
	}
	bc := backendChans[tDChanIndex]

	node := resolveServiceNode(path)
	if node == nil || utils.VSSgetType(node) != utils.PROCEDURE {
		sendServiceError(bc, "invoke", requestId, "", StatusFailed,
			"400", "bad_request", "path must address a procedure node", requestMap)
		return
	}

	inputParams, _ := requestMap["input"].(map[string]interface{})
	if ok, missingFields := validateInputSignature(node, inputParams); !ok {
		sendValidationError(bc, "invoke", requestId, missingFields, requestMap)
		return
	}

	authToken, _ := requestMap["authorization"].(string)

	// Built-in (in-process) simulation: used only when no external service
	// process has registered for this path (instance paths resolve too). It
	// lets the demo run without a separate service binary and, crucially, makes
	// the invocation actually terminate instead of emitting content-less
	// ONGOING events until the timeout watchdog fires.
	var builtinRun func(string, []chan map[string]interface{})
	var builtinMinDuration time.Duration
	if resolveRegistration(path) == nil {
		if bh, ok := builtinServices[procedureName(path)]; ok {
			decision := bh(path, inputParams)
			switch {
			case decision.errNum != "":
				sendServiceError(bc, "invoke", requestId, "", StatusFailed,
					decision.errNum, decision.errReason, decision.errDesc, requestMap)
				return
			case decision.immediate != "":
				ts := getTimestamp()
				response := map[string]interface{}{
					"action":    "invoke",
					"path":      path,
					"status":    string(decision.immediate),
					"requestId": requestId,
					"ts":        ts,
				}
				if decision.outdata != nil {
					response["outdata"] = map[string]interface{}{"output": decision.outdata, "ts": ts}
				}
				copyRouteFields(requestMap, response)
				bc <- response
				return
			default:
				builtinRun = decision.run
				builtinMinDuration = decision.minDuration
			}
		}
	}

	timeout := timeoutFromRequest(requestMap)
	if builtinMinDuration > timeout {
		timeout = builtinMinDuration
	}
	deadline := time.Now().Add(timeout)

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
			routerId:    extractRouterId(requestMap),
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

	if builtinRun != nil {
		go builtinRun(serviceId, backendChans)
	} else {
		forwardInvokeToService(path, serviceId, inputParams, authToken)
	}

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
	if tDChanIndex < 0 || tDChanIndex >= len(backendChans) {
		utils.Error.Printf("vissServiceMgr: HandleMonitor: routerIndex %d out of range (%d chans)", tDChanIndex, len(backendChans))
		return
	}
	bc := backendChans[tDChanIndex]

	node := resolveServiceNode(path)
	if node == nil || utils.VSSgetType(node) != utils.PROCEDURE {
		sendServiceError(bc, "monitor", requestId, "", StatusFailed,
			"400", "bad_request", "path must address a procedure node", requestMap)
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
			routerId:    extractRouterId(requestMap),
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
			"400", "bad_request", "serviceId is required for cancel", requestMap)
		return
	}

	mu.Lock()
	sess, ok := sessions[serviceId]
	if !ok {
		mu.Unlock()
		sendServiceError(backendChan, "cancel", "", serviceId, StatusFailed,
			"400", "bad_request", "serviceId not found", requestMap)
		return
	}

	if sess.cancelTicker != nil {
		sess.cancelTicker()
	}
	delete(sessions, serviceId)

	var outdataCopy map[string]interface{}
	var cancelPath, cancelInvId string
	if sess.isInvoke {
		inv, invOk := invocations[sess.serviceId]
		if invOk {
			if inv.cancelFn != nil {
				inv.cancelFn()
			}
			outdataCopy = copyMap(inv.outdata)
			cancelPath = inv.path
			cancelInvId = inv.serviceId
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

	// Forward cancel to the service process so it can stop cleanly (VISSv3.3 §26).
	if cancelPath != "" {
		forwardCancelToService(cancelPath, cancelInvId)
	}

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
// procedure node (VISSv3.3 §25).
func HandleDiscover(requestMap map[string]interface{}, backendChan chan map[string]interface{}) {
	path, _ := requestMap["path"].(string)
	requestId, _ := requestMap["requestId"].(string)

	node := resolveServiceNode(path)
	if node == nil {
		sendServiceError(backendChan, "discover", requestId, "", StatusUnknown,
			"400", "bad_request", "path not found in service tree", requestMap)
		return
	}

	nodeType := utils.VSSgetType(node)
	if nodeType != utils.BRANCH && nodeType != utils.PROCEDURE {
		sendServiceError(backendChan, "discover", requestId, "", StatusUnknown,
			"400", "bad_request", "path must address a branch or procedure node", requestMap)
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
//
// progress, when non-nil, stores the completion percentage (0-100) and is
// included in ONGOING monitoring events (VISSv3.3 §28).
func UpdateServiceState(serviceId string, status ServiceStatus,
	outdata map[string]interface{}, svcErr *ServiceError, progress *int,
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
	if progress != nil {
		inv.progress = progress
	}

	// Snapshot progress and terminal-status data before releasing the lock.
	var progressVal *int
	if inv.progress != nil {
		v := *inv.progress
		progressVal = &v
	}
	var termPath string
	var termDur time.Duration
	if status != StatusOngoing {
		termPath = inv.path
		termDur = time.Since(inv.startedAt)
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

	// Update per-path observability counters for terminal transitions (§31).
	if termPath != "" {
		metricsMu.Lock()
		pm := metrics[termPath]
		if pm == nil {
			pm = &pathMetrics{}
			metrics[termPath] = pm
		}
		pm.total++
		pm.totalDurMs += termDur.Milliseconds()
		switch status {
		case StatusSuccessful:
			pm.successes++
		case StatusCanceled:
			pm.cancels++
		case StatusFailed:
			pm.failures++
		}
		metricsMu.Unlock()
	}

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
		if t.sess.routerId != "" {
			event["RouterId"] = t.sess.routerId // address the event back to the requesting client
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
		// Include progress percentage in ONGOING events only (§28).
		if status == StatusOngoing && progressVal != nil {
			event["progress"] = *progressVal
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

	// Snapshot registration, version, and health fields under regMu.
	// sc.mu is taken briefly while regMu is held; no other code takes regMu
	// while holding sc.mu, so there is no deadlock risk.
	regMu.Lock()
	sc := registrations[path]
	var connected bool
	var version, healthDetail string
	var healthy bool
	var healthUpdatedAt time.Time
	if sc != nil {
		connected = true
		version = sc.version
		sc.mu.Lock()
		healthy = sc.healthy
		healthDetail = sc.healthDetail
		healthUpdatedAt = sc.healthUpdatedAt
		sc.mu.Unlock()
	}
	regMu.Unlock()

	if connected {
		meta["serviceStatus"] = "registered"
		if version != "" {
			meta["version"] = version
		}
		if !healthUpdatedAt.IsZero() {
			meta["serviceHealth"] = map[string]interface{}{
				"healthy":   healthy,
				"detail":    healthDetail,
				"updatedAt": healthUpdatedAt.UTC().Format(time.RFC3339),
			}
		}
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

	// Observability counters (§31).
	metricsMu.Lock()
	pm := metrics[path]
	var pmTotal, pmSuccesses, pmTotalDurMs int64
	if pm != nil {
		pmTotal = pm.total
		pmSuccesses = pm.successes
		pmTotalDurMs = pm.totalDurMs
	}
	metricsMu.Unlock()
	meta["totalInvocations"] = pmTotal
	if pmTotal > 0 {
		meta["successRate"] = float64(pmSuccesses) / float64(pmTotal)
		meta["avgDurationMs"] = pmTotalDurMs / pmTotal
	}

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

// validateInputSignature checks that all required Input fields are present.
// Returns (true, nil) when valid; (false, missingFields) when fields are absent.
func validateInputSignature(procedureNode *utils.Node_t, inputParams map[string]interface{}) (bool, []string) {
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
	return true, nil // no Input iostruct means no input required
}

func validateIoParams(iostructNode *utils.Node_t, params map[string]interface{}) (bool, []string) {
	var missing []string
	numChildren := utils.VSSgetNumOfChildren(iostructNode)
	for i := 0; i < numChildren; i++ {
		child := utils.VSSgetChild(iostructNode, i)
		if child == nil {
			continue
		}
		name := utils.VSSgetName(child)
		if _, ok := params[name]; ok {
			continue
		}
		if isOptionalParam(child) {
			continue // optional parameters may be omitted (e.g. MoveSeat.Credentials)
		}
		missing = append(missing, name)
	}
	return len(missing) == 0, missing
}

// isOptionalParam reports whether an Input/Output parameter node is optional.
//
// The HIM Node_t model has no structured "optional" flag, so optionality is
// currently only expressed in the node description prose (the COVESA HIM
// service example marks MoveSeat.Credentials as "Optional parameter."). Until a
// structured directive (e.g. @optional) is added to the vspec/HIM tooling and a
// corresponding Node_t field, we honour that convention so a request omitting
// an optional parameter is not rejected as missing a required field.
func isOptionalParam(node *utils.Node_t) bool {
	return strings.Contains(strings.ToLower(utils.VSSgetDescr(node)), "optional")
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

// extractRouterId returns the originating "mgrId?clientId" RouterId string from
// a request so that asynchronous monitoring events can be addressed back to the
// requesting client. Without it the transport managers cannot recover a
// clientId and (post-fix) drop the event. Returns "" when absent.
func extractRouterId(requestMap map[string]interface{}) string {
	for _, k := range []string{"RouterId", "routerId"} {
		if v, ok := requestMap[k].(string); ok {
			return v
		}
	}
	return ""
}

// copyRouteFields copies the client-addressing RouterId from a request onto a
// response so the transport manager can route it back and then strip it
// (RemoveInternalData). It deliberately does NOT copy "routerIndex": that is a
// server-internal transport-channel index injected by serveRequest and read
// only from the request (extractRouterIndex). Copying it onto the response
// leaked it to clients, who would receive e.g. "routerIndex":1 in an invoke
// reply. No transport manager strips routerIndex, so it must never be added.
func copyRouteFields(src, dst map[string]interface{}) {
	for _, k := range []string{"RouterId", "routerId"} {
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

// sendValidationError sends a 400 error that lists the missing input field
// names, providing callers with actionable detail (VISSv3.3 §29).
//
// requestMap is the originating request: its RouterId is copied onto the error
// so the transport manager can route the reply back to the requesting client.
// Without it the WS/UDS managers recover clientId=-1 and drop the response,
// wedging the client's synchronous request/response channel.
func sendValidationError(backendChan chan map[string]interface{},
	action, requestId string, missingFields []string,
	requestMap map[string]interface{}) {

	errMap := map[string]interface{}{
		"action": action,
		"status": string(StatusFailed),
		"error": map[string]interface{}{
			"number":      "400",
			"reason":      "bad_request",
			"description": "input does not conform to service signature",
			"fields":      missingFields,
		},
		"ts": getTimestamp(),
	}
	if requestId != "" {
		errMap["requestId"] = requestId
	}
	copyRouteFields(requestMap, errMap)
	backendChan <- errMap
}

// sendServiceError sends a structured error response. As with
// sendValidationError, requestMap supplies the RouterId so the reply can be
// addressed back to the originating client rather than dropped.
func sendServiceError(backendChan chan map[string]interface{},
	action, requestId, serviceId string,
	status ServiceStatus, errNum, errReason, errDesc string,
	requestMap map[string]interface{}) {

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
	copyRouteFields(requestMap, errMap)
	backendChan <- errMap
}
