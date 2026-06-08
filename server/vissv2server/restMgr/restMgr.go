// Package restMgr provides a REST+SSE transport for VISS.
//
// Endpoints:
//
//   GET  /viss/v2/{path}               — read a signal (VISS get)
//   PUT  /viss/v2/{path}               — write a signal (VISS set); body: {"value":"…"}
//   GET  /viss/v2/{path}/subscribe     — SSE stream of signal values (VISS subscribe)
//   DELETE /viss/v2/{path}/subscribe   — cancel an active SSE subscription
//   GET  /viss/v2/metadata/{path}      — metadata for a node (VISS get + filter)
//
// Wire format: VISS JSON payloads forwarded through the standard
// transportMgrChannel / backendChannel pair, identical to every other
// transport manager.
package restMgr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/covesa/vissr/utils"
)

// clientEntry tracks one REST client awaiting a response.
type clientEntry struct {
	ch chan string
}

var (
	clientMu    sync.Mutex
	clients     = map[int]*clientEntry{}
	nextID      atomic.Int64
	mgrIdGlobal int
)

// sseEntry tracks one active SSE subscription.
type sseEntry struct {
	cancel context.CancelFunc
	ch     chan string
}

var (
	sseMu sync.Mutex
	subs  = map[string]*sseEntry{} // subscriptionId → sseEntry
)

// RestMgrInit starts the REST transport on addr (e.g. ":8081").
// mgrId is the channel slot index assigned to this transport in vissv2server.
// Call in a goroutine; it blocks until the server stops.
func RestMgrInit(mgrId int, transportMgrChan chan string, addr string) {
	mgrIdGlobal = mgrId
	utils.JsonSchemaInit()

	mux := http.NewServeMux()
	mux.HandleFunc("/viss/v2/", makeHandler(transportMgrChan))

	go func() {
		utils.Info.Printf("restMgr: listening on http://%s", addr)
		if err := http.ListenAndServe(addr, mux); err != nil {
			utils.Error.Printf("restMgr: server stopped: %v", err)
		}
	}()

	// Dispatch loop: pull responses from the transport hub and route
	// them back to the waiting HTTP handler by clientId.
	for resp := range transportMgrChan {
		routeResponse(resp, transportMgrChan)
	}
}

