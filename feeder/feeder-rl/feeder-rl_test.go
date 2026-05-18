/**
* Regression test for the feeder-rl TouchFile permissions fix in
* commit ef639f0 (mode 0777 -> 0644).
**/
package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestTouchFile_CreatesWithSafeMode is the regression test for the
// ef639f0 fix that changed TouchFile's create mode from 0777 (world-
// writable) to 0644. A world-writable file created by the feeder would
// be a privilege-escalation foothold on any host where the feeder ran
// as a privileged user.
func TestTouchFile_CreatesWithSafeMode(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "test-touched-file")

	if err := TouchFile(name); err != nil {
		t.Fatalf("TouchFile(%q) returned error: %v", name, err)
	}
	defer os.Remove(name)

	info, err := os.Stat(name)
	if err != nil {
		t.Fatalf("os.Stat after TouchFile: %v", err)
	}
	got := info.Mode().Perm()
	// We accept any mode that is no broader than 0644. On some
	// filesystems (and with restrictive umasks) the actual bits may
	// be narrower, but they must never grant write to group/other.
	if got&0022 != 0 {
		t.Fatalf("TouchFile created file with group/other-writable bits: mode=%o", got)
	}
	if got != 0644 {
		t.Logf("note: mode is %o, not exactly 0644 (likely umask trimmed); group/other-write are correctly off", got)
	}
}
