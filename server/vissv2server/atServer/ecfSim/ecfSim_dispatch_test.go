/**
* (C) 2026 Matt Jones
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE file in this repository.
*
* Tests for the stdin-driven prepareCancelRequest and the dispatchResponse
* channel writer. The websocket client/receiver goroutines, the interactive
* uiDialogue loop, and main() are integration-only.
**/
package main

import (
	"os"
	"strings"
	"testing"
)

// withStdin feeds input to os.Stdin while fn runs (for fmt.Scanf prompts).
func withStdin(t *testing.T, input string, fn func()) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = old }()
	go func() { w.WriteString(input); w.Close() }()
	fn()
}

func TestPrepareCancelRequest(t *testing.T) {
	var got bool
	withStdin(t, "yes\n", func() { got = prepareCancelRequest(`{"action":"x"}`) })
	if !got {
		t.Error(`prepareCancelRequest with "yes" = false; want true`)
	}
	withStdin(t, "no\n", func() { got = prepareCancelRequest(`{"action":"x"}`) })
	if got {
		t.Error(`prepareCancelRequest with "no" = true; want false`)
	}
}

func TestDispatchResponse(t *testing.T) {
	sendChan := make(chan string, 1)
	dispatchResponse(`{"action":"get","messageId":"7"}`, sendChan)
	select {
	case resp := <-sendChan:
		if !strings.Contains(resp, `"action":"get"`) {
			t.Errorf("dispatchResponse sent %q; want it to echo action get", resp)
		}
	default:
		t.Fatal("dispatchResponse did not send a response")
	}
}
