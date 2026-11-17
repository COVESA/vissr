/**
* (C) 2026 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE
* file in this repository.
*
* Tests of the validation of arrays of structs ("Types.X[]") in SET requests, and of the
* rule that only struct types are verified: other definitions in a type definition
* tree, e.g. enums, are not.
**/
package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/covesa/vissr/utils"
)

func prop(parent *utils.Node_t, name string, datatype string) {
	n := &utils.Node_t{Name: name, NodeType: utils.PROPERTY, Datatype: datatype, Parent: parent}
	parent.Child = append(parent.Child, n)
	parent.Children = uint8(len(parent.Child))
}

func addChild(parent *utils.Node_t, child *utils.Node_t) {
	child.Parent = parent
	parent.Child = append(parent.Child, child)
	parent.Children = uint8(len(parent.Child))
}

// structArrayTypes builds a type definition tree, root name root:
//
//	Struct.Seat    { Id (uint8) }
//	Struct.Person  { Name (string), Seat (Types.Struct.Seat), Seats (Types.Struct.Seat[]), Tags (string[]) }
//	Color          enum, an attribute that is not a struct
func structArrayTypes() *utils.Node_t {
	seat := &utils.Node_t{Name: "Seat", NodeType: utils.STRUCT}
	prop(seat, "Id", "uint8")
	person := &utils.Node_t{Name: "Person", NodeType: utils.STRUCT}
	prop(person, "Name", "string")
	prop(person, "Seat", "Types.Struct.Seat")
	prop(person, "Seats", "Types.Struct.Seat[]")
	prop(person, "Tags", "string[]")
	structBranch := utils.NewBranchNode("Struct", seat, person)
	color := &utils.Node_t{Name: "Color", NodeType: utils.ATTRIBUTE, Datatype: "string"}
	return utils.NewBranchNode("Types", structBranch, color)
}

// loadStructArrayForest loads the forest
//
//	Vehicle  (no typestree)            Vehicle.Seat Vehicle.Seats Vehicle.Person Vehicle.People Vehicle.Color Vehicle.Colors
//	Truck    (typestree -> Types2)     Truck.People
//	Types    structArrayTypes()
//	Types2   structArrayTypes()
func loadStructArrayForest(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	vehicle := utils.NewBranchNode("Vehicle")
	for name, datatype := range map[string]string{
		"Seat": "Types.Struct.Seat", "Seats": "Types.Struct.Seat[]",
		"Person": "Types.Struct.Person", "People": "Types.Struct.Person[]",
		"Color": "Types.Color", "Colors": "Types.Color[]", "Missing": "Types.Struct.Missing[]",
	} {
		addChild(vehicle, utils.NewSignalNode(name, utils.ACTUATOR, datatype, name, "", "", ""))
	}
	truck := utils.NewBranchNode("Truck", utils.NewSignalNode("People", utils.ACTUATOR, "Types.Struct.Person[]", "p", "", "", ""))
	utils.VSSWriteTree(filepath.Join(dir, "vehicle.binary"), vehicle)
	utils.VSSWriteTree(filepath.Join(dir, "truck.binary"), truck)
	utils.VSSWriteTree(filepath.Join(dir, "types.binary"), structArrayTypes())
	utils.VSSWriteTree(filepath.Join(dir, "types2.binary"), structArrayTypes())
	block := func(name, domain, version, file, typesTree string) string {
		s := "HIM." + name + ":\ntype: direct\ndomain: " + domain + "\nversion: " + version + "\nlocal: " + filepath.Join(dir, file) + "\n"
		if typesTree != "" {
			s += "typestree: " + typesTree + "\n"
		}
		return s + "description: test\n\n"
	}
	him := "HIM:\ntype: branch\ndescription: test\n\n" +
		block("Vehicle", "Vehicle.Car.VehicleData", "5.0", "vehicle.binary", "") +
		block("Truck", "Vehicle.Truck.VehicleData", "5.0", "truck.binary", "Vehicle.Car.TypeDefinition+0.2") +
		block("Types", "Vehicle.Car.TypeDefinition", "0.1", "types.binary", "") +
		block("Types2", "Vehicle.Car.TypeDefinition", "0.2", "types2.binary", "")
	himPath := filepath.Join(dir, "arrays.him")
	if err := os.WriteFile(himPath, []byte(him), 0644); err != nil {
		t.Fatal(err)
	}
	if !utils.InitForest(himPath) {
		t.Fatal("InitForest failed")
	}
}

func obj(kv ...interface{}) map[string]interface{} {
	m := map[string]interface{}{}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i].(string)] = kv[i+1]
	}
	return m
}

func arr(elements ...interface{}) []interface{} {
	if elements == nil {
		return []interface{}{}
	}
	return elements
}

func seatArray(n int) []interface{} {
	a := make([]interface{}, n)
	for i := range a {
		a[i] = obj("Id", "1")
	}
	return a
}

