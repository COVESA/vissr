/**
* (C) 2019 Geotab Inc
* (C) 2019 Volvo Cars
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE file in this repository.
*
**/

package utils

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// secureConfigOnce protects SecureConfiguration from a multi-goroutine race.
// Pre-fix, ReadTransportSecConfig was called concurrently from httpMgr, wsMgr,
// udsMgr, grpcMgr, atServer (each starts in its own goroutine) and all of them
// json.Unmarshal-ed into the same package-global SecureConfiguration. -race
// flagged this. sync.Once guarantees the file is read once and SecureConfiguration
// is safe to read from any goroutine after the first call returns.
var secureConfigOnce sync.Once

// HTTP timeouts applied to all TLS / plaintext listeners spawned from this
// package. Pre-fix the http.Server{} had no read/write timeouts so a slowloris-
// style attacker could hold a connection open indefinitely, exhausting the
// listener fd budget. These values are conservative; tune via config if a real
// workload needs longer-lived streams.
const (
	httpReadHeaderTimeout = 10 * time.Second
	httpReadTimeout       = 30 * time.Second
	httpWriteTimeout      = 60 * time.Second
	httpIdleTimeout       = 120 * time.Second
	httpMaxHeaderBytes    = 1 << 20 // 1 MiB
)

const backendTermination = "internal-backend-termination"

// getWsClientIndex allocates the first free WS client slot from
// WsClientIndexList. Pre-fix this raced with ReturnWsClientIndex (both
// called from per-connection goroutines with no mutex) - two simultaneous
// Accepts could observe the same `true` slot and both reserve it, breaking
// the at-most-one-client-per-slot invariant. Now mutex-protected.
func getWsClientIndex() int {
	WsClientIndexMu.Lock()
	defer WsClientIndexMu.Unlock()
	for i := range WsClientIndexList {
		if WsClientIndexList[i] {
			WsClientIndexList[i] = false
			return i
		}
	}
	return -1
}

// ReturnWsClientIndex releases a WS client slot back to the free pool.
// Pre-fix this had no bounds check, so a clientId of -1 (e.g. from an
// earlier exhausted allocation that was never guarded against) would panic
// with slice-out-of-range. Now bounds-checked + mutex-protected.
func ReturnWsClientIndex(index int) {
	if index < 0 || index >= len(WsClientIndexList) {
		Error.Printf("ReturnWsClientIndex: invalid index=%d (len=%d)", index, len(WsClientIndexList))
		return
	}
	WsClientIndexMu.Lock()
	defer WsClientIndexMu.Unlock()
	WsClientIndexList[index] = true
}

// ReadTransportSecConfig parses transportSec.json and populates the package-
// global SecureConfiguration. Pre-fix:
//   - was called from N goroutines (each transport manager) and they all wrote
//     to SecureConfiguration concurrently with no synchronisation;
//   - logged a missing config at Info level so ops couldn't tell at a glance
//     that the server was running plaintext;
//   - performed no field validation, so transportSec.json={} loaded cleanly
//     into a zero-valued struct and silently fell through to plaintext;
//   - did not default the SNI/ServerName field that every call site of
//     GetTLSConfig hardcoded to "localhost".
//
// Now wrapped in sync.Once (race-free regardless of caller count),
// validateSecConfig runs after parsing, and ServerName defaults to "localhost"
// with a loud warning so the operator knows to override it.
func ReadTransportSecConfig() {
	secureConfigOnce.Do(readTransportSecConfigOnce)
}

