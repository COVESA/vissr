/**
* (C) 2024 Ford Motor Company
*
* All files and artifacts are licensed under the provisions of the license provided by the LICENSE file in this repository.
*
**/

package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/akamensky/argparse"
	"github.com/bradfitz/gomemcache/memcache"
	"github.com/covesa/vissr/utils"
	"github.com/go-redis/redis"
	_ "github.com/mattn/go-sqlite3"
)

type DomainData struct {
	Name  string
	Value string
}

type DataItem struct {
	Path string   `json:"path"`
	Dp   []DpItem `json:"dp"`
}

type DpItem struct {
	Ts    string `json:"ts"`
	Value string `json:"value"`
}

var tripData []DataItem
var simulatedSource string

type FeederMap struct {
	MapIndex     uint16
	Name         string
	Type         int8
	Datatype     int8
	ConvertIndex uint16
}

var scalingDataList []string

var redisClient *redis.Client
var memcacheClient *memcache.Client
var dbHandle *sql.DB
var stateDbType string

// notificationList is mutated from udsReader (server messages) and read from
// initVSSInterfaceMgr (data-write path). Pre-fix these ran in two goroutines
// with no synchronization. notificationMu serializes both sides.
var (
	notificationList []string
	notificationMu   sync.Mutex
)

// mapString returns m[key] as a string. Returns ("", false) on absent, nil,
// or wrong-type entries. Replaces the unchecked .(string) assertions that
// previously panicked the feeder on any malformed server message.
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

// marshalDatapointJSON encodes the {"value":..., "ts":...} datapoint using
// encoding/json so values containing quotes / backslashes / control chars
// can't produce malformed JSON. The pre-fix string-concat path corrupted
// any value with a `"` in it.
func marshalDatapointJSON(val, ts string) (string, error) {
	b, err := json.Marshal(map[string]string{"value": val, "ts": ts})
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func fileExists(filename string) bool {
	info, err := os.Stat(filename)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
}

func readscalingDataList(listFilename string) []string {
	if !fileExists(listFilename) {
		utils.Error.Printf("readscalingDataList: The file %s does not exist.", listFilename)
		return nil
	}
	data, err := os.ReadFile(listFilename)
	if err != nil {
		utils.Error.Printf("readscalingDataList:Error reading %s: %s", listFilename, err)
		return nil
	}
	var convertData []string
	err = json.Unmarshal([]byte(data), &convertData)
	if err != nil {
		utils.Error.Printf("readscalingDataList:Error unmarshal json=%s", err)
		return nil
	}
	return convertData
}

func readFeederMap(mapFilename string) []FeederMap {
	var feederMap []FeederMap
	treeFp, err := os.OpenFile(mapFilename, os.O_RDONLY, 0644)
	if err != nil {
		utils.Error.Printf("Could not open %s for reading map data", mapFilename)
		return nil
	}
	defer treeFp.Close()
	for {
		mapElement, ok := readElement(treeFp)
		if !ok {
			break
		}
		if mapElement.Name == "" {
			break
		}
		feederMap = append(feederMap, mapElement)
	}
	// Sort by Name so callers using sort.Search work correctly.
	sort.Slice(feederMap, func(i, j int) bool { return feederMap[i].Name < feederMap[j].Name })
	return feederMap
}

// readElement reads one FeederMap entry. Returns (zero, false) on short/corrupt
// read so the caller stops cleanly instead of inheriting a half-populated entry.
// Pre-fix this had five unchecked type assertions on deSerializeUInt's return
// (which is `nil` on short reads) and would panic on any truncated mapfile.
func readElement(treeFp *os.File) (FeederMap, bool) {
	var feederMap FeederMap
	mapIndex, ok := readUint16(treeFp)
	if !ok {
		return feederMap, false
	}
	feederMap.MapIndex = mapIndex

	nameLen, ok := readUint8(treeFp)
	if !ok {
		return feederMap, false
	}
	nameBytes, ok := readBytes(uint32(nameLen), treeFp)
	if !ok {
		return feederMap, false
	}
	feederMap.Name = string(nameBytes)

	typeByte, ok := readUint8(treeFp)
	if !ok {
		return feederMap, false
	}
	feederMap.Type = int8(typeByte)

	dataTypeByte, ok := readUint8(treeFp)
	if !ok {
		return feederMap, false
	}
	feederMap.Datatype = int8(dataTypeByte)

	convertIndex, ok := readUint16(treeFp)
	if !ok {
		return feederMap, false
	}
	feederMap.ConvertIndex = convertIndex
	return feederMap, true
}

// MaxReadBytes caps per-call allocation so a corrupt mapfile with a large
// length prefix cannot trigger a multi-GiB allocation.
const MaxReadBytes = 1 << 20 // 1 MiB

// readBytes pre-fix called treeFp.Read once and ignored the returned count and
// error. It now uses io.ReadFull, caps the request size, and returns ok=false
// on any short read so the caller can bail out cleanly.
func readBytes(numOfBytes uint32, treeFp *os.File) ([]byte, bool) {
	if numOfBytes == 0 {
		return nil, true
	}
	if numOfBytes > MaxReadBytes {
		utils.Error.Printf("readBytes: refusing to allocate %d bytes (cap=%d)", numOfBytes, MaxReadBytes)
		return nil, false
	}
	buf := make([]byte, numOfBytes)
	if _, err := io.ReadFull(treeFp, buf); err != nil {
		if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
			utils.Error.Printf("readBytes: read failed: %v", err)
		}
		return nil, false
	}
	return buf, true
}

