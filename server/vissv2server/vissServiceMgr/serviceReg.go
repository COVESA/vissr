/**
* (C) 2026 Ford Motor Company
*
* VISSv3.3-alpha — Server-Service Registration Protocol
*
* Service processes connect to the server on ServiceRegPort (TCP, or TLS via
* StartServiceRegServerTLS). The protocol is line-delimited JSON.
*
* Server ← Service  register:   {"action":"register","path":"Root.Proc","signature":{...}}
* Server → Service  ack:        {"registered":true,"path":"Root.Proc"}
*                   or error:   {"registered":false,"reason":"..."}
*
* When a client invokes the procedure:
* Server → Service  invoke:     {"action":"invoke","sessionId":"xxx","input":{...},"authorization":"<token>"}
*
* Service → Server  progress:   {"sessionId":"xxx","status":"ONGOING","output":{...}}
* Service → Server  terminal:   {"sessionId":"xxx","status":"SUCCESSFUL","output":{...}}
* Service → Server  failure:    {"sessionId":"xxx","status":"FAILED","error":{"code":"...","message":"..."}}
*
* Server → Service  ping:       {"action":"ping"}   (heartbeat, VISSv3.3 §19)
* Server ← Service  pong:       {"action":"pong"}
*
* Server ← Service  deregister: {"action":"deregister"}
*
* Connections are long-lived; the server keeps one goroutine per connection.
* If the connection is lost or a heartbeat times out, all procedures registered
* by that service are deregistered and any ONGOING invocations are marked FAILED.
**/

package vissServiceMgr

import (
	"bufio"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/covesa/vissr/utils"
)

// ServiceRegPort is the TCP port the registration server listens on.
const ServiceRegPort = 8300

// HeartbeatInterval is how often the server sends a ping to a connected service.
// Tests may lower this value.
var HeartbeatInterval = 15 * time.Second

// HeartbeatTimeout is how long the server waits for a pong after sending a ping.
// Tests may lower this value.
var HeartbeatTimeout = 5 * time.Second

// serviceConn holds one registered service connection.
type serviceConn struct {
	path     string // registered procedure path
	conn     net.Conn
	writer   *bufio.Writer
	mu       sync.Mutex
	lastPong time.Time           // updated on each received pong
	sig      map[string]interface{} // stored signature for metadata
}

// sendJSON marshals v and writes it as a newline-delimited JSON frame.
// Protected by sc.mu so it is safe to call concurrently with forwardInvokeToService.
func (sc *serviceConn) sendJSON(v interface{}) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.writer.Write(b)
	sc.writer.WriteByte('\n')
	return sc.writer.Flush()
}

var (
	regMu         sync.Mutex
	registrations = map[string]*serviceConn{} // path → connection
)

// StartServiceRegServer begins listening for service registrations over plain TCP.
// backendChans is passed through so UpdateServiceState can fan out events.
func StartServiceRegServer(backendChans []chan map[string]interface{}) {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", ServiceRegPort))
	if err != nil {
		utils.Error.Printf("serviceReg: failed to listen on port %d: %v", ServiceRegPort, err)
		return
	}
	utils.Info.Printf("serviceReg: listening on :%d", ServiceRegPort)
	go serveListener(ln, backendChans)
}

// StartServiceRegServerTLS begins listening for service registrations over TLS
// (VISSv3.3 §22). certFile and keyFile are paths to the PEM-encoded server
// certificate and private key.
func StartServiceRegServerTLS(backendChans []chan map[string]interface{}, certFile, keyFile string) error {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return fmt.Errorf("serviceReg: load TLS certificate: %w", err)
	}
	cfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}
	ln, err := tls.Listen("tcp", fmt.Sprintf(":%d", ServiceRegPort), cfg)
	if err != nil {
		return fmt.Errorf("serviceReg: TLS listen on port %d: %w", ServiceRegPort, err)
	}
	utils.Info.Printf("serviceReg: TLS listening on :%d", ServiceRegPort)
	go serveListener(ln, backendChans)
	return nil
}

func serveListener(ln net.Listener, backendChans []chan map[string]interface{}) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			utils.Error.Printf("serviceReg: accept error: %v", err)
			return
		}
		go handleServiceConn(conn, backendChans)
	}
}

