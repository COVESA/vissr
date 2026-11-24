/**
* (C) 2026 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE file in this repository.
*
* Tests for the multiple tree addressability Read and the multiple data Update over HTTP
* (POST to the root path, see the HTTPS section of the VISS TRANSPORT specification), and for the
* HTTP status code of the response.
**/

package utils

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClassifyHttpDataRequest(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		wantAction string
		wantLen    int
		wantReason int    // index in ErrorInfoList, checked only if wantErr
		wantErr    string // substring of the error message, empty if success
	}{
		{"multi-set", `{"data":[{"path":"Vehicle.A.B","value":"1"},{"path":"Vehicle.C","value":"2"}]}`, "set", 2, 0, ""},
		{"multi-get", `{"data":["Vehicle.A.B","Vehicle.C"]}`, "get", 2, 0, ""},
		{"multi-set with one element", `{"data":[{"path":"Vehicle.A","value":"1"}]}`, "set", 1, 0, ""},
		{"multi-set, struct value", `{"data":[{"path":"Vehicle.A","value":{"x":"1","y":"2"}}]}`, "set", 1, 0, ""},
		{"multi-set, array value", `{"data":[{"path":"Vehicle.A","value":["1","2"]}]}`, "set", 1, 0, ""},
		// The reason for not searching the body text: these are reads, but the body contains "value".
		{"multi-get, path ends with value", `{"data":["Vehicle.Setting.value","Vehicle.C"]}`, "get", 2, 0, ""},
		{"multi-get, path is value", `{"data":["value"]}`, "get", 1, 0, ""},
		{"multi-get, whitespace and key order", " {\n \"requestId\":\"1\", \"data\" : [ \"Vehicle.A\" ] }\n", "get", 1, 0, ""},
		{"empty array", `{"data":[]}`, "", 0, 1, "non-empty"},
		{"data is not an array, string", `{"data":"Vehicle.A"}`, "", 0, 1, "non-empty array"},
		{"data is not an array, object", `{"data":{"path":"Vehicle.A","value":"1"}}`, "", 0, 1, "non-empty array"},
		{"data is null", `{"data":null}`, "", 0, 1, "non-empty array"},
		{"no data key", `{"foo":"bar"}`, "", 0, 0, `no "data"`},
		{"malformed JSON", `{"data":[`, "", 0, 0, "not a JSON object"},
		{"empty body", ``, "", 0, 0, "not a JSON object"},
		{"JSON array body", `["Vehicle.A"]`, "", 0, 0, "not a JSON object"},
		{"JSON null body", `null`, "", 0, 0, "not a JSON object"},
		{"elements are numbers", `{"data":[1,2]}`, "", 0, 1, "paths (strings) or path/value objects"},
		{"mixed, path first", `{"data":["Vehicle.A",{"path":"Vehicle.B","value":"1"}]}`, "", 0, 1, "all paths"},
		{"mixed, object first", `{"data":[{"path":"Vehicle.B","value":"1"},"Vehicle.A"]}`, "", 0, 1, "all paths"},
		{"mixed, null last", `{"data":["Vehicle.A",null]}`, "", 0, 1, "all paths"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			action, data, reason, errMsg := classifyHttpDataRequest([]byte(c.body))
			if c.wantErr != "" {
				if errMsg == "" || !strings.Contains(errMsg, c.wantErr) {
					t.Fatalf("errMsg=%q, want it to contain %q", errMsg, c.wantErr)
				}
				if action != "" || data != nil {
					t.Errorf("on error action and data must be empty, got %q, %v", action, data)
				}
				if reason != c.wantReason {
					t.Errorf("reasonIndex=%d, want %d", reason, c.wantReason)
				}
				return
			}
			if errMsg != "" {
				t.Fatalf("unexpected error: %s", errMsg)
			}
			if action != c.wantAction || len(data) != c.wantLen {
				t.Errorf("action=%q len=%d, want action=%q len=%d", action, len(data), c.wantAction, c.wantLen)
			}
		})
	}
}

