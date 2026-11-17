/**
* (C) 2026 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE
* file in this repository.
*
* Tests that the validation of struct values in SET requests uses the type definition tree
* that the signal's tree references with its typestree metadata, when the forest holds
* more than one type definition tree.
**/
package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/covesa/vissr/utils"
)

// typeTree builds a type definition tree with one struct, Types.Struct.<structName>,
// with the given members. A member whose datatype starts with "Types" is a nested struct.
func typeTree(structName string, members map[string]string) *utils.Node_t {
	st := &utils.Node_t{Name: structName, NodeType: utils.STRUCT}
	for name, datatype := range members {
		m := &utils.Node_t{Name: name, NodeType: utils.PROPERTY, Datatype: datatype, Parent: st}
		st.Child = append(st.Child, m)
	}
	st.Children = uint8(len(st.Child))
	return utils.NewBranchNode("Types", utils.NewBranchNode("Struct", st))
}

// loadTwoTypeTrees loads a forest with two main trees and two type definition trees:
//
//	Vehicle  (typestree Car+0.1)  Vehicle.Door   actuator, datatype Types.Struct.Door
//	Truck    (typestree Car+0.2)  Truck.Door     actuator, datatype Types.Struct.Door
//	Plain    (no typestree)       Plain.Door     actuator, datatype Types.Struct.Door
//	Types    (Car 0.1)  Struct.Door { Open (boolean) }
//	Types2   (Car 0.2)  Struct.Door { Width (uint8), Hinge (Types.Struct.Hinge) }  Struct.Hinge{ Side (string) }
//
// Both type trees define Types.Struct.Door, but differently, so the outcome of a validation shows
// which of them was used. Types.Struct.Hinge exists in Types2 only; its struct member reference
// is written "Types.Struct.Hinge" as in a type definition tree, whatever the tree is mapped to.
func loadTwoTypeTrees(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	door := func(root string) *utils.Node_t {
		d := utils.NewSignalNode("Door", utils.ACTUATOR, "Types.Struct.Door", "door", "", "", "")
		return utils.NewBranchNode(root, d)
	}
	utils.VSSWriteTree(filepath.Join(dir, "vehicle.binary"), door("Vehicle"))
	utils.VSSWriteTree(filepath.Join(dir, "truck.binary"), door("Truck"))
	utils.VSSWriteTree(filepath.Join(dir, "plain.binary"), door("Plain"))
	utils.VSSWriteTree(filepath.Join(dir, "types.binary"), typeTree("Door", map[string]string{"Open": "boolean"}))
	types2 := typeTree("Door", map[string]string{"Width": "uint8", "Hinge": "Types.Struct.Hinge"})
	hinge := &utils.Node_t{Name: "Hinge", NodeType: utils.STRUCT}
	side := &utils.Node_t{Name: "Side", NodeType: utils.PROPERTY, Datatype: "string", Parent: hinge}
	hinge.Child = []*utils.Node_t{side}
	hinge.Children = 1
	structBranch := types2.Child[0]
	structBranch.Child = append(structBranch.Child, hinge)
	structBranch.Children = 2
	hinge.Parent = structBranch
	utils.VSSWriteTree(filepath.Join(dir, "types2.binary"), types2)

	block := func(name, domain, version, file, typesTree string) string {
		s := "HIM." + name + ":\ntype: direct\ndomain: " + domain + "\nversion: " + version + "\nlocal: " + filepath.Join(dir, file) + "\n"
		if typesTree != "" {
			s += "typestree: " + typesTree + "\n"
		}
		return s + "description: test\n\n"
	}
	him := "HIM:\ntype: branch\ndescription: test\n\n" +
		block("Vehicle", "Vehicle.Car.VehicleData", "5.0", "vehicle.binary", "Vehicle.Car.TypeDefinition+0.1") +
		block("Truck", "Vehicle.Truck.VehicleData", "5.0", "truck.binary", "Vehicle.Car.TypeDefinition+0.2") +
		block("Plain", "Vehicle.Plain.VehicleData", "5.0", "plain.binary", "") +
		block("Types", "Vehicle.Car.TypeDefinition", "0.1", "types.binary", "") +
		block("Types2", "Vehicle.Car.TypeDefinition", "0.2", "types2.binary", "")
	himPath := filepath.Join(dir, "types.him")
	if err := os.WriteFile(himPath, []byte(him), 0644); err != nil {
		t.Fatal(err)
	}
	if !utils.InitForest(himPath) {
		t.Fatal("InitForest failed")
	}
}

func TestValidateStructSetValue_TypeTreeFollowsMainTree(t *testing.T) {
	loadTwoTypeTrees(t)
	open := map[string]interface{}{"Open": true}
	width := map[string]interface{}{"Width": "20", "Hinge": map[string]interface{}{"Side": "left"}}
	cases := []struct {
		name   string
		signal string
		value  map[string]interface{}
		want   string
	}{
		{"Vehicle uses Types", "Vehicle.Door", open, ""},
		{"Vehicle rejects the Types2 struct", "Vehicle.Door", width, structErrMsg},
		{"Truck uses Types2", "Truck.Door", width, ""},
		{"Truck rejects the Types struct", "Truck.Door", open, structErrMsg},
		{"Truck nested struct is looked up in Types2", "Truck.Door", map[string]interface{}{"Hinge": map[string]interface{}{"Bogus": "x"}}, structErrMsg},
		{"no typestree falls back to the datatype's own root", "Plain.Door", open, ""},
	}
	for _, c := range cases {
		if got := validateStructSetValue(c.signal, "Types.Struct.Door", c.value); got != c.want {
			t.Errorf("%s: got %q; want %q", c.name, got, c.want)
		}
	}
}

// A nested struct whose member datatype names a type that only the referenced type tree has.
func TestVerifyStruct_NestedTypeResolvedInSameTypeTree(t *testing.T) {
	loadTwoTypeTrees(t)
	root := utils.SetRootNodePointer("Types2")
	v := map[string]interface{}{"Hinge": map[string]interface{}{"Side": "left"}}
	if got := verifyStruct(v, root, "Types2.Struct.Door", 0); got != "ok" {
		t.Errorf("got %q; want ok", got)
	}
}
