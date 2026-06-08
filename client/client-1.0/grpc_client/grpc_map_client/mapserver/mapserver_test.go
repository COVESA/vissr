/**
* (C) 2026 Matt Jones
*
* Unit tests for mapserver package.
*
* ServeWebViewSite binds to :8085 and blocks indefinitely — integration-only,
* not unit-tested here.
**/
package mapserver

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
)

// TestPingHandler_Returns200 exercises the /version handler directly without
// starting a real HTTP server.
func TestPingHandler_Returns200(t *testing.T) {
	r := mux.NewRouter()
	r.Handle("/version", pingHandler).Methods("GET")

	req := httptest.NewRequest("GET", "/version", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("GET /version status = %d; want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.Len() == 0 {
		t.Error("GET /version returned empty body")
	}
}

func TestReleaseTag_NonEmpty(t *testing.T) {
	if ReleaseTag == "" {
		t.Error("ReleaseTag is empty")
	}
}

// Integration-only entry points — NOT unit-tested here:
//
//   ServeWebViewSite — calls http.ListenAndServe(":8085", r); blocks forever.
