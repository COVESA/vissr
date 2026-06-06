// Package webdash serves an optional embedded web dashboard that visualises
// the vissr HIM forest. Start it with Start(addr); it runs in the background.
// Disabled by default — only started when --web-addr is set on vissv2server.
package webdash

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"

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

// Start registers the dashboard routes and launches an HTTP server on addr
// (e.g. ":8090") in a background goroutine. It returns immediately.
func Start(addr string) error {
	mux := http.NewServeMux()

	// Serve embedded static files at /
	staticRoot, err := fs.Sub(embeddedStatic, "static")
	if err != nil {
		return err
	}
	mux.Handle("/", http.FileServer(http.FS(staticRoot)))

	// GET /api/forest → [{rootName, domain, version}, …]
	mux.HandleFunc("/api/forest", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
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
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		if err := json.NewEncoder(w).Encode(toNodeJSON(root)); err != nil {
			utils.Error.Printf("webdash: /api/tree/%s encode: %v", rootName, err)
		}
	})

	go func() {
		utils.Info.Printf("webdash: listening on http://%s", addr)
		if err := http.ListenAndServe(addr, mux); err != nil {
			utils.Error.Printf("webdash: server stopped: %v", err)
		}
	}()
	return nil
}
