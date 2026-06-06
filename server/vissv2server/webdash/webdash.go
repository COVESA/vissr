// Package webdash serves an optional embedded web dashboard that visualises
// the vissr HIM forest. Start it with Start(addr); it runs in the background.
// Disabled by default — only started when --web-addr is set on vissv2server.
package webdash

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/covesa/vissr/utils"
)

// nodeJSON is a JSON-serialisable snapshot of a Node_t subtree.
// Parent pointers are omitted to avoid circular-reference problems.
type nodeJSON struct {
	Name        string     `json:"name"`
	NodeType    string     `json:"nodeType"`
	Description string     `json:"description,omitempty"`
	Datatype    string     `json:"datatype,omitempty"`
	Min         string     `json:"min,omitempty"`
	Max         string     `json:"max,omitempty"`
	Unit        string     `json:"unit,omitempty"`
	Default     string     `json:"default,omitempty"`
	Allowed     []string   `json:"allowed,omitempty"`
	Children    []nodeJSON `json:"children,omitempty"`
}

func toNodeJSON(n *utils.Node_t) nodeJSON {
	j := nodeJSON{
		Name:        n.Name,
		NodeType:    n.NodeType,
		Description: n.Description,
		Datatype:    n.Datatype,
		Min:         n.Min,
		Max:         n.Max,
		Unit:        n.Unit,
		Default:     n.DefaultValue,
	}
	if len(n.AllowedDef) > 0 {
		j.Allowed = n.AllowedDef
	}
	for _, child := range n.Child {
		j.Children = append(j.Children, toNodeJSON(child))
	}
	return j
}

// ── Health ───────────────────────────────────────────────────────────────────

// healthPayload is the JSON body for GET /api/health and SSE health events.
type healthPayload struct {
	Goroutines int     `json:"goroutines"`
	HeapMB     float64 `json:"heapMB"`
	UptimeS    int64   `json:"uptimeS"`
	Trees      int     `json:"trees"`
}

func currentHealth(startTime time.Time) healthPayload {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return healthPayload{
		Goroutines: runtime.NumGoroutine(),
		HeapMB:     float64(ms.HeapInuse) / (1024 * 1024),
		UptimeS:    int64(time.Since(startTime).Seconds()),
		Trees:      len(utils.ForestInfoList()),
	}
}

// ── Validation ───────────────────────────────────────────────────────────────

// validateIssue is one entry in the /api/validate response.
type validateIssue struct {
	Path     string `json:"path"`
	Severity string `json:"severity"` // "error" or "warning"
	Message  string `json:"message"`
}

func validateTree(node *utils.Node_t, path string) []validateIssue {
	var issues []validateIssue
	if node.NodeType != utils.BRANCH {
		if node.Description == "" {
			issues = append(issues, validateIssue{path, "warning", "missing description"})
		}
		if node.Datatype == "" {
			issues = append(issues, validateIssue{path, "error", "missing datatype"})
		}
		if (node.NodeType == utils.SENSOR || node.NodeType == utils.ACTUATOR) && node.Unit == "" {
			issues = append(issues, validateIssue{path, "warning", "missing unit"})
		}
		if node.Min != "" && node.Max != "" {
			minV, errMin := strconv.ParseFloat(node.Min, 64)
			maxV, errMax := strconv.ParseFloat(node.Max, 64)
			if errMin == nil && errMax == nil && minV > maxV {
				issues = append(issues, validateIssue{path, "error",
					fmt.Sprintf("min (%s) > max (%s)", node.Min, node.Max)})
			}
		}
	}
	for _, child := range node.Child {
		issues = append(issues, validateTree(child, path+"."+child.Name)...)
	}
	return issues
}

// ── Search ───────────────────────────────────────────────────────────────────

// searchResult is one entry in the /api/search response.
type searchResult struct {
	Path     string `json:"path"`
	NodeType string `json:"nodeType"`
	TreeName string `json:"treeName"`
}

func searchForest(q string) []searchResult {
	q = strings.ToLower(q)
	var results []searchResult
	for _, info := range utils.ForestInfoList() {
		root := utils.GetForestRoot(info.RootName)
		if root != nil {
			searchNode(root, root.Name, info.RootName, q, &results)
		}
	}
	return results
}

func searchNode(node *utils.Node_t, path, treeName, q string, results *[]searchResult) {
	if strings.Contains(strings.ToLower(node.Name), q) || strings.Contains(strings.ToLower(path), q) {
		*results = append(*results, searchResult{path, node.NodeType, treeName})
	}
	for _, child := range node.Child {
		searchNode(child, path+"."+child.Name, treeName, q, results)
	}
}

// ── SSE hub ───────────────────────────────────────────────────────────────────

// sseHub fans out SSE messages to all connected browser clients.
type sseHub struct {
	mu      sync.Mutex
	clients map[chan string]struct{}
}

