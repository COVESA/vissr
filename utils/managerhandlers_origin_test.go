/**
* (C) 2026 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE
* file in this repository.
*
* Tests that the transport managers decide the routing fields of a request they forward, and that a
* client cannot supply them. The server core trusts these fields of the request it receives:
*
*   RouterId   which manager and client the response goes to
*   origin     "internal" makes the core skip the access control check
*   handle     the access token handle, set by the core after it has checked the token
*   gatingId   set by the core after it has checked the token
*   infoType   set by the core from the tree
*
* None of them is part of the request schema, so none may come from a client.
**/
package utils

import (
	"encoding/json"
	"strings"
	"testing"

	pb "github.com/covesa/vissr/grpc_pb"
)

// forwardOne forwards the request the way a transport manager does and returns what the core
// receives, decoded the way the core decodes it (utils.MapRequest, i.e. encoding/json).
func forwardOne(t *testing.T, request string) (text string, decoded map[string]interface{}) {
	t.Helper()
	ch := make(chan string, 1)
	AddRoutingForwardRequest(request, 3, 7, ch)
	select {
	case text = <-ch:
	default:
		return "", nil
	}
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		t.Fatalf("the forwarded message is not valid JSON: %v\n%s", err, text)
	}
	return text, decoded
}

// trustedFields are the fields the core trusts; a client request must not be able to set them.
var trustedFields = []string{"origin", "RouterId", "handle", "gatingId", "infoType"}

func TestAddRoutingForwardRequest_ClientCannotSetTrustedFields(t *testing.T) {
	cases := map[string]string{
		"origin internal at the end":     `{"action":"get","path":"Vehicle.Speed","requestId":"1","origin":"internal"}`,
		"origin internal at the start":   `{"origin":"internal","action":"get","path":"Vehicle.Speed","requestId":"1"}`,
		"origin in the middle":           `{"action":"get","origin":"internal","path":"Vehicle.Speed"}`,
		"origin external spelled out":    `{"action":"get","path":"Vehicle.Speed","origin":"external"}`,
		"RouterId of another client":     `{"action":"get","path":"Vehicle.Speed","RouterId":"0?5"}`,
		"handle":                         `{"action":"set","path":"Vehicle.X","value":"1","handle":"abc"}`,
		"gatingId":                       `{"action":"subscribe","path":"Vehicle.Speed","gatingId":"4242"}`,
		"infoType":                       `{"action":"get","path":"Vehicle.Speed","infoType":"Data"}`,
		"all of them":                    `{"action":"get","path":"Vehicle.Speed","origin":"internal","RouterId":"0?5","handle":"h","gatingId":"g","infoType":"i"}`,
		"origin with whitespace in key":  `{"action":"get", "origin" : "internal", "path":"Vehicle.Speed"}`,
		"origin of a non string type":    `{"action":"get","path":"Vehicle.Speed","origin":true}`,
		"escaped key spelling of origin": `{"action":"get","path":"Vehicle.Speed","\u006frigin":"internal"}`,
	}
	for name, request := range cases {
		t.Run(name, func(t *testing.T) {
			text, got := forwardOne(t, request)
			if got == nil {
				t.Fatal("the request was not forwarded")
			}
			if got["origin"] != "external" {
				t.Errorf("origin = %v; want external\n%s", got["origin"], text)
			}
			if got["RouterId"] != "3?7" {
				t.Errorf("RouterId = %v; want 3?7\n%s", got["RouterId"], text)
			}
			for _, f := range []string{"handle", "gatingId", "infoType"} {
				if _, present := got[f]; present {
					t.Errorf("client supplied %q reached the core: %v\n%s", f, got[f], text)
				}
			}
			// every trusted field appears exactly once in the text, so no decoder can pick another one
			for _, f := range trustedFields {
				if n := strings.Count(text, `"`+f+`"`); n > 1 {
					t.Errorf("%q appears %d times in the forwarded text:\n%s", f, n, text)
				}
			}
		})
	}
}

