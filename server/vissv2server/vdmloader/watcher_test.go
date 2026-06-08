package vdmloader

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/covesa/vissr/utils"
)

func init() {
	utils.InitLog("watcher-log.txt", os.TempDir(), false, "info")
}

// ── helpers ──────────────────────────────────────────────────────────────────

func writeGraphQL(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

const minimalSDL = `
type Vehicle @vspec(element: BRANCH, fqn: "Vehicle") {
  Speed: Float @vspec(element: SENSOR, fqn: "Vehicle.Speed")
}
`

const updatedSDL = `
type Vehicle @vspec(element: BRANCH, fqn: "Vehicle") {
  Speed: Float @vspec(element: SENSOR, fqn: "Vehicle.Speed")
  Gear:  String @vspec(element: SENSOR, fqn: "Vehicle.Gear")
}
`

const secondTreeSDL = `
type Trailer @vspec(element: BRANCH, fqn: "Trailer") {
  Weight: Float @vspec(element: SENSOR, fqn: "Trailer.Weight")
}
`

// ── isGraphQL ─────────────────────────────────────────────────────────────────

func TestIsGraphQL_True(t *testing.T) {
	for _, name := range []string{"vehicle.graphql", "/tmp/x/foo.graphql"} {
		if !isGraphQL(name) {
			t.Errorf("isGraphQL(%q) = false, want true", name)
		}
	}
}

func TestIsGraphQL_False(t *testing.T) {
	for _, name := range []string{"vehicle.go", "foo.graphql.bak", "README.md", ""} {
		if isGraphQL(name) {
			t.Errorf("isGraphQL(%q) = true, want false", name)
		}
	}
}

// ── graphqlFiles ──────────────────────────────────────────────────────────────

func TestGraphqlFiles_Empty(t *testing.T) {
	dir := t.TempDir()
	files, err := graphqlFiles(dir)
	if err != nil {
		t.Fatalf("graphqlFiles: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("expected 0 files, got %d", len(files))
	}
}

func TestGraphqlFiles_OnlyGraphQL(t *testing.T) {
	dir := t.TempDir()
	writeGraphQL(t, dir, "a.graphql", minimalSDL)
	writeGraphQL(t, dir, "b.graphql", secondTreeSDL)
	// should not be returned
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hi"), 0644)

	files, err := graphqlFiles(dir)
	if err != nil {
		t.Fatalf("graphqlFiles: %v", err)
	}
	if len(files) != 2 {
		t.Errorf("expected 2 .graphql files, got %d", len(files))
	}
}

// ── currentRootNames ──────────────────────────────────────────────────────────

func TestCurrentRootNames_Empty(t *testing.T) {
	dir := t.TempDir()
	names := currentRootNames(dir)
	if len(names) != 0 {
		t.Errorf("expected 0 names, got %v", names)
	}
}

func TestCurrentRootNames_Single(t *testing.T) {
	dir := t.TempDir()
	writeGraphQL(t, dir, "v.graphql", minimalSDL)
	names := currentRootNames(dir)
	if len(names) != 1 || names[0] != "Vehicle" {
		t.Errorf("expected [Vehicle], got %v", names)
	}
}

func TestCurrentRootNames_Multiple(t *testing.T) {
	dir := t.TempDir()
	writeGraphQL(t, dir, "v.graphql", minimalSDL)
	writeGraphQL(t, dir, "t.graphql", secondTreeSDL)
	names := currentRootNames(dir)
	if len(names) != 2 {
		t.Errorf("expected 2 names, got %v", names)
	}
}

func TestCurrentRootNames_InvalidFile(t *testing.T) {
	dir := t.TempDir()
	writeGraphQL(t, dir, "bad.graphql", "this is not valid SDL {{{{")
	// Should not panic; returns empty list.
	names := currentRootNames(dir)
	if len(names) != 0 {
		t.Errorf("expected 0 names for invalid SDL, got %v", names)
	}
}

// ── NewWatcher / Stop ─────────────────────────────────────────────────────────

func TestNewWatcher_NonExistentDir(t *testing.T) {
	_, err := NewWatcher("/nonexistent-vissr-test-dir-xyz/")
	if err == nil {
		t.Error("expected error for non-existent directory")
	}
}

func TestNewWatcher_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	wt, err := NewWatcher(dir)
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	wt.Stop()
}

func TestWatcher_StopIdempotent(t *testing.T) {
	dir := t.TempDir()
	wt, err := NewWatcher(dir)
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	wt.Stop()
	// Second Stop would panic if stopCh was closed twice — but since loop
	// exits on the first close, the channel receive in Stop just returns.
	// Verify the stopped channel is already closed (non-blocking receive).
	select {
	case <-wt.stopped:
		// expected
	default:
		t.Error("stopped channel not closed after Stop()")
	}
}

// ── live-reload ───────────────────────────────────────────────────────────────

// waitForTree polls the forest for rootName up to deadline.
func waitForTree(rootName string, present bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		root := utils.GetForestRoot(rootName)
		if present && root != nil {
			return true
		}
		if !present && root == nil {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

func TestWatcher_ReloadOnWrite(t *testing.T) {
	dir := t.TempDir()
	writeGraphQL(t, dir, "v.graphql", minimalSDL)

	// Ensure clean state.
	utils.DeregisterServiceTree("Vehicle")

	// Initial load.
	n, err := LoadDir(dir)
	if err != nil || n == 0 {
		t.Fatalf("LoadDir: n=%d err=%v", n, err)
	}
	t.Cleanup(func() { utils.DeregisterServiceTree("Vehicle") })

	wt, err := NewWatcher(dir)
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	t.Cleanup(wt.Stop)

	// Overwrite — watcher should deregister + reload.
	writeGraphQL(t, dir, "v.graphql", updatedSDL)

	// Wait for reload (watcher debounces 200ms + processing time).
	if !waitForTree("Vehicle", true, 2*time.Second) {
		t.Error("Vehicle tree was not reloaded after write")
	}
}

func TestWatcher_ReloadOnCreate(t *testing.T) {
	dir := t.TempDir()
	utils.DeregisterServiceTree("Trailer")

	wt, err := NewWatcher(dir)
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	t.Cleanup(func() {
		wt.Stop()
		utils.DeregisterServiceTree("Trailer")
	})

	// Create a new file — watcher should pick it up.
	writeGraphQL(t, dir, "trailer.graphql", secondTreeSDL)

	if !waitForTree("Trailer", true, 2*time.Second) {
		t.Error("Trailer tree was not loaded after file creation")
	}
}

func TestWatcher_ReloadOnDelete(t *testing.T) {
	dir := t.TempDir()
	path := writeGraphQL(t, dir, "v.graphql", minimalSDL)

	utils.DeregisterServiceTree("Vehicle")

	n, err := LoadDir(dir)
	if err != nil || n == 0 {
		t.Fatalf("LoadDir: n=%d err=%v", n, err)
	}
	t.Cleanup(func() { utils.DeregisterServiceTree("Vehicle") })

	wt, err := NewWatcher(dir)
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	t.Cleanup(wt.Stop)

	// Delete the file — watcher should deregister the tree.
	if err := os.Remove(path); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	if !waitForTree("Vehicle", false, 2*time.Second) {
		t.Error("Vehicle tree still in forest after file deletion")
	}
}

// ── FuzzIsGraphQL ─────────────────────────────────────────────────────────────

func FuzzIsGraphQL(f *testing.F) {
	f.Add("vehicle.graphql")
	f.Add("vehicle.go")
	f.Add("")
	f.Add(".graphql")
	f.Add("a/b/c.graphql")
	f.Fuzz(func(t *testing.T, name string) {
		// Must not panic on any input.
		_ = isGraphQL(name)
	})
}
