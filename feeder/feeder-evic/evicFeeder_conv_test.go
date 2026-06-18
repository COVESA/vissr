/**
* (C) 2026 Matt Jones
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE file in this repository.
*
* Unit tests for the EVIC feeder's pure conversion logic, message
* (de)composition, and the binary domain-conversion file readers. The socket /
* UDS / CAN / redis plumbing and main() remain integration-only.
**/
package main

import (
	"os"
	"path/filepath"
	"testing"
)

// cvtElement encodes one FeederMap entry in the exact byte layout the Domain
// Conversion Tool's writeElement emits (and readElement expects).
func cvtElement(mapIndex uint16, name string, typ, datatype uint8, convIdx uint16) []byte {
	b := []byte{byte(mapIndex & 0xff), byte(mapIndex >> 8)}
	b = append(b, byte(len(name)))
	b = append(b, []byte(name)...)
	b = append(b, typ, datatype)
	b = append(b, byte(convIdx&0xff), byte(convIdx>>8))
	return b
}

// ── mapString ─────────────────────────────────────────────────────────────────

func TestMapString(t *testing.T) {
	m := map[string]interface{}{"s": "v", "n": nil, "i": 7}
	if v, ok := mapString(m, "s"); !ok || v != "v" {
		t.Errorf("mapString(s) = %q,%v; want v,true", v, ok)
	}
	if _, ok := mapString(m, "absent"); ok {
		t.Error("mapString(absent) ok=true; want false")
	}
	if _, ok := mapString(m, "n"); ok {
		t.Error("mapString(nil) ok=true; want false")
	}
	if _, ok := mapString(m, "i"); ok {
		t.Error("mapString(int) ok=true; want false")
	}
}

// ── convertValue / enum / linear ──────────────────────────────────────────────

func TestConvertValue_NoConversion(t *testing.T) {
	if got := convertValue("42", 0, 0, 0, true); got != "42" {
		t.Errorf("no-conversion got %q; want 42", got)
	}
}

func TestConvertValue_Enum(t *testing.T) {
	scalingDataList = []string{`{"LOW":"0","HIGH":"1"}`}
	defer func() { scalingDataList = nil }()
	if got := convertValue("LOW", 1, 0, 0, true); got != "0" {
		t.Errorf("north2south enum got %q; want 0", got)
	}
	if got := convertValue("1", 1, 0, 0, false); got != "HIGH" {
		t.Errorf("south2north enum got %q; want HIGH", got)
	}
	if got := convertValue("NOPE", 1, 0, 0, true); got != "" {
		t.Errorf("enum out-of-range got %q; want empty", got)
	}
}

func TestConvertValue_Linear(t *testing.T) {
	scalingDataList = []string{`[2, 1]`} // y = 2x + 1
	defer func() { scalingDataList = nil }()
	if got := convertValue("3", 1, 0, 0, true); got != "7" {
		t.Errorf("north2south linear got %q; want 7", got)
	}
	if got := convertValue("7", 1, 0, 0, false); got != "3" {
		t.Errorf("south2north linear got %q; want 3", got)
	}
}

func TestConvertValue_Errors(t *testing.T) {
	scalingDataList = []string{`not json`, `[0, 5]`}
	defer func() { scalingDataList = nil }()
	if got := convertValue("1", 99, 0, 0, true); got != "" {
		t.Errorf("out-of-range index got %q; want empty", got)
	}
	if got := convertValue("1", 1, 0, 0, true); got != "" { // bad json entry
		t.Errorf("bad json got %q; want empty", got)
	}
	if got := convertValue("1", 2, 0, 0, false); got != "" { // A=0, south2north divide-by-zero
		t.Errorf("divide-by-zero got %q; want empty", got)
	}
}

func TestLinearConversion_BadInputs(t *testing.T) {
	if got := linearConversion([]interface{}{1.0}, true, "5"); got != "" {
		t.Errorf("short coeff array got %q; want empty", got)
	}
	if got := linearConversion([]interface{}{2.0, 1.0}, true, "notnum"); got != "" {
		t.Errorf("non-numeric input got %q; want empty", got)
	}
	if got := linearConversion([]interface{}{"x", "y"}, true, "5"); got != "" {
		t.Errorf("non-numeric coeffs got %q; want empty", got)
	}
}

