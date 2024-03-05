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
	"github.com/akamensky/argparse"
	"github.com/go-redis/redis"
	_ "github.com/mattn/go-sqlite3"
	"github.com/bradfitz/gomemcache/memcache"
	"github.com/w3c/automotive-viss2/utils"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
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

var scalingDataList []string

var redisClient *redis.Client
var memcacheClient *memcache.Client
var dbHandle *sql.DB
var stateDbType string

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
	for {
		mapElement := readElement(treeFp)
		if mapElement.Name == "" {
			break
		}
		feederMap = append(feederMap, mapElement)
	}
	treeFp.Close()
	return feederMap
}

// The reading order must be aligned with the reading order by the Domain Conversion Tool
func readElement(treeFp *os.File) FeederMap {
	var feederMap FeederMap
	feederMap.MapIndex = deSerializeUInt(readBytes(2, treeFp)).(uint16)
	//utils.Info.Printf("feederMap.MapIndex=%d\n", feederMap.MapIndex)

	NameLen := deSerializeUInt(readBytes(1, treeFp)).(uint8)
	feederMap.Name = string(readBytes((uint32)(NameLen), treeFp))
	//utils.Info.Printf("NameLen=%d\n", NameLen)
	//utils.Info.Printf("feederMap.Name=%s\n", feederMap.Name)

	feederMap.Type = (int8)(deSerializeUInt(readBytes(1, treeFp)).(uint8))
	//utils.Info.Printf("feederMap.Type=%d\n", feederMap.Type)

	feederMap.Datatype = (int8)(deSerializeUInt(readBytes(1, treeFp)).(uint8))
	//utils.Info.Printf("feederMap.Datatype=%d\n", feederMap.Datatype)

	feederMap.ConvertIndex = deSerializeUInt(readBytes(2, treeFp)).(uint16)
	//utils.Info.Printf("feederMap.ConvertIndex=%d\n", feederMap.ConvertIndex)

	return feederMap
}

func readBytes(numOfBytes uint32, treeFp *os.File) []byte {
	if numOfBytes > 0 {
		buf := make([]byte, numOfBytes)
		treeFp.Read(buf)
		return buf
	}
	return nil
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
	case "memcache":
		dp := `{"val":"` + val + `", "ts":"` + ts + `"}`
		err := memcacheClient.Set(&memcache.Item{Key: path, Value: []byte(dp)})
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
	signalIndex := getRandomVssfMapIndex(fMap)
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
	utils.Info.Printf("Simulated data from Vehicle interface: Name=%s, Value=%s", domainData.Name, domainData.Value)
	return domainData
}

func getRandomVssfMapIndex(fMap []FeederMap) int {
	signalIndex := rand.Intn(len(fMap))
	for strings.Contains(fMap[signalIndex].Name, ".") { // assuming vehicle if names do not contain dot...
		signalIndex = (signalIndex + 1) % (len(fMap) - 1)
	}
	return signalIndex
}

func convertDomainData(north2SouthConv bool, inData DomainData, feederMap []FeederMap) DomainData {
	var outData DomainData
	matchIndex := sort.Search(len(feederMap), func(i int) bool { return feederMap[i].Name >= inData.Name })
	if matchIndex == len(feederMap) || feederMap[matchIndex].Name != inData.Name {
		matchIndex = -1
	}
	outData.Name = feederMap[feederMap[matchIndex].MapIndex].Name
	outData.Value = convertValue(inData.Value, feederMap[matchIndex].ConvertIndex,
		feederMap[matchIndex].Datatype, feederMap[feederMap[matchIndex].MapIndex].Datatype, north2SouthConv)
	return outData
}

func convertValue(value string, convertIndex uint16, inDatatype int8, outDatatype int8, north2SouthConv bool) string {
	switch convertIndex {
	case 0: // no conversion
		return value
	default: // call to conversion method
		var convertDataMap interface{}
		err := json.Unmarshal([]byte(scalingDataList[convertIndex-1]), &convertDataMap)
		if err != nil {
			utils.Error.Printf("convertValue:Error unmarshal scalingDataList item=%s", scalingDataList[convertIndex-1])
			return ""
		}
		switch vv := convertDataMap.(type) {
		case map[string]interface{}:
			return enumConversion(vv, north2SouthConv, value)
		case interface{}:
			return linearConversion(vv.([]interface{}), north2SouthConv, value)
		default:
			utils.Error.Printf("convertValue: convert data=%s has unknown format.", scalingDataList[convertIndex-1])
		}
	}
	return ""
}

func enumConversion(enumObj map[string]interface{}, north2SouthConv bool, inValue string) string { // enumObj = {"Key1":"value1", .., "KeyN":"valueN"}, k is VSS value
	for k, v := range enumObj {
		if north2SouthConv {
			if k == inValue {
				return v.(string)
			}
		} else {
			if v.(string) == inValue {
				return k
			}
		}
	}
	utils.Error.Printf("enumConversion: value=%s is out of range.", inValue)
	return ""
}

func linearConversion(coeffArray []interface{}, north2SouthConv bool, inValue string) string { // coeffArray = [A, B], y = Ax +B, y is VSS value
	var A float64
	var B float64
	var x float64
	var err error
	if x, err = strconv.ParseFloat(inValue, 64); err != nil {
		utils.Error.Printf("linearConversion: input value=%s cannot be converted to float.", inValue)
		return ""
	}
	A = coeffArray[0].(float64)
	B = coeffArray[1].(float64)
	var y float64
	if north2SouthConv {
		y = A*x + B
	} else {
		y = (x - B) / A
	}
	return strconv.FormatFloat(y, 'f', -1, 32)
}

func main() {
	// Create new parser object
	parser := argparse.NewParser("print", "Data feeder template version 2")
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
	stateDB := parser.Selector("d", "statestorage", []string{"sqlite", "redis", "memcache", "none"}, &argparse.Options{Required: false,
		Help: "Statestorage must be either sqlite, redis, memcache, or none", Default: "redis"})
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

	vssInputChan := make(chan DomainData, 1)
	vssOutputChan := make(chan DomainData, 1)
	vehicleInputChan := make(chan DomainData, 1)
	vehicleOutputChan := make(chan DomainData, 1)

	utils.Info.Printf("Initializing the feeder for mapping file %s.", *mapFile)
	feederMap := readFeederMap(*mapFile)
	scalingDataList = readscalingDataList(*sclDataFile)
	go initVSSInterfaceMgr(vssInputChan, vssOutputChan)
	go initVehicleInterfaceMgr(feederMap, vehicleInputChan, vehicleOutputChan)

	for {
		select {
		case vssInData := <-vssInputChan:
			vehicleOutputChan <- convertDomainData(true, vssInData, feederMap)
		case vehicleInData := <-vehicleInputChan:
			vssOutputChan <- convertDomainData(false, vehicleInData, feederMap)
		}
	}
}
