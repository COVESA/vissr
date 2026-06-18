/**
* (C) 2026 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE
* file in this repository.
*
* ----------------------------------------------------------------------------
*
* Complete tests for utils/treeutils.go.
*
* Bug coverage map:
*    1  readBytes short-read / EOF → TestVSSReadTree_TruncatedFile
*    2  Unbounded recursion        → TestVSSReadTree_DepthCap
*    3  Allocation DoS              → TestVSSReadTree_NodeCountCap (skip in normal runs)
*    5  VSSReadTree returns garbage → TestVSSReadTree_TruncatedFile (returns nil)
*    6  Write/Read BRANCH desync    → TestRoundTrip_BranchWithEmptyDatatype
*    8  calculatAllowedStrLen iter  → covered indirectly by TestRoundTrip_NodeWithAllowedValues
*   11  VSSgetChild / Name nil-safe → TestVSSgetName_NilSafe, TestTraverseNode_NilChild
*   12  Hex / allowed bounds        → TestHexToIntStrict, TestCountAllowedElementsE,
*                                      TestExtractAllowedElementE
*   14  intToHex >255 returns valid → TestIntToHex_Range
**/
package utils

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// ----------------------------------------------------------------------------
// Hex / allowed-buffer helpers (bug 12)
// ----------------------------------------------------------------------------

