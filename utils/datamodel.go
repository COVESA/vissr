/**
* (C) 2020 Geotab Inc
*
* All files and artifacts in this repository are licensed under the
* provisions of the license provided by the LICENSE file in this repository.
*
*
* Go binary format HIM data model (node types of all HIM profiles).
*
* Vendored from github.com/COVESA/vss-tools/binary/go_parser/datamodel
* (UlfBj/vss-tools him-support branch, commit ca5b158). Keep in sync with upstream.
**/

package utils

import (
    "fmt"
    "strings"
)

type NodeTypes_t uint8

const (   // allowed elements of nodeTypes_t
    SENSOR = 1
    ACTUATOR = 2
    ATTRIBUTE = 3
    BRANCH = 4
    STRUCT = 5
    PROPERTY = 6
    RO = 7
    RW = 8
    PROCEDURE = 9
    IOSTRUCT = 10
    SYMLINK = 11
    TYPEDEF = 12
)

// SENSOR..PROPERTY are the node types of the HIM Vehicle-Data profile (i.e. VSS),
// RO/RW are added by the HIM Data profile, PROCEDURE/IOSTRUCT/SYMLINK by the HIM Service
// profile, and TYPEDEF by the HIM Type Definition rule set (BRANCH/STRUCT/PROPERTY are common).
// New types are appended to keep the values of the existing types unchanged.

type Node_t struct {
    Name string
    NodeType NodeTypes_t
    Uuid string
    Description string
    Datatype string
    Min string
    Max string
    Unit string
    Allowed uint8
    AllowedDef []string
    DefaultAllowed string
    Validate uint8
    Children uint8
    Parent *Node_t
    Child []*Node_t
}

func StringToNodetype(nodeType string) uint8 {
    if (nodeType == "branch") {
        return BRANCH
    }
    if (nodeType == "sensor") {
        return SENSOR
    }
    if (nodeType == "actuator") {
        return ACTUATOR
    }
    if (nodeType == "attribute") {
        return ATTRIBUTE
    }
    if (nodeType == "struct") {
        return STRUCT
    }
    if (nodeType == "property") {
        return PROPERTY
    }
    if (nodeType == "ro") {
        return RO
    }
    if (nodeType == "rw") {
        return RW
    }
    if (nodeType == "procedure") {
        return PROCEDURE
    }
    if (nodeType == "iostruct") {
        return IOSTRUCT
    }
    if (nodeType == "symlink") {
        return SYMLINK
    }
    if (nodeType == "typedef") {
        return TYPEDEF
    }
    fmt.Printf("Unknown type! |%s|\n", nodeType);
    return 0
}

// IsContainerType returns true for node types that are only containers of other nodes, i.e. never leaf nodes.
func IsContainerType(nodeType NodeTypes_t) bool {
    return nodeType == BRANCH || nodeType == STRUCT || nodeType == PROCEDURE || nodeType == IOSTRUCT
}

// IsTypeWithoutDatatype returns true for node types that do not have datatype, unit, or allowed values.
func IsTypeWithoutDatatype(nodeType NodeTypes_t) bool {
    return IsContainerType(nodeType) || nodeType == SYMLINK
}

func ValidateToInt(validate string) uint8 {
    validation := (uint8)(0)
    if strings.Contains(validate, "write-only") {
        validation = 1
    } else if strings.Contains(validate, "read-write") {
        validation = 2
    }
    if strings.Contains(validate, "consent") {
        validation += 10
    }
    return validation
}

func NodetypeToString(nodeType NodeTypes_t) string {
    if (nodeType == BRANCH) {
        return "branch"
    }
    if (nodeType == SENSOR) {
        return "sensor"
    }
    if (nodeType == ACTUATOR) {
        return "actuator"
    }
    if (nodeType == ATTRIBUTE) {
        return "attribute"
    }
    if (nodeType == STRUCT) {
        return "struct"
    }
    if (nodeType == PROPERTY) {
        return "property"
    }
    if (nodeType == RO) {
        return "ro"
    }
    if (nodeType == RW) {
        return "rw"
    }
    if (nodeType == PROCEDURE) {
        return "procedure"
    }
    if (nodeType == IOSTRUCT) {
        return "iostruct"
    }
    if (nodeType == SYMLINK) {
        return "symlink"
    }
    if (nodeType == TYPEDEF) {
        return "typedef"
    }
    fmt.Printf("Unknown type! |%d|\n", nodeType);
    return ""
}


func ValidateToString(validate uint8) string {
    var validation string
    if (validate%10 == 1) {
        validation = "write-only"
    }
    if (validate%10 == 2) {
        validation = "read-write"
    }
    if (validate/10 == 1) {
        validation = "+consent"
    }
    return validation
}