func readUint8(treeFp *os.File) (uint8, bool) {
	buf, ok := readBytes(1, treeFp)
	if !ok {
		return 0, false
	}
	return buf[0], true
}

func readUint16(treeFp *os.File) (uint16, bool) {
	buf, ok := readBytes(2, treeFp)
	if !ok {
		return 0, false
	}
	return uint16(buf[1])*256 + uint16(buf[0]), true
}

func deSerializeUInt(buf []byte) interface{} {
	switch len(buf) {
	case 1:
		var intVal uint8
		intVal = (uint8)(buf[0])
		return intVal
	case 2:
		var intVal uint16
		intVal = (uint16)((uint16)((uint16)(buf[1])*256) + (uint16)(buf[0]))
		return intVal
	case 4:
		var intVal uint32
		intVal = (uint32)((uint32)((uint32)(buf[3])*16777216) + (uint32)((uint32)(buf[2])*65536) + (uint32)((uint32)(buf[1])*256) + (uint32)(buf[0]))
		return intVal
	default:
		utils.Error.Printf("Buffer length=%d is of an unknown size", len(buf))
		return nil
	}
}

// initVSSInterfaceMgr pre-fix accepted exactly one connection then ran forever;
// if the peer dropped, the udsReader returned and writes piled up on a dead
// channel. The fixed version loops on Accept so the feeder reconnects after
// the server restarts.
func initVSSInterfaceMgr(inputChan chan DomainData, outputChan chan DomainData) {
	const sockPath = "/var/tmp/vissv2/serverFeeder.sock"
	os.Remove(sockPath)
	listener, err := net.Listen("unix", sockPath) //the file must be the same as declared in the feeder-registration.json that the service mgr reads
	if err != nil {
		utils.Error.Printf("udsReader:UDS listen failed, err = %s", err)
		os.Exit(-1)
	}
	defer listener.Close()
	for {
		conn, err := listener.Accept()
		if err != nil {
			utils.Error.Printf("udsReader:UDS accept failed, err = %s. Retrying in 1s.", err)
			time.Sleep(1 * time.Second)
			continue
		}
		serveOnce(conn, inputChan, outputChan)
	}
}

// serveOnce runs the reader and writer goroutines on a single connection,
// then returns when either side ends so the outer Accept loop can reconnect.
// On exit it closes udsChan so the writer goroutine returns cleanly rather
// than leaking blocked on a receive.
func serveOnce(conn net.Conn, inputChan chan DomainData, outputChan chan DomainData) {
	udsChan := make(chan string, 4)
	done := make(chan struct{})
	go func() {
		udsReader(conn, inputChan, udsChan)
		close(done)
	}()
	writerDone := make(chan struct{})
	go func() {
		udsWriter(conn, udsChan)
		close(writerDone)
	}()
	defer func() {
		close(udsChan) // let udsWriter exit cleanly
		<-writerDone
	}()
	for {
		select {
		case outData := <-outputChan:
			if len(outData.Name) == 0 {
				continue
			}
			status := statestorageSet(outData.Name, outData.Value, utils.GetRfcTime())
			if status != 0 {
				utils.Error.Printf("initVSSInterfaceMgr():State storage write failed")
				continue
			}
			notificationMu.Lock()
			notify := onNotificationListLocked(outData.Name) != -1
			notificationMu.Unlock()
			if notify {
				message := `{"action": "subscription", "path":"` + outData.Name + `"}`
				select {
				case udsChan <- message:
				default:
					utils.Error.Printf("udsWriter:udsChan full, dropping subscription notification for %q", outData.Name)
				}
				utils.Info.Printf("Server notified that data written to %s", outData.Name)
			}
		case <-done:
			return
		}
	}
}

