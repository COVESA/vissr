/**
* (C) 2023 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE file in this repository.
*
**/

package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/akamensky/argparse"
	"github.com/covesa/vissr/utils"
	"github.com/go-redis/redis"
	_ "github.com/mattn/go-sqlite3"
	"io"
	"net"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/gorilla/websocket"
	"net/http"
	"net/url"
)

type DomainData struct {
	Name  string
	Value string
}

type FeederMap struct {
	MapIndex     uint16
	Name         string
	Type         int8
	Datatype     int8
	ConvertIndex uint16
}

var Upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

var MuxServer = []*http.ServeMux{
	http.NewServeMux(),
}

var redisClient *redis.Client
var dbHandle *sql.DB
var stateDbType string

var scalingDataList []string
var canDriverUrl string

func initVSSInterfaceMgr(inputChan chan DomainData, outputChan chan DomainData) {
	udsChan := make(chan DomainData, 32)
	go initUdsEndpoint(udsChan)
	for {
		select {
		case outData := <-outputChan:
			utils.Info.Printf("Data written to statestorage: Name=%s, Value=%s", outData.Name, outData.Value)
			status := statestorageSet(outData.Name, outData.Value, utils.GetRfcTime())
			if status != 0 {
				utils.Error.Printf("initVSSInterfaceMgr():Redis write failed")
			}
		case actuatorData := <-udsChan:
			pipelineSend(inputChan, actuatorData, "vssInputChan")
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
		dpBytes, err := json.Marshal(map[string]string{"val": val, "ts": ts})
		if err != nil {
			utils.Error.Printf("statestorageSet:Marshal error=%s", err)
			return -1
		}
		err = redisClient.Set(path, string(dpBytes), time.Duration(0)).Err()
		if err != nil {
			utils.Error.Printf("Job failed. Err=%s", err)
			return -1
		}
		return 0
	}
	return -1
}

// UdsBufSize is the per-read buffer for the server→feeder UDS channel.
// Sized for a typical JSON datapoint with margin; messages larger than this
// are dropped with a clear error rather than truncated silently.
const UdsBufSize = 64 * 1024

func initUdsEndpoint(udsChan chan DomainData) {
	os.Remove("/var/tmp/vissv2/server-feeder-channel.sock")
	listener, err := net.Listen("unix", "/var/tmp/vissv2/server-feeder-channel.sock") //the file must be the same as declared in the feeder-registration.json that the service mgr reads
	if err != nil {
		utils.Error.Printf("initUdsEndpoint:UDS listen failed, err = %s", err)
		os.Exit(-1)
	}
	for {
		conn, err := listener.Accept()
		if err != nil {
			utils.Error.Printf("initUdsEndpoint:UDS accept failed, err = %s", err)
			// transient accept errors should not crash the feeder
			time.Sleep(100 * time.Millisecond)
			continue
		}
		handleUdsConn(conn, udsChan)
	}
}

// handleUdsConn reads framed datapoints from a single accepted UDS connection
// and forwards each parsed DomainData to udsChan. Returns when the connection
// closes; the caller should accept again.
func handleUdsConn(conn net.Conn, udsChan chan DomainData) {
	defer conn.Close()
	buf := make([]byte, UdsBufSize)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			if err != io.EOF {
				utils.Error.Printf("initUdsEndpoint:Read failed, err = %s", err)
			}
			return
		}
		if n == len(buf) {
			utils.Error.Printf("initUdsEndpoint: message at UDS buffer size (%d bytes); may be truncated, dropping", n)
			continue
		}
		utils.Info.Printf("Feeder:Server message: %s", string(buf[:n]))
		domainData, _, ok := splitToDomainDataAndTs(string(buf[:n]))
		if !ok {
			continue
		}
		// non-blocking send: a wedged downstream must not back-pressure the reader
		select {
		case udsChan <- domainData:
		default:
			utils.Error.Printf("initUdsEndpoint: udsChan full, dropping datapoint for %q", domainData.Name)
		}
	}
}