func readTransportSecConfigOnce() {
	data, err := os.ReadFile(TrSecConfigPath + "transportSec.json")
	if err != nil {
		Warning.Printf("ReadTransportSecConfig: %stransportSec.json missing/unreadable (err=%v) - falling back to plaintext", TrSecConfigPath, err)
		SecureConfiguration.TransportSec = "no"
		return
	}
	if err := json.Unmarshal(data, &SecureConfiguration); err != nil {
		Error.Printf("ReadTransportSecConfig: malformed transportSec.json: %v - falling back to plaintext", err)
		SecureConfiguration.TransportSec = "no"
		return
	}
	validateSecConfig(&SecureConfiguration)
	Info.Printf("ReadTransportSecConfig: TransportSec=%s ServerName=%s ServerCertOpt=%s", SecureConfiguration.TransportSec, SecureConfiguration.ServerName, SecureConfiguration.ServerCertOpt)
}

// validateSecConfig fills in safe defaults and emits warnings on questionable
// configurations. It does not return errors - the server keeps running with
// whatever the operator supplied; we just make the holes obvious in the log.
func validateSecConfig(cfg *SecConfig) {
	if cfg == nil {
		return
	}
	if cfg.TransportSec != "yes" {
		// If TLS is off there's nothing to validate; coerce to canonical "no".
		if cfg.TransportSec != "no" && cfg.TransportSec != "" {
			Warning.Printf("validateSecConfig: TransportSec=%q is neither \"yes\" nor \"no\"; treating as \"no\"", cfg.TransportSec)
		}
		cfg.TransportSec = "no"
		return
	}
	// TransportSec == "yes" - check the fields TLS will actually need.
	if cfg.ServerName == "" {
		Warning.Printf("validateSecConfig: serverName not set in transportSec.json - defaulting to \"localhost\". TLS handshakes from any non-localhost client will fail SNI matching. Set serverName to the public DNS name of this server.")
		cfg.ServerName = "localhost"
	}
	if cfg.CaSecPath == "" {
		Warning.Printf("validateSecConfig: caSecPath empty - CA cert path will resolve to %s", TrSecConfigPath)
	}
	if cfg.ServerSecPath == "" {
		Warning.Printf("validateSecConfig: serverSecPath empty - server.crt/key path will resolve to %s", TrSecConfigPath)
	}
	if cfg.ServerCertOpt == "" {
		Warning.Printf("validateSecConfig: serverCertOpt empty - defaulting to \"ClientCertVerification\" (max security)")
		cfg.ServerCertOpt = "ClientCertVerification"
	}
}

// safeCertPath joins base + sub + filename with filepath.Clean and rejects any
// resulting path that escapes `base` via `..` segments. Pre-fix the call sites
// did raw string concatenation, so an operator-supplied CaSecPath of
// "../../etc/" would let a misconfigured server load CA certs from anywhere
// on disk.
func safeCertPath(base, sub, filename string) (string, error) {
	if base == "" {
		return "", errors.New("safeCertPath: base is empty")
	}
	if filename == "" {
		return "", errors.New("safeCertPath: filename is empty")
	}
	if strings.ContainsAny(filename, "/\\") {
		return "", errors.New("safeCertPath: filename must not contain path separators")
	}
	cleanedBase := filepath.Clean(base)
	full := filepath.Clean(filepath.Join(cleanedBase, sub, filename))
	// Ensure `full` is inside `cleanedBase` (or equal to it for sub=="").
	rel, err := filepath.Rel(cleanedBase, full)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("safeCertPath: path traversal rejected")
	}
	return full, nil
}

func createRouterIdProperty(mgrId int, clientId int) string {
	return "\"RouterId\":\"" + strconv.Itoa(mgrId) + "?" + strconv.Itoa(clientId) + "\""
}

func AddRoutingForwardRequest(reqMessage string, mgrId int, clientId int, transportMgrChan chan string) {
	//	newPrefix := "{ \"RouterId\":\"" + strconv.Itoa(mgrId) + "?" + strconv.Itoa(clientId) + "\", "
	newPrefix := "{" + createRouterIdProperty(mgrId, clientId) + ", " + "\"origin\":\"external\", "
	request := strings.Replace(reqMessage, "{", newPrefix, 1)
	Info.Printf("AddRoutingForwardRequest: %s", request)
	transportMgrChan <- request
}

