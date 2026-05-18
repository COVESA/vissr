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
	"errors"
	"fmt"
	"os"
	"io"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"github.com/gorilla/websocket"
)

// sessionListMu protects the sessionList read-modify-write in
// getDataSessionIndex / returnDataSessionIndex from concurrent WS
// upgrade goroutines.
//var sessionListMu sync.Mutex

// validateTransferName rejects file-transfer names that contain path
// separators or parent-directory references. Without this, a VISS
// client that issues a `set` against a FileDescriptor actuator
// controls the `name` field that initFtSession concatenates onto the
// hardcoded "./" base path and passes to os.Create / os.Open — i.e.
// arbitrary file write/read on the in-vehicle host with the daemon's
// privileges (e.g. name = "../../etc/cron.d/owned").
func validateTransferName(name string) error {
	if name == "" {
		return fmt.Errorf("empty filename")
	}
	if filepath.Base(name) != name {
		return fmt.Errorf("filename %q contains path separators", name)
	}
	if strings.Contains(name, "..") {
		return fmt.Errorf("filename %q contains parent-directory reference", name)
	}
	return nil
}

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
var sessionListMu sync.Mutex // guards sessionList — getDataSessionIndex
                              // runs on the HTTP handler goroutine,
                              // returnDataSessionIndex on the per-session
                              // reader goroutine

var clientIndex int

var fileTransferCache []utils.FileTransferCache
const FILETRANSFERCACHESIZE = 10

// Maximum file size we will accept for upload. The chunk-size
// calculation in initFtSession truncates a 64-bit Stat result into
// the `int` ChunkSize field; capping the file size here keeps the
// math safe on 32-bit and guards against pathological inputs on
// 64-bit. (utils.FileTransferCache.ChunkSize is `int`.)
const MAX_FILE_SIZE = (1 << 31) - 1 // 2 GiB - 1

type ChunkDataCache struct {
	MessageNo byte
	LastMessage byte
	ChunkSize []byte
	Chunk []byte
}

// One chunkDataCache per session so that session B's writeChunkData
// cannot clobber session A's resend buffer. Previously a single
// package-global ChunkDataCache was shared across all MAXSESSIONS,
// which corrupted resend behaviour under concurrent transfers.
var chunkDataCache [MAXSESSIONS]ChunkDataCache