// splitToDomainDataAndTs parses a server datapoint of the shape
//   {"dp": {"ts": "Z","value": "Y"},"path": "X"}
// and returns the extracted DomainData, the timestamp string, and ok=true.
// On any parse / shape error, returns zero values and ok=false (without
// panicking) so callers can keep processing further datapoints.
func splitToDomainDataAndTs(serverMessage string) (DomainData, string, bool) {
	var domainData DomainData
	var serverMessageMap map[string]interface{}
	err := json.Unmarshal([]byte(serverMessage), &serverMessageMap)
	if err != nil {
		utils.Error.Printf("splitToDomainDataAndTs:Unmarshal error=%s", err)
		return domainData, "", false
	}
	name, ok := mapString(serverMessageMap, "path")
	if !ok {
		utils.Error.Printf("splitToDomainDataAndTs: missing/invalid 'path' in %q", serverMessage)
		return domainData, "", false
	}
	dpRaw, present := serverMessageMap["dp"]
	if !present || dpRaw == nil {
		utils.Error.Printf("splitToDomainDataAndTs: missing 'dp' in %q", serverMessage)
		return domainData, "", false
	}
	dpMap, ok := dpRaw.(map[string]interface{})
	if !ok {
		utils.Error.Printf("splitToDomainDataAndTs: 'dp' not an object in %q", serverMessage)
		return domainData, "", false
	}
	value, ok := mapString(dpMap, "value")
	if !ok {
		utils.Error.Printf("splitToDomainDataAndTs: missing/invalid 'value' in %q", serverMessage)
		return domainData, "", false
	}
	ts, ok := mapString(dpMap, "ts")
	if !ok {
		utils.Error.Printf("splitToDomainDataAndTs: missing/invalid 'ts' in %q", serverMessage)
		return domainData, "", false
	}
	domainData.Name = name
	domainData.Value = value
	return domainData, ts, true
}

// mapString fetches m[key] as a string. Returns "", false if absent, nil,
// or the wrong type.
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

func initVehicleInterfaceMgr(fMap []FeederMap, inputChan chan DomainData, outputChan chan DomainData) {
	CanDriverOutputChan := make(chan DomainData, 32)
	CanDriverInputChan := make(chan DomainData, 32)
	go initCanDriverOutput(CanDriverOutputChan)
	go initCanDriverInput(CanDriverInputChan)

	for {
		select {
		case outData := <-outputChan:
			utils.Info.Printf("Data to the vehicle interface: Name=%s, Value=%s", outData.Name, outData.Value)
			pipelineSend(CanDriverOutputChan, outData, "CanDriverOutputChan")
		case inData := <-CanDriverInputChan:
			utils.Info.Printf("Data from the vehicle interface: Name=%s, Value=%s", inData.Name, inData.Value)
			pipelineSend(inputChan, inData, "vehicleInputChan")
		}
	}
}

func initCanDriverOutput(outputChan chan DomainData) { // WS client
	scheme := "ws"
	portNum := "8002"
	addr := canDriverUrl + ":" + portNum
	dataSessionUrl := url.URL{Scheme: scheme, Host: addr, Path: ""}
	dialer := websocket.Dialer{
		HandshakeTimeout: time.Second,
		ReadBufferSize:   1024,
		WriteBufferSize:  1024,
	}
	go canDriverClient(dialer, dataSessionUrl, outputChan)
}

// canDriverClient owns the lifetime of a single CAN-driver WebSocket
// connection: it dials with retry, writes each DomainData from clientChan,
// and on any write error it closes the conn and re-dials. This avoids the
// previous bug where the goroutine exited on first error and clientChan
// then filled and back-pressured the whole pipeline.
func canDriverClient(dialer websocket.Dialer, sessionUrl url.URL, clientChan chan DomainData) {
	for {
		conn := reDialer(dialer, sessionUrl)
		if conn == nil {
			utils.Error.Printf("canDriverClient: failed to dial %s after retries; drop-and-retry mode", sessionUrl.String())
			// drain a few messages so back-pressure doesn't wedge the pipeline
			drainStart := time.Now()
			for time.Since(drainStart) < 5*time.Second {
				select {
				case dropped := <-clientChan:
					utils.Error.Printf("canDriverClient: dropping %q while disconnected", dropped.Name)
				case <-time.After(time.Second):
				}
			}
			continue
		}
		writeLoopErr := writeCanDriverLoop(conn, clientChan)
		conn.Close()
		if writeLoopErr == errClientChanClosed {
			return
		}
	}
}

