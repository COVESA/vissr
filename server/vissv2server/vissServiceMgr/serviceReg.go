/**
* (C) 2026 Ford Motor Company
*
* VISSv3.3-alpha — Server-Service Registration Protocol
*
* Service processes connect to the server on ServiceRegPort (TCP).
* The protocol is line-delimited JSON.
*
* Server ← Service  register:   {"action":"register","path":"Root.Proc","signature":{...}}
* Server → Service  ack:        {"registered":true,"path":"Root.Proc"}
*                   or error:   {"registered":false,"reason":"..."}
*
* When a client invokes the procedure:
* Server → Service  invoke:     {"action":"invoke","sessionId":"xxx","input":{...}}
*
* Service → Server  progress:   {"sessionId":"xxx","status":"ONGOING","output":{...}}
* Service → Server  terminal:   {"sessionId":"xxx","status":"SUCCESSFUL","output":{...}}
*
* Server ← Service  deregister: {"action":"deregister"}
*
* Connections are long-lived; the server keeps one goroutine per connection.
* If the connection is lost, all procedures registered by that service are
* deregistered and any ONGOING invocations are marked FAILED.
**/

package vissServiceMgr

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync"

	"github.com/covesa/vissr/utils"
)

// ServiceRegPort is the TCP port the registration server listens on.
const ServiceRegPort = 8300

// serviceConn holds one registered service connection.
type serviceConn struct {
	path   string // registered procedure root-name
	conn   net.Conn
	writer *bufio.Writer
	mu     sync.Mutex
}

var (
	regMu        sync.Mutex
	registrations = map[string]*serviceConn{} // path → connection
)

// StartServiceRegServer begins listening for service registrations.
// backendChans is passed through so UpdateServiceState can fan out events.
func StartServiceRegServer(backendChans []chan map[string]interface{}) {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", ServiceRegPort))
	if err != nil {
		utils.Error.Printf("serviceReg: failed to listen on port %d: %v", ServiceRegPort, err)
		return
	}
	utils.Info.Printf("serviceReg: listening on :%d", ServiceRegPort)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				utils.Error.Printf("serviceReg: accept error: %v", err)
				return
			}
			go handleServiceConn(conn, backendChans)
		}
	}()
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
		case "deregister":
			if sc != nil {
				handleDeregister(sc, backendChans)
				return
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

func handleRegister(msg map[string]interface{}, conn net.Conn,
	writer *bufio.Writer, backendChans []chan map[string]interface{}) *serviceConn {

	path, _ := msg["path"].(string)
	if path == "" {
		replyJSON(writer, map[string]interface{}{
			"registered": false, "reason": "missing path",
		})
		return nil
	}

	// Build a dynamic HIM service tree from the provided signature.
	sig, _ := msg["signature"].(map[string]interface{})
	root := buildTreeFromSignature(path, sig)

	// Register root name derives from the first segment of path.
	rootName := strings.SplitN(path, ".", 2)[0]
	domain := rootName + ".Service"
	registered := utils.RegisterServiceTree(rootName, domain, "1.0", root)
	if !registered {
		// Tree already exists — still allow the connection to update it.
		utils.Info.Printf("serviceReg: tree %q already registered, reusing", rootName)
	}

	sc := &serviceConn{path: path, conn: conn, writer: writer}
	regMu.Lock()
	registrations[path] = sc
	regMu.Unlock()

	utils.Info.Printf("serviceReg: registered service %q", path)
	replyJSON(writer, map[string]interface{}{"registered": true, "path": path})
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
		UpdateServiceState(id, StatusFailed, nil, backendChans)
	}

	rootName := strings.SplitN(sc.path, ".", 2)[0]
	utils.DeregisterServiceTree(rootName)
	utils.Info.Printf("serviceReg: deregistered %q", sc.path)
}

func handleProgress(msg map[string]interface{}, backendChans []chan map[string]interface{}) {
	sessionId, _ := msg["sessionId"].(string)
	statusStr, _ := msg["status"].(string)
	output, _ := msg["output"].(map[string]interface{})

	status := ServiceStatus(statusStr)
	switch status {
	case StatusOngoing, StatusSuccessful, StatusFailed, StatusCanceled:
	default:
		utils.Error.Printf("serviceReg: invalid status %q from service", statusStr)
		return
	}
	UpdateServiceState(sessionId, status, output, backendChans)
}

// forwardInvokeToService sends an invoke notification to the registered
// service process for path (if any). Called from HandleInvoke.
func forwardInvokeToService(path, serviceId string, input map[string]interface{}) {
	regMu.Lock()
	sc := registrations[path]
	regMu.Unlock()
	if sc == nil {
		return // no service registered; caller must call UpdateServiceState
	}

	msg := map[string]interface{}{
		"action":    "invoke",
		"sessionId": serviceId,
		"input":     input,
	}
	sc.mu.Lock()
	defer sc.mu.Unlock()
	replyJSON(sc.writer, msg)
}

// buildTreeFromSignature constructs a HIM procedure tree from the signature
// map supplied during registration.
//
// Expected signature format:
//   {
//     "input":  {"Param1": "uint32", "Param2": "string"},
//     "output": {"Result1": "uint32"}
//   }
//
// Produces: Root → ProcedureName (procedure) → Input (iostruct) → Param1, Param2
//                                            → Output (iostruct) → Result1
func buildTreeFromSignature(path string, sig map[string]interface{}) *utils.Node_t {
	// The last segment of path is the procedure name; the rest is the branch path.
	segments := strings.Split(path, ".")
	procName := segments[len(segments)-1]

	var inputChildren []*utils.Node_t
	var outputChildren []*utils.Node_t

	if sig != nil {
		if inputMap, ok := sig["input"].(map[string]interface{}); ok {
			for paramName, dt := range inputMap {
				dtStr, _ := dt.(string)
				inputChildren = append(inputChildren, utils.NewPropertyNode(paramName, dtStr, ""))
			}
		}
		if outputMap, ok := sig["output"].(map[string]interface{}); ok {
			for paramName, dt := range outputMap {
				dtStr, _ := dt.(string)
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

	// Wrap in branch nodes for intermediate segments.
	root := procNode
	for i := len(segments) - 2; i >= 0; i-- {
		root = utils.NewBranchNode(segments[i], root)
	}
	return root
}

func replyJSON(w *bufio.Writer, v interface{}) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	w.Write(b)
	w.WriteByte('\n')
	w.Flush()
}
