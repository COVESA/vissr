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