var errClientChanClosed = errors.New("clientChan closed")

func writeCanDriverLoop(conn *websocket.Conn, clientChan chan DomainData) error {
	for {
		domainData, ok := <-clientChan
		if !ok {
			return errClientChanClosed
		}
		if domainData.Name == "" || domainData.Value == "" {
			utils.Error.Printf("canDriverClient:Invalid domain data - Name=%s, Value=%s", domainData.Name, domainData.Value)
			continue
		}
		reqBytes, err := json.Marshal(map[string]string{
			"path":  domainData.Name,
			"value": domainData.Value,
		})
		if err != nil {
			utils.Error.Printf("canDriverClient:Marshal error: %s", err)
			continue
		}
		if err := conn.WriteMessage(websocket.TextMessage, reqBytes); err != nil {
			utils.Error.Printf("canDriverClient:Request write error:%s; reconnecting", err)
			return err
		}
	}
}

func reDialer(dialer websocket.Dialer, sessionUrl url.URL) *websocket.Conn {
	for i := 0; i < 15; i++ {
		conn, _, err := dialer.Dial(sessionUrl.String(), nil)
		if err != nil {
			utils.Error.Printf("Data session dial error:%s\n", err)
			time.Sleep(2 * time.Second)
		} else {
			return conn
		}
	}
	return nil
}

func initCanDriverInput(inputChan chan DomainData) { // WS server
	serverHandler := makeServerHandler(inputChan)
	MuxServer[0].HandleFunc("/", serverHandler)
	utils.Error.Fatal(http.ListenAndServe(":8001", MuxServer[0]))
}

func makeServerHandler(serverChannel chan DomainData) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("Upgrade") == "websocket" {
			utils.Info.Printf("Received websocket request: we are upgrading to a websocket connection.")
			Upgrader.CheckOrigin = func(r *http.Request) bool { return true }
			h := http.Header{}
			conn, err := Upgrader.Upgrade(w, req, h)
			if err != nil {
				utils.Error.Print("upgrade error:", err)
				return
			}
			go serverSession(conn, serverChannel)
		} else {
			utils.Error.Printf("Client must set up a Websocket session.")
		}
	}
}

func serverSession(conn *websocket.Conn, serverChannel chan DomainData) {
	defer conn.Close()
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			utils.Error.Printf("App client read error: %s", err)
			break
		}
		payload := string(msg)
		utils.Info.Printf("%s request: %s, len=%d", conn.RemoteAddr(), payload, len(payload))
		domainData := convertToDomainData(payload)
		if domainData.Name == "" {
			continue // parse failed; already logged
		}
		// non-blocking: a wedged downstream cannot back-pressure the WS reader
		select {
		case serverChannel <- domainData:
		default:
			utils.Error.Printf("serverSession: serverChannel full, dropping datapoint for %q", domainData.Name)
		}
	}
}

func convertToDomainData(message string) DomainData { // {"path":"x.y.z", "value":"123"}
	var domainData DomainData
	var messageMap map[string]interface{}
	err := json.Unmarshal([]byte(message), &messageMap)
	if err != nil {
		utils.Error.Printf("convertToDomainData:Unmarshal error=%s", err)
		return domainData
	}
	name, ok := mapString(messageMap, "path")
	if !ok {
		utils.Error.Printf("convertToDomainData: missing/invalid 'path' in %q", message)
		return domainData
	}
	value, ok := mapString(messageMap, "value")
	if !ok {
		utils.Error.Printf("convertToDomainData: missing/invalid 'value' in %q", message)
		return domainData
	}
	domainData.Name = name
	domainData.Value = value
	return domainData
}