// ── lookupFeederMap / convertDomainData ───────────────────────────────────────

func TestLookupFeederMap(t *testing.T) {
	if _, ok := lookupFeederMap("x", nil); ok {
		t.Error("empty map should miss")
	}
	fm := []FeederMap{{Name: "a"}, {Name: "b"}, {Name: "c"}}
	if i, ok := lookupFeederMap("b", fm); !ok || i != 1 {
		t.Errorf("lookup b = %d,%v; want 1,true", i, ok)
	}
	if _, ok := lookupFeederMap("z", fm); ok {
		t.Error("lookup z should miss")
	}
}

func TestConvertDomainData(t *testing.T) {
	// a -> b (MapIndex 1 points at entry "b"); no conversion (ConvertIndex 0).
	fm := []FeederMap{
		{Name: "a", MapIndex: 1, ConvertIndex: 0, Datatype: 0},
		{Name: "b", MapIndex: 0, ConvertIndex: 0, Datatype: 0},
	}
	out := convertDomainData(true, DomainData{Name: "a", Value: "5"}, fm)
	if out.Name != "b" || out.Value != "5" {
		t.Errorf("convertDomainData = %+v; want b/5", out)
	}
	// name not in map.
	if out := convertDomainData(true, DomainData{Name: "zzz"}, fm); out.Name != "" {
		t.Errorf("missing name = %+v; want empty", out)
	}
	// MapIndex out of range.
	bad := []FeederMap{{Name: "a", MapIndex: 99}}
	if out := convertDomainData(true, DomainData{Name: "a"}, bad); out.Name != "" {
		t.Errorf("bad MapIndex = %+v; want empty", out)
	}
}

// ── binary file readers ───────────────────────────────────────────────────────

func TestReadFeederMap(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "map.cvt")
	var data []byte
	data = append(data, cvtElement(1, "Vehicle.Speed", 0, 1, 0)...)
	data = append(data, cvtElement(0, "Can.Spd", 0, 1, 0)...)
	if err := os.WriteFile(p, data, 0644); err != nil {
		t.Fatal(err)
	}
	fm := readFeederMap(p)
	if len(fm) != 2 {
		t.Fatalf("readFeederMap len=%d; want 2", len(fm))
	}
	// sorted by Name: Can.Spd before Vehicle.Speed.
	if fm[0].Name != "Can.Spd" || fm[1].Name != "Vehicle.Speed" {
		t.Fatalf("readFeederMap names = %q,%q; want sorted", fm[0].Name, fm[1].Name)
	}
	if fm[1].MapIndex != 1 || fm[1].Datatype != 1 {
		t.Errorf("Vehicle.Speed entry = %+v; want MapIndex 1, Datatype 1", fm[1])
	}

	if readFeederMap(filepath.Join(dir, "missing.cvt")) != nil {
		t.Error("missing map file should return nil")
	}

	// Truncated element (only a partial header) -> readElement stops cleanly.
	tp := filepath.Join(dir, "trunc.cvt")
	os.WriteFile(tp, []byte{0x01}, 0644)
	if got := readFeederMap(tp); len(got) != 0 {
		t.Errorf("truncated file = %+v; want empty", got)
	}
}

func TestReadscalingDataList(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "scl.json")
	os.WriteFile(p, []byte(`["{\"LOW\":\"0\"}","[2, 1]"]`), 0644)
	got := readscalingDataList(p)
	if len(got) != 2 {
		t.Fatalf("readscalingDataList len=%d; want 2", len(got))
	}
	if readscalingDataList(filepath.Join(dir, "missing.json")) != nil {
		t.Error("missing scaling file should return nil")
	}
	bad := filepath.Join(dir, "bad.json")
	os.WriteFile(bad, []byte(`not json`), 0644)
	if readscalingDataList(bad) != nil {
		t.Error("bad json scaling file should return nil")
	}
}
