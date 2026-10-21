/**
* (C) 2026 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE
* file in this repository.
*
* Tests that handleWsClientRequest validates every client request against the schema, except the
* internal "internal-killsubscriptions" message that the manager creates itself. The exception is
* decided by the action of the message, not by whether its text contains the name of the action.
**/
package wsMgr

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/covesa/vissr/utils"
)

var wsSchemaOnce sync.Once

func loadWsSchema(t *testing.T) {
	t.Helper()
	wsSchemaOnce.Do(func() {
		data, err := os.ReadFile(filepath.Join("..", "vissv3.0-schema.json"))
		if err != nil {
			t.Fatalf("cannot read the schema: %v", err)
		}
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "vissv3.0-schema.json"), data, 0644); err != nil {
			t.Fatal(err)
		}
		orig, _ := os.Getwd()
		os.Chdir(dir)
		utils.JsonSchemaInit()
		os.Chdir(orig)
	})
}

// handle runs handleWsClientRequest for client 0 and reports whether the request was forwarded to the core
// (and what was forwarded) or answered with an error.
func handle(t *testing.T, request string) (forwarded string, answered string) {
	t.Helper()
	loadWsSchema(t)
	transport := make(chan string, 4)
	wsClientChan[0] = make(chan string, 4)
	done := make(chan struct{})
	go func() { handleWsClientRequest(request, 1, 0, transport); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("handleWsClientRequest did not return")
	}
	select {
	case forwarded = <-transport:
	default:
	}
	select {
	case answered = <-wsClientChan[0]:
	default:
	}
	return
}

func TestIsKillSubscriptions(t *testing.T) {
	cases := map[string]bool{
		`{"action":"internal-killsubscriptions"}`:                      true,
		`{ "action" : "internal-killsubscriptions" }`:                  true,
		`{"action":"get","path":"Vehicle.Speed"}`:                      false,
		`{"action":"get","note":"\"internal-killsubscriptions\""}`:     false,
		`{"action":"get","path":"Vehicle.internal-killsubscriptions"}`: false,
		`{"action":["internal-killsubscriptions"]}`:                    false,
		`{"action":42}`:                         false,
		`{"note":"internal-killsubscriptions"}`: false,
		`not json`:                              false,
		``:                                      false,
		`["internal-killsubscriptions"]`:        false,
		`{"action":"internal-killsubscriptions","path":"Vehicle.Speed"}`:            true, // it is the action that decides
		`{"action":"internal-killsubscriptions-and-more"}`:                          false,
		`{"action":"get"}{"action":"internal-killsubscriptions"}`:                   false,
		`{"action":"internal-killsubscriptions"} trailing`:                          false,
		`{"action":"get","action":"internal-killsubscriptions"}`:                    true, // encoding/json keeps the last of two equal keys, as the core does
		`{"action":"internal-killsubscriptions","action":"get","path":"Vehicle.X"}`: false,
	}
	for msg, want := range cases {
		if got := isKillSubscriptions(msg); got != want {
			t.Errorf("isKillSubscriptions(%q) = %v; want %v", msg, got, want)
		}
	}
}

// The manager's own kill message is forwarded without a schema check, as before.
func TestHandleWsClientRequest_KillSubscriptionsMessageIsForwardedWithoutValidation(t *testing.T) {
	forwarded, answered := handle(t, `{"action":"internal-killsubscriptions"}`)
	if forwarded == "" || answered != "" {
		t.Fatalf("forwarded=%q answered=%q; want the kill message forwarded", forwarded, answered)
	}
}

// A client request that merely contains the text of the kill action must be validated like any other.
func TestHandleWsClientRequest_KillTextInsideAnotherRequestDoesNotSkipTheSchemaCheck(t *testing.T) {
	// every one of these is invalid under the schema: a get needs a path (or data), a set needs a value.
	// The text of the action is there as a plain string, as a key, or as part of a longer string, which
	// is what the old check (strings.Contains of the quoted name) reacted to.
	cases := map[string]string{
		"get without path, name as a value":        `{"action":"get","requestId":"1","note":"internal-killsubscriptions"}`,
		"get without path, name as a key":          `{"action":"get","requestId":"1","internal-killsubscriptions":true}`,
		"get without path, name as the request id": `{"action":"get","requestId":"internal-killsubscriptions"}`,
		"set without value, name as a value":       `{"action":"set","path":"Vehicle.Secret","note":"internal-killsubscriptions"}`,
		"unknown action, name as a value":          `{"action":"frobnicate","note":"internal-killsubscriptions"}`,
		"subscribe without filter, name in a list": `{"action":"subscribe","path":"Vehicle.Speed","x":["internal-killsubscriptions"]}`,
	}
	for name, request := range cases {
		t.Run(name, func(t *testing.T) {
			forwarded, answered := handle(t, request)
			if forwarded != "" {
				t.Errorf("a request that is not valid under the schema was forwarded to the core: %s", forwarded)
			}
			if answered == "" {
				t.Errorf("the client got no error response")
			}
		})
	}
}

// Ordinary valid and invalid requests behave as before.
func TestHandleWsClientRequest_ValidRequestIsForwardedAndInvalidIsRejected(t *testing.T) {
	if forwarded, _ := handle(t, `{"action":"get","path":"Vehicle.Speed","requestId":"1"}`); forwarded == "" {
		t.Error("a valid request was not forwarded")
	}
	if forwarded, answered := handle(t, `{"action":"get","requestId":"1"}`); forwarded != "" || answered == "" {
		t.Errorf("an invalid request: forwarded=%q answered=%q; want it rejected", forwarded, answered)
	}
}