func statestorageSet(path string, val string, ts string) int {
	switch stateDbType {
	case "sqlite":
		stmt, err := dbHandle.Prepare("UPDATE VSS_MAP SET c_value=?, c_ts=? WHERE `path`=?")
		if err != nil {
			utils.Error.Printf("Could not prepare for statestorage updating, err = %s", err)
			return -1
		}
		defer stmt.Close()

		_, err = stmt.Exec(val, ts, path)
		if err != nil {
			utils.Error.Printf("Could not update statestorage, err = %s", err)
			return -1
		}
		return 0
	case "redis":
		dp, err := marshalDatapointJSON(val, ts)
		if err != nil {
			utils.Error.Printf("statestorageSet:Marshal error=%v", err)
			return -1
		}
		if err := redisClient.Set(path, dp, time.Duration(0)).Err(); err != nil {
			utils.Error.Printf("Job failed. Err=%s", err)
			return -1
		}
		return 0
	case "memcache":
		dp, err := marshalDatapointJSON(val, ts)
		if err != nil {
			utils.Error.Printf("statestorageSet:Marshal error=%v", err)
			return -1
		}
		if err := memcacheClient.Set(&memcache.Item{Key: path, Value: []byte(dp)}); err != nil {
			utils.Error.Printf("Job failed. Err=%s", err)
			return -1
		}
		return 0
	}
	return -1
}

func udsReader(conn net.Conn, inputChan chan DomainData, udsChan chan string) {
	defer conn.Close()
	buf := make([]byte, 8192)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			utils.Error.Printf("udsReader:Read failed, err = %s", err)
			if errors.Is(err, io.EOF) {
				return
			}
			time.Sleep(1 * time.Second)
			continue
		}
		utils.Info.Printf("udsReader:Message from server: %s", string(buf[:n]))
		// n cannot exceed len(buf); fill of the buffer suggests truncation.
		if n == len(buf) {
			utils.Error.Printf("udsReader: message at buffer size (%d); likely truncated, dropping", n)
			continue
		}
		handleServerMessage(buf[:n], inputChan, udsChan)
	}
}

// handleServerMessage parses one UDS message and dispatches by action. Extracted
// from udsReader so the dispatch logic is unit-testable and every type
// assertion uses comma-ok form. Pre-fix, ~10 bare type assertions on
// attacker-controlled JSON panicked the feeder.
func handleServerMessage(raw []byte, inputChan chan DomainData, udsChan chan string) {
	var serverMessageMap map[string]interface{}
	if err := json.Unmarshal(raw, &serverMessageMap); err != nil {
		utils.Error.Printf("udsReader:Unmarshal error=%s", err)
		return
	}
	action, ok := mapString(serverMessageMap, "action")
	if !ok {
		utils.Error.Printf("udsReader:Message missing/invalid action field")
		return
	}
	switch action {
	case "set":
		dataMap, ok := serverMessageMap["data"].(map[string]interface{})
		if !ok {
			utils.Error.Printf("udsReader:set message missing/invalid data field")
			return
		}
		domainData, _, ok := splitToDomainDataAndTs(dataMap)
		if !ok {
			return
		}
		select {
		case inputChan <- domainData:
		default:
			utils.Error.Printf("udsReader: inputChan full, dropping set for %q", domainData.Name)
		}
	case "subscribe":
		pathList, ok := serverMessageMap["path"].([]interface{})
		if !ok {
			utils.Error.Printf("udsReader:subscribe missing/invalid path array")
			return
		}
		addNotifications(pathList)
		select {
		case udsChan <- `{"action": "subscribe", "status": "ok"}`:
		default:
			utils.Error.Printf("udsReader: udsChan full, subscribe response dropped")
		}
	case "unsubscribe":
		pathList, ok := serverMessageMap["path"].([]interface{})
		if !ok {
			utils.Error.Printf("udsReader:unsubscribe missing/invalid path array")
			return
		}
		removeNotifications(pathList)
	case "update":
		defaultArray, ok := serverMessageMap["defaultList"].([]interface{})
		if !ok {
			utils.Error.Printf("udsReader:update missing/invalid defaultList array")
			return
		}
		for i := 0; i < len(defaultArray); i++ {
			elem, ok := defaultArray[i].(map[string]interface{})
			if !ok {
				utils.Error.Printf("udsReader:update element %d not an object", i)
				continue
			}
			path, _ := mapString(elem, "path")
			defVal, _ := mapString(elem, "default")
			if path == "" {
				continue
			}
			statestorageSet(path, defVal, utils.GetRfcTime())
		}
	default:
		utils.Error.Printf("udsReader:Message action unknown = %s", action)
	}
}

