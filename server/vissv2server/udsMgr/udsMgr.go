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
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	utils "github.com/covesa/vissr/utils"
)

const backendTermination = "internal-backend-termination"

// the number of channel array elements sets the limit for max number of parallel WS app clients
const NUMOFUDSCLIENTS = 20

var (
	udsClientChan      []chan string
	clientBackendChan  []chan string
	UdsClientIndexList []bool
	udsClientIndex     int
	// indexListMu protects UdsClientIndexList (mutated by initClientServer's
	// accept loop on one goroutine and by serveConn cleanup on another).
	indexListMu sync.Mutex
)


const isClientLocal = false

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
	PayloadId        string
	Dc               CompressionType
	ResponseHandling int //possible values 1..4, see description
	SortedList       []string
}

var dataCompressionCache []DataCompressionElement

const DCCACHESIZE = 20

// channelSendTimeout caps how long the hub will wait when sending the
// synchronous reply on udsClientChan or routing a subscription on
// clientBackendChan. If the consumer goroutine is gone we'd otherwise block
// the entire UDS manager. 5s is well above any realistic latency.
const channelSendTimeout = 5 * time.Second

// mapString returns m[key] as a string. Returns "", false on absent, nil,
// or wrong-type entries. Replaces the unchecked .(string) assertions that
// previously panicked the manager on any malformed JSON message.
func mapString(m map[string]interface{}, key string) (string, bool) {
	v, ok := m[key]
	if !ok || v == nil {
		return "", false
	}
	s, ok := v.(string)
	if !ok {
		return "", false
	}
	return s, true
}

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

// RemoveRoutingForwardResponse pre-fix:
//   - Used clientId from utils.RemoveInternalData as a slice index with no
//     bounds check. A malformed response (missing RouterId, non-numeric
//     clientId) produced clientId=0 (via Atoi swallowing the error) or a
//     value outside [0, NUMOFUDSCLIENTS), causing a slice-bounds panic or a
//     write to the wrong client's channel (data leak across sessions).
//   - The non-subscription branch did a blocking send on udsClientChan[id];
//     if the matching reader had exited, the entire hub froze. The
//     subscription branch used select/default but the regular branch did not.
//   - The log message said "wsmgr:Event dropped" - misleading for the UDS
//     manager.
//
// transportMgrChan retained for signature compatibility with httpMgr / wsMgr.
func RemoveRoutingForwardResponse(response string, transportMgrChan chan string) {
	trimmedResponse, clientId := utils.RemoveInternalData(response)
	if clientId < 0 || clientId >= NUMOFUDSCLIENTS {
		utils.Error.Printf("udsMgr:RemoveRoutingForwardResponse: invalid clientId=%d (response=%q); dropping", clientId, trimmedResponse)
		return
	}
	if strings.Contains(trimmedResponse, "\"subscription\"") {
		select {
		case clientBackendChan[clientId] <- trimmedResponse: //subscription notification
		default:
			utils.Error.Printf("udsMgr:subscription event dropped, clientId=%d", clientId)
		}
		return
	}
	// Synchronous-response path. The udsReader for this client is expected
	// to be parked on `<-clientChannel`. If it isn't (dead reader, slow
	// consumer), bound the wait so the hub doesn't freeze.
	select {
	case udsClientChan[clientId] <- trimmedResponse:
	case <-time.After(channelSendTimeout):
		utils.Error.Printf("udsMgr:response dropped (clientId=%d not consuming after %s)", clientId, channelSendTimeout)
	}
}