// postToHttpSession runs a request through frontendHttpAppSession. If the request is forwarded to the manager hub
// the forwarded message is returned, and the hub answers with hubResponse. If not, forwarded is empty.
func postToHttpSession(t *testing.T, method, uri, body, hubResponse string) (forwarded string, rec *httptest.ResponseRecorder) {
	t.Helper()
	clientChannel := make(chan string, 4)
	req := httptest.NewRequest(method, uri, strings.NewReader(body))
	rec = httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		frontendHttpAppSession(rec, req, clientChannel)
	}()
	select {
	case forwarded = <-clientChannel:
		clientChannel <- hubResponse
		<-done
	case <-done: // answered by the HTTP manager without forwarding
	case <-time.After(3 * time.Second):
		t.Fatal("timeout: frontendHttpAppSession neither forwarded nor answered")
	}
	return forwarded, rec
}

func TestFrontendHttpAppSession_MultiSignalForwarding(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		wantAction string
		wantData   string // substring of the forwarded message
	}{
		{"multi-set", `{"data":[{"path":"Vehicle.A","value":"1"},{"path":"Vehicle.B","value":"2"}]}`, "set", `"data":[{"path":"Vehicle.A","value":"1"}`},
		{"multi-get", `{"data":["Vehicle.A","Trailer1.B"]}`, "get", `"data":["Vehicle.A","Trailer1.B"]`},
		{"multi-get, path ends with value", `{"data":["Vehicle.Setting.value"]}`, "get", `"data":["Vehicle.Setting.value"]`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			forwarded, rec := postToHttpSession(t, "POST", "/", c.body, `{"action":"set","ts":"2020-04-15T13:37:00Z"}`)
			if forwarded == "" {
				t.Fatalf("request was not forwarded, response: %d %s", rec.Code, rec.Body.String())
			}
			var m map[string]interface{}
			if err := json.Unmarshal([]byte(forwarded), &m); err != nil {
				t.Fatalf("forwarded message is not JSON: %v: %s", err, forwarded)
			}
			if m["action"] != c.wantAction {
				t.Errorf("action=%v, want %s, forwarded: %s", m["action"], c.wantAction, forwarded)
			}
			if _, hasPath := m["path"]; hasPath {
				t.Errorf("a multi-signal request must not have a path: %s", forwarded)
			}
			if !strings.Contains(forwarded, c.wantData) {
				t.Errorf("forwarded message %s does not contain %s", forwarded, c.wantData)
			}
			if rec.Code != http.StatusOK {
				t.Errorf("status=%d, want 200", rec.Code)
			}
		})
	}
}

// A request that is not a valid multi-signal request is answered by the HTTP manager with a 400 error,
// and must not be forwarded to the server core.
func TestFrontendHttpAppSession_MultiSignalRejected(t *testing.T) {
	bodies := map[string]string{
		"empty array":   `{"data":[]}`,
		"no data":       `{"foo":"bar"}`,
		"malformed":     `{"data":[`,
		"mixed":         `{"data":["Vehicle.A",{"path":"Vehicle.B","value":"1"}]}`,
		"data a string": `{"data":"Vehicle.A"}`,
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			forwarded, rec := postToHttpSession(t, "POST", "/", body, "")
			if forwarded != "" {
				t.Fatalf("an invalid request must not be forwarded: %s", forwarded)
			}
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status=%d, want 400, body: %s", rec.Code, rec.Body.String())
			}
			var m map[string]interface{}
			if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
				t.Fatalf("response is not JSON: %v: %s", err, rec.Body.String())
			}
			errMap, ok := m["error"].(map[string]interface{})
			if !ok || errMap["number"] != "400" {
				t.Errorf("expected an error object with number 400, got %s", rec.Body.String())
			}
			if m["ts"] == nil {
				t.Errorf("an error response must have a ts: %s", rec.Body.String())
			}
		})
	}
}