// addNotifications appends each string element of pathList to notificationList
// unless already present. Non-string elements are skipped.
func addNotifications(pathList []interface{}) {
	notificationMu.Lock()
	defer notificationMu.Unlock()
	for i := 0; i < len(pathList); i++ {
		p, ok := pathList[i].(string)
		if !ok {
			utils.Error.Printf("addNotifications: element %d not a string", i)
			continue
		}
		if onNotificationListLocked(p) == -1 {
			notificationList = append(notificationList, p)
		}
	}
}

// removeNotifications drops each path in pathList from notificationList.
// Bug fix: pre-fix code did `slices.Delete(notificationList, i, i+1)` using the
// input-pathList index `i` instead of the matching index in notificationList,
// deleting the wrong element. It also never decremented `i` after deletion so
// shifted entries were skipped. This is the v3 smoking gun.
func removeNotifications(pathList []interface{}) {
	notificationMu.Lock()
	defer notificationMu.Unlock()
	for i := 0; i < len(pathList); i++ {
		p, ok := pathList[i].(string)
		if !ok {
			utils.Error.Printf("removeNotifications: element %d not a string", i)
			continue
		}
		idx := onNotificationListLocked(p)
		if idx == -1 {
			continue
		}
		notificationList = slices.Delete(notificationList, idx, idx+1)
	}
}

func udsWriter(conn net.Conn, udsChan chan string) {
	for {
		message, ok := <-udsChan
		if !ok {
			return
		}
		utils.Info.Printf("udsWriter:Message to server: %s", message)
		if _, err := conn.Write([]byte(message)); err != nil {
			utils.Error.Printf("udsWriter:Write failed, err = %s", err)
		}
	}
}

// onNotificationList is the locking wrapper used by external callers (none in
// this file - retained for API parity with v4). onNotificationListLocked is
// what the rest of the file uses while already holding notificationMu.
func onNotificationList(path string) int {
	notificationMu.Lock()
	defer notificationMu.Unlock()
	return onNotificationListLocked(path)
}

func onNotificationListLocked(path string) int {
	for i := 0; i < len(notificationList); i++ {
		if notificationList[i] == path {
			return i
		}
	}
	return -1
}

// splitToDomainDataAndTs parses a server datapoint of the shape
//   {"dp": {"ts": "Z","value": "Y"},"path": "X"}
// and returns the extracted DomainData, the timestamp string, and ok=true.
// On any parse / shape error, returns zero values and ok=false (without
// panicking). Pre-fix this had four unchecked .(string)/.(map) assertions.
func splitToDomainDataAndTs(serverMessageMap map[string]interface{}) (DomainData, string, bool) {
	var domainData DomainData
	name, ok := mapString(serverMessageMap, "path")
	if !ok {
		utils.Error.Printf("splitToDomainDataAndTs: missing/invalid path")
		return domainData, "", false
	}
	dpMap, ok := serverMessageMap["dp"].(map[string]interface{})
	if !ok {
		utils.Error.Printf("splitToDomainDataAndTs: missing/invalid dp object")
		return domainData, "", false
	}
	value, ok := mapString(dpMap, "value")
	if !ok {
		utils.Error.Printf("splitToDomainDataAndTs: missing/invalid value")
		return domainData, "", false
	}
	ts, ok := mapString(dpMap, "ts")
	if !ok {
		utils.Error.Printf("splitToDomainDataAndTs: missing/invalid ts")
		return domainData, "", false
	}
	domainData.Name = name
	domainData.Value = value
	return domainData, ts, true
}

type simulateDataCtx struct {
	RandomSim bool        // true=random, false=stepwise change of signal written to
	Fmap      []FeederMap // used for random simulation
	Path      string      // signal written to
	SetVal    string      // value written
	Iteration int
}

type ActuatorSimCtx struct {
	RemainingSteps int
	CurrVal        string
	EndVal         string
	Path           string
}

