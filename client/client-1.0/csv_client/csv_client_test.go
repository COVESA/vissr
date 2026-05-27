/**
* (C) 2026 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE
* file in this repository.
*
* ----------------------------------------------------------------------------
*
* Coverage tests for the testable pure functions in csv_client:
* pathToUrl, jsonToStructList, retrieveRequest, createListFromFile,
* the processDataLevel1..5 walkers, storeinArrays, saveInCsv.
*
* The network-driven entry points (initVissV2WebSocket, getResponse,
* performCommand, performNoneCommand, performPbCommand, main, the
* interactive displayOptions / readOption) need a running server or
* a TTY; they are exercised by runtest.sh integration.
**/
package main

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"testing"

	"github.com/covesa/vissr/utils"
)

func init() {
	utils.InitLog("csv_client-test.log", os.TempDir(), false, "error")
}

func TestPathToUrl_CsvClient(t *testing.T) {
	cases := map[string]string{
		"Vehicle":            "/Vehicle",
		"Vehicle.Speed":      "/Vehicle/Speed",
		"":                   "/",
	}
	for in, want := range cases {
		if got := pathToUrl(in); got != want {
			t.Fatalf("pathToUrl(%q) = %q; want %q", in, got, want)
		}
	}
}

func TestJsonToStructList_CsvClient(t *testing.T) {
	if got := jsonToStructList(`{"request": [{"action":"get","path":"X"}]}`); got != 1 {
		t.Fatalf("valid input = %d; want 1", got)
	}
	if got := jsonToStructList(`not json`); got != 0 {
		t.Fatalf("garbage = %d; want 0", got)
	}
}

func TestRetrieveRequest_CsvClient(t *testing.T) {
	in := map[string]interface{}{"action": "get", "path": "Vehicle.Speed"}
	got := retrieveRequest(in)
	if got == "" {
		t.Fatalf("retrieveRequest returned empty for valid input")
	}
}

// TestCreateListFromFile_CsvClient reads a JSON file and parses it.
func TestCreateListFromFile_CsvClient(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "in.json")
	if err := os.WriteFile(path, []byte(`{"request":[{"action":"get","path":"X"}]}`), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if got := createListFromFile(path); got != 1 {
		t.Fatalf("createListFromFile = %d; want 1", got)
	}
}

// TestProcessDataLevel5 — innermost dp object: extracts ts and value
// into the arrays at the given index.
func TestProcessDataLevel5(t *testing.T) {
	val := make([]string, 4)
	ts := make([]string, 4)
	dp := map[string]interface{}{
		"value": "100",
		"ts":    "2026-05-16T12:00:00Z",
	}
	idx := processDataLevel5(dp, &val, &ts, 0)
	if idx != 1 {
		t.Fatalf("processDataLevel5 returned %d; want 1", idx)
	}
	if val[0] != "100" || ts[0] != "2026-05-16T12:00:00Z" {
		t.Fatalf("data not stored: val=%v ts=%v", val, ts)
	}
}

// TestProcessDataLevel4 — array of dps.
func TestProcessDataLevel4(t *testing.T) {
	val := make([]string, 4)
	ts := make([]string, 4)
	arr := []interface{}{
		map[string]interface{}{"value": "1", "ts": "t1"},
		map[string]interface{}{"value": "2", "ts": "t2"},
	}
	idx := processDataLevel4(arr, &val, &ts, 0)
	if idx != 2 {
		t.Fatalf("processDataLevel4 returned %d; want 2", idx)
	}
	if val[0] != "1" || val[1] != "2" {
		t.Fatalf("values: %v", val)
	}
}

// TestProcessDataLevel3 — inside data object: dp or []dp.
func TestProcessDataLevel3_SingleDp(t *testing.T) {
	val := make([]string, 4)
	ts := make([]string, 4)
	data := map[string]interface{}{
		"path": "Vehicle.Speed",
		"dp": map[string]interface{}{
			"value": "100",
			"ts":    "now",
		},
	}
	idx := processDataLevel3(data, &val, &ts, 0)
	if idx != 1 {
		t.Fatalf("processDataLevel3 single dp returned %d; want 1", idx)
	}
}

