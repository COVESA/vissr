/**
* (C) 2023 Ford Motor Company
* (C) 2022 Geotab Inc
* (C) 2021 Mitsubishi Electrics Automotive
* (C) 2019 Geotab Inc
* (C) 2019 Volvo Cars
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE file in this repository.
*
**/

package serviceMgr

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/apache/iotdb-client-go/client"
	"github.com/bradfitz/gomemcache/memcache"
	"github.com/covesa/vissr/utils"
	"github.com/go-redis/redis"
	_ "github.com/mattn/go-sqlite3"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"slices"
	"time"
)

type RegRequest struct {
	Rootnode string
}

type SubscriptionState struct {
	SubscriptionId      int
	SubscriptionThreads int //only used by subs that spawn multiple threads that return notifications
	RouterId            string
	Path                []string
	FilterList          []utils.FilterObject
	LatestDataPoint     string
	GatingId            string
}

var subscriptionId int

type HistoryList struct {
	Path      string
	Frequency int
	BufSize   int
	Status    int
	BufIndex  int // points to next empty buffer element
	Buffer    []string
}

var historyList []HistoryList
var historyAccessChannel chan string

// for feeder notifications
var toFeeder chan string       // requests
var fromFeederRorC chan string //range and change messages
var fromFeederCl chan string   // curvelog messages

type FeederSubElem struct {
	SubscriptionId string
	Path []string
	Variant string
}
var feederSubList []FeederSubElem

type FeederPathElem struct {
	Path string
	Reference int
}
var feederPathList []FeederPathElem
var feederConnected bool

//var feederConn net.Conn
//var hostIp string

var errorResponseMap = map[string]interface{}{
	"RouterId":  "0?0",
	"action":    "unknown",
	"requestId": "XX",
	"error":     `{"number":AA, "reason": "BB", "message": "CC"}`,
	"ts":        "yy",
}

var dbHandle *sql.DB
var dbErr error
var redisClient *redis.Client
var memcacheClient *memcache.Client
var stateDbType string
var historySupport bool

// Apache IoTDB
var IoTDBsession client.Session
var IoTDBClientConfig = &client.Config{
	Host:     "127.0.0.1",
	Port:     "6667",
	UserName: "root",
	Password: "root",
}

type IoTDBConfiguration struct {
	Host       string `json:"host"`
	Port       string `json:"port"`
	UserName   string `json:"username"`
	Password   string `json:"password"`
	PrefixPath string `json:"queryPrefixPath"`
	Timeout    int64  `json:"queryTimeout(ms)"`
}

// Default IoTDB connector configuration
var IoTDBConfig = IoTDBConfiguration{
	"127.0.0.1",
	"6667",
	"root",
	"root",
	"root.test2.dev1",
	5000,
}

var dummyValue int // dummy value returned when DB configured to none. Counts from 0 to 999, wrap around, updated every 47 msec

func initDataServer(serviceMgrChan chan map[string]interface{}, clientChannel chan map[string]interface{}, backendChannel chan map[string]interface{}) {
	for {
		select {
		case request := <-serviceMgrChan:
//			utils.Info.Printf("Service mgr request: %s", request)

			clientChannel <- request                                              // forward to mgr hub,
			if request["action"] != "internal-killsubscriptions" { // no response on kill sub
				response := <-clientChannel //  and wait for response
//				utils.Info.Printf("Service mgr response: %s", response)
				serviceMgrChan <- response
			}
		case notification := <-backendChannel: // notification
			utils.Info.Printf("Service mgr notification: %s", notification)
			serviceMgrChan <- notification
		}
	}
}

const MAXTICKERS = 255 // total number of active subscription and history tickers
var subscriptionTicker [MAXTICKERS]*time.Ticker
var historyTicker [MAXTICKERS]*time.Ticker
var tickerIndexList [MAXTICKERS]int

func allocateTicker(subscriptionId int) int {
	for i := 0; i < len(tickerIndexList); i++ {
		if tickerIndexList[i] == 0 {
			tickerIndexList[i] = subscriptionId
			return i
		}
	}
	return -1
}

func deallocateTicker(subscriptionId int) int {
	for i := 0; i < len(tickerIndexList); i++ {
		if tickerIndexList[i] == subscriptionId {
			tickerIndexList[i] = 0
			return i
		}
	}
	return -1
}

func activateInterval(subscriptionChannel chan int, subscriptionId int, interval int) {
	index := allocateTicker(subscriptionId)
	if index == -1 {
		utils.Error.Printf("activateInterval: No available ticker.")
		return
	}
	subscriptionTicker[index] = time.NewTicker(time.Duration(interval) * time.Millisecond) // interval in milliseconds
	go func() {
		for range subscriptionTicker[index].C {
			subscriptionChannel <- subscriptionId
		}
	}()
}

func deactivateInterval(subscriptionId int) {
	subscriptionTicker[deallocateTicker(subscriptionId)].Stop()
}

func activateHistory(historyChannel chan int, signalId int, frequency int) {
	index := allocateTicker(signalId)
	if index == -1 {
		utils.Error.Printf("activateHistory: No available ticker.")
		return
	}
	historyTicker[index] = time.NewTicker(time.Duration((3600*1000)/frequency) * time.Millisecond) // freq in cycles per hour
	go func() {
		for range historyTicker[index].C {
			historyChannel <- signalId
		}
	}()
}

func deactivateHistory(signalId int) {
	historyTicker[deallocateTicker(signalId)].Stop()
}

func getSubcriptionStateIndex(subscriptionId int, subscriptionList []SubscriptionState) int {
	index := -1
	for i := 0; i < len(subscriptionList); i++ {
		if subscriptionList[i].SubscriptionId == subscriptionId {
			index = i
			break
		}
	}
	return index
}

func setSubscriptionListThreads(subscriptionList []SubscriptionState, subThreads SubThreads) []SubscriptionState {
	index := getSubcriptionStateIndex(subThreads.SubscriptionId, subscriptionList)
	subscriptionList[index].SubscriptionThreads = subThreads.NumofThreads
	return subscriptionList
}

func checkRangeChangeFilter(filterList []utils.FilterObject, latestDataPoint string, path string) (bool, string) {
	for i := 0; i < len(filterList); i++ {
		if filterList[i].Type == "paths" || filterList[i].Type == "timebased" || filterList[i].Type == "curvelog" {
			continue
		}
		currentDataPoint := getVehicleData(path)
		if filterList[i].Type == "range" {
			return evaluateRangeFilter(filterList[i].Parameter, getDPValue(currentDataPoint)), currentDataPoint // do not update latestValue
		}
		if filterList[i].Type == "change" {
			return evaluateChangeFilter(filterList[i].Parameter, getDPValue(latestDataPoint), getDPValue(currentDataPoint), currentDataPoint)
		}
	}
	return false, ""
}

func getDPValue(dp string) string {
	value, _ := unpackDataPoint(dp)
	return value
}

func getDPTs(dp string) string {
	_, ts := unpackDataPoint(dp)
	return ts
}

func unpackDataPoint(dp string) (string, string) { // {"value":"Y", "ts":"Z"}
	type DataPoint struct {
		Value string `json:"value"`
		Ts    string `json:"ts"`
	}
	var dataPoint DataPoint
	err := json.Unmarshal([]byte(dp), &dataPoint)
	if err != nil {
		utils.Error.Printf("unpackDataPoint: Unmarshal failed for dp=%s, error=%s", dp, err)
		return "", ""
	}
	return dataPoint.Value, dataPoint.Ts
}