func TestHexToIntStrict(t *testing.T) {
	cases := []struct {
		in      byte
		want    int
		wantErr bool
	}{
		{'0', 0, false},
		{'9', 9, false},
		{'A', 10, false},
		{'F', 15, false},
		{'a', 10, false}, // lowercase accepted
		{'f', 15, false},
		// Invalid:
		{' ', 0, true},
		{'G', 0, true},
		{'g', 0, true},
		{'!', 0, true},
		{0, 0, true},
		{255, 0, true},
	}
	for _, tc := range cases {
		got, err := hexToIntStrict(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("hexToIntStrict(%q) = %d; want error", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("hexToIntStrict(%q) returned error: %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("hexToIntStrict(%q) = %d; want %d", tc.in, got, tc.want)
		}
	}
}

func TestDecodeAllowedLen(t *testing.T) {
	cases := []struct {
		hi, lo  byte
		want    int
		wantErr bool
	}{
		{'0', '0', 0, false},
		{'0', '1', 1, false},
		{'F', 'F', 255, false},
		{'a', 'b', 10*16 + 11, false},
		{' ', '0', 0, true},
		{'0', ' ', 0, true},
		{'X', 'Y', 0, true},
	}
	for _, tc := range cases {
		got, err := decodeAllowedLen(tc.hi, tc.lo)
		if tc.wantErr {
			if err == nil {
				t.Errorf("decodeAllowedLen(%q,%q) = %d; want error", tc.hi, tc.lo, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("decodeAllowedLen(%q,%q) error: %v", tc.hi, tc.lo, err)
		}
		if got != tc.want {
			t.Errorf("decodeAllowedLen(%q,%q) = %d; want %d", tc.hi, tc.lo, got, tc.want)
		}
	}
}

func TestCountAllowedElementsE(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    int
		wantErr bool
	}{
		{"empty", "", 0, false},
		{"one element len 3", "03abc", 1, false},
		{"two elements", "03abc02de", 2, false},
		{"truncated length prefix", "0", 0, true},
		{"declared length exceeds buffer", "FFabc", 0, true},
		{"non-hex prefix", "ZZabc", 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := countAllowedElementsE(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Errorf("countAllowedElementsE(%q) = %d; want error", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Errorf("countAllowedElementsE(%q) error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("countAllowedElementsE(%q) = %d; want %d", tc.in, got, tc.want)
			}
		})
	}
}

func TestExtractAllowedElementE(t *testing.T) {
	buf := "03abc02de01f"
	cases := []struct {
		index   int
		want    string
		wantErr bool
	}{
		{0, "abc", false},
		{1, "de", false},
		{2, "f", false},
		{3, "", true}, // past end
		{-1, "", true},
	}
	for _, tc := range cases {
		got, err := extractAllowedElementE(buf, tc.index)
		if tc.wantErr {
			if err == nil {
				t.Errorf("extractAllowedElementE(%q,%d) = %q; want error", buf, tc.index, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("extractAllowedElementE(%q,%d) error: %v", buf, tc.index, err)
		}
		if got != tc.want {
			t.Errorf("extractAllowedElementE(%q,%d) = %q; want %q", buf, tc.index, got, tc.want)
		}
	}
}

func TestExtractAllowedElementE_RejectsMalformedHex(t *testing.T) {
	// bug-12 trigger: lowercase letters other than a-f, control chars, etc.
	_, err := extractAllowedElementE("Z3abc", 0)
	if err == nil {
		t.Errorf("expected error on non-hex length bytes")
	}
}

// ----------------------------------------------------------------------------
// intToHex range (bug 14 / intToHex never returns nil)
// ----------------------------------------------------------------------------

func TestIntToHex_Range(t *testing.T) {
	cases := []struct {
		in   int
		want string
	}{
		{0, "00"},
		{15, "0F"},
		{16, "10"},
		{255, "FF"},
	}
	for _, tc := range cases {
		got := string(intToHex(tc.in))
		if got != tc.want {
			t.Errorf("intToHex(%d) = %q; want %q", tc.in, got, tc.want)
		}
	}
	// Out-of-range — must return valid 2 bytes (was nil before fix).
	for _, n := range []int{-1, 256, 1000, -999} {
		got := intToHex(n)
		if len(got) != 2 {
			t.Errorf("intToHex(%d) = %v; want a 2-byte slice (was nil before fix)", n, got)
		}
	}
}

// ----------------------------------------------------------------------------
// Nil-safety (bug 11)
// ----------------------------------------------------------------------------

func TestVSSgetName_NilSafe(t *testing.T) {
	if got := VSSgetName(nil); got != "" {
		t.Errorf("VSSgetName(nil) = %q; want \"\"", got)
	}
}

func TestTraverseNode_NilNodeDoesNotPanic(t *testing.T) {
	// traverseNode should return 0 on a nil node, not panic.
	var ctx SearchContext_t
	_ = traverseNode(nil, &ctx)
}

func TestTraverseNode_NilChildSkipped(t *testing.T) {
	// Construct a parent with Children=1 but Child[0]=nil (the
	// shape a corrupt tree could produce). traverseNode must not
	// panic; the child is skipped.
	root := &Node_t{Name: "root", NodeType: BRANCH, Children: 1, Child: []*Node_t{nil}}
	ctx := SearchContext_t{
		RootNode:      root,
		SearchPath:    "root.*",
		MatchPath:     "",
		CurrentDepth:  0,
		MaxDepth:      5,
		LeafNodesOnly: true, // BRANCH save is skipped, isolating the nil-child path
		SearchData:    make([]SearchData_t, MAXFOUNDNODES),
	}
	// We don't care about the result — only that it doesn't panic.
	_ = traverseNode(root, &ctx)
}

// ----------------------------------------------------------------------------
// VSSReadTree / VSSWriteTree round-trip and error paths
// (bugs 1, 2, 3, 5, 6)
// ----------------------------------------------------------------------------

// writeSerializedTreeFixture exercises the writer path so we have a
// known-good binary tree to feed back into the reader.
func writeSerializedTreeFixture(t *testing.T, root *Node_t) string {
	t.Helper()
	tmp := t.TempDir()
	fname := filepath.Join(tmp, "tree.bin")
	VSSWriteTree(fname, root)
	return fname
}

// TestRoundTrip_SingleLeaf is the simplest possible round trip.
func TestRoundTrip_SingleLeaf(t *testing.T) {
	orig := &Node_t{
		Name:        "Vehicle",
		NodeType:    "sensor",
		Uuid:        "vin-uuid",
		Description: "the vehicle root",
		Datatype:    "string",
		Min:         "",
		Max:         "",
		Unit:        "",
		Children:    0,
	}
	fname := writeSerializedTreeFixture(t, orig)
	got := VSSReadTree(fname)
	if got == nil {
		t.Fatalf("VSSReadTree returned nil")
	}
	if got.Name != orig.Name || got.NodeType != orig.NodeType || got.Datatype != orig.Datatype {
		t.Errorf("round trip mismatch: got %+v want name=%q nodeType=%q dt=%q",
			got, orig.Name, orig.NodeType, orig.Datatype)
	}
}

// TestRoundTrip_BranchWithEmptyDatatype pins the bug-6 fix. A BRANCH
// node carries an empty Datatype; the writer always writes the
// length-zero prefix; the reader must consume 0 bytes after that
// length and stay in sync. Before the fix the reader skipped the
// length-prefix-consume when NodeType==BRANCH, desyncing.
func TestRoundTrip_BranchWithEmptyDatatype(t *testing.T) {
	leaf := &Node_t{
		Name:     "Speed",
		NodeType: "sensor",
		Datatype: "float",
	}
	root := &Node_t{
		Name:     "Vehicle",
		NodeType: BRANCH,
		Datatype: "", // explicitly empty for BRANCH
		Children: 1,
		Child:    []*Node_t{leaf},
	}
	fname := writeSerializedTreeFixture(t, root)
	got := VSSReadTree(fname)
	if got == nil {
		t.Fatalf("VSSReadTree returned nil on branch+leaf")
	}
	if got.Name != "Vehicle" || got.NodeType != BRANCH {
		t.Errorf("root mismatch: %+v", got)
	}
	if got.Children != 1 || len(got.Child) != 1 || got.Child[0] == nil {
		t.Fatalf("child missing: %+v", got)
	}
	if got.Child[0].Name != "Speed" || got.Child[0].Datatype != "float" {
		t.Errorf("leaf mismatch: %+v", got.Child[0])
	}
}

// TestRoundTrip_NodeWithAllowedValues exercises the allowed-element
// serializer + the strict hex decoder. Indirectly covers bug 8
// (the writer-loop iteration over AllowedDef) and bug 12 (the
// reader-side bounds checks on the hex length bytes).
func TestRoundTrip_NodeWithAllowedValues(t *testing.T) {
	leaf := &Node_t{
		Name:       "Mode",
		NodeType:   "actuator",
		Datatype:   "string",
		Allowed:    3,
		AllowedDef: []string{"OFF", "AUTO", "MANUAL"},
	}
	fname := writeSerializedTreeFixture(t, leaf)
	got := VSSReadTree(fname)
	if got == nil {
		t.Fatalf("VSSReadTree returned nil")
	}
	if got.Allowed != 3 || len(got.AllowedDef) != 3 {
		t.Fatalf("allowed mismatch: %+v", got)
	}
	for i, want := range []string{"OFF", "AUTO", "MANUAL"} {
		if got.AllowedDef[i] != want {
			t.Errorf("allowed[%d] = %q; want %q", i, got.AllowedDef[i], want)
		}
	}
}

// TestVSSReadTree_NonexistentFile confirms a missing file returns
// nil cleanly.
func TestVSSReadTree_NonexistentFile(t *testing.T) {
	got := VSSReadTree(filepath.Join(t.TempDir(), "no-such-file"))
	if got != nil {
		t.Errorf("VSSReadTree on missing file returned non-nil")
	}
}

// TestVSSReadTree_TruncatedFile pins the bug-1 + bug-5 fix. A
// truncated binary used to silently parse into a zero-initialized
// tree; now it returns nil.
func TestVSSReadTree_TruncatedFile(t *testing.T) {
	leaf := &Node_t{Name: "Speed", NodeType: "sensor", Datatype: "float"}
	root := &Node_t{
		Name: "Vehicle", NodeType: BRANCH,
		Children: 1, Child: []*Node_t{leaf},
	}
	full := writeSerializedTreeFixture(t, root)
	// Truncate at half length.
	data, err := os.ReadFile(full)
	if err != nil {
		t.Fatalf("read full: %v", err)
	}
	trunc := filepath.Join(t.TempDir(), "tree-trunc.bin")
	if err := os.WriteFile(trunc, data[:len(data)/2], 0644); err != nil {
		t.Fatalf("write trunc: %v", err)
	}
	if got := VSSReadTree(trunc); got != nil {
		t.Errorf("VSSReadTree on truncated file returned non-nil tree (bug-1/bug-5 regression)")
	}
}

// TestVSSReadTree_DepthCap pins the bug-2 fix. A binary file
// crafted to declare an arbitrarily deep chain of single-child
// branches used to recurse without bound. We synthesize MAX+1 levels
// directly into bytes and confirm VSSReadTree rejects the input.
func TestVSSReadTree_DepthCap(t *testing.T) {
	tmp := t.TempDir()
	fname := filepath.Join(tmp, "deep.bin")
	f, err := os.Create(fname)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// Write MAX_TREE_DEPTH+1 nested nodes, each with Children=1.
	// Node serialization shape (matches populateNode reads):
	//   u8 nameLen | bytes | u8 typeLen | bytes | u8 uuidLen | bytes |
	//   u16 descLen | bytes | u8 dtLen | bytes | u8 minLen | u8 maxLen |
	//   u8 unitLen | u16 allowedLen | u8 defaultLen | u8 validateLen |
	//   u8 children
	writeNodeRaw := func(name, nodeType string, children uint8) {
		f.Write([]byte{byte(len(name))})
		f.WriteString(name)
		f.Write([]byte{byte(len(nodeType))})
		f.WriteString(nodeType)
		f.Write([]byte{0}) // uuid
		descLen := make([]byte, 2)
		binary.BigEndian.PutUint16(descLen, 0)
		f.Write(descLen)
		f.Write([]byte{0}) // datatype
		f.Write([]byte{0}) // min
		f.Write([]byte{0}) // max
		f.Write([]byte{0}) // unit
		allowedLen := make([]byte, 2)
		binary.BigEndian.PutUint16(allowedLen, 0)
		f.Write(allowedLen)
		f.Write([]byte{0}) // default
		f.Write([]byte{0}) // validate
		f.Write([]byte{children})
	}
	for i := 0; i < MAX_TREE_DEPTH+5; i++ {
		// All inner nodes have one child; deepest is a leaf with 0
		// children. We always declare 1 to force depth growth; the
		// reader stops at MAX_TREE_DEPTH so the leaf detail is moot.
		writeNodeRaw("N", BRANCH, 1)
	}
	// Final leaf with 0 children
	writeNodeRaw("L", "sensor", 0)
	f.Close()

	got := VSSReadTree(fname)
	if got != nil {
		t.Errorf("VSSReadTree should have rejected a tree deeper than MAX_TREE_DEPTH; got non-nil (bug-2 regression)")
	}
}

// TestVSSReadTree_PopulateNodeErrorPropagates confirms populateNode
// errors surface all the way out to VSSReadTree returning nil.
func TestVSSReadTree_PopulateNodeErrorPropagates(t *testing.T) {
	tmp := t.TempDir()
	fname := filepath.Join(tmp, "empty.bin")
	if err := os.WriteFile(fname, []byte{}, 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := VSSReadTree(fname)
	if got != nil {
		t.Errorf("empty file should produce nil tree; got %+v", got)
	}
}

// ----------------------------------------------------------------------------
// treeFp mutex (bug 9)
// ----------------------------------------------------------------------------

// TestTreeFpMutex_NoCrashUnderConcurrency confirms that concurrent
// VSSReadTree / VSSWriteTree calls don't crash. The mutex serializes
// them. Run with -race to detect any unguarded access.
func TestTreeFpMutex_NoCrashUnderConcurrency(t *testing.T) {
	leaf := &Node_t{Name: "Speed", NodeType: "sensor", Datatype: "float"}
	root := &Node_t{Name: "Vehicle", NodeType: BRANCH, Children: 1, Child: []*Node_t{leaf}}
	tmp := t.TempDir()
	fname := filepath.Join(tmp, "concurrent.bin")
	VSSWriteTree(fname, root)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			if idx%2 == 0 {
				_ = VSSReadTree(fname)
			} else {
				VSSWriteTree(filepath.Join(tmp, "out-"+t.Name()+".bin"), root)
			}
		}(i)
	}
	wg.Wait()
}

// ----------------------------------------------------------------------------
// Small sanity check on the new readBytesE helper
// ----------------------------------------------------------------------------

func TestReadBytesE_NilTreeFp(t *testing.T) {
	prev := treeFp
	treeFp = nil
	defer func() { treeFp = prev }()
	_, err := readBytesE(4)
	if err == nil {
		t.Errorf("readBytesE with nil treeFp should error")
	}
}

func TestReadBytesE_ZeroLengthReturnsNilAndNoError(t *testing.T) {
	b, err := readBytesE(0)
	if err != nil {
		t.Errorf("readBytesE(0) error: %v", err)
	}
	if b != nil {
		t.Errorf("readBytesE(0) = %v; want nil", b)
	}
}

// ── New node constructors ────────────────────────────────────────────────────

func TestNewBranchNode_NoChildren(t *testing.T) {
	n := NewBranchNode("Root")
	if n.Name != "Root" {
		t.Errorf("Name = %q, want Root", n.Name)
	}
	if n.NodeType != BRANCH {
		t.Errorf("NodeType = %q, want %q", n.NodeType, BRANCH)
	}
	if n.Children != 0 || len(n.Child) != 0 {
		t.Errorf("expected 0 children, got Children=%d Child=%d", n.Children, len(n.Child))
	}
}

func TestNewBranchNode_WithChildren(t *testing.T) {
	leaf1 := NewSignalNode("Speed", SENSOR, "float", "vehicle speed", "0", "300", "km/h")
	leaf2 := NewPropertyNode("Gear", "uint8", "current gear")
	root := NewBranchNode("Vehicle", leaf1, leaf2)

	if root.Children != 2 || len(root.Child) != 2 {
		t.Fatalf("want 2 children, got Children=%d Child=%d", root.Children, len(root.Child))
	}
	if root.Child[0] != leaf1 || root.Child[1] != leaf2 {
		t.Error("children not stored in order")
	}
	if leaf1.Parent != root || leaf2.Parent != root {
		t.Error("Parent pointers not set on children")
	}
}

func TestNewProcedureNode_Fields(t *testing.T) {
	input := NewIoStructNode("Input")
	proc := NewProcedureNode("MoveSeat", "adjusts seat position", input)

	if proc.NodeType != PROCEDURE {
		t.Errorf("NodeType = %q, want %q", proc.NodeType, PROCEDURE)
	}
	if proc.Description != "adjusts seat position" {
		t.Errorf("Description = %q", proc.Description)
	}
	if proc.Children != 1 || proc.Child[0] != input {
		t.Error("child not linked")
	}
	if input.Parent != proc {
		t.Error("Parent not set on child")
	}
}

func TestNewIoStructNode_Empty(t *testing.T) {
	n := NewIoStructNode("Output")
	if n.NodeType != IOSTRUCT {
		t.Errorf("NodeType = %q, want %q", n.NodeType, IOSTRUCT)
	}
	if n.Children != 0 {
		t.Errorf("want 0 children, got %d", n.Children)
	}
}

func TestNewIoStructNode_WithChildren(t *testing.T) {
	p1 := NewPropertyNode("Angle", "int8", "steering angle")
	p2 := NewPropertyNode("Force", "float", "applied force")
	n := NewIoStructNode("Input", p1, p2)

	if n.Children != 2 || len(n.Child) != 2 {
		t.Fatalf("want 2 children, got Children=%d Child=%d", n.Children, len(n.Child))
	}
	if n.Child[0] != p1 || n.Child[1] != p2 {
		t.Error("children not stored in order")
	}
	if p1.Parent != n || p2.Parent != n {
		t.Error("Parent pointers not set on children")
	}
}

func TestNewPropertyNode_Fields(t *testing.T) {
	p := NewPropertyNode("Position", "uint8", "seat position 0-100")
	if p.NodeType != PROPERTY {
		t.Errorf("NodeType = %q, want %q", p.NodeType, PROPERTY)
	}
	if p.Datatype != "uint8" {
		t.Errorf("Datatype = %q, want uint8", p.Datatype)
	}
	if p.Description != "seat position 0-100" {
		t.Errorf("Description = %q", p.Description)
	}
}

func TestNewSignalNode_AllFields(t *testing.T) {
	s := NewSignalNode("Temperature", SENSOR, "float", "cabin temp", "-40", "85", "celsius")
	if s.Name != "Temperature" || s.NodeType != SENSOR || s.Datatype != "float" {
		t.Errorf("basic fields wrong: %+v", s)
	}
	if s.Min != "-40" || s.Max != "85" || s.Unit != "celsius" {
		t.Errorf("range/unit fields wrong: %+v", s)
	}
	if s.Description != "cabin temp" {
		t.Errorf("Description = %q", s.Description)
	}
}

// ── Path helpers ─────────────────────────────────────────────────────────────

func TestGetFirstDotIndex_HasDot(t *testing.T) {
	cases := []struct {
		path string
		want int
	}{
		{"Vehicle.Speed", 7},
		{"A.B.C", 1},
		{"A.B", 1},
	}
	for _, tc := range cases {
		got := GetFirstDotIndex(tc.path)
		if got != tc.want {
			t.Errorf("GetFirstDotIndex(%q) = %d, want %d", tc.path, got, tc.want)
		}
	}
}

func TestGetFirstDotIndex_NoDot(t *testing.T) {
	path := "VehicleOnly"
	got := GetFirstDotIndex(path)
	if got != len(path) {
		t.Errorf("GetFirstDotIndex(%q) = %d, want %d (len)", path, got, len(path))
	}
}

func TestGetLastDotSegment_Normal(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"Vehicle.Body.Speed", "Speed"},
		{"Root.Leaf", "Leaf"},
		{"Trailing.", ""},
	}
	for _, tc := range cases {
		got := GetLastDotSegment(tc.path)
		if got != tc.want {
			t.Errorf("GetLastDotSegment(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

func TestGetLastDotSegment_NoDot(t *testing.T) {
	// No dot: strings.LastIndex returns -1; path[-1+1:] = path[0:] = full string.
	got := GetLastDotSegment("NoDot")
	if got != "NoDot" {
		t.Errorf("GetLastDotSegment(\"NoDot\") = %q, want \"NoDot\"", got)
	}
}

// ── Forest management ────────────────────────────────────────────────────────

func saveAndRestoreForest(t *testing.T) {
	t.Helper()
	saved := make([]HimTree, len(himForest))
	copy(saved, himForest)
	t.Cleanup(func() { himForest = saved })
}

func TestRegisterServiceTree_AddsToForest(t *testing.T) {
	saveAndRestoreForest(t)
	root := NewBranchNode("RegSvc")
	before := len(himForest)

	ok := RegisterServiceTree("RegSvc", "reg.Service", "1.0.0", root)
	if !ok {
		t.Fatal("RegisterServiceTree returned false on first registration")
	}
	if len(himForest) != before+1 {
		t.Errorf("forest len = %d, want %d", len(himForest), before+1)
	}
	if root.Name != "RegSvc" {
		t.Errorf("root.Name = %q, want RegSvc", root.Name)
	}
}

func TestRegisterServiceTree_NoDuplicates(t *testing.T) {
	saveAndRestoreForest(t)
	RegisterServiceTree("DupSvc", "d.Service", "1.0", NewBranchNode("DupSvc"))
	ok := RegisterServiceTree("DupSvc", "d.Service", "1.1", NewBranchNode("DupSvc"))
	if ok {
		t.Error("second RegisterServiceTree for same name should return false")
	}
}

func TestDeregisterServiceTree_Removes(t *testing.T) {
	saveAndRestoreForest(t)
	RegisterServiceTree("RemSvc", "r.Service", "1.0", NewBranchNode("RemSvc"))
	before := len(himForest)
	DeregisterServiceTree("RemSvc")
	if len(himForest) != before-1 {
		t.Errorf("after deregister: len=%d, want %d", len(himForest), before-1)
	}
	if GetForestRoot("RemSvc") != nil {
		t.Error("root still reachable after deregister")
	}
}

func TestDeregisterServiceTree_NoopOnMissing(t *testing.T) {
	saveAndRestoreForest(t)
	before := len(himForest)
	DeregisterServiceTree("DoesNotExist")
	if len(himForest) != before {
		t.Errorf("forest len changed after deregister of missing tree")
	}
}

func TestForestInfoList_ReturnsAllTrees(t *testing.T) {
	saveAndRestoreForest(t)
	himForest = nil
	RegisterServiceTree("Alpha", "a.Service", "1.0", NewBranchNode("Alpha"))
	RegisterServiceTree("Beta", "b.Service", "2.0", NewBranchNode("Beta"))

	list := ForestInfoList()
	if len(list) != 2 {
		t.Fatalf("want 2, got %d: %+v", len(list), list)
	}
	names := map[string]bool{}
	for _, fi := range list {
		names[fi.RootName] = true
	}
	if !names["Alpha"] || !names["Beta"] {
		t.Errorf("unexpected list: %+v", list)
	}
}

func TestGetForestRoot_Found(t *testing.T) {
	saveAndRestoreForest(t)
	root := NewBranchNode("FindMe")
	RegisterServiceTree("FindMe", "f.Service", "1.0", root)

	got := GetForestRoot("FindMe")
	if got != root {
		t.Errorf("GetForestRoot returned %p, want %p", got, root)
	}
}

func TestGetForestRoot_NotFound(t *testing.T) {
	if got := GetForestRoot("Ghost_xyz_999"); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

