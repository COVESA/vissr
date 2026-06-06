package vdmloader

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/covesa/vissr/utils"
)

func init() {
	utils.InitLog("vdmloader-test.log", os.TempDir(), false, "info")
}

// findNode does a depth-first search for a node by name in the tree.
func findNode(root *utils.Node_t, name string) *utils.Node_t {
	if root == nil {
		return nil
	}
	if root.Name == name {
		return root
	}
	for _, c := range root.Child {
		if n := findNode(c, name); n != nil {
			return n
		}
	}
	return nil
}

// findByFQN walks the tree following dot-separated FQN segments.
func findByFQN(root *utils.Node_t, fqn string) *utils.Node_t {
	parts := splitFQN(fqn)
	cur := root
	for _, p := range parts {
		if cur.Name != p {
			return nil
		}
		// Already at the right node; if only one part, we're done
		if len(parts) == 1 {
			return cur
		}
		break
	}
	// Traverse remaining segments into children
	cur = root
	for _, p := range parts {
		if cur.Name == p {
			continue
		}
		var found *utils.Node_t
		for _, child := range cur.Child {
			if child.Name == p {
				found = child
				break
			}
		}
		if found == nil {
			return nil
		}
		cur = found
	}
	return cur
}

func splitFQN(fqn string) []string {
	out := []string{}
	s := fqn
	for {
		i := indexOf(s, '.')
		if i < 0 {
			out = append(out, s)
			break
		}
		out = append(out, s[:i])
		s = s[i+1:]
	}
	return out
}

