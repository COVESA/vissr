/**
* (C) 2026 Matt Jones
*
* Unit tests for grpc_map_client.
*
* The network entry points (noStreamCall, streamCall, main) require a running
* gRPC server and are integration-only — not unit-tested here.
**/
package main

import (
	"os"
	"testing"

	"github.com/covesa/vissr/utils"
)

func init() {
	utils.InitLog("grpc_map_client-test.log", os.TempDir(), false, "error")
}

func TestInitCommandList_Length(t *testing.T) {
	initCommandList()
	if len(commandList) != 4 {
		t.Fatalf("commandList len = %d; want 4", len(commandList))
	}
}

func TestInitCommandList_GetCommand(t *testing.T) {
	initCommandList()
	if commandList[0] == "" {
		t.Error("commandList[0] (GET) is empty")
	}
}

func TestInitCommandList_SubscribeCommand(t *testing.T) {
	initCommandList()
	if commandList[1] == "" {
		t.Error("commandList[1] (SUBSCRIBE) is empty")
	}
}

func TestInitCommandList_UnsubscribeCommand(t *testing.T) {
	initCommandList()
	if commandList[2] == "" {
		t.Error("commandList[2] (UNSUBSCRIBE) is empty")
	}
}

func TestInitCommandList_SetCommand(t *testing.T) {
	initCommandList()
	if commandList[3] == "" {
		t.Error("commandList[3] (SET) is empty")
	}
}

// Integration-only entry points — NOT unit-tested here:
//
//   noStreamCall  — dials a real gRPC server on :8887
//   streamCall    — dials a real gRPC server on :8887 and streams
//   main          — interactive TTY loop requiring a live server