func TestValidateStructSetValue_Arrays(t *testing.T) {
	loadStructArrayForest(t)
	const (
		seat   = "Vehicle.Seats"
		people = "Vehicle.People"
	)
	cases := []struct {
		name     string
		signal   string
		datatype string
		value    interface{}
		want     string
	}{
		{"empty array", people, "Types.Struct.Person[]", arr(), ""},
		{"valid elements", people, "Types.Struct.Person[]", arr(obj("Name", "a"), obj("Name", "b", "Seat", obj("Id", "1"))), ""},
		{"element with no members", people, "Types.Struct.Person[]", arr(obj()), ""},
		{"an object is not an array", people, "Types.Struct.Person[]", obj("Name", "a"), "struct array value must be an array"},
		{"a scalar is not an array", people, "Types.Struct.Person[]", "x", "struct array value must be an array"},
		{"null is not an array", people, "Types.Struct.Person[]", nil, "struct array value must be an array"},
		{"an array is not a struct", "Vehicle.Person", "Types.Struct.Person", arr(obj("Name", "a")), "struct value must be an object"},
		{"element is not an object", people, "Types.Struct.Person[]", arr(obj("Name", "a"), "b"), structErrMsg},
		{"element has unknown member", people, "Types.Struct.Person[]", arr(obj("Name", "a"), obj("Bogus", "b")), structErrMsg},
		{"first element is invalid", people, "Types.Struct.Person[]", arr(obj("Bogus", "b"), obj("Name", "a")), structErrMsg},
		{"nested struct of an element is invalid", people, "Types.Struct.Person[]", arr(obj("Seat", obj("Bogus", "x"))), structErrMsg},
		{"nested struct of an element is not an object", people, "Types.Struct.Person[]", arr(obj("Seat", "x")), structErrMsg},
		{"array of structs in a member", people, "Types.Struct.Person[]", arr(obj("Seats", arr(obj("Id", "1"), obj("Id", "2")))), ""},
		{"array member is not an array", people, "Types.Struct.Person[]", arr(obj("Seats", obj("Id", "1"))), structErrMsg},
		{"array member element is invalid", people, "Types.Struct.Person[]", arr(obj("Seats", arr(obj("Id", "1"), obj("Bogus", "2")))), structErrMsg},
		{"array member element is not an object", people, "Types.Struct.Person[]", arr(obj("Seats", arr("x"))), structErrMsg},
		{"basic type array member is not checked", people, "Types.Struct.Person[]", arr(obj("Tags", arr("a", "b"))), ""},
		{"object for a basic type array member", people, "Types.Struct.Person[]", arr(obj("Tags", obj("a", "b"))), structErrMsg},
		{"array at the length limit", seat, "Types.Struct.Seat[]", seatArray(maxStructArrayLen), ""},
		{"array over the length limit", seat, "Types.Struct.Seat[]", seatArray(maxStructArrayLen + 1), structArrayTooLongMsg},
		{"unknown struct type", "Vehicle.Missing", "Types.Struct.Missing[]", arr(obj("a", "b")), structErrMsg},
	}
	for _, c := range cases {
		if got := validateStructSetValue(c.signal, c.datatype, c.value); got != c.want {
			t.Errorf("%s: got %q; want %q", c.name, got, c.want)
		}
	}
}

// A type definition tree may define other things than structs, e.g. enums, and the value of a signal
// with such a datatype is not verified here.
func TestValidateStructSetValue_NonStructTypesAreNotVerified(t *testing.T) {
	loadStructArrayForest(t)
	for _, v := range []interface{}{"RED", float64(3), true, nil, obj("a", "b"), arr("RED", "BLUE")} {
		if got := validateStructSetValue("Vehicle.Color", "Types.Color", v); got != "" {
			t.Errorf("Types.Color value %#v: got %q; want no error", v, got)
		}
		if got := validateStructSetValue("Vehicle.Colors", "Types.Color[]", v); got != "" {
			t.Errorf("Types.Color[] value %#v: got %q; want no error", v, got)
		}
	}
}

// The same rules apply when the main tree references a type definition tree that has another root name,
// also for the struct types that members of an element refer to.
func TestValidateStructSetValue_ArraysInRemappedTypeTree(t *testing.T) {
	loadStructArrayForest(t)
	good := arr(obj("Name", "a", "Seat", obj("Id", "1"), "Seats", arr(obj("Id", "2"))))
	if got := validateStructSetValue("Truck.People", "Types.Struct.Person[]", good); got != "" {
		t.Errorf("valid array: got %q; want no error", got)
	}
	bad := arr(obj("Seats", arr(obj("Bogus", "2"))))
	if got := validateStructSetValue("Truck.People", "Types.Struct.Person[]", bad); got != structErrMsg {
		t.Errorf("invalid array: got %q; want %q", got, structErrMsg)
	}
	if got := validateStructSetValue("Truck.People", "Types.Struct.Person[]", obj("Name", "a")); got != "struct array value must be an array" {
		t.Errorf("object instead of array: got %q", got)
	}
}

// An object is only accepted for a member that is declared with a struct type.
func TestValidateStructSetValue_ObjectForEnumMemberRejected(t *testing.T) {
	loadStructArrayForest(t)
	if got := verifyStructMemberValue(obj("a", "b"), "Types.Color", utils.SetRootNodePointer("Types"), 0); got {
		t.Error("an object was accepted for a member of an enum type")
	}
	if got := verifyStructMemberValue("RED", "Types.Color", utils.SetRootNodePointer("Types"), 0); !got {
		t.Error("a scalar was rejected for a member of an enum type")
	}
}
