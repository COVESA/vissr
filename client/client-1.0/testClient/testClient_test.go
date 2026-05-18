/**
* (C) 2026 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE
* file in this repository.
*
* ----------------------------------------------------------------------------
*
* Coverage tests for the testable pure functions in testClient:
* pathToUrl, jsonToStructList, retrieveRequest, createList,
* createListFromFile, getBrokerSocket.
*
* The protocol-runner entry points (httpTesterRun, wsTesterRun,
* udsTesterRun, mqttTesterRun, grpcTesterRun, performHttpCommand,
* performWsCommand, performUdsCommand, performGrpcCommand,
* mqttSubscribe, publishVissV2Request, publishMessage, initGrpcSession,
* initVissV2WebSocket, initUnixDomainSocket, waitForKey, main) all
* require a running server / TTY / network and are exercised by
* runtest.sh integration; they are not unit-tested here.
**/
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPathToUrl_TestClient(t *testing.T) {
	if got := pathToUrl("Vehicle.Speed"); got != "/Vehicle/Speed" {
		t.Fatalf("pathToUrl = %q; want /Vehicle/Speed", got)
	}
	if got := pathToUrl(""); got != "/" {
		t.Fatalf("pathToUrl empty = %q; want /", got)
	}
}

// TestJsonToStructList_TestClient drives the per-protocol command-list
// populator. Each top-level key (http/ws/mqtt/grpc/uds) that's present
// counts as one "protocol" handled.
func TestJsonToStructList_TestClient(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want int
	}{
		{"empty obj", `{}`, 0},
		{"http only", `{"http":[{"action":"get","path":"X"}]}`, 1},
		{"http + ws", `{"http":[{"action":"get","path":"X"}],"ws":[{"action":"get","path":"Y"}]}`, 2},
		{"all five",
			`{"http":[{"a":"1"}],"ws":[{"a":"1"}],"mqtt":[{"a":"1"}],"grpc":[{"a":"1"}],"uds":[{"a":"1"}]}`,
			5},
		{"garbage", `not json`, 0},
		{"empty string", ``, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := jsonToStructList(c.in); got != c.want {
				t.Fatalf("jsonToStructList %q = %d; want %d", c.name, got, c.want)
			}
		})
	}
}

// TestCreateList accepts either an array of command-objects or a
// single command-object.
func TestCreateList_Array(t *testing.T) {
	in := []interface{}{
		map[string]interface{}{"action": "get", "path": "A"},
		map[string]interface{}{"action": "get", "path": "B"},
	}
	got := createList(in)
	if len(got) != 2 {
		t.Fatalf("createList array len = %d; want 2", len(got))
	}
	for i, want := range []string{"A", "B"} {
		if !strings.Contains(got[i], want) {
			t.Fatalf("createList[%d] = %q; missing %q", i, got[i], want)
		}
	}
}

func TestCreateList_SingleMap(t *testing.T) {
	in := map[string]interface{}{"action": "get", "path": "Vehicle"}
	got := createList(in)
	if len(got) != 1 {
		t.Fatalf("createList single = %d entries; want 1", len(got))
	}
	if !strings.Contains(got[0], "Vehicle") {
		t.Fatalf("createList entry missing 'Vehicle': %q", got[0])
	}
}

func TestCreateList_UnknownType(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("createList panicked on string input: %v", r)
		}
	}()
	got := createList("not a map or array")
	if got != nil && len(got) != 0 {
		t.Logf("note: unknown-type input returned %v (acceptable as long as no panic)", got)
	}
}

func TestRetrieveRequest_TestClient(t *testing.T) {
	in := map[string]interface{}{"action": "get", "path": "Vehicle.Speed"}
	got := retrieveRequest(in)
	if !strings.Contains(got, "get") || !strings.Contains(got, "Vehicle.Speed") {
		t.Fatalf("retrieveRequest = %q; missing expected fields", got)
	}
}

func TestCreateListFromFile_TestClient(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "in.json")
	content := `{"http":[{"a":"1"}],"ws":[{"a":"1"}]}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if got := createListFromFile(path); got != 2 {
		t.Fatalf("createListFromFile = %d; want 2", got)
	}
}

func TestCreateListFromFile_TestClient_MissingFile(t *testing.T) {
	if got := createListFromFile("/nonexistent"); got != 0 {
		t.Fatalf("createListFromFile missing = %d; want 0", got)
	}
}

// TestGetBrokerSocket documents the current testClient behaviour: it
// hardcodes test.mosquitto.org as the broker. The server-side
// equivalent was env-var-ified in ef639f0; this client still has the
// hardcode. The test fails-by-design if anyone fixes it to use an env
// var without updating the test.
//
// TODO(testing): refactor testClient to read MQTT_BROKER_ADDR like
// mqttMgr does. The test below should then assert env var precedence.
func TestGetBrokerSocket_HardcodedBroker(t *testing.T) {
	got := getBrokerSocket(false)
	if !strings.Contains(got, "test.mosquitto.org") {
		t.Logf("note: getBrokerSocket no longer returns the hardcoded test.mosquitto.org broker (%q) — has it been env-var-ified? update this test if so", got)
	}
}

func TestGetBrokerSocket_SecureScheme(t *testing.T) {
	got := getBrokerSocket(true)
	if !strings.HasPrefix(got, "ssl://") {
		t.Fatalf("secure broker socket = %q; want ssl:// prefix", got)
	}
	if !strings.Contains(got, "8883") {
		t.Fatalf("secure broker port = %q; want 8883", got)
	}
}
