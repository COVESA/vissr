/**
* (C) 2026 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE
* file in this repository.
*
* ----------------------------------------------------------------------------
*
* "Tier 2" real-socket tests for the HTTP transport delivery path: the genuine
* HttpChannel handler (makeappClientHandler -> frontendHttpAppSession ->
* backendHttpAppSession) mounted on an httptest.Server and driven with a real
* net/http client. Previously this path was only reachable via
* HttpServer.InitClientServer, which binds :8081 and Fatal()s, so it was
* integration-only.
**/
package utils

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// startHttpHub emulates the server core for the HTTP handler: it reads each
// request the frontend forwards and replies on the same per-client channel
// (frontendHttpAppSession's synchronous rendezvous). Forwarded requests are
// exposed on the returned channel for assertions.
func startHttpHub(appChan chan string, reply string) (seen chan string, stop func()) {
	seen = make(chan string, 8)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			case req := <-appChan:
				seen <- req
				select {
				case appChan <- reply:
				case <-done:
					return
				}
			}
		}
	}()
	return seen, func() { close(done) }
}

func newHttpTestServer(t *testing.T) (*httptest.Server, chan string) {
	t.Helper()
	appClientChannel := []chan string{make(chan string)}
	handler := HttpChannel{}.makeappClientHandler(appClientChannel)
	srv := httptest.NewServer(http.HandlerFunc(handler))
	t.Cleanup(srv.Close)
	return srv, appClientChannel[0]
}

func TestHttpClientServer_RealSocketGet(t *testing.T) {
	srv, appChan := newHttpTestServer(t)
	seen, stop := startHttpHub(appChan, `{"action":"get","requestId":"x","data":{"path":"Vehicle.Speed","dp":{"value":"5"}}}`)
	defer stop()

	resp, err := http.Get(srv.URL + "/Vehicle.Speed")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"value":"5"`) {
		t.Fatalf("GET body = %q; want the hub's data", body)
	}
	// action/requestId are stripped from the HTTP response envelope.
	if strings.Contains(string(body), `"action"`) || strings.Contains(string(body), `"requestId"`) {
		t.Fatalf("HTTP response should not carry action/requestId: %q", body)
	}

	select {
	case fwd := <-seen:
		if !strings.Contains(fwd, `"action":"get"`) {
			t.Fatalf("forwarded request = %q; want action get", fwd)
		}
	case <-time.After(time.Second):
		t.Fatal("request was not forwarded to the hub")
	}
}

func TestHttpClientServer_RealSocketPost(t *testing.T) {
	srv, appChan := newHttpTestServer(t)
	seen, stop := startHttpHub(appChan, `{"action":"set","requestId":"x","status":"ok"}`)
	defer stop()

	resp, err := http.Post(srv.URL+"/Vehicle.Speed", "application/json",
		strings.NewReader(`{"value":"42"}`))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"status":"ok"`) {
		t.Fatalf("POST body = %q; want the hub's ack", body)
	}

	select {
	case fwd := <-seen:
		if !strings.Contains(fwd, `"action":"set"`) {
			t.Fatalf("forwarded request = %q; want action set", fwd)
		}
		if !strings.Contains(fwd, `"value":"42"`) {
			t.Fatalf("forwarded request = %q; want the posted value", fwd)
		}
	case <-time.After(time.Second):
		t.Fatal("POST was not forwarded to the hub")
	}
}

// A GET carrying a query string and a bearer token exercises the query-split
// and authorization branches of frontendHttpAppSession.
func TestHttpClientServer_RealSocketGetWithQueryAndAuth(t *testing.T) {
	srv, appChan := newHttpTestServer(t)
	seen, stop := startHttpHub(appChan, `{"action":"get","requestId":"x","data":{}}`)
	defer stop()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/Vehicle.Speed?metadata=static", nil)
	req.Header.Set("Authorization", "Bearer tok123")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET with query: %v", err)
	}
	resp.Body.Close()

	select {
	case fwd := <-seen:
		if !strings.Contains(fwd, `"authorization":"tok123"`) {
			t.Fatalf("forwarded request = %q; want the bearer token stripped to tok123", fwd)
		}
	case <-time.After(time.Second):
		t.Fatal("request was not forwarded to the hub")
	}
}

// OPTIONS falls through to the GET handling.
func TestHttpClientServer_RealSocketOptions(t *testing.T) {
	srv, appChan := newHttpTestServer(t)
	seen, stop := startHttpHub(appChan, `{"action":"get","requestId":"x","data":{}}`)
	defer stop()

	req, _ := http.NewRequest(http.MethodOptions, srv.URL+"/Vehicle.Speed", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("OPTIONS: %v", err)
	}
	resp.Body.Close()

	select {
	case fwd := <-seen:
		if !strings.Contains(fwd, `"action":"get"`) {
			t.Fatalf("OPTIONS forwarded = %q; want action get (fallthrough)", fwd)
		}
	case <-time.After(time.Second):
		t.Fatal("OPTIONS was not forwarded to the hub")
	}
}

// An unsupported HTTP method is answered directly with a 400 envelope and never
// reaches the hub.
func TestHttpClientServer_RealSocketUnsupportedMethod(t *testing.T) {
	srv, _ := newHttpTestServer(t)

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/Vehicle.Speed", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Unsupported HTTP method") {
		t.Fatalf("DELETE body = %q; want the unsupported-method error", body)
	}
}

// A POST body over the 64 KiB MaxBytesReader cap is rejected with a 413 envelope
// before reaching the hub.
func TestHttpClientServer_RealSocketOversizedBodyRejected(t *testing.T) {
	srv, _ := newHttpTestServer(t)

	big := bytes.Repeat([]byte("a"), 70*1024) // > 64 KiB cap
	resp, err := http.Post(srv.URL+"/Vehicle.Speed", "application/json", bytes.NewReader(big))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "413") {
		t.Fatalf("oversized POST body = %q; want a 413 rejection", body)
	}
}

// A websocket upgrade aimed at the HTTP port is rejected with the
// "incorrect port number" 400, exercising the guard in HttpChannel's handler.
func TestHttpClientServer_RealSocketWebsocketUpgradeRejected(t *testing.T) {
	srv, _ := newHttpTestServer(t)

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/", nil)
	req.Header.Set("Upgrade", "websocket")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET with upgrade: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d; want 400 for a websocket upgrade on the HTTP handler", resp.StatusCode)
	}
}
