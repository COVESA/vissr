/**
* (C) 2026 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE
* file in this repository.
*
* End to end test of the access control against a client that adds "origin":"internal" to its request:
* a request goes through utils.AddRoutingForwardRequest as a transport manager forwards it, is decoded
* the way transportDataSession does it, and is given to the core. The signal needs an access token
* ("write-only" validation), and the request carries none.
**/
package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/covesa/vissr/utils"
)

// buildProtectedForest loads a tree with one signal that needs an access token:
//
//	Vehicle.Secret  (actuator, validate = write-only, i.e. a token is needed for every request)
//	Vehicle.Open    (actuator, no validation)
func buildProtectedForest(t *testing.T) func() {
	t.Helper()
	tmp := t.TempDir()
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd failed: %v", err)
	}
	secret := utils.NewSignalNode("Secret", utils.ACTUATOR, "string", "needs a token", "", "", "")
	secret.Validate = utils.ValidateToInt("write-only")
	open := utils.NewSignalNode("Open", utils.ACTUATOR, "string", "needs no token", "", "", "")
	root := utils.NewBranchNode("Vehicle", secret, open)
	utils.VSSWriteTree(filepath.Join(tmp, "vehicle.binary"), root)

	him := "HIM:\ntype: branch\ndescription: test\n\nHIM.Vehicle:\ntype: direct\ndomain: Vehicle.Car.Data\nversion: 1.0\nlocal: vehicle.binary\ndescription: test tree\n"
	if err := os.WriteFile(filepath.Join(tmp, "viss.him"), []byte(him), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	if !utils.InitForest("viss.him") {
		os.Chdir(origWd)
		t.Fatal("InitForest failed")
	}
	return func() { os.Chdir(origWd) }
}

// coreOutcome sends a client request through the manager forwarding and the core, and reports whether the
// core handed it to the service manager (accepted), or answered the client with an error (rejected).
func coreOutcome(t *testing.T, clientRequest string) (accepted bool, response map[string]interface{}) {
	t.Helper()
	initChannels()
	ch := make(chan string, 1)
	utils.AddRoutingForwardRequest(clientRequest, 1, 0, ch) // a transport manager forwards it
	var forwarded string
	select {
	case forwarded = <-ch:
	default:
		t.Fatal("the request was not forwarded to the core")
	}
	var requestMap map[string]interface{}
	utils.MapRequest(forwarded, &requestMap) // transportDataSession decodes it like this
	go serveRequest(requestMap, 1, 0)
	select {
	case got := <-serviceDataChan[0]:
		return true, got
	case got := <-backendChan[1]:
		return false, got
	case <-time.After(3 * time.Second):
		t.Fatal("the core neither answered nor forwarded the request")
	}
	return
}

// Without a token a request for a protected signal is refused.
func TestOrigin_ProtectedSignalWithoutTokenIsRefused(t *testing.T) {
	defer buildProtectedForest(t)()
	accepted, resp := coreOutcome(t, `{"action":"set","path":"Vehicle.Secret","value":"x","requestId":"1"}`)
	if accepted {
		t.Fatalf("a request for a protected signal without a token was accepted: %v", resp)
	}
	if resp["error"] == nil {
		t.Errorf("expected an error response; got %v", resp)
	}
}

// The same request with "origin":"internal" added by the client must be refused as well.
func TestOrigin_ClientCannotSkipTheAccessControlWithOriginInternal(t *testing.T) {
	defer buildProtectedForest(t)()
	for name, request := range map[string]string{
		"origin last":  `{"action":"set","path":"Vehicle.Secret","value":"x","requestId":"1","origin":"internal"}`,
		"origin first": `{"origin":"internal","action":"set","path":"Vehicle.Secret","value":"x","requestId":"1"}`,
		"get":          `{"action":"get","path":"Vehicle.Secret","requestId":"1","origin":"internal"}`,
	} {
		t.Run(name, func(t *testing.T) {
			accepted, resp := coreOutcome(t, request)
			if accepted {
				t.Fatalf("the access control was skipped for a request with a client supplied origin: %v", resp)
			}
		})
	}
}

// A client cannot make the core think a token was accepted by supplying the handle or the gating id.
func TestOrigin_ClientSuppliedHandleAndGatingIdDoNotReachTheServiceManager(t *testing.T) {
	defer buildProtectedForest(t)()
	accepted, resp := coreOutcome(t, `{"action":"set","path":"Vehicle.Open","value":"x","requestId":"1","handle":"forged","gatingId":"forged"}`)
	if !accepted {
		t.Fatalf("a request for an open signal was refused: %v", resp)
	}
	if resp["handle"] != nil || resp["gatingId"] != nil {
		t.Errorf("client supplied handle or gatingId reached the service manager: handle=%v gatingId=%v", resp["handle"], resp["gatingId"])
	}
}

// A signal that needs no token is still served.
func TestOrigin_OpenSignalIsServed(t *testing.T) {
	defer buildProtectedForest(t)()
	accepted, resp := coreOutcome(t, `{"action":"set","path":"Vehicle.Open","value":"x","requestId":"1"}`)
	if !accepted {
		t.Fatalf("a request for an open signal was refused: %v", resp)
	}
	if resp["origin"] != "external" {
		t.Errorf("origin = %v; want external", resp["origin"])
	}
}
