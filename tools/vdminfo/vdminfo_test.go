/**
* (C) 2026 Matt Jones
*
* Unit tests for vdminfo.
*
* printNode is the only pure logic function; main requires filesystem paths
* and is integration-only.
**/
package main

import (
	"os"
	"strings"
	"testing"

	"github.com/covesa/vissr/utils"
)

func init() {
	utils.InitLog("vdminfo-test.log", os.TempDir(), false, "error")
}

// captureOutput redirects stdout during fn and returns what was printed.
func captureOutput(fn func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	fn()

	w.Close()
	os.Stdout = old

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	return string(buf[:n])
}

// ── printNode ─────────────────────────────────────────────────────────────────

func TestPrintNode_LeafNode(t *testing.T) {
	node := &utils.Node_t{
		Name:     "Speed",
		NodeType: "sensor",
		Datatype: "float",
		Unit:     "km/h",
	}
	out := captureOutput(func() { printNode(node, 0) })
	if !strings.Contains(out, "Speed") {
		t.Errorf("output missing node name: %q", out)
	}
	if !strings.Contains(out, "sensor") {
		t.Errorf("output missing NodeType: %q", out)
	}
	if !strings.Contains(out, "float") {
		t.Errorf("output missing Datatype: %q", out)
	}
	if !strings.Contains(out, "km/h") {
		t.Errorf("output missing Unit: %q", out)
	}
}

func TestPrintNode_BranchNode(t *testing.T) {
	child := &utils.Node_t{Name: "Speed", NodeType: "sensor"}
	node := &utils.Node_t{
		Name:     "Vehicle",
		NodeType: utils.BRANCH,
		Child:    []*utils.Node_t{child},
	}
	out := captureOutput(func() { printNode(node, 0) })
	if !strings.Contains(out, "Vehicle") {
		t.Errorf("output missing branch name: %q", out)
	}
	if !strings.Contains(out, "branch") {
		t.Errorf("output missing branch marker: %q", out)
	}
	// Child should appear indented.
	if !strings.Contains(out, "Speed") {
		t.Errorf("output missing child node: %q", out)
	}
}

func TestPrintNode_WithRangeAndDefault(t *testing.T) {
	node := &utils.Node_t{
		Name:         "PedalPosition",
		NodeType:     "sensor",
		Datatype:     "uint8",
		Min:          "0",
		Max:          "100",
		DefaultValue: "0",
	}
	out := captureOutput(func() { printNode(node, 0) })
	if !strings.Contains(out, "0..100") {
		t.Errorf("output missing range: %q", out)
	}
	if !strings.Contains(out, "default=0") {
		t.Errorf("output missing default: %q", out)
	}
}

func TestPrintNode_WithAllowed(t *testing.T) {
	node := &utils.Node_t{
		Name:       "GearMode",
		NodeType:   "actuator",
		AllowedDef: []string{"P", "R", "N", "D"},
	}
	out := captureOutput(func() { printNode(node, 0) })
	if !strings.Contains(out, "allowed=") {
		t.Errorf("output missing allowed values: %q", out)
	}
}

func TestPrintNode_DepthIndent(t *testing.T) {
	node := &utils.Node_t{Name: "Lights", NodeType: "sensor"}
	out := captureOutput(func() { printNode(node, 3) })
	// depth=3 → 6 leading spaces ("  " * 3)
	if !strings.HasPrefix(out, "      Lights") {
		t.Errorf("depth-3 node should have 6 leading spaces; got: %q", out)
	}
}

func TestPrintNode_DescriptionAtDepthZero(t *testing.T) {
	node := &utils.Node_t{
		Name:        "Vehicle",
		NodeType:    utils.BRANCH,
		Description: "Root VSS node",
	}
	out := captureOutput(func() { printNode(node, 0) })
	if !strings.Contains(out, "Root VSS node") {
		t.Errorf("description should be printed at depth 0: %q", out)
	}
}

func TestPrintNode_DescriptionSuppressedBelowDepthZero(t *testing.T) {
	node := &utils.Node_t{
		Name:        "Body",
		NodeType:    utils.BRANCH,
		Description: "Body node",
	}
	out := captureOutput(func() { printNode(node, 1) })
	if strings.Contains(out, "Body node") {
		t.Errorf("description should be suppressed at depth > 0: %q", out)
	}
}

// Integration-only entry points — NOT unit-tested here:
//
//   main — reads filesystem paths from os.Args, calls vdmloader.ParseFile.
