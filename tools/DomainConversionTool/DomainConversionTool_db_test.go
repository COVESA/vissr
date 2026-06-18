/**
* (C) 2026 Matt Jones
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE file in this repository.
*
* DB-backed and file-IO tests for DomainConversionTool. These drive the
* functions the original test file documented as "integration-only" by giving
* them what they actually need: a real (temporary, file-backed) sqlite database
* assigned to the package global, temp YAML fixtures, redirected stdin for the
* interactive Scanf prompts, and a temp working directory for the generated
* artefact files.
**/
package main

import (
	"os"
	"path/filepath"
	"testing"
)

// newToolDB points the package-global db at a fresh temp sqlite file and
// creates the standard tables (exercises initDb + createTables + the table
// creators + initializeInternalToolTable).
func newToolDB(t *testing.T) {
	t.Helper()
	dbFile := filepath.Join(t.TempDir(), "dct.db")
	db = initDb(dbFile, db)
	// NB: deliberately not capping MaxOpenConns to 1 — several query helpers
	// leak an open *Rows on their error path (no rows.Close before return), so
	// a single-connection pool would deadlock the next statement.
	t.Cleanup(func() {
		if db != nil {
			db.Close()
			db = nil
		}
	})
}

// resetGlobals clears the mutable package globals so tests don't bleed into
// each other.
func resetGlobals() {
	scaleDataList = nil
	unitScaleList = nil
	branchPathList = nil
	domainData = nil
}

// inTempDir switches into a fresh temp working directory for the duration of a
// test (the file-writing functions emit artefacts into the cwd).
func inTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(old) })
	return dir
}

// withStdin feeds input to os.Stdin while fn runs (for the fmt.Scanf prompts).
func withStdin(t *testing.T, input string, fn func()) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = old }()
	go func() {
		w.WriteString(input)
		w.Close()
	}()
	fn()
}

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

const domainYaml = `Domain: NorthDom
VehicleA:
  type: branch
  description: A branch
VehicleA.Speed:
  type: sensor
  datatype: uint16
  unit: km/h
  min: 0
  max: 300
  description: Vehicle speed
VehicleA.Mode:
  type: actuator
  datatype: string
  allowed:
    - 'LOW'
    - 'HIGH'
  description: Drive mode
`

// ── initDb / createTables / InternalTool round-trip ───────────────────────────

func TestInitDbCreatesTablesAndInternalTool(t *testing.T) {
	resetGlobals()
	newToolDB(t)

	// Fresh InternalTool row has NULL nbd/sbd names -> scan-error branch -> "","".
	if nbd, sbd := getInternalToolNbdTableNames(); nbd != "" || sbd != "" {
		t.Errorf("fresh InternalTool names = %q/%q; want empty/empty", nbd, sbd)
	}

	updateInternalToolTableNames("NorthDom", "SouthDom")
	if nbd, sbd := getInternalToolNbdTableNames(); nbd != "NorthDom" || sbd != "SouthDom" {
		t.Errorf("after update = %q/%q; want NorthDom/SouthDom", nbd, sbd)
	}
}

// ── domain table lifecycle + row read-back ────────────────────────────────────

func TestDomainTableLifecycle(t *testing.T) {
	resetGlobals()
	newToolDB(t)

	createDomainTableIfNotExist(db, "NorthDom")
	createDomainTableIfNotExist(db, "NorthDom") // idempotent (already exists)

	if !checkThisTable(db, "NorthDom") {
		t.Fatal("checkThisTable: NorthDom should exist after creation")
	}
	if checkThisTable(db, "Nope") {
		t.Error("checkThisTable: nonexistent table should be false")
	}
	if names := getDomainTableNames(); !domainTable(names, "NorthDom") {
		t.Fatalf("getDomainTableNames=%v; want it to include NorthDom", names)
	}

	row := DomainData{Path: "VehicleA.Speed", Type: "sensor", Datatype: "uint16", Unit: "km/h",
		Min: "0", Max: "300", Description: "Vehicle speed"}
	insertTableRow(row, "NorthDom")

	got := getDomainData("NorthDom", "VehicleA.Speed")
	if got.Name != "VehicleA.Speed" || got.Datatype != "uint16" || got.Unit != "km/h" {
		t.Fatalf("getDomainData = %+v; want the inserted row", got)
	}
	if missing := getDomainData("NorthDom", "Nope"); missing.Name != "" {
		t.Errorf("getDomainData(missing) = %+v; want empty", missing)
	}

	if model := readDomainDatamodel("NorthDom"); len(model) != 1 || model[0].Path != "VehicleA.Speed" {
		t.Fatalf("readDomainDatamodel = %+v; want one VehicleA.Speed node", model)
	}
	if node := readMapElementDatamodel("NorthDom", "VehicleA.Speed"); node.Path != "VehicleA.Speed" {
		t.Fatalf("readMapElementDatamodel = %+v; want VehicleA.Speed", node)
	}
}