func TestProcessDataLevel3_DpArray(t *testing.T) {
	val := make([]string, 4)
	ts := make([]string, 4)
	data := map[string]interface{}{
		"path": "Vehicle.Speed",
		"dp": []interface{}{
			map[string]interface{}{"value": "1", "ts": "t1"},
			map[string]interface{}{"value": "2", "ts": "t2"},
		},
	}
	idx := processDataLevel3(data, &val, &ts, 0)
	if idx != 2 {
		t.Fatalf("processDataLevel3 dp array returned %d; want 2", idx)
	}
}

// TestProcessDataLevel2 exercises the []data array dispatcher directly.
func TestProcessDataLevel2_ArrayOfData(t *testing.T) {
	val := make([]string, 4)
	ts := make([]string, 4)
	dataArray := []interface{}{
		map[string]interface{}{
			"path": "Vehicle.Speed",
			"dp":   map[string]interface{}{"value": "42", "ts": "t1"},
		},
		map[string]interface{}{
			"path": "Vehicle.Rpm",
			"dp":   map[string]interface{}{"value": "3000", "ts": "t2"},
		},
	}
	idx := processDataLevel2(dataArray, &val, &ts, 0)
	if idx != 2 {
		t.Fatalf("processDataLevel2 two-element array = %d; want 2", idx)
	}
	if val[0] != "42" || ts[0] != "t1" {
		t.Errorf("first entry: val=%q ts=%q; want 42,t1", val[0], ts[0])
	}
	if val[1] != "3000" || ts[1] != "t2" {
		t.Errorf("second entry: val=%q ts=%q; want 3000,t2", val[1], ts[1])
	}
}

func TestProcessDataLevel2_NonMapElementSkipped(t *testing.T) {
	val := make([]string, 4)
	ts := make([]string, 4)
	dataArray := []interface{}{
		"not-a-map", // skipped without panic
		map[string]interface{}{
			"path": "Vehicle.Speed",
			"dp":   map[string]interface{}{"value": "10", "ts": "t"},
		},
	}
	idx := processDataLevel2(dataArray, &val, &ts, 0)
	if idx != 1 {
		t.Fatalf("expected 1 after skipping non-map element; got %d", idx)
	}
}

// TestProcessDataLevel1 — top-level dispatch for single-data and array-of-data.
func TestProcessDataLevel1_SingleData(t *testing.T) {
	val := make([]string, 4)
	ts := make([]string, 4)
	data := map[string]interface{}{
		"path": "Vehicle.Speed",
		"dp":   map[string]interface{}{"value": "v", "ts": "t"},
	}
	idx := processDataLevel1(data, &val, &ts, 0)
	if idx < 1 {
		t.Fatalf("processDataLevel1 single = %d; want >= 1", idx)
	}
}

// TestStoreInArrays_RoundTrip exercises the JSON-to-array converter
// with a well-formed notification.
func TestStoreInArrays_RoundTrip(t *testing.T) {
	val := make([]string, 4)
	ts := make([]string, 4)
	notification := `{"action":"subscription","data":{"path":"Vehicle.Speed","dp":{"value":"42","ts":"now"}}}`
	idx := storeinArrays(notification, &val, &ts, 0)
	if idx != 1 {
		t.Fatalf("storeinArrays = %d; want 1", idx)
	}
	if val[0] != "42" || ts[0] != "now" {
		t.Fatalf("not stored: val=%v ts=%v", val, ts)
	}
}

// TestSaveInCsv writes a small CSV and we read it back to verify shape.
// saveInCsv writes to "data.csv" in the cwd; chdir to a temp dir so we
// don't pollute the repository working tree.
func TestSaveInCsv(t *testing.T) {
	dir := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("os.Chdir: %v", err)
	}
	defer os.Chdir(cwd)

	saveInCsv([]string{"10", "20", "30"}, []string{"t1", "t2", "t3"}, 3)

	f, err := os.Open(filepath.Join(dir, "data.csv"))
	if err != nil {
		t.Fatalf("saveInCsv didn't produce data.csv: %v", err)
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatalf("csv parse: %v", err)
	}
	if len(rows) < 3 {
		t.Fatalf("expected at least 3 data rows; got %d (%v)", len(rows), rows)
	}
}