func evaluateRangeFilter(opValue string, currentValue string) bool {
	//utils.Info.Printf("evaluateRangeFilter: opValue=%s", opValue)
	type RangeFilter struct {
		LogicOp  string `json:"logic-op"`
		Boundary string `json:"boundary"`
	}
	var rangeFilter []RangeFilter
	var err error
	if strings.Contains(opValue, "[") == false {
		rangeFilter = make([]RangeFilter, 1)
		err = json.Unmarshal([]byte(opValue), &(rangeFilter[0]))
	} else {
		err = json.Unmarshal([]byte(opValue), &rangeFilter)
	}
	if err != nil {
		utils.Error.Printf("evaluateRangeFilter: Unmarshal error=%s", err)
		return false
	}
	evaluation := true
	for i := 0; i < len(rangeFilter); i++ {
		eval := compareValues(rangeFilter[i].LogicOp, rangeFilter[i].Boundary, currentValue, "0") // currVal - 0 logic-op boundary
		evaluation = evaluation && eval
	}
	return evaluation
}

func evaluateChangeFilter(opValue string, latestValue string, currentValue string, currentDataPoint string) (bool, string) {
	//utils.Info.Printf("evaluateChangeFilter: opValue=%s", opValue)
	type ChangeFilter struct {
		LogicOp string `json:"logic-op"`
		Diff    string `json:"diff"`
	}
	var changeFilter ChangeFilter
	err := json.Unmarshal([]byte(opValue), &changeFilter)
	if err != nil {
		utils.Error.Printf("evaluateChangeFilter: Unmarshal error=%s", err)
		return false, ""
	}
	val1 := compareValues(changeFilter.LogicOp, latestValue, currentValue, changeFilter.Diff)
	return val1, currentDataPoint
}

func compareValues(logicOp string, latestValue string, currentValue string, diff string) bool {
	latestValueType := utils.AnalyzeValueType(latestValue)
	if latestValueType != utils.AnalyzeValueType(currentValue) {
		utils.Error.Printf("compareValues: Incompatible types, latVal=%s, curVal=%s", latestValue, currentValue)
		return false
	}
	switch latestValueType {
	case 0:
		fallthrough // string
	case 2: // bool
		if diff != "0" {
			utils.Error.Printf("compareValues: invalid parameter for boolean type")
			return false
		}
		switch logicOp {
		case "eq":
			return currentValue == latestValue
		case "ne":
			return currentValue != latestValue // true->false OR false->true
		case "gt":
			return latestValue == "false" && currentValue != latestValue // false->true
		case "lt":
			return latestValue == "true" && currentValue != latestValue // true->false
		}
		return false
	case 1: // int
		curVal, err := strconv.Atoi(currentValue)
		if err != nil {
			return false
		}
		latVal, err := strconv.Atoi(latestValue)
		if err != nil {
			return false
		}
		diffVal, err := strconv.Atoi(diff)
		if err != nil {
			return false
		}
//		utils.Info.Printf("compareValues: value type=integer, cv=%d, lv=%d, diff=%d, logicOp=%s", curVal, latVal, diffVal, logicOp)
		switch logicOp {
		case "eq":
			return curVal-diffVal == latVal
		case "ne":
			return curVal-diffVal != latVal
		case "gt":
			return curVal-diffVal > latVal
		case "gte":
			return curVal-diffVal >= latVal
		case "lt":
			return curVal-diffVal < latVal
		case "lte":
			return curVal-diffVal <= latVal
		}
		return false
	case 3: // float
		f64Val, err := strconv.ParseFloat(currentValue, 32)
		if err != nil {
			return false
		}
		curVal := float32(f64Val)
		f64Val, err = strconv.ParseFloat(latestValue, 32)
		if err != nil {
			return false
		}
		latVal := float32(f64Val)
		f64Val, err = strconv.ParseFloat(diff, 32)
		if err != nil {
			return false
		}
		diffVal := float32(f64Val)
		//utils.Info.Printf("compareValues: value type=float, cv=%d, lv=%d, diff=%d, logicOp=%s", curVal, latVal, diffVal, logicOp)
		switch logicOp {
		case "eq":
			return curVal-diffVal == latVal
		case "ne":
			return curVal-diffVal != latVal
		case "gt":
			return curVal-diffVal > latVal
		case "gte":
			return curVal-diffVal >= latVal
		case "lt":
			return curVal-diffVal < latVal
		case "lte":
			return curVal-diffVal <= latVal
		}
		return false
	}
	return false
}

func deactivateSubscription(subscriptionList []SubscriptionState, subscriptionId string) (int, []SubscriptionState) {
	id, _ := strconv.Atoi(subscriptionId)
	index := getSubcriptionStateIndex(id, subscriptionList)
	if index == -1 {
		return -1, subscriptionList
	}
	if getOpType(subscriptionList[index].FilterList, "timebased") == true {
		deactivateInterval(subscriptionList[index].SubscriptionId)
	} else if getOpType(subscriptionList[index].FilterList, "curvelog") == true {
		mcloseClSubId.Lock()
		closeClSubId = subscriptionList[index].SubscriptionId
		utils.Info.Printf("deactivateSubscription: closeClSubId set to %d", closeClSubId)
		mcloseClSubId.Unlock()
	}
	subscriptionList = removeFromsubscriptionList(subscriptionList, index)
	return 1, subscriptionList
}

func removeFromsubscriptionList(subscriptionList []SubscriptionState, index int) []SubscriptionState {
	subscriptionList[index] = subscriptionList[len(subscriptionList)-1] // Copy last element to index i.
	subscriptionList = subscriptionList[:len(subscriptionList)-1]       // Truncate slice.
	utils.Info.Printf("Killed subscription, listno=%d", index)
	return subscriptionList
}

func getOpType(filterList []utils.FilterObject, opType string) bool {
	for i := 0; i < len(filterList); i++ {
		if filterList[i].Type == opType {
			return true
		}
	}
	return false
}

func getIntervalPeriod(opValue string) int { // {"period":"X"}
	type IntervalData struct {
		Period string `json:"period"`
	}
	var intervalData IntervalData
	err := json.Unmarshal([]byte(opValue), &intervalData)
	if err != nil {
		utils.Error.Printf("getIntervalPeriod: Unmarshal failed, err=%s", err)
		return -1
	}
	period, err := strconv.Atoi(intervalData.Period)
	if err != nil {
		utils.Error.Printf("getIntervalPeriod: Invalid period=%s", period)
		return -1
	}
	return period
}

func getCurveLoggingParams(opValue string) (float64, int) { // {"maxerr": "X", "bufsize":"Y"}
	type CLData struct {
		MaxErr  string `json:"maxerr"`
		BufSize string `json:"bufsize"`
	}
	var cLData CLData
	err := json.Unmarshal([]byte(opValue), &cLData)
	if err != nil {
		utils.Error.Printf("getIntervalPeriod: Unmarshal failed, err=%s", err)
		return 0.0, 0
	}
	maxErr, err := strconv.ParseFloat(cLData.MaxErr, 64)
	if err != nil {
		utils.Error.Printf("getIntervalPeriod: MaxErr invalid integer, maxErr=%s", cLData.MaxErr)
		maxErr = 0.0
	}
	bufSize, err := strconv.Atoi(cLData.BufSize)
	if err != nil {
		utils.Error.Printf("getIntervalPeriod: BufSize invalid integer, BufSize=%s", cLData.BufSize)
		maxErr = 0.0
	}
	return maxErr, bufSize
}

