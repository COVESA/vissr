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

// ── Unit mapping tests ──────────────────────────────────────────────────────

func TestParseSDL_UnitPopulated(t *testing.T) {
	sdl := readFixture(t, "vehicle.graphql")
	roots, _, err := ParseSDL(sdl)
	if err != nil {
		t.Fatal(err)
	}
	speed := findNode(roots[0], "Speed")
	if speed == nil {
		t.Fatal("Speed not found")
	}
	if speed.Unit != "km/h" {
		t.Errorf("Speed.Unit = %q, want km/h", speed.Unit)
	}
}

func TestParseSDL_UnitNormalised(t *testing.T) {
	sdl := `
type T @vspec(element: BRANCH, fqn: "T") {
  Temp: Float @vspec(element: SENSOR, fqn: "T.Temp") @unit(value: "DEGREES_CELSIUS")
}
`
	roots, _, err := ParseSDL(sdl)
	if err != nil {
		t.Fatal(err)
	}
	temp := findNode(roots[0], "Temp")
	if temp == nil {
		t.Fatal("Temp not found")
	}
	if temp.Unit != "celsius" {
		t.Errorf("Temp.Unit = %q, want celsius", temp.Unit)
	}
}

func TestNormalizeUnit(t *testing.T) {
	cases := []struct{ in, want string }{
		{"KM_PER_HOUR", "km/h"},
		{"M_PER_S", "m/s"},
		{"DEGREES_CELSIUS", "celsius"},
		{"PERCENT", "percent"},
		{"km/h", "km/h"},   // already a standard string — pass through
		{"unknown", "unknown"},
	}
	for _, tc := range cases {
		if got := normalizeUnit(tc.in); got != tc.want {
			t.Errorf("normalizeUnit(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// ── Default value tests ─────────────────────────────────────────────────────

func TestParseSDL_DefaultValuePopulated(t *testing.T) {
	sdl := readFixture(t, "vehicle.graphql")
	roots, _, err := ParseSDL(sdl)
	if err != nil {
		t.Fatal(err)
	}
	mileage := findNode(roots[0], "TripMileage")
	if mileage == nil {
		t.Fatal("TripMileage not found")
	}
	if mileage.DefaultValue != "0" {
		t.Errorf("TripMileage.DefaultValue = %q, want 0", mileage.DefaultValue)
	}
}

func TestParseSDL_DefaultValueString(t *testing.T) {
	sdl := readFixture(t, "vehicle.graphql")
	roots, _, err := ParseSDL(sdl)
	if err != nil {
		t.Fatal(err)
	}
	isEnabled := findNode(roots[0], "IsEnabled")
	if isEnabled == nil {
		t.Fatal("IsEnabled not found")
	}
	if isEnabled.DefaultValue != "false" {
		t.Errorf("IsEnabled.DefaultValue = %q, want false", isEnabled.DefaultValue)
	}
}

func TestParseSDL_NoDefaultValue(t *testing.T) {
	sdl := readFixture(t, "vehicle.graphql")
	roots, _, err := ParseSDL(sdl)
	if err != nil {
		t.Fatal(err)
	}
	speed := findNode(roots[0], "Speed")
	if speed == nil {
		t.Fatal("Speed not found")
	}
	if speed.DefaultValue != "" {
		t.Errorf("Speed.DefaultValue = %q, want empty", speed.DefaultValue)
	}
}

// ── Allowed values tests ─────────────────────────────────────────────────────

func TestParseSDL_AllowedValuesFromEnum(t *testing.T) {
	sdl := readFixture(t, "vehicle.graphql")
	roots, _, err := ParseSDL(sdl)
	if err != nil {
		t.Fatal(err)
	}
	gear := findNode(roots[0], "CurrentGear")
	if gear == nil {
		t.Fatal("CurrentGear not found")
	}
	if int(gear.Allowed) != 4 {
		t.Errorf("CurrentGear.Allowed = %d, want 4", gear.Allowed)
	}
	want := map[string]bool{"Park": true, "Reverse": true, "Neutral": true, "Drive": true}
	for _, v := range gear.AllowedDef {
		if !want[v] {
			t.Errorf("unexpected allowed value %q", v)
		}
	}
	if len(gear.AllowedDef) != 4 {
		t.Errorf("len(AllowedDef) = %d, want 4", len(gear.AllowedDef))
	}
}

func TestParseSDL_AllowedOriginalNames(t *testing.T) {
	sdl := `
enum Dir { NORTH @vspec(originalName: "North") SOUTH @vspec(originalName: "South") }
type Nav @vspec(element: BRANCH, fqn: "Nav") {
  Heading: Dir @vspec(element: SENSOR, fqn: "Nav.Heading")
}
`
	roots, _, err := ParseSDL(sdl)
	if err != nil {
		t.Fatal(err)
	}
	heading := findNode(roots[0], "Heading")
	if heading == nil {
		t.Fatal("Heading not found")
	}
	if len(heading.AllowedDef) != 2 {
		t.Fatalf("AllowedDef len = %d, want 2", len(heading.AllowedDef))
	}
	if heading.AllowedDef[0] != "North" || heading.AllowedDef[1] != "South" {
		t.Errorf("AllowedDef = %v, want [North South]", heading.AllowedDef)
	}
}

func TestParseSDL_ScalarHasNoAllowed(t *testing.T) {
	sdl := readFixture(t, "vehicle.graphql")
	roots, _, err := ParseSDL(sdl)
	if err != nil {
		t.Fatal(err)
	}
	speed := findNode(roots[0], "Speed")
	if speed == nil {
		t.Fatal("Speed not found")
	}
	if len(speed.AllowedDef) != 0 {
		t.Errorf("Speed.AllowedDef = %v, want empty", speed.AllowedDef)
	}
}

// ── Instance tag expansion tests ─────────────────────────────────────────────

func TestInstanceTag_2D_ExpandsCorrectInstances(t *testing.T) {
	sdl := readFixture(t, "cabin.graphql")
	roots, _, err := ParseSDL(sdl)
	if err != nil {
		t.Fatalf("ParseSDL cabin.graphql: %v", err)
	}
	seat := findNode(roots[0], "Seat")
	if seat == nil {
		t.Fatal("Seat node not found")
	}
	// Seat must have Row1 and Row2 as children (not IsOccupied/IsBelted/Position directly)
	if int(seat.Children) != 2 {
		t.Errorf("Seat.Children = %d, want 2 (Row1, Row2)", seat.Children)
	}
	row1 := findNode(seat, "Row1")
	if row1 == nil {
		t.Fatal("Seat.Row1 not found")
	}
	if row1.NodeType != utils.BRANCH {
		t.Errorf("Row1.NodeType = %q, want branch", row1.NodeType)
	}
	// Row1 must have DriverSide and PassengerSide
	if int(row1.Children) != 2 {
		t.Errorf("Row1.Children = %d, want 2 (DriverSide, PassengerSide)", row1.Children)
	}
}

func TestInstanceTag_2D_ChildrenClonedPerInstance(t *testing.T) {
	sdl := readFixture(t, "cabin.graphql")
	roots, _, err := ParseSDL(sdl)
	if err != nil {
		t.Fatal(err)
	}
	// Every leaf instance should have IsOccupied, IsBelted, Position
	for _, rowName := range []string{"Row1", "Row2"} {
		for _, sideName := range []string{"DriverSide", "PassengerSide"} {
			seat := findNode(roots[0], "Seat")
			row := findNode(seat, rowName)
			if row == nil {
				t.Fatalf("%s not found under Seat", rowName)
			}
			side := findNode(row, sideName)
			if side == nil {
				t.Fatalf("%s not found under %s", sideName, rowName)
			}
			for _, childName := range []string{"IsOccupied", "IsBelted", "Position"} {
				if findNode(side, childName) == nil {
					t.Errorf("Seat.%s.%s.%s not found", rowName, sideName, childName)
				}
			}
		}
	}
}

func TestInstanceTag_2D_SignalPropertiesPreserved(t *testing.T) {
	sdl := readFixture(t, "cabin.graphql")
	roots, _, err := ParseSDL(sdl)
	if err != nil {
		t.Fatal(err)
	}
	seat := findNode(roots[0], "Seat")
	row1 := findNode(seat, "Row1")
	driver := findNode(row1, "DriverSide")
	pos := findNode(driver, "Position")
	if pos == nil {
		t.Fatal("Position not found under Row1.DriverSide")
	}
	if pos.NodeType != utils.ACTUATOR {
		t.Errorf("Position.NodeType = %q, want actuator", pos.NodeType)
	}
	if pos.Min != "0" || pos.Max != "100" {
		t.Errorf("Position range = [%q, %q], want [0, 100]", pos.Min, pos.Max)
	}
}

func TestInstanceTag_2D_ClonesAreIndependent(t *testing.T) {
	sdl := readFixture(t, "cabin.graphql")
	roots, _, err := ParseSDL(sdl)
	if err != nil {
		t.Fatal(err)
	}
	seat := findNode(roots[0], "Seat")
	row1 := findNode(seat, "Row1")
	row2 := findNode(seat, "Row2")
	driver1 := findNode(row1, "DriverSide")
	driver2 := findNode(row2, "DriverSide")
	pos1 := findNode(driver1, "Position")
	pos2 := findNode(driver2, "Position")
	if pos1 == nil || pos2 == nil {
		t.Fatal("Position not found in one or both instances")
	}
	if pos1 == pos2 {
		t.Error("Row1 and Row2 share the same Position pointer — deep clone failed")
	}
}

func TestInstanceTag_2D_ParentLinksCorrect(t *testing.T) {
	sdl := readFixture(t, "cabin.graphql")
	roots, _, err := ParseSDL(sdl)
	if err != nil {
		t.Fatal(err)
	}
	seat := findNode(roots[0], "Seat")
	row1 := findNode(seat, "Row1")
	driver := findNode(row1, "DriverSide")
	if driver.Parent == nil || driver.Parent.Name != "Row1" {
		t.Errorf("DriverSide.Parent = %v, want Row1", driver.Parent)
	}
	pos := findNode(driver, "Position")
	if pos.Parent == nil || pos.Parent.Name != "DriverSide" {
		t.Errorf("Position.Parent = %v, want DriverSide", pos.Parent)
	}
}

func TestInstanceTag_1D_ExpandsCorrectInstances(t *testing.T) {
	sdl := readFixture(t, "cabin.graphql")
	roots, _, err := ParseSDL(sdl)
	if err != nil {
		t.Fatal(err)
	}
	door := findNode(roots[0], "Door")
	if door == nil {
		t.Fatal("Door node not found")
	}
	// Door must have Row1, Row2, Row3
	if int(door.Children) != 3 {
		t.Errorf("Door.Children = %d, want 3 (Row1, Row2, Row3)", door.Children)
	}
	for _, row := range []string{"Row1", "Row2", "Row3"} {
		n := findNode(door, row)
		if n == nil {
			t.Errorf("Door.%s not found", row)
		}
	}
}

func TestInstanceTag_1D_ChildrenCloned(t *testing.T) {
	sdl := readFixture(t, "cabin.graphql")
	roots, _, err := ParseSDL(sdl)
	if err != nil {
		t.Fatal(err)
	}
	door := findNode(roots[0], "Door")
	for _, row := range []string{"Row1", "Row2", "Row3"} {
		rowNode := findNode(door, row)
		if rowNode == nil {
			t.Fatalf("Door.%s not found", row)
		}
		for _, childName := range []string{"IsOpen", "IsLocked"} {
			if findNode(rowNode, childName) == nil {
				t.Errorf("Door.%s.%s not found", row, childName)
			}
		}
	}
}

// ── screaming2Pascal and dimensionCombinations helpers ───────────────────────

func TestScreaming2Pascal(t *testing.T) {
	cases := []struct{ in, want string }{
		{"ROW1", "Row1"},
		{"DRIVER_SIDE", "DriverSide"},
		{"PASSENGER_SIDE", "PassengerSide"},
		{"LEFT", "Left"},
		{"RIGHT", "Right"},
		{"MIDDLE", "Middle"},
	}
	for _, tc := range cases {
		if got := screaming2Pascal(tc.in); got != tc.want {
			t.Errorf("screaming2Pascal(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestDimensionCombinations_2D(t *testing.T) {
	dims := [][]dimValue{
		{{origName: "Row1"}, {origName: "Row2"}},
		{{origName: "DriverSide"}, {origName: "PassengerSide"}},
	}
	combos := dimensionCombinations(dims)
	if len(combos) != 4 {
		t.Fatalf("expected 4 combinations, got %d", len(combos))
	}
	want := [][2]string{{"Row1", "DriverSide"}, {"Row1", "PassengerSide"}, {"Row2", "DriverSide"}, {"Row2", "PassengerSide"}}
	for i, combo := range combos {
		if combo[0].origName != want[i][0] || combo[1].origName != want[i][1] {
			t.Errorf("combo[%d] = [%s,%s], want [%s,%s]", i, combo[0].origName, combo[1].origName, want[i][0], want[i][1])
		}
	}
}

func TestDimensionCombinations_1D(t *testing.T) {
	dims := [][]dimValue{
		{{origName: "Row1"}, {origName: "Row2"}, {origName: "Row3"}},
	}
	combos := dimensionCombinations(dims)
	if len(combos) != 3 {
		t.Fatalf("expected 3 combinations, got %d", len(combos))
	}
}

func TestDeepClone_IsIndependent(t *testing.T) {
	orig := utils.NewBranchNode("Parent")
	child := utils.NewSignalNode("Speed", utils.SENSOR, "float", "speed", "0", "250", "km/h")
	appendChild(orig, child)

	clone := deepClone(orig)
	if clone == orig {
		t.Fatal("clone is same pointer as orig")
	}
	if clone.Child[0] == orig.Child[0] {
		t.Error("clone child is same pointer as orig child")
	}
	if clone.Child[0].Name != "Speed" {
		t.Errorf("clone child name = %q, want Speed", clone.Child[0].Name)
	}
	if clone.Child[0].Max != "250" {
		t.Errorf("clone child Max = %q, want 250", clone.Child[0].Max)
	}
	if clone.Child[0].Parent != clone {
		t.Error("clone child Parent does not point to clone")
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
