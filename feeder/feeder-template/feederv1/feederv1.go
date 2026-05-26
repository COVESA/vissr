/**
* (C) 2023 Ford Motor Company
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
	"strconv"
	"time"

	"github.com/akamensky/argparse"
	"github.com/covesa/vissr/utils"
	"github.com/go-redis/redis"
	_ "github.com/mattn/go-sqlite3"
)

type DomainData struct {
	Name  string
	Value string
}

type FeederMap struct {
	VssName     string `json:"vssdata"`
	VehicleName string `json:"vehicledata"`
}

var redisClient *redis.Client
var dbHandle *sql.DB
var stateDbType string

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

func readFeederMap(mapFilename string) []FeederMap {
	var fMap []FeederMap
	data, err := os.ReadFile(mapFilename)
	if err != nil {
		utils.Error.Printf("readFeederMap():%s error=%s", mapFilename, err)
		return nil
	}
	err = json.Unmarshal(data, &fMap)
	if err != nil {
		utils.Error.Printf("readFeederMap():unmarshal error=%s", err)
		return nil
	}
	return fMap
}

func initVSSInterfaceMgr(inputChan chan DomainData, outputChan chan DomainData) {
	udsChan := make(chan DomainData, 1)
	go initUdsEndpoint(udsChan)
	for {
		select {
		case outData := <-outputChan:
			utils.Info.Printf("Data written to statestorage: Name=%s, Value=%s", outData.Name, outData.Value)
			status := statestorageSet(outData.Name, outData.Value, utils.GetRfcTime())
			if status != 0 {
				utils.Error.Printf("initVSSInterfaceMgr():State storage write failed")
			}
		case actuatorData := <-udsChan:
			inputChan <- actuatorData
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
	}
	return -1
}

// The pre-fix initUdsEndpoint accepted exactly one connection then read into
// a fixed 512-byte buffer with no framing; any longer message was truncated
// silently and produced an unmarshalable fragment. The fixed version loops on
// Accept (reconnect support), uses an 8 KiB buffer, and drops oversized frames
// rather than feeding garbage downstream.
func initUdsEndpoint(udsChan chan DomainData) {
	const sockPath = "/var/tmp/vissv2/server-feeder-channel.sock"
	os.Remove(sockPath)
	listener, err := net.Listen("unix", sockPath) //the file must be the same as declared in the feeder-registration.json that the service mgr reads
	if err != nil {
		utils.Error.Printf("initUdsEndpoint:UDS listen failed, err = %s", err)
		os.Exit(-1)
	}
	defer listener.Close()
	for {
		conn, err := listener.Accept()
		if err != nil {
			utils.Error.Printf("initUdsEndpoint:UDS accept failed, err = %s. Retrying in 1s.", err)
			time.Sleep(1 * time.Second)
			continue
		}
		udsServeConn(conn, udsChan)
	}
}

const udsReadBuf = 8192

func udsServeConn(conn net.Conn, udsChan chan DomainData) {
	defer conn.Close()
	buf := make([]byte, udsReadBuf)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			if errors.Is(err, io.EOF) {
				utils.Info.Printf("initUdsEndpoint:peer closed, awaiting next connection")
			} else {
				utils.Error.Printf("initUdsEndpoint:Read failed, err = %s", err)
			}
			return
		}
		// n cannot exceed len(buf) but a full buffer suggests a truncated frame.
		if n == len(buf) {
			utils.Error.Printf("initUdsEndpoint:message at buffer size (%d); likely truncated, dropped", n)
			continue
		}
		utils.Info.Printf("Feeder:Server message: %s", string(buf[:n]))
		domainData, _, ok := splitToDomainDataAndTs(string(buf[:n]))
		if !ok {
			continue
		}
		select {
		case udsChan <- domainData:
		default:
			utils.Error.Printf("initUdsEndpoint:udsChan full, dropping %q", domainData.Name)
		}
	}
}

// splitToDomainDataAndTs parses a server message of the shape
//   {"dp": {"ts": "Z","value": "Y"},"path": "X"}
// and returns the extracted DomainData, the timestamp string, and ok=true.
// On any parse or shape error, returns zero values and ok=false. Pre-fix this
// had four unchecked .(string)/.(map) assertions and panicked on any malformed
// or partial message from the server.
func splitToDomainDataAndTs(serverMessage string) (DomainData, string, bool) {
	var domainData DomainData
	var serverMessageMap map[string]interface{}
	if err := json.Unmarshal([]byte(serverMessage), &serverMessageMap); err != nil {
		utils.Error.Printf("splitToDomainDataAndTs:Unmarshal error=%s", err)
		return domainData, "", false
	}
	name, ok := mapString(serverMessageMap, "path")
	if !ok {
		utils.Error.Printf("splitToDomainDataAndTs:missing/invalid path")
		return domainData, "", false
	}
	dpMap, ok := serverMessageMap["dp"].(map[string]interface{})
	if !ok {
		utils.Error.Printf("splitToDomainDataAndTs:missing/invalid dp object")
		return domainData, "", false
	}
	value, ok := mapString(dpMap, "value")
	if !ok {
		utils.Error.Printf("splitToDomainDataAndTs:missing/invalid value")
		return domainData, "", false
	}
	ts, ok := mapString(dpMap, "ts")
	if !ok {
		utils.Error.Printf("splitToDomainDataAndTs:missing/invalid ts")
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

func initVehicleInterfaceMgr(fMap []FeederMap, inputChan chan DomainData, outputChan chan DomainData) {
	var simCtx simulateDataCtx
	simCtx.RandomSim = true
	simCtx.Fmap = fMap
	for {
		select {
		case outData := <-outputChan:
			utils.Info.Printf("Data for calling the vehicle interface: Name=%s, Value=%s", outData.Name, outData.Value)
			// TODO: writing the data to the vehicle interface
			// simulate a slowly changing state of the signal
			simCtx.RandomSim = false
			simCtx.Path = outData.Name
			simCtx.SetVal = outData.Value
			simCtx.Iteration = 0

		default:
			time.Sleep(3 * time.Second)         // not to overload input channel
			inputChan <- simulateInput(&simCtx) // simulating signals read from the vehicle interface
		}
	}
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

// calcInputValue returns setValue - 10 + iteration as a decimal string. If
// setValue is not numeric the parse error is reported (was silently swallowed
// pre-fix) and the iteration offset alone is returned.
func calcInputValue(iteration int, setValue string) string {
	setVal, err := strconv.Atoi(setValue)
	if err != nil {
		utils.Error.Printf("calcInputValue:setValue=%q not an integer: %v", setValue, err)
	}
	newVal := setVal - 10 + iteration
	return strconv.Itoa(newVal)
}

// selectRandomInput now guards against an empty fMap (rand.Intn panics on 0).
func selectRandomInput(fMap []FeederMap) DomainData {
	var domainData DomainData
	if len(fMap) == 0 {
		return domainData
	}
	signalIndex := rand.Intn(len(fMap))
	domainData.Name = fMap[signalIndex].VehicleName
	domainData.Value = strconv.Itoa(rand.Intn(1000))
	utils.Info.Printf("Simulated data from Vehicle interface: Name=%s, Value=%s", domainData.Name, domainData.Value)
	return domainData
}

func searchMap(fMap []FeederMap, inDomain string, signalName string) string {
	for i := 0; i < len(fMap); i++ {
		if inDomain == "VSS" {
			if fMap[i].VssName == signalName {
				return fMap[i].VehicleName
			}
		} else {
			if fMap[i].VehicleName == signalName {
				return fMap[i].VssName
			}
		}
	}
	return ""
}

// convertDomainData pre-fix would happily emit a DomainData with Name="" when
// the lookup failed, poisoning downstream channels. It now returns the input
// unchanged on unmapped names rather than producing an empty-name datapoint.
func convertDomainData(inDomain string, inData DomainData, feederMap []FeederMap) DomainData {
	var outData DomainData
	outName := searchMap(feederMap, inDomain, inData.Name)
	if outName == "" {
		utils.Error.Printf("Domain mapping failed for %s/%s", inDomain, inData.Name)
		return inData
	}
	outData.Name = outName
	outData.Value = convertValue(inData.Value)
	return outData
}

func convertValue(value string) string { // TODO: value may need to be scaled, and have datatype changed
	return value
}

func main() {
	// Create new parser object
	parser := argparse.NewParser("print", "Data feeder template version 1") // The root node name Vehicle must be synched with the feeder-registration.json file.
	mapFile := parser.String("m", "mapfile", &argparse.Options{
		Required: false,
		Help:     "Vehicle-VSS mapping data filename",
		Default:  "VehicleVssMapData.json"})
	logFile := parser.Flag("", "logfile", &argparse.Options{Required: false, Help: "outputs to logfile in ./logs folder"})
	logLevel := parser.Selector("", "loglevel", []string{"trace", "debug", "info", "warn", "error", "fatal", "panic"}, &argparse.Options{
		Required: false,
		Help:     "changes log output level",
		Default:  "info"})
	stateDB := parser.Selector("s", "statestorage", []string{"sqlite", "redis", "none"}, &argparse.Options{Required: false,
		Help: "Statestorage must be either sqlite, redis, or none", Default: "redis"})
	dbFile := parser.String("f", "dbfile", &argparse.Options{
		Required: false,
		Help:     "statestorage database filename",
		Default:  "../../server/vissv2server/serviceMgr/statestorage.db"})
	// Parse input. Pre-fix this only logged the error and continued running
	// with whatever defaults / zero values had been left in place. Now we
	// exit non-zero so misconfiguration is obvious.
	err := parser.Parse(os.Args)
	if err != nil {
		utils.Error.Print(parser.Usage(err))
		os.Exit(1)
	}
	stateDbType = *stateDB

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
	default:
		utils.Error.Printf("Unknown state storage type = %s", stateDbType)
		os.Exit(1)
	}

	vssInputChan := make(chan DomainData, 1)
	vssOutputChan := make(chan DomainData, 1)
	vehicleInputChan := make(chan DomainData, 1)
	vehicleOutputChan := make(chan DomainData, 1)

	utils.Info.Printf("Initializing the feeder for mapping file %s.", *mapFile)
	feederMap := readFeederMap(*mapFile)
	if feederMap == nil {
		utils.Error.Printf("Failed to read feeder map %s.", *mapFile)
		os.Exit(1)
	}
	go initVSSInterfaceMgr(vssInputChan, vssOutputChan)
	go initVehicleInterfaceMgr(feederMap, vehicleInputChan, vehicleOutputChan)

	for {
		select {
		case vssInData := <-vssInputChan:
			vehicleOutputChan <- convertDomainData("VSS", vssInData, feederMap)
		case vehicleInData := <-vehicleInputChan:
			vssOutputChan <- convertDomainData("Vehicle", vehicleInData, feederMap)
		}
	}
}
