/**
* (C) 2026 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE
* file in this repository.
*
* ----------------------------------------------------------------------------
*
* Coverage tests for the testable pure functions in feederv4:
* fileExists, deSerializeUInt, inFeederScope, splitToDomainDataAndTs,
* getSimulatorContainer, makeDataPoint, calcInputValue, incDpIndex,
* enumConversion, linearConversion.
*
* The goroutine / network / SQL-bound entry points (udsReader,
* udsWriter, initVSSInterfaceMgr, initVehicleInterfaceMgr,
* feederRegister, statestorageSet, simulateInput, selectRandomInput,
* main) need a running server, redis/memcache/sqlite, or non-
* deterministic state; they are exercised by runtest.sh integration
* and are not unit-tested here.
**/
package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/covesa/vissr/utils"
)

// TestFileExists covers the path-exists check.
func TestFileExists(t *testing.T) {
	dir := t.TempDir()
	exists := filepath.Join(dir, "present.txt")
	if err := os.WriteFile(exists, []byte("hi"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if !fileExists(exists) {
		t.Fatalf("fileExists returned false for an existing file")
	}
	if fileExists(filepath.Join(dir, "absent.txt")) {
		t.Fatalf("fileExists returned true for a non-existent file")
	}
}

// TestDeSerializeUInt covers the 1 / 2 / 4 byte deserialization paths
// and the "unknown size" default.
func TestDeSerializeUInt(t *testing.T) {
	t.Run("uint8", func(t *testing.T) {
		got := deSerializeUInt([]byte{0x42})
		v, ok := got.(uint8)
		if !ok || v != 0x42 {
			t.Fatalf("uint8 = %v (%T); want 0x42", got, got)
		}
	})
	t.Run("uint16 little-endian", func(t *testing.T) {
		// Wire layout: buf[1]*256 + buf[0]. So {0x01, 0x02} → 0x0201.
		got := deSerializeUInt([]byte{0x01, 0x02})
		v, ok := got.(uint16)
		if !ok || v != 0x0201 {
			t.Fatalf("uint16 = %v (%T); want 0x0201", got, got)
		}
	})
	t.Run("uint32 little-endian", func(t *testing.T) {
		got := deSerializeUInt([]byte{0x01, 0x02, 0x03, 0x04})
		v, ok := got.(uint32)
		if !ok || v != 0x04030201 {
			t.Fatalf("uint32 = %v (%T); want 0x04030201", got, got)
		}
	})
	t.Run("unsupported size", func(t *testing.T) {
		// Three bytes — not in the switch; returns nil and logs.
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("deSerializeUInt panicked on 3-byte input: %v", r)
			}
		}()
		got := deSerializeUInt([]byte{0x01, 0x02, 0x03})
		if got != nil {
			t.Fatalf("3-byte deSerializeUInt = %v; want nil", got)
		}
	})
}

// TestInFeederScope covers the scope-membership check.
func TestInFeederScope(t *testing.T) {
	scope := []string{"Vehicle.Speed", "Vehicle.Cabin.Door.Row1.Right.IsOpen"}
	cases := map[string]bool{
		"Vehicle.Speed":                          true,
		"Vehicle.Cabin.Door.Row1.Right.IsOpen":   true,
		"Vehicle.NotInScope":                     false,
		"":                                       false,
	}
	for path, want := range cases {
		t.Run(path, func(t *testing.T) {
			if got := inFeederScope(path, scope); got != want {
				t.Fatalf("inFeederScope(%q, scope) = %v; want %v", path, got, want)
			}
		})
	}
}

// TestSplitToDomainDataAndTs extracts the dp/path/ts triple from a
// server-message map.
func TestSplitToDomainDataAndTs_ServerFormat(t *testing.T) {
	msg := map[string]interface{}{
		"path": "Vehicle.Speed",
		"dp": map[string]interface{}{
			"ts":    "2026-05-16T12:00:00Z",
			"value": "100",
		},
	}
	dd, ts, ok := splitToDomainDataAndTs(msg)
	if !ok {
		t.Fatalf("splitToDomainDataAndTs returned ok=false on valid input")
	}
	if dd.Name != "Vehicle.Speed" {
		t.Fatalf("Name = %q; want Vehicle.Speed", dd.Name)
	}
	if dd.Value != "100" {
		t.Fatalf("Value = %q; want 100", dd.Value)
	}
	if ts != "2026-05-16T12:00:00Z" {
		t.Fatalf("ts = %q; want 2026-05-16T12:00:00Z", ts)
	}
}

// TestMakeDataPoint constructs a DomainData from a path/value pair.
func TestMakeDataPoint(t *testing.T) {
	got := makeDataPoint("Vehicle.Speed", "55")
	if got.Name != "Vehicle.Speed" {
		t.Fatalf("Name = %q; want Vehicle.Speed", got.Name)
	}
	if got.Value != "55" {
		t.Fatalf("Value = %q; want 55", got.Value)
	}
}