func convertDomainData(north2SouthConv bool, inData DomainData, feederMap []FeederMap) DomainData {
	var outData DomainData
	matchIndex, ok := lookupFeederMap(inData.Name, feederMap)
	if !ok {
		utils.Error.Printf("convertDomainData: name %q not in feeder map", inData.Name)
		return outData
	}
	mapIdx := int(feederMap[matchIndex].MapIndex)
	if mapIdx < 0 || mapIdx >= len(feederMap) {
		utils.Error.Printf("convertDomainData: MapIndex %d for %q out of range [0,%d)", mapIdx, inData.Name, len(feederMap))
		return outData
	}
	outData.Name = feederMap[mapIdx].Name
	outData.Value = convertValue(inData.Value, feederMap[matchIndex].ConvertIndex,
		feederMap[matchIndex].Datatype, feederMap[mapIdx].Datatype, north2SouthConv)
	return outData
}

// lookupFeederMap returns the index of the entry whose Name matches `name`.
// The feeder map is required to be sorted by Name (see readFeederMap).
// Returns (-1, false) when no entry matches.
func lookupFeederMap(name string, feederMap []FeederMap) (int, bool) {
	if len(feederMap) == 0 {
		return -1, false
	}
	matchIndex := sort.Search(len(feederMap), func(i int) bool { return feederMap[i].Name >= name })
	if matchIndex == len(feederMap) || feederMap[matchIndex].Name != name {
		return -1, false
	}
	return matchIndex, true
}

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
	err := json.Unmarshal([]byte(scalingDataList[idx]), &convertDataMap)
	if err != nil {
		utils.Error.Printf("convertValue:Error unmarshal scalingDataList item=%s", scalingDataList[idx])
		return ""
	}
	switch vv := convertDataMap.(type) {
	case map[string]interface{}:
		return enumConversion(vv, north2SouthConv, value)
	case []interface{}:
		return linearConversion(vv, north2SouthConv, value)
	default:
		utils.Error.Printf("convertValue: convert data=%s has unknown format (got %T).", scalingDataList[idx], convertDataMap)
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
	return strconv.FormatFloat(y, 'f', -1, 32)
}

func fileExists(filename string) bool {
	info, err := os.Stat(filename)
	if err != nil {
		// covers IsNotExist plus permission errors etc.; in either case
		// callers should treat "missing" and behave accordingly.
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
		utils.Error.Printf("Could not open %s for reading map data: %v", mapFilename, err)
		return nil
	}
	defer treeFp.Close()
	for {
		mapElement, ok := readElement(treeFp)
		if !ok || mapElement.Name == "" {
			break
		}
		feederMap = append(feederMap, mapElement)
	}
	// convertDomainData relies on sort.Search; ensure the slice is sorted by Name.
	sort.Slice(feederMap, func(i, j int) bool { return feederMap[i].Name < feederMap[j].Name })
	return feederMap
}

// readElement reads one FeederMap entry from the binary domain-conversion file.
// Returns (zero, false) on EOF or short/corrupt read so the caller can stop
// the loop cleanly instead of inheriting a half-populated element.
// The reading order must be aligned with the writing order by the Domain Conversion Tool.
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