func initVehicleInterfaceMgr(fMap []FeederMap, inputChan chan DomainData, outputChan chan DomainData) {
	aSimCtx := make([]ActuatorSimCtx, 5) // max 5 signals can be simulated in parallel
	var simCtx simulateDataCtx
	simCtx.RandomSim = true
	simCtx.Fmap = fMap
	dpIndex := 0
	for {
		select {
		case outData := <-outputChan:
			utils.Info.Printf("Data for calling the vehicle interface: Name=%s, Value=%s", outData.Name, outData.Value)
			if simulatedSource == "internal" {
				simCtx.RandomSim = false
				simCtx.Path = outData.Name
				simCtx.SetVal = outData.Value
				simCtx.Iteration = 0
			} else { // simulate actuation over a time period
				simIndex := getSimulatorContainer(aSimCtx, outData.Name)
				if simIndex == -1 {
					utils.Info.Printf("initVehicleInterfaceMgr: max parallel simulations reached")
					continue
				}
				aSimCtx[simIndex].RemainingSteps = 10
				aSimCtx[simIndex].CurrVal = "0" // well....
				aSimCtx[simIndex].EndVal = outData.Value
				aSimCtx[simIndex].Path = outData.Name
			}

		default:
			if simulatedSource == "internal" {
				time.Sleep(3 * time.Second)         // not to overload input channel
				inputChan <- simulateInput(&simCtx) // simulating signals read from the vehicle interface
			} else {
				time.Sleep(1 * time.Second) // set to the tripdata "time base"
				dataPoint := getSimulatedDataPoints(dpIndex)
				for i := 0; i < len(dataPoint); i++ {
					inputChan <- dataPoint[i]
				}
				dpIndex = incDpIndex(dpIndex)
				for i := 0; i < len(aSimCtx); i++ {
					if aSimCtx[i].RemainingSteps > 0 {
						select {
						case inputChan <- makeDataPoint(aSimCtx[i].Path, calculateSimValue(&(aSimCtx[i]))):
							if aSimCtx[i].RemainingSteps == 0 {
								aSimCtx[i].Path = ""
							}
						default:
							utils.Info.Printf("initVehicleInterfaceMgr: dropping dp")
						}
					}
				}
			}
		}
	}
}

func getSimulatorContainer(aSimCtx []ActuatorSimCtx, path string) int {
	for i := 0; i < len(aSimCtx); i++ {
		if aSimCtx[i].Path == path {
			return i // restart actuation
		}
	}
	for i := 0; i < len(aSimCtx); i++ {
		if aSimCtx[i].RemainingSteps == 0 {
			return i
		}
	}
	return -1
}

// calculateSimValue pre-fix performed `step := (endValue - currValue) / noOfSteps`
// in integer arithmetic; if the delta was smaller than the step count the
// result truncated to 0 and CurrVal never moved. It also used FormatFloat
// bitSize=32 on a float64 (precision loss). Both fixed below: integer ramps
// distribute the remainder, float ramps use bitSize=64.
func calculateSimValue(aSimCtx *ActuatorSimCtx) string {
	noOfSteps := aSimCtx.RemainingSteps
	if noOfSteps <= 0 {
		return aSimCtx.CurrVal
	}
	aSimCtx.RemainingSteps--
	endValue, err := strconv.Atoi(aSimCtx.EndVal)
	if err != nil {
		endValueF, err := strconv.ParseFloat(aSimCtx.EndVal, 64)
		if err != nil {
			aSimCtx.RemainingSteps = 0
			return aSimCtx.EndVal
		}
		currValueF, err := strconv.ParseFloat(aSimCtx.CurrVal, 64)
		if err != nil {
			aSimCtx.RemainingSteps = 0
			return aSimCtx.EndVal
		}
		step := (endValueF - currValueF) / float64(noOfSteps)
		aSimCtx.CurrVal = strconv.FormatFloat(currValueF+step, 'f', -1, 64)
		return aSimCtx.CurrVal
	}
	currValue, err := strconv.Atoi(aSimCtx.CurrVal)
	if err != nil {
		aSimCtx.RemainingSteps = 0
		return aSimCtx.EndVal
	}
	// Integer ramp: on the final step jump to endValue so int-truncation can't
	// strand the simulation short of its target.
	if aSimCtx.RemainingSteps == 0 {
		aSimCtx.CurrVal = strconv.Itoa(endValue)
		return aSimCtx.CurrVal
	}
	step := (endValue - currValue) / noOfSteps
	if step == 0 && endValue != currValue {
		// Nudge by 1 in the right direction so we make forward progress
		// instead of stalling at currValue.
		if endValue > currValue {
			step = 1
		} else {
			step = -1
		}
	}
	aSimCtx.CurrVal = strconv.Itoa(currValue + step)
	return aSimCtx.CurrVal
}

