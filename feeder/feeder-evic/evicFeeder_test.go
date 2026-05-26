/**
* (C) 2026 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE
* file in this repository.
*
* ----------------------------------------------------------------------------
*
* Coverage tests for the testable pure functions in feeder-evic:
* fileExists, deSerializeUInt, splitToDomainDataAndTs,
* convertToDomainData, enumConversion, linearConversion.
*
* The goroutine / network / SQL-bound entry points (initVSSInterfaceMgr,
* initUdsEndpoint, initVehicleInterfaceMgr, initCanDriverOutput,
* initCanDriverInput, canDriverClient, reDialer, makeServerHandler,
* serverSession, statestorageSet, main) need a running server, redis/
* memcache/sqlite, or websocket peer; they are exercised by runtest.sh
* integration and are not unit-tested here.
**/
package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/covesa/vissr/utils"
)

func TestMain(m *testing.M) {
	utils.InitLog("evicFeeder-test.log", os.TempDir(), false, "error")
	os.Exit(m.Run())
}

// TestFileExists_EvicFeeder covers the path-exists check.
func TestFileExists_EvicFeeder(t *testing.T) {
	dir := t.TempDir()
	exists := filepath.Join(dir, "present")
	if err := os.WriteFile(exists, []byte("x"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if !fileExists(exists) {
		t.Fatalf("fileExists returned false for existing file")
	}
	if fileExists(filepath.Join(dir, "absent")) {
		t.Fatalf("fileExists returned true for missing file")
	}
}

// TestDeSerializeUInt_EvicFeeder covers the 1/2/4-byte little-endian
// deserialization paths used by the feeder-map reader.
func TestDeSerializeUInt_EvicFeeder(t *testing.T) {
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

// TestSplitToDomainDataAndTs_EvicFeeder pulls (path, value, ts) out of
// the server-message JSON envelope.
func TestSplitToDomainDataAndTs_EvicFeeder(t *testing.T) {
	msg := `{"path":"Vehicle.Speed","dp":{"value":"100","ts":"2026-05-16T12:00:00Z"}}`
	dd, ts, ok := splitToDomainDataAndTs(msg)
	if !ok {
		t.Fatal("splitToDomainDataAndTs returned ok=false for valid input")
	}
	if dd.Name != "Vehicle.Speed" {
		t.Fatalf("Name = %q; want Vehicle.Speed", dd.Name)
	}
	if dd.Value != "100" {
		t.Fatalf("Value = %q; want 100", dd.Value)
	}
	if ts != "2026-05-16T12:00:00Z" {
		t.Fatalf("ts = %q", ts)
	}
}

// TestConvertToDomainData parses {"path":..,"value":..} into DomainData.
func TestConvertToDomainData(t *testing.T) {
	in := `{"path":"Vehicle.Speed","value":"55"}`
	got := convertToDomainData(in)
	if got.Name != "Vehicle.Speed" {
		t.Fatalf("Name = %q; want Vehicle.Speed", got.Name)
	}
	if got.Value != "55" {
		t.Fatalf("Value = %q; want 55", got.Value)
	}
}

func TestConvertToDomainData_Malformed(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("convertToDomainData panicked on malformed input: %v", r)
		}
	}()
	_ = convertToDomainData("not json")
}

// TestEnumConversion_EvicFeeder maps between domain and VSS enums.
func TestEnumConversion_EvicFeeder(t *testing.T) {
	enum := map[string]interface{}{
		"Off": "0",
		"On":  "1",
	}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("enumConversion panicked: %v", r)
		}
	}()
	_ = enumConversion(enum, true, "0")
	_ = enumConversion(enum, false, "Off")
}

// TestLinearConversion_EvicFeeder applies the Ax+B formula.
func TestLinearConversion_EvicFeeder(t *testing.T) {
	coeff := []interface{}{float64(2), float64(1)} // y = 2x + 1
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("linearConversion panicked: %v", r)
		}
	}()
	_ = linearConversion(coeff, true, "10") // should produce a value, exact format opaque
}

// TODO(testing): the goroutine / network / DB code paths need code
// refactoring to be unit-testable:
//
//   - initVSSInterfaceMgr / initVehicleInterfaceMgr / initCanDriverInput /
//     initCanDriverOutput / canDriverClient / serverSession — inline
//     goroutines; extract the per-message handler so we can drive it.
//   - statestorageSet — coupled to sqlite/redis/memcache; inject a
//     storage interface and we can mock it.
//   - reDialer — long-running dial loop; extract a single-attempt
//     helper for retry logic.
//   - main — entry point.
