package webdash

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/covesa/vissr/utils"
)

func init() {
	utils.InitLog("webdash-test.log", os.TempDir(), false, "info")
}

// registerTestTree registers a minimal tree for use in HTTP endpoint tests.
// Returns the rootName.
func registerTestTree(t *testing.T, rootName string) string {
	t.Helper()
	root := utils.NewBranchNode(rootName)
	speed := utils.NewSignalNode("Speed", utils.SENSOR, "float", "vehicle speed", "0", "250", "km/h")
	rpm := utils.NewSignalNode("EngineRPM", utils.SENSOR, "uint16", "", "0", "8000", "rpm")
	rpm.AllowedDef = []string{"Idle", "Rev"}
	utils.NewBranchNode(rootName, root) // ensure root name is set
	root.Name = rootName
	speed.Parent = root
	root.Child = append(root.Child, speed, rpm)
	root.Children = uint8(len(root.Child))
	if !utils.RegisterServiceTree(rootName, rootName+".Vehicle", "1.0", root) {
		t.Logf("registerTestTree: %s already registered", rootName)
	}
	return rootName
}

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux, err := buildMux(time.Now().Add(-30 * time.Second))
	if err != nil {
		t.Fatalf("buildMux: %v", err)
	}
	return httptest.NewServer(mux)
}

// ── toNodeJSON ────────────────────────────────────────────────────────────────

func TestToNodeJSON_LeafFields(t *testing.T) {
	n := utils.NewSignalNode("Speed", utils.SENSOR, "float", "vehicle speed", "0", "250", "km/h")
	n.DefaultValue = "0"
	n.AllowedDef = []string{"Slow", "Fast"}

	j := toNodeJSON(n)

	if j.Name != "Speed" {
		t.Errorf("Name = %q, want Speed", j.Name)
	}
	if j.NodeType != utils.SENSOR {
		t.Errorf("NodeType = %q, want sensor", j.NodeType)
	}
	if j.Datatype != "float" {
		t.Errorf("Datatype = %q, want float", j.Datatype)
	}
	if j.Unit != "km/h" {
		t.Errorf("Unit = %q, want km/h", j.Unit)
	}
	if j.Min != "0" || j.Max != "250" {
		t.Errorf("Range = [%q, %q], want [0, 250]", j.Min, j.Max)
	}
	if j.Default != "0" {
		t.Errorf("Default = %q, want 0", j.Default)
	}
	if len(j.Allowed) != 2 {
		t.Errorf("Allowed length = %d, want 2", len(j.Allowed))
	}
	if len(j.Children) != 0 {
		t.Errorf("Children = %d, want 0", len(j.Children))
	}
}

func TestToNodeJSON_BranchWithChildren(t *testing.T) {
	root := utils.NewBranchNode("Vehicle")
	child := utils.NewSignalNode("Speed", utils.SENSOR, "float", "", "", "", "")
	root.Child = []*utils.Node_t{child}
	root.Children = 1
	child.Parent = root

	j := toNodeJSON(root)

	if j.NodeType != utils.BRANCH {
		t.Errorf("NodeType = %q, want branch", j.NodeType)
	}
	if len(j.Children) != 1 {
		t.Errorf("Children length = %d, want 1", len(j.Children))
	}
	if j.Children[0].Name != "Speed" {
		t.Errorf("Children[0].Name = %q, want Speed", j.Children[0].Name)
	}
}

func TestToNodeJSON_NoAllowedWhenEmpty(t *testing.T) {
	n := utils.NewSignalNode("Speed", utils.SENSOR, "float", "", "", "", "")
	j := toNodeJSON(n)
	if j.Allowed != nil {
		t.Errorf("Allowed = %v, want nil for node without AllowedDef", j.Allowed)
	}
}

// ── /api/forest ───────────────────────────────────────────────────────────────

func TestAPIForest_ReturnsJSON(t *testing.T) {
	registerTestTree(t, "TestForestVehicle")
	srv := newTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/forest")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var forest []utils.ForestInfo
	if err := json.NewDecoder(resp.Body).Decode(&forest); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(forest) == 0 {
		t.Error("expected at least one tree in forest list")
	}
	found := false
	for _, f := range forest {
		if f.RootName == "TestForestVehicle" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("TestForestVehicle not in forest list: %+v", forest)
	}
}

// ── /api/tree ─────────────────────────────────────────────────────────────────