func makeDataPoint(path string, value string) DomainData {
	var dataPoint DomainData
	dataPoint.Name = path
	dataPoint.Value = value
	return dataPoint
}

func simulateInput(simCtx *simulateDataCtx) DomainData {
	var input DomainData
	if simCtx.RandomSim == true {
		return selectRandomInput(simCtx.Fmap)
	}
	if simCtx.Iteration == 10 {
		simCtx.RandomSim = true
	}
	input.Name = simCtx.Path
	input.Value = calcInputValue(simCtx.Iteration, simCtx.SetVal)
	simCtx.Iteration++
	return input
}

func calcInputValue(iteration int, setValue string) string {
	setVal, err := strconv.Atoi(setValue)
	if err != nil {
		utils.Error.Printf("calcInputValue:setValue=%q not an integer: %v", setValue, err)
	}
	newVal := setVal - 10 + iteration
	return strconv.Itoa(newVal)
}

func selectRandomInput(fMap []FeederMap) DomainData {
	var domainData DomainData
	signalIndex := getRandomVssfMapIndex(fMap)
	if signalIndex < 0 || signalIndex >= len(fMap) {
		return domainData
	}
	domainData.Name = fMap[signalIndex].Name
	if fMap[signalIndex].Datatype == 0 { // uint8, maybe allowed...
		domainData.Value = strconv.Itoa(rand.Intn(10))
	} else if fMap[signalIndex].Datatype == 9 { // double, maybe lat/long
		domainData.Value = strconv.Itoa(rand.Intn(90))
	} else if fMap[signalIndex].Datatype == 10 { // bool
		domainData.Value = strconv.Itoa(rand.Intn(2))
	} else {
		domainData.Value = strconv.Itoa(rand.Intn(1000))
	}
	return domainData
}

// getRandomVssfMapIndex picks an entry without a dot in the Name (i.e. a
// vehicle-side leaf). Returns -1 if fMap is empty or contains no qualifying
// entries. Pre-fix used `% (len(fMap)-1)` which skipped the last slot and
// divided by zero when len(fMap)==1; if every name contained a dot it
// looped forever.
func getRandomVssfMapIndex(fMap []FeederMap) int {
	if len(fMap) == 0 {
		return -1
	}
	signalIndex := rand.Intn(len(fMap))
	for attempts := 0; attempts < len(fMap); attempts++ {
		if !strings.Contains(fMap[signalIndex].Name, ".") {
			return signalIndex
		}
		signalIndex = (signalIndex + 1) % len(fMap)
	}
	return -1
}

func readSimulatedData(fname string) []DataItem {
	if !fileExists(fname) {
		utils.Error.Printf("readSimulatedData: The file %s does not exist.", fname)
		return nil
	}
	data, err := os.ReadFile(fname)
	if err != nil {
		utils.Error.Printf("readSimulatedData:Error reading %s: %s", fname, err)
		return nil
	}
	err = json.Unmarshal([]byte(data), &tripData)
	if err != nil {
		utils.Error.Printf("readSimulatedData:Error unmarshal json=%s", err)
		return nil
	}
	return tripData
}

// getSimulatedDataPoints returns one datapoint per tripData path. Skips paths
// whose per-path Dp slice is shorter than dpIndex+1 (rather than panicking,
// which the pre-fix code did the moment any per-path row was shorter than
// tripData[0].Dp).
func getSimulatedDataPoints(dpIndex int) []DomainData {
	dataPoints := make([]DomainData, 0, len(tripData))
	for i := 0; i < len(tripData); i++ {
		if dpIndex < 0 || dpIndex >= len(tripData[i].Dp) {
			continue
		}
		dataPoints = append(dataPoints, DomainData{
			Name:  tripData[i].Path,
			Value: tripData[i].Dp[dpIndex].Value,
		})
	}
	return dataPoints
}

func incDpIndex(index int) int {
	if len(tripData) == 0 || len(tripData[0].Dp) == 0 {
		return 0
	}
	index++
	if index >= len(tripData[0].Dp) {
		return 0
	}
	return index
}

