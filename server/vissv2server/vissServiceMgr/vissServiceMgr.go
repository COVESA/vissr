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
	"sort"
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
//
// resources holds the concrete resource-instance keys addressed by this
// invocation (e.g. ["Row1.DriverSide", "Row1.PassengerSide"]), resolved once
// at HandleInvoke time via resolveResourceInstances. It is nil/empty for a
// single-resource procedure (e.g. GetCapabilities, or a multiplexed procedure
// invoked with a resource filter matching exactly one instance) — outdata is
// used as before in that case. When len(resources) > 1, outdataByResource and
// resourceStatus are used instead (see UpdateServiceState).
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

	resources         []string                          // resource keys addressed by this invocation; nil for single-resource
	outdataByResource map[string]map[string]interface{} // resource key -> {"output":...,"ts":...}; used when len(resources) > 1
	resourceStatus    map[string]ServiceStatus          // resource key -> latest reported status; used when len(resources) > 1
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
	hasOutput    bool   // true if the addressed procedure declares an Output iostruct (outdata is then mandatory on every event, per VISSv3.2 §monitorEvent)
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
			UpdateServiceState(inv.serviceId, StatusFailed, "", nil, nil, nil, backendChans)
		case <-stopCh:
		}
	}()
	return func() { close(stopCh) }
}