// makeHandler returns the single mux handler for all /viss/v2/ requests.
func makeHandler(ch chan string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// Parse path: /viss/v2/<signal-path>[/subscribe]
		rawPath := strings.TrimPrefix(r.URL.Path, "/viss/v2/")

		// Metadata shortcut: /viss/v2/metadata/<path>
		if strings.HasPrefix(rawPath, "metadata/") {
			signalPath := strings.TrimPrefix(rawPath, "metadata/")
			handleMetadata(w, r, signalPath, ch)
			return
		}

		isSubscribe := strings.HasSuffix(rawPath, "/subscribe")
		signalPath := rawPath
		if isSubscribe {
			signalPath = strings.TrimSuffix(rawPath, "/subscribe")
		}
		signalPath = pathFromURL(signalPath)

		switch {
		case isSubscribe && r.Method == http.MethodGet:
			handleSubscribe(w, r, signalPath, ch)
		case isSubscribe && r.Method == http.MethodDelete:
			handleUnsubscribe(w, r, signalPath)
		case r.Method == http.MethodGet:
			handleGet(w, r, signalPath, ch)
		case r.Method == http.MethodPut:
			handleSet(w, r, signalPath, ch)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

// handleGet serves GET /viss/v2/{path}
func handleGet(w http.ResponseWriter, r *http.Request, signalPath string, ch chan string) {
	requestID := newRequestID()
	reqJSON := fmt.Sprintf(`{"action":"get","path":"%s","requestId":"%s"}`, signalPath, requestID)
	w.Header().Set("Content-Type", "application/json")
	dispatch(w, reqJSON, requestID, ch)
}

// handleSet serves PUT /viss/v2/{path}
func handleSet(w http.ResponseWriter, r *http.Request, signalPath string, ch chan string) {
	var body struct {
		Value     string `json:"value"`
		Timestamp string `json:"ts,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":{"number":400,"reason":"bad_request","message":"invalid body"}}`, http.StatusBadRequest)
		return
	}
	requestID := newRequestID()
	reqJSON := fmt.Sprintf(`{"action":"set","path":"%s","value":"%s","requestId":"%s"}`,
		signalPath, jsonEscapeString(body.Value), requestID)
	w.Header().Set("Content-Type", "application/json")
	dispatch(w, reqJSON, requestID, ch)
}

// handleMetadata serves GET /viss/v2/metadata/{path}
func handleMetadata(w http.ResponseWriter, r *http.Request, signalPath string, ch chan string) {
	requestID := newRequestID()
	reqJSON := fmt.Sprintf(`{"action":"get","path":"%s","filter":{"variant":"metadata"},"requestId":"%s"}`,
		signalPath, requestID)
	w.Header().Set("Content-Type", "application/json")
	dispatch(w, reqJSON, requestID, ch)
}

// handleSubscribe serves GET /viss/v2/{path}/subscribe — opens an SSE stream.
func handleSubscribe(w http.ResponseWriter, r *http.Request, signalPath string, ch chan string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported by server", http.StatusInternalServerError)
		return
	}

	requestID := newRequestID()
	reqJSON := fmt.Sprintf(`{"action":"subscribe","path":"%s","requestId":"%s"}`,
		signalPath, requestID)

	// Register a client entry so routeResponse can deliver the first
	// ack (and subsequent subscription events via subscriptionId).
	entry := &clientEntry{ch: make(chan string, 16)}
	clientID := registerClient(entry)
	defer unregisterClient(clientID)

	// Forward to transport hub.
	sendToHub(reqJSON, mgrIdGlobal, clientID, ch)

	// Wait for the subscribe ack to get the subscriptionId.
	var subsID string
	select {
	case ack := <-entry.ch:
		var m map[string]interface{}
		if err := json.Unmarshal([]byte(ack), &m); err == nil {
			if v, ok := m["subscriptionId"].(string); ok {
				subsID = v
			}
		}
		if subsID == "" {
			// Error response — relay and close.
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, ack)
			return
		}
	case <-time.After(10 * time.Second):
		http.Error(w, `{"error":{"number":503,"reason":"service_unavailable","message":"subscribe timeout"}}`, http.StatusServiceUnavailable)
		return
	case <-r.Context().Done():
		return
	}

	// Register SSE subscription so DELETE can cancel it.
	ctx, cancel := context.WithCancel(r.Context())
	sseCh := make(chan string, 32)
	sseMu.Lock()
	subs[subsID] = &sseEntry{cancel: cancel, ch: sseCh}
	sseMu.Unlock()
	defer func() {
		sseMu.Lock()
		delete(subs, subsID)
		sseMu.Unlock()
		cancel()
		// Send unsubscribe.
		unsubID := newRequestID()
		unsubJSON := fmt.Sprintf(`{"action":"unsubscribe","subscriptionId":"%s","requestId":"%s"}`,
			subsID, unsubID)
		unsubEntry := &clientEntry{ch: make(chan string, 1)}
		uid := registerClient(unsubEntry)
		sendToHub(unsubJSON, mgrIdGlobal, uid, ch)
		unregisterClient(uid)
	}()

	// SSE headers.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	// Send the subscription ack as the first SSE event.
	fmt.Fprintf(w, "event: subscribe\ndata: {\"subscriptionId\":\"%s\"}\n\n", subsID)
	flusher.Flush()

	for {
		select {
		case <-ctx.Done():
			return
		case <-r.Context().Done():
			return
		case event := <-sseCh:
			fmt.Fprintf(w, "event: notification\ndata: %s\n\n", event)
			flusher.Flush()
		}
	}
}