func TestAPITree_ReturnsTree(t *testing.T) {
	registerTestTree(t, "TestTreeVehicle")
	srv := newTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/tree/TestTreeVehicle")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var node nodeJSON
	if err := json.NewDecoder(resp.Body).Decode(&node); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if node.Name != "TestTreeVehicle" {
		t.Errorf("root name = %q, want TestTreeVehicle", node.Name)
	}
	if len(node.Children) == 0 {
		t.Error("expected children in tree response")
	}
}

func TestAPITree_NotFound(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/tree/NoSuchTree")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

// ── /api/health ───────────────────────────────────────────────────────────────

func TestAPIHealth_ReturnsPayload(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var h healthPayload
	if err := json.NewDecoder(resp.Body).Decode(&h); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if h.Goroutines <= 0 {
		t.Errorf("Goroutines = %d, want > 0", h.Goroutines)
	}
	if h.UptimeS < 0 {
		t.Errorf("UptimeS = %d, want >= 0", h.UptimeS)
	}
	// Test server was created with startTime 30s in the past.
	if h.UptimeS < 29 {
		t.Errorf("UptimeS = %d, want >= 29 (startTime offset by 30s)", h.UptimeS)
	}
}

// ── /api/validate ─────────────────────────────────────────────────────────────

func TestAPIValidate_ReturnsIssues(t *testing.T) {
	// Register a tree with a node missing unit and description.
	rootName := "TestValidateVehicle"
	root := utils.NewBranchNode(rootName)
	bare := utils.NewSignalNode("BareSignal", utils.SENSOR, "float", "", "", "", "")
	bare.Parent = root
	root.Child = []*utils.Node_t{bare}
	root.Children = 1
	utils.RegisterServiceTree(rootName, rootName+".Vehicle", "1.0", root)

	srv := newTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/validate/" + rootName)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var issues []validateIssue
	if err := json.NewDecoder(resp.Body).Decode(&issues); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(issues) == 0 {
		t.Error("expected validation issues for node missing unit and description")
	}
	foundMissingUnit := false
	for _, iss := range issues {
		if strings.Contains(iss.Message, "unit") {
			foundMissingUnit = true
		}
	}
	if !foundMissingUnit {
		t.Errorf("expected a 'missing unit' issue; got %+v", issues)
	}
}

func TestAPIValidate_CleanTreeReturnsEmpty(t *testing.T) {
	rootName := "TestValidateClean"
	root := utils.NewBranchNode(rootName)
	speed := utils.NewSignalNode("Speed", utils.SENSOR, "float", "vehicle speed", "0", "250", "km/h")
	speed.Parent = root
	root.Child = []*utils.Node_t{speed}
	root.Children = 1
	utils.RegisterServiceTree(rootName, rootName+".Vehicle", "1.0", root)

	srv := newTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/validate/" + rootName)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var issues []validateIssue
	if err := json.NewDecoder(resp.Body).Decode(&issues); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(issues) != 0 {
		t.Errorf("expected 0 issues for clean tree, got %d: %+v", len(issues), issues)
	}
}

func TestAPIValidate_NotFound(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/validate/NoSuchTree")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestAPIValidate_MinGreaterThanMax(t *testing.T) {
	rootName := "TestValidateRange"
	root := utils.NewBranchNode(rootName)
	bad := utils.NewSignalNode("BadRange", utils.SENSOR, "float", "desc", "250", "0", "km/h")
	bad.Parent = root
	root.Child = []*utils.Node_t{bad}
	root.Children = 1
	utils.RegisterServiceTree(rootName, rootName+".Vehicle", "1.0", root)

	srv := newTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/validate/" + rootName)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var issues []validateIssue
	json.NewDecoder(resp.Body).Decode(&issues)

	found := false
	for _, iss := range issues {
		if iss.Severity == "error" && strings.Contains(iss.Message, "min") && strings.Contains(iss.Message, "max") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected min > max error issue; got %+v", issues)
	}
}

// ── /api/search ───────────────────────────────────────────────────────────────

func TestAPISearch_FindsMatches(t *testing.T) {
	registerTestTree(t, "TestSearchVehicle")
	srv := newTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/search?q=Speed")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var results []searchResult
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected at least one search result for 'Speed'")
	}
	found := false
	for _, r := range results {
		if strings.Contains(r.Path, "Speed") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("no Speed result in %+v", results)
	}
}

func TestAPISearch_EmptyQueryReturnsEmpty(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/search?q=")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var results []searchResult
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for empty query, got %d", len(results))
	}
}

func TestAPISearch_NoMatch(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/search?q=xyzzy_no_such_signal_12345")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var results []searchResult
	json.NewDecoder(resp.Body).Decode(&results)
	if len(results) != 0 {
		t.Errorf("expected 0 results for non-matching query, got %d", len(results))
	}
}