func backendHttpAppSession(message string, w *http.ResponseWriter) {
	Info.Printf("backendHttpAppSession(): Message received=%s", message)

	var responseMap = make(map[string]interface{})
	MapRequest(message, &responseMap)
	if responseMap["action"] != nil {
		delete(responseMap, "action")
	}
	if responseMap["requestId"] != nil {
		delete(responseMap, "requestId")
	}
	response := FinalizeMessage(responseMap)

	resp := []byte(response)
	(*w).Header().Set("Access-Control-Allow-Origin", "*")
	(*w).Header().Set("Access-Control-Allow-Headers", "*")
	(*w).Header().Set("Content-Length", strconv.Itoa(len(resp)))
	written, err := (*w).Write(resp)
	if err != nil {
		Error.Printf("HTTP manager error on response write.Written bytes=%d. Error=%s", written, err.Error())
	}
}

func splitToPathQueryKeyValue(path string) (string, string, string) {
	delim1 := strings.Index(path, "?")
	if delim1 != -1 {
		delim2 := strings.Index(path, "=")
		if delim2 != -1 {
			if path[delim1+1:delim2] == "filter" {
				return path[:delim1], "filter", path[delim1+8:] // path?filter=json-exp
			} else if path[delim1+1:delim2] == "metadata" {
				return path[:delim1], "metadata", path[delim1+10:] // path?metadata=static (or dynamic)
			} else {
				return path[:delim1], "filter", "incorrect http query key"
			}
		}
	}
	return path, "", ""
}

// Manages first communication with Client
func frontendHttpAppSession(w http.ResponseWriter, req *http.Request, clientChannel chan string) {
	path := req.RequestURI
	if len(path) == 0 {
		path = "empty-path" // will generate error as not found in VSS tree
	}
	path = strings.ReplaceAll(path, "%22", "\"")
	path = strings.ReplaceAll(path, "%20", "")
	var requestMap = make(map[string]interface{})
	queryKey := ""
	queryValue := ""
	if strings.Contains(path, "?") {
		requestMap["path"], queryKey, queryValue = splitToPathQueryKeyValue(path)
	} else {
		requestMap["path"] = path
	}
	Info.Printf("HTTP method:%s, path: %s", req.Method, path)
	token := req.Header.Get("Authorization")
	Info.Printf("HTTP token:%s", token)
	if len(token) > 0 {
		requestMap["authorization"] = strings.TrimPrefix(token, "Bearer ")
	}
	requestMap["requestId"] = strconv.Itoa(requestTag)
	requestTag++
	switch req.Method {
	case "OPTIONS":
		fallthrough // should work for POST also...
	case "GET":
		requestMap["action"] = "get"
	case "POST": // set
		requestMap["action"] = "set"
		// Bound the request body before io.ReadAll. Without this an
		// anonymous client can send a Content-Length: <huge> or a
		// never-closing chunked body and ReadAll will allocate until
		// the daemon OOMs. VISS set payloads are small JSON envelopes,
		// so 64 KiB is well above any legitimate use.
		req.Body = http.MaxBytesReader(w, req.Body, 64*1024)
		body, err := io.ReadAll(req.Body)
		if err != nil {
			Warning.Printf("frontendHttpAppSession: request body rejected: %s", err)
			backendHttpAppSession(`{"error": "413", "reason": "Request body too large", "description":"max 64 KiB"}`, &w)
			return
		}
		var bodyMap map[string]interface{}
		MapRequest(string(body), &bodyMap)
		requestMap["value"] = bodyMap["value"]
	default:
		//		http.Error(w, "400 Unsupported method", http.StatusBadRequest)
		Warning.Printf("Only GET and POST methods are supported.")
		backendHttpAppSession(`{"error": "400", "reason": "Bad request", "description":"Unsupported HTTP method"}`, &w)
		return
	}
	clientChannel <- AddKeyValue(FinalizeMessage(requestMap), queryKey, queryValue) // forward to mgr hub,
	response := <-clientChannel                                                     //  and wait for response
	backendHttpAppSession(response, &w)
}

