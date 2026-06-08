package vdmloader

// watcher.go — fsnotify-based live-reload for VDM directories.
// When any *.graphql file in a watched directory changes, the watcher
// deregisters all trees previously loaded from that directory and calls
// LoadDir again.

import (
	"path/filepath"
	"sync"
	"time"

	"github.com/covesa/vissr/utils"
	"github.com/fsnotify/fsnotify"
)

// Watcher watches a directory of *.graphql files and reloads the VDM trees
// whenever a file changes.  Create with NewWatcher and stop with Stop.
type Watcher struct {
	dir     string
	w       *fsnotify.Watcher
	mu      sync.Mutex
	loaded  []string // rootNames last loaded from this dir
	stopCh  chan struct{}
	stopped chan struct{}
}

// NewWatcher creates a Watcher for dir and starts the background goroutine.
// The caller should call Stop() when done.
func NewWatcher(dir string) (*Watcher, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	if err := fw.Add(dir); err != nil {
		fw.Close()
		return nil, err
	}

	wt := &Watcher{
		dir:     dir,
		w:       fw,
		stopCh:  make(chan struct{}),
		stopped: make(chan struct{}),
	}
	// Record the names of trees already loaded by an initial LoadDir call.
	wt.loaded = currentRootNames(dir)

	go wt.loop()
	return wt, nil
}

// Stop shuts down the watcher and waits for the background goroutine to exit.
func (wt *Watcher) Stop() {
	close(wt.stopCh)
	<-wt.stopped
	wt.w.Close()
}

// loop is the background event handler.
func (wt *Watcher) loop() {
	defer close(wt.stopped)

	// Debounce: coalesce rapid writes (editors often do multiple writes per save).
	var debounce <-chan time.Time

	for {
		select {
		case <-wt.stopCh:
			return

		case event, ok := <-wt.w.Events:
			if !ok {
				return
			}
			if !isGraphQL(event.Name) {
				continue
			}
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) ||
				event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
				debounce = time.After(200 * time.Millisecond)
			}

		case err, ok := <-wt.w.Errors:
			if !ok {
				return
			}
			utils.Error.Printf("vdmloader watcher: %v", err)

		case <-debounce:
			debounce = nil
			wt.reload()
		}
	}
}

// reload deregisters the previously loaded trees and reloads from the directory.
func (wt *Watcher) reload() {
	wt.mu.Lock()
	prev := wt.loaded
	wt.mu.Unlock()

	for _, name := range prev {
		utils.DeregisterServiceTree(name)
	}

	n, err := LoadDir(wt.dir)
	if err != nil {
		utils.Error.Printf("vdmloader watcher: reload %s: %v", wt.dir, err)
		// Keep prev loaded list so we try to deregister them next time.
		return
	}

	next := currentRootNames(wt.dir)
	wt.mu.Lock()
	wt.loaded = next
	wt.mu.Unlock()

	utils.Info.Printf("vdmloader watcher: reloaded %s (%d tree(s))", wt.dir, n)
}

// currentRootNames parses all *.graphql files in dir and returns the unique
// root names — without registering anything in the forest.
func currentRootNames(dir string) []string {
	entries, err := graphqlFiles(dir)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var names []string
	for _, path := range entries {
		_, metas, err := ParseFile(path)
		if err != nil || len(metas) == 0 {
			continue
		}
		for _, m := range metas {
			if !seen[m.RootName] {
				seen[m.RootName] = true
				names = append(names, m.RootName)
			}
		}
	}
	return names
}

// graphqlFiles returns the absolute paths of *.graphql files in dir.
func graphqlFiles(dir string) ([]string, error) {
	entries, err := filepath.Glob(filepath.Join(dir, "*.graphql"))
	if err != nil {
		return nil, err
	}
	return entries, nil
}

func isGraphQL(name string) bool {
	return filepath.Ext(name) == ".graphql"
}
