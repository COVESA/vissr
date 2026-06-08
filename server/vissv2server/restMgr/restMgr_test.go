package restMgr

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/covesa/vissr/utils"
)

const schemaSourcePath = "../vissv3.0-schema.json"

func copySchemaToDir(t *testing.T, dir string) {
	t.Helper()
	data, err := os.ReadFile(schemaSourcePath)
	if err != nil {
		t.Skipf("schema file %s not readable: %v (skipping)", schemaSourcePath, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "vissv3.0-schema.json"), data, 0644); err != nil {
		t.Fatalf("write schema: %v", err)
	}
}

func init() {
	utils.InitLog("restmgr-log.txt", os.TempDir(), false, "info")
	mgrIdGlobal = 99
}

// ── helpers ──────────────────────────────────────────────────────────────────

// replyOnChannel starts a goroutine that reads one request from ch, extracts
// the clientId from the RouterId, and delivers replyJSON to that client.
func replyOnChannel(ch chan string, replyJSON string) {
	go func() {
		msg := <-ch
		var m map[string]interface{}
		if err := json.Unmarshal([]byte(msg), &m); err != nil {
			return
		}
		routerID, _ := m["RouterId"].(string)
		// routerID is "mgrId?clientId"
		parts := strings.SplitN(routerID, "?", 2)
		if len(parts) != 2 {
			return
		}
		clientID := 0
		for _, c := range parts[1] {
			clientID = clientID*10 + int(c-'0')
		}
		// Build reply that includes RouterId so RemoveInternalData strips correctly.
		reply := `{"RouterId":"` + routerID + `",` + replyJSON[1:]
		clientMu.Lock()
		entry, ok := clients[clientID]
		clientMu.Unlock()
		if ok {
			select {
			case entry.ch <- reply:
			default:
			}
		}
	}()
}

// ── pathFromURL ───────────────────────────────────────────────────────────────

func TestPathFromURL_Dots(t *testing.T) {
	if got := pathFromURL("Vehicle.Speed"); got != "Vehicle.Speed" {
		t.Errorf("got %q", got)
	}
}

func TestPathFromURL_Slashes(t *testing.T) {
	if got := pathFromURL("Vehicle/Speed"); got != "Vehicle.Speed" {
		t.Errorf("got %q", got)
	}
}

func TestPathFromURL_Empty(t *testing.T) {
	if got := pathFromURL(""); got != "" {
		t.Errorf("got %q", got)
	}
}

// ── jsonEscapeString ──────────────────────────────────────────────────────────

func TestJsonEscapeString_Plain(t *testing.T) {
	if got := jsonEscapeString("hello"); got != "hello" {
		t.Errorf("got %q", got)
	}
}

func TestJsonEscapeString_Quotes(t *testing.T) {
	got := jsonEscapeString(`say "hi"`)
	if !strings.Contains(got, `\"`) {
		t.Errorf("quotes not escaped: %q", got)
	}
}

func TestJsonEscapeString_Empty(t *testing.T) {
	if got := jsonEscapeString(""); got != "" {
		t.Errorf("got %q", got)
	}
}

// ── CORS preflight ────────────────────────────────────────────────────────────

func TestOptionsReturns204(t *testing.T) {
	ch := make(chan string, 1)
	handler := makeHandler(ch)
	r := httptest.NewRequest(http.MethodOptions, "/viss/v2/Vehicle.Speed", nil)
	w := httptest.NewRecorder()
	handler(w, r)
	if w.Code != http.StatusNoContent {
		t.Errorf("OPTIONS: code = %d, want 204", w.Code)
	}
	if w.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Error("missing CORS header")
	}
}

// ── GET (handleGet) ───────────────────────────────────────────────────────────

