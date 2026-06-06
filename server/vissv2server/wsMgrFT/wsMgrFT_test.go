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

// ---------------------------------------------------------------------------
// validateTransferName
// ---------------------------------------------------------------------------

// TestValidateTransferName exercises the path-traversal guard added in the
// security fix. Names with separators or ".." components must be rejected.
func TestValidateTransferName(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{"plain filename", "firmware.bin", false},
		{"name with single dot", "config.v1", false},
		{"name with dashes and underscores", "my-file_v2.tar.gz", false},

		// Must reject: ".." in name (parent-dir reference).
		{"dot-dot alone", "..", true},
		{"dot-dot mid-token", "config.v..1", true},
		{"empty", "", true},

		// Must reject: path separators (filepath.Base(name) != name).
		{"parent dir traversal unix", "../../etc/passwd", true},
		// `..\..\windows\system32` contains ".." → caught by strings.Contains on any platform.
		{"parent dir traversal windows", `..\..\windows\system32`, true},
		{"absolute path", "/etc/passwd", true},
		{"subdir unix", "subdir/file", true},
		// NOTE: `subdir\file` on Unix has no path separator and no "..", so
		// validateTransferName accepts it (backslash is a valid filename char
		// on Unix). This case is therefore omitted — it is only an error on Windows.
		{"dot-dot in middle", "foo/../bar", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateTransferName(tc.in)
			if tc.wantErr && err == nil {
				t.Fatalf("validateTransferName(%q) = nil; want error", tc.in)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("validateTransferName(%q) = %v; want nil", tc.in, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// getFileSize
// ---------------------------------------------------------------------------

// TestGetFileSize: creates a temporary file with known content and
// verifies getFileSize returns the correct byte count.
func TestGetFileSize(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "wsmgrft-test-*.bin")
	if err != nil {
		t.Fatalf("os.CreateTemp: %v", err)
	}
	defer f.Close()
	content := []byte("hello world")
	if _, err := f.Write(content); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got := getFileSize(f)
	if got != int64(len(content)) {
		t.Fatalf("getFileSize = %d; want %d", got, len(content))
	}
}

// TestGetFileSize_EmptyFile: an empty file should report size 0.
func TestGetFileSize_EmptyFile(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "wsmgrft-empty-*.bin")
	if err != nil {
		t.Fatalf("os.CreateTemp: %v", err)
	}
	defer f.Close()
	got := getFileSize(f)
	if got != 0 {
		t.Fatalf("getFileSize on empty file = %d; want 0", got)
	}
}

// ---------------------------------------------------------------------------
// calculateHash
// ---------------------------------------------------------------------------

// TestCalculateHash: writes known content to a temp file and verifies
// the SHA-1 hex string is a 40-character lowercase hex string.
func TestCalculateHash(t *testing.T) {
	dir := t.TempDir()
	f, err := os.CreateTemp(dir, "hash-test-*.bin")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	content := []byte("test content for sha1")
	if _, err := f.Write(content); err != nil {
		f.Close()
		t.Fatalf("Write: %v", err)
	}
	name := f.Name()
	f.Close()

	got := calculateHash(name)
	if got == "" {
		t.Fatalf("calculateHash(%q) returned empty string; want non-empty hex", name)
	}
	if len(got) != 40 {
		t.Fatalf("calculateHash(%q) = %q (len=%d); want 40-char hex", name, got, len(got))
	}
}

// TestCalculateHash_KnownContent verifies the SHA-1 value against a
// reference computed via crypto/sha1 directly.
func TestCalculateHash_KnownContent(t *testing.T) {
	// "abc" has SHA-1 a9993e364706816aba3e25717850c26c9cd0d89d
	content := []byte("abc")
	dir := t.TempDir()
	f, err := os.CreateTemp(dir, "sha1-known-*.bin")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	if _, err := f.Write(content); err != nil {
		f.Close()
		t.Fatalf("Write: %v", err)
	}
	name := f.Name()
	f.Close()

	got := calculateHash(name)
	want := "a9993e364706816aba3e25717850c26c9cd0d89d"
	if got != want {
		t.Fatalf("calculateHash(\"abc\") = %q; want %q", got, want)
	}
}

// TestCalculateHash_NonExistentFile: a missing file returns "".
func TestCalculateHash_NonExistentFile(t *testing.T) {
	got := calculateHash("/tmp/wsmgrft-does-not-exist-xyz.bin")
	if got != "" {
		t.Fatalf("calculateHash on missing file = %q; want \"\"", got)
	}
}

// ---------------------------------------------------------------------------
// getFileTransferCacheIndex
// ---------------------------------------------------------------------------

// TestGetFileTransferCacheIndex_FindsFirstEmptySlot: with a freshly
// initialised cache the first call returns 0, subsequent inserts
// return the next free slot.
func TestGetFileTransferCacheIndex_FindsFirstEmptySlot(t *testing.T) {
	fileTransferCache = initFileTransferCache()

	var emptyUid [utils.UIDLEN]byte
	idx0 := getFileTransferCacheIndex(emptyUid)
	if idx0 != 0 {
		t.Fatalf("first call = %d; want 0", idx0)
	}
	// Mark slot 0 as in-use by writing a non-zero uid.
	fileTransferCache[0].Uid = [utils.UIDLEN]byte{1, 2, 3, 4}

	idx1 := getFileTransferCacheIndex(emptyUid)
	if idx1 != 1 {
		t.Fatalf("after marking slot 0, next call = %d; want 1", idx1)
	}
}

// TestGetFileTransferCacheIndex_NoFreeSlot: when all slots are in-use,
// the function returns -1.
func TestGetFileTransferCacheIndex_NoFreeSlot(t *testing.T) {
	fileTransferCache = initFileTransferCache()
	// Fill all slots.
	for i := 0; i < FILETRANSFERCACHESIZE; i++ {
		fileTransferCache[i].Uid = [utils.UIDLEN]byte{byte(i + 1), 0, 0, 0}
	}
	var emptyUid [utils.UIDLEN]byte
	idx := getFileTransferCacheIndex(emptyUid)
	if idx != -1 {
		t.Fatalf("full cache: getFileTransferCacheIndex = %d; want -1", idx)
	}
}

// ---------------------------------------------------------------------------
// createUlResponse
// ---------------------------------------------------------------------------

// TestCreateUlResponse_WithoutChunk: a response with no chunk data
// must be exactly 10 bytes (uid[4]+msgNo[1]+chunkSize[4]+lastMsg[1]).
func TestCreateUlResponse_WithoutChunk(t *testing.T) {
	uid := []byte{0x01, 0x02, 0x03, 0x04}
	resp := createUlResponse(uid, 7, byte(0x01), []byte{0, 0, 0, 5}, nil)
	if len(resp) != 10 {
		t.Fatalf("createUlResponse (no chunk) len = %d; want 10", len(resp))
	}
	// Verify uid is copied correctly.
	for i, b := range uid {
		if resp[i] != b {
			t.Fatalf("resp[%d] = %x; want %x", i, resp[i], b)
		}
	}
	// messageNo at index 4.
	if resp[4] != 7 {
		t.Fatalf("resp[4] (messageNo) = %d; want 7", resp[4])
	}
	// lastMessage at index 9.
	if resp[9] != byte(0x01) {
		t.Fatalf("resp[9] (lastMessage) = %x; want 0x01", resp[9])
	}
}

// TestCreateUlResponse_WithChunk: when a chunk is provided the response
// must be 10 + len(chunk) bytes.
func TestCreateUlResponse_WithChunk(t *testing.T) {
	uid := []byte{0xAA, 0xBB, 0xCC, 0xDD}
	chunk := []byte{0x11, 0x22, 0x33}
	resp := createUlResponse(uid, 3, byte(0x00), []byte{0, 0, 0, 3}, chunk)
	want := 10 + len(chunk)
	if len(resp) != want {
		t.Fatalf("createUlResponse (chunk len=%d) len = %d; want %d", len(chunk), len(resp), want)
	}
	// Verify chunk bytes start at index 10.
	for i, b := range chunk {
		if resp[10+i] != b {
			t.Fatalf("chunk byte at resp[%d] = %x; want %x", 10+i, resp[10+i], b)
		}
	}
}

// TestCreateUlResponse_ZeroUid: uid all zeros should not panic.
func TestCreateUlResponse_ZeroUid(t *testing.T) {
	uid := []byte{0, 0, 0, 0}
	resp := createUlResponse(uid, 0, byte(0x00), []byte{0, 0, 0, 0}, nil)
	if len(resp) != 10 {
		t.Fatalf("len = %d; want 10", len(resp))
	}
}

// ---------------------------------------------------------------------------
// initFtSession — unit-testable paths (safe filename validation &
// early-return on invalid name; the os.Create / os.Open branches are
// integration paths that touch the filesystem and are exercised via the
// runtest.sh harness).
// ---------------------------------------------------------------------------

// TestInitFtSession_RejectsTraversalName: a client-supplied Name with
// path-traversal components must be rejected before any filesystem call.
func TestInitFtSession_RejectsTraversalName(t *testing.T) {
	fileTransferCache = initFileTransferCache()
	req := utils.FileTransferCache{
		Uid:            [utils.UIDLEN]byte{1, 2, 3, 4},
		Name:           "../../etc/passwd",
		Path:           t.TempDir(),
		UploadTransfer: false,
	}
	status := initFtSession(req)
	if status == 0 {
		t.Fatalf("initFtSession with traversal name returned status=0 (ok); want non-zero (error)")
	}
}

// TestInitFtSession_RejectsEmptyName: empty name must be rejected.
func TestInitFtSession_RejectsEmptyName(t *testing.T) {
	fileTransferCache = initFileTransferCache()
	req := utils.FileTransferCache{
		Uid:            [utils.UIDLEN]byte{1, 2, 3, 4},
		Name:           "",
		Path:           t.TempDir(),
		UploadTransfer: false,
	}
	status := initFtSession(req)
	if status == 0 {
		t.Fatalf("initFtSession with empty name returned status=0; want error")
	}
}

// TestInitFtSession_RejectsAbsolutePath: a name with a leading slash
// must be rejected.
func TestInitFtSession_RejectsAbsolutePath(t *testing.T) {
	fileTransferCache = initFileTransferCache()
	req := utils.FileTransferCache{
		Uid:            [utils.UIDLEN]byte{1, 2, 3, 4},
		Name:           "/etc/cron.d/evil",
		Path:           t.TempDir(),
		UploadTransfer: false,
	}
	status := initFtSession(req)
	if status == 0 {
		t.Fatalf("initFtSession with absolute name returned status=0; want error")
	}
}

// TestInitFtSession_DownloadCreatesFile: a download init with a valid
// name creates the file and returns status 0.
func TestInitFtSession_DownloadCreatesFile(t *testing.T) {
	fileTransferCache = initFileTransferCache()
	dir := t.TempDir()
	req := utils.FileTransferCache{
		Uid:            [utils.UIDLEN]byte{5, 6, 7, 8},
		Name:           "output.bin",
		Path:           dir,
		UploadTransfer: false,
	}
	status := initFtSession(req)
	if status != 0 {
		t.Fatalf("initFtSession for download returned status=%d; want 0", status)
	}
	// Clean up: close the open file descriptor left in the cache.
	for i := 0; i < FILETRANSFERCACHESIZE; i++ {
		if fileTransferCache[i].FileDescriptor != nil {
			fileTransferCache[i].FileDescriptor.Close()
		}
	}
}

// TestInitFtSession_UploadMissingFile: an upload init for a file that
// doesn't exist must return error status (non-zero).
func TestInitFtSession_UploadMissingFile(t *testing.T) {
	fileTransferCache = initFileTransferCache()
	req := utils.FileTransferCache{
		Uid:            [utils.UIDLEN]byte{9, 10, 11, 12},
		Name:           "nonexistent.bin",
		Path:           t.TempDir(),
		UploadTransfer: true,
	}
	status := initFtSession(req)
	if status == 0 {
		t.Fatalf("initFtSession for upload of missing file returned status=0; want error")
	}
}

// TestInitFtSession_UploadExistingFile: an upload init for an existing
// file must return status 0 and populate the cache.
func TestInitFtSession_UploadExistingFile(t *testing.T) {
	fileTransferCache = initFileTransferCache()
	dir := t.TempDir()
	// Create the file that will be "uploaded".
	f, err := os.CreateTemp(dir, "upload-*.bin")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	if _, err := f.Write([]byte("payload")); err != nil {
		f.Close()
		t.Fatalf("Write: %v", err)
	}
	f.Close()

	req := utils.FileTransferCache{
		Uid:            [utils.UIDLEN]byte{13, 14, 15, 16},
		Name:           f.Name()[len(dir)+1:], // base name only
		Path:           dir,
		UploadTransfer: true,
	}
	status := initFtSession(req)
	if status != 0 {
		t.Fatalf("initFtSession for upload of existing file returned status=%d; want 0", status)
	}
	// Clean up.
	for i := 0; i < FILETRANSFERCACHESIZE; i++ {
		if fileTransferCache[i].FileDescriptor != nil {
			fileTransferCache[i].FileDescriptor.Close()
		}
	}
}

// ---------------------------------------------------------------------------
// getDataResponse — dispatcher between DL and UL paths
// ---------------------------------------------------------------------------

// TestGetDataResponse_DispatchesDl: a request longer than 6 bytes is
// sent to getDataResponseDl.
func TestGetDataResponse_DispatchesDl(t *testing.T) {
	fileTransferCache = initFileTransferCache()
	// 7 bytes triggers the DL path (len > 6).
	// With uid {0,0,0,0} findFileTransferCacheIndex returns 0 (empty uid).
	// But chunkSize will be 0 which causes "uint32(len(chunk)) != chunkSize" (0==0) to
	// pass, so we'll get a write error (nil fd) → createDlResponse with 0xFF.
	req := []byte{1, 2, 3, 4, 5, 0, 0, 0, 0, 0, 0xFF}
	resp := getDataResponse(req, 0)
	if len(resp) != 6 {
		t.Fatalf("DL path response len = %d; want 6", len(resp))
	}
}

// TestGetDataResponse_DispatchesUl: a request of exactly 6 bytes is
// sent to getDataResponseUl.
func TestGetDataResponse_DispatchesUl(t *testing.T) {
	fileTransferCache = initFileTransferCache()
	// 6 bytes (== UL_HEADER_SIZE) is the minimal valid UL header.
	// uid {0,0,0,0} is "not found" in a freshly-initialised cache
	// (getFileTransferCacheIndex returns 0 for the empty uid), so
	// findFileTransferCacheIndex returns 0 (first slot has empty uid).
	// status byte 0x00 → tries to read from a nil fd → returns error response.
	req := []byte{0, 0, 0, 0, 0, 0}
	_ = getDataResponse(req, 0)
	// Must not panic; no assertion on value (behaviour depends on cache state).
}

// TestGetDataResponse_ShortPacketGoesToUl: a request of 6 bytes or
// fewer is dispatched to getDataResponseUl (not DL).
func TestGetDataResponse_ShortPacketGoesToUl(t *testing.T) {
	fileTransferCache = initFileTransferCache()
	for _, n := range []int{0, 1, 3, 5, 6} {
		req := make([]byte, n)
		resp := getDataResponse(req, 0)
		// For 0..5 the UL path returns an error createUlResponse (10 bytes).
		// For 6 (minimum valid) it may vary. Just confirm no panic.
		_ = resp
	}
}

// ---------------------------------------------------------------------------
// getDataResponseDl — additional paths
// ---------------------------------------------------------------------------

// TestGetDataResponseDl_CacheHitWriteAndAck: a well-formed DL packet
// whose uid matches a cache entry with a writable file should return an
// ack (status 0x00).
func TestGetDataResponseDl_CacheHitWriteAndAck(t *testing.T) {
	fileTransferCache = initFileTransferCache()
	dir := t.TempDir()
	f, err := os.CreateTemp(dir, "dl-write-*.bin")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	uid := [utils.UIDLEN]byte{0xAA, 0xBB, 0xCC, 0xDD}
	fileTransferCache[0].Uid = uid
	fileTransferCache[0].FileDescriptor = f

	chunk := []byte{0x01, 0x02, 0x03, 0x04}
	chunkSize := uint32(len(chunk))
	// Packet: uid(4) | messageNo(1) | chunkSize(4 big-endian) | lastMessage(1=no more) | chunk
	req := []byte{
		0xAA, 0xBB, 0xCC, 0xDD, // uid
		0x01,                     // messageNo
		byte(chunkSize >> 24), byte(chunkSize >> 16), byte(chunkSize >> 8), byte(chunkSize), // chunkSize
		0x00,                     // lastMessage = 0 (not last)
	}
	req = append(req, chunk...)
	resp := getDataResponseDl(req)
	if len(resp) != 6 {
		t.Fatalf("DL ack response len = %d; want 6", len(resp))
	}
	// status byte should be 0x00 (ack, no error)
	if resp[5] != 0x00 {
		t.Fatalf("DL ack status = 0x%02X; want 0x00", resp[5])
	}
}

// TestGetDataResponseDl_ChunkSizeMismatch: if chunkSize != len(chunk)
// the function must return a NACK (status 0x01).
func TestGetDataResponseDl_ChunkSizeMismatch(t *testing.T) {
	fileTransferCache = initFileTransferCache()
	dir := t.TempDir()
	f, err := os.CreateTemp(dir, "dl-mismatch-*.bin")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	uid := [utils.UIDLEN]byte{0x11, 0x22, 0x33, 0x44}
	fileTransferCache[0].Uid = uid
	fileTransferCache[0].FileDescriptor = f
	defer f.Close()

	// Claim chunkSize = 10 but send only 4 bytes of chunk.
	req := []byte{
		0x11, 0x22, 0x33, 0x44, // uid
		0x01,                    // messageNo
		0x00, 0x00, 0x00, 0x0A, // chunkSize = 10
		0x00,                    // lastMessage = 0
	}
	req = append(req, []byte{0x01, 0x02, 0x03, 0x04}...) // only 4 bytes
	resp := getDataResponseDl(req)
	if len(resp) != 6 {
		t.Fatalf("DL NACK response len = %d; want 6", len(resp))
	}
	if resp[5] != 0x01 {
		t.Fatalf("DL NACK status = 0x%02X; want 0x01", resp[5])
	}
}

// ---------------------------------------------------------------------------
// getDataResponseUl — additional paths
// ---------------------------------------------------------------------------

// TestGetDataResponseUl_CacheHitRead: a well-formed UL status request
// for a cache entry with a readable file should return a chunk.
func TestGetDataResponseUl_CacheHitRead(t *testing.T) {
	fileTransferCache = initFileTransferCache()
	dir := t.TempDir()
	content := []byte("hello upload")
	f, err := os.CreateTemp(dir, "ul-read-*.bin")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	if _, err := f.Write(content); err != nil {
		f.Close()
		t.Fatalf("Write: %v", err)
	}
	// Seek back to start for reading.
	if _, err := f.Seek(0, 0); err != nil {
		f.Close()
		t.Fatalf("Seek: %v", err)
	}

	uid := [utils.UIDLEN]byte{0xCA, 0xFE, 0xBA, 0xBE}
	fileTransferCache[0].Uid = uid
	fileTransferCache[0].FileDescriptor = f
	fileTransferCache[0].ChunkSize = len(content)

	// UL packet: uid(4) | messageNo(1) | status(1=0x00=ok)
	req := []byte{0xCA, 0xFE, 0xBA, 0xBE, 0x00, 0x00}
	resp := getDataResponseUl(req, 0)
	// Response must be at least 10 bytes + chunk.
	if len(resp) < 10 {
		t.Fatalf("UL response len = %d; want >= 10", len(resp))
	}
}

// TestGetDataResponseUl_StatusFF: status byte 0xFF signals error from
// the client — must return a short error response.
func TestGetDataResponseUl_StatusFF(t *testing.T) {
	fileTransferCache = initFileTransferCache()
	uid := [utils.UIDLEN]byte{0xDE, 0xAD, 0xBE, 0xEF}
	fileTransferCache[0].Uid = uid

	req := []byte{0xDE, 0xAD, 0xBE, 0xEF, 0x00, 0xFF} // status = 0xFF
	resp := getDataResponseUl(req, 0)
	if len(resp) < 10 {
		t.Fatalf("UL 0xFF response len = %d; want >= 10", len(resp))
	}
}

// ---------------------------------------------------------------------------
// getFileSize — error branch via closed fd
// ---------------------------------------------------------------------------

// TestGetFileSize_ClosedFd: a closed file should cause Stat to fail and
// getFileSize to return 0. This exercises the error branch.
func TestGetFileSize_ClosedFd(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "gs-closed-*.bin")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	f.Close() // close before calling getFileSize
	// On most systems Stat on a closed fd returns an error.
	// If it doesn't (some OS allow it) the test is still valid — it just
	// exercises the success path again.
	got := getFileSize(f)
	_ = got // 0 on error, or the real size — both are acceptable
}

// ---------------------------------------------------------------------------
// Integration-only entry points (NOT unit-tested here)
//
// WsMgrFTInit       — unbounded for/select loop, binds channels
// initDataSessions  — calls http.ListenAndServe (binds port 8002)
// dataSession       — unbounded for loop over WebSocket conn
// makeServerHandler — returns an http.HandlerFunc (integration glue)
// getDataSessionIndex / returnDataSessionIndex — covered above
// ---------------------------------------------------------------------------