func handleServiceConn(conn net.Conn, backendChans []chan map[string]interface{}) {
	defer conn.Close()
	scanner := bufio.NewScanner(conn)
	writer := bufio.NewWriter(conn)
	var sc *serviceConn

	for scanner.Scan() {
		line := scanner.Text()
		var msg map[string]interface{}
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			utils.Error.Printf("serviceReg: malformed message: %v", err)
			continue
		}

		action, _ := msg["action"].(string)
		switch action {
		case "register":
			sc = handleRegister(msg, conn, writer, backendChans)
			if sc != nil {
				go startHeartbeat(sc)
			}
		case "deregister":
			if sc != nil {
				handleDeregister(sc, backendChans)
				return
			}
		case "pong":
			// Heartbeat response: update last-seen time.
			if sc != nil {
				sc.mu.Lock()
				sc.lastPong = time.Now()
				sc.mu.Unlock()
			}
		default:
			// Progress update keyed by sessionId.
			if sid, ok := msg["sessionId"].(string); ok && sid != "" {
				handleProgress(msg, backendChans)
			} else {
				utils.Error.Printf("serviceReg: unknown message action %q", action)
			}
		}
	}

	// Connection closed unexpectedly — clean up.
	if sc != nil {
		utils.Info.Printf("serviceReg: connection for %q lost, deregistering", sc.path)
		handleDeregister(sc, backendChans)
	}
}

// startHeartbeat sends periodic pings to sc and closes the connection if no
// pong is received within HeartbeatTimeout (VISSv3.3 §19).
func startHeartbeat(sc *serviceConn) {
	for {
		time.Sleep(HeartbeatInterval)

		sc.mu.Lock()
		prePong := sc.lastPong
		sc.mu.Unlock()

		if err := sc.sendJSON(map[string]interface{}{"action": "ping"}); err != nil {
			return // connection already gone
		}

		time.Sleep(HeartbeatTimeout)

		sc.mu.Lock()
		missed := sc.lastPong.Equal(prePong)
		sc.mu.Unlock()

		if missed {
			utils.Info.Printf("serviceReg: heartbeat timeout for %q, closing connection", sc.path)
			sc.conn.Close()
			return
		}
	}
}

func handleRegister(msg map[string]interface{}, conn net.Conn,
	writer *bufio.Writer, backendChans []chan map[string]interface{}) *serviceConn {

	path, _ := msg["path"].(string)
	if path == "" {
		replyJSON(writer, map[string]interface{}{ //nolint:errcheck
			"registered": false, "reason": "missing path",
		})
		return nil
	}

	// Reject duplicate registrations (VISSv3.3 §13).
	regMu.Lock()
	if _, exists := registrations[path]; exists {
		regMu.Unlock()
		replyJSON(writer, map[string]interface{}{ //nolint:errcheck
			"registered": false, "reason": "path already registered",
		})
		return nil
	}
	regMu.Unlock()

	sig, _ := msg["signature"].(map[string]interface{})
	root := buildTreeFromSignature(path, sig)
	rootName := strings.SplitN(path, ".", 2)[0]
	domain := rootName + ".Service"
	utils.RegisterServiceTree(rootName, domain, "1.0", root)

	sc := &serviceConn{
		path:     path,
		conn:     conn,
		writer:   writer,
		lastPong: time.Now(), // initialise so first heartbeat check passes
		sig:      sig,
	}
	regMu.Lock()
	registrations[path] = sc
	regMu.Unlock()

	utils.Info.Printf("serviceReg: registered service %q", path)
	replyJSON(writer, map[string]interface{}{"registered": true, "path": path}) //nolint:errcheck
	return sc
}

func handleDeregister(sc *serviceConn, backendChans []chan map[string]interface{}) {
	regMu.Lock()
	delete(registrations, sc.path)
	regMu.Unlock()

	// Mark any ONGOING invocations for this path as FAILED.
	mu.Lock()
	var failIds []string
	for id, inv := range invocations {
		if inv.path == sc.path && inv.status == StatusOngoing {
			failIds = append(failIds, id)
		}
	}
	mu.Unlock()

	for _, id := range failIds {
		UpdateServiceState(id, StatusFailed, nil, nil, backendChans)
	}

	rootName := strings.SplitN(sc.path, ".", 2)[0]
	utils.DeregisterServiceTree(rootName)
	utils.Info.Printf("serviceReg: deregistered %q", sc.path)
}