func createFeederNotifyMessage(variant string, pathList []string, subscriptionId int) string {
	paths := `["`
	for i := 0; i < len(pathList); i++ {
		paths += pathList[i] + `", "`
	}
	paths = paths[:len(paths)-3] + "]"
	return `{"action": "subscribe", "variant": "` + variant + `", "path": ` + paths + `, "subscriptionId": "` + strconv.Itoa(subscriptionId) + `"}`
}

func getFeederNotifyType(filterList []utils.FilterObject) string {
	for i := 0; i < len(filterList); i++ {
		if filterList[i].Type == "curvelog" || filterList[i].Type == "change" || filterList[i].Type == "range" {
			return filterList[i].Type
		}
	}
	return ""
}

func activateIfIntervalOrCL(filterList []utils.FilterObject, subscriptionChan chan int, CLChan chan CLPack, subscriptionId int, paths []string, subscriptionList []SubscriptionState) []SubscriptionState {
	for i := 0; i < len(filterList); i++ {
		if filterList[i].Type == "timebased" {
			interval := getIntervalPeriod(filterList[i].Parameter)
			utils.Info.Printf("interval activated, period=%d", interval)
			if interval > 0 {
				activateInterval(subscriptionChan, subscriptionId, interval)
			}
			break
		}
		if filterList[i].Type == "curvelog" {
			clRoutingList, subThreads := curveLoggingDispatcher(CLChan, subscriptionId, filterList[i].Parameter, paths)
			subscriptionList = setSubscriptionListThreads(subscriptionList, subThreads)
			clRouterChan <- clRoutingList
			break
		}
	}
	return subscriptionList
}

func getVehicleData(path string) string { // returns {"value":"Y", "ts":"Z"}
	switch stateDbType {
	case "sqlite":

		rows, err := dbHandle.Query("SELECT `c_value`, `c_ts` FROM VSS_MAP WHERE `path`=?", path)
		if err != nil {
			return `{"value":"Data-error", "ts":"` + utils.GetRfcTime() + `"}`
		}
		defer rows.Close()
		value := ""
		timestamp := ""

		rows.Next()
		err = rows.Scan(&value, &timestamp)
		if err != nil {
			utils.Warning.Printf("Data not found: %s for path=%s", err, path)
			return `{"value":"Data-not-available", "ts":"` + utils.GetRfcTime() + `"}`
		}
		return `{"value":"` + value + `", "ts":"` + timestamp + `"}`
	case "redis":
		utils.Info.Printf(path)
		dp, err := redisClient.Get(path).Result()
		if err != nil {
			if err.Error() != "redis: nil" {
				utils.Error.Printf("Job failed. Error()=%s", err.Error())
				return `{"value":"Database-error", "ts":"` + utils.GetRfcTime() + `"}`
			} else {
				utils.Warning.Printf("Data not found.")
				return `{"value":"Data-not-found", "ts":"` + utils.GetRfcTime() + `"}`
			}
		} else {
			return dp
		}
	case "apache-iotdb":
		var (
			// Back-quote the VSS node for the DB query, e.g. `Vehicle.CurrentLocation.Longitude`
			selectLastSQL = fmt.Sprintf("select last `%v` from %v", path, IoTDBConfig.PrefixPath)
			value         = ""
			ts            = ""
		)
		//		utils.Info.Printf("IoTDB: query using: %v", selectLastSQL)
		sessionDataSet, err := IoTDBsession.ExecuteQueryStatement(selectLastSQL, &IoTDBConfig.Timeout)
		if err == nil {
			var success bool
			success, err = sessionDataSet.Next()
			if err == nil && success {
				value = sessionDataSet.GetText("Value")
				ts = sessionDataSet.GetText(client.TimestampColumnName)
				//				utils.Info.Printf("IoTDB: get returned: ts=%v, Value=%v", ts, value)
				//				resultStr := `{"value":"` + value + `", "ts":"` + ts + `"}`
				//				utils.Info.Printf("IoTDB: returning get result=%v", resultStr)
			}
			sessionDataSet.Close()
		} else {
			utils.Error.Printf("IoTDB: Query failed with error=%s", err)
			return `{"value":"Data-not-found", "ts":"` + utils.GetRfcTime() + `"}`
		}
		return `{"value":"` + value + `", "ts":"` + ts + `"}`
	case "memcache":
		mcItem, err := memcacheClient.Get(path)
		if err != nil {
			if err.Error() != "memcache: cache miss" {
				utils.Error.Printf("Job failed. Error()=%s", err.Error())
				return `{"value":"Database-error", "ts":"` + utils.GetRfcTime() + `"}`
			} else {
				utils.Warning.Printf("Data not found.")
				return `{"value":"Data-not-found", "ts":"` + utils.GetRfcTime() + `"}`
			}
		} else {
			return string(mcItem.Value)
		}
	case "none":
		return `{"value":"` + strconv.Itoa(dummyValue) + `", "ts":"` + utils.GetRfcTime() + `"}`
	}
	return ""
}

func setVehicleData(path string, value string) string {
	ts := utils.GetRfcTime()
	switch stateDbType {
	case "sqlite":
		stmt, err := dbHandle.Prepare("UPDATE VSS_MAP SET d_value=?, d_ts=? WHERE `path`=?")
		if err != nil {
			utils.Error.Printf("Could not prepare for statestorage updating, err = %s", err)
			return ""
		}
		defer stmt.Close()

		_, err = stmt.Exec(value, ts, path[1:len(path)-1]) // remove quotes surrounding path
		if err != nil {
			utils.Error.Printf("Could not update statestorage, err = %s", err)
			return ""
		}
		return ts
	case "memcache":
		fallthrough
	case "redis":
/*		udsConn := utils.GetUdsConn(path, "serverFeeder")
		if udsConn == nil {
			utils.Error.Printf("setVehicleData:Failed to UDS connect to feeder for path = %s", path)
			return ""
		}
		data := `{"path":"` + path + `", "dp":{"value":"` + value + `", "ts":"` + ts + `"}}`
		_, err := udsConn.Write([]byte(data))
		if err != nil {
			utils.Error.Printf("setVehicleData:Write failed, err = %s", err)
			return ""
		}*/
		if !feederConnected {
			return ""
		}
		message := `{"action": "set", "data": {"path":"` + path + `", "dp":{"value":"` + value + `", "ts":"` + ts + `"}}}`
		toFeeder <- message
		return ts
	case "apache-iotdb":
		vssKey := []string{"`" + path + "`"} // Back-quote the VSS node for the DB insert, e.g. `Vehicle.CurrentLocation.Longitude`
		vssValue := []string{value}
		IoTDBts := time.Now().UTC().UnixNano() / 1000000

		// IoTDB will automatically convert the value string to the native data type in the timeseries schema for basic types
		//		utils.Info.Printf("IoTDB: DB insert with prefixPath: %v vssKey: %v, vssValue: %v, ts: %v", IoTDBConfig.PrefixPath, vssKey, vssValue, IoTDBts)
		if status, err := IoTDBsession.InsertStringRecord(IoTDBConfig.PrefixPath, vssKey, vssValue, IoTDBts); err != nil {
			utils.Error.Printf("IoTDB: DB insert using InsertStringRecord failed with: %v", err)
			return ""
		} else {
			if status != nil {
				if err = client.VerifySuccess(status); err != nil {
					utils.Error.Printf("IoTDB: DB insert Verify failed with: %v", err)
					return ""
				}
			}
		}
		return ts
	}
	return ""
}