func indexOf(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// ── ParseSDL tests ──────────────────────────────────────────────────────────

func TestParseSDL_ReturnsRoot(t *testing.T) {
	sdl := readFixture(t, "vehicle.graphql")
	roots, metas, err := ParseSDL(sdl)
	if err != nil {
		t.Fatalf("ParseSDL: unexpected error: %v", err)
	}
	if len(roots) != 1 {
		t.Fatalf("expected 1 root, got %d", len(roots))
	}
	if roots[0].Name != "Vehicle" {
		t.Errorf("root name = %q, want Vehicle", roots[0].Name)
	}
	if metas[0].RootName != "Vehicle" {
		t.Errorf("meta.RootName = %q, want Vehicle", metas[0].RootName)
	}
}

func TestParseSDL_RootIsBranch(t *testing.T) {
	sdl := readFixture(t, "vehicle.graphql")
	roots, _, _ := ParseSDL(sdl)
	if roots[0].NodeType != utils.BRANCH {
		t.Errorf("root NodeType = %q, want %q", roots[0].NodeType, utils.BRANCH)
	}
}

func TestParseSDL_SensorLeaf(t *testing.T) {
	sdl := readFixture(t, "vehicle.graphql")
	roots, _, err := ParseSDL(sdl)
	if err != nil {
		t.Fatal(err)
	}
	speed := findNode(roots[0], "Speed")
	if speed == nil {
		t.Fatal("Speed node not found")
	}
	if speed.NodeType != utils.SENSOR {
		t.Errorf("Speed.NodeType = %q, want sensor", speed.NodeType)
	}
	if speed.Datatype != "float" {
		t.Errorf("Speed.Datatype = %q, want float", speed.Datatype)
	}
	if speed.Min != "0" {
		t.Errorf("Speed.Min = %q, want 0", speed.Min)
	}
	if speed.Max != "250" {
		t.Errorf("Speed.Max = %q, want 250", speed.Max)
	}
}

func TestParseSDL_ActuatorLeaf(t *testing.T) {
	sdl := readFixture(t, "vehicle.graphql")
	roots, _, err := ParseSDL(sdl)
	if err != nil {
		t.Fatal(err)
	}
	isEnabled := findNode(roots[0], "IsEnabled")
	if isEnabled == nil {
		t.Fatal("IsEnabled node not found")
	}
	if isEnabled.NodeType != utils.ACTUATOR {
		t.Errorf("IsEnabled.NodeType = %q, want actuator", isEnabled.NodeType)
	}
	if isEnabled.Datatype != "bool" {
		t.Errorf("IsEnabled.Datatype = %q, want bool", isEnabled.Datatype)
	}
}

func TestParseSDL_AttributeLeaf(t *testing.T) {
	sdl := readFixture(t, "vehicle.graphql")
	roots, _, err := ParseSDL(sdl)
	if err != nil {
		t.Fatal(err)
	}
	level := findNode(roots[0], "ActiveAutonomyLevel")
	if level == nil {
		t.Fatal("ActiveAutonomyLevel node not found")
	}
	if level.NodeType != utils.ATTRIBUTE {
		t.Errorf("ActiveAutonomyLevel.NodeType = %q, want attribute", level.NodeType)
	}
}

func TestParseSDL_NestedBranch(t *testing.T) {
	sdl := readFixture(t, "vehicle.graphql")
	roots, _, err := ParseSDL(sdl)
	if err != nil {
		t.Fatal(err)
	}
	adas := findNode(roots[0], "ADAS")
	if adas == nil {
		t.Fatal("ADAS node not found")
	}
	if adas.NodeType != utils.BRANCH {
		t.Errorf("ADAS.NodeType = %q, want branch", adas.NodeType)
	}
	abs := findNode(adas, "ABS")
	if abs == nil {
		t.Fatal("ABS node not found under ADAS")
	}
	if abs.NodeType != utils.BRANCH {
		t.Errorf("ABS.NodeType = %q, want branch", abs.NodeType)
	}
}

func TestParseSDL_RangeNegative(t *testing.T) {
	sdl := readFixture(t, "vehicle.graphql")
	roots, _, err := ParseSDL(sdl)
	if err != nil {
		t.Fatal(err)
	}
	lat := findNode(roots[0], "Latitude")
	if lat == nil {
		t.Fatal("Latitude node not found")
	}
	if lat.Min != "-90" {
		t.Errorf("Latitude.Min = %q, want -90", lat.Min)
	}
	if lat.Max != "90" {
		t.Errorf("Latitude.Max = %q, want 90", lat.Max)
	}
}

func TestParseSDL_ParentChildLinks(t *testing.T) {
	sdl := readFixture(t, "vehicle.graphql")
	roots, _, err := ParseSDL(sdl)
	if err != nil {
		t.Fatal(err)
	}
	speed := findNode(roots[0], "Speed")
	if speed == nil {
		t.Fatal("Speed not found")
	}
	if speed.Parent == nil {
		t.Fatal("Speed.Parent is nil")
	}
	if speed.Parent.Name != "Vehicle" {
		t.Errorf("Speed.Parent.Name = %q, want Vehicle", speed.Parent.Name)
	}
}

func TestParseSDL_ChildrenCount(t *testing.T) {
	sdl := readFixture(t, "vehicle.graphql")
	roots, _, err := ParseSDL(sdl)
	if err != nil {
		t.Fatal(err)
	}
	root := roots[0]
	if int(root.Children) != len(root.Child) {
		t.Errorf("Children field %d != len(Child) %d", root.Children, len(root.Child))
	}
}

func TestParseSDL_ErrorOnEmpty(t *testing.T) {
	_, _, err := ParseSDL("")
	if err == nil {
		t.Fatal("expected error for empty SDL, got nil")
	}
}

func TestParseSDL_ErrorOnNoVspecAnnotations(t *testing.T) {
	plain := `type Foo { bar: String }`
	_, _, err := ParseSDL(plain)
	if err == nil {
		t.Fatal("expected error for SDL with no @vspec annotations, got nil")
	}
}

func TestParseSDL_ErrorOnInvalidSyntax(t *testing.T) {
	_, _, err := ParseSDL("this is not graphql")
	if err == nil {
		t.Fatal("expected error for invalid SDL syntax, got nil")
	}
}

// ── ParseFile tests ─────────────────────────────────────────────────────────

func TestParseFile_Roundtrip(t *testing.T) {
	path := filepath.Join("testdata", "vehicle.graphql")
	roots, metas, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if len(roots) != 1 || metas[0].RootName != "Vehicle" {
		t.Errorf("unexpected roots: %v / %v", len(roots), metas)
	}
}

func TestParseFile_NonExistentReturnsError(t *testing.T) {
	_, _, err := ParseFile("/nonexistent/path.graphql")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

// ── LoadDir tests ───────────────────────────────────────────────────────────

func TestLoadDir_RegistersTree(t *testing.T) {
	n, err := LoadDir("testdata")
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if n < 1 {
		t.Errorf("expected at least 1 tree registered, got %d", n)
	}
}

func TestLoadDir_NonExistentReturnsError(t *testing.T) {
	_, err := LoadDir("/nonexistent/dir")
	if err == nil {
		t.Fatal("expected error for nonexistent directory")
	}
}

func TestLoadDir_EmptyDirReturnsZero(t *testing.T) {
	dir := t.TempDir()
	n, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir on empty dir: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 trees from empty dir, got %d", n)
	}
}

// ── Datatype mapping tests ──────────────────────────────────────────────────

func TestMapDatatype(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Int8", "int8"},
		{"UInt8", "uint8"},
		{"Int16", "int16"},
		{"UInt16", "uint16"},
		{"Int", "int32"},
		{"UInt32", "uint32"},
		{"Int64", "int64"},
		{"UInt64", "uint64"},
		{"Float", "float"},
		{"Boolean", "bool"},
		{"String", "string"},
		{"Unknown", "unknown"},
	}
	for _, tc := range cases {
		got := mapDatatype(tc.in)
		if got != tc.want {
			t.Errorf("mapDatatype(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// ── Helper tests ────────────────────────────────────────────────────────────

func TestLastSegment(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Vehicle", "Vehicle"},
		{"Vehicle.Speed", "Speed"},
		{"Vehicle.ADAS.ABS", "ABS"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := lastSegment(tc.in); got != tc.want {
			t.Errorf("lastSegment(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestParentOf(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Vehicle", ""},
		{"Vehicle.Speed", "Vehicle"},
		{"Vehicle.ADAS.ABS", "Vehicle.ADAS"},
	}
	for _, tc := range cases {
		if got := parentOf(tc.in); got != tc.want {
			t.Errorf("parentOf(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestFormatFloat(t *testing.T) {
	cases := []struct{ in, want string }{
		{"0", "0"},
		{"250", "250"},
		{"-90", "-90"},
		{"3.14", "3.14"},
		{"notanumber", "notanumber"},
	}
	for _, tc := range cases {
		if got := formatFloat(tc.in); got != tc.want {
			t.Errorf("formatFloat(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestAppendChild_NoDuplicate(t *testing.T) {
	parent := utils.NewBranchNode("Parent")
	child := utils.NewBranchNode("Child")
	appendChild(parent, child)
	appendChild(parent, child) // second call must be idempotent
	if int(parent.Children) != 1 {
		t.Errorf("expected 1 child, got %d", parent.Children)
	}
}

// ── viss_service extension test ─────────────────────────────────────────────

func TestParseSDL_VissServiceCreatesProc(t *testing.T) {
	sdl := `
type Svc @vspec(element: BRANCH, fqn: "Svc", description: "service root") {
  DoThing: Boolean @vspec(element: SENSOR, fqn: "Svc.DoThing") @viss_service
}
`
	roots, _, err := ParseSDL(sdl)
	if err != nil {
		t.Fatalf("ParseSDL: %v", err)
	}
	doThing := findNode(roots[0], "DoThing")
	if doThing == nil {
		t.Fatal("DoThing not found")
	}
	if doThing.NodeType != utils.PROCEDURE {
		t.Errorf("DoThing.NodeType = %q, want procedure", doThing.NodeType)
	}
}

// ── helpers ──────────────────────────────────────────────────────────────────

func readFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("readFixture(%q): %v", name, err)
	}
	return string(data)
}
