/**
* (C) 2026 Matt Jones
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE file in this repository.
*
* Unit tests for the RemotiveLabs feeder's pure helpers: message
* (de)composition, the feeder-map reader, domain mapping/conversion, the
* simulation value helpers, and channel-data formatting. The redis / UDS /
* RemotiveLabs-broker / interface-manager goroutines and main() are
* integration-only.
**/
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/covesa/vissr/utils"
)

func TestMain(m *testing.M) {
	utils.InitLog("feeder-rl-test.log", os.TempDir(), false, "error")
	os.Exit(m.Run())
}

func TestMapString(t *testing.T) {
	m := map[string]interface{}{"s": "v", "n": nil, "i": 7}
	if v, ok := mapString(m, "s"); !ok || v != "v" {
		t.Errorf("mapString(s) = %q,%v; want v,true", v, ok)
	}
	for _, k := range []string{"absent", "n", "i"} {
		if _, ok := mapString(m, k); ok {
			t.Errorf("mapString(%s) ok=true; want false", k)
		}
	}
}

func TestMarshalDatapointJSON(t *testing.T) {
	got, err := marshalDatapointJSON(`5"6`, "T")
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	// encoding/json escapes the embedded quote so the result stays valid JSON.
	if !strings.Contains(got, `\"`) || !strings.Contains(got, `"ts":"T"`) {
		t.Errorf("marshalDatapointJSON = %q; want escaped value + ts", got)
	}
}

func TestReadFeederMap(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "map.json")
	os.WriteFile(p, []byte(`[{"vssdata":"Vehicle.Speed","vehicledata":"Spd"}]`), 0644)
	fm := readFeederMap(p)
	if len(fm) != 1 || fm[0].VssName != "Vehicle.Speed" || fm[0].VehicleName != "Spd" {
		t.Fatalf("readFeederMap = %+v; want one Vehicle.Speed/Spd entry", fm)
	}

	empty := filepath.Join(dir, "empty.json")
	os.WriteFile(empty, []byte(`[]`), 0644)
	if got := readFeederMap(empty); len(got) != 0 {
		t.Errorf("empty map = %+v; want 0 entries (no panic)", got)
	}
	if readFeederMap(filepath.Join(dir, "missing.json")) != nil {
		t.Error("missing file should return nil")
	}
	bad := filepath.Join(dir, "bad.json")
	os.WriteFile(bad, []byte(`not json`), 0644)
	if readFeederMap(bad) != nil {
		t.Error("bad json should return nil")
	}
}

func TestSplitToDomainDataAndTs(t *testing.T) {
	d, ts, ok := splitToDomainDataAndTs(`{"path":"X","dp":{"value":"5","ts":"T"}}`)
	if !ok || d.Name != "X" || d.Value != "5" || ts != "T" {
		t.Fatalf("got %+v,%q,%v; want X/5,T,true", d, ts, ok)
	}
	for _, bad := range []string{
		`not json`,
		`{"dp":{"value":"5","ts":"T"}}`,   // missing path
		`{"path":"X"}`,                    // missing dp
		`{"path":"X","dp":{"ts":"T"}}`,    // missing value
		`{"path":"X","dp":{"value":"5"}}`, // missing ts
	} {
		if _, _, ok := splitToDomainDataAndTs(bad); ok {
			t.Errorf("splitToDomainDataAndTs(%s) ok=true; want false", bad)
		}
	}
}

func TestCalcInputValue(t *testing.T) {
	if got := calcInputValue(0, "100"); got != "90" { // 100 - 10 + 0
		t.Errorf("calcInputValue(0,100) = %q; want 90", got)
	}
	if got := calcInputValue(5, "100"); got != "95" { // 100 - 10 + 5
		t.Errorf("calcInputValue(5,100) = %q; want 95", got)
	}
	// Non-integer setValue logs an error and treats setVal as 0.
	if got := calcInputValue(0, "xx"); got != "-10" {
		t.Errorf("calcInputValue(0,xx) = %q; want -10", got)
	}
}

func TestSelectRandomInput(t *testing.T) {
	if d := selectRandomInput(nil); d.Name != "" {
		t.Errorf("selectRandomInput(nil) = %+v; want empty (no rand.Intn(0) panic)", d)
	}
	fm := []FeederMap{{VssName: "Vehicle.Speed", VehicleName: "Spd"}}
	d := selectRandomInput(fm)
	if d.Name != "Spd" {
		t.Errorf("selectRandomInput = %+v; want VehicleName Spd", d)
	}
}

func TestSearchMapAndConvert(t *testing.T) {
	fm := []FeederMap{{VssName: "Vehicle.Speed", VehicleName: "Spd"}}
	if got := searchMap(fm, "VSS", "Vehicle.Speed"); got != "Spd" {
		t.Errorf("searchMap VSS = %q; want Spd", got)
	}
	if got := searchMap(fm, "Vehicle", "Spd"); got != "Vehicle.Speed" {
		t.Errorf("searchMap Vehicle = %q; want Vehicle.Speed", got)
	}
	if got := searchMap(fm, "VSS", "nope"); got != "" {
		t.Errorf("searchMap miss = %q; want empty", got)
	}

	out := convertDomainData("VSS", DomainData{Name: "Vehicle.Speed", Value: "5"}, fm)
	if out.Name != "Spd" || out.Value != "5" {
		t.Errorf("convertDomainData = %+v; want Spd/5", out)
	}
	// Unmapped name returns the input unchanged (not an empty-name datapoint).
	in := DomainData{Name: "Unmapped", Value: "9"}
	if out := convertDomainData("VSS", in, fm); out != in {
		t.Errorf("convertDomainData(unmapped) = %+v; want the input unchanged", out)
	}
	if got := convertValue("passthru"); got != "passthru" {
		t.Errorf("convertValue = %q; want passthru", got)
	}
}

func TestCovertChannelDataToString(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{float64(3.5), "3.5"},
		{int64(42), "42"},
		{true, "true"},
		{[]byte("bytes"), "bytes"},
		{struct{}{}, ""}, // unknown type
	}
	for _, c := range cases {
		if got := covertChannelDataToString(c.in); got != c.want {
			t.Errorf("covertChannelDataToString(%v) = %q; want %q", c.in, got, c.want)
		}
	}
}
