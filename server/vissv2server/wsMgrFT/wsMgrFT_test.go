/**
* (C) 2026 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE
* file in this repository.
*
* ----------------------------------------------------------------------------
*
* Tests for the Tier-2 bug-fixes applied to wsMgrFT. Eight bugs were
* fixed in this PR; this file covers the ones that can be exercised
* without a live WebSocket connection or a real file system swap.
*
*   - sanitizeFileName              (bug 1: path traversal via Name)
*   - getDataResponseDl OOB guards  (bug 2: slice OOB on short packets)
*   - getDataResponseUl OOB guards  (bug 2 mirror + bug 3: UID short slice)
*   - getDataSessionIndex          (bug 5: index never marked taken)
*   - per-session chunk cache       (bug 6: cross-session corruption)
**/
package wsMgrFT

import (
	"os"
	"testing"

	"github.com/covesa/vissr/utils"
)

func TestMain(m *testing.M) {
	utils.InitLog("wsMgrFT-test.log", os.TempDir(), false, "error")
	os.Exit(m.Run())
}

// TestSanitizeFileName pins the bug-1 fix: client-supplied file
// names with path traversal or directory separators must be rejected
// or stripped before being concatenated with the configured Path.
func TestSanitizeFileName(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"firmware.bin", "firmware.bin", false},
		{"a-b_c.123", "a-b_c.123", false},

		// Direct traversal attempts.
		{"../../etc/passwd", "", true},
		{"..", "", true},
		{".", "", true},
		{"", "", true},
		{"/", "", true},

		// Absolute paths.
		{"/etc/passwd", "", true},
		{`C:\Windows\System32\config`, "", true},

		// Slashes / backslashes anywhere.
		{"foo/bar", "", true},
		{`foo\bar`, "", true},
		{"sub/../firmware.bin", "", true}, // Clean would normalize to "firmware.bin" but we reject the ..
	}
	for _, tc := range cases {
		got, err := sanitizeFileName(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("sanitizeFileName(%q) = %q, nil; want error", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("sanitizeFileName(%q) returned error: %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("sanitizeFileName(%q) = %q; want %q", tc.in, got, tc.want)
		}
	}
}

// TestGetDataResponseDl_ShortPacketDoesNotPanic exercises the bug-2
// fix: download requests shorter than DL_HEADER_SIZE used to slice
// req[5:9] / req[9:10] / req[10:] without bounds checks, panicking
// the WsMgrFTInit goroutine. The handler must now respond with the
// terminate-session error byte.
func TestGetDataResponseDl_ShortPacketDoesNotPanic(t *testing.T) {
	// Initialise the file-transfer cache so findFileTransferCacheIndex
	// can run safely even though we don't expect it to match.
	fileTransferCache = initFileTransferCache()

	cases := [][]byte{
		{0, 0, 0, 0, 0, 0, 0},                         // len 7 — used to OOB at req[5:9]
		{0, 0, 0, 0, 0, 0, 0, 0},                      // len 8 — same
		{0, 0, 0, 0, 0, 0, 0, 0, 0},                   // len 9 — used to OOB at req[9:10]
		{1, 2, 3, 4, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, // len 15 — sane size; should reach findFileTransferCacheIndex
	}
	for _, req := range cases {
		resp := getDataResponseDl(req)
		if len(resp) != 6 {
			t.Errorf("getDataResponseDl(len=%d) returned response of len %d; want 6", len(req), len(resp))
		}
	}
}

// TestGetDataResponseUl_ShortPacketDoesNotPanic exercises the bug-3
// fix: upload-status requests shorter than UL_HEADER_SIZE used to
// slice req[:4] / req[4] / req[5] without bounds checks, panicking
// the goroutine.
func TestGetDataResponseUl_ShortPacketDoesNotPanic(t *testing.T) {
	fileTransferCache = initFileTransferCache()

	cases := [][]byte{
		{},                  // len 0
		{0, 0, 0},           // len 3 — used to OOB at req[:4]
		{0, 0, 0, 0},        // len 4 — used to OOB at req[4]
		{0, 0, 0, 0, 0},     // len 5 — used to OOB at req[5]
		{0, 0, 0, 0, 0, 0},  // len 6 — minimum valid; should reach cache lookup
	}
	for _, req := range cases {
		// Must not panic.
		_ = getDataResponseUl(req, 0)
	}
}

// TestGetDataSessionIndex_MarksSlotsTaken pins the bug-5 fix: the
// original returned 0, 0, 0... because it never marked the slot
// taken. After the fix, consecutive calls must return 0, 1, 2,
// ... up to MAXSESSIONS-1, then -1.
func TestGetDataSessionIndex_MarksSlotsTaken(t *testing.T) {
	// Reset state.
	for i := range sessionList {
		sessionList[i] = false
	}
	defer func() {
		for i := range sessionList {
			sessionList[i] = false
		}
	}()

	for want := 0; want < MAXSESSIONS; want++ {
		got := getDataSessionIndex()
		if got != want {
			t.Fatalf("call #%d: getDataSessionIndex() = %d; want %d", want, got, want)
		}
	}
	// All slots full → -1.
	if got := getDataSessionIndex(); got != -1 {
		t.Errorf("after %d allocations: getDataSessionIndex() = %d; want -1", MAXSESSIONS, got)
	}

	// Returning a slot makes it available again.
	returnDataSessionIndex(3)
	if got := getDataSessionIndex(); got != 3 {
		t.Errorf("after returning slot 3: getDataSessionIndex() = %d; want 3", got)
	}
}

// TestReturnDataSessionIndex_BoundsDefensive confirms the bounds
// check on the returnDataSessionIndex helper (added as part of the
// bug-5 fix). An out-of-range index must not panic.
func TestReturnDataSessionIndex_BoundsDefensive(t *testing.T) {
	returnDataSessionIndex(-1)         // must not panic
	returnDataSessionIndex(MAXSESSIONS) // must not panic
	returnDataSessionIndex(1000)        // must not panic
}

// TestChunkDataCache_PerSession pins the bug-6 fix: the per-session
// chunk cache must isolate sessions from each other. Previously a
// single package-global ChunkDataCache served all 10 sessions, so
// session B's writeChunkData would clobber session A's resend
// buffer.
func TestChunkDataCache_PerSession(t *testing.T) {
	// Wipe to a known state.
	for i := range chunkDataCache {
		chunkDataCache[i] = ChunkDataCache{}
	}

	// Session 0 caches a chunk with messageNo=7.
	writeChunkData(7, byte(0), []byte{0, 0, 0, 4}, []byte{0xAA, 0xBB, 0xCC, 0xDD}, 0)
	// Session 1 caches a different chunk with messageNo=7.
	writeChunkData(7, byte(1), []byte{0, 0, 0, 2}, []byte{0x11, 0x22}, 1)

	// Reading session 0's cache must return session 0's data, not
	// session 1's. Previously these would have collided.
	lastMsg0, _, chunk0 := readChunkData(7, 0)
	if lastMsg0 != byte(0) {
		t.Errorf("session 0 lastMsg = %d; want 0", lastMsg0)
	}
	if len(chunk0) != 4 || chunk0[0] != 0xAA {
		t.Errorf("session 0 chunk = %v; want [AA BB CC DD]", chunk0)
	}

	lastMsg1, _, chunk1 := readChunkData(7, 1)
	if lastMsg1 != byte(1) {
		t.Errorf("session 1 lastMsg = %d; want 1", lastMsg1)
	}
	if len(chunk1) != 2 || chunk1[0] != 0x11 {
		t.Errorf("session 1 chunk = %v; want [11 22]", chunk1)
	}
}

// TestReadChunkData_WrongMessageNoReturnsEmpty confirms the resend
// guard still works: a request for a messageNo that doesn't match
// the cached one returns no data.
func TestReadChunkData_WrongMessageNoReturnsEmpty(t *testing.T) {
	for i := range chunkDataCache {
		chunkDataCache[i] = ChunkDataCache{}
	}
	writeChunkData(7, byte(0), []byte{0, 0, 0, 4}, []byte{0xAA, 0xBB, 0xCC, 0xDD}, 0)
	_, _, chunk := readChunkData(8, 0) // wrong messageNo
	if chunk != nil {
		t.Errorf("readChunkData with wrong messageNo returned %v; want nil", chunk)
	}
}

// TestReadChunkData_OutOfRangeSessionIndexIsSafe confirms the bounds
// check on the per-session cache helpers.
func TestReadChunkData_OutOfRangeSessionIndexIsSafe(t *testing.T) {
	_, _, c1 := readChunkData(0, -1)         // must not panic
	_, _, c2 := readChunkData(0, MAXSESSIONS) // must not panic
	if c1 != nil || c2 != nil {
		t.Errorf("out-of-range session index should return nil chunk; got %v / %v", c1, c2)
	}
	writeChunkData(0, 0, nil, []byte{1}, -1)         // must not panic
	writeChunkData(0, 0, nil, []byte{1}, MAXSESSIONS) // must not panic
}
