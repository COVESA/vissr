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
*   4. Optionally enables auto-reconnect via WithReconnect.
*   5. Calls Register() to connect to the VISS server.
*   6. Uses ReportProgress() or ReportError() to push state updates.
*
* Example:
*
*   svc, err := vissServiceSDK.NewService("localhost:8300", "VehicleService.Seating.MoveSeat").
*       WithInput("SeatId", "string").
*       WithInput("Position", "uint8").
*       WithOutput("Position", "uint8").
*       WithReconnect(5, time.Second).
*       OnInvoke(func(ctx *vissServiceSDK.InvokeContext) {
*           // ctx.Authorization carries the client auth token (may be empty)
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
	"time"
)

// InvokeHandler is called for each incoming invoke request.
type InvokeHandler func(ctx *InvokeContext)

// InvokeContext carries the parameters for one invocation and provides methods
// to report progress back to the VISS server.
type InvokeContext struct {
	SessionId     string
	Input         map[string]interface{}
	Authorization string // forwarded client auth token (VISSv3.3 §21); may be empty
	svc           *Service
	doneCh        chan struct{} // closed when server cancels this invocation (§26)
}

// Done returns a channel that is closed when the server cancels this invocation
// (VISSv3.3 §26). Handlers should select on Done() to stop early.
func (ctx *InvokeContext) Done() <-chan struct{} {
	return ctx.doneCh
}

// ReportProgress sends a status update to the VISS server.
// status must be one of: "ONGOING", "SUCCESSFUL", "CANCELED", "FAILED".
// output may be nil for intermediate progress reports.
func (ctx *InvokeContext) ReportProgress(status string, output map[string]interface{}) error {
	msg := map[string]interface{}{
		"sessionId": ctx.SessionId,
		"status":    status,
	}
	if output != nil {
		msg["output"] = output
	}
	return ctx.svc.sendJSON(msg)
}

// ReportProgressPct sends an ONGOING status update with a completion percentage
// (0-100) in the "progress" field (VISSv3.3 §28). Values outside [0,100] are
// clamped. output may be nil.
func (ctx *InvokeContext) ReportProgressPct(pct int, status string, output map[string]interface{}) error {
	if pct < 0 {
		pct = 0
	} else if pct > 100 {
		pct = 100
	}
	msg := map[string]interface{}{
		"sessionId": ctx.SessionId,
		"status":    status,
		"progress":  pct,
	}
	if output != nil {
		msg["output"] = output
	}
	return ctx.svc.sendJSON(msg)
}

// ReportError sends a FAILED status with a structured error payload to the
// VISS server (VISSv3.3 §20). output may be nil.
func (ctx *InvokeContext) ReportError(code, message string, output map[string]interface{}) error {
	msg := map[string]interface{}{
		"sessionId": ctx.SessionId,
		"status":    "FAILED",
		"error": map[string]interface{}{
			"code":    code,
			"message": message,
		},
	}
	if output != nil {
		msg["output"] = output
	}
	return ctx.svc.sendJSON(msg)
}

// Service represents a registered VISS service procedure.
type Service struct {
	serverAddr string
	path       string
	signature  map[string]map[string]string // "input"/"output" → {paramName: datatype}
	handler    InvokeHandler
	conn       net.Conn
	writer     *bufio.Writer
	scanner    *bufio.Scanner
	mu         sync.Mutex
	stopCh     chan struct{}
	reconnect  bool
	maxRetries int
	retryDelay time.Duration
	version    string                    // optional version declaration (§27)
	activeCtxs map[string]*InvokeContext // live invocation contexts keyed by sessionId (§26)
	ctxMu      sync.Mutex
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
		stopCh:     make(chan struct{}),
		activeCtxs: make(map[string]*InvokeContext),
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

// WithVersion declares the service implementation version, included in the
// registration message and discover responses (VISSv3.3 §27).
func (s *Service) WithVersion(v string) *Service {
	s.version = v
	return s
}

// WithReconnect enables automatic reconnect on connection loss (VISSv3.3 §24).
// maxRetries is the maximum number of reconnect attempts (0 = unlimited).
// initialDelay is the starting backoff; it doubles on each failure, capped at 2 min.
func (s *Service) WithReconnect(maxRetries int, initialDelay time.Duration) *Service {
	s.reconnect = true
	s.maxRetries = maxRetries
	if initialDelay <= 0 {
		initialDelay = time.Second
	}
	s.retryDelay = initialDelay
	return s
}

// Register connects to the VISS server and sends the registration message.
// Returns the connected service ready for Run(), or an error.
func (s *Service) Register() (*Service, error) {
	if s.handler == nil {
		return nil, fmt.Errorf("vissServiceSDK: OnInvoke handler must be set before Register")
	}
	if err := s.connect(); err != nil {
		return nil, err
	}
	return s, nil
}

// connect establishes (or re-establishes) the TCP connection and completes
// the registration handshake. It stores the resulting conn, writer, and scanner.
func (s *Service) connect() error {
	conn, err := net.Dial("tcp", s.serverAddr)
	if err != nil {
		return fmt.Errorf("vissServiceSDK: connect to %s: %w", s.serverAddr, err)
	}

	s.conn = conn
	s.writer = bufio.NewWriter(conn)
	s.scanner = bufio.NewScanner(conn)

	// Build registration message.
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
	if s.version != "" {
		regMsg["version"] = s.version
	}
	if err := s.sendJSON(regMsg); err != nil {
		conn.Close()
		return fmt.Errorf("vissServiceSDK: send register: %w", err)
	}

	// Read ack from the same scanner we'll reuse in Run().
	if !s.scanner.Scan() {
		conn.Close()
		return fmt.Errorf("vissServiceSDK: no ack from server")
	}
	var ack map[string]interface{}
	if err := json.Unmarshal(s.scanner.Bytes(), &ack); err != nil || ack["registered"] != true {
		conn.Close()
		reason, _ := ack["reason"].(string)
		return fmt.Errorf("vissServiceSDK: registration rejected: %s", reason)
	}
	// Auto-report healthy status after successful registration (§30).
	s.sendJSON(map[string]interface{}{ //nolint:errcheck
		"action":  "health",
		"healthy": true,
		"detail":  "running",
	})
	return nil
}

// Run blocks, reading messages from the server and dispatching invocations to
// the registered handler. If WithReconnect was called, it automatically
// re-registers on connection loss with exponential backoff (VISSv3.3 §24).
func (s *Service) Run() {
	retries := 0
	for {
		s.runLoop()

		select {
		case <-s.stopCh:
			return
		default:
		}
		if !s.reconnect {
			return
		}
		if s.maxRetries > 0 && retries >= s.maxRetries {
			return
		}

		delay := s.retryDelay
		for i := 0; i < retries; i++ {
			delay *= 2
			if delay > 2*time.Minute {
				delay = 2 * time.Minute
				break
			}
		}

		select {
		case <-time.After(delay):
		case <-s.stopCh:
			return
		}

		if err := s.connect(); err != nil {
			retries++
			continue
		}
		retries = 0
	}
}

// runLoop reads and dispatches messages until the connection closes or stopCh fires.
func (s *Service) runLoop() {
	for {
		select {
		case <-s.stopCh:
			return
		default:
		}
		if !s.scanner.Scan() {
			return
		}
		var msg map[string]interface{}
		if err := json.Unmarshal(s.scanner.Bytes(), &msg); err != nil {
			continue
		}
		action, _ := msg["action"].(string)
		switch action {
		case "invoke":
			sessionId, _ := msg["sessionId"].(string)
			input, _ := msg["input"].(map[string]interface{})
			authToken, _ := msg["authorization"].(string)
			ctx := &InvokeContext{
				SessionId:     sessionId,
				Input:         input,
				Authorization: authToken,
				svc:           s,
				doneCh:        make(chan struct{}),
			}
			s.ctxMu.Lock()
			s.activeCtxs[sessionId] = ctx
			s.ctxMu.Unlock()
			go func() {
				s.handler(ctx)
				s.ctxMu.Lock()
				delete(s.activeCtxs, sessionId)
				s.ctxMu.Unlock()
			}()
		case "cancel":
			// Close the done channel for the matching invocation context (§26).
			sessionId, _ := msg["sessionId"].(string)
			s.ctxMu.Lock()
			if ctx, ok := s.activeCtxs[sessionId]; ok {
				select {
				case <-ctx.doneCh:
				default:
					close(ctx.doneCh)
				}
			}
			s.ctxMu.Unlock()
		case "ping":
			// Heartbeat: respond with pong (VISSv3.3 §19).
			s.sendJSON(map[string]interface{}{"action": "pong"}) //nolint:errcheck
		}
	}
}

// Close deregisters from the server and closes the connection. Idempotent.
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

// ReportHealth sends a health status update to the VISS server (VISSv3.3 §30).
// A healthy:true report is sent automatically after Register(); call this to
// update the status at any time (e.g., to report degraded health).
func (s *Service) ReportHealth(healthy bool, detail string) error {
	return s.sendJSON(map[string]interface{}{
		"action":  "health",
		"healthy": healthy,
		"detail":  detail,
	})
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
