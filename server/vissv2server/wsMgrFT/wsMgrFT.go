/**
* (C) 2024 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE file in this repository.
*
**/
package wsMgrFT

import (
	utils "github.com/covesa/vissr/utils"
	"bytes"
	"os"
	"io"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"net/http"
	"github.com/gorilla/websocket"
)

var MuxServer = []*http.ServeMux{
	http.NewServeMux(),
}

var Upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

const MAXSESSIONS = 10
var clientChan []chan []byte
var sessionList [MAXSESSIONS]bool

var clientIndex int

var fileTransferCache []utils.FileTransferCache
const FILETRANSFERCACHESIZE = 10

type ChunkDataCache struct {
	MessageNo byte
	LastMessage byte
	ChunkSize []byte
	Chunk []byte
}
var chunkDataCache = ChunkDataCache{}

func WsMgrFTInit(ftChannel chan utils.FileTransferCache) {
	var clientRequest utils.FileTransferCache
	var dataMessage, dataResponse []byte
	fileTransferCache = initFileTransferCache()
	clientChan = make([]chan []byte, MAXSESSIONS)
	for i := 0; i< MAXSESSIONS; i++ {
		sessionList[i] = false
		clientChan[i] = make(chan []byte)
	}
	go initDataSessions(clientChan)

	var clientId int
	for {
		select {
		case clientRequest = <-ftChannel:
			clientRequest.Status = initFtSession(clientRequest)
			ftChannel <-clientRequest
			continue
		case dataMessage = <-clientChan[0]:
			clientId = 0
		case dataMessage = <-clientChan[1]:
			clientId = 1
		case dataMessage = <-clientChan[2]:
			clientId = 2
		case dataMessage = <-clientChan[3]:
			clientId = 3
		case dataMessage = <-clientChan[4]:
			clientId = 4
		case dataMessage = <-clientChan[5]:
			clientId = 5
		case dataMessage = <-clientChan[6]:
			clientId = 6
		case dataMessage = <-clientChan[7]:
			clientId = 7
		case dataMessage = <-clientChan[8]:
			clientId = 8
		case dataMessage = <-clientChan[9]:
			clientId = 9
		}
		dataResponse = getDataResponse(dataMessage)
		if len(dataResponse) > 0 {
			clientChan[clientId] <- dataResponse
		}
	}
}

func getDataResponse(req []byte) []byte {
	if len(req) > 6 {
		return getDataResponseDl(req)
	} else {
		return getDataResponseUl(req)
	}
}

func getDataResponseDl(req []byte) []byte {  // request: uid(4)|messageNo(1)|chunkSize(4)| lastMessage(1)|chunk(N)
	resp := make([]byte, 4+1+1)  // response: uid(4)|messageNo(1)|status(1)
	uid := []byte(req[:4])
	var messageNo uint8
	buf := bytes.NewReader(req[4:5])
	err := binary.Read(buf, binary.BigEndian, &messageNo)
	if err != nil {
		utils.Error.Println("binary.Read failed for messageNo:", err)
		return createDlResponse(uid, byte(0x00), byte(0xFF)) // terminate session error response
	}
	var chunkSize uint32
	buf = bytes.NewReader(req[5:9])
	err = binary.Read(buf, binary.BigEndian, &chunkSize)
	if err != nil {
		utils.Error.Println("binary.Read failed for chunkSize:", err)
		return createDlResponse(uid, byte(0x00), byte(0xFF)) // terminate session error response
	}
	var lastMessage uint8
	buf = bytes.NewReader(req[9:10])
	err = binary.Read(buf, binary.BigEndian, &lastMessage)
	if err != nil {
		utils.Error.Println("binary.Read failed for lastMessage:", err)
		return createDlResponse(uid, byte(0x00), byte(0xFF)) // terminate session error response
	}
	chunk := req[10:]
	cacheIndex := findFileTransferCacheIndex(uid)
	if cacheIndex != -1 {
		if uint32(len(chunk)) != chunkSize {
			return createDlResponse(uid, req[4], byte(0x01))
		}
		n, err := fileTransferCache[cacheIndex].FileDescriptor.Write(chunk)
		if err != nil {
			return createDlResponse(uid, req[4], byte(0x01))
		}
		fileTransferCache[cacheIndex].FileOffset += n
		if lastMessage != 0 {
			fileTransferCache[cacheIndex].FileDescriptor.Close()
			if calculateHash(fileTransferCache[cacheIndex].Path + fileTransferCache[cacheIndex].Name) != fileTransferCache[cacheIndex].Hash {
				return createDlResponse(uid, byte(0x00), byte(0x01))
			}
			fileTransferCache[cacheIndex].Uid = clearUid() // delete cache entry
			return createDlResponse(uid, req[4], byte(0x00))
		}
		return createDlResponse(uid, req[4], byte(0x00))
	} else {
		return createDlResponse(uid, byte(0x00), byte(0xFF)) // terminate session error response
	}
	return resp
}

