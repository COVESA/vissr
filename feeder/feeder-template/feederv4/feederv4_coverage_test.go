/**
* (C) 2026 Matt Jones
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE file in this repository.
*
* Coverage for feederv4's file readers (scaling list, config, binary feeder
* map), the simulation helpers, notification-list mutation, and the
* domain-conversion logic. The redis/memcache/sqlite state-storage, the UDS and
* websocket plumbing, and main() remain integration-only.
**/
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// feederCvtElement encodes one FeederMap entry in the byte layout readElement
// expects (matching the Domain Conversion Tool's writeElement).
func feederCvtElement(mapIndex uint16, name string, typ, datatype uint8, convIdx uint16) []byte {
	b := []byte{byte(mapIndex & 0xff), byte(mapIndex >> 8)}
	b = append(b, byte(len(name)))
	b = append(b, []byte(name)...)
	b = append(b, typ, datatype)
	b = append(b, byte(convIdx&0xff), byte(convIdx>>8))
	return b
}

func TestReadscalingDataListV4(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "scl.json")
	os.WriteFile(p, []byte(`["{\"LOW\":\"0\"}","[2, 1]"]`), 0644)
	if got := readscalingDataList(p); len(got) != 2 {
		t.Fatalf("readscalingDataList len=%d; want 2", len(got))
	}
	if readscalingDataList(filepath.Join(dir, "missing.json")) != nil {
		t.Error("missing file should return nil")
	}
	bad := filepath.Join(dir, "bad.json")
	os.WriteFile(bad, []byte(`nope`), 0644)
	if readscalingDataList(bad) != nil {
		t.Error("bad json should return nil")
	}
}

func TestReadFeederConfig(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "cfg.json")
	os.WriteFile(p, []byte(`{"name":"Vehicle","scope":["Vehicle.Speed"]}`), 0644)
	cfg := readFeederConfig(p)
	if cfg.Name != "Vehicle" || cfg.InfoType != "Data" || len(cfg.Scope) != 1 {
		t.Fatalf("readFeederConfig = %+v; want Name Vehicle, InfoType Data, 1 scope", cfg)
	}
	if cfg := readFeederConfig(filepath.Join(dir, "missing.json")); cfg.InfoType != "error" {
		t.Errorf("missing config InfoType=%q; want error", cfg.InfoType)
	}
	bad := filepath.Join(dir, "bad.json")
	os.WriteFile(bad, []byte(`nope`), 0644)
	if cfg := readFeederConfig(bad); cfg.InfoType != "error" {
		t.Errorf("bad config InfoType=%q; want error", cfg.InfoType)
	}
}

func TestReadFeederMapV4(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "map.cvt")
	var data []byte
	data = append(data, feederCvtElement(1, "Vehicle.Speed", 0, 1, 0)...)
	data = append(data, feederCvtElement(0, "Can.Spd", 0, 1, 0)...)
	os.WriteFile(p, data, 0644)
	fm := readFeederMap(p)
	if len(fm) != 2 || fm[0].Name != "Can.Spd" || fm[1].Name != "Vehicle.Speed" {
		t.Fatalf("readFeederMap = %+v; want two sorted entries", fm)
	}
	if readFeederMap(filepath.Join(dir, "missing.cvt")) != nil {
		t.Error("missing map file should return nil")
	}
	tp := filepath.Join(dir, "trunc.cvt")
	os.WriteFile(tp, []byte{0x01}, 0644)
	if got := readFeederMap(tp); len(got) != 0 {
		t.Errorf("truncated file = %+v; want empty", got)
	}
}

func TestMarshalDatapointJSONV4(t *testing.T) {
	got, err := marshalDatapointJSON(`5"6`, "T")
	if err != nil || !strings.Contains(got, `"ts":"T"`) {
		t.Fatalf("marshalDatapointJSON = %q, err=%v", got, err)
	}
}

func TestRemoveNotifications(t *testing.T) {
	notificationList = nil
	defer func() { notificationList = nil }()
	addNotifications([]interface{}{"A", "B", "C", 7 /*non-string skipped*/})
	if len(notificationList) != 3 {
		t.Fatalf("addNotifications -> %v; want [A B C]", notificationList)
	}
	removeNotifications([]interface{}{"B", "missing", 9})
	if onNotificationList("B") != -1 || onNotificationList("A") == -1 || onNotificationList("C") == -1 {
		t.Fatalf("removeNotifications result=%v; want B gone, A and C kept", notificationList)
	}
}

func TestGetSimulatorContainer(t *testing.T) {
	ctx := []ActuatorSimCtx{{Path: "a", RemainingSteps: 3}, {RemainingSteps: 0}}
	if i := getSimulatorContainer(ctx, "a"); i != 0 {
		t.Errorf("existing path -> %d; want 0", i)
	}
	if i := getSimulatorContainer(ctx, "new"); i != 1 {
		t.Errorf("free slot -> %d; want 1", i)
	}
	full := []ActuatorSimCtx{{Path: "a", RemainingSteps: 3}}
	if i := getSimulatorContainer(full, "new"); i != -1 {
		t.Errorf("full -> %d; want -1", i)
	}
}

