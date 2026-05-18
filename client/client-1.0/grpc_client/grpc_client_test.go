/**
* (C) 2026 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE
* file in this repository.
*
* ----------------------------------------------------------------------------
*
* Coverage tests for grpc_client. The only testable pure helper is
* initCommandList, which populates the package-level commandList slice
* with four canonical VISS requests (get / set / subscribe /
* unsubscribe).
*
* The rest of the file (noStreamCall, streamCall, main,
* readTransportSecConfig, prepareTransportSecConfig) drives a gRPC
* connection to a running vissv2server and is exercised by runtest.sh
* integration.
**/
package main

import (
	"strings"
	"testing"
)

// TestInitCommandList sets up the four canonical commands. We verify
// each one is a non-empty JSON snippet that contains the expected
// action keyword.
func TestInitCommandList(t *testing.T) {
	initCommandList()
	if len(commandList) != 4 {
		t.Fatalf("commandList length = %d; want 4", len(commandList))
	}
	wantSubstrings := []string{`"get"`, `"set"`, `"subscribe"`, `"unsubscribe"`}
	for i, want := range wantSubstrings {
		if !strings.Contains(commandList[i], want) {
			t.Fatalf("commandList[%d] = %q; missing %q", i, commandList[i], want)
		}
		if !strings.Contains(commandList[i], "requestId") {
			t.Fatalf("commandList[%d] missing requestId", i)
		}
	}
}