func convertDomainData(north2SouthConv bool, inData DomainData, feederMap []FeederMap) DomainData {
	var outData DomainData
	if len(feederMap) == 0 || inData.Name == "" {
		return inData
	}
	matchIndex := sort.Search(len(feederMap), func(i int) bool { return feederMap[i].Name >= inData.Name })
	if matchIndex == len(feederMap) || feederMap[matchIndex].Name != inData.Name {
		utils.Error.Printf("convertDomainData:Failed to map= %s", inData.Name)
		return inData //assume 1-to-1...
	}
	mapIdx := int(feederMap[matchIndex].MapIndex)
	if mapIdx < 0 || mapIdx >= len(feederMap) {
		utils.Error.Printf("convertDomainData:MapIndex %d for %q out of range [0,%d)", mapIdx, inData.Name, len(feederMap))
		return inData
	}
	outData.Name = feederMap[mapIdx].Name
	outData.Value = convertValue(inData.Value, feederMap[matchIndex].ConvertIndex,
		feederMap[matchIndex].Datatype, feederMap[mapIdx].Datatype, north2SouthConv)
	return outData
}

// convertValue pre-fix indexed scalingDataList[convertIndex-1] without a
// bounds check (panic on out-of-range / nil list), and its type switch had a
// `case interface{}` arm that matched every type, then blindly asserted
// `.([]interface{})` and panicked on strings/numbers.
func convertValue(value string, convertIndex uint16, inDatatype int8, outDatatype int8, north2SouthConv bool) string {
	if convertIndex == 0 { // no conversion
		return value
	}
	idx := int(convertIndex) - 1
	if idx < 0 || idx >= len(scalingDataList) {
		utils.Error.Printf("convertValue: convertIndex %d out of range for scalingDataList(len=%d)", convertIndex, len(scalingDataList))
		return ""
	}
	var convertDataMap interface{}
	if err := json.Unmarshal([]byte(scalingDataList[idx]), &convertDataMap); err != nil {
		utils.Error.Printf("convertValue:Error unmarshal scalingDataList item=%s", scalingDataList[idx])
		return ""
	}
	switch vv := convertDataMap.(type) {
	case map[string]interface{}:
		return enumConversion(vv, north2SouthConv, value)
	case []interface{}:
		return linearConversion(vv, north2SouthConv, value)
	default:
		utils.Error.Printf("convertValue: convert data=%s has unknown format (got %T)", scalingDataList[idx], convertDataMap)
		return ""
	}
}

func enumConversion(enumObj map[string]interface{}, north2SouthConv bool, inValue string) string { // enumObj = {"Key1":"value1", .., "KeyN":"valueN"}, k is VSS value
	for k, v := range enumObj {
		s, ok := v.(string)
		if !ok {
			utils.Error.Printf("enumConversion: non-string value for key %q (got %T)", k, v)
			continue
		}
		if north2SouthConv {
			if k == inValue {
				return s
			}
		} else {
			if s == inValue {
				return k
			}
		}
	}
	utils.Error.Printf("enumConversion: value=%s is out of range.", inValue)
	return ""
}

func linearConversion(coeffArray []interface{}, north2SouthConv bool, inValue string) string { // coeffArray = [A, B], y = Ax +B, y is VSS value
	if len(coeffArray) < 2 {
		utils.Error.Printf("linearConversion: coefficient array too short (len=%d, need 2)", len(coeffArray))
		return ""
	}
	x, err := strconv.ParseFloat(inValue, 64)
	if err != nil {
		utils.Error.Printf("linearConversion: input value=%s cannot be converted to float.", inValue)
		return ""
	}
	A, okA := coeffArray[0].(float64)
	B, okB := coeffArray[1].(float64)
	if !okA || !okB {
		utils.Error.Printf("linearConversion: coefficients must be numbers (got A=%T, B=%T)", coeffArray[0], coeffArray[1])
		return ""
	}
	var y float64
	if north2SouthConv {
		y = A*x + B
	} else {
		if A == 0 {
			utils.Error.Printf("linearConversion: south-to-north divide-by-zero (A=0)")
			return ""
		}
		y = (x - B) / A
	}
	return strconv.FormatFloat(y, 'f', -1, 64)
}

