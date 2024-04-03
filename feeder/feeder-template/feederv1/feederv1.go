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
	"github.com/akamensky/argparse"
	"github.com/covesa/vissr/utils"
	"github.com/go-redis/redis"
	_ "github.com/mattn/go-sqlite3"
	"math/rand"
	"net"
	"os"
	"strconv"
	"time"
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
	//utils.Info.Printf("readFeederMap():fMap[0].VssName=%s", fMap[0].VssName)
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
				utils.Error.Printf("initVSSInterfaceMgr():Redis write failed")
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
		dp := `{"val":"` + val + `", "ts":"` + ts + `"}`
		err := redisClient.Set(path, dp, time.Duration(0)).Err()
		if err != nil {
			utils.Error.Printf("Job failed. Err=%s", err)
			return -1
		}
		return 0
	}
	return -1
}

func initUdsEndpoint(udsChan chan DomainData) {
	os.Remove("/var/tmp/vissv2/server-feeder-channel.sock")
	listener, err := net.Listen("unix", "/var/tmp/vissv2/server-feeder-channel.sock") //the file must be the same as declared in the feeder-registration.json that the service mgr reads
	if err != nil {
		utils.Error.Printf("initUdsEndpoint:UDS listen failed, err = %s", err)
		os.Exit(-1)
	}
	conn, err := listener.Accept()
	if err != nil {
		utils.Error.Printf("initUdsEndpoint:UDS accept failed, err = %s", err)
		os.Exit(-1)
	}
	defer conn.Close()
	buf := make([]byte, 512)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			utils.Error.Printf("initUdsEndpoint:Read failed, err = %s", err)
			continue
		}
		utils.Info.Printf("Feeder:Server message: %s", string(buf[:n]))
		domainData, _ := splitToDomainDataAndTs(string(buf[:n]))
		udsChan <- domainData
	}
}

func splitToDomainDataAndTs(serverMessage string) (DomainData, string) { // server={"dp": {"ts": "Z","value": "Y"},"path": "X"}, redis={"value":"xxx", "ts":"zzz"}
	var domainData DomainData
	var serverMessageMap map[string]interface{}
	err := json.Unmarshal([]byte(serverMessage), &serverMessageMap)
	if err != nil {
		utils.Error.Printf("splitToDomainDataAndTs:Unmarshal error=%s", err)
		return domainData, ""
	}
	domainData.Name = serverMessageMap["path"].(string)
	dpMap := serverMessageMap["dp"].(map[string]interface{})
	domainData.Value = dpMap["value"].(string)
	return domainData, dpMap["ts"].(string)
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

func calcInputValue(iteration int, setValue string) string {
	setVal, _ := strconv.Atoi(setValue)
	newVal := setVal - 10 + iteration
	return strconv.Itoa(newVal)
}

func selectRandomInput(fMap []FeederMap) DomainData {
	var domainData DomainData
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

func convertDomainData(inDomain string, inData DomainData, feederMap []FeederMap) DomainData {
	var outData DomainData
	outName := searchMap(feederMap, inDomain, inData.Name)
	if outName == "" {
		utils.Error.Printf("Domain mapping failed")
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
	// Parse input
	err := parser.Parse(os.Args)
	if err != nil {
		utils.Error.Print(parser.Usage(err))
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
