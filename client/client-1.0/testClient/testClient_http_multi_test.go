/**
* (C) 2026 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE
* file in this repository.
*
* ----------------------------------------------------------------------------
*
* Tests for buildHttpRequest, which maps a request in the VISS JSON format to an HTTP request,
* including the multiple tree addressability Read and the multiple data Update (POST to the root path).
**/
package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/covesa/vissr/utils"
)

func buildFromJson(t *testing.T, request string) (string, string, string, error) {
	t.Helper()
	var reqMap map[string]interface{}
	if utils.MapRequest(request, &reqMap) != 0 {
		t.Fatalf("test request is not valid JSON: %s", request)
	}
	return buildHttpRequest(reqMap)
}

func TestBuildHttpRequest(t *testing.T) {
	cases := []struct {
		name       string
		request    string
		wantMethod string
		wantURL    string
		wantBody   string // exact for single requests, checked as JSON for data requests
		wantErr    bool
	}{
		{"single get", `{"action":"get","path":"Vehicle.Speed"}`, "GET", http_url + "/Vehicle.Speed", "", false},
		{"single get with filter", `{"action":"get","path":"Vehicle.ADAS","filter":{"variant":"paths","parameter":"ABS.*"}}`,
			"GET", http_url + `/Vehicle.ADAS?filter={"parameter":"ABS.*","variant":"paths"}`, "", false},
		{"single set", `{"action":"set","path":"Vehicle.A","value":"1"}`, "POST", http_url + "/Vehicle.A", `{"value":"1"}`, false},
		{"multi get", `{"action":"get","data":["Vehicle.A","Trailer1.B"],"requestId":"1"}`,
			"POST", http_url + "/", `{"data":["Vehicle.A","Trailer1.B"]}`, false},
		{"multi set", `{"action":"set","data":[{"path":"Vehicle.A","value":"1"},{"path":"Vehicle.B","value":"2"}],"requestId":"2"}`,
			"POST", http_url + "/", `{"data":[{"path":"Vehicle.A","value":"1"},{"path":"Vehicle.B","value":"2"}]}`, false},
		{"neither path nor data", `{"action":"get"}`, "", "", "", true},
		{"set without string value", `{"action":"set","path":"Vehicle.A","value":{"x":"1"}}`, "", "", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			method, url, body, err := buildFromJson(t, c.request)
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got %s %s %s", method, url, body)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if method != c.wantMethod || url != c.wantURL {
				t.Errorf("got %s %s, want %s %s", method, url, c.wantMethod, c.wantURL)
			}
			if strings.HasPrefix(c.wantBody, `{"data"`) {
				var got, want map[string]interface{}
				if err := json.Unmarshal([]byte(body), &got); err != nil {
					t.Fatalf("body is not JSON: %v: %s", err, body)
				}
				_ = json.Unmarshal([]byte(c.wantBody), &want)
				g, _ := json.Marshal(got)
				w, _ := json.Marshal(want)
				if string(g) != string(w) {
					t.Errorf("body %s, want %s", g, w)
				}
				if _, hasAction := got["action"]; hasAction {
					t.Errorf("the body of a multi-signal request must only contain data: %s", body)
				}
			} else if body != c.wantBody {
				t.Errorf("body %q, want %q", body, c.wantBody)
			}
		})
	}
}
