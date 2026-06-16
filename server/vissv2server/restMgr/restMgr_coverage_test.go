package restMgr

// Additional coverage for the response-routing helpers and the metadata /
// subscribe (SSE) handlers, which the original restMgr_test.go left at 0%.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/covesa/vissr/utils"
)

// syncRecorder is a thread-safe http.ResponseWriter + http.Flusher for the SSE
// test, where the handler writes from its own goroutine while the test observes
// progress. It signals `notified` once the first notification frame is written.
type syncRecorder struct {
	mu       sync.Mutex
	buf      bytes.Buffer
	header   http.Header
	notified chan struct{}
	once     sync.Once
}

func newSyncRecorder() *syncRecorder {
	return &syncRecorder{header: http.Header{}, notified: make(chan struct{})}
}
func (s *syncRecorder) Header() http.Header { return s.header }
func (s *syncRecorder) WriteHeader(int)     {}
func (s *syncRecorder) Flush()              {}
func (s *syncRecorder) Write(p []byte) (int, error) {
	s.mu.Lock()
	n, err := s.buf.Write(p)
	s.mu.Unlock()
	if bytes.Contains(p, []byte("event: notification")) {
		s.once.Do(func() { close(s.notified) })
	}
	return n, err
}
func (s *syncRecorder) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// withSchema chdirs into a temp dir holding the VISS schema and initialises the
// JSON validator, restoring the cwd on cleanup. dispatch() validates requests,
// so handlers that forward to the hub need a loaded schema.
func withSchema(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	data, err := os.ReadFile(schemaSourcePath)
	if err != nil {
		t.Skipf("schema %s not readable: %v", schemaSourcePath, err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "vissv3.0-schema.json"), data, 0644); err != nil {
		t.Fatalf("write schema: %v", err)
	}
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { os.Chdir(orig) })
	utils.JsonSchemaInit()
}

// clientIdFromRouterId parses "mgrId?clientId".
func clientIdFromRouterId(routerID string) (int, bool) {
	parts := strings.SplitN(routerID, "?", 2)
	if len(parts) != 2 || parts[1] == "" {
		return 0, false
	}
	id := 0
	for _, c := range parts[1] {
		if c < '0' || c > '9' {
			return 0, false
		}
		id = id*10 + int(c-'0')
	}
	return id, true
}

// serveOneReply reads one forwarded request from ch and delivers a reply built
// by fn(routerID) to the originating client (so RemoveInternalData can route it).
func serveOneReply(ch chan string, fn func(routerID string) string) {
	go func() {
		msg := <-ch
		var m map[string]interface{}
		if json.Unmarshal([]byte(msg), &m) != nil {
			return
		}
		routerID, _ := m["RouterId"].(string)
		id, ok := clientIdFromRouterId(routerID)
		if !ok {
			return
		}
		clientMu.Lock()
		entry, found := clients[id]
		clientMu.Unlock()
		if found {
			select {
			case entry.ch <- fn(routerID):
			default:
			}
		}
	}()
}

// ── RemoveRoutingForwardResponse / routeResponse ───────────────────────────────

func TestRemoveRoutingForwardResponse_RoutesToClient(t *testing.T) {
	e := &clientEntry{ch: make(chan string, 1)}
	id := registerClient(e)
	defer unregisterClient(id)

	resp := `{"RouterId":"99?` + itoa(id) + `","action":"get","data":{"dp":{"value":"7"}}}`
	RemoveRoutingForwardResponse(resp, nil)

	select {
	case got := <-e.ch:
		if strings.Contains(got, "RouterId") {
			t.Errorf("RouterId not stripped: %q", got)
		}
		if !strings.Contains(got, `"value":"7"`) {
			t.Errorf("payload lost: %q", got)
		}
	default:
		t.Fatal("response not routed to the registered client")
	}
}

func TestRouteResponse_Wrapper(t *testing.T) {
	e := &clientEntry{ch: make(chan string, 1)}
	id := registerClient(e)
	defer unregisterClient(id)

	routeResponse(`{"RouterId":"99?`+itoa(id)+`","action":"set","status":"ok"}`, nil)
	select {
	case <-e.ch:
	default:
		t.Fatal("routeResponse did not deliver to the client")
	}
}

func TestRemoveRoutingForwardResponse_FallsThroughToSubscription(t *testing.T) {
	// clientId 999999 is not registered → must fall through to the SSE path
	// and be delivered to the matching subscription.
	ssech := make(chan string, 1)
	sseMu.Lock()
	subs["fallthrough-sub"] = &sseEntry{ch: ssech, cancel: func() {}}
	sseMu.Unlock()
	defer func() { sseMu.Lock(); delete(subs, "fallthrough-sub"); sseMu.Unlock() }()

	resp := `{"RouterId":"99?999999","subscriptionId":"fallthrough-sub","data":{"dp":{"value":"1"}}}`
	RemoveRoutingForwardResponse(resp, nil)

	select {
	case got := <-ssech:
		if !strings.Contains(got, "fallthrough-sub") {
			t.Errorf("unexpected sse payload: %q", got)
		}
	default:
		t.Fatal("notification not delivered to subscription")
	}
}

// NOTE: a response carrying no RouterId is only safe once the utils.RemoveInternalData
// hardening (PR #181) is present on the base branch — restMgr's own routing uses a
// map lookup, but the shared parser panics pre-#181. The missing-RouterId regression
// is therefore owned by #181's utils test, not this package's coverage PR.

// ── handleMetadata ─────────────────────────────────────────────────────────────