// Receives the message from client, sends it to the manager hub, and waits for the response
// decodeWsRequestPayload converts a raw inbound WS message into the
// JSON string the manager hub expects. Extracted from
// frontendWSAppSession so the encoding-dispatch (Protobuf vs JSON
// passthrough) can be unit-tested without a live WebSocket. See
// managerhandlers_dispatch_test.go.
func decodeWsRequestPayload(msg []byte, encoding Encoding) string {
	if encoding == PROTOBUF {
		return ProtobufToJson(msg)
	}
	return string(msg)
}

// forwardWsRequest takes a decoded payload from a WS client, forwards
// it to the manager hub via clientChannel, waits for the hub's
// response, and pushes the response on to the backend channel for
// backendWSAppSession to write back to the WS. Extracted from
// frontendWSAppSession so the per-message request/response handshake
// can be unit-tested independently of the goroutine machinery — see
// managerhandlers_dispatch_test.go.
func forwardWsRequest(payload string, clientChannel chan string, clientBackendChannel chan string) {
	clientChannel <- payload
	response := <-clientChannel
	clientBackendChannel <- response
}

func frontendWSAppSession(conn *websocket.Conn, clientChannel chan string, clientBackendChannel chan string, clientId int, encoding Encoding) {
	for {
		_, msg, err := conn.ReadMessage() // Reads message from websocket
		if err != nil {                   // Error reading message, kills socket
			Error.Printf("App client read error: %s", err)
			clientChannel <- `{"action":"internal-killsubscriptions"}`
			clientBackendChannel <- backendTermination
			ReturnWsClientIndex(clientId)
			return
		}
		payload := decodeWsRequestPayload(msg, encoding)
		Info.Printf("%s request: %s, len=%d", conn.RemoteAddr(), payload, len(payload))
		forwardWsRequest(payload, clientChannel, clientBackendChannel)
	}
}

// encodeWsResponsePayload converts a JSON response message into the
// raw bytes + WebSocket message type expected by gorilla/websocket's
// WriteMessage. Extracted from backendWSAppSession so the encoding
// dispatch (Protobuf binary vs JSON text) can be unit-tested without
// a live WebSocket.
func encodeWsResponsePayload(message string, encoding Encoding) (messageType int, response []byte) {
	if encoding == PROTOBUF {
		return websocket.BinaryMessage, []byte(JsonToProtobuf(message))
	}
	return websocket.TextMessage, []byte(message)
}

// Receives a response for the client through the channel. Then writes the response back to the client.
func backendWSAppSession(conn *websocket.Conn, clientBackendChannel chan string, encoding Encoding) {
	defer conn.Close()
	for {
		message := <-clientBackendChannel
		Info.Printf("backendWSAppSession(): Message received=%s", message)
		if message == backendTermination {
			Error.Print("App client websocket session error.")
			break
		}
		// Write response/notification back to app client
		messageType, response := encodeWsResponsePayload(message, encoding)
		err := conn.WriteMessage(messageType, response)
		if err != nil {
			Error.Print("App client write error:", err)
//			break  // likely to be followed by a read error which trigger the termination above.
		}
	}
}

func (httpH HttpChannel) makeappClientHandler(appClientChannel []chan string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("Upgrade") == "websocket" {
			http.Error(w, "400 Incorrect port number", http.StatusBadRequest)
			Warning.Printf("Client call to incorrect port number for websocket connection.")
			return
		}
		frontendHttpAppSession(w, req, appClientChannel[0])
	}
}