// MaxReadBytes caps the per-call size of readBytes so a corrupt file with a
// large length prefix cannot trigger a multi-GiB allocation.
const MaxReadBytes = 1 << 20 // 1 MiB

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
		if err != io.EOF && err != io.ErrUnexpectedEOF {
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

// deSerializeUInt is retained for any external callers, but no longer used
// internally; readUint8 / readUint16 are preferred because they propagate
// read errors instead of swallowing them.
func deSerializeUInt(buf []byte) interface{} {
	switch len(buf) {
	case 1:
		return uint8(buf[0])
	case 2:
		return uint16(buf[1])*256 + uint16(buf[0])
	case 4:
		return uint32(buf[3])*16777216 + uint32(buf[2])*65536 + uint32(buf[1])*256 + uint32(buf[0])
	default:
		utils.Error.Printf("Buffer length=%d is of an unknown size", len(buf))
		return nil
	}
}

func main() {
	// Create new parser object
	parser := argparse.NewParser("print", "External Vehicle Interface Client (EVIC) Feeder")
	clientUrl := parser.String("u", "url", &argparse.Options{
		Required: false,
		Help:     "CAN driver URL",
		Default:  "localhost"})
	mapFile := parser.String("m", "mapfile", &argparse.Options{
		Required: false,
		Help:     "VSS-Vehicle mapping data filename",
		Default:  "VssVehicle.cvt"})
	sclDataFile := parser.String("s", "scldatafile", &argparse.Options{
		Required: false,
		Help:     "VSS-Vehicle scaling data filename",
		Default:  "VssVehicleScaling.json"})
	logFile := parser.Flag("", "logfile", &argparse.Options{Required: false, Help: "outputs to logfile in ./logs folder"})
	logLevel := parser.Selector("", "loglevel", []string{"trace", "debug", "info", "warn", "error", "fatal", "panic"}, &argparse.Options{
		Required: false,
		Help:     "changes log output level",
		Default:  "info"})
	stateDB := parser.Selector("d", "statestoragedb", []string{"sqlite", "redis", "none"}, &argparse.Options{Required: false,
		Help: "Statestorage must be either sqlite, redis, or none", Default: "redis"})
	dbFile := parser.String("f", "dbfile", &argparse.Options{
		Required: false,
		Help:     "statestorage database filename",
		Default:  "../../server/vissv2server/serviceMgr/statestorage.db"})
	// Parse input
	err := parser.Parse(os.Args)
	if err != nil {
		utils.Error.Print(parser.Usage(err))
		os.Exit(1)
	}
	stateDbType = *stateDB
	canDriverUrl = *clientUrl

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
	default:
		utils.Error.Printf("Unknown state storage type = %s", stateDbType)
		os.Exit(1)
	}

	// Pipeline channels are buffered so a slow downstream cannot back-pressure
	// the WS/UDS readers; further safety is in the non-blocking sends below.
	const pipelineBuf = 32
	vssInputChan := make(chan DomainData, pipelineBuf)
	vssOutputChan := make(chan DomainData, pipelineBuf)
	vehicleInputChan := make(chan DomainData, pipelineBuf)
	vehicleOutputChan := make(chan DomainData, pipelineBuf)

	feederMap := readFeederMap(*mapFile)
	if feederMap == nil {
		utils.Error.Printf("Feeder map (%s) could not be loaded; aborting.", *mapFile)
		os.Exit(1)
	}
	scalingDataList = readscalingDataList(*sclDataFile)
	if scalingDataList == nil {
		utils.Error.Printf("Scaling data list (%s) could not be loaded; aborting.", *sclDataFile)
		os.Exit(1)
	}
	go initVSSInterfaceMgr(vssInputChan, vssOutputChan)
	go initVehicleInterfaceMgr(feederMap, vehicleInputChan, vehicleOutputChan)
	utils.Info.Printf("Feeder started.")

	for {
		select {
		case vssInData := <-vssInputChan:
			out := convertDomainData(true, vssInData, feederMap) // VSS -> Vehicle
			if out.Name == "" {
				continue // miss already logged
			}
			pipelineSend(vehicleOutputChan, out, "vehicleOutputChan")
		case vehicleInData := <-vehicleInputChan:
			out := convertDomainData(false, vehicleInData, feederMap) // Vehicle -> VSS
			if out.Name == "" {
				continue
			}
			pipelineSend(vssOutputChan, out, "vssOutputChan")
		}
	}
}

// pipelineSend pushes onto a pipeline channel non-blockingly so a wedged
// downstream cannot back-pressure the main loop. Drops with a log on overflow.
func pipelineSend(ch chan DomainData, data DomainData, chName string) {
	select {
	case ch <- data:
	default:
		utils.Error.Printf("pipelineSend: %s full, dropping datapoint for %q", chName, data.Name)
	}
}