func TestGetForwardsAction(t *testing.T) {
	tmp := t.TempDir()
	copySchemaToDir(t, tmp)
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer os.Chdir(orig)
	utils.JsonSchemaInit()

	var captured string
	ch := make(chan string, 2)

	go func() {
		msg := <-ch
		captured = msg
		replyOnChannel(ch, `{"action":"get","path":"Vehicle.Speed","data":{"dp":{"value":"42","ts":"2026-01-01T00:00:00Z"}}}`)
		ch <- msg // put back so replyOnChannel can read it
	}()

	r := httptest.NewRequest(http.MethodGet, "/viss/v2/Vehicle.Speed", nil)
	w := httptest.NewRecorder()
	makeHandler(ch)(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("GET: code = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	_ = captured
}

// TestGetForwardsActionAndPath verifies the action and path are included
// in the message sent to the hub.
func TestGetForwardsActionAndPath(t *testing.T) {
	tmp := t.TempDir()
	copySchemaToDir(t, tmp)
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer os.Chdir(orig)
	utils.JsonSchemaInit()

	ch := make(chan string, 2)
	captured := make(chan string, 1)

	go func() {
		msg := <-ch
		captured <- msg
		// Build a reply routed to the correct clientId.
		var m map[string]interface{}
		json.Unmarshal([]byte(msg), &m)
		rID, _ := m["RouterId"].(string)
		reply := `{"RouterId":"` + rID + `","action":"get","path":"Vehicle.Speed","data":{"dp":{"value":"0"}}}`
		parts := strings.SplitN(rID, "?", 2)
		if len(parts) == 2 {
			clientID := 0
			for _, c := range parts[1] {
				clientID = clientID*10 + int(c-'0')
			}
			clientMu.Lock()
			e, ok := clients[clientID]
			clientMu.Unlock()
			if ok {
				e.ch <- reply
			}
		}
	}()

	r := httptest.NewRequest(http.MethodGet, "/viss/v2/Vehicle.Speed", nil)
	w := httptest.NewRecorder()
	makeHandler(ch)(w, r)

	select {
	case msg := <-captured:
		if !strings.Contains(msg, `"action":"get"`) {
			t.Errorf("forwarded message missing action:get: %s", msg)
		}
		if !strings.Contains(msg, `"path":"Vehicle.Speed"`) {
			t.Errorf("forwarded message missing path: %s", msg)
		}
	default:
		t.Error("no message captured from hub")
	}
	if w.Code != http.StatusOK {
		t.Errorf("GET: code = %d, want 200; body=%s", w.Code, w.Body.String())
	}
}

// ── PUT (handleSet) ───────────────────────────────────────────────────────────

func TestPutForwardsActionSet(t *testing.T) {
	tmp := t.TempDir()
	copySchemaToDir(t, tmp)
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer os.Chdir(orig)
	utils.JsonSchemaInit()

	ch := make(chan string, 2)
	captured := make(chan string, 1)

	go func() {
		msg := <-ch
		captured <- msg
		var m map[string]interface{}
		json.Unmarshal([]byte(msg), &m)
		rID, _ := m["RouterId"].(string)
		reply := `{"RouterId":"` + rID + `","action":"set","status":"ok"}`
		parts := strings.SplitN(rID, "?", 2)
		if len(parts) == 2 {
			clientID := 0
			for _, c := range parts[1] {
				clientID = clientID*10 + int(c-'0')
			}
			clientMu.Lock()
			e, ok := clients[clientID]
			clientMu.Unlock()
			if ok {
				e.ch <- reply
			}
		}
	}()

	body := `{"value":"true"}`
	r := httptest.NewRequest(http.MethodPut, "/viss/v2/Vehicle.ADAS.ABS.IsEnabled", strings.NewReader(body))
	w := httptest.NewRecorder()
	makeHandler(ch)(w, r)

	select {
	case msg := <-captured:
		if !strings.Contains(msg, `"action":"set"`) {
			t.Errorf("forwarded message missing action:set: %s", msg)
		}
		if !strings.Contains(msg, `"value":"true"`) {
			t.Errorf("forwarded message missing value: %s", msg)
		}
	default:
		t.Error("no message captured from hub")
	}
	if w.Code != http.StatusOK {
		t.Errorf("PUT: code = %d, want 200; body=%s", w.Code, w.Body.String())
	}
}

func TestPutBadBody(t *testing.T) {
	ch := make(chan string, 1)
	r := httptest.NewRequest(http.MethodPut, "/viss/v2/Vehicle.Speed", strings.NewReader("not json"))
	w := httptest.NewRecorder()
	makeHandler(ch)(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("bad body: code = %d, want 400", w.Code)
	}
}

// ── method not allowed ────────────────────────────────────────────────────────

func TestUnknownMethodReturns405(t *testing.T) {
	ch := make(chan string, 1)
	r := httptest.NewRequest(http.MethodPost, "/viss/v2/Vehicle.Speed", nil)
	w := httptest.NewRecorder()
	makeHandler(ch)(w, r)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST: code = %d, want 405", w.Code)
	}
}

// ── unsubscribe ───────────────────────────────────────────────────────────────

func TestDeleteUnsubscribeMissingID(t *testing.T) {
	ch := make(chan string, 1)
	r := httptest.NewRequest(http.MethodDelete, "/viss/v2/Vehicle.Speed/subscribe", nil)
	w := httptest.NewRecorder()
	makeHandler(ch)(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("DELETE missing subId: code = %d, want 400", w.Code)
	}
}

func TestDeleteUnsubscribeNotFound(t *testing.T) {
	ch := make(chan string, 1)
	r := httptest.NewRequest(http.MethodDelete, "/viss/v2/Vehicle.Speed/subscribe?subscriptionId=nonexistent", nil)
	w := httptest.NewRecorder()
	makeHandler(ch)(w, r)
	if w.Code != http.StatusNotFound {
		t.Errorf("DELETE not-found sub: code = %d, want 404", w.Code)
	}
}

// ── registerClient / unregisterClient ─────────────────────────────────────────

func TestClientRegistry(t *testing.T) {
	e := &clientEntry{ch: make(chan string, 1)}
	id := registerClient(e)
	clientMu.Lock()
	_, ok := clients[id]
	clientMu.Unlock()
	if !ok {
		t.Error("client not registered")
	}
	unregisterClient(id)
	clientMu.Lock()
	_, ok = clients[id]
	clientMu.Unlock()
	if ok {
		t.Error("client still registered after unregister")
	}
}

// ── routeSubscriptionEvent ────────────────────────────────────────────────────

func TestRouteSubscriptionEvent_Delivers(t *testing.T) {
	ch := make(chan string, 1)
	sseMu.Lock()
	subs["test-sub-1"] = &sseEntry{ch: ch, cancel: func() {}}
	sseMu.Unlock()
	defer func() {
		sseMu.Lock()
		delete(subs, "test-sub-1")
		sseMu.Unlock()
	}()

	routeSubscriptionEvent(`{"subscriptionId":"test-sub-1","data":{"dp":{"value":"99"}}}`)
	select {
	case msg := <-ch:
		if !strings.Contains(msg, "test-sub-1") {
			t.Errorf("unexpected message: %s", msg)
		}
	default:
		t.Error("no message delivered to SSE channel")
	}
}

func TestRouteSubscriptionEvent_UnknownSub(t *testing.T) {
	// Should not panic when subscription is not registered.
	routeSubscriptionEvent(`{"subscriptionId":"unknown-xyz","data":{}}`)
}

func TestRouteSubscriptionEvent_BadJSON(t *testing.T) {
	// Should not panic on garbage input.
	routeSubscriptionEvent("not json at all")
}

// ── Fuzz ──────────────────────────────────────────────────────────────────────

func FuzzPathFromURL(f *testing.F) {
	f.Add("Vehicle.Speed")
	f.Add("Vehicle/Speed")
	f.Add("")
	f.Add("a/b/c.d")
	f.Fuzz(func(t *testing.T, path string) {
		_ = pathFromURL(path)
	})
}

func FuzzJsonEscapeString(f *testing.F) {
	f.Add("hello")
	f.Add(`say "hi"`)
	f.Add("")
	f.Add("\n\t\r")
	f.Fuzz(func(t *testing.T, s string) {
		_ = jsonEscapeString(s)
	})
}