func getDataResponseUl(req []byte) []byte {  // request: uid(4)|messageNo(1)|status(1)
	uid := req[:4]
	messageNo := req[4]
	status := req[5]
	lastMessage := byte(0x00)
	chunkSize := make([]byte,4)
	var chunk []byte
	cacheIndex := findFileTransferCacheIndex([]byte(uid))
	if cacheIndex != -1 {
		if status == byte(0x00) {
			var n int
			var err error
			chunk = make([]byte, fileTransferCache[cacheIndex].ChunkSize)
			n, err = fileTransferCache[cacheIndex].FileDescriptor.Read(chunk)
			if err != nil {
				if err == io.EOF {
					lastMessage = byte(0x01)
					fileTransferCache[cacheIndex].FileDescriptor.Close()
					fileTransferCache[cacheIndex].Uid = clearUid() // delete cache entry
				} else {
					return createUlResponse(uid, messageNo, lastMessage, []byte{0,0,0,0}, chunk)
				}
			}
			messageNo += 1
			fileTransferCache[cacheIndex].FileOffset += n
			buf := new(bytes.Buffer)
			err = binary.Write(buf, binary.BigEndian, uint32(n))
			if err != nil {
				utils.Error.Printf("binary.Write failed:%s", err)
				return createUlResponse(uid, messageNo, lastMessage, []byte{0,0,0,0}, nil)
			}
			for i := 0; i < 4; i++ {
				chunkSize[i] = buf.Bytes()[i]
			}
		} else if status == byte(0xFF) {
			return createUlResponse(uid, messageNo, lastMessage, []byte{0,0,0,0}, nil) // error
		} else {
			lastMessage, chunkSize, chunk = readChunkData(messageNo)
			if len(chunk) > 0 {
				return createUlResponse(uid, messageNo, lastMessage, chunkSize, chunk) // resend previous message
			} else {
				return createUlResponse(uid, messageNo, lastMessage, []byte{0,0,0,0}, nil)  //error
			}
		}
	} else {
		return createUlResponse(uid, messageNo, lastMessage, []byte{0,0,0,0}, nil)  //error
	}
	writeChunkData(messageNo, lastMessage, chunkSize, chunk)
	return createUlResponse(uid, messageNo, lastMessage, chunkSize, chunk)
}

func readChunkData(messageNo byte) (byte, []byte, []byte) {
	if messageNo != chunkDataCache.MessageNo {
		return byte(0), nil, nil
	}
	return chunkDataCache.LastMessage, chunkDataCache.ChunkSize, chunkDataCache.Chunk
}

func writeChunkData(messageNo byte, lastMessage byte, chunkSize []byte, chunk []byte) {
	chunkDataCache.MessageNo = messageNo
	chunkDataCache.LastMessage = lastMessage
	chunkDataCache.ChunkSize = chunkSize
	chunkDataCache.Chunk = chunk
}

func clearUid() [utils.UIDLEN]byte {
	return [utils.UIDLEN]byte{0}
}

func createDlResponse(uid []byte, messNo byte, status byte) []byte { // response: uid(4)|messageNo(1)|status(1)
		resp := make([]byte,6)
		resp[0] = uid[0]
		resp[1] = uid[1]
		resp[2] = uid[2]
		resp[3] = uid[3]
		resp[4] = messNo
		resp[5] = status
		return resp
}

func createUlResponse(uid []byte, messNo byte, lastMessage byte, chunkSize []byte, chunk []byte) []byte { // response: uid(4)|messageNo(1)|chunkSize(4)| lastMessage(1)|chunk(N)
	resp := make([]byte,4+1+4+1+len(chunk))
	resp[0] = uid[0]
	resp[1] = uid[1]
	resp[2] = uid[2]
	resp[3] = uid[3]
	resp[4] = messNo
	resp[5] = chunkSize[0]
	resp[6] = chunkSize[1]
	resp[7] = chunkSize[2]
	resp[8] = chunkSize[3]
	resp[9] = lastMessage
	for i := 0; i < len(chunk); i++ {
		resp[10+i] = chunk[i]
	}
		return resp
}