func checkCompressionRequest(reqMessage string) {
	if strings.Contains(reqMessage, `"dc"`) {
		dcValue, payloadId, singleResponse, singlePath := getDcConfig(reqMessage)
		if len(dcValue) > 0 {
			responseHandling := 1 // singleResponse && singlePath
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

// getValueForKey pre-fix would underflow when `key` wasn't in `reqMessage`:
//
//	keyIndex := strings.Index(reqMessage, key) + len(key)
//
// returned len(key)-1 (positive) and the subsequent slice expression could
// panic with `slice bounds out of range` or read from the wrong byte
// offset. Now we early-return on miss and bounds-check every slice index.
func getValueForKey(reqMessage string, key string) string {
	keyPos := strings.Index(reqMessage, key)
	if keyPos == -1 {
		return ""
	}
	keyIndex := keyPos + len(key)
	if keyIndex >= len(reqMessage) {
		return ""
	}
	hyphenIndex1 := strings.Index(reqMessage[keyIndex:], `"`)
	if hyphenIndex1 == -1 {
		return ""
	}
	valueStart := keyIndex + hyphenIndex1 + 1
	if valueStart >= len(reqMessage) {
		return ""
	}
	hyphenIndex2 := strings.Index(reqMessage[valueStart:], `"`)
	if hyphenIndex2 == -1 {
		return ""
	}
	return reqMessage[valueStart : valueStart+hyphenIndex2]
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
		if err == nil && (pc == 2 || pc == 0) { // only request local compression is supported
			dataCompressionCache[cacheIndex].Dc.Pc = uint8(pc)
			tsc, err := strconv.Atoi(dcValue[plusIndex+1:])
			if err == nil && (tsc == 1 || tsc == 0) { // message local ts compression supported
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
	default:
		return respMessage
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
	case 2:
		return respMessage
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

// getSortedPaths pre-fix had ~5 naked type assertions on attacker-controlled
// JSON: data[i].(map[string]interface{}), v.(string), data.(map[...]). Any
// payload where a `data` element wasn't an object, or `path` wasn't a string,
// crashed the manager. Now every assertion uses comma-ok form and bad shapes
// are logged and skipped.
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
		for i := 0; i < len(data); i++ {
			elem, ok := data[i].(map[string]interface{})
			if !ok {
				utils.Error.Printf("getSortedPaths():data[%d] not an object (got %T)", i, data[i])
				continue
			}
			if p, ok := mapString(elem, "path"); ok {
				paths = append(paths, p)
			}
		}
	case map[string]interface{}:
		if p, ok := mapString(data, "path"); ok {
			paths = append(paths, p)
		}
	default:
		utils.Info.Printf("getSortedPaths():data is of an unknown type=%T", dataIf)
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
	messageTs, ok := mapString(respMap, "ts")
	if !ok {
		utils.Error.Printf("compressTs():missing/invalid ts field; leaving message unchanged")
		return respMessage
	}
	var tsList []string
	dataIf := respMap["data"]
	switch data := dataIf.(type) {
	case []interface{}:
		for i := 0; i < len(data); i++ {
			elem, ok := data[i].(map[string]interface{})
			if !ok {
				utils.Error.Printf("compressTs():data[%d] not an object (got %T)", i, data[i])
				continue
			}
			if v, present := elem["dp"]; present {
				tsList = append(tsList, getDpTsList(v)...)
			}
		}
	case map[string]interface{}:
		if v, present := data["dp"]; present {
			tsList = getDpTsList(v)
		}
	default:
		utils.Info.Printf("compressTs():data is of an unknown type=%T", dataIf)
	}
	respMessage = replaceTs(respMessage, messageTs, tsList)
	return respMessage
}

// getDpTsList pre-fix had naked assertions on dp[i].(map) and v.(string).
// Now every shape is checked.
func getDpTsList(dpMap interface{}) []string {
	var tsList []string
	switch dp := dpMap.(type) {
	case []interface{}:
		for i := 0; i < len(dp); i++ {
			elem, ok := dp[i].(map[string]interface{})
			if !ok {
				utils.Error.Printf("getDpTsList():dp[%d] not an object (got %T)", i, dp[i])
				continue
			}
			if ts, ok := mapString(elem, "ts"); ok {
				tsList = append(tsList, ts)
			}
		}
	case map[string]interface{}:
		if ts, ok := mapString(dp, "ts"); ok {
			tsList = append(tsList, ts)
		}
	default:
		utils.Info.Printf("getDpTsList():dp is of an unknown type=%T", dpMap)
	}
	return tsList
}

// replaceTs pre-fix discarded the time.Parse errors silently; on malformed
// timestamps tsRef became zero-time, refMs was -6.2e13, and the diff window
// rejected every replacement (silent no-op). Now parse errors are logged.
func replaceTs(respMessage string, messageTs string, tsList []string) string {
	tsRef, err := time.Parse(time.RFC3339, messageTs)
	if err != nil {
		utils.Error.Printf("replaceTs:failed to parse messageTs %q: %v; skipping ts compression", messageTs, err)
		return respMessage
	}
	refMs := tsRef.UnixMilli()
	preIndex := 0
	postIndex := len(respMessage)
	var respFraction string
	messageTsIndex := strings.Index(respMessage, messageTs)
	if messageTsIndex == -1 {
		return respMessage
	}
	if strings.Count(respMessage[:messageTsIndex], "{") == 1 {
		preIndex = messageTsIndex
		respFraction = respMessage[:preIndex]
	} else {
		messageTsIndex = strings.LastIndex(respMessage, messageTs)
		postIndex = messageTsIndex
		respFraction = respMessage[postIndex:]
	}
	for i := 0; i < len(tsList); i++ {
		tsDp, err := time.Parse(time.RFC3339, tsList[i])
		if err != nil {
			utils.Error.Printf("replaceTs:failed to parse tsList[%d]=%q: %v", i, tsList[i], err)
			continue
		}
		dpMs := tsDp.UnixMilli()
		diffMs := refMs - dpMs
		if diffMs > 999999999 || diffMs < -999999999 {
			continue // keep iso time
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

// signedTimeDiff hardened: pre-fix would panic on diffMsStr[1:] if diffMsStr
// was ever empty. Now bounds-checked.
func signedTimeDiff(diffMsStr string, diffMs int64) string {
	if diffMs > 0 {
		return "-" + diffMsStr
	}
	if diffMs == 0 {
		return "+" + diffMsStr
	}
	// diffMs < 0 -> diffMsStr starts with '-'; strip the sign.
	if len(diffMsStr) <= 1 {
		return "+0"
	}
	return "+" + diffMsStr[1:]
}

func compressPaths(respMessage string, sortedList []string) string {
	for i := 0; i < len(sortedList); i++ {
		respMessage = strings.Replace(respMessage, sortedList[i], strconv.Itoa(i), 1)
	}
	return respMessage
}

// initClientServer pre-fix:
//   - Never closed the listener (resource leak across the lifetime of the
//     process; if listener is externally invalidated the accept loop spins).
//   - Used `getUdsClientIndex()` and passed the result to `udsClientChan[*]`
//     without checking for -1 (the "no slot free" sentinel). The 21st
//     simultaneous feeder connect crashed the entire server.
//   - Did not call os.MkdirAll on the socket directory or os.Chmod on the
//     socket file - the socket could be world-readable/writable depending
//     on the umask, allowing any local user to inject "feeder" requests.
//   - On Accept error simply `continue`d in a tight loop, burning CPU when
//     the listener was permanently broken.
func initClientServer(mgrId int, clientIndex *int) {
	*clientIndex = 0
	const sockPath = "/var/tmp/vissv2/udsMgr.sock"
	if err := os.MkdirAll(filepath.Dir(sockPath), 0755); err != nil {
		utils.Error.Printf("UdsMgrInit:UDS mkdir parent failed, err = %s", err)
		return
	}
	os.Remove(sockPath)
	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		utils.Error.Printf("UdsMgrInit:UDS listen failed, err = %s", err)
		return
	}
	defer listener.Close()
	if err := os.Chmod(sockPath, 0660); err != nil {
		utils.Error.Printf("UdsMgrInit:UDS chmod failed (continuing anyway), err = %s", err)
	}
	for {
		conn, err := listener.Accept()
		if err != nil {
			utils.Error.Printf("UdsMgrInit:UDS accept failed, err = %s. Retrying in 1s.", err)
			time.Sleep(1 * time.Second)
			continue
		}
		idx := getUdsClientIndex()
		if idx < 0 {
			// The pool is full (NUMOFUDSCLIENTS active connections).
			// Without this check, indexing udsClientChan[-1] would panic
			// and tear down the daemon. Close the new connection.
			utils.Error.Printf("UdsMgrInit:max clients (%d) reached; rejecting connection", NUMOFUDSCLIENTS)
			conn.Close()
			continue
		}
		*clientIndex = idx
		go serveConn(conn, idx)
	}
}

// serveConn owns the lifecycle of one feeder connection. Pre-fix the reader
// and writer goroutines both did `defer conn.Close()` on the same conn,
// causing a double-close race - the first to exit closed the fd under the
// other one. They also both could block forever on their channels if the
// peer was gone, leaking goroutines. Now this wrapper owns conn.Close() and
// a shared `quit` channel that both reader and writer watch.
func serveConn(conn net.Conn, clientId int) {
	defer conn.Close()
	defer returnUdsClientIndex(clientId)

	quit := make(chan struct{})
	var quitOnce sync.Once
	closeQuit := func() { quitOnce.Do(func() { close(quit) }) }

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		defer closeQuit()
		udsReader(conn, udsClientChan[clientId], clientBackendChan[clientId], clientId, quit)
	}()
	go func() {
		defer wg.Done()
		defer closeQuit()
		udsWriter(conn, clientBackendChan[clientId], quit)
	}()
	wg.Wait()
}

func getUdsClientIndex() int {
	indexListMu.Lock()
	defer indexListMu.Unlock()
	for i := range UdsClientIndexList {
		if UdsClientIndexList[i] {
			UdsClientIndexList[i] = false
			return i
		}
	}
	return -1
}

func returnUdsClientIndex(index int) {
	if index < 0 || index >= NUMOFUDSCLIENTS {
		utils.Error.Printf("returnUdsClientIndex:invalid index=%d", index)
		return
	}
	indexListMu.Lock()
	defer indexListMu.Unlock()
	UdsClientIndexList[index] = true
}

// handleUdsTransportResponse processes a single response coming back
// from the server core. Extracted from UdsMgrInit's for/select loop so
// the response path can be unit-tested independently of the goroutine
// machinery — see udsMgr_dispatch_test.go.
func handleUdsTransportResponse(respMessage string, transportMgrChan chan string) {
	utils.Info.Printf("UDS mgr hub: Response from server core:%s", respMessage)
	respMessage = checkCompressionResponse(respMessage)
	RemoveRoutingForwardResponse(respMessage, transportMgrChan)
}

// handleUdsClientRequest processes a single inbound UDS-client
// request. Extracted from UdsMgrInit so the validation + compression
// + forwarding behaviour can be unit-tested. The four-way decision
// (kill-bypass / validation-error / compression-tagged / forward)
// previously lived inline at the bottom of UdsMgrInit; it now lives
// here as a named function.
func handleUdsClientRequest(reqMessage string, mgrId int, clientId int, transportMgrChan chan string) {
	if !isKillSubscriptions(reqMessage) {
		validationError := utils.JsonSchemaValidate(reqMessage)
		if len(validationError) > 0 {
			// Build a fresh error response map each call. The pre-fix version
			// used a package-global map that accumulated leaked keys
			// (subscriptionId, etc.) from previous error responses.
			errorResponseMap := map[string]interface{}{}
			requestMap := make(map[string]interface{})
			requestMap["action"] = utils.ExtractFromRequest(reqMessage, "action")
			requestMap["requestId"] = utils.ExtractFromRequest(reqMessage, "requestId")
			utils.SetErrorResponse(requestMap, errorResponseMap, 0, validationError) //bad_request
			// Bounded send: don't freeze the hub if the client has gone away.
			select {
			case udsClientChan[clientId] <- utils.FinalizeMessage(errorResponseMap):
			case <-time.After(channelSendTimeout):
				utils.Error.Printf("udsMgr:error response dropped (clientId=%d not consuming after %s)", clientId, channelSendTimeout)
			}
			return
		}
		checkCompressionRequest(reqMessage)
	}
	utils.AddRoutingForwardRequest(reqMessage, mgrId, clientId, transportMgrChan)
}

// isKillSubscriptions parses the message and returns true only when the
// `action` field is the exact string "internal-killsubscriptions". Pre-fix
// the hub used `strings.Contains(reqMessage, "internal-killsubscriptions")`,
// which any feeder could trigger by embedding the literal in a path or value
// to bypass JSON schema validation.
func isKillSubscriptions(reqMessage string) bool {
	var msg map[string]interface{}
	if err := json.Unmarshal([]byte(reqMessage), &msg); err != nil {
		return false
	}
	action, ok := mapString(msg, "action")
	if !ok {
		return false
	}
	return action == "internal-killsubscriptions"
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
			handleUdsTransportResponse(respMessage, transportMgrChan)
			continue
		// NOTE: the case list below is hand-coupled to NUMOFUDSCLIENTS. If
		// NUMOFUDSCLIENTS changes, this select must change to match - extend
		// the case list (or rewrite using reflect.Select).
		case reqMessage = <-udsClientChan[0]:
			clientId = 0
		case reqMessage = <-udsClientChan[1]:
			clientId = 1
		case reqMessage = <-udsClientChan[2]:
			clientId = 2
		case reqMessage = <-udsClientChan[3]:
			clientId = 3
		case reqMessage = <-udsClientChan[4]:
			clientId = 4
		case reqMessage = <-udsClientChan[5]:
			clientId = 5
		case reqMessage = <-udsClientChan[6]:
			clientId = 6
		case reqMessage = <-udsClientChan[7]:
			clientId = 7
		case reqMessage = <-udsClientChan[8]:
			clientId = 8
		case reqMessage = <-udsClientChan[9]:
			clientId = 9
		case reqMessage = <-udsClientChan[10]:
			clientId = 10
		case reqMessage = <-udsClientChan[11]:
			clientId = 11
		case reqMessage = <-udsClientChan[12]:
			clientId = 12
		case reqMessage = <-udsClientChan[13]:
			clientId = 13
		case reqMessage = <-udsClientChan[14]:
			clientId = 14
		case reqMessage = <-udsClientChan[15]:
			clientId = 15
		case reqMessage = <-udsClientChan[16]:
			clientId = 16
		case reqMessage = <-udsClientChan[17]:
			clientId = 17
		case reqMessage = <-udsClientChan[18]:
			clientId = 18
		case reqMessage = <-udsClientChan[19]:
			clientId = 19
		}
		handleUdsClientRequest(reqMessage, mgrId, clientId, transportMgrChan)
	}
}

const udsReadBuf = 8192

// udsReader pre-fix:
//   - did `defer conn.Close()` while udsWriter on the same conn did the
//     same, causing a double-close race;
//   - had a dead truncation check (`n > 8192` is impossible when buf has
//     length 8192) - the real check is `n == 8192` (likely truncated);
//   - on read error did *blocking* sends on clientChannel + clientBackendChannel,
//     deadlocking if the hub or writer had already exited;
//   - did not return the client slot on exit if the kill-subscriptions
//     send blocked, permanently leaking a slot.
//
// All four are fixed below. conn.Close() is now owned by serveConn; the
// reader/writer return on quit signal and the wrapper handles teardown.
func udsReader(conn net.Conn, clientChannel chan string, clientBackendChannel chan string, clientId int, quit <-chan struct{}) {
	buf := make([]byte, udsReadBuf)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			utils.Error.Printf("udsReader:Read failed, clientId=%d, err = %s", clientId, err)
			// Best-effort: ask the hub to clean up subscriptions for this
			// client, and tell the writer to terminate. Non-blocking with
			// timeout so we never deadlock.
			select {
			case clientChannel <- `{"action":"internal-killsubscriptions"}`:
			case <-time.After(channelSendTimeout):
				utils.Error.Printf("udsReader:kill-subscriptions send timed out, clientId=%d", clientId)
			case <-quit:
			}
			select {
			case clientBackendChannel <- backendTermination:
			case <-time.After(channelSendTimeout):
				utils.Error.Printf("udsReader:backendTermination send timed out, clientId=%d", clientId)
			case <-quit:
			}
			return
		}
		// n cannot exceed len(buf); fill of the buffer suggests truncation.
		if n == len(buf) {
			utils.Error.Printf("udsReader:message at buffer size (%d); likely truncated, dropped", n)
			continue
		}
		utils.Info.Printf("udsReader:Message from server: %s", string(buf[:n]))
		// Forward request to hub. Use select-with-quit so we don't park
		// forever if the hub is gone (shouldn't happen in production but
		// keeps test/teardown clean).
		select {
		case clientChannel <- string(buf[:n]):
		case <-quit:
			return
		}
		// Wait for the synchronous response on the same channel.
		var response string
		select {
		case response = <-clientChannel:
		case <-quit:
			return
		}
		// Forward response to writer.
		select {
		case clientBackendChannel <- response:
		case <-quit:
			return
		}
	}
}

// udsWriter pre-fix:
//   - did its own `defer conn.Close()` racing the reader's defer;
//   - on conn.Write error just logged and kept looping forever - the
//     writer goroutine outlived the connection;
//   - had no way to exit on shutdown other than a backendTermination
//     message - a write failure made the goroutine permanent.
//
// Fixed: no defer (serveConn owns the conn); exit on quit; exit on Write
// error.
func udsWriter(conn net.Conn, clientBackendChannel chan string, quit <-chan struct{}) {
	for {
		var message string
		select {
		case message = <-clientBackendChannel:
		case <-quit:
			return
		}
		utils.Info.Printf("udsWriter: Message received=%s", message)
		if message == backendTermination {
			utils.Error.Print("udsWriter:App client websocket session error.")
			return
		}
		if _, err := conn.Write([]byte(message)); err != nil {
			utils.Error.Printf("udsWriter:App client write error: %v", err)
			return
		}
	}
}