func newSSEHub() *sseHub {
	return &sseHub{clients: make(map[chan string]struct{})}
}

func (h *sseHub) subscribe() chan string {
	ch := make(chan string, 8)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *sseHub) unsubscribe(ch chan string) {
	h.mu.Lock()
	delete(h.clients, ch)
	h.mu.Unlock()
}

func (h *sseHub) publish(eventType, data string) {
	msg := fmt.Sprintf("event: %s\ndata: %s\n\n", eventType, data)
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		select {
		case ch <- msg:
		default: // slow client — skip rather than block
		}
	}
}

// ── Mux builder ──────────────────────────────────────────────────────────────

// buildMux constructs the HTTP mux. Extracted for testability.
func buildMux(startTime time.Time) (*http.ServeMux, error) {
	mux := http.NewServeMux()

	// Serve embedded static files at /
	staticRoot, err := fs.Sub(embeddedStatic, "static")
	if err != nil {
		return nil, err
	}
	mux.Handle("/", http.FileServer(http.FS(staticRoot)))

	hub := newSSEHub()

	// Background health ticker — publishes health events to SSE clients.
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			b, _ := json.Marshal(currentHealth(startTime))
			hub.publish("health", string(b))
		}
	}()

	setJSON := func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
	}

	// GET /api/forest → [{rootName, domain, version}, …]
	mux.HandleFunc("/api/forest", func(w http.ResponseWriter, r *http.Request) {
		setJSON(w)
		if err := json.NewEncoder(w).Encode(utils.ForestInfoList()); err != nil {
			utils.Error.Printf("webdash: /api/forest encode: %v", err)
		}
	})

	// GET /api/tree/{rootName} → full nodeJSON tree
	mux.HandleFunc("/api/tree/", func(w http.ResponseWriter, r *http.Request) {
		rootName := strings.TrimPrefix(r.URL.Path, "/api/tree/")
		root := utils.GetForestRoot(rootName)
		if root == nil {
			http.NotFound(w, r)
			return
		}
		setJSON(w)
		if err := json.NewEncoder(w).Encode(toNodeJSON(root)); err != nil {
			utils.Error.Printf("webdash: /api/tree/%s encode: %v", rootName, err)
		}
	})

	// GET /api/health → goroutines, heapMB, uptimeS, trees
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		setJSON(w)
		if err := json.NewEncoder(w).Encode(currentHealth(startTime)); err != nil {
			utils.Error.Printf("webdash: /api/health encode: %v", err)
		}
	})

	// GET /api/validate/{rootName} → [{path, severity, message}, …]
	mux.HandleFunc("/api/validate/", func(w http.ResponseWriter, r *http.Request) {
		rootName := strings.TrimPrefix(r.URL.Path, "/api/validate/")
		root := utils.GetForestRoot(rootName)
		if root == nil {
			http.NotFound(w, r)
			return
		}
		issues := validateTree(root, root.Name)
		if issues == nil {
			issues = []validateIssue{}
		}
		setJSON(w)
		if err := json.NewEncoder(w).Encode(issues); err != nil {
			utils.Error.Printf("webdash: /api/validate/%s encode: %v", rootName, err)
		}
	})

	// GET /api/search?q=... → [{path, nodeType, treeName}, …]
	mux.HandleFunc("/api/search", func(w http.ResponseWriter, r *http.Request) {
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		var results []searchResult
		if q != "" {
			results = searchForest(q)
		}
		if results == nil {
			results = []searchResult{}
		}
		setJSON(w)
		if err := json.NewEncoder(w).Encode(results); err != nil {
			utils.Error.Printf("webdash: /api/search encode: %v", err)
		}
	})

	// GET /api/events → SSE stream (health ticks every 5 s)
	mux.HandleFunc("/api/events", func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "SSE not supported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		// Send current health immediately on connect.
		b, _ := json.Marshal(currentHealth(startTime))
		fmt.Fprintf(w, "event: health\ndata: %s\n\n", b)
		flusher.Flush()

		ch := hub.subscribe()
		defer hub.unsubscribe(ch)

		for {
			select {
			case msg := <-ch:
				fmt.Fprint(w, msg)
				flusher.Flush()
			case <-r.Context().Done():
				return
			}
		}
	})

	return mux, nil
}

// Start registers the dashboard routes and launches an HTTP server on addr
// (e.g. ":8090") in a background goroutine. It returns immediately.
func Start(addr string) error {
	mux, err := buildMux(time.Now())
	if err != nil {
		return err
	}
	go func() {
		utils.Info.Printf("webdash: listening on http://%s", addr)
		if err := http.ListenAndServe(addr, mux); err != nil {
			utils.Error.Printf("webdash: server stopped: %v", err)
		}
	}()
	return nil
}