func TestCalculateSimValue(t *testing.T) {
	intCtx := &ActuatorSimCtx{RemainingSteps: 2, CurrVal: "0", EndVal: "10"}
	if got := calculateSimValue(intCtx); got != "5" { // step (10-0)/2
		t.Errorf("int step = %q; want 5", got)
	}
	floatCtx := &ActuatorSimCtx{RemainingSteps: 2, CurrVal: "0", EndVal: "10.5"}
	if got := calculateSimValue(floatCtx); got == "" {
		t.Errorf("float step returned empty")
	}
	badCtx := &ActuatorSimCtx{RemainingSteps: 2, CurrVal: "0", EndVal: "abc"}
	if got := calculateSimValue(badCtx); got != "abc" || badCtx.RemainingSteps != 0 {
		t.Errorf("bad endval = %q, steps=%d; want abc/0", got, badCtx.RemainingSteps)
	}
}

func TestSelectRandomInputAndIndex(t *testing.T) {
	if i := getRandomVssfMapIndex(nil); i != -1 {
		t.Errorf("empty map index = %d; want -1", i)
	}
	if i := getRandomVssfMapIndex([]FeederMap{{Name: "a.b"}}); i != -1 {
		t.Errorf("all-dotted index = %d; want -1", i)
	}
	leaf := []FeederMap{{Name: "Speed", Datatype: 0}}
	if i := getRandomVssfMapIndex(leaf); i != 0 {
		t.Errorf("leaf index = %d; want 0", i)
	}
	if d := selectRandomInput(nil); d.Name != "" {
		t.Errorf("selectRandomInput(nil) = %+v; want empty", d)
	}
	if d := selectRandomInput(leaf); d.Name != "Speed" {
		t.Errorf("selectRandomInput = %+v; want Name Speed", d)
	}
}

func TestSimulatedDataLifecycle(t *testing.T) {
	tripData = nil
	defer func() { tripData = nil }()
	dir := t.TempDir()
	p := filepath.Join(dir, "trip.json")
	os.WriteFile(p, []byte(`[{"path":"X","dp":[{"ts":"t1","value":"1"},{"ts":"t2","value":"2"}]}]`), 0644)

	if got := readSimulatedData(p); len(got) != 1 {
		t.Fatalf("readSimulatedData len=%d; want 1", len(got))
	}
	if dps := getSimulatedDataPoints(0); len(dps) != 1 || dps[0].Value != "1" {
		t.Fatalf("getSimulatedDataPoints(0) = %+v; want X/1", dps)
	}
	if dps := getSimulatedDataPoints(5); len(dps) != 0 { // out of range -> skipped
		t.Errorf("getSimulatedDataPoints(5) = %+v; want empty", dps)
	}
	if incDpIndex(0) != 1 || incDpIndex(1) != 0 { // wraps at len 2
		t.Errorf("incDpIndex wrap incorrect")
	}

	if readSimulatedData(filepath.Join(dir, "missing.json")) != nil {
		t.Error("missing trip file should return nil")
	}
}

func TestConvertDomainDataV4(t *testing.T) {
	scalingDataList = nil
	defer func() { scalingDataList = nil }()
	fm := []FeederMap{
		{Name: "a", MapIndex: 1, ConvertIndex: 0, Datatype: 0},
		{Name: "b", MapIndex: 0, ConvertIndex: 0, Datatype: 0},
	}
	if out := convertDomainData(true, DomainData{Name: "a", Value: "5"}, fm); out.Name != "b" || out.Value != "5" {
		t.Errorf("convertDomainData = %+v; want b/5", out)
	}
	// Unmapped name -> input returned unchanged.
	in := DomainData{Name: "zzz", Value: "1"}
	if out := convertDomainData(true, in, fm); out != in {
		t.Errorf("unmapped = %+v; want input unchanged", out)
	}
	// Empty map -> input.
	if out := convertDomainData(true, in, nil); out != in {
		t.Errorf("empty map = %+v; want input", out)
	}
	// MapIndex out of range -> input.
	oob := []FeederMap{{Name: "a", MapIndex: 99}}
	if out := convertDomainData(true, DomainData{Name: "a"}, oob); out.Name != "a" {
		t.Errorf("oob MapIndex = %+v; want input", out)
	}
}

func TestConvertValueV4(t *testing.T) {
	if got := convertValue("9", 0, 0, 0, true); got != "9" {
		t.Errorf("no-conv = %q; want 9", got)
	}
	scalingDataList = []string{`{"LOW":"0"}`, `[2, 1]`, `bad`}
	defer func() { scalingDataList = nil }()
	if got := convertValue("LOW", 1, 0, 0, true); got != "0" {
		t.Errorf("enum = %q; want 0", got)
	}
	if got := convertValue("3", 2, 0, 0, true); got != "7" { // 2*3+1
		t.Errorf("linear = %q; want 7", got)
	}
	if got := convertValue("1", 99, 0, 0, true); got != "" {
		t.Errorf("oob index = %q; want empty", got)
	}
	if got := convertValue("1", 3, 0, 0, true); got != "" { // bad json entry
		t.Errorf("bad json = %q; want empty", got)
	}
}