// Generates WS Handler
func (wsH WsChannel) makeappClientHandler(appClientChannel []chan string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, req *http.Request) {
		*wsH.clientIndex = getWsClientIndex()
		if *wsH.clientIndex == -1 {
			Warning.Printf("WS session not started, too many clients")
			return
		}
		if req.Header.Get("Upgrade") == "websocket" {
			Info.Printf("Received websocket request: we are upgrading to a websocket connection.")
			Upgrader.CheckOrigin = func(r *http.Request) bool { return true }
			var encoding Encoding
			encoding = NONE
			h := http.Header{}
			for _, sub := range websocket.Subprotocols(req) {
				if sub == "VISS-noenc" {
					encoding = NONE
					h.Set("Sec-Websocket-Protocol", sub)
					break
				}
				if sub == "VISS-protoenc" {
					encoding = PROTOBUF
					h.Set("Sec-Websocket-Protocol", sub)
					break
				}
			}
			//			*wsH.clientIndex = getWsClientIndex()
			Info.Printf("ClientIndex=%d", *wsH.clientIndex)
			if *wsH.clientIndex >= 0 {
				conn, err := Upgrader.Upgrade(w, req, h)
				if err != nil {
					Error.Print("upgrade error:", err)
					return
				}
				Info.Printf("WS session started, encoding variant=%d", encoding)
				go frontendWSAppSession(conn, appClientChannel[*wsH.clientIndex], wsH.clientBackendChannel[*wsH.clientIndex],
					*wsH.clientIndex, encoding)
				go backendWSAppSession(conn, wsH.clientBackendChannel[*wsH.clientIndex], encoding)
			}
		} else {
			Error.Printf("Client must set up a Websocket session.")
		}
	}
}

// Launches the HTTP Manager. Pre-fix this http.Server had no Read/Write/Idle
// timeouts (slowloris DoS), hardcoded "localhost" SNI, and did raw string
// concatenation for cert paths (path traversal). All three fixed.
func (server HttpServer) InitClientServer(muxServer *http.ServeMux, httpClientChan []chan string) {
	appClientHandler := HttpChannel{}.makeappClientHandler(httpClientChan)
	muxServer.HandleFunc("/", appClientHandler)
	Info.Printf("InitClientServer():SecureConfiguration.TransportSec=%s", SecureConfiguration.TransportSec)
	if SecureConfiguration.TransportSec == "yes" {
		caCertFile, err := safeCertPath(TrSecConfigPath, SecureConfiguration.CaSecPath, "Root.CA.crt")
		if err != nil {
			Error.Fatalf("HttpServer.InitClientServer: invalid CA cert path: %v", err)
		}
		serverCrt, err := safeCertPath(TrSecConfigPath, SecureConfiguration.ServerSecPath, "server.crt")
		if err != nil {
			Error.Fatalf("HttpServer.InitClientServer: invalid server.crt path: %v", err)
		}
		serverKey, err := safeCertPath(TrSecConfigPath, SecureConfiguration.ServerSecPath, "server.key")
		if err != nil {
			Error.Fatalf("HttpServer.InitClientServer: invalid server.key path: %v", err)
		}
		s := http.Server{
			Addr: ":" + SecureConfiguration.HttpSecPort,
			TLSConfig: GetTLSConfig(SecureConfiguration.ServerName, caCertFile,
				tls.ClientAuthType(CertOptToInt(SecureConfiguration.ServerCertOpt)), nil),
			Handler:           muxServer,
			ReadHeaderTimeout: httpReadHeaderTimeout,
			ReadTimeout:       httpReadTimeout,
			WriteTimeout:      httpWriteTimeout,
			IdleTimeout:       httpIdleTimeout,
			MaxHeaderBytes:    httpMaxHeaderBytes,
		}
		Info.Printf("HTTPS: ServerName=%s CertOpt=%s Addr=%s", SecureConfiguration.ServerName, SecureConfiguration.ServerCertOpt, s.Addr)
		Error.Fatal(s.ListenAndServeTLS(serverCrt, serverKey))
	} else {
		s := &http.Server{
			Addr:              ":8888",
			Handler:           muxServer,
			ReadHeaderTimeout: httpReadHeaderTimeout,
			ReadTimeout:       httpReadTimeout,
			WriteTimeout:      httpWriteTimeout,
			IdleTimeout:       httpIdleTimeout,
			MaxHeaderBytes:    httpMaxHeaderBytes,
		}
		Error.Fatal(s.ListenAndServe())
	}
}