func TestHandleMetadata_ForwardsMetadataFilter(t *testing.T) {
	withSchema(t)
	ch := make(chan string, 4)
	captured := make(chan string, 1)

	go func() {
		msg := <-ch
		captured <- msg
		var m map[string]interface{}
		json.Unmarshal([]byte(msg), &m)
		rID, _ := m["RouterId"].(string)
		if id, ok := clientIdFromRouterId(rID); ok {
			clientMu.Lock()
			e, found := clients[id]
			clientMu.Unlock()
			if found {
				e.ch <- `{"RouterId":"` + rID + `","action":"get","metadata":{"x":1}}`
			}
		}
	}()

	r := httptest.NewRequest(http.MethodGet, "/viss/v2/metadata/Vehicle.Speed", nil)
	w := httptest.NewRecorder()
	makeHandler(ch)(w, r)

	select {
	case msg := <-captured:
		if !strings.Contains(msg, `"variant":"metadata"`) {
			t.Errorf("metadata filter missing in forwarded request: %s", msg)
		}
		// Regression: the filter must include a parameter or the schema rejects it.
		if !strings.Contains(msg, `"parameter":"static"`) {
			t.Errorf("metadata filter missing parameter (would 400): %s", msg)
		}
		if !strings.Contains(msg, `"action":"get"`) {
			t.Errorf("action:get missing: %s", msg)
		}
	default:
		t.Error("no request captured from hub (metadata request rejected by schema?)")
	}
	if w.Code != http.StatusOK {
		t.Errorf("metadata GET: code=%d, want 200; body=%s", w.Code, w.Body.String())
	}
}

// ── handleSubscribe ────────────────────────────────────────────────────────────

// An error ack (no subscriptionId) is relayed verbatim and the handler returns
// without opening an SSE stream.
func TestHandleSubscribe_ErrorAckRelayed(t *testing.T) {
	withSchema(t)
	ch := make(chan string, 4)
	serveOneReply(ch, func(routerID string) string {
		return `{"RouterId":"` + routerID + `","error":{"number":404,"reason":"unavailable_data"}}`
	})

	r := httptest.NewRequest(http.MethodGet, "/viss/v2/Vehicle.Speed/subscribe", nil)
	w := httptest.NewRecorder()
	makeHandler(ch)(w, r)

	if !strings.Contains(w.Body.String(), "unavailable_data") {
		t.Errorf("error ack not relayed: body=%s", w.Body.String())
	}
}

// Full SSE path: subscribe ack → stream open → notification delivered → client
// disconnect ends the stream.
func TestHandleSubscribe_SSEStream(t *testing.T) {
	withSchema(t)
	const subID = "sse-stream-sub"
	ch := make(chan string, 8)
	serveOneReply(ch, func(routerID string) string {
		return `{"RouterId":"` + routerID + `","action":"subscribe","subscriptionId":"` + subID + `"}`
	})

	ctx, cancel := context.WithCancel(context.Background())
	r := httptest.NewRequest(http.MethodGet, "/viss/v2/Vehicle.Speed/subscribe", nil).WithContext(ctx)
	w := newSyncRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		makeHandler(ch)(w, r)
	}()

	// Wait until the SSE subscription is registered, then push a notification.
	if !waitForSub(subID, 2*time.Second) {
		cancel()
		<-done
		t.Fatal("SSE subscription was never registered")
	}
	routeSubscriptionEvent(`{"subscriptionId":"` + subID + `","data":{"dp":{"value":"55"}}}`)

	// Wait for the handler to write the notification frame, then disconnect.
	select {
	case <-w.notified:
	case <-time.After(2 * time.Second):
		cancel()
		<-done
		t.Fatalf("notification frame was never written: %s", w.String())
	}
	cancel()
	<-done

	body := w.String()
	if !strings.Contains(body, "event: subscribe") {
		t.Errorf("missing initial subscribe event: %s", body)
	}
	if !strings.Contains(body, "event: notification") || !strings.Contains(body, `"value":"55"`) {
		t.Errorf("notification not streamed: %s", body)
	}
}

// ── handleUnsubscribe success path ─────────────────────────────────────────────

func TestHandleUnsubscribe_Success(t *testing.T) {
	const subID = "unsub-ok"
	cancelled := false
	sseMu.Lock()
	subs[subID] = &sseEntry{ch: make(chan string, 1), cancel: func() { cancelled = true }}
	sseMu.Unlock()
	defer func() { sseMu.Lock(); delete(subs, subID); sseMu.Unlock() }()

	r := httptest.NewRequest(http.MethodDelete, "/viss/v2/Vehicle.Speed/subscribe?subscriptionId="+subID, nil)
	w := httptest.NewRecorder()
	makeHandler(make(chan string, 1))(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("unsubscribe success: code=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "cancelled") {
		t.Errorf("body missing cancelled status: %s", w.Body.String())
	}
	if !cancelled {
		t.Error("subscription cancel func was not invoked")
	}
}

// ── dispatch timeout path ──────────────────────────────────────────────────────

func TestDispatch_Timeout(t *testing.T) {
	withSchema(t)
	orig := responseTimeout
	responseTimeout = 20 * time.Millisecond
	defer func() { responseTimeout = orig }()

	ch := make(chan string, 1) // request is forwarded but never answered
	r := httptest.NewRequest(http.MethodGet, "/viss/v2/Vehicle.Speed", nil)
	w := httptest.NewRecorder()
	makeHandler(ch)(w, r)

	if w.Code != http.StatusGatewayTimeout {
		t.Errorf("timeout: code=%d, want 504; body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "timeout") {
		t.Errorf("body missing timeout message: %s", w.Body.String())
	}
}

func waitForSub(id string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		sseMu.Lock()
		_, ok := subs[id]
		sseMu.Unlock()
		if ok {
			return true
		}
		time.Sleep(2 * time.Millisecond)
	}
	return false
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}
