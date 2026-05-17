/**
* (C) 2023 Ford Motor Company
* (C) 2023 Volvo Cars
* All files and artifacts are licensed under the provisions of the license provided by the LICENSE file in this repository.
*
**/

package main

import (
	"encoding/json"
	"errors"
	"io"
	"math/rand"
	"net"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/akamensky/argparse"
	"github.com/covesa/vissr/utils"
	"github.com/go-redis/redis"
	"github.com/petervolvowinz/viss-rl-interfaces"
)

var (
	dbPath   string
	feedChan string
)

type DomainData struct {
	Name  string
	Value string
}

type FeederMap struct {
	VssName     string `json:"vssdata"`
	VehicleName string `json:"vehicledata"`
}

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

// readFeederMap pre-fix would index fMap[0] in the info log even when the
// JSON unmarshal produced an empty slice (legal but unhelpful payload).
// That panicked the feeder on any empty mapfile.
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
	if len(fMap) > 0 {
		utils.Info.Printf("readFeederMap():fMap[0].VssName=%s", fMap[0].VssName)
	} else {
		utils.Info.Printf("readFeederMap():loaded 0 entries from %s", mapFilename)
	}
	return fMap
}

func initVSSInterfaceMgr(inputChan chan DomainData, outputChan chan DomainData) {
	udsChan := make(chan DomainData, 1)
	feederClient := initRedisClient()
	go initUdsEndpoint(udsChan, feederClient)
	for {
		select {
		case outData := <-outputChan:
			utils.Info.Printf("Data written to statestorage: Name=%s, Value=%s", outData.Name, outData.Value)
			status := redisSet(feederClient, outData.Name, outData.Value /*utils.GetRfcTime()*/, utils.GetTimeInMilliSecs())
			if status != 0 {
				utils.Error.Printf("initVSSInterfaceMgr():Redis write failed")
			}
		case actuatorData := <-udsChan:
			inputChan <- actuatorData
		}
	}
}

func initRedisClient() *redis.Client {
	return redis.NewClient(&redis.Options{
		Network:  "unix",
		Addr:     dbPath,
		Password: "",
		DB:       1,
	})
}

func redisSet(client *redis.Client, path string, val string, ts string) int {
	dp, err := marshalDatapointJSON(val, ts)
	if err != nil {
		utils.Error.Printf("redisSet:Marshal error=%v", err)
		return -1
	}
	if err := client.Set(path, dp, time.Duration(0)).Err(); err != nil {
		utils.Error.Printf("Job failed. Err=%s", err)
		return -1
	}
	return 0
}