func unpackPaths(paths string) []string {
	var pathArray []string
	if strings.Contains(paths, "[") == true {
		err := json.Unmarshal([]byte(paths), &pathArray)
		if err != nil {
			return nil
		}
	} else {
		pathArray = make([]string, 1)
		pathArray[0] = paths[:]
	}
	return pathArray
}

func createHistoryList(vss_data []byte) bool {
	type PathList struct {
		LeafPaths []string
	}

	var pathList PathList
	err := json.Unmarshal(vss_data, &pathList)
	if err != nil {
		utils.Error.Printf("Error unmarshal json, err=%s\n", err)
		return false
	}

	utils.Info.Printf("createHistoryList: len(data.Vsspathlist)=%d, len(pathList.LeafPaths)=%d", len(vss_data), len(pathList.LeafPaths))

	historyList = make([]HistoryList, len(pathList.LeafPaths))
	for i := 0; i < len(pathList.LeafPaths); i++ {
		historyList[i].Path = pathList.LeafPaths[i]
		historyList[i].Frequency = 0
		historyList[i].BufSize = 0
		historyList[i].Status = 0
		historyList[i].BufIndex = 0
		historyList[i].Buffer = nil
	}
	return true
}

func historyServer(historyAccessChan chan string, vss_data []byte) {
	listExists := createHistoryList(vss_data) // file is created by core-server at startup
	histCtrlChannel := make(chan string)
	go initHistoryControlServer(histCtrlChannel)
	historyChannel := make(chan int)
	for {
		select {
		case signalId := <-historyChannel:
			captureHistoryValue(signalId)
		case histCtrlReq := <-histCtrlChannel: // history config request
			histCtrlChannel <- processHistoryCtrl(histCtrlReq, historyChannel, listExists)
		case getRequest := <-historyAccessChan: // history get request
			response := ""
			if listExists == true {
				response = processHistoryGet(getRequest)
			}
			historyAccessChan <- response
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}
}

func processHistoryCtrl(histCtrlReq string, historyChan chan int, listExists bool) string {
	if listExists == false {
		utils.Error.Printf("processHistoryCtrl:Path list not found")
		return "500 Internal Server Error"
	}
	var requestMap = make(map[string]interface{})
	utils.MapRequest(histCtrlReq, &requestMap)
	if requestMap["action"] == nil || requestMap["path"] == nil {
		utils.Error.Printf("processHistoryCtrl:Missing command param")
		return "400 Bad Request"
	}
	index := getHistoryListIndex(requestMap["path"].(string))
	switch requestMap["action"].(string) {
	case "create":
		if requestMap["buf-size"] == nil {
			utils.Error.Printf("processHistoryCtrl:Buffer size missing")
			return "400 Bad Request"
		}
		bufSize, err := strconv.Atoi(requestMap["buf-size"].(string))
		if err != nil {
			utils.Error.Printf("processHistoryCtrl:Buffer size malformed=%s", requestMap["buf-size"].(string))
			return "400 Bad Request"
		}
		historyList[index].BufSize = bufSize
		historyList[index].Buffer = make([]string, bufSize)
	case "start":
		if requestMap["frequency"] == nil {
			utils.Error.Printf("processHistoryCtrl:Frequency missing")
			return "400 Bad Request"
		}
		freq, err := strconv.Atoi(requestMap["frequency"].(string))
		if err != nil {
			utils.Error.Printf("processHistoryCtrl:Frequeny malformed=%s", requestMap["frequency"].(string))
			return "400 Bad Request"
		}
		historyList[index].Frequency = freq
		historyList[index].Status = 1
		activateHistory(historyChan, index, freq)
	case "stop":
		historyList[index].Status = 0
		deactivateHistory(index)
	case "delete":
		if historyList[index].Status != 0 {
			utils.Error.Printf("processHistoryCtrl:History recording must first be stopped")
			return "409 Conflict"
		}
		historyList[index].Frequency = 0
		historyList[index].BufSize = 0
		historyList[index].BufIndex = 0
		historyList[index].Buffer = nil
	default:
		utils.Error.Printf("processHistoryCtrl:Unknown command:action=%s", requestMap["action"].(string))
		return "400 Bad Request"
	}
	return "200 OK"
}

func getHistoryListIndex(path string) int {
	for i := 0; i < len(historyList); i++ {
		if historyList[i].Path == path {
			return i
		}
	}
	return -1
}

func getCurrentUtcTime() time.Time {
	return time.Now().UTC()
}

func convertFromIsoTime(isoTime string) (time.Time, error) {
	time, err := time.Parse(time.RFC3339, isoTime)
	return time, err
}

func processHistoryGet(request string) string { // {"path":"X", "period":"Y"}
	var requestMap = make(map[string]interface{})
	utils.MapRequest(request, &requestMap)
	index := getHistoryListIndex(requestMap["path"].(string))
	currentTs := getCurrentUtcTime()
	periodTime, _ := convertFromIsoTime(requestMap["period"].(string))
	oldTs := currentTs.Add(time.Hour*(time.Duration)((24*periodTime.Day()+periodTime.Hour())*(-1)) -
		time.Minute*(time.Duration)(periodTime.Minute()) - time.Second*(time.Duration)(periodTime.Second())).UTC()
	var matches int
	for matches = 0; matches < historyList[index].BufIndex; matches++ {
		storedTs, _ := convertFromIsoTime(getDPTs(historyList[index].Buffer[matches]))
		if storedTs.Before(oldTs) {
			break
		}
	}
	return historicDataPack(index, matches)
}

func historicDataPack(index int, matches int) string {
	dp := ""
	if matches > 1 {
		dp += "["
	}
	for i := 0; i < matches; i++ {
		dp += `{"value":"` + getDPValue(historyList[index].Buffer[i]) + `", "ts":"` + getDPTs(historyList[index].Buffer[i]) + `"}, `
	}
	if matches > 0 {
		dp = dp[:len(dp)-2]
	}
	if matches > 1 {
		dp += "]"
	}
	return dp
}

func captureHistoryValue(signalId int) {
	dp := getVehicleData(historyList[signalId].Path)
	utils.Info.Printf("captureHistoryValue:Captured historic dp = %s", dp)
	newTs := getDPTs(dp)
	latestTs := ""
	if historyList[signalId].BufIndex > 0 {
		latestTs = getDPTs(historyList[signalId].Buffer[historyList[signalId].BufIndex-1])
	}
	if newTs != latestTs && historyList[signalId].BufIndex < historyList[signalId].BufSize-1 {
		historyList[signalId].Buffer[historyList[signalId].BufIndex] = dp
		utils.Info.Printf("captureHistoryValue:Saved historic dp in buffer element=%d", historyList[signalId].BufIndex)
		historyList[signalId].BufIndex++
	}
}

func initHistoryControlServer(histCtrlChan chan string) {
	l, err := net.Listen("unix", utils.GetUdsPath("Vehicle", "history"))
	if err != nil {
		utils.Error.Printf("HistCtrlServer:Listen failed, er = %s.", err)
		return
	}

	for {
		conn, err := l.Accept()
		if err != nil {
			utils.Error.Printf("HistCtrlServer:Accept failed, err = %s", err)
			return
		}

		go historyControlServer(conn, histCtrlChan)
	}
}

func historyControlServer(conn net.Conn, histCtrlChan chan string) {
	buf := make([]byte, 512)
	for {
		nr, err := conn.Read(buf)
		if err != nil {
			utils.Error.Printf("HistCtrlServer:Read failed, err = %s", err)
			conn.Close() // assuming client hang up
			return
		}

		data := buf[:nr]
		utils.Info.Printf("HistCtrlServer:Read:data = %s", string(data))
		histCtrlChan <- string(data)
		resp := <-histCtrlChan
		_, err = conn.Write([]byte(resp))
		if err != nil {
			utils.Error.Printf("HistCtrlServer:Write failed, err = %s", err)
			return
		}
	}
}

func getDataPack(pathArray []string, filterList []utils.FilterObject) string {
	dataPack := ""
	if len(pathArray) > 1 {
		dataPack += "["
	}
	getHistory := false
	getDomain := false
	period := ""
	domain := ""
	if filterList != nil {
		for i := 0; i < len(filterList); i++ {
			if filterList[i].Type == "history" {
				if !historySupport {
					return ""
				}
				period = filterList[i].Parameter
				utils.Info.Printf("Historic data request, period=%s", period)
				getHistory = true
				break
			}
		}
	}
	var dataPoint string
	var request string
	for i := 0; i < len(pathArray); i++ {
		if getHistory == true {
			request = `{"path":"` + pathArray[i] + `", "period":"` + period + `"}`
			historyAccessChannel <- request
			dataPoint = <-historyAccessChannel
			if len(dataPoint) == 0 {
				return ""
			}
		} else if getDomain == true {
			dataPoint = getMetadataDomainDp(domain, pathArray[i])
		} else {
			dataPoint = getVehicleData(pathArray[i])
		}
		dataPack += `{"path":"` + pathArray[i] + `", "dp":` + dataPoint + "}, "
	}
	dataPack = dataPack[:len(dataPack)-2]
	if len(pathArray) > 1 {
		dataPack += "]"
	}
	return dataPack
}

func getDataPackMap(pathArray []string) map[string]interface{} {
	dataPack := make(map[string]interface{}, 0)
	if len(pathArray) > 1 {
		dataPackElement := make([]interface{}, len(pathArray))
		for i := 0; i < len(pathArray); i++ {
			dataPackElement[i] = map[string]interface{}{
				"path":  pathArray[i],
				"dp":  string2Map(getVehicleData(pathArray[i]))["s2m"],
			}
		}
		dataPack["dpack"] = dataPackElement
	} else {
		dataPack["dpack"] = map[string]interface{}{
				"path":  pathArray[0],
				"dp":  string2Map(getVehicleData(pathArray[0]))["s2m"],
		}
	}
	return dataPack
}

func getVssPathList(host string, port int, path string) []byte {
	url := "http://" + host + ":" + strconv.Itoa(port) + path
	utils.Info.Printf("url = %s", url)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		utils.Error.Fatal("getVssPathList: Error creating request:: ", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Host", host+":"+strconv.Itoa(port))

	// Set client timeout
	client := &http.Client{Timeout: time.Second * 10}

	// Send request
	resp, err := client.Do(req)
	if err != nil {
		utils.Error.Fatal("getVssPathList: Error reading response:: ", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		utils.Error.Fatal("getVssPathList::response Status: ", resp.Status)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		utils.Error.Fatal("getVssPathList::Error reading response. ", err)
	}

	utils.Info.Printf("getVssPathList fetched %d bytes", len(data))
	return data
}

func getMetadataDomainDp(domain string, path string) string {
	value := ""
	switch domain {
	case "samplerate":
		value = getSampleRate(path)
	case "availability":
		value = getAvailability(path)
	case "validate":
		value = getValidation(path)
	default:
		value = "Unknown domain"
	}
	return `{"value":"` + value + `","ts":"` + utils.GetRfcTime() + `"}`
}

func getSampleRate(path string) string {
	return "X Hz" //dummy return
}

func getAvailability(path string) string {
	return "available" //dummy return
}

func getValidation(path string) string {
	return "read-write" //dummy return
}

func configureDefault(udsConn net.Conn) {
	defaultFile := "defaultList1.json" //created at server startup if defaults found in any tree.
	for i := 2; utils.FileExists(defaultFile); i++ {
		data, err := os.ReadFile(defaultFile)
		if err != nil {
			utils.Error.Printf("configureDefault: Failed to read default configuration from %s with error = %s", defaultFile, err)
			return
		}
		defaultMessage := `{"action": "update", "defaultList": ` + string(data) + "}"
		_, err = udsConn.Write([]byte(defaultMessage))
		if err != nil {
			utils.Error.Printf("configureDefault:Feeder write failed, err = %s", err)
		}
		os.Remove(defaultFile)
		defaultFile = "defaultList" + strconv.Itoa(i) + ".json"
		time.Sleep(50 * time.Millisecond)  // give feeder some time to process the message sent
	}
}

func feederFrontend(toFeeder chan string, fromFeederRorC chan string, fromFeederCl chan string) {
	var udsConn net.Conn
	attempts := 0
	utils.Info.Printf("feederFrontend:Trying to connect to feeder...")
	for udsConn == nil && attempts < 10 {
		udsConn = utils.GetUdsConn("*", "serverFeeder")
		if udsConn == nil && attempts >= 10-1 {
			utils.Error.Printf("feederFrontend:Failed to UDS connect to feeder.")
			return
		}
		attempts++
		time.Sleep(3 * time.Second)
	}
	feederConnected = true
	utils.Info.Printf("feederFrontend:Connected to feeder.")
	configureDefault(udsConn)
	fromFeeder := make(chan string)
	go feederReader(udsConn, fromFeeder)
	feederNotification := "not-verified"  // possible alues ["not-verified", "not-supported", "supported"]
	subMessageCount := 0
	for {
		select {
			case message := <- toFeeder:
				var messageMap map[string]interface{}
				var feederUpdatePath string
				var unsubscribePath []string
				err := json.Unmarshal([]byte(message), &messageMap)
				if err != nil || messageMap["action"] == nil {
					utils.Error.Printf("feederFrontend:Feeder message corrupt, err = %s\nmessage=%s", err, message)
				} else {
					switch messageMap["action"].(string) {
						case "subscribe":
							if messageMap["subscriptionId"] != nil && messageMap["variant"] != nil && messageMap["path"] != nil {
								feederSubList = addOnFeederSubList(messageMap["subscriptionId"].(string), messageMap["variant"].(string), messageMap["path"].([]interface{}))
								feederPathList, feederUpdatePath = addOnFeederPathList(messageMap["path"].([]interface{}))
								if feederNotification != "not-supported" {
									if subMessageCount >= 5 {
										feederNotification = "not-supported"
										continue
									}
									message = `{"action": "subscribe", "path":` + feederUpdatePath + `}`
									subMessageCount++
								}
							} else {
								utils.Error.Printf("feederFrontend:Feeder message corrupt, message=%s", message)
								continue
							}
						case "unsubscribe":
							if messageMap["subscriptionId"] != nil {
								fromFeederCl <- message  // needed for list purging
								feederSubList, unsubscribePath = deleteOnFeederSubList(messageMap["subscriptionId"].(string))
								feederPathList, feederUpdatePath = deleteOnFeederPathList(unsubscribePath)
								if len(feederUpdatePath) > 4 {
									message = `{"action": "unsubscribe", "path":` + feederUpdatePath + `}`
								} else {
									continue
								}
							} else {
								utils.Error.Printf("feederFrontend:Feeder message corrupt, message=%s", message)
								continue
							}
						case "set": // send message as is to feeder
						default:
							utils.Error.Printf("feederFrontend:Feeder message action unknown, message=%s", message)
							continue
					}
					_, err := udsConn.Write([]byte(message))
					if err != nil {
						utils.Error.Printf("feederFrontend:Feeder write failed, err = %s", err)
					}
				}
			case message := <- fromFeeder:
				var messageMap map[string]interface{}
				err := json.Unmarshal([]byte(message), &messageMap)
				if err != nil || messageMap["action"] == nil {
					utils.Error.Printf("feederFrontend:Feeder message corrupt, err = %s\nmessage=%s", err, message)
				} else {
					switch messageMap["action"].(string) {
						case "subscribe": //response
							if messageMap["status"] != nil {
								if messageMap["status"].(string) == "ok" {
									fromFeederRorC <- message
									fromFeederCl <- message
									feederNotification = "supported"
								} else {
									feederNotification = "not-supported"
								}
							} else {
								utils.Error.Printf("feederFrontend:Feeder message corrupt, message=%s", message)
								continue
							}
						case "subscription": //notification
							if messageMap["path"] != nil {
								variant := getSubscribeVariant(messageMap["path"].(string))
								if strings.Contains(variant, "change") || strings.Contains(variant, "range") {
									fromFeederRorC <- message
								}
								if strings.Contains(variant, "curvelog") {
									fromFeederCl <- message
								}
							} else {
								utils.Error.Printf("feederFrontend:Feeder message corrupt, message=%s", message)
							}
						default:
							utils.Error.Printf("feederFrontend:Feeder message action unknown, message=%s", message)
					}
				}
		}
	}
}

func getSubscribeVariant(path string) string {
	variants := ""
	for i := 0; i < len(feederSubList); i++ {
		for j := 0; j < len(feederSubList[i].Path); j++ {
			if feederSubList[i].Path[j] == path {
				if !strings.Contains(variants, feederSubList[i].Variant) {
					variants += feederSubList[i].Variant + "+"
				}
			}
		}
	}
	if len(variants) > 0 {
		variants = variants[:len(variants)-1]
	}
	return variants
}

func addOnFeederSubList(subscriptionId string, variant string, path []interface{}) []FeederSubElem {
        var feederSubElem FeederSubElem
        feederSubElem.SubscriptionId = subscriptionId
        feederSubElem.Variant = variant
        feederSubElem.Path = make([]string, len(path))
        for i := 0; i < len(path); i++ {
        	feederSubElem.Path[i] = path[i].(string)
        }
	feederSubList = append(feederSubList, feederSubElem)
	return feederSubList
}

func deleteOnFeederSubList(subscriptionId string) ([]FeederSubElem, []string) {
	for i := 0; i < len(feederSubList); i++ {
		if feederSubList[i].SubscriptionId == subscriptionId {
			unsubscribePath := make([]string, len(feederSubList[i].Path))
			for j := 0; j < len(feederSubList[i].Path); j++ {
				unsubscribePath[j] = feederSubList[i].Path[j]
			}
			return slices.Delete(feederSubList, i, i+1), unsubscribePath
		}
	}
	return nil, nil
}

func addOnFeederPathList(path []interface{}) ([]FeederPathElem, string) {
        var feederPathElem FeederPathElem
	var pathFound bool
	feederUpdatePath := `["`
	for i := 0; i < len(path); i++ {
		pathFound = false
		for j := 0; j < len(feederPathList); j++ {
			if path [i] == feederPathList[j].Path {
				feederPathList[j].Reference++
				pathFound = true
			}
		}
		if !pathFound {
			feederPathElem.Path = path[i].(string)
			feederPathElem.Reference = 1
			feederPathList = append(feederPathList, feederPathElem)
			feederUpdatePath += path[i].(string) + `", "`
		}
	}
	if len(feederUpdatePath) > 2 {
		feederUpdatePath = feederUpdatePath[:len(feederUpdatePath)-4]
	}
	return feederPathList, feederUpdatePath + `"]`
}

func deleteOnFeederPathList(path []string) ([]FeederPathElem, string) {
	feederUpdatePath := `["`
	removeIndex := make([]int, len(path))
	k := 0
	for i := 0; i < len(path); i++ {
		removeIndex[k] = -1
		for j := 0; j < len(feederPathList); j++ {
			if path [i] == feederPathList[j].Path {
				if feederPathList[j].Reference > 1 {
					feederPathList[j].Reference--
				} else {
					removeIndex[k] = j
					k++
				}
			}
		}
		k = 0
		for i := 0; i < len(path); i++ {
			if removeIndex[k] == i {
				feederUpdatePath += path[i] + `", "`
				k++
				feederPathList = slices.Delete(feederPathList, i, i+1)
			}
		}
	}
	if len(feederUpdatePath) > 2 {
		feederUpdatePath = feederUpdatePath[:len(feederUpdatePath)-4]
	}
	return feederPathList, feederUpdatePath + `"]`
}

func feederReader(udsConn net.Conn, fromFeeder chan string) {
	buf := make([]byte, 512)
	for {
		nr, err := udsConn.Read(buf)
		if err != nil {
			utils.Error.Printf("feederReader:Read failed, err = %s", err)
			break
		} else {
			fromFeeder <- string(buf[:nr])
		}
	}
}

//func ServiceMgrInit(mgrId int, serviceMgrChan chan string, stateStorageType string, histSupport bool, dbFile string) {
func ServiceMgrInit(mgrId int, serviceMgrChan chan map[string]interface{}, stateStorageType string, histSupport bool, dbFile string) {
	stateDbType = stateStorageType
	historySupport = histSupport
	feederConnected = false

	utils.ReadUdsRegistrations("uds-registration.json")

	switch stateDbType {
	case "sqlite":
		if utils.FileExists(dbFile) {
			dbHandle, dbErr = sql.Open("sqlite3", dbFile)
			if dbErr != nil {
				utils.Error.Printf("Could not open state storage file = %s, err = %s", dbFile, dbErr)
				os.Exit(1)
			} else {
				utils.Info.Printf("SQLite state storage initialised.")
			}
		} else {
			utils.Error.Printf("Could not find state storage file = %s", dbFile)
		}
	case "redis":
		addr := utils.GetUdsPath("*", "redis")
		if len(addr) == 0 {
			utils.Error.Printf("redis-server socket address not found.")
			// os.Exit(1) should terminate the process
			return
		}
		utils.Info.Printf(addr)
		redisClient = redis.NewClient(&redis.Options{
			Network:  "unix",
			Addr:     addr,
			Password: "",
			DB:       1,
		})
		err := redisClient.Ping().Err()
		if err != nil {
			utils.Info.Printf("Redis-server not started. Trying to start it.")
			if utils.FileExists("redis.log") {
				os.Remove("redis.log")
			}
			cmd := exec.Command("/usr/bin/bash", "redisNativeInit.sh")
			err := cmd.Run()
			if err != nil {
				utils.Error.Printf("redis-server startup failed, err=%s", err)
				// os.Exit(1) should terminate the process
				return
			}
		} else {
			utils.Info.Printf("Redis state ping is ok")
		}
		utils.Info.Printf("Redis state storage initialised.")
	case "apache-iotdb":
		// Read configuration from file if present else use defaults
		IoTDBConfigFilename := "iotdb-config.json"
		utils.Info.Printf("IoTDB: Default configuration before config file read = %+v", IoTDBConfig)
		data, err := os.ReadFile(IoTDBConfigFilename)
		if err != nil {
			utils.Error.Printf("IoTDB: Failed to read configuration from %v with error = %+v", IoTDBConfigFilename, err)
		} else {
			var IoTDBJSONConfig IoTDBConfiguration
			err = json.Unmarshal(data, &IoTDBJSONConfig)
			if err != nil {
				utils.Error.Printf("IoTDB: Failed to unmarshal the JSON config data from %v with error = %+v", IoTDBConfigFilename, err)
			} else {
				utils.Info.Printf("IoTDB: Configuration read from config file %v = %+v", IoTDBConfigFilename, IoTDBJSONConfig)
				// Success. Copy config read from the file.
				IoTDBConfig = IoTDBJSONConfig
			}
		}

		IoTDBClientConfig.Host = IoTDBConfig.Host
		IoTDBClientConfig.Port = IoTDBConfig.Port
		IoTDBClientConfig.UserName = IoTDBConfig.UserName
		IoTDBClientConfig.Password = IoTDBConfig.Password

		// Create new client session with IoTDB server
		utils.Info.Printf("IoTDB: Creating new session with client config = %+v", *IoTDBClientConfig)
		IoTDBsession = client.NewSession(IoTDBClientConfig)
		if err := IoTDBsession.Open(false, 0); err != nil {
			utils.Error.Printf("IoTDB: Failed to open server session with error=%s", err)
			os.Exit(1)
		}
		defer IoTDBsession.Close()
	case "memcache":
		addr := utils.GetUdsPath("Vehicle", "memcache")
		if len(addr) == 0 {
			utils.Error.Printf("memcache socket address not found.")
			// os.Exit(1) should terminate the process
			return
		}
		memcacheClient = memcache.New(addr)
		err := memcacheClient.Ping()
		if err != nil {
			utils.Info.Printf("Memcache daemon not alive. Trying to start it")
			cmd := exec.Command("/usr/bin/bash", "memcacheNativeInit.sh")
			err := cmd.Run()
			if err != nil {
				utils.Error.Printf("Memcache daemon startup failed, err=%s", err)
				// os.Exit(1) should terminate the process
				return
			}
		}
		utils.Info.Printf("Memcache daemon alive.")
	default:
		utils.Error.Printf("Unknown state storage type = %s", stateDbType)
	}

	dataChan := make(chan map[string]interface{})
	backendChan := make(chan map[string]interface{})
	subscriptionChan := make(chan int)
	historyAccessChannel = make(chan string)
	initClResources()
	subscriptionList := []SubscriptionState{}
	subscriptionId = 1 // do not start with zero!

/*	var serverCoreIP string = utils.GetModelIP(2)

	vss_data := getVssPathList(serverCoreIP, 8081, "/vsspathlist")
	if historySupport {
		go historyServer(historyAccessChannel, vss_data)
	}*/
	go initDataServer(serviceMgrChan, dataChan, backendChan)
	toFeeder = make(chan string)
	fromFeederRorC = make(chan string)
	fromFeederCl = make(chan string)
	go feederFrontend(toFeeder, fromFeederRorC, fromFeederCl)
	go curveLogServer()
	var dummyTicker *time.Ticker
	if stateDbType != "none" {
		dummyTicker = time.NewTicker(47 * time.Millisecond)
	}
	subscriptTicker := time.NewTicker(23 * time.Millisecond) //range/change subscription ticker when no feeder notifications
	feederNotification := false
	triggeredPath := ""

	for {
		select {
		case requestMap := <-dataChan: // request from server core
//			utils.Info.Printf("Service manager: Request from Server core:%s\n", request)
			// TODO: interact with underlying subsystem to get the value
			var responseMap = make(map[string]interface{})
			responseMap["RouterId"] = requestMap["RouterId"]
			responseMap["action"] = requestMap["action"]
			responseMap["requestId"] = requestMap["requestId"]
			responseMap["ts"] = utils.GetRfcTime()
			if requestMap["handle"] != nil {
				responseMap["authorization"] = requestMap["handle"]
			}
			switch requestMap["action"] {
			case "set":
				if strings.Contains(requestMap["path"].(string), "[") == true {
					utils.SetErrorResponse(requestMap, errorResponseMap, 1, "") //invalid_data
					dataChan <- errorResponseMap
					break
				}
				ts := setVehicleData(requestMap["path"].(string), requestMap["value"].(string))
				if len(ts) == 0 {
					utils.SetErrorResponse(requestMap, errorResponseMap, 7, "") //service_unavailable
					dataChan <- errorResponseMap
					break
				}
				responseMap["ts"] = ts
				dataChan <- responseMap
			case "get":
				pathArray := unpackPaths(requestMap["path"].(string))
				if pathArray == nil {
					utils.Error.Printf("Unmarshal of path array failed.")
					utils.SetErrorResponse(requestMap, errorResponseMap, 1, "") //invalid_data
					dataChan <- errorResponseMap
					break
				}
				var filterList []utils.FilterObject
				if requestMap["filter"] != nil && requestMap["filter"] != "" {
					utils.UnpackFilter(requestMap["filter"], &filterList)
					if len(filterList) == 0 {
						utils.Error.Printf("Request filter malformed.")
						utils.SetErrorResponse(requestMap, errorResponseMap, 0, "") //bad_request
						dataChan <- errorResponseMap
						break
					}
				}
				dataPack := getDataPackMap(pathArray)
/*				if len(dataPack) == 0 {
					utils.Info.Printf("No historic data available")
					utils.SetErrorResponse(requestMap, errorResponseMap, 6, "") //unavailable_data
					dataChan <- errorResponseMap
					break
				}*/
				responseMap["data"] = dataPack["dpack"]
				dataChan <- responseMap
			case "subscribe":
				var subscriptionState SubscriptionState
				subscriptionState.SubscriptionId = subscriptionId
				subscriptionState.RouterId = requestMap["RouterId"].(string)
				subscriptionState.Path = unpackPaths(requestMap["path"].(string))
				if requestMap["filter"] == nil || requestMap["filter"] == "" {
					utils.SetErrorResponse(requestMap, errorResponseMap, 0, "") //bad_request
					dataChan <- errorResponseMap
					break
				}
				utils.UnpackFilter(requestMap["filter"], &(subscriptionState.FilterList))
				if len(subscriptionState.FilterList) == 0 {
					utils.SetErrorResponse(requestMap, errorResponseMap, 1, "") //invalid_data
					dataChan <- errorResponseMap
				}
				if requestMap["gatingId"] != nil {
					subscriptionState.GatingId = requestMap["gatingId"].(string)
				}
				subscriptionState.LatestDataPoint = getVehicleData(subscriptionState.Path[0])
				subscriptionList = append(subscriptionList, subscriptionState)
				responseMap["subscriptionId"] = strconv.Itoa(subscriptionId)
				subscriptionList = activateIfIntervalOrCL(subscriptionState.FilterList, subscriptionChan, CLChannel, subscriptionId, subscriptionState.Path, subscriptionList)
				variant := getFeederNotifyType(subscriptionState.FilterList)
				if variant == "curvelog" || variant == "range" || variant == "change" {
					toFeeder <- createFeederNotifyMessage(variant, subscriptionState.Path, subscriptionId)
				}
				subscriptionId++ // not to be incremented elsewhere
				dataChan <- responseMap
			case "unsubscribe":
				if requestMap["subscriptionId"] != nil {
					status := -1
					subscriptId, ok := requestMap["subscriptionId"].(string)
					if ok == true {
						status, subscriptionList = deactivateSubscription(subscriptionList, subscriptId)
						if status != -1 {
							responseMap["subscriptionId"] = subscriptId
							dataChan <- responseMap
							toFeeder <- utils.FinalizeMessage(requestMap)
							break
						}
						requestMap["subscriptionId"] = subscriptId
					}
				}
				utils.SetErrorResponse(requestMap, errorResponseMap, 1, "") //invalid_data
				dataChan <- errorResponseMap
			case "internal-killsubscriptions":
				isRemoved := true
				for isRemoved == true {
					isRemoved, subscriptionList = scanAndRemoveListItem(subscriptionList, requestMap["RouterId"].(string))
					utils.Info.Printf("internal-killsubscriptions: RouterId = %s", requestMap["RouterId"].(string))
				}
			case "internal-cancelsubscription":
				routerId, subscriptionId := getSubscriptionData(subscriptionList, requestMap["gatingId"].(string))
				if routerId != "" {
					requestMap["RouterId"] = routerId
					requestMap["action"] = "subscription"
					requestMap["requestId"] = nil
					requestMap["subscriptionId"] = subscriptionId
					utils.SetErrorResponse(requestMap, errorResponseMap, 2, "Token expired or consent cancelled.")
					dataChan <- errorResponseMap
					_, subscriptionList = scanAndRemoveListItem(subscriptionList, routerId)
				}
			default:
				utils.SetErrorResponse(requestMap, errorResponseMap, 1, "Unknown action") //invalid_data
					dataChan <- errorResponseMap
			} // switch
		case <-dummyTicker.C:
			dummyValue++
			if dummyValue > 999 {
				dummyValue = 0
			}
		case subscriptionId := <-subscriptionChan: // interval notification triggered
			subscriptionState := subscriptionList[getSubcriptionStateIndex(subscriptionId, subscriptionList)]
			var subscriptionMap = make(map[string]interface{})
			subscriptionMap["action"] = "subscription"
			subscriptionMap["ts"] = utils.GetRfcTime()
			subscriptionMap["subscriptionId"] = strconv.Itoa(subscriptionState.SubscriptionId)
			subscriptionMap["RouterId"] = subscriptionState.RouterId
			subscriptionMap["data"] = getDataPackMap(subscriptionState.Path)["dpack"]
			backendChan <- subscriptionMap
		case clPack := <-CLChannel: // curve logging notification
			index := getSubcriptionStateIndex(clPack.SubscriptionId, subscriptionList)
			if index == -1 {
				closeClSubId = -1
				continue
			}
			//subscriptionState := subscriptionList[index]
			subscriptionList[index].SubscriptionThreads--
			if clPack.SubscriptionId == closeClSubId && subscriptionList[index].SubscriptionThreads == 0 {
				subscriptionList = removeFromsubscriptionList(subscriptionList, index)
				closeClSubId = -1
			}
			var subscriptionMap = make(map[string]interface{})
			subscriptionMap["action"] = "subscription"
			subscriptionMap["ts"] = utils.GetRfcTime()
			subscriptionMap["subscriptionId"] = strconv.Itoa(subscriptionList[index].SubscriptionId)
			subscriptionMap["RouterId"] = subscriptionList[index].RouterId
			subscriptionMap["data"] = string2Map(clPack.DataPack)["s2m"]
			backendChan <- subscriptionMap
		case <-subscriptTicker.C:
			if feederNotification == false { // feeder does not issue notifications
				subscriptionList = checkRCFilterAndIssueMessages("", subscriptionList, backendChan)
			} else {
				subscriptTicker.Stop()
			}
		case feederMessage := <-fromFeederRorC:
//utils.Info.Printf("Feeder message=%s", feederMessage)
			triggeredPath, feederNotification = decodeFeederMessage(feederMessage, feederNotification)
			subscriptionList = checkRCFilterAndIssueMessages(triggeredPath, subscriptionList, backendChan)
		} // select
	} // for
utils.Info.Printf("Service manager exit")
}

func checkRCFilterAndIssueMessages(triggeredPath string, subscriptionList []SubscriptionState, backendChan chan map[string]interface{}) []SubscriptionState {
	// check if range or change notification triggered
	for i := range subscriptionList {
		if len(triggeredPath) == 0 || triggeredPath == subscriptionList[i].Path[0] {
			doTrigger, triggerDataPoint := checkRangeChangeFilter(subscriptionList[i].FilterList, subscriptionList[i].LatestDataPoint, subscriptionList[i].Path[0])
			subscriptionList[i].LatestDataPoint = triggerDataPoint
			if doTrigger == true {
				subscriptionState := subscriptionList[i]
				var subscriptionMap = make(map[string]interface{})
				subscriptionMap["action"] = "subscription"
				subscriptionMap["ts"] = utils.GetRfcTime()
				subscriptionMap["subscriptionId"] = strconv.Itoa(subscriptionState.SubscriptionId)
				subscriptionMap["RouterId"] = subscriptionState.RouterId
				subscriptionMap["data"] = getDataPackMap(subscriptionList[i].Path)["dpack"]
				backendChan <- subscriptionMap
			}
		}
	}
	return subscriptionList
}

func string2Map(msg string) map[string]interface{} {
	var msgMap map[string]interface{}
	utils.MapRequest(`{"s2m":`+msg+"}", &msgMap)
	return msgMap
}

func decodeFeederMessage(feederMessage string, feederNotification bool) (string, bool) {
	if len(feederMessage) == 0 {
		return "", feederNotification
	}
	var messageMap map[string]interface{}
	err := json.Unmarshal([]byte(feederMessage), &messageMap)
	if err != nil || messageMap["action"] == nil {
		utils.Error.Printf("Error in feeder message=%s", feederMessage)
		return "", feederNotification
	}
	var triggeredPath string
	switch messageMap["action"].(string) {
		case "subscribe":
			if messageMap["status"] != nil && messageMap["status"].(string) == "ok" {
				feederNotification = true
			}
		case "subscription":
			if messageMap["path"] != nil {
				triggeredPath  = messageMap["path"].(string)
			}
		default:
			utils.Error.Printf("Unknown action=%s", messageMap["action"].(string))
			return "", feederNotification
	}
	return triggeredPath, feederNotification
}

func getSubscriptionData(subscriptionList []SubscriptionState, gatingId string) (string, string) {
	for i := 0; i < len(subscriptionList); i++ {
		if subscriptionList[i].GatingId == gatingId {
			return subscriptionList[i].RouterId, strconv.Itoa(subscriptionList[i].SubscriptionId)
		}
	}
	utils.Error.Printf("getSubscriptionData: gatingId = %s not on subscription list", gatingId)
	return "", ""
}

func scanAndRemoveListItem(subscriptionList []SubscriptionState, routerId string) (bool, []SubscriptionState) {
	removed := false
	doRemove := false
	for i := 0; i < len(subscriptionList); i++ {
		if subscriptionList[i].RouterId == routerId {
			doRemove = true
		}
		if doRemove {
			_, subscriptionList = deactivateSubscription(subscriptionList, strconv.Itoa(subscriptionList[i].SubscriptionId))
			removed = true
			break
		}
		doRemove = false
	}
	return removed, subscriptionList
}
