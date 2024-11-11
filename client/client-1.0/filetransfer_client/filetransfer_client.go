/**
* (C) 2024 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE file in this repository.
*
**/

package main

import (
	"crypto/sha1"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/hex"
	"bytes"
	"flag"
	"fmt"
	"io"
	"github.com/akamensky/argparse"
	"github.com/covesa/vissr/utils"
	"github.com/gorilla/websocket"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

var serverUrl string

var clientCert tls.Certificate
var caCertPool x509.CertPool

func initWebSocketControl() *websocket.Conn {
	return initWebSocket("8080")
}

func initWebSocketData() *websocket.Conn {
	return initWebSocket("8002")
}

func initWebSocket(portNum string) *websocket.Conn {
	scheme := "ws"
	if secConfig.TransportSec == "yes" {
		scheme = "wss"
		portNum = secConfig.WsSecPort
		websocket.DefaultDialer.TLSClientConfig = &tls.Config{
			Certificates: []tls.Certificate{clientCert},
			RootCAs:      &caCertPool,
		}
	}
	var addr = flag.String("addr"+portNum, serverUrl+":"+portNum, "http service address")
	dataSessionUrl := url.URL{Scheme: scheme, Host: *addr, Path: ""}
//	subProtocol := make([]string, 1)
//	subProtocol[0] = "VISSv2" + compression
	dialer := websocket.Dialer{
		HandshakeTimeout: time.Second,
		ReadBufferSize:   1024,
		WriteBufferSize:  1024,
//		Subprotocols:     subProtocol,
	}
	conn, _, err := dialer.Dial(dataSessionUrl.String(), nil)
	if err != nil {
		fmt.Printf("Data session dial error:%s\n", err)
		os.Exit(-1)
	}
	return conn
}

func controlClient(conn *websocket.Conn, ctrlChannel chan string) {
	defer conn.Close()
	for {
		select {
			case request := <-ctrlChannel:
				conn.WriteMessage(websocket.TextMessage, []byte(request))
				_, msg, err := conn.ReadMessage()
				if err != nil {
					utils.Error.Printf("Control response read error: %s", err)
					continue
				}
				ctrlChannel <- string(msg)
		}
	}
}

func dataClient(conn *websocket.Conn, dataChannel chan []byte) {
	defer conn.Close()
	for {
		select {
			case request := <-dataChannel:
				conn.WriteMessage(websocket.BinaryMessage, request)
				_, msg, err := conn.ReadMessage()
				if err != nil {
					utils.Error.Printf("Data response read error: %s", err)
					continue
				}
				dataChannel <- msg
		}
	}
}

func calculateHash(fileName string) string {
	f, err := os.Open(fileName)
	if err != nil {
		utils.Info.Printf("calculateHash: failed to open %s, err=%s", fileName, err)
	}
	defer f.Close()

	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		utils.Info.Printf("calculateHash: failed to read %s, err=%s", fileName, err)
	}

//	utils.Info.Printf("SHA-1 hash=%s", hex.EncodeToString(h.Sum(nil)))
	return hex.EncodeToString(h.Sum(nil))
}

func encodeDlRequest(uid string, messageNo byte, lastMessage byte, chunkSize uint32, chunk []byte) []byte { // request: uid(4)|messageNo(1)|chunkSize(4)| lastMessage(1)|chunk(N)
	dataReq := make([]byte, 4+1+4+1+chunkSize)
	uidBytes, _ := hex.DecodeString(uid)
	dataReq[0] = uidBytes[0]
	dataReq[1] = uidBytes[1]
	dataReq[2] = uidBytes[2]
	dataReq[3] = uidBytes[3]
	dataReq[4] = messageNo
	buf := new(bytes.Buffer)
	err := binary.Write(buf, binary.BigEndian, chunkSize)
	if err != nil {
		utils.Error.Printf("binary.Write failed:%s", err)
	}
	for i := 0; i < 4; i++ {
		dataReq[5+i] = buf.Bytes()[i]
	}
	dataReq[9] = lastMessage
	var i uint32
	for i = 0; i < chunkSize; i++ {
		dataReq[10+i] = chunk[i]
	}
	return dataReq
}

func encodeUlRequest(uid []byte, messageNo byte, status byte) []byte { // request: uid(4)|messageNo(1)|status(1)
	dataReq := make([]byte, 4+1+1)
	dataReq[0] = uid[0]
	dataReq[1] = uid[1]
	dataReq[2] = uid[2]
	dataReq[3] = uid[3]
	dataReq[4] = messageNo
	dataReq[5] = status
	return dataReq
}

