/**
* (C) 2026 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE
* file in this repository.
*
* Tests for the validation of struct-typed values in SET requests
* (validateStructSetValue / verifyStruct), run against the real type
* definition tree forest/typedef.binary:
*
*   Types.Struct.InlineStruct
*     Name            (string)
*     NewStruct       (Types.Struct.InlineStruct.InlineStructMember)
*   Types.Struct.InlineStruct.InlineStructMember
*     Name            (string)
**/
package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/covesa/vissr/utils"
)

const (
	inlineStructType = "Types.Struct.InlineStruct"
	structErrMsg     = "Incorrect struct data"
)

// loadTypesForest registers the shipped type definition tree under the
// root name "Types" in the utils forest. The HIM file is written to a temp
// dir and refers to the tree by absolute path, so the working directory is
// not changed.
//
// utils.InitForest is not idempotent: a repeated call fills in the first
// forest entry again (and appends an empty one) instead of adding a new
// entry. Other tests in this package load a different tree the same way, so
// every test that needs the Types tree calls this function itself, rather
// than relying on a once-per-binary load, to make the result independent of
// test order.
func loadTypesForest(t *testing.T) {
	t.Helper()
	typedef, err := filepath.Abs(filepath.Join("forest", "typedef.binary"))
	if err != nil {
		t.Fatalf("Abs failed: %v", err)
	}
	if _, err := os.Stat(typedef); err != nil {
		t.Fatalf("type definition tree not found: %v", err)
	}
	him := "HIM:\n" +
		"type: branch\n" +
		"description: test\n" +
		"\n" +
		"HIM.Types:\n" +
		"type: direct\n" +
		"domain: Vehicle.Car.TypeDefinition\n" +
		"version: 0.1\n" +
		"local: " + typedef + "\n" +
		"description: type definitions\n"
	himPath := filepath.Join(t.TempDir(), "types.him")
	if err := os.WriteFile(himPath, []byte(him), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	if !utils.InitForest(himPath) || utils.SetRootNodePointer("Types") == nil {
		t.Fatalf("could not load the Types tree")
	}
}

func TestValidateStructSetValue_NotAStructTypeIsIgnored(t *testing.T) {
	for _, dt := range []string{"string", "uint8", "float", "boolean", "string[]", ""} {
		if got := validateStructSetValue(dt, "anything"); got != "" {
			t.Errorf("datatype %q: got %q; want no error", dt, got)
		}
	}
}

func TestValidateStructSetValue_StructValueMustBeAnObject(t *testing.T) {
	loadTypesForest(t)
	for _, v := range []interface{}{"text", float64(3), true, nil, []interface{}{"a"}} {
		if got := validateStructSetValue(inlineStructType, v); got != "struct value must be an object" {
			t.Errorf("value %#v: got %q; want the object error", v, got)
		}
	}
}

func TestValidateStructSetValue_ValidStructs(t *testing.T) {
	loadTypesForest(t)
	cases := []map[string]interface{}{
		{"Name": "a"},
		{"Name": "a", "NewStruct": map[string]interface{}{"Name": "b"}},
		{"NewStruct": map[string]interface{}{}},
		{},
	}
	for _, v := range cases {
		if got := validateStructSetValue(inlineStructType, v); got != "" {
			t.Errorf("value %v: got %q; want valid", v, got)
		}
	}
}

func TestValidateStructSetValue_UnknownMemberRejected(t *testing.T) {
	loadTypesForest(t)
	cases := []map[string]interface{}{
		{"Bogus": "a"},
		{"Name": "a", "Bogus": "b"},
		{"NewStruct": map[string]interface{}{"Bogus": "b"}},
	}
	for _, v := range cases {
		if got := validateStructSetValue(inlineStructType, v); got != structErrMsg {
			t.Errorf("value %v: got %q; want %q", v, got, structErrMsg)
		}
	}
}

// Member names are matched case-insensitively (verifyStructMember), so a
// differently-cased nested member must be validated against ITS type
// definition, not against whatever happens to be first in the search result.
func TestValidateStructSetValue_MemberCaseDoesNotChangeValidation(t *testing.T) {
	loadTypesForest(t)
	if got := validateStructSetValue(inlineStructType,
		map[string]interface{}{"newstruct": map[string]interface{}{"Name": "b"}}); got != "" {
		t.Errorf("valid nested struct with lowercase member name rejected: %q", got)
	}
	if got := validateStructSetValue(inlineStructType,
		map[string]interface{}{"newstruct": map[string]interface{}{"Bogus": "b"}}); got != structErrMsg {
		t.Errorf("invalid nested struct with lowercase member name: got %q; want %q", got, structErrMsg)
	}
}

// A member that is declared as a struct type must be given an object. A
// scalar used to be accepted without any check.
func TestValidateStructSetValue_StructMemberMustBeAnObject(t *testing.T) {
	loadTypesForest(t)
	for _, v := range []interface{}{"text", float64(3), true, []interface{}{"a"}} {
		got := validateStructSetValue(inlineStructType, map[string]interface{}{"NewStruct": v})
		if got != structErrMsg {
			t.Errorf("NewStruct=%#v: got %q; want %q", v, got, structErrMsg)
		}
	}
}

// An object given for a member that is not a struct (Name is a string) must
// be rejected.
func TestValidateStructSetValue_ObjectForScalarMemberRejected(t *testing.T) {
	loadTypesForest(t)
	got := validateStructSetValue(inlineStructType,
		map[string]interface{}{"Name": map[string]interface{}{"x": "y"}})
	if got != structErrMsg {
		t.Errorf("got %q; want %q", got, structErrMsg)
	}
}

func TestVerifyStruct_UnknownTypeRoot(t *testing.T) {
	loadTypesForest(t)
	if got := verifyStruct(map[string]interface{}{"a": "b"}, "NoSuchRoot.Struct.X", 0); got != "Struct cannot be verified" {
		t.Errorf("got %q; want %q", got, "Struct cannot be verified")
	}
}

func TestVerifyStruct_UnknownTypeInKnownRoot(t *testing.T) {
	loadTypesForest(t)
	if got := verifyStruct(map[string]interface{}{"a": "b"}, "Types.Struct.NoSuchStruct", 0); got != structErrMsg {
		t.Errorf("got %q; want %q", got, structErrMsg)
	}
}

// Too deep a nesting must be rejected without descending any further.
func TestVerifyStruct_DepthLimit(t *testing.T) {
	loadTypesForest(t)
	if got := verifyStruct(map[string]interface{}{"Name": "a"}, inlineStructType, 6); got != structErrMsg {
		t.Errorf("depth 6: got %q; want %q", got, structErrMsg)
	}
	if got := verifyStruct(map[string]interface{}{"Name": "a"}, inlineStructType, 5); got != "ok" {
		t.Errorf("depth 5: got %q; want ok", got)
	}
}