func handleProgress(msg map[string]interface{}, backendChans []chan map[string]interface{}) {
	sessionId, _ := msg["sessionId"].(string)
	statusStr, _ := msg["status"].(string)
	output, _ := msg["output"].(map[string]interface{})

	// Extract optional structured error payload (VISSv3.3 §20).
	var svcErr *ServiceError
	if errRaw, ok := msg["error"].(map[string]interface{}); ok {
		code, _ := errRaw["code"].(string)
		message, _ := errRaw["message"].(string)
		if code != "" || message != "" {
			svcErr = &ServiceError{Code: code, Message: message}
		}
	}

	status := ServiceStatus(statusStr)
	switch status {
	case StatusOngoing, StatusSuccessful, StatusFailed, StatusCanceled:
	default:
		utils.Error.Printf("serviceReg: invalid status %q from service", statusStr)
		return
	}
	UpdateServiceState(sessionId, status, output, svcErr, backendChans)
}

// forwardInvokeToService sends an invoke notification to the registered service
// process for path (if any). authToken is forwarded from the client request so
// services can perform their own access checks (VISSv3.3 §21).
func forwardInvokeToService(path, serviceId string, input map[string]interface{}, authToken string) {
	regMu.Lock()
	sc := registrations[path]
	regMu.Unlock()
	if sc == nil {
		return // no service registered; caller must handle via UpdateServiceState
	}

	msg := map[string]interface{}{
		"action":    "invoke",
		"sessionId": serviceId,
		"input":     input,
	}
	if authToken != "" {
		msg["authorization"] = authToken
	}
	sc.sendJSON(msg) //nolint:errcheck
}

// buildTreeFromSignature constructs a HIM procedure tree from the signature
// map supplied during registration.
//
// Expected signature format:
//
//	{
//	  "input":  {"Param1": "uint32", "Param2": "string"},
//	  "output": {"Result1": "uint32"}
//	}
//
// Produces: Root → ProcedureName (procedure) → Input (iostruct) → Param1, Param2
//
//	→ Output (iostruct) → Result1
func buildTreeFromSignature(path string, sig map[string]interface{}) *utils.Node_t {
	segments := strings.Split(path, ".")
	procName := segments[len(segments)-1]

	var inputChildren []*utils.Node_t
	var outputChildren []*utils.Node_t

	if sig != nil {
		if inputMap, ok := sig["input"].(map[string]interface{}); ok {
			for paramName, dt := range inputMap {
				dtStr, ok2 := dt.(string)
				if !ok2 || dtStr == "" {
					dtStr = "string"
				}
				inputChildren = append(inputChildren, utils.NewPropertyNode(paramName, dtStr, ""))
			}
		}
		if outputMap, ok := sig["output"].(map[string]interface{}); ok {
			for paramName, dt := range outputMap {
				dtStr, ok2 := dt.(string)
				if !ok2 || dtStr == "" {
					dtStr = "string"
				}
				outputChildren = append(outputChildren, utils.NewPropertyNode(paramName, dtStr, ""))
			}
		}
	}

	var ioNodes []*utils.Node_t
	if len(inputChildren) > 0 {
		ioNodes = append(ioNodes, utils.NewIoStructNode("Input", inputChildren...))
	}
	if len(outputChildren) > 0 {
		ioNodes = append(ioNodes, utils.NewIoStructNode("Output", outputChildren...))
	}

	procNode := utils.NewProcedureNode(procName, path, ioNodes...)

	root := procNode
	for i := len(segments) - 2; i >= 0; i-- {
		root = utils.NewBranchNode(segments[i], root)
	}
	return root
}

// replyJSON writes a single JSON line to w. Used before sc is constructed.
func replyJSON(w *bufio.Writer, v interface{}) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	w.Write(b)
	w.WriteByte('\n')
	return w.Flush()
}