func decodeDlResponse(dataResp []byte) (byte, byte) { // response: uid(4)|messageNo(1)|status(1)
//	uid := string(dataResp[:4])
	mNo := dataResp[4]
	status := dataResp[5]
	return mNo, status
}

func decodeUlResponse(dataResp []byte) ([]byte, byte, byte, uint32, []byte) {// response: uid(4)|messageNo(1)|chunkSize(4)| lastMessage(1)|chunk(N)
	uid := dataResp[:4]
	mNo := dataResp[4]
	var chunkSize uint32
	buf := bytes.NewReader(dataResp[5:9])
	err := binary.Read(buf, binary.BigEndian, &chunkSize)
	if err != nil {
		utils.Error.Println("binary.Read failed for chunkSize:", err)
		chunkSize = 0
	}
	lastMessage := dataResp[9]
	chunk := dataResp[10:]
	return uid, mNo, lastMessage, chunkSize, chunk
}

func getFileSize(fp *os.File) int {
	fi, err := fp.Stat()
	if err != nil {
		utils.Error.Printf("Server failed to get file size, error =%s", err)
		return 0
	}
	return int(fi.Size())
}

func readChunk(fname string, fp *os.File, readBuffer []byte) (int, []byte, byte) {
	n, err := fp.Read(readBuffer)
	if err != nil {
		if err == io.EOF {
			return n, readBuffer, byte(0x01)
		} else {
			utils.Error.Printf("Server failed to read %s, error =%s", fname, err)
			return 0, readBuffer, byte(0x01)
		}
	}
	return n, readBuffer, byte(0x00)
}

func writeChunk(fname string, fp *os.File, dataSize uint32, writeBuffer []byte) {
	var err error
	if dataSize != uint32(len(writeBuffer)) {
		buf := make([]byte, dataSize)
		for i := 0; uint32(i) < dataSize; i++ {
			buf[i] = writeBuffer[i]
		}
		_, err = fp.Write(buf)
	} else {
		_, err = fp.Write(writeBuffer)
	}
	if err != nil {
		utils.Error.Printf("Server failed to write %s, error =%s", fname, err)
	}
}

func downloadFile(dlFile string, numOfChunks int, ctrlChannel chan string, dataChannel chan []byte) {
	file, err := os.Open(dlFile)
	if err != nil {
		utils.Error.Printf("Server failed to open %s, error =%s", dlFile, err)
		return
	}
	defer file.Close()
	hash := calculateHash(dlFile)
	fileSize := getFileSize(file)
	readBuffer := make([]byte, fileSize/numOfChunks+1)
	uid := "1d878212"  //TODO: random generation
	ctrlChannel <- `{"action": "set","path": "Vehicle.DownloadFile","value":{"name": "` + dlFile + `", "hash":"` + hash + `","uid":"` + uid + `"},` +
			 ` "ts": "` + utils.GetRfcTime() + `"}`
	ctrlResp := <-ctrlChannel
	if strings.Contains(ctrlResp, "error") {
		utils.Error.Printf("Server responded with error message=%s", ctrlResp)
		return
	}
	sessionDone := false
	var bytesRead int
	var  dataReq, dataResp []byte
	messageNo := byte(0x01)
	var lastMessage byte
	bytesRead, readBuffer, lastMessage = readChunk(dlFile, file, readBuffer)
	for !sessionDone {
		dataReq = encodeDlRequest(uid, messageNo, lastMessage, uint32(bytesRead), readBuffer)
		dataChannel <- dataReq
		dataResp = <-dataChannel
		mNo, status := decodeDlResponse(dataResp)
		if status == byte(0x00) && mNo == messageNo {
			messageNo += 1
		} else {
			messageNo = mNo
			// rewind file
		}
		if lastMessage != 0 {
			utils.Info.Printf("Reading %s completed", dlFile)
			sessionDone = true
		}
		bytesRead, readBuffer, lastMessage = readChunk(dlFile, file, readBuffer)
	}
}