// handleUnsubscribe serves DELETE /viss/v2/{path}/subscribe?subscriptionId=...
func handleUnsubscribe(w http.ResponseWriter, r *http.Request, _ string) {
	subsID := r.URL.Query().Get("subscriptionId")
	if subsID == "" {
		http.Error(w, `{"error":{"number":400,"reason":"bad_request","message":"missing subscriptionId"}}`, http.StatusBadRequest)
		return
	}
	sseMu.Lock()
	entry, ok := subs[subsID]
	sseMu.Unlock()
	if !ok {
		http.Error(w, `{"error":{"number":404,"reason":"not_found","message":"subscription not found"}}`, http.StatusNotFound)
		return
	}
	entry.cancel()
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"subscriptionId":"%s","status":"cancelled"}`, subsID)
}

// ── routing helpers ───────────────────────────────────────────────────────────

// RemoveRoutingForwardResponse is called by vissv2server to route a response
// back to the REST handler that is waiting for it.
func RemoveRoutingForwardResponse(response string, transportMgrChan chan string) {
	trimmed, clientID := utils.RemoveInternalData(response)
	clientMu.Lock()
	entry, ok := clients[clientID]
	clientMu.Unlock()
	if ok {
		select {
		case entry.ch <- trimmed:
		default:
		}
		return
	}
	// It may be a subscription notification — check by subscriptionId.
	routeSubscriptionEvent(trimmed)
}

func routeResponse(response string, _ chan string) {
	RemoveRoutingForwardResponse(response, nil)
}

func routeSubscriptionEvent(msg string) {
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(msg), &m); err != nil {
		return
	}
	subsID, _ := m["subscriptionId"].(string)
	if subsID == "" {
		return
	}
	sseMu.Lock()
	entry, ok := subs[subsID]
	sseMu.Unlock()
	if !ok {
		return
	}
	select {
	case entry.ch <- msg:
	default: // slow client — drop
	}
}

// dispatch sends a VISS request to the hub and writes the response to w.
func dispatch(w http.ResponseWriter, reqJSON, requestID string, ch chan string) {
	validationError := utils.JsonSchemaValidate(reqJSON)
	if len(validationError) > 0 {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `{"error":{"number":400,"reason":"bad_request","message":%q}}`, validationError)
		return
	}
	entry := &clientEntry{ch: make(chan string, 1)}
	clientID := registerClient(entry)
	defer unregisterClient(clientID)

	sendToHub(reqJSON, mgrIdGlobal, clientID, ch)

	select {
	case resp := <-entry.ch:
		fmt.Fprint(w, resp)
	case <-time.After(10 * time.Second):
		w.WriteHeader(http.StatusGatewayTimeout)
		fmt.Fprint(w, `{"error":{"number":503,"reason":"service_unavailable","message":"request timeout"}}`)
	}
}

func sendToHub(reqJSON string, mgrID, clientID int, ch chan string) {
	utils.AddRoutingForwardRequest(reqJSON, mgrID, clientID, ch)
}

// ── client registry ───────────────────────────────────────────────────────────

func registerClient(e *clientEntry) int {
	id := int(nextID.Add(1))
	clientMu.Lock()
	clients[id] = e
	clientMu.Unlock()
	return id
}

func unregisterClient(id int) {
	clientMu.Lock()
	delete(clients, id)
	clientMu.Unlock()
}

// ── utils ────────────────────────────────────────────────────────────────────

func newRequestID() string {
	return fmt.Sprintf("rest-%d", nextID.Add(1))
}

// pathFromURL converts a URL path segment (slashes) to a dot-separated VISS path.
// VISS paths use dots; REST paths use dots too but clients may send slashes.
func pathFromURL(p string) string {
	return strings.ReplaceAll(p, "/", ".")
}

func jsonEscapeString(s string) string {
	b, _ := json.Marshal(s)
	// json.Marshal wraps in quotes; strip them.
	if len(b) >= 2 {
		return string(b[1 : len(b)-1])
	}
	return s
}