// Launches the WebSocket Manager. Same pre-fix bugs as HttpServer.InitClientServer;
// same fixes.
func (server WsServer) InitClientServer(muxServer *http.ServeMux, wsClientChan []chan string, mgrIndex int, clientIndex *int) {
	*clientIndex = 0
	appClientHandler := WsChannel{server.ClientBackendChannel, mgrIndex, clientIndex}.makeappClientHandler(wsClientChan) // Generates a handler for the requests
	// For the web client
	muxServer.HandleFunc("/webclient/", http.StripPrefix("/webclient/", http.FileServer(http.Dir("../../viss-web-client"))).ServeHTTP)
	muxServer.HandleFunc("/", appClientHandler)
	Info.Printf("InitClientServer():SecureConfiguration.TransportSec=%s", SecureConfiguration.TransportSec)
	if SecureConfiguration.TransportSec == "yes" { // In  case a secure connection is used
		caCertFile, err := safeCertPath(TrSecConfigPath, SecureConfiguration.CaSecPath, "Root.CA.crt")
		if err != nil {
			Error.Fatalf("WsServer.InitClientServer: invalid CA cert path: %v", err)
		}
		serverCrt, err := safeCertPath(TrSecConfigPath, SecureConfiguration.ServerSecPath, "server.crt")
		if err != nil {
			Error.Fatalf("WsServer.InitClientServer: invalid server.crt path: %v", err)
		}
		serverKey, err := safeCertPath(TrSecConfigPath, SecureConfiguration.ServerSecPath, "server.key")
		if err != nil {
			Error.Fatalf("WsServer.InitClientServer: invalid server.key path: %v", err)
		}
		s := http.Server{
			Addr: ":" + SecureConfiguration.WsSecPort,
			TLSConfig: GetTLSConfig(SecureConfiguration.ServerName, caCertFile,
				tls.ClientAuthType(CertOptToInt(SecureConfiguration.ServerCertOpt)), nil),
			Handler:           muxServer,
			ReadHeaderTimeout: httpReadHeaderTimeout,
			ReadTimeout:       httpReadTimeout,
			WriteTimeout:      httpWriteTimeout,
			IdleTimeout:       httpIdleTimeout,
			MaxHeaderBytes:    httpMaxHeaderBytes,
		}
		Info.Printf("HTTPS(WS): ServerName=%s CertOpt=%s Addr=%s", SecureConfiguration.ServerName, SecureConfiguration.ServerCertOpt, s.Addr)
		Error.Fatal(s.ListenAndServeTLS(serverCrt, serverKey))
	} else { // No sec connection
		s := &http.Server{
			Addr:              ":8080",
			Handler:           muxServer,
			ReadHeaderTimeout: httpReadHeaderTimeout,
			ReadTimeout:       httpReadTimeout,
			WriteTimeout:      httpWriteTimeout,
			IdleTimeout:       httpIdleTimeout,
			MaxHeaderBytes:    httpMaxHeaderBytes,
		}
		Error.Fatal(s.ListenAndServe())
	}
}

// CertOptToInt maps the JSON serverCertOpt string to a tls.ClientAuthType int.
// Pre-fix the match was case-sensitive and unknown values silently fell through
// to max security (4 = RequireAndVerifyClientCert). The fail-safe default is
// kept, but unknown values now emit a warning so the operator sees the
// mismatch.
func CertOptToInt(serverCertOpt string) int {
	switch strings.TrimSpace(strings.ToLower(serverCertOpt)) {
	case "noclientcert":
		return int(tls.NoClientCert) // 0
	case "clientcertnoverification":
		return int(tls.RequireAnyClientCert) // 2
	case "clientcertverification":
		return int(tls.RequireAndVerifyClientCert) // 4
	case "":
		return int(tls.RequireAndVerifyClientCert)
	default:
		Warning.Printf("CertOptToInt: unknown serverCertOpt=%q; defaulting to max security (ClientCertVerification)", serverCertOpt)
		return int(tls.RequireAndVerifyClientCert)
	}
}

