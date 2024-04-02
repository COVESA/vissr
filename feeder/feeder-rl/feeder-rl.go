/**
* (C) 2023 Ford Motor Company
* (C) 2023 Volvo Cars
* All files and artifacts are licensed under the provisions of the license provided by the LICENSE file in this repository.
*
**/

package main

import (
	"encoding/json"
	"github.com/akamensky/argparse"
	"github.com/covesa/vissr/utils"
	"github.com/go-redis/redis"
	"github.com/petervolvowinz/viss-rl-interfaces"
	"log"
	"math/rand"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
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
	utils.Info.Printf("readFeederMap():fMap[0].VssName=%s", fMap[0].VssName)
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
			status := redisSet(feederClient, outData.Name, outData.Value, utils.GetRfcTime())
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
	dp := `{"value":"` + val + `", "ts":"` + ts + `"}`
	err := client.Set(path, dp, time.Duration(0)).Err()
	if err != nil {
		utils.Error.Printf("Job failed. Err=%s", err)
		return -1
	}
	return 0
}

func initUdsEndpoint(udsChan chan DomainData, redisClient *redis.Client) {
	os.Remove(feedChan)
	listener, err := net.Listen("unix", feedChan) //the file must be the same as declared in the feeder-registration.json that the service mgr reads
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

func initVehicleInterfaceMgr_2(pub_Chan chan DomainData, outputChan chan viss_rl_interfaces.ValueChannel) {
	for {
		select {
		case outData := <-pub_Chan:
			data := &viss_rl_interfaces.ValueChannel{
				Name:  outData.Name,
				Value: outData.Value,
			}
			outputChan <- *data
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

func covertChannelDataToString(data any) string {
	switch data.(type) {
	case float64:
		{
			str := strconv.FormatFloat(data.(float64), 'f', -1, 64)
			return str
		}
	case int64:
		{
			str := strconv.FormatInt(data.(int64), 10)
			return str
		}
	case bool:
		{
			str := strconv.FormatBool(data.(bool))
			return str
		}
	case []byte:
		return string(data.([]byte))
	}
	return ""
}

func TouchFile(name string) error {
	file, err := os.OpenFile(name, os.O_RDONLY|os.O_CREATE, 777)
	if err != nil {
		return err
	}
	return file.Close()
}

func RemotiveLabsBroker() {
	vssInputChan := make(chan DomainData, 1)
	vssOutputChan := make(chan DomainData, 1)
	vehicleOutputChan := make(chan DomainData, 1)

	// Retrieving an instance of the SignalApi, start receiving data from the broker...
	streamQuitSignal := make(chan struct{}, 1)
	readQuitSignal := make(chan struct{}, 1)

	sig := make(chan os.Signal, 1)
	api := viss_rl_interfaces.GetWriterReaderlApi()
	signal.Notify(sig, os.Interrupt, os.Kill, syscall.SIGTERM) // listen to OS interrupt, like ctrl-c
	go func() {                                                // listen for system interrupt, quit streaming if so...
		<-sig
		close(streamQuitSignal)
		close(readQuitSignal)
	}()

	writerChannel := make(chan viss_rl_interfaces.ValueChannel, 1)
	readerChannel := make(chan viss_rl_interfaces.ValueChannel, 1)

	utils.Info.Printf("feeder for rl stsrting...")
	ch := make(chan int, 2)
	go func() {
		err := api.WriterReader(readQuitSignal, writerChannel, readerChannel)
		if err != nil {
			log.Println(err)
			ch <- 1
		}
		close(readQuitSignal)
		log.Println("subscribing is done")
		ch <- 0
	}()

	go initVSSInterfaceMgr(vssInputChan, vssOutputChan)
	go initVehicleInterfaceMgr_2(vssInputChan, writerChannel)

	utils.Info.Printf("feeder for rl started")
	for {
		select {
		case vssInData := <-vssInputChan:
			vehicleOutputChan <- convertDomainData("VSS", vssInData, nil)
		case vehicleInData := <-readerChannel:
			domainData := &DomainData{
				Name:  vehicleInData.Name,
				Value: covertChannelDataToString(vehicleInData.Value),
			}
			vssOutputChan <- *domainData
		}
	}

	os.Exit(<-ch | <-ch)
}

func Simulation(mapFile *string) {
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

func main() {
	// Create new parser object

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

	err := parser.Parse(os.Args)
	dbPath = *dbp
	feedChan = *fch
	if err != nil {
		utils.Error.Print(parser.Usage(err))
	}

	utils.InitLog("feeder-log.txt", "./logs", *logFile, *logLevel)
	utils.Info.Printf("db path is=%s", dbPath)
	utils.Info.Printf("Running version 0.1.0")

	err = TouchFile(feedChan)
	if err != nil {
		utils.Error.Printf("file not created error=%s", err)
		// utils.Error.Printf("could not create feeder channel file=#{err}")
		// os.Exit(0)
	}

	switch *dataprovider {
	case "remotive":
		RemotiveLabsBroker()
		break
	case "sim":
		Simulation(mapFile)
	}
}