// ── populateTable (stdin-driven) imports a YAML domain into the DB ─────────────

func TestPopulateTableFromYaml(t *testing.T) {
	resetGlobals()
	dir := inTempDir(t)
	newToolDB(t)

	yamlPath := writeFile(t, dir, "north.yaml", domainYaml)
	withStdin(t, yamlPath+"\n", func() { populateTable() })

	names := getDomainTableNames()
	if !domainTable(names, "NorthDom") {
		t.Fatalf("populateTable did not create NorthDom; tables=%v", names)
	}
	speed := getDomainData("NorthDom", "VehicleA.Speed")
	if speed.Name != "VehicleA.Speed" || speed.Datatype != "uint16" {
		t.Fatalf("imported VehicleA.Speed = %+v; want datatype uint16", speed)
	}
	mode := getDomainData("NorthDom", "VehicleA.Mode")
	if mode.Name != "VehicleA.Mode" || mode.EnumValues == "" {
		t.Fatalf("imported VehicleA.Mode = %+v; want non-empty enumValues", mode)
	}

	// showDomains just prints; call it for coverage (it must not panic).
	showDomains()
}

// ── unit-scale + signal-mapping file readers ──────────────────────────────────

func TestReadUnitScaleData(t *testing.T) {
	resetGlobals()
	dir := t.TempDir()
	p := writeFile(t, dir, "UnitScaling.yaml", `coefficients:
  - unit1: km/h
    unit2: m/s
    A: 0.2777
    B: 0
  - unit1: celsius
    unit2: kelvin
    A: 1
    B: 273.15
`)
	readUnitScaleData(p)
	if len(unitScaleList) < 2 {
		t.Fatalf("unitScaleList=%d; want at least 2 entries", len(unitScaleList))
	}
	readUnitScaleData(filepath.Join(dir, "missing.yaml")) // missing-file branch (no panic)
}

func TestReadSignalMappingFile(t *testing.T) {
	resetGlobals()
	dir := t.TempDir()
	p := writeFile(t, dir, "map.yaml", `# a comment
NorthBoundDomain: NorthDom
SouthBoundDomain: SouthDom
Mapping:
  - North: VehicleA.Speed
    South: Can.Spd
  - North: VehicleA.Mode
    South: Can.Mode
`)
	nbd, sbd, list := readSignalMappingFile(p)
	if nbd != "NorthDom" || sbd != "SouthDom" {
		t.Fatalf("nbd/sbd = %q/%q; want NorthDom/SouthDom", nbd, sbd)
	}
	if len(list) != 2 || list[0].North != "VehicleA.Speed" || list[0].South != "Can.Spd" {
		t.Fatalf("mapping list = %+v; want two entries", list)
	}
	if n, _, _ := readSignalMappingFile(filepath.Join(dir, "missing.yaml")); n != "" {
		t.Errorf("missing mapping file should yield empty nbd; got %q", n)
	}
}

// ── enum + linear conversion type selection ───────────────────────────────────

func TestGetConversionTypeForEnum(t *testing.T) {
	resetGlobals()
	// Matched lengths -> new scale entry index 0.
	if idx := getConversionTypeForEnum(`["LOW","HIGH"]`, `["0","1"]`); idx != 0 {
		t.Fatalf("first enum mapping index=%d; want 0", idx)
	}
	// Same mapping again -> reused index 0.
	if idx := getConversionTypeForEnum(`["LOW","HIGH"]`, `["0","1"]`); idx != 0 {
		t.Fatalf("repeat enum mapping index=%d; want reused 0", idx)
	}
	// Mismatched lengths -> error sentinel.
	if idx := getConversionTypeForEnum(`["LOW"]`, `["0","1"]`); idx != 65535-2 {
		t.Errorf("mismatched enum index=%d; want 65533", idx)
	}
}

func TestGetConversionTypeForLinear(t *testing.T) {
	resetGlobals()
	unitScaleList = []UnitScaleElem{{Unit1: "km/h", Unit2: "m/s", A: "0.2777", B: "0"}}
	if idx := getConversionTypeForLinear("km/h", "m/s"); idx != 0 {
		t.Fatalf("forward linear index=%d; want 0", idx)
	}
	// Inverse direction triggers the coefficient-inversion branch and a new entry.
	if idx := getConversionTypeForLinear("m/s", "km/h"); idx != 1 {
		t.Fatalf("inverse linear index=%d; want 1", idx)
	}
	// Unknown pair -> error sentinel.
	if idx := getConversionTypeForLinear("foo", "bar"); idx != 65535-2 {
		t.Errorf("unknown linear index=%d; want 65533", idx)
	}
}

