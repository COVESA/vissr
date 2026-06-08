/**
* (C) 2026 Matt Jones
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE file in this repository.
*
* Unit tests for the pure / pure-enough functions in DomainConversionTool.
*
* Functions that require an open *sql.DB, interactive stdin, or filesystem
* loops (initDb, createTables, getDomainTableNames, showDomains, populateTable,
* createConversionTable, createDomainDatamodel, createConversionFiles, main)
* are integration-only entry points and are not tested here.
**/
package main

import (
	"os"
	"path/filepath"
	"testing"
)

// ── fileExists ────────────────────────────────────────────────────────────────

func TestFileExists_Present(t *testing.T) {
	f, err := os.CreateTemp("", "domconv-test-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	defer os.Remove(f.Name())

	if !fileExists(f.Name()) {
		t.Errorf("fileExists(%q) = false; want true", f.Name())
	}
}

func TestFileExists_Missing(t *testing.T) {
	name := filepath.Join(os.TempDir(), "domconv-test-missing-file-xyz.txt")
	if fileExists(name) {
		t.Errorf("fileExists(%q) = true; want false for missing file", name)
	}
}

func TestFileExists_Directory(t *testing.T) {
	// A directory is not a file.
	if fileExists(os.TempDir()) {
		t.Errorf("fileExists(dir) = true; want false")
	}
}

// ── domainTable ───────────────────────────────────────────────────────────────

func TestDomainTable_Found(t *testing.T) {
	tables := []string{"Alpha", "Beta", "Gamma"}
	if !domainTable(tables, "Beta") {
		t.Error("domainTable: expected true for existing name")
	}
}

func TestDomainTable_NotFound(t *testing.T) {
	tables := []string{"Alpha", "Beta"}
	if domainTable(tables, "Delta") {
		t.Error("domainTable: expected false for absent name")
	}
}

func TestDomainTable_Empty(t *testing.T) {
	if domainTable(nil, "X") {
		t.Error("domainTable: expected false for nil slice")
	}
}

// ── getTableNameList ──────────────────────────────────────────────────────────

func TestGetTableNameList_Single(t *testing.T) {
	got := getTableNameList([]string{"Alpha"})
	if got != "Alpha" {
		t.Errorf("got %q; want %q", got, "Alpha")
	}
}

func TestGetTableNameList_Multiple(t *testing.T) {
	got := getTableNameList([]string{"Alpha", "Beta", "Gamma"})
	if got != "Alpha, Beta, Gamma" {
		t.Errorf("got %q; want %q", got, "Alpha, Beta, Gamma")
	}
}

func TestGetTableNameList_Empty(t *testing.T) {
	got := getTableNameList(nil)
	if got != "" {
		t.Errorf("got %q; want empty string for nil input", got)
	}
}

// ── domainTableName ───────────────────────────────────────────────────────────

func TestDomainTableName_OtherTable(t *testing.T) {
	// Names in otherTables should return false.
	for _, name := range otherTables {
		if domainTableName(name) {
			t.Errorf("domainTableName(%q) = true; want false (in otherTables)", name)
		}
	}
}

func TestDomainTableName_DomainTable(t *testing.T) {
	if !domainTableName("MySensorDomain") {
		t.Error("domainTableName: expected true for non-reserved name")
	}
}

// ── insertEscape ──────────────────────────────────────────────────────────────

func TestInsertEscape_NoQuotes(t *testing.T) {
	got := insertEscape("hello world")
	if got != "hello world" {
		t.Errorf("got %q; want %q", got, "hello world")
	}
}

func TestInsertEscape_WithQuotes(t *testing.T) {
	got := insertEscape(`{"a":"b"}`)
	want := `{\"a\":\"b\"}`
	if got != want {
		t.Errorf("got %q; want %q", got, want)
	}
}

func TestInsertEscape_Empty(t *testing.T) {
	if got := insertEscape(""); got != "" {
		t.Errorf("got %q; want empty", got)
	}
}

// ── isEmptyLine ───────────────────────────────────────────────────────────────

func TestIsEmptyLine_AllSpaces(t *testing.T) {
	if !isEmptyLine("   ") {
		t.Error("expected true for all-space line")
	}
}

func TestIsEmptyLine_EmptyString(t *testing.T) {
	if !isEmptyLine("") {
		t.Error("expected true for empty string")
	}
}

func TestIsEmptyLine_HasContent(t *testing.T) {
	if isEmptyLine("  x  ") {
		t.Error("expected false for line with non-space character")
	}
}

// ── readValue ─────────────────────────────────────────────────────────────────

func TestReadValue_Basic(t *testing.T) {
	got := readValue("unit: km/h")
	if got != "km/h" {
		t.Errorf("got %q; want %q", got, "km/h")
	}
}

func TestReadValue_LeadingSpaces(t *testing.T) {
	got := readValue("datatype:   uint8")
	if got != "uint8" {
		t.Errorf("got %q; want %q", got, "uint8")
	}
}

func TestReadValue_EmptyAfterColon(t *testing.T) {
	got := readValue("allowed:")
	if got != "" {
		t.Errorf("got %q; want empty string", got)
	}
}

// ── readArrayValue ────────────────────────────────────────────────────────────

func TestReadArrayValue_Simple(t *testing.T) {
	got := readArrayValue("  - LOW")
	if got != "LOW" {
		t.Errorf("got %q; want %q", got, "LOW")
	}
}

func TestReadArrayValue_WithQuotes(t *testing.T) {
	got := readArrayValue("  - 'HIGH'")
	if got != "HIGH" {
		t.Errorf("got %q; want %q (quotes stripped)", got, "HIGH")
	}
}

func TestReadArrayValue_WithComma(t *testing.T) {
	// Comma terminates the value.
	got := readArrayValue("  - MEDIUM, # comment")
	if got != "MEDIUM" {
		t.Errorf("got %q; want %q", got, "MEDIUM")
	}
}

func TestReadArrayValue_WithHash(t *testing.T) {
	got := readArrayValue("  - LOW # inline comment")
	if got != "LOW" {
		t.Errorf("got %q; want %q", got, "LOW")
	}
}

// ── getTypeIndex ──────────────────────────────────────────────────────────────

func TestGetTypeIndex_Known(t *testing.T) {
	cases := map[string]int8{
		"sensor":    0,
		"actuator":  1,
		"attribute": 2,
		"not used":  -1,
	}
	for input, want := range cases {
		if got := getTypeIndex(input); got != want {
			t.Errorf("getTypeIndex(%q) = %d; want %d", input, got, want)
		}
	}
}

func TestGetTypeIndex_Unknown(t *testing.T) {
	if got := getTypeIndex("branch"); got != -1 {
		t.Errorf("getTypeIndex(unknown) = %d; want -1", got)
	}
}

// ── getDatatypeIndex ──────────────────────────────────────────────────────────

func TestGetDatatypeIndex_Known(t *testing.T) {
	cases := map[string]int8{
		"uint8":    0,
		"uint16":   1,
		"uint32":   2,
		"uint64":   3,
		"int8":     4,
		"int16":    5,
		"int32":    6,
		"int64":    7,
		"float":    8,
		"float32":  8,
		"double":   9,
		"float64":  9,
		"boolean":  10,
		"string":   11,
		"uint8[]":  12,
		"string[]": 23,
	}
	for input, want := range cases {
		if got := getDatatypeIndex(input); got != want {
			t.Errorf("getDatatypeIndex(%q) = %d; want %d", input, got, want)
		}
	}
}

func TestGetDatatypeIndex_Unknown(t *testing.T) {
	if got := getDatatypeIndex("blob"); got != -1 {
		t.Errorf("getDatatypeIndex(unknown) = %d; want -1", got)
	}
}

// ── extractJsonData ───────────────────────────────────────────────────────────

func TestExtractJsonData_Empty(t *testing.T) {
	if got := extractJsonData(""); got != nil {
		t.Errorf("got %v; want nil for empty input", got)
	}
}

func TestExtractJsonData_Array(t *testing.T) {
	got := extractJsonData(`["LOW","MEDIUM","HIGH"]`)
	if len(got) != 3 || got[0] != "LOW" || got[1] != "MEDIUM" || got[2] != "HIGH" {
		t.Errorf("got %v; want [LOW MEDIUM HIGH]", got)
	}
}

func TestExtractJsonData_Object(t *testing.T) {
	// Object keys become the values (VSS enum pattern).
	got := extractJsonData(`{"ON":"1","OFF":"0"}`)
	// extractJsonDataLevel2 sorts keys.
	if len(got) != 2 {
		t.Fatalf("got len=%d; want 2", len(got))
	}
	if got[0] != "OFF" || got[1] != "ON" {
		t.Errorf("got %v; want [OFF ON] (sorted keys)", got)
	}
}

func TestExtractJsonData_InvalidJSON(t *testing.T) {
	if got := extractJsonData("not json"); got != nil {
		t.Errorf("expected nil for invalid JSON; got %v", got)
	}
}

// ── extractJsonDataLevel1 ─────────────────────────────────────────────────────

func TestExtractJsonDataLevel1_Map(t *testing.T) {
	input := map[string]interface{}{"A": "1", "B": "2"}
	got := extractJsonDataLevel1(input)
	if len(got) != 2 {
		t.Fatalf("got len=%d; want 2", len(got))
	}
}

func TestExtractJsonDataLevel1_NonMap(t *testing.T) {
	if got := extractJsonDataLevel1("a string"); got != nil {
		t.Errorf("expected nil for non-map input; got %v", got)
	}
}

// ── extractJsonDataLevel2 ─────────────────────────────────────────────────────

func TestExtractJsonDataLevel2_Sorted(t *testing.T) {
	input := map[string]interface{}{"C": "3", "A": "1", "B": "2"}
	got := extractJsonDataLevel2(input)
	if len(got) != 3 || got[0] != "A" || got[1] != "B" || got[2] != "C" {
		t.Errorf("expected sorted keys [A B C]; got %v", got)
	}
}

// ── decomposePath ─────────────────────────────────────────────────────────────

func TestDecomposePath_TwoLevels(t *testing.T) {
	// "Vehicle.Speed" → first element is the parent branch "Vehicle"
	got := decomposePath("Vehicle.Speed")
	if len(got) < 1 || got[0] != "Vehicle" {
		t.Errorf("decomposePath(Vehicle.Speed)[0] = %q; want %q", got[0], "Vehicle")
	}
}

func TestDecomposePath_ThreeLevels(t *testing.T) {
	got := decomposePath("Vehicle.Body.Lights")
	// First two elements should be the branch hierarchy.
	if len(got) < 2 || got[0] != "Vehicle" || got[1] != "Vehicle.Body" {
		t.Errorf("decomposePath(Vehicle.Body.Lights)[:2] = %v; want [Vehicle Vehicle.Body]", got[:2])
	}
}

// ── serializeUInt ─────────────────────────────────────────────────────────────

func TestSerializeUInt_Uint8(t *testing.T) {
	got := serializeUInt(uint8(0xAB))
	if len(got) != 1 || got[0] != 0xAB {
		t.Errorf("uint8: got %v; want [0xAB]", got)
	}
}

func TestSerializeUInt_Uint16_LittleEndian(t *testing.T) {
	// 0x0102 → [lo=0x02, hi=0x01]
	got := serializeUInt(uint16(0x0102))
	if len(got) != 2 || got[0] != 0x02 || got[1] != 0x01 {
		t.Errorf("uint16: got %v; want [0x02 0x01]", got)
	}
}

func TestSerializeUInt_Uint32(t *testing.T) {
	got := serializeUInt(uint32(0x01020304))
	if len(got) != 4 {
		t.Errorf("uint32: expected 4 bytes; got %d", len(got))
	}
	// little-endian: [0x04, 0x03, 0x02, 0x01]
	if got[0] != 0x04 || got[1] != 0x03 || got[2] != 0x02 || got[3] != 0x01 {
		t.Errorf("uint32: got %v; want [0x04 0x03 0x02 0x01]", got)
	}
}

func TestSerializeUInt_Unknown(t *testing.T) {
	if got := serializeUInt("bad"); got != nil {
		t.Errorf("unknown type: expected nil; got %v", got)
	}
}

// ── inBranchList / addToBranchList ────────────────────────────────────────────

func TestInBranchList_AddAndFind(t *testing.T) {
	// Reset the global before the test.
	branchPathList = nil
	addToBranchList("Vehicle")
	if !inBranchList("Vehicle") {
		t.Error("expected to find Vehicle after addToBranchList")
	}
	if inBranchList("Vehicle.Speed") {
		t.Error("expected not to find Vehicle.Speed when it was not added")
	}
	branchPathList = nil // clean up
}

// Integration-only functions — NOT unit-tested here:
//
//   initDb                   — opens a real sqlite3 file
//   createTables             — requires *sql.DB
//   createConversionDataTable — requires *sql.DB
//   createInternalToolTable  — requires *sql.DB
//   initializeInternalToolTable — requires *sql.DB
//   checkThisTable           — requires *sql.DB
//   getDomainTableNames      — requires global db
//   getInternalToolNbdTableNames — requires global db
//   updateInternalToolTableNames — requires global db
//   insertTableRow           — requires global db
//   insertFeederData         — requires global db
//   getDomainData            — requires global db
//   createDomainTable        — requires global db
//   populateTable            — reads stdin + db
//   createConversionTable    — reads stdin + db
//   createDomainDatamodel    — reads stdin + db
//   showDomains              — requires global db
//   createConversionFiles    — requires global db
//   writescaleDataList       — writes to filesystem (output artefact)
//   readUnitScaleData        — reads an external YAML file
//   readSignalMappingFile    — reads an external YAML file
//   main                     — interactive CLI