// initUdsEndpoint pre-fix accepted exactly one connection then read into a
// fixed 512-byte buffer with no framing; any longer message was truncated
// silently and produced an unmarshalable fragment. Connection drops left the
// feeder dead. The fixed version loops on Accept, uses 8 KiB, and drops
// oversized frames.
func initUdsEndpoint(udsChan chan DomainData, redisClient *redis.Client) {
	os.Remove(feedChan)
	listener, err := net.Listen("unix", feedChan) //the file must be the same as declared in the feeder-registration.json that the service mgr reads
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
// had four unchecked .(string)/.(map) assertions.
func splitToDomainDataAndTs(serverMessage string) (DomainData, string, bool) {
	var domainData DomainData
	var serverMessageMap map[string]interface{}
	if err := json.Unmarshal([]byte(serverMessage), &serverMessageMap); err != nil {
		utils.Error.Printf("splitToDomainDataAndTs:Unmarshal error=%s", err)
		return domainData, "", false
	}
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

// initVehicleInterfaceMgr_2 pre-fix used a single-case `select` inside `for`,
// which spins forever burning CPU once the input channel is closed. The fixed
// version uses a plain for-range and returns when the publisher closes pub_Chan.
func initVehicleInterfaceMgr_2(pub_Chan chan DomainData, outputChan chan viss_rl_interfaces.ValueChannel) {
	for outData := range pub_Chan {
		data := viss_rl_interfaces.ValueChannel{
			Name:  outData.Name,
			Value: outData.Value,
		}
		outputChan <- data
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

func covertChannelDataToString(data any) string {
	switch data.(type) {
	case float64:
		return strconv.FormatFloat(data.(float64), 'f', -1, 64)
	case int64:
		return strconv.FormatInt(data.(int64), 10)
	case bool:
		return strconv.FormatBool(data.(bool))
	case []byte:
		return string(data.([]byte))
	}
	return ""
}

func TouchFile(name string) error {
	file, err := os.OpenFile(name, os.O_RDONLY|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	return file.Close()
}

// RemotiveLabsBroker pre-fix had three serious issues:
//   1. Double-close of readQuitSignal: the signal handler closed it on SIGTERM
//      AND the WriterReader goroutine closed it again on return - the second
//      close panics. Fixed by giving the signal handler sole ownership via
//      sync.Once.
//   2. ch := make(chan int, 2) was drained by `os.Exit(<-ch | <-ch)` *after* an
//      infinite for-select - unreachable. Dropped that line.
//   3. The for-select had no exit path: when WriterReader returned (errored or
//      cleanly), the for loop kept running on dead channels. Added a quit
//      channel that the WriterReader goroutine signals on return.
func RemotiveLabsBroker() {
	vssInputChan := make(chan DomainData, 1)
	vssOutputChan := make(chan DomainData, 1)
	vehicleOutputChan := make(chan DomainData, 1)

	streamQuitSignal := make(chan struct{}, 1)
	readQuitSignal := make(chan struct{}, 1)
	var quitOnce sync.Once
	closeQuits := func() {
		quitOnce.Do(func() {
			close(streamQuitSignal)
			close(readQuitSignal)
		})
	}

	sig := make(chan os.Signal, 1)
	api := viss_rl_interfaces.GetWriterReaderlApi()
	signal.Notify(sig, os.Interrupt, os.Kill, syscall.SIGTERM)
	go func() { // listen for system interrupt, quit streaming if so...
		<-sig
		closeQuits()
	}()

	writerChannel := make(chan viss_rl_interfaces.ValueChannel, 1)
	readerChannel := make(chan viss_rl_interfaces.ValueChannel, 1)

	utils.Info.Printf("feeder for rl starting...")
	apiDone := make(chan struct{})
	go func() {
		defer close(apiDone)
		err := api.WriterReader(readQuitSignal, writerChannel, readerChannel)
		if err != nil {
			utils.Error.Printf("WriterReader error: %v", err)
		}
		utils.Info.Printf("subscribing is done")
	}()

	go initVSSInterfaceMgr(vssInputChan, vssOutputChan)
	go initVehicleInterfaceMgr_2(vssInputChan, writerChannel)

	utils.Info.Printf("feeder for rl started")
	for {
		select {
		case vssInData := <-vssInputChan:
			vehicleOutputChan <- convertDomainData("VSS", vssInData, nil)
		case vehicleInData := <-readerChannel:
			domainData := DomainData{
				Name:  vehicleInData.Name,
				Value: covertChannelDataToString(vehicleInData.Value),
			}
			vssOutputChan <- domainData
		case <-apiDone:
			closeQuits()
			return
		}
	}
}

func Simulation(mapFile *string) {
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

func main() {
	parser := argparse.NewParser("print", "Data feeder for the Vehicle tree") // The root node name Vehicle must be synched with the feeder-registration.json file.

	mapFile := parser.String("m", "mapfile", &argparse.Options{
		Required: false,
		Help:     "Vehicle-VSS mapping data filename",
		Default:  "VehicleVssMapData.json"})

	logFile := parser.Flag("", "logfile", &argparse.Options{Required: false, Help: "outputs to logfile in ./logs folder"})
	logLevel := parser.Selector("", "loglevel", []string{"trace", "debug", "info", "warn", "error", "fatal", "panic"}, &argparse.Options{
		Required: false,
		Help:     "changes log output level",
		Default:  "info"})

	dataprovider := parser.String("p", "dataprovider", &argparse.Options{
		Required: false,
		Help:     "the south bound provider of data stream",
		Default:  "sim",
	})

	dbp := parser.String("", "rdb", &argparse.Options{
		Required: false,
		Help:     "Set the path and redis db file",
		Default:  "/var/tmp/vissv2/redisDB.sock"})

	fch := parser.String("", "fch", &argparse.Options{
		Required: false,
		Help:     "Set the path and redis channel",
		Default:  "/var/tmp/vissv2/server-feeder-channel.sock"})

	// Parse input. Pre-fix this only logged the error and continued running
	// with whatever defaults / zero values had been left in place.
	err := parser.Parse(os.Args)
	if err != nil {
		utils.Error.Print(parser.Usage(err))
		os.Exit(1)
	}
	dbPath = *dbp
	feedChan = *fch

	utils.InitLog("feeder-log.txt", "./logs", *logFile, *logLevel)
	utils.Info.Printf("db path is=%s", dbPath)
	utils.Info.Printf("Running version 0.1.0")

	if err := TouchFile(feedChan); err != nil {
		utils.Error.Printf("feed-channel file not created path=%s err=%v", feedChan, err)
		os.Exit(1)
	}

	switch *dataprovider {
	case "remotive":
		RemotiveLabsBroker()
	case "sim":
		Simulation(mapFile)
	default:
		utils.Error.Printf("Unknown dataprovider=%q (expected one of: remotive, sim)", *dataprovider)
		os.Exit(1)
	}
}