func main() {
	// Create new parser object
	parser := argparse.NewParser("print", "Data feeder template version 3")
	mapFile := parser.String("m", "mapfile", &argparse.Options{
		Required: false,
		Help:     "VSS-Vehicle mapping data filename",
		Default:  "VssVehicle.cvt"})
	sclDataFile := parser.String("s", "scldatafile", &argparse.Options{
		Required: false,
		Help:     "VSS-Vehicle scaling data filename",
		Default:  "VssVehicleScaling.json"})
	tripDataFile := parser.String("t", "tripdatafile", &argparse.Options{
		Required: false,
		Help:     "Filename for simulated trip data",
		Default:  "tripdata.json"})
	logFile := parser.Flag("", "logfile", &argparse.Options{Required: false, Help: "outputs to logfile in ./logs folder"})
	logLevel := parser.Selector("", "loglevel", []string{"trace", "debug", "info", "warn", "error", "fatal", "panic"}, &argparse.Options{
		Required: false,
		Help:     "changes log output level",
		Default:  "info"})
	simSource := parser.Selector("i", "simsource", []string{"vssjson", "internal"}, &argparse.Options{Required: false,
		Help: "Simulator source must be either vssjson, or internal", Default: "internal"}) // "vehiclejson" could be added for non-converted simulator data
	stateDB := parser.Selector("d", "statestorage", []string{"sqlite", "redis", "memcache", "none"}, &argparse.Options{Required: false,
		Help: "Statestorage must be either sqlite, redis, memcache, or none", Default: "redis"})
	dbFile := parser.String("f", "dbfile", &argparse.Options{
		Required: false,
		Help:     "statestorage database filename",
		Default:  "../../server/vissv2server/serviceMgr/statestorage.db"})
	// Parse input. Pre-fix this only logged the error and continued running
	// with whatever defaults / zero values had been left in place.
	err := parser.Parse(os.Args)
	if err != nil {
		utils.Error.Print(parser.Usage(err))
		os.Exit(1)
	}
	stateDbType = *stateDB
	simulatedSource = *simSource

	utils.InitLog("feeder-log.txt", "./logs", *logFile, *logLevel)

	switch stateDbType {
	case "sqlite":
		var dbErr error
		if utils.FileExists(*dbFile) {
			dbHandle, dbErr = sql.Open("sqlite3", *dbFile)
			if dbErr != nil {
				utils.Error.Printf("Could not open state storage file = %s, err = %s", *dbFile, dbErr)
				os.Exit(1)
			} else {
				utils.Info.Printf("SQLite state storage initialised.")
			}
		} else {
			utils.Error.Printf("Could not find state storage file = %s", *dbFile)
			os.Exit(1)
		}
	case "redis":
		redisClient = redis.NewClient(&redis.Options{
			Network:  "unix",
			Addr:     "/var/tmp/vissv2/redisDB.sock",
			Password: "",
			DB:       1,
		})
		err := redisClient.Ping().Err()
		if err != nil {
			utils.Error.Printf("Could not initialise redis DB, err = %s", err)
			os.Exit(1)
		} else {
			utils.Info.Printf("Redis state storage initialised.")
		}
	case "memcache":
		memcacheClient = memcache.New("/var/tmp/vissv2/memcacheDB.sock")
		err := memcacheClient.Ping()
		if err != nil {
			utils.Info.Printf("Memcache daemon not alive. Trying to start it")
			cmd := exec.Command("/usr/bin/bash", "memcacheNativeInit.sh")
			err := cmd.Run()
			if err != nil {
				utils.Error.Printf("Memcache daemon startup failed, err=%s", err)
				os.Exit(1)
			}
		}
		utils.Info.Printf("Memcache daemon alive.")
	default:
		utils.Error.Printf("Unknown state storage type = %s", stateDbType)
		os.Exit(1)
	}

	vssInputChan := make(chan DomainData, 3)
	vssOutputChan := make(chan DomainData, 3)
	vehicleInputChan := make(chan DomainData, 3)
	vehicleOutputChan := make(chan DomainData, 3)

	utils.Info.Printf("Initializing the feeder for mapping file %s.", *mapFile)
	feederMap := readFeederMap(*mapFile)
	if len(feederMap) == 0 {
		utils.Error.Printf("Empty/unreadable feeder map %s.", *mapFile)
		os.Exit(1)
	}
	if simulatedSource != "internal" {
		tripData = readSimulatedData(*tripDataFile)
		if len(tripData) == 0 {
			utils.Error.Printf("Tripdata file not found.")
			os.Exit(1)
		}
	}
	scalingDataList = readscalingDataList(*sclDataFile)
	go initVSSInterfaceMgr(vssInputChan, vssOutputChan)
	go initVehicleInterfaceMgr(feederMap, vehicleInputChan, vehicleOutputChan)

	for {
		select {
		case vssInData := <-vssInputChan:
			vehicleOutputChan <- convertDomainData(true, vssInData, feederMap)
		case vehicleInData := <-vehicleInputChan:
			if simulatedSource != "vssjson" {
				vssOutputChan <- convertDomainData(false, vehicleInData, feederMap)
			} else {
				vssOutputChan <- vehicleInData // conversion not needed
			}
		}
	}
}
