/**
* (C) 2023 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE file in this repository.
*
**/

package main

import (
	"encoding/json"
	"flag"
	"github.com/akamensky/argparse"
	"github.com/covesa/vissr/utils"
	"github.com/gorilla/websocket"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"
)

type DomainData struct {
	Name  string
	Value string
}

var Upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

var MuxServer = []*http.ServeMux{
	http.NewServeMux(),
}

var canDriverUrl string

func initVehicleInterfaceMgr(inputChan chan DomainData, outputChan chan DomainData) {
	CanDriverOutputChan := make(chan DomainData, 1)
	CanDriverInputChan := make(chan DomainData, 1)
	go initCanDriverOutput(CanDriverOutputChan) // RxData
	go initCanDriverInput(CanDriverInputChan)   // TxData

	for {
		select {
		case outData := <-outputChan:
			utils.Info.Printf("Data from the vehicle interface: Name=%s, Value=%s", outData.Name, outData.Value)
			CanDriverOutputChan <- outData
		case inData := <-CanDriverInputChan:
			utils.Info.Printf("Data to the vehicle interface: Name=%s, Value=%s", inData.Name, inData.Value)
		}
	}
}

func initCanDriverOutput(outputChan chan DomainData) { // CAN driver WS client -> RxData
	scheme := "ws"
	portNum := "8001"
	var addr = flag.String("addr", canDriverUrl+":"+portNum, "http service address")
	dataSessionUrl := url.URL{Scheme: scheme, Host: *addr, Path: ""}
	dialer := websocket.Dialer{
		HandshakeTimeout: time.Second,
		ReadBufferSize:   1024,
		WriteBufferSize:  1024,
	}
	conn := reDialer(dialer, dataSessionUrl)
	go canDriverClient(conn, outputChan)
}

func canDriverClient(conn *websocket.Conn, clientChan chan DomainData) {
	defer conn.Close()
	for {
		domainData := <-clientChan
		request := `{"path":"` + domainData.Name + `", "value":"` + domainData.Value + `"}`
		err := conn.WriteMessage(websocket.BinaryMessage, []byte(request))
		if err != nil {
			utils.Error.Printf("canDriverClient:Request write error:%s\n", err)
			return
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

func initCanDriverInput(inputChan chan DomainData) { // CAN driver WS server -> TxData
	serverHandler := makeServerHandler(inputChan)
	MuxServer[0].HandleFunc("/", serverHandler)
	utils.Error.Fatal(http.ListenAndServe(":8002", MuxServer[0]))
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

		serverChannel <- domainData
	}
}

func convertToDomainData(message string) DomainData { // {"path":"x.y.z", "value":"123"}
	var domainData DomainData
	var messageMap map[string]interface{}
	err := json.Unmarshal([]byte(message), &messageMap)
	if err != nil {
		utils.Error.Printf("convertToDomainData:Unmarshal error=%s", err)
		domainData.Name = ""
		return domainData
	}
	domainData.Name = messageMap["path"].(string)
	domainData.Value = messageMap["value"].(string)
	return domainData
}

func fileExists(filename string) bool {
	info, err := os.Stat(filename)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
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

func main() {
	// Create new parser object
	parser := argparse.NewParser("print", "External Vehicle Interface Client Simulator")
	clientUrl := parser.String("u", "url", &argparse.Options{
		Required: false,
		Help:     "CAN driver URL",
		Default:  "localhost"})
	mapFile := parser.String("m", "mapfile", &argparse.Options{
		Required: false,
		Help:     "Vehicle-VSS mapping data filename",
		Default:  "VehicleVssMapData.json"})
	logFile := parser.Flag("", "logfile", &argparse.Options{Required: false, Help: "outputs to logfile in ./logs folder"})
	logLevel := parser.Selector("", "loglevel", []string{"trace", "debug", "info", "warn", "error", "fatal", "panic"}, &argparse.Options{
		Required: false,
		Help:     "changes log output level",
		Default:  "info"})
	// Parse input
	err := parser.Parse(os.Args)
	if err != nil {
		utils.Error.Print(parser.Usage(err))
	}
	canDriverUrl = *clientUrl

	utils.InitLog("feeder-log.txt", "./logs", *logFile, *logLevel)

	canSignals := []string{"LVoltSysSt", "GpsFxTy", "GpsLong", "TrMetRead", "VehSpd"} // simulated signals

	vehicleInputChan := make(chan DomainData, 1)
	vehicleOutputChan := make(chan DomainData, 1)

	utils.Info.Printf("Initializing the feeder for mapping file %s.", *mapFile)
	go initVehicleInterfaceMgr(vehicleInputChan, vehicleOutputChan)

	for {
		select {
		case vehicleInData := <-vehicleInputChan:
			utils.Info.Printf("CAN Driver: TXData received. Path = %s, Value=%s", vehicleInData.Name, vehicleInData.Value)
		default:
			time.Sleep(10 * time.Second)
			var domainData DomainData // simulated input data
			domainData.Name = canSignals[rand.Intn(len(canSignals))]
			domainData.Value = strconv.Itoa(rand.Intn(2))
			vehicleOutputChan <- domainData
		}
	}
}