// sanitizeFileName validates a client-supplied file name before it
// is concatenated with the configured Path. Originally
// `os.Create(Path + Name)` was passed Name verbatim, so a client
// sending Name="../../etc/passwd" could overwrite arbitrary files
// relative to the server's working directory.
//
// Rule: a valid name has no directory separators and is not "." or
// "..". This blocks all path-traversal attempts (which require a
// separator) while still permitting legitimate names that happen to
// contain ".." mid-token like "foo..bar" or "config.v..1".
func sanitizeFileName(name string) (string, error) {
	if name == "" || name == "." || name == ".." {
		return "", errors.New("invalid file name: " + name)
	}
	if strings.ContainsAny(name, `/\`) {
		return "", errors.New("file name contains path separator: " + name)
	}
	return name, nil
}

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
		dataResponse = getDataResponse(dataMessage, clientId)
		if len(dataResponse) > 0 {
			clientChan[clientId] <- dataResponse
		}
	}
}

func getDataResponse(req []byte, sessionIndex int) []byte {
	if len(req) > 6 {
		return getDataResponseDl(req)
	}
	return getDataResponseUl(req, sessionIndex)
}

// DL_HEADER_SIZE is uid(4) + messageNo(1) + chunkSize(4) + lastMessage(1).
// A download request shorter than this would have caused slice OOB
// panics at the uid/messageNo/chunkSize/lastMessage decode sites.
const DL_HEADER_SIZE = 10

func getDataResponseDl(req []byte) []byte {  // request: uid(4)|messageNo(1)|chunkSize(4)| lastMessage(1)|chunk(N)
	if len(req) < DL_HEADER_SIZE {
		utils.Error.Printf("getDataResponseDl: malformed packet, len=%d < %d", len(req), DL_HEADER_SIZE)
		uid := make([]byte, utils.UIDLEN) // zero uid; caller's uid field is unknown
		if len(req) >= utils.UIDLEN {
			uid = req[:utils.UIDLEN]
		}
		return createDlResponse(uid, byte(0x00), byte(0xFF)) // terminate session
	}
	uid := []byte(req[:4])
	var messageNo uint8
	buf := bytes.NewReader(req[4:5])
	err := binary.Read(buf, binary.BigEndian, &messageNo)
	if err != nil {
		utils.Error.Println("binary.Read failed for messageNo:", err)
		return createDlResponse(uid, byte(0x00), byte(0xFF))
	}
	var chunkSize uint32
	buf = bytes.NewReader(req[5:9])
	err = binary.Read(buf, binary.BigEndian, &chunkSize)
	if err != nil {
		utils.Error.Println("binary.Read failed for chunkSize:", err)
		return createDlResponse(uid, byte(0x00), byte(0xFF))
	}
	var lastMessage uint8
	buf = bytes.NewReader(req[9:10])
	err = binary.Read(buf, binary.BigEndian, &lastMessage)
	if err != nil {
		utils.Error.Println("binary.Read failed for lastMessage:", err)
		return createDlResponse(uid, byte(0x00), byte(0xFF))
	}
	chunk := req[10:]
	cacheIndex := findFileTransferCacheIndex(uid)
	if cacheIndex == -1 {
		return createDlResponse(uid, byte(0x00), byte(0xFF)) // terminate session error response
	}
	if uint32(len(chunk)) != chunkSize {
		return createDlResponse(uid, req[4], byte(0x01))
	}
	n, err := fileTransferCache[cacheIndex].FileDescriptor.Write(chunk)
	if err != nil {
		return createDlResponse(uid, req[4], byte(0x01))
	}
	fileTransferCache[cacheIndex].FileOffset += n
	if lastMessage == 0 {
		return createDlResponse(uid, req[4], byte(0x00))
	}
	// End-of-transfer: verify hash against the file we just wrote.
	// The original code closed the fd and reopened the file by path,
	// which was both a TOCTOU vector (path-traversal-controlled name
	// could swap files between close and reopen) and pointless I/O.
	// We now hash from the already-open fd before closing it.
	fd := fileTransferCache[cacheIndex].FileDescriptor
	if _, seekErr := fd.Seek(0, io.SeekStart); seekErr != nil {
		fd.Close()
		fileTransferCache[cacheIndex].Uid = clearUid()
		return createDlResponse(uid, byte(0x00), byte(0x01))
	}
	h := sha1.New()
	if _, copyErr := io.Copy(h, fd); copyErr != nil {
		fd.Close()
		fileTransferCache[cacheIndex].Uid = clearUid()
		return createDlResponse(uid, byte(0x00), byte(0x01))
	}
	fd.Close()
	gotHash := hex.EncodeToString(h.Sum(nil))
	fileTransferCache[cacheIndex].Uid = clearUid() // delete cache entry regardless of result
	if gotHash != fileTransferCache[cacheIndex].Hash {
		return createDlResponse(uid, byte(0x00), byte(0x01))
	}
	return createDlResponse(uid, req[4], byte(0x00))
}

// UL_HEADER_SIZE is uid(4) + messageNo(1) + status(1).
// An upload-status request shorter than this would have caused slice
// OOB panics decoding the uid / messageNo / status fields.
const UL_HEADER_SIZE = 6

func getDataResponseUl(req []byte, sessionIndex int) []byte {  // request: uid(4)|messageNo(1)|status(1)
	if len(req) < UL_HEADER_SIZE {
		utils.Error.Printf("getDataResponseUl: malformed packet, len=%d < %d", len(req), UL_HEADER_SIZE)
		// No safely-derivable uid; respond with zeros to signal error.
		uid := make([]byte, utils.UIDLEN)
		if len(req) >= utils.UIDLEN {
			uid = req[:utils.UIDLEN]
		}
		return createUlResponse(uid, 0, byte(0x00), []byte{0, 0, 0, 0}, nil)
	}
	uid := req[:4]
	messageNo := req[4]
	status := req[5]
	lastMessage := byte(0x00)
	chunkSize := make([]byte,4)
	var chunk []byte
	cacheIndex := findFileTransferCacheIndex([]byte(uid))
	if cacheIndex == -1 {
		return createUlResponse(uid, messageNo, lastMessage, []byte{0,0,0,0}, nil)  //error
	}
	if status == byte(0x00) {
		chunk = make([]byte, fileTransferCache[cacheIndex].ChunkSize)
		n, err := fileTransferCache[cacheIndex].FileDescriptor.Read(chunk)
		if err != nil {
			if err == io.EOF {
				lastMessage = byte(0x01)
				fileTransferCache[cacheIndex].FileDescriptor.Close()
				fileTransferCache[cacheIndex].Uid = clearUid() // delete cache entry
			} else {
				// Fix: previously this returned without closing the fd
				// or clearing the cache entry — a per-error file
				// descriptor leak that also left the slot pinned.
				utils.Error.Printf("getDataResponseUl: read error on uid=%x, err=%s", uid, err)
				fileTransferCache[cacheIndex].FileDescriptor.Close()
				fileTransferCache[cacheIndex].Uid = clearUid()
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
		lastMessage, chunkSize, chunk = readChunkData(messageNo, sessionIndex)
		if len(chunk) > 0 {
			return createUlResponse(uid, messageNo, lastMessage, chunkSize, chunk) // resend previous message
		}
		return createUlResponse(uid, messageNo, lastMessage, []byte{0,0,0,0}, nil)  //error
	}
	writeChunkData(messageNo, lastMessage, chunkSize, chunk, sessionIndex)
	return createUlResponse(uid, messageNo, lastMessage, chunkSize, chunk)
}

// readChunkData / writeChunkData now key by sessionIndex so each
// session has its own resend buffer. Previously a single
// package-global chunkDataCache was shared across all MAXSESSIONS,
// causing cross-session data corruption when two clients were
// resending different chunks concurrently.
func readChunkData(messageNo byte, sessionIndex int) (byte, []byte, []byte) {
	if sessionIndex < 0 || sessionIndex >= MAXSESSIONS {
		return byte(0), nil, nil
	}
	cache := &chunkDataCache[sessionIndex]
	if messageNo != cache.MessageNo {
		return byte(0), nil, nil
	}
	return cache.LastMessage, cache.ChunkSize, cache.Chunk
}

func writeChunkData(messageNo byte, lastMessage byte, chunkSize []byte, chunk []byte, sessionIndex int) {
	if sessionIndex < 0 || sessionIndex >= MAXSESSIONS {
		return
	}
	cache := &chunkDataCache[sessionIndex]
	cache.MessageNo = messageNo
	cache.LastMessage = lastMessage
	cache.ChunkSize = chunkSize
	cache.Chunk = chunk
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
	// Reject client-controlled names that would escape the transfer
	// directory before any os.Create / os.Open call. See
	// validateTransferName for the underlying threat model.
	if err := validateTransferName(clientRequest.Name); err != nil {
		utils.Error.Printf("initFtSession: rejecting unsafe filename: %s", err)
		return status
	}
	cacheIndex := getFileTransferCacheIndex(clientRequest.Uid)
	if cacheIndex == -1 {
		return status
	}
	// Sanitize the client-supplied file name BEFORE concatenating it
	// with Path. Previously `os.Create(Path + Name)` was vulnerable to
	// path traversal: a Name like "../../etc/passwd" would write
	// outside the configured directory.
	safeName, nameErr := sanitizeFileName(clientRequest.Name)
	if nameErr != nil {
		utils.Error.Printf("initFtSession: rejecting file name: %s", nameErr)
		return status
	}
	clientRequest.Name = safeName
	fullPath := filepath.Join(clientRequest.Path, safeName)

	var fd *os.File
	var err error
	if !clientRequest.UploadTransfer { // download
		fd, err = os.Create(fullPath) // overwrites existing file in the sanitized dir
	} else { // upload
		fd, err = os.Open(fullPath)
		if err != nil {
			utils.Error.Printf("Server failed to open file for upload, error =%s", err)
		} else {
			fileSize := getFileSize(fd)
			if fileSize < 0 || fileSize > MAX_FILE_SIZE {
				utils.Error.Printf("initFtSession: file too large for transfer: %d bytes (max %d)", fileSize, MAX_FILE_SIZE)
				fd.Close()
				return status
			}
			// fileSize fits in int after the cap check above. Cap the
			// derived chunk size so make([]byte, ChunkSize) in the
			// upload path can't be asked for a multi-GB allocation.
			chunkSize := int(fileSize)/10 + 1
			const MAX_CHUNK_SIZE = 1 << 20 // 1 MiB
			if chunkSize > MAX_CHUNK_SIZE {
				chunkSize = MAX_CHUNK_SIZE
			}
			clientRequest.ChunkSize = chunkSize
		}
	}
	if err == nil {
		populateFTCache(cacheIndex, clientRequest, fd)
		status = 0
	}
	return status
}

func getFileSize(fp *os.File) int64 {
	fi, err := fp.Stat()
	if err != nil {
		utils.Error.Printf("Server failed to get file size, error =%s", err)
		return 0
	}
	return fi.Size()
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

// getDataSessionIndex returns the first free session slot and MARKS
// IT TAKEN. The original implementation returned the first false
// index without setting it true, so every new WebSocket session got
// index 0 and all sessions collided on clientChan[0] — the
// multi-client path was completely broken.
//
// Mutex protects against the case where two upgrades land at roughly
// the same time on the HTTP handler goroutines (one per request).
func getDataSessionIndex() int {
	sessionListMu.Lock()
	defer sessionListMu.Unlock()
	for i := 0; i < MAXSESSIONS; i++ {
		if !sessionList[i] {
			sessionList[i] = true
			return i
		}
	}
	return -1
}

func returnDataSessionIndex(index int) {
	if index < 0 || index >= MAXSESSIONS {
		return
	}
	sessionListMu.Lock()
	sessionList[index] = false
	sessionListMu.Unlock()
}
