/**
* (C) 2026 Ford Motor Company
*
* VISSv3.3-alpha — Service Software Development Kit
*
* Package vissServiceSDK provides a Go SDK for implementing VISSv3.3 service
* procedures. A service process:
*   1. Creates a Service via NewService.
*   2. Declares its procedure signature via WithInput / WithOutput.
*   3. Registers an invocation handler via OnInvoke.
*   4. Calls Register() to connect to the VISS server.
*   5. Uses ReportProgress() to push state updates during execution.
*
* Example:
*
*   svc, err := vissServiceSDK.NewService("localhost:8300", "VehicleService.Seating.MoveSeat").
*       WithInput("SeatId", "string").
*       WithInput("Position", "uint8").
*       WithOutput("Position", "uint8").
*       OnInvoke(func(ctx *vissServiceSDK.InvokeContext) {
*           // ...move the seat...
*           ctx.ReportProgress("ONGOING", map[string]interface{}{"Position": "25"})
*           ctx.ReportProgress("SUCCESSFUL", map[string]interface{}{"Position": "40"})
*       }).
*       Register()
*   if err != nil { log.Fatal(err) }
*   defer svc.Close()
*   svc.Run() // blocks
**/

package vissServiceSDK

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"sync"
)

// InvokeHandler is called for each incoming invoke request.
type InvokeHandler func(ctx *InvokeContext)

// InvokeContext carries the parameters for one invocation and provides methods
// to report progress back to the VISS server.
type InvokeContext struct {
	SessionId string
	Input     map[string]interface{}
	svc       *Service
}

// ReportProgress sends a status update to the VISS server.
// status must be one of: "ONGOING", "SUCCESSFUL", "CANCELED", "FAILED".
// output may be nil for intermediate progress reports.
func (ctx *InvokeContext) ReportProgress(status string, output map[string]interface{}) error {
	return ctx.svc.sendProgress(ctx.SessionId, status, output)
}

// Service represents a registered VISS service procedure.
type Service struct {
	serverAddr string
	path       string
	signature  map[string]map[string]string // "input"/"output" → {paramName: datatype}
	handler    InvokeHandler
	conn       net.Conn
	writer     *bufio.Writer
	mu         sync.Mutex
	stopCh     chan struct{}
}

// NewService creates a new service bound to the given procedure path.
// serverAddr is the VISS server registration address, e.g., "localhost:8300".
func NewService(serverAddr, path string) *Service {
	return &Service{
		serverAddr: serverAddr,
		path:       path,
		signature: map[string]map[string]string{
			"input":  {},
			"output": {},
		},
		stopCh: make(chan struct{}),
	}
}

// WithInput declares an input parameter of the procedure signature.
func (s *Service) WithInput(name, datatype string) *Service {
	s.signature["input"][name] = datatype
	return s
}

// WithOutput declares an output parameter of the procedure signature.
func (s *Service) WithOutput(name, datatype string) *Service {
	s.signature["output"][name] = datatype
	return s
}

// OnInvoke registers the handler that is called for each incoming invocation.
func (s *Service) OnInvoke(h InvokeHandler) *Service {
	s.handler = h
	return s
}

// Register connects to the VISS server and sends the registration message.
// Returns the connected service ready for Run(), or an error.
func (s *Service) Register() (*Service, error) {
	if s.handler == nil {
		return nil, fmt.Errorf("vissServiceSDK: OnInvoke handler must be set before Register")
	}

	conn, err := net.Dial("tcp", s.serverAddr)
	if err != nil {
		return nil, fmt.Errorf("vissServiceSDK: connect to %s: %w", s.serverAddr, err)
	}

	s.conn = conn
	s.writer = bufio.NewWriter(conn)

	// Convert signature to JSON-compatible nested map.
	sig := map[string]interface{}{}
	for side, params := range s.signature {
		if len(params) > 0 {
			m := map[string]interface{}{}
			for name, dt := range params {
				m[name] = dt
			}
			sig[side] = m
		}
	}

	regMsg := map[string]interface{}{
		"action":    "register",
		"path":      s.path,
		"signature": sig,
	}
	if err := s.sendJSON(regMsg); err != nil {
		conn.Close()
		return nil, fmt.Errorf("vissServiceSDK: send register: %w", err)
	}

	// Read ack.
	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		conn.Close()
		return nil, fmt.Errorf("vissServiceSDK: no ack from server")
	}
	var ack map[string]interface{}
	if err := json.Unmarshal(scanner.Bytes(), &ack); err != nil || ack["registered"] != true {
		conn.Close()
		reason, _ := ack["reason"].(string)
		return nil, fmt.Errorf("vissServiceSDK: registration rejected: %s", reason)
	}

	return s, nil
}

// Run blocks, reading invoke messages from the server and dispatching them to
// the registered handler. Returns when the connection is closed or Close() is called.
func (s *Service) Run() {
	scanner := bufio.NewScanner(s.conn)
	for {
		select {
		case <-s.stopCh:
			return
		default:
		}
		if !scanner.Scan() {
			return
		}
		var msg map[string]interface{}
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			continue
		}
		action, _ := msg["action"].(string)
		if action != "invoke" {
			continue
		}
		sessionId, _ := msg["sessionId"].(string)
		input, _ := msg["input"].(map[string]interface{})
		ctx := &InvokeContext{SessionId: sessionId, Input: input, svc: s}
		go s.handler(ctx)
	}
}

// Close deregisters from the server and closes the connection.
func (s *Service) Close() {
	select {
	case <-s.stopCh:
	default:
		close(s.stopCh)
	}
	s.sendJSON(map[string]interface{}{"action": "deregister"}) //nolint:errcheck
	if s.conn != nil {
		s.conn.Close()
	}
}

func (s *Service) sendProgress(sessionId, status string, output map[string]interface{}) error {
	msg := map[string]interface{}{
		"sessionId": sessionId,
		"status":    status,
	}
	if output != nil {
		msg["output"] = output
	}
	return s.sendJSON(msg)
}

func (s *Service) sendJSON(v interface{}) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writer.Write(b)
	s.writer.WriteByte('\n')
	return s.writer.Flush()
}