// startTimebasedTicker launches a goroutine that periodically pushes the
// current invocation state to the session's backend channel.
//
// If sess.hasOutput is true (the addressed procedure declares an Output
// iostruct, e.g. MoveSeat), a tick is skipped — no event sent — while the
// invocation has not yet reported any output. Per VISSv3.2 §monitorEvent,
// "outdata ... Yes (*) - If it is not specified in the service signature then
// the parameter shall be omitted": outdata is therefore mandatory on every
// emitted event for such a procedure, but previously the ticker would emit
// an outdata-less event anyway whenever its period was shorter than the
// driver's own update rate, so the first tick(s) fired before any
// UpdateServiceState call had landed. Skipping keeps every emitted event
// spec-compliant; the session still gets prompt notice of status changes via
// the status-changed delivery path in UpdateServiceState, and later ticks
// resume once output becomes available. Procedures with no Output at all
// (sess.hasOutput false) are unaffected and keep emitting on every tick.
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
				var outdataField interface{}
				if len(inv.resources) > 1 {
					if arr := buildOutdataArrayFromWrapped(inv.outdataByResource); len(arr) > 0 {
						outdataField = arr
					}
				} else if inv.outdata != nil {
					outdataField = copyMap(inv.outdata)
				}
				if outdataField == nil && sess.hasOutput {
					// The procedure's signature requires outdata but none has
					// been reported yet: sending now would omit the mandatory
					// field, so wait for the next tick.
					mu.Unlock()
					continue
				}
				event := map[string]interface{}{
					"action":    "monitoring",
					"path":      sess.path,
					"serviceId": sess.sessionId,
					"status":    string(inv.status),
					"ts":        getTimestamp(),
				}
				if outdataField != nil {
					event["outdata"] = outdataField
				}
				if sess.routerId != "" {
					event["RouterId"] = sess.routerId // address the event back to the requesting client
				}
				status := inv.status
				mu.Unlock()
				if sess.routerIndex < len(backendChans) {
					backendChans[sess.routerIndex] <- event
				}
				if status != StatusOngoing {
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

	pf, filterErr := parseServiceFilter(requestMap["filter"])
	if filterErr != "" {
		sendServiceError(bc, "invoke", requestId, "", StatusFailed,
			"400", "bad_request", filterErr, requestMap)
		return
	}
	resourceKeys, resErr := resolveResourceInstances(node, pf.resourceSnips)
	if resErr != "" {
		sendServiceError(bc, "invoke", requestId, "", StatusFailed,
			"400", "bad_request", resErr, requestMap)
		return
	}

	inputParams, _ := requestMap["input"].(map[string]interface{})
	if ok, missingFields := validateInputSignature(node, resourceKeys, inputParams); !ok {
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
			decision := bh(path, resourceKeys, inputParams)
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
				if decision.outdataByResource != nil {
					response["outdata"] = wrapOutdataByResource(decision.outdataByResource, ts)
				} else if decision.outdata != nil {
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
		resources: resourceKeys,
	}
	if len(resourceKeys) > 1 {
		inv.outdataByResource = map[string]map[string]interface{}{}
		inv.resourceStatus = map[string]ServiceStatus{}
		for _, rk := range resourceKeys {
			inv.resourceStatus[rk] = StatusOngoing
		}
	}
	invocations[serviceId] = inv

	var sessionId string
	if pf.variant != "none" {
		sessionId = generateId()
		sess := &monitorSession{
			sessionId:   sessionId,
			serviceId:   serviceId,
			path:        path,
			isInvoke:    true,
			routerIndex: tDChanIndex,
			routerId:    extractRouterId(requestMap),
			filterKind:  pf.variant,
			hasOutput:   procedureHasOutput(node, resourceKeys),
		}
		if pf.variant == "timebased" {
			sess.filterPeriod = pf.period
			sess.cancelTicker = startTimebasedTicker(sess, pf.period, backendChans)
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

// wrapOutdataByResource builds the multi-resource outdata array shape used by
// the immediate-invoke response: [{"Row1.DriverSide": {"output":...,"ts":...}}, ...],
// sorted by resource key for deterministic ordering (needed for stable test
// assertions). byResource holds the *raw* (unwrapped) output map per resource
// key; ts is applied uniformly to every entry.
func wrapOutdataByResource(byResource map[string]map[string]interface{}, ts string) []map[string]interface{} {
	keys := make([]string, 0, len(byResource))
	for k := range byResource {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]map[string]interface{}, 0, len(keys))
	for _, k := range keys {
		wrapped := map[string]interface{}{"output": byResource[k], "ts": ts}
		out = append(out, map[string]interface{}{k: wrapped})
	}
	return out
}

// buildOutdataArrayFromWrapped builds the multi-resource outdata array shape
// from a map of *already-wrapped* per-resource outdata (each value already
// shaped {"output":...,"ts":...}, as stored in invocationState.outdataByResource
// by UpdateServiceState), sorted by resource key for deterministic ordering.
func buildOutdataArrayFromWrapped(byResource map[string]map[string]interface{}) []map[string]interface{} {
	keys := make([]string, 0, len(byResource))
	for k := range byResource {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]map[string]interface{}, 0, len(keys))
	for _, k := range keys {
		out = append(out, map[string]interface{}{k: copyMap(byResource[k])})
	}
	return out
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

	pf, filterErr := parseServiceFilter(requestMap["filter"])
	if filterErr != "" {
		sendServiceError(bc, "monitor", requestId, "", StatusFailed,
			"400", "bad_request", filterErr, requestMap)
		return
	}

	mu.Lock()
	inv := latestInvocationForPath(path)

	var currentStatus ServiceStatus
	var indataCopy, outdataCopy map[string]interface{}
	var outdataArr []map[string]interface{}
	var watchedServiceId string

	if inv != nil {
		currentStatus = inv.status
		indataCopy = copyMap(inv.indata)
		if len(inv.resources) > 1 {
			outdataArr = buildOutdataArrayFromWrapped(inv.outdataByResource)
		} else {
			outdataCopy = copyMap(inv.outdata)
		}
		watchedServiceId = inv.serviceId
	} else {
		currentStatus = StatusUnknown
	}

	var sessionId string
	if inv != nil && currentStatus == StatusOngoing && pf.variant != "none" {
		sessionId = generateId()
		sess := &monitorSession{
			sessionId:   sessionId,
			serviceId:   watchedServiceId,
			path:        path,
			isInvoke:    false,
			routerIndex: tDChanIndex,
			routerId:    extractRouterId(requestMap),
			filterKind:  pf.variant,
			hasOutput:   procedureHasOutput(node, inv.resources),
		}
		if pf.variant == "timebased" {
			sess.filterPeriod = pf.period
			sess.cancelTicker = startTimebasedTicker(sess, pf.period, backendChans)
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
	if outdataArr != nil {
		response["outdata"] = outdataArr
	} else if outdataCopy != nil {
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
//
// An optional "resource" filter (§6) narrows the returned metadata to the
// matching resource-instance subtree(s) of a multiplexed procedure. Per the
// merged spec text, a resource filter must not be used when path addresses a
// branch node, and Discover accepts no other filter variant.
func HandleDiscover(requestMap map[string]interface{}, backendChan chan map[string]interface{}) {
	path, _ := requestMap["path"].(string)
	requestId, _ := requestMap["requestId"].(string)

	resourceSnips, filterErr := discoverResourceFilter(requestMap["filter"])
	if filterErr != "" {
		sendServiceError(backendChan, "discover", requestId, "", StatusUnknown,
			"400", "bad_request", filterErr, requestMap)
		return
	}

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
	if len(resourceSnips) > 0 && nodeType != utils.PROCEDURE {
		sendServiceError(backendChan, "discover", requestId, "", StatusUnknown,
			"400", "bad_request", "a 'resource' filter must not be used if the path addresses a branch node", requestMap)
		return
	}

	var metadata map[string]interface{}
	if nodeType == utils.PROCEDURE && len(resourceSnips) > 0 {
		resourceKeys, resErr := resolveResourceInstances(node, resourceSnips)
		if resErr != "" {
			sendServiceError(backendChan, "discover", requestId, "", StatusUnknown,
				"400", "bad_request", resErr, requestMap)
			return
		}
		metadata = buildProcedureMetadataFiltered(node, path, resourceKeys)
	} else if nodeType == utils.PROCEDURE {
		metadata = buildProcedureMetadata(node, path)
	} else {
		metadata = buildServiceMetadata(node, path)
	}

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

// discoverResourceFilter parses requestMap["filter"] for HandleDiscover,
// which only accepts a single {"variant":"resource",...} object (no array
// combination, no other variant) — Discover has no monitoring semantics to
// combine a resource filter with. Returns (nil, "") when no filter is given.
func discoverResourceFilter(filter interface{}) ([]string, string) {
	if filter == nil {
		return nil, ""
	}
	m, ok := filter.(map[string]interface{})
	if !ok {
		return nil, "discover only supports a single 'resource' filter object"
	}
	variant, _ := m["variant"].(string)
	if variant != "resource" {
		return nil, "discover only supports the 'resource' filter variant"
	}
	return resourceSnippetsFromFilter(m)
}

// aggregateResourceStatus computes a multi-resource invocation's overall
// status from its per-resource statuses, per the confirmed "wait for all"
// completion semantics: the overall status stays ONGOING until every
// addressed resource has reported a terminal status; once all have, it is
// SUCCESSFUL only if every resource succeeded (any FAILED resource fails the
// whole invocation; CANCELED is treated the same as FAILED here since
// resources do not receive individual cancel signals — only the whole
// invocation can be cancelled, via HandleCancel).
func aggregateResourceStatus(inv *invocationState) ServiceStatus {
	anyFailed := false
	for _, rk := range inv.resources {
		st, ok := inv.resourceStatus[rk]
		if !ok || st == StatusOngoing {
			return StatusOngoing // not every resource has reported terminal yet
		}
		if st != StatusSuccessful {
			anyFailed = true
		}
	}
	if anyFailed {
		return StatusFailed
	}
	return StatusSuccessful
}

// UpdateServiceState is called by a registered service process (via
// serviceReg.go) to report execution progress. It updates the invocation
// state and fans out monitoring events to all watching sessions, respecting
// each session's filter settings.
//
// resourceKey identifies which resource instance this update is for, and
// must be "" for a single-resource invocation (unchanged behaviour: outdata
// is stored directly on the invocation and reported as-is). For a
// multi-resource invocation (len(inv.resources) > 1), resourceKey must name
// one of inv.resources; outdata is stored under
// inv.outdataByResource[resourceKey] instead, the invocation's per-resource
// status is updated, and the invocation's overall status is (re)computed via
// aggregateResourceStatus — see its doc comment for the completion semantics.
//
// svcErr, when non-nil, is included in monitoring events as
// {"error":{"code":"...","message":"..."}} (VISSv3.3 §20).
//
// progress, when non-nil, stores the completion percentage (0-100) and is
// included in ONGOING monitoring events (VISSv3.3 §28).
func UpdateServiceState(serviceId string, status ServiceStatus, resourceKey string,
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
	multiResource := len(inv.resources) > 1

	var effectiveStatus ServiceStatus
	var outdataArr []map[string]interface{}
	if multiResource && resourceKey != "" {
		if inv.resourceStatus == nil {
			inv.resourceStatus = map[string]ServiceStatus{}
		}
		inv.resourceStatus[resourceKey] = status
		if outdataWrapped != nil {
			if inv.outdataByResource == nil {
				inv.outdataByResource = map[string]map[string]interface{}{}
			}
			inv.outdataByResource[resourceKey] = outdataWrapped
		}
		effectiveStatus = aggregateResourceStatus(inv)
		outdataArr = buildOutdataArrayFromWrapped(inv.outdataByResource)
	} else {
		effectiveStatus = status
		if outdataWrapped != nil {
			inv.outdata = outdataWrapped
		}
	}
	inv.status = effectiveStatus
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
	if effectiveStatus != StatusOngoing {
		termPath = inv.path
		termDur = time.Since(inv.startedAt)
	}

	statusChanged := prevStatus != effectiveStatus

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
		if effectiveStatus != StatusOngoing {
			if sess.cancelTicker != nil {
				sess.cancelTicker()
			}
			toRemove = append(toRemove, id)
		}
	}
	for _, id := range toRemove {
		delete(sessions, id)
	}
	if effectiveStatus != StatusOngoing {
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
		switch effectiveStatus {
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
			"status":    string(effectiveStatus),
			"ts":        ts,
		}
		if t.sess.routerId != "" {
			event["RouterId"] = t.sess.routerId // address the event back to the requesting client
		}
		if outdataArr != nil {
			event["outdata"] = outdataArr
		} else if outdataWrapped != nil {
			event["outdata"] = outdataWrapped
		}
		if svcErr != nil {
			event["error"] = map[string]interface{}{
				"code":    svcErr.Code,
				"message": svcErr.Message,
			}
		}
		// Include progress percentage in ONGOING events only (§28).
		if effectiveStatus == StatusOngoing && progressVal != nil {
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
	return buildProcedureMetadataFiltered(node, path, nil)
}

// buildProcedureMetadataFiltered is buildProcedureMetadata's implementation,
// additionally narrowing the returned tree metadata to the given
// resource-instance keys (e.g. ["Row1.DriverSide"]) when resourceKeys is
// non-empty (§6: HandleDiscover's "resource" filter support). When
// resourceKeys is nil/empty, every child of node is walked unchanged
// (single-resource procedure, or "all resources" — buildProcedureMetadata's
// existing unfiltered behaviour).
func buildProcedureMetadataFiltered(node *utils.Node_t, path string, resourceKeys []string) map[string]interface{} {
	meta := map[string]interface{}{"type": "procedure"}
	if len(resourceKeys) > 0 {
		for _, rk := range resourceKeys {
			if resourceNode := resolveRelativeNode(node, rk); resourceNode != nil {
				meta[rk] = buildResourceInstanceMetadata(resourceNode)
			}
		}
	} else {
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

// buildResourceInstanceMetadata returns metadata for one resource-instance
// branch (e.g. Row1.DriverSide) under a multiplexed procedure: its nested
// Input/Output iostructs, keyed the same way buildProcedureMetadata keys a
// non-multiplexed procedure's direct iostruct children.
func buildResourceInstanceMetadata(node *utils.Node_t) map[string]interface{} {
	meta := map[string]interface{}{"type": "branch"}
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
//
// For a single-resource procedure, the Input iostruct is a direct child of
// procedureNode (unchanged behaviour). For a multiplexed procedure (§1), the
// Input iostruct instead lives under each resource-instance branch
// (e.g. Row1.DriverSide.Input); since the signature is guaranteed identical
// across every resource instance (they all come from the same 'instances:'
// expansion), it is sufficient to validate against any one resolved resource
// — the first entry of resourceKeys is used when no direct Input child exists.
func validateInputSignature(procedureNode *utils.Node_t, resourceKeys []string, inputParams map[string]interface{}) (bool, []string) {
	if inputNode := findDirectInputChild(procedureNode); inputNode != nil {
		return validateIoParams(inputNode, inputParams)
	}
	if len(resourceKeys) > 0 {
		if resourceNode := resolveRelativeNode(procedureNode, resourceKeys[0]); resourceNode != nil {
			if inputNode := findDirectInputChild(resourceNode); inputNode != nil {
				return validateIoParams(inputNode, inputParams)
			}
		}
	}
	return true, nil // no Input iostruct means no input required
}

// findDirectInputChild returns node's direct "Input" IOSTRUCT child, or nil.
func findDirectInputChild(node *utils.Node_t) *utils.Node_t {
	numChildren := utils.VSSgetNumOfChildren(node)
	for i := 0; i < numChildren; i++ {
		child := utils.VSSgetChild(node, i)
		if child == nil {
			continue
		}
		if utils.VSSgetName(child) == "Input" && utils.VSSgetType(child) == utils.IOSTRUCT {
			return child
		}
	}
	return nil
}

// findDirectOutputChild returns node's direct "Output" IOSTRUCT child, or nil.
func findDirectOutputChild(node *utils.Node_t) *utils.Node_t {
	numChildren := utils.VSSgetNumOfChildren(node)
	for i := 0; i < numChildren; i++ {
		child := utils.VSSgetChild(node, i)
		if child == nil {
			continue
		}
		if utils.VSSgetName(child) == "Output" && utils.VSSgetType(child) == utils.IOSTRUCT {
			return child
		}
	}
	return nil
}

// procedureHasOutput reports whether the addressed procedure's signature
// declares an Output iostruct — i.e. whether outdata is mandatory on every
// monitoring event for it (VISSv3.2 §monitorEvent's "Yes (*)" outdata rule).
// Mirrors validateInputSignature's node resolution: for a multiplexed
// procedure (§1) the Output iostruct lives under each resource-instance
// branch instead of directly on procedureNode, so the first resolved
// resource key is checked when no direct Output child exists (the signature
// is identical across every resource instance).
func procedureHasOutput(procedureNode *utils.Node_t, resourceKeys []string) bool {
	if findDirectOutputChild(procedureNode) != nil {
		return true
	}
	if len(resourceKeys) > 0 {
		if resourceNode := resolveRelativeNode(procedureNode, resourceKeys[0]); resourceNode != nil {
			if findDirectOutputChild(resourceNode) != nil {
				return true
			}
		}
	}
	return false
}

// resolveRelativeNode walks the "."-separated relative path (e.g.
// "Row1.DriverSide") down from node, matching child names exactly, and
// returns the addressed descendant or nil if any segment is not found.
func resolveRelativeNode(node *utils.Node_t, relPath string) *utils.Node_t {
	current := node
	for _, seg := range strings.Split(relPath, ".") {
		found := (*utils.Node_t)(nil)
		numChildren := utils.VSSgetNumOfChildren(current)
		for i := 0; i < numChildren; i++ {
			child := utils.VSSgetChild(current, i)
			if child != nil && utils.VSSgetName(child) == seg {
				found = child
				break
			}
		}
		if found == nil {
			return nil
		}
		current = found
	}
	return current
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

// parsedFilter is the decomposed form of a service request's "filter" field,
// which may be a single filter object or an array containing exactly one
// "resource" filter plus exactly one other variant (VISSv3.2 "Multiple
// Filters").
type parsedFilter struct {
	variant       string        // "timebased" | "status" | "all" | "none" (defaults to "all" if absent)
	period        time.Duration // only meaningful when variant == "timebased"
	resourceSnips []string      // path snippets from a "resource" variant filter, e.g. ["Row1.DriverSide"]; nil if none
}

// parseServiceFilter accepts requestMap["filter"] in any of the shapes
// allowed by the VISSv3.2 Services filter (including the "resource" variant
// and the "Multiple Filters" array-of-two-objects combination). Returns an
// error string (non-empty) for any other shape, e.g. two non-resource
// variants combined, or an array not containing a "resource" filter.
func parseServiceFilter(filter interface{}) (parsedFilter, string) {
	switch f := filter.(type) {
	case nil:
		return parsedFilter{variant: "all"}, ""
	case []interface{}:
		return parseFilterArray(f)
	default:
		m := filterToMap(filter)
		if m == nil {
			return parsedFilter{variant: "all"}, ""
		}
		return parseFilterObject(m)
	}
}

// parseFilterObject decomposes a single filter object (map form). A bare
// {"variant":"resource",...} is a valid standalone filter (e.g. for Discover,
// or an Invoke/Monitor that does not also need event-rate/status control).
func parseFilterObject(m map[string]interface{}) (parsedFilter, string) {
	variant, _ := m["variant"].(string)
	if variant == "" {
		variant = "all"
	}
	switch variant {
	case "resource":
		snips, err := resourceSnippetsFromFilter(m)
		if err != "" {
			return parsedFilter{}, err
		}
		return parsedFilter{variant: "all", resourceSnips: snips}, ""
	case "timebased":
		return parsedFilter{variant: variant, period: periodFromFilterMap(m)}, ""
	case "status", "all", "none":
		return parsedFilter{variant: variant}, ""
	default:
		return parsedFilter{}, fmt.Sprintf("unsupported filter variant %q", variant)
	}
}

// parseFilterArray decomposes the VISSv3.2 "Multiple Filters" array form: an
// array of exactly two filter objects, one of which must have variant
// "resource" and the other one of the standard monitoring variants.
func parseFilterArray(arr []interface{}) (parsedFilter, string) {
	if len(arr) != 2 {
		return parsedFilter{}, "filter array must contain exactly two filter objects"
	}
	var resourceObj, otherObj map[string]interface{}
	for _, raw := range arr {
		m, ok := raw.(map[string]interface{})
		if !ok {
			return parsedFilter{}, "filter array entries must be filter objects"
		}
		variant, _ := m["variant"].(string)
		if variant == "resource" {
			if resourceObj != nil {
				return parsedFilter{}, "filter array must contain exactly one 'resource' filter"
			}
			resourceObj = m
		} else {
			if otherObj != nil {
				return parsedFilter{}, "filter array must combine 'resource' with exactly one other variant"
			}
			otherObj = m
		}
	}
	if resourceObj == nil || otherObj == nil {
		return parsedFilter{}, "filter array must contain a 'resource' filter and one other variant"
	}
	snips, err := resourceSnippetsFromFilter(resourceObj)
	if err != "" {
		return parsedFilter{}, err
	}
	pf, err := parseFilterObject(otherObj)
	if err != "" {
		return parsedFilter{}, err
	}
	pf.resourceSnips = snips
	return pf, ""
}

// resourceSnippetsFromFilter extracts and validates the "parameter" array of
// path snippet strings from a {"variant":"resource",...} filter object.
func resourceSnippetsFromFilter(m map[string]interface{}) ([]string, string) {
	paramRaw, ok := m["parameter"].([]interface{})
	if !ok || len(paramRaw) == 0 {
		return nil, "'resource' filter requires a non-empty 'parameter' array"
	}
	snips := make([]string, 0, len(paramRaw))
	for _, p := range paramRaw {
		s, ok := p.(string)
		if !ok || s == "" {
			return nil, "'resource' filter 'parameter' entries must be non-empty strings"
		}
		snips = append(snips, s)
	}
	return snips, ""
}

func periodFromFilterMap(m map[string]interface{}) time.Duration {
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

// ---- resource resolution ----------------------------------------------------

// maxResourceDepth bounds how many path segments resolveResourceInstances
// walks below a procedure node when collecting resource-instance branches
// (e.g. Row1.DriverSide is depth 2). Generous enough for any realistic HIM
// service tree while preventing runaway recursion on malformed trees.
const maxResourceDepth = 8

// resolveResourceInstances returns the concrete resource-instance branch
// paths (relative to procedureNode, e.g. "Row1.DriverSide") that exist under
// procedureNode and match the given filter snippets. Snippets may contain "*"
// wildcards on individual path segments (matching the wildcard semantics used
// elsewhere in the tree-search code, utils.VSSsearchNodes's "*" handling).
//
// If snips is empty (no resource filter given), ALL resource-instance leaf
// branches are returned (VISSv3.2: "If a resource variant filter is not
// included... the response... will contain output data from all the
// resources").
//
// A procedure with no resource-instance branches (single-resource, e.g.
// GetCapabilities) returns (nil, "") — callers treat a nil/empty result as
// "not multiplexed" and skip resource-keyed handling entirely.
func resolveResourceInstances(procedureNode *utils.Node_t, snips []string) ([]string, string) {
	all := collectResourceInstanceBranches(procedureNode, "")
	if len(all) == 0 {
		return nil, "" // not a multiplexed procedure
	}
	if len(snips) == 0 {
		sort.Strings(all)
		return all, ""
	}
	seen := map[string]bool{}
	var matched []string
	for _, snip := range snips {
		found := false
		for _, key := range all {
			if resourcePathMatches(snip, key) {
				found = true
				if !seen[key] {
					seen[key] = true
					matched = append(matched, key)
				}
			}
		}
		if !found {
			return nil, fmt.Sprintf("resource filter snippet %q does not match any resource of this procedure", snip)
		}
	}
	sort.Strings(matched)
	return matched, ""
}

// collectResourceInstanceBranches walks the plain BRANCH nodes nested
// directly under a multiplexed procedure node and returns the dot-joined
// relative path of every LEAF branch (a branch with no further BRANCH
// children, e.g. "Row1.DriverSide") — the level at which Input/Output
// iostructs are attached. Non-BRANCH children (Input/Output/Version) are not
// resource instances and are ignored.
func collectResourceInstanceBranches(node *utils.Node_t, prefix string) []string {
	return collectResourceInstanceBranchesDepth(node, prefix, 0)
}

func collectResourceInstanceBranchesDepth(node *utils.Node_t, prefix string, depth int) []string {
	if depth > maxResourceDepth {
		return nil
	}
	var result []string
	numChildren := utils.VSSgetNumOfChildren(node)
	for i := 0; i < numChildren; i++ {
		child := utils.VSSgetChild(node, i)
		if child == nil || utils.VSSgetType(child) != utils.BRANCH {
			continue
		}
		name := utils.VSSgetName(child)
		childPath := name
		if prefix != "" {
			childPath = prefix + "." + name
		}
		grandChildBranches := hasChildBranch(child)
		if grandChildBranches {
			result = append(result, collectResourceInstanceBranchesDepth(child, childPath, depth+1)...)
		} else {
			result = append(result, childPath)
		}
	}
	return result
}

// hasChildBranch reports whether node has at least one direct BRANCH child.
func hasChildBranch(node *utils.Node_t) bool {
	numChildren := utils.VSSgetNumOfChildren(node)
	for i := 0; i < numChildren; i++ {
		child := utils.VSSgetChild(node, i)
		if child != nil && utils.VSSgetType(child) == utils.BRANCH {
			return true
		}
	}
	return false
}

// resourcePathMatches reports whether the resource-instance path key (e.g.
// "Row1.DriverSide") matches filter snippet snip, which may contain "*"
// wildcards on individual "."-separated segments (matching every remaining
// segment of key at that position, same convention as the rest of the
// tree-search code).
func resourcePathMatches(snip, key string) bool {
	snipSegs := strings.Split(snip, ".")
	keySegs := strings.Split(key, ".")
	if len(snipSegs) > len(keySegs) {
		return false
	}
	for i, s := range snipSegs {
		if s == "*" {
			continue
		}
		if s != keySegs[i] {
			return false
		}
	}
	// A snippet may address a partial prefix (e.g. "Row1" matching both
	// Row1.DriverSide and Row1.PassengerSide) as well as a full leaf path.
	return true
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
