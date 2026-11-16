/**
* (C) 2026 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE
* file in this repository.
*
* Tests of the HIM configuration file reader (InitForest), with focus on the
* typestree metadata that binds a tree to the type definition tree it references.
**/
package utils

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// himFixture writes small trees and a HIM file into a temp dir. A block is written for each
// entry in blocks, in order; "{DIR}" in a block is replaced by the dir that holds the tree files.
type himFixture struct {
	dir string
}

func newHimFixture(t *testing.T) *himFixture {
	t.Helper()
	f := &himFixture{dir: t.TempDir()}
	// a main tree and a type definition tree, in one file each
	main := NewBranchNode("Vehicle", NewSignalNode("Speed", SENSOR, "float", "speed", "", "", ""))
	VSSWriteTree(filepath.Join(f.dir, "main.binary"), main)
	types := NewBranchNode("Types", NewBranchNode("Struct", &Node_t{Name: "S", NodeType: STRUCT}))
	VSSWriteTree(filepath.Join(f.dir, "types.binary"), types)
	return f
}

// initForest writes the HIM text and runs InitForest on it, and restores the forest when the test ends.
func (f *himFixture) initForest(t *testing.T, him string) bool {
	t.Helper()
	old := himForest
	t.Cleanup(func() { himForest = old })
	path := filepath.Join(f.dir, "test.him")
	him = strings.ReplaceAll(him, "{DIR}", f.dir)
	if err := os.WriteFile(path, []byte(him), 0644); err != nil {
		t.Fatal(err)
	}
	return InitForest(path)
}

func mainBlock(name string, typesTree string) string {
	s := "HIM." + name + ":\ntype: direct\ndomain: Vehicle.Car.VehicleData\nversion: 5.0\nlocal: {DIR}/main.binary\n"
	if typesTree != "" {
		s += "typestree: " + typesTree + "\n"
	}
	return s + "description: main tree\n\n"
}

func typesBlock(name string, version string) string {
	return "HIM." + name + ":\ntype: direct\ndomain: Vehicle.Car.TypeDefinition\nversion: " + version +
		"\nlocal: {DIR}/types.binary\ndescription: types\n\n"
}

const himHeader = "HIM:\ntype: branch\ndescription: test forest\n\n"

func forestEntry(t *testing.T, rootName string) HimTree {
	t.Helper()
	for _, tr := range himForest {
		if tr.RootName == rootName {
			return tr
		}
	}
	t.Fatalf("no tree %q in the forest", rootName)
	return HimTree{}
}

func TestInitForest_TypesTreeResolved(t *testing.T) {
	f := newHimFixture(t)
	if !f.initForest(t, himHeader+mainBlock("Vehicle", "Vehicle.Car.TypeDefinition+0.1")+typesBlock("Types", "0.1")) {
		t.Fatal("InitForest failed")
	}
	v := forestEntry(t, "Vehicle")
	if v.TypesTree != "Vehicle.Car.TypeDefinition+0.1" || v.TypeRootName != "Types" {
		t.Errorf("Vehicle: TypesTree=%q TypeRootName=%q", v.TypesTree, v.TypeRootName)
	}
	if ty := forestEntry(t, "Types"); ty.TypeRootName != "" || ty.TypesTree != "" {
		t.Errorf("Types must not reference a type tree: %+v", ty)
	}
}

// The type definition tree may be defined before or after the tree that references it.
func TestInitForest_TypesTreeDefinedFirst(t *testing.T) {
	f := newHimFixture(t)
	if !f.initForest(t, himHeader+typesBlock("TypesCar", "0.1")+mainBlock("Vehicle", "Vehicle.Car.TypeDefinition+0.1")) {
		t.Fatal("InitForest failed")
	}
	if got := forestEntry(t, "Vehicle").TypeRootName; got != "TypesCar" {
		t.Errorf("TypeRootName = %q; want TypesCar", got)
	}
}

func TestInitForest_NoTypesTree(t *testing.T) {
	f := newHimFixture(t)
	if !f.initForest(t, himHeader+mainBlock("Vehicle", "")+typesBlock("Types", "0.1")) {
		t.Fatal("InitForest failed")
	}
	if got := forestEntry(t, "Vehicle").TypeRootName; got != "" {
		t.Errorf("TypeRootName = %q; want empty", got)
	}
}