// GetTLSConfig builds a *tls.Config suitable for http.Server.TLSConfig.
//
// Pre-fix bugs (now patched):
//   - AppendCertsFromPEM's bool return was discarded, so a non-empty but
//     unparseable Root.CA.crt produced an empty cert pool and ClientCertVerification
//     proceeded with ZERO trusted CAs - undefined behaviour that could trust
//     anyone or trust no one depending on Go version.
//   - On any internal failure the function returned nil. The caller fed that
//     nil into http.Server{TLSConfig: nil}, which made Go's ListenAndServeTLS
//     synthesise a default TLS config - the carefully constructed MinVersion /
//     ClientAuth / ClientCAs were silently lost. Every internal failure now
//     Error.Fatal's so the process exits with a clear "fail-stop on
//     misconfiguration" rather than running insecurely.
//   - Drive-by: line 435 Printf had no format-verbs and concatenated args
//     positionally; reduced to a single %v that actually prints the path.
//
// The host parameter is used for the SNI ServerName. Callers should pass
// SecureConfiguration.ServerName (which defaults to "localhost" with a warning).
func GetTLSConfig(host string, caCertFile string, certOpt tls.ClientAuthType, serverCert *tls.Certificate) *tls.Config {
	var caCertPool *x509.CertPool
	if certOpt > tls.RequestClientCert { // If a client certificate is required, then the CA certificate is needed
		caCert, err := os.ReadFile(caCertFile)
		if err != nil {
			Error.Fatalf("GetTLSConfig: cannot open CA cert file %q: %v - refusing to start with empty cert pool", caCertFile, err)
			return nil // unreachable; appease the compiler
		}
		caCertPool = x509.NewCertPool()
		if ok := caCertPool.AppendCertsFromPEM(caCert); !ok {
			Error.Fatalf("GetTLSConfig: CA cert file %q contained no valid PEM blocks - refusing to start with empty cert pool", caCertFile)
			return nil // unreachable
		}
	}

	if host == "" {
		Warning.Printf("GetTLSConfig: empty host - SNI/ServerName will be unset; clients verifying the cert SAN will fail")
	}

	if serverCert == nil {
		return &tls.Config{
			ServerName: host,
			ClientAuth: certOpt,
			ClientCAs:  caCertPool,
			MinVersion: tls.VersionTLS12, // TLS versions below 1.2 are considered insecure - see https://www.rfc-editor.org/rfc/rfc7525.txt for details
		}
	}
	return &tls.Config{
		ServerName:   host,
		Certificates: []tls.Certificate{*serverCert},
		ClientAuth:   certOpt,
		ClientCAs:    caCertPool,
		MinVersion:   tls.VersionTLS12, // TLS versions below 1.2 are considered insecure - see https://www.rfc-editor.org/rfc/rfc7525.txt for details
	}
}

func RemoveInternalData(response string) (string, int) { // "RouterId":"mgrId?clientId",
	routerIdStart := strings.Index(response, "RouterId") - 1
	clientIdStart := strings.Index(response[routerIdStart:], "?") + 1 + routerIdStart
	clientIdStop := NextQuoteMark([]byte(response), clientIdStart)
	clientId, _ := strconv.Atoi(response[clientIdStart:clientIdStop])
	routerIdStop := strings.Index(response[clientIdStop:], ",") + 1 + clientIdStop
	trimmedResponse := response[:routerIdStart] + response[routerIdStop:]
	//Info.Printf("response=%s, trimmedResponse=%s, clientId=%d", response, trimmedResponse, clientId)
	return trimmedResponse, clientId
}