func initFtSession(clientRequest utils.FileTransferCache) int {
	status := 1  // assume error
	cacheIndex := getFileTransferCacheIndex(clientRequest.Uid)
	if cacheIndex != -1 {
		var fd *os.File
		var err error
		if !clientRequest.UploadTransfer { // download
			fd, err = os.Create(clientRequest.Path + clientRequest.Name) //overwrites existing file...
		} else { // upload
			fd, err = os.Open(clientRequest.Path + clientRequest.Name)
			if err != nil {
				utils.Error.Printf("Server failed to get file size, error =%s", err)
			} else {
				fileSize := getFileSize(fd)
				clientRequest.ChunkSize = fileSize/10 + 1
			}
		}
		if err == nil {
			populateFTCache(cacheIndex, clientRequest, fd)
			status = 0
		}
	}
	return status
}

func getFileSize(fp *os.File) int {
	fi, err := fp.Stat()
	if err != nil {
		utils.Error.Printf("Server failed to get file size, error =%s", err)
		return 0
	}
	return int(fi.Size())
}

func findFileTransferCacheIndex(uid []byte) int {
	for i := 0; i < FILETRANSFERCACHESIZE; i++ {
		if fileTransferCache[i].Uid == [utils.UIDLEN]byte(uid) {
			return i
		}
	}
	return -1
}

func getFileTransferCacheIndex(uid [utils.UIDLEN]byte) int {
	emptyUid := clearUid()
	for i := 0; i < FILETRANSFERCACHESIZE; i++ {
		if fileTransferCache[i].Uid == emptyUid {
			return i
		}
	}
	return -1
}

func initFileTransferCache() []utils.FileTransferCache {
	fileTransferCache := make([]utils.FileTransferCache, FILETRANSFERCACHESIZE)
	for i := 0; i < FILETRANSFERCACHESIZE; i++ {
		fileTransferCache[i].Uid = clearUid()
	}
	return fileTransferCache
}

func populateFTCache(cacheIndex int, clientData utils.FileTransferCache, fd *os.File) {
	fileTransferCache[cacheIndex].Uid = clientData.Uid
	fileTransferCache[cacheIndex].UploadTransfer = clientData.UploadTransfer
	fileTransferCache[cacheIndex].Name = clientData.Name
	fileTransferCache[cacheIndex].Path = clientData.Path
	fileTransferCache[cacheIndex].FileDescriptor = fd
	fileTransferCache[cacheIndex].FileOffset = 0
	fileTransferCache[cacheIndex].ChunkSize = clientData.ChunkSize
	fileTransferCache[cacheIndex].Hash = clientData.Hash
	fileTransferCache[cacheIndex].MessageNo = 0
}

func calculateHash(fileName string) string {
	f, err := os.Open(fileName)
	if err != nil {
		utils.Error.Printf("calculateHash: failed to open %s, err=%s", fileName, err)
		return ""
	}
	defer f.Close()

	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		utils.Info.Printf("calculateHash: failed to read %s, err=%s", fileName, err)
	}
//	utils.Info.Printf("SHA-1 hash=%x", h.Sum(nil))
	return hex.EncodeToString(h.Sum(nil))
}

func initDataSessions(clientChan []chan []byte) { // WS server
	serverHandler := makeServerHandler(clientChan)
	MuxServer[0].HandleFunc("/", serverHandler)
	utils.Error.Fatal(http.ListenAndServe(":8002", MuxServer[0]))
}

func makeServerHandler(clientChannel []chan []byte) func(http.ResponseWriter, *http.Request) {
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
			sessionIndex := getDataSessionIndex()
			if sessionIndex != -1 {
				go dataSession(conn, clientChannel[sessionIndex], sessionIndex)
			} else {
				utils.Error.Printf("No Websocket session available.")
			}
		} else {
			utils.Error.Printf("Client must set up a Websocket session.")
		}
	}
}

func dataSession(conn *websocket.Conn, clientChannel chan []byte, sessionIndex int) {
	defer conn.Close()
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			returnDataSessionIndex(sessionIndex)
			utils.Error.Printf("App client read error: %s", err)
			break
		}
		utils.Info.Printf("DataSession[%d]:message received: len=%d", sessionIndex, len(msg))
		clientChannel <- msg
		msg = <- clientChannel
		err = conn.WriteMessage(websocket.BinaryMessage, msg)
		if err != nil {
			utils.Error.Printf("dataSession[%d]:Request write error:%s", sessionIndex, err)
			break
		}
		utils.Info.Printf("message sent: len=%d", len(msg))
	}
}

func getDataSessionIndex() int {
	for i := 0; i< MAXSESSIONS; i++ {
		if !sessionList[i] {
			return i
		}
	}
	return -1
}

func returnDataSessionIndex(index int) {
	sessionList[index] = false
}