// ── full conversion build: createConversionTable + createConversionFiles ───────

func TestConversionBuildEndToEnd(t *testing.T) {
	resetGlobals()
	dir := inTempDir(t)
	newToolDB(t)

	// Two domain tables with one mapped signal pair (differing units -> linear).
	createDomainTable(db, "NorthDom")
	createDomainTable(db, "SouthDom")
	insertTableRow(DomainData{Path: "VehicleA.Speed", Type: "sensor", Datatype: "uint16", Unit: "km/h", Description: "n"}, "NorthDom")
	insertTableRow(DomainData{Path: "Can.Spd", Type: "sensor", Datatype: "uint16", Unit: "m/s", Description: "s"}, "SouthDom")
	unitScaleList = []UnitScaleElem{{Unit1: "km/h", Unit2: "m/s", A: "0.2777", B: "0"}}

	mapPath := writeFile(t, dir, "map.yaml", `NorthBoundDomain: NorthDom
SouthBoundDomain: SouthDom
Mapping:
  - North: VehicleA.Speed
    South: Can.Spd
`)
	withStdin(t, mapPath+"\n", func() { createConversionTable() })

	// ConversionPreparation now holds the two feeder rows; build the .cvt + files.
	createConversionFiles()

	// Artefacts land in the cwd (temp dir).
	if matches, _ := filepath.Glob(filepath.Join(dir, "*.cvt")); len(matches) == 0 {
		t.Error("createConversionFiles did not produce a .cvt file")
	}
	if matches, _ := filepath.Glob(filepath.Join(dir, "Scaling-*.json")); len(matches) == 0 {
		t.Error("createConversionTable did not produce a Scaling json file")
	}
	if matches, _ := filepath.Glob(filepath.Join(dir, "Datamodel-*.yaml")); len(matches) == 0 {
		t.Error("createConversionTable did not produce a Datamodel yaml file")
	}
}

// ── createDomainDatamodel (stdin-driven) writes a VSS YAML tree ────────────────

func TestCreateDomainDatamodelWritesYaml(t *testing.T) {
	resetGlobals()
	dir := inTempDir(t)
	newToolDB(t)

	createDomainTable(db, "NorthDom")
	insertTableRow(DomainData{Path: "VehicleA.Speed", Type: "sensor", Datatype: "uint16", Unit: "km/h",
		Min: "0", Max: "300", Default: "0", Description: "Speed"}, "NorthDom")
	insertTableRow(DomainData{Path: "VehicleA.Mode", Type: "actuator", Datatype: "string",
		EnumValues: `["LOW","HIGH"]`, Default: `["LOW"]`, Description: "Mode"}, "NorthDom")

	// domain name, then tree-content selection.
	withStdin(t, "NorthDom\ncomplete\n", func() { createDomainDatamodel() })

	if matches, _ := filepath.Glob(filepath.Join(dir, "Datamodel-NorthDom.yaml")); len(matches) == 0 {
		t.Fatal("createDomainDatamodel did not produce Datamodel-NorthDom.yaml")
	}
}

// ── low-level writers / serialisers ───────────────────────────────────────────

func TestWriteArrayAndScaleArtefacts(t *testing.T) {
	resetGlobals()
	dir := inTempDir(t)

	feederMap := []FeederConversionData{
		{MapIndex: 0, Name: "a", Type: 0, Datatype: 1, ConvertIndex: 0},
		{MapIndex: 0, Name: "b", Type: 0, Datatype: 1, ConvertIndex: 0},
	}
	reorderMapIndex(feederMap) // pairs index 0<->1
	printArray(feederMap)      // stdout only; must not panic
	writeArrayToFile(feederMap, "out.cvt")
	if !fileExists(filepath.Join(dir, "out.cvt")) {
		t.Error("writeArrayToFile did not create out.cvt")
	}

	scaleDataList = []string{`{"LOW":"0", "HIGH":"1"}`, `[0.2777, 0]`}
	writescaleDataList("NorthDom", "SouthDom")
	if !fileExists(filepath.Join(dir, "Scaling-NorthDom-SouthDom.json")) {
		t.Error("writescaleDataList did not create the scaling json")
	}
}

func TestGetCorrespondingIndexNotFound(t *testing.T) {
	resetGlobals()
	feederMap := []FeederConversionData{{MapIndex: 7, Name: "only"}}
	if got := getCorrespondingIndex(0, 7, feederMap); got != MAXUINT16 {
		t.Errorf("getCorrespondingIndex with no partner = %d; want MAXUINT16", got)
	}
}