// The single data Update and the single tree Read are unchanged.
func TestFrontendHttpAppSession_SinglePathUnchanged(t *testing.T) {
	forwarded, _ := postToHttpSession(t, "POST", "/Vehicle/A/B", `{"value":"1"}`, `{"action":"set","ts":"x"}`)
	if !strings.Contains(forwarded, `"action":"set"`) || !strings.Contains(forwarded, `"path":"/Vehicle/A/B"`) ||
		!strings.Contains(forwarded, `"value":"1"`) || strings.Contains(forwarded, `"data"`) {
		t.Errorf("single set not forwarded as before: %s", forwarded)
	}
	forwarded, _ = postToHttpSession(t, "GET", "/Vehicle/A/B", ``, `{"action":"get","ts":"x"}`)
	if !strings.Contains(forwarded, `"action":"get"`) || !strings.Contains(forwarded, `"path":"/Vehicle/A/B"`) {
		t.Errorf("single get not forwarded as before: %s", forwarded)
	}
}

func TestHttpStatusFromResponse(t *testing.T) {
	cases := []struct {
		name string
		resp string
		want int
	}{
		{"success", `{"data":{"path":"Vehicle.A","dp":{"value":"1","ts":"x"}},"ts":"x"}`, 200},
		{"set success", `{"ts":"x"}`, 200},
		{"error is null", `{"error":null,"ts":"x"}`, 200},
		{"400 bad_request", `{"error":{"number":"400","reason":"bad_request","description":"d"},"ts":"x"}`, 400},
		{"401 invalid_token", `{"error":{"number":"401","reason":"invalid_token","description":"d"},"ts":"x"}`, 401},
		{"403 forbidden_request", `{"error":{"number":"403","reason":"forbidden_request","description":"d"},"ts":"x"}`, 403},
		{"404 unavailable_data", `{"error":{"number":"404","reason":"unavailable_data","description":"d"},"ts":"x"}`, 404},
		{"503 service_unavailable", `{"error":{"number":"503","reason":"service_unavailable","description":"d"},"ts":"x"}`, 503},
		{"number as a JSON number", `{"error":{"number":404,"reason":"r","description":"d"},"ts":"x"}`, 404},
		{"short form used by the managers", `{"error":"413","reason":"Request body too large"}`, 413},
		{"number not numeric", `{"error":{"number":"abc","reason":"r","description":"d"}}`, 500},
		{"number missing", `{"error":{"reason":"r","description":"d"}}`, 500},
		{"number below 400", `{"error":{"number":"200","reason":"r","description":"d"}}`, 500},
		{"number above 599", `{"error":{"number":"999","reason":"r","description":"d"}}`, 500},
		{"error of unexpected type", `{"error":true}`, 500},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var m map[string]interface{}
			if err := json.Unmarshal([]byte(c.resp), &m); err != nil {
				t.Fatal(err)
			}
			if got := httpStatusFromResponse(m); got != c.want {
				t.Errorf("status=%d, want %d", got, c.want)
			}
		})
	}
}

// The HTTP status code of the response written to the client follows the error number of the response from the core.
func TestBackendHttpAppSession_StatusAndHeaders(t *testing.T) {
	cases := []struct {
		name       string
		message    string
		wantStatus int
		wantAuth   bool // WWW-Authenticate header
	}{
		{"success", `{"action":"get","requestId":"1","data":{"path":"Vehicle.A","dp":{"value":"1","ts":"x"}},"ts":"x"}`, 200, false},
		{"404", `{"action":"get","requestId":"1","error":{"number":"404","reason":"unavailable_data","description":"d"},"ts":"x"}`, 404, false},
		{"401", `{"action":"set","requestId":"1","error":{"number":"401","reason":"invalid_token","description":"d"},"ts":"x"}`, 401, true},
		{"too large", `{"error": "413", "reason": "Request body too large", "description":"max 64 KiB"}`, 413, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			var w http.ResponseWriter = rec
			backendHttpAppSession(c.message, &w)
			if rec.Code != c.wantStatus {
				t.Errorf("status=%d, want %d", rec.Code, c.wantStatus)
			}
			if got := rec.Header().Get("WWW-Authenticate") != ""; got != c.wantAuth {
				t.Errorf("WWW-Authenticate present=%v, want %v", got, c.wantAuth)
			}
			if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
				t.Errorf("Content-Type=%q, want application/json", ct)
			}
			if strings.Contains(rec.Body.String(), `"action"`) || strings.Contains(rec.Body.String(), `"requestId"`) {
				t.Errorf("action and requestId must be removed from the HTTP response: %s", rec.Body.String())
			}
		})
	}
}