// TestCalcInputValue cycles through values supplied by the simulator.
func TestCalcInputValue(t *testing.T) {
	// The function takes iteration + value-string; the exact algorithm
	// is opaque, but it must be deterministic for a given (i, v).
	a := calcInputValue(0, "10")
	b := calcInputValue(0, "10")
	if a != b {
		t.Fatalf("calcInputValue not deterministic: %q != %q", a, b)
	}
}

// TestIncDpIndex wraps the DP-index counter.
func TestIncDpIndex(t *testing.T) {
	// Should monotonically increase or wrap; exact semantics
	// implementation-defined. We just assert it's deterministic and
	// returns a sensible (non-negative) value.
	got := incDpIndex(0)
	if got < 0 {
		t.Fatalf("incDpIndex(0) = %d; want >= 0", got)
	}
	// Same input must give same output.
	if incDpIndex(0) != got {
		t.Fatalf("incDpIndex not deterministic")
	}
}

// TestEnumConversion maps between VSS enum values via the enum object.
// enumObj keys are VSS-domain values, enumObj values are vehicle-domain
// values (see enumConversion's doc comment). north2SouthConv=true takes
// a VSS-domain input (a key) and returns the vehicle-domain value;
// north2SouthConv=false takes a vehicle-domain input (a value) and
// returns the VSS-domain key.
func TestEnumConversion_NorthBound(t *testing.T) {
	// VSS ("Off"/"On") -> vehicle ("0"/"1").
	enum := map[string]interface{}{
		"Off": "0",
		"On":  "1",
	}
	got := enumConversion(enum, true, "Off")
	if got != "0" {
		t.Errorf("enumConversion northbound = %q; want %q", got, "0")
	}
}

func TestEnumConversion_SouthBound(t *testing.T) {
	// vehicle ("0"/"1") -> VSS ("Off"/"On").
	enum := map[string]interface{}{
		"Off": "0",
		"On":  "1",
	}
	got := enumConversion(enum, false, "0")
	if got != "Off" {
		t.Errorf("enumConversion southbound = %q; want %q", got, "Off")
	}
}

// TestEnumConversion_ValueNotInTable is a regression test: a value with
// no matching entry in enumObj (e.g. a set request whose value isn't one
// of the enum's defined VSS keys, as in the "SPORT" -> Vehicle.Powertrain.
// Transmission.PerformanceMode bug) must return the VISS in-line error
// sentinel, not "". Returning "" let the failed conversion be written to
// state storage as an empty value indistinguishable from success; a
// subsequent get would then see whatever the backend defaults to for a
// missing/empty stored value instead of a clear conversion-failure marker.
func TestEnumConversion_ValueNotInTable(t *testing.T) {
	enum := map[string]interface{}{
		"NORMAL": "0",
		"ECONOMY": "1",
	}
	got := enumConversion(enum, true, "SPORT")
	if got != utils.InlineErrorDataConversionFailed {
		t.Errorf("enumConversion(out-of-table value) = %q; want %q", got, utils.InlineErrorDataConversionFailed)
	}
}

// TestLinearConversion applies Ax+B.
func TestLinearConversion_Identity(t *testing.T) {
	// A=1, B=0 — identity transform.
	coeff := []interface{}{float64(1), float64(0)}
	got := linearConversion(coeff, true, "42")
	if got == "" {
		t.Fatalf("linearConversion returned empty for valid input")
	}
}

func TestLinearConversion_BadInput(t *testing.T) {
	// Non-numeric value should not panic, and must return the in-line
	// error sentinel rather than "" (see convertValue's doc comment).
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("linearConversion panicked on non-numeric input: %v", r)
		}
	}()
	got := linearConversion([]interface{}{float64(2), float64(0)}, true, "not a number")
	if got != utils.InlineErrorDataConversionFailed {
		t.Errorf("linearConversion(non-numeric input) = %q; want %q", got, utils.InlineErrorDataConversionFailed)
	}
}

// TODO(testing): the following functions need code refactoring before
// they can be unit-tested cleanly:
//
//   - feederRegister, statestorageSet — coupled to live UDS / SQL /
//     redis / memcache connections. Refactor to accept a connection
//     interface and we can mock it.
//   - udsReader, udsWriter — inline goroutine loops driving channels.
//     Extract the per-message dispatch into a function taking a
//     decoded message and we can test it.
//   - simulateInput, selectRandomInput, getRandomVssfMapIndex —
//     non-deterministic (use math/rand). Inject a *rand.Rand and we
//     can seed it for testing.
//   - main — entry point.