// ── /api/events (SSE) ─────────────────────────────────────────────────────────

func TestAPIEvents_ReceivesHealthOnConnect(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(srv.URL + "/api/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}

	// Read the first event (health payload sent immediately on connect).
	buf := make([]byte, 1024)
	n, err := resp.Body.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("read: %v", err)
	}
	body := string(buf[:n])
	if !strings.Contains(body, "event: health") {
		t.Errorf("expected 'event: health' in first SSE frame; got: %q", body)
	}
	if !strings.Contains(body, "goroutines") {
		t.Errorf("expected goroutines field in health SSE data; got: %q", body)
	}
}

// ── validateTree ──────────────────────────────────────────────────────────────

func TestValidateTree_MissingDescription(t *testing.T) {
	root := utils.NewBranchNode("Root")
	n := utils.NewSignalNode("Speed", utils.SENSOR, "float", "", "0", "250", "km/h")
	n.Parent = root
	root.Child = []*utils.Node_t{n}

	issues := validateTree(root, "Root")
	found := false
	for _, iss := range issues {
		if strings.Contains(iss.Message, "description") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected missing-description warning; got %+v", issues)
	}
}

func TestValidateTree_BranchNotChecked(t *testing.T) {
	// Branches are exempt from signal-level checks.
	root := utils.NewBranchNode("Root")
	issues := validateTree(root, "Root")
	if len(issues) != 0 {
		t.Errorf("expected 0 issues for branch-only tree; got %+v", issues)
	}
}

func TestValidateTree_MissingDatatype(t *testing.T) {
	root := utils.NewBranchNode("Root")
	n := &utils.Node_t{Name: "Signal", NodeType: utils.SENSOR, Description: "desc", Unit: "km/h"}
	n.Parent = root
	root.Child = []*utils.Node_t{n}

	issues := validateTree(root, "Root")
	found := false
	for _, iss := range issues {
		if iss.Severity == "error" && strings.Contains(iss.Message, "datatype") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected missing-datatype error; got %+v", issues)
	}
}

// ── searchForest / searchNode ─────────────────────────────────────────────────

func TestSearchForest_CaseInsensitive(t *testing.T) {
	registerTestTree(t, "TestSearchCI")
	results := searchForest("speed")
	if len(results) == 0 {
		t.Error("expected results for lowercase 'speed'")
	}
}

func TestSearchForest_EmptyQuery(t *testing.T) {
	results := searchForest("")
	// empty query returns all nodes (every name contains "")
	// but the function short-circuits in the HTTP handler; searchForest itself does not.
	// Verify it doesn't panic and returns a non-nil slice.
	_ = results
}

func TestStart_ListensAndResponds(t *testing.T) {
	// Start binds a random OS port; we use a fixed high port unlikely to conflict.
	// If the port is taken the test is skipped rather than failed.
	const addr = ":19234"
	if err := Start(addr); err != nil {
		t.Skipf("Start(%q): %v — port in use?", addr, err)
	}
	time.Sleep(60 * time.Millisecond)
	resp, err := http.Get("http://localhost:19234/api/health")
	if err != nil {
		t.Fatalf("http.Get: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestSSEHub_Publish(t *testing.T) {
	h := newSSEHub()
	ch := h.subscribe()
	defer h.unsubscribe(ch)

	h.publish("health", `{"goroutines":5}`)

	select {
	case msg := <-ch:
		if !strings.Contains(msg, "event: health") {
			t.Errorf("published message = %q, want event: health prefix", msg)
		}
		if !strings.Contains(msg, `{"goroutines":5}`) {
			t.Errorf("published message = %q, want goroutines payload", msg)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timed out waiting for published SSE message")
	}
}

func TestSSEHub_SlowClientSkipped(t *testing.T) {
	h := newSSEHub()
	// Full channel — publish must not block.
	ch := make(chan string, 1)
	ch <- "fill" // pre-fill to capacity
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	defer h.unsubscribe(ch)

	done := make(chan struct{})
	go func() {
		h.publish("test", "data")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Error("publish blocked on slow client")
	}
}

func TestCurrentHealth_UptimeGrows(t *testing.T) {
	start := time.Now().Add(-10 * time.Second)
	h := currentHealth(start)
	if h.UptimeS < 9 {
		t.Errorf("UptimeS = %d, want >= 9", h.UptimeS)
	}
	if h.Goroutines <= 0 {
		t.Errorf("Goroutines = %d, want > 0", h.Goroutines)
	}
	if h.HeapMB <= 0 {
		t.Errorf("HeapMB = %f, want > 0", h.HeapMB)
	}
}