func uploadFile(ctrlChannel chan string, dataChannel chan []byte) {
	var ulFile, hash, uidString string
	ctrlChannel <- `{"action": "get","path": "Vehicle.UploadFile"}`
	ctrlResp := <-ctrlChannel
	var responseMap = make(map[string]interface{})
	if utils.MapRequest(ctrlResp, &responseMap) == 0 {
		if responseMap["error"] != nil {
			utils.Error.Printf("Server responded with error message=%s", ctrlResp)
			return
		} else {
			ulFile, hash, uidString = getFileDescriptorData(responseMap["value"].(interface{}))
		}
	} else {
		utils.Error.Printf("Response=%s is corrupt", ctrlResp)
		return
	}
	uid, _ := hex.DecodeString(uidString)
	file, err := os.Create(ulFile)  // overwriting any existing file...
	if err != nil {
		utils.Error.Printf("Server failed to create %s, error =%s", ulFile, err)
		return
	}
	defer file.Close()
	sessionDone := false
	messageNo := byte(0xFF)
	status := byte(0x00)
	for !sessionDone {
		dataReq := encodeUlRequest(uid, messageNo, status)
		dataChannel <- dataReq
		dataResp := <-dataChannel
		uidResp, mNo, lastMessage, chunkSize, chunk := decodeUlResponse(dataResp)
		messageNo = mNo
		if equalByteArray(uid, uidResp) && mNo == messageNo {
			writeChunk(ulFile, file, chunkSize, chunk)
			status = byte(0x00)
		} else {
			status = byte(0x01)
		}
		if lastMessage != 0 {
			file.Close()
			if calculateHash(ulFile) != hash {
//				status = byte(0x01)  not sent anyway...
			}
			sessionDone = true
			utils.Info.Printf("Writing %s completed", ulFile)
		}
	}
}

func equalByteArray(array1 []byte, array2 []byte) bool {
	for i := 0; i < len(array1); i++ {
		if array1[i] != array2[i] {
			return false
		}
	}
	return true
}

func getFileDescriptorData(value interface{}) (string, string, string) { // {"name": "xxx","hash": "yyy","uid": "zzz"}
	var name, hash, uid string
	for k, v := range value.(map[string]interface{}) {
		switch vv := v.(type) {
		case string:
//			utils.Info.Println(k, "is string", vv)
			if k == "name" {
				name = vv
			} else if k == "hash" {
				hash = vv
			} else if k == "uid" {
				uid = vv
			} else {
				utils.Info.Println(k, "is of an unknown type")
				return "", "", ""
			}
		default:
			utils.Info.Println(k, "is of an unknown type")
			return "", "", ""
		}
	}
	return name, hash, uid
}

func main() {
	// Create new parser object
	parser := argparse.NewParser("File transfer client", "Client for up/down-load of files according to VISSv3.0")

	// Create flags
	server_url := parser.String("u", "serverUrl", &argparse.Options{Required: true, Help: "IP/url to VISS server"})
	ftDirection := parser.Selector("f", "ftDirection", []string{"upload", "download"}, &argparse.Options{Required: false, Help:"file transfer direction:[upload,download]", Default:  "download"})
	dlFile := parser.String("d", "dlFName", &argparse.Options{Required: false, Help: "path to pathlist file", Default: "dlFile.txt"})
	chunks := parser.String("c", "chunks", &argparse.Options{Required: false, Help: "path to pathlist file", Default: "10"})
	logFile := parser.Flag("", "logfile", &argparse.Options{Required: false, Help: "outputs to logfile in ./logs folder"})
	logLevel := parser.Selector("", "loglevel", []string{"trace", "debug", "info", "warn", "error", "fatal", "panic"}, &argparse.Options{
		Required: false,
		Help:     "changes log output level",
		Default:  "info"})

	// Parse input
	err := parser.Parse(os.Args)
	if err != nil {
		fmt.Print(parser.Usage(err))
		//exits due to required info not provided by user
		os.Exit(1)
	}
	serverUrl = *server_url
	
	numOfChunks, err := strconv.Atoi(*chunks)
	if err != nil {
		fmt.Printf("CLI param chunks not a number. Defaults to 10.")
		numOfChunks = 10
	}

	utils.InitLog("client-log.txt", "./logs", *logFile, *logLevel)

	readTransportSecConfig()
	utils.Info.Printf("secConfig.TransportSec=%s", secConfig.TransportSec)
	if secConfig.TransportSec == "yes" {
		caCertPool = *prepareTransportSecConfig()
	}

	connC := initWebSocketControl()
	connD := initWebSocketData()

	ctrlChannel := make(chan string)
	dataChannel := make(chan []byte)

	go controlClient(connC, ctrlChannel)
	go dataClient(connD, dataChannel)

	if *ftDirection == "download" {
		downloadFile(*dlFile, numOfChunks, ctrlChannel, dataChannel)
	} else {
		uploadFile(ctrlChannel, dataChannel)
	}
}
