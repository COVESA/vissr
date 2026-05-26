/**
* (C) 2026 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE
* file in this repository.
*
* ----------------------------------------------------------------------------
*
* Coverage tests for evicSim. The evicSim binary simulates a CAN-driver
* peer; most of the file is goroutine / websocket / file I/O and is
* exercised by runtest.sh integration. The remaining testable helpers
* are convertToDomainData, fileExists, and deSerializeUInt.
**/
package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/covesa/vissr/utils"
)

func TestMain(m *testing.M) {
	utils.InitLog("evicSim-test.log", os.TempDir(), false, "error")
	os.Exit(m.Run())
}

// TestConvertToDomainData_EvicSim parses {"path","value"} JSON into
// DomainData (mirror of the helper in evicFeeder).
func TestConvertToDomainData_EvicSim(t *testing.T) {
	got := convertToDomainData(`{"path":"Vehicle.Speed","value":"42"}`)
	if got.Name != "Vehicle.Speed" || got.Value != "42" {
		t.Fatalf("convertToDomainData = %+v; want {Vehicle.Speed, 42}", got)
	}
}

func TestConvertToDomainData_EvicSim_Malformed(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("convertToDomainData panicked on malformed input: %v", r)
		}
	}()
	_ = convertToDomainData("not json")
}

// TestFileExists_EvicSim covers the path-exists check.
func TestFileExists_EvicSim(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x")
	if err := os.WriteFile(p, []byte("y"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if !fileExists(p) {
		t.Fatalf("fileExists returned false on existing file")
	}
	if fileExists(filepath.Join(dir, "absent")) {
		t.Fatalf("fileExists returned true on missing file")
	}
}

// TestDeSerializeUInt_EvicSim covers the 1/2/4-byte deserialization.
func TestDeSerializeUInt_EvicSim(t *testing.T) {
	if v, ok := deSerializeUInt([]byte{0x42}).(uint8); !ok || v != 0x42 {
		t.Fatalf("uint8 case failed")
	}
	if v, ok := deSerializeUInt([]byte{0x01, 0x02}).(uint16); !ok || v != 0x0201 {
		t.Fatalf("uint16 case failed")
	}
	if v, ok := deSerializeUInt([]byte{0x01, 0x02, 0x03, 0x04}).(uint32); !ok || v != 0x04030201 {
		t.Fatalf("uint32 case failed")
	}
	if got := deSerializeUInt([]byte{0x01, 0x02, 0x03}); got != nil {
		t.Fatalf("unsupported size: got %v; want nil", got)
	}
}

// TODO(testing): the websocket-driven entry points
// (initVehicleInterfaceMgr, initCanDriverOutput, initCanDriverInput,
// canDriverClient, makeServerHandler, serverSession, reDialer) need
// either a mock websocket or a refactor to expose the per-message
// handler. main is an entry point.