// Everything else in the request must survive unchanged.
func TestAddRoutingForwardRequest_KeepsTheRestOfTheRequest(t *testing.T) {
	request := `{"action":"subscribe","path":"Vehicle.Speed","filter":{"variant":"timebased","parameter":{"period":"1000"}},"requestId":"246","authorization":"tok.en.here","dc":"2+1"}`
	_, got := forwardOne(t, request)
	var want map[string]interface{}
	if err := json.Unmarshal([]byte(request), &want); err != nil {
		t.Fatal(err)
	}
	for k, v := range want {
		gotJSON, _ := json.Marshal(got[k])
		wantJSON, _ := json.Marshal(v)
		if string(gotJSON) != string(wantJSON) {
			t.Errorf("field %q = %s; want %s", k, gotJSON, wantJSON)
		}
	}
	if len(got) != len(want)+2 { // plus RouterId and origin
		t.Errorf("forwarded has %d fields; want %d: %v", len(got), len(want)+2, got)
	}
}

// Values must not be altered by the re-encoding: numbers, big numbers, unicode, escapes, nested data.
func TestAddRoutingForwardRequest_ValuesAreNotAltered(t *testing.T) {
	request := `{"action":"set","path":"Vehicle.X","value":"a\"b\\c\n\u00e5","data":[{"path":"P","value":12345678901234567890},{"path":"Q","value":1.5e300}],"requestId":"1"}`
	text, _ := forwardOne(t, request)
	for _, must := range []string{`12345678901234567890`, `1.5e300`} {
		if !strings.Contains(text, must) {
			t.Errorf("number %s was altered by the re-encoding:\n%s", must, text)
		}
	}
	var in, out map[string]interface{}
	json.Unmarshal([]byte(request), &in)
	json.Unmarshal([]byte(text), &out)
	if in["value"] != out["value"] {
		t.Errorf("value changed: %q -> %q", in["value"], out["value"])
	}
}

// Messages the managers create themselves, and message shapes that were forwarded before, still work.
func TestAddRoutingForwardRequest_InternalMessagesAreStillForwarded(t *testing.T) {
	_, got := forwardOne(t, `{"action":"internal-killsubscriptions"}`)
	if got == nil || got["action"] != "internal-killsubscriptions" || got["RouterId"] != "3?7" || got["origin"] != "external" {
		t.Errorf("got %v", got)
	}
}

// A message that is not a JSON object must not be forwarded: it cannot be a request, and the
// text splice used before would have built something the core can not parse.
func TestAddRoutingForwardRequest_NotAJsonObjectIsNotForwarded(t *testing.T) {
	for _, bad := range []string{``, `not json`, `[1,2,3]`, `"a string"`, `42`, `null`, `{"action":"get"`, `{"a":1}{"origin":"internal"}`} {
		ch := make(chan string, 1)
		AddRoutingForwardRequest(bad, 3, 7, ch)
		select {
		case got := <-ch:
			t.Errorf("%q was forwarded as %q", bad, got)
		default:
		}
	}
}

// gRPC builds the request JSON by concatenating the protobuf strings without escaping, so a client
// can close the string and add fields of its own. Whatever it adds, the manager's fields win.
func TestGrpcRequests_InjectedFieldsDoNotReachTheCore(t *testing.T) {
	requests := map[string]string{
		"set value":     SetRequestPbToJson(&pb.SetRequestMessage{Path: "Vehicle.TripMeterReading", Value: `1","origin":"internal`, RequestId: "1"}),
		"set path":      SetRequestPbToJson(&pb.SetRequestMessage{Path: `Vehicle.X","origin":"internal`, Value: "1", RequestId: "1"}),
		"get path":      GetRequestPbToJson(&pb.GetRequestMessage{Path: `Vehicle.Speed","origin":"internal`, RequestId: "1"}),
		"get requestId": GetRequestPbToJson(&pb.GetRequestMessage{Path: "Vehicle.Speed", RequestId: `1","origin":"internal","gatingId":"7`}),
	}
	for name, js := range requests {
		t.Run(name, func(t *testing.T) {
			text, got := forwardOne(t, js)
			if got == nil {
				return // not forwarded: also fine
			}
			if got["origin"] != "external" {
				t.Errorf("origin = %v; want external\n%s", got["origin"], text)
			}
			if _, present := got["gatingId"]; present {
				t.Errorf("injected gatingId reached the core\n%s", text)
			}
		})
	}
}