// Several trees may reference the same type definition tree, or different versions of it.
func TestInitForest_MultipleTypeTrees(t *testing.T) {
	f := newHimFixture(t)
	him := himHeader +
		mainBlock("Vehicle", "Vehicle.Car.TypeDefinition+0.1") +
		mainBlock("Vehicle2", "Vehicle.Car.TypeDefinition+0.1") +
		mainBlock("Vehicle3", "Vehicle.Car.TypeDefinition+0.2") +
		mainBlock("Vehicle4", "") +
		typesBlock("Types", "0.1") + typesBlock("Types2", "0.2")
	if !f.initForest(t, him) {
		t.Fatal("InitForest failed")
	}
	want := map[string]string{"Vehicle": "Types", "Vehicle2": "Types", "Vehicle3": "Types2", "Vehicle4": ""}
	for root, typeRoot := range want {
		if got := forestEntry(t, root).TypeRootName; got != typeRoot {
			t.Errorf("%s: TypeRootName = %q; want %q", root, got, typeRoot)
		}
	}
}

// "0.1" and "0.1.0" denote the same version.
func TestInitForest_TypesTreeVersionNormalized(t *testing.T) {
	f := newHimFixture(t)
	if !f.initForest(t, himHeader+mainBlock("Vehicle", "Vehicle.Car.TypeDefinition+0.1.0")+typesBlock("Types", "0.1")) {
		t.Fatal("InitForest failed")
	}
	if got := forestEntry(t, "Vehicle").TypeRootName; got != "Types" {
		t.Errorf("TypeRootName = %q; want Types", got)
	}
}

func TestInitForest_TypesTreeErrors(t *testing.T) {
	cases := map[string]string{
		"unknown version":   himHeader + mainBlock("Vehicle", "Vehicle.Car.TypeDefinition+0.9") + typesBlock("Types", "0.1"),
		"unknown domain":    himHeader + mainBlock("Vehicle", "Other.Domain+0.1") + typesBlock("Types", "0.1"),
		"no plus sign":      himHeader + mainBlock("Vehicle", "Vehicle.Car.TypeDefinition") + typesBlock("Types", "0.1"),
		"empty version":     himHeader + mainBlock("Vehicle", "Vehicle.Car.TypeDefinition+") + typesBlock("Types", "0.1"),
		"ambiguous":         himHeader + mainBlock("Vehicle", "Vehicle.Car.TypeDefinition+0.1") + typesBlock("Types", "0.1") + typesBlock("Types2", "0.1"),
		"references itself": himHeader + mainBlock("Vehicle", "Vehicle.Car.VehicleData+5.0"),
	}
	for name, him := range cases {
		t.Run(name, func(t *testing.T) {
			f := newHimFixture(t)
			if f.initForest(t, him) {
				t.Errorf("InitForest succeeded; want failure")
			}
		})
	}
}

// The unknown spelling in the first drafts of the HIM spec is not accepted as a binding.
func TestInitForest_TypesDefIsNotTypesTree(t *testing.T) {
	f := newHimFixture(t)
	him := himHeader + strings.Replace(mainBlock("Vehicle", "Vehicle.Car.TypeDefinition+0.1"), "typestree:", "typesdef:", 1) + typesBlock("Types", "0.1")
	if !f.initForest(t, him) {
		t.Fatal("InitForest failed")
	}
	if v := forestEntry(t, "Vehicle"); v.TypesTree != "" || v.TypeRootName != "" {
		t.Errorf("typesdef must be ignored: %+v", v)
	}
}

// A failed InitForest leaves the forest as it was, and a second successful one replaces it.
func TestInitForest_IsRepeatable(t *testing.T) {
	f := newHimFixture(t)
	good := himHeader + mainBlock("Vehicle", "Vehicle.Car.TypeDefinition+0.1") + typesBlock("Types", "0.1")
	if !f.initForest(t, good) {
		t.Fatal("InitForest failed")
	}
	before := len(himForest)
	if f.initForest(t, himHeader+mainBlock("Vehicle", "Other+1.0")) {
		t.Fatal("InitForest succeeded for a dangling typestree")
	}
	if len(himForest) != before {
		t.Errorf("failed InitForest changed the forest: %d trees, was %d", len(himForest), before)
	}
	if !f.initForest(t, good) || len(himForest) != before {
		t.Errorf("second InitForest: %d trees; want %d", len(himForest), before)
	}
}

// Keys are matched exactly and a value may contain colons and text that looks like other keys.
func TestInitForest_ValuesWithKeywordsAndColons(t *testing.T) {
	f := newHimFixture(t)
	him := himHeader +
		"HIM.Vehicle:\ntype: direct\ndomain: Vehicle.Car.VehicleData\nversion: 5.0\nlocal: {DIR}/main.binary\n" +
		"description: the type: sensor, domain: none, local: nowhere, version: 9\n" +
		"public: https://example.com/tree?instance=A.B.C\n"
	if !f.initForest(t, him) {
		t.Fatal("InitForest failed")
	}
	v := forestEntry(t, "Vehicle")
	if v.TreeType != "direct" || v.Domain != "Vehicle.Car.VehicleData" || v.Version != "5.0" {
		t.Errorf("metadata was overwritten by a description: %+v", v)
	}
	if v.Description != "the type: sensor, domain: none, local: nowhere, version: 9" {
		t.Errorf("description = %q", v.Description)
	}
}

func TestInitForest_DuplicateRootName(t *testing.T) {
	f := newHimFixture(t)
	if f.initForest(t, himHeader+mainBlock("Vehicle", "")+mainBlock("Vehicle", "")) {
		t.Error("InitForest succeeded for a duplicate root name")
	}
}

func TestInitForest_MissingFile(t *testing.T) {
	if InitForest(filepath.Join(t.TempDir(), "none.him")) {
		t.Error("InitForest succeeded for a missing file")
	}
}

func TestResolveTypePath(t *testing.T) {
	f := newHimFixture(t)
	him := himHeader +
		mainBlock("Vehicle", "Vehicle.Car.TypeDefinition+0.1") +
		mainBlock("Vehicle2", "Vehicle.Car.TypeDefinition+0.2") +
		mainBlock("Vehicle3", "") +
		typesBlock("Types", "0.1") + typesBlock("Types2", "0.2")
	if !f.initForest(t, him) {
		t.Fatal("InitForest failed")
	}
	cases := []struct {
		mainPath, typePath string
		wantRoot           string // root name of the returned tree, "" if none
		wantPath           string
	}{
		{"Vehicle.Speed", "Types.Struct.S", "Types", "Types.Struct.S"},
		{"Vehicle2.Speed", "Types.Struct.S", "Types2", "Types2.Struct.S"},
		{"Vehicle2", "Types", "Types2", "Types2"},
		{"Vehicle3.Speed", "Types.Struct.S", "Types", "Types.Struct.S"}, // no typestree, by own root
		{"Vehicle3.Speed", "Types2.Struct.S", "Types2", "Types2.Struct.S"},
		{"Vehicle3.Speed", "Nope.Struct.S", "", ""},
		{"Unknown.Speed", "Types.Struct.S", "Types", "Types.Struct.S"}, // not a tree in the forest
	}
	for _, c := range cases {
		root, path := ResolveTypePath(c.mainPath, c.typePath)
		gotRoot := ""
		if root != nil {
			gotRoot = root.Name
		}
		if gotRoot != c.wantRoot || path != c.wantPath {
			t.Errorf("ResolveTypePath(%q, %q) = (%q, %q); want (%q, %q)", c.mainPath, c.typePath, gotRoot, path, c.wantRoot, c.wantPath)
		}
	}
	if got := GetTypeRootName("Vehicle2.Speed"); got != "Types2" {
		t.Errorf("GetTypeRootName = %q; want Types2", got)
	}
	if got := GetTypeRootName("Vehicle3.Speed"); got != "" {
		t.Errorf("GetTypeRootName = %q; want empty", got)
	}
}

// The type definition tree's mapped name differing from "Types" is allowed, but one that is
// not of the form Types or TypesX only gives a warning.
func TestInitForest_NonTypesRootNameStillResolves(t *testing.T) {
	f := newHimFixture(t)
	if !f.initForest(t, himHeader+mainBlock("Vehicle", "Vehicle.Car.TypeDefinition+0.1")+typesBlock("Defs", "0.1")) {
		t.Fatal("InitForest failed")
	}
	if got := forestEntry(t, "Vehicle").TypeRootName; got != "Defs" {
		t.Errorf("TypeRootName = %q; want Defs", got)
	}
}

func TestGetTypesRootName(t *testing.T) {
	f := newHimFixture(t)
	him := himHeader +
		mainBlock("Vehicle", "Vehicle.Car.TypeDefinition+0.1") +
		mainBlock("Vehicle2", "Vehicle.Car.TypeDefinition+0.2") +
		mainBlock("Vehicle3", "") +
		typesBlock("Types", "0.1") + typesBlock("Types2", "0.2") + typesBlock("Types3", "0.3")
	if !f.initForest(t, him) {
		t.Fatal("InitForest failed")
	}
	cases := map[string]string{
		"Vehicle.Speed":  "Types", // a tree that references a type tree
		"Vehicle2":       "Types2",
		"Types2.Struct":  "Types2", // a type tree that is referenced
		"Vehicle3.Speed": "",       // no typestree
		"Types3.Struct":  "",       // a type tree that nothing references
		"Unknown.X":      "",
	}
	for path, want := range cases {
		if got := GetTypesRootName(path); got != want {
			t.Errorf("GetTypesRootName(%q) = %q; want %q", path, got, want)
		}
	}
}
