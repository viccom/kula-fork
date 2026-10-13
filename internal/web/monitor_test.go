package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"kula/internal/config"
	"kula/internal/sysinfo"
)

// TestMonitorRoutes covers /api/processes and /api/connections: both sit behind
// the same authentication and show_system_info gate as the inventory route, and
// both serialise an array (never null) so the monitor tables can iterate them
// without a guard.
func TestMonitorRoutes(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Web.UI = true
	cfg.Web.Auth.Enabled = true
	cfg.Web.Auth.SessionTimeout = time.Hour
	cfg.Global.ShowSystemInfo = true
	s := NewServer(cfg.Web, cfg.Global, nil, nil, t.TempDir(), config.OllamaConfig{})
	handler := s.buildHandler()
	request := func(token, path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", path, nil)
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	for _, path := range []string{"/api/processes", "/api/connections"} {
		if w := request("", path); w.Code != http.StatusUnauthorized {
			t.Fatalf("%s unprotected: %d", path, w.Code)
		}
	}
	token, err := s.auth.CreateSession("test")
	if err != nil {
		t.Fatal(err)
	}
	w := request(token, "/api/processes")
	if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("processes: %d %s", w.Code, w.Body.String())
	}
	var processes sysinfo.ProcessList
	if err := json.Unmarshal(w.Body.Bytes(), &processes); err != nil {
		t.Fatal(err)
	}
	if processes.Timestamp.IsZero() || processes.Processes == nil {
		t.Fatalf("process payload malformed: %+v", processes)
	}
	w = request(token, "/api/connections")
	if w.Code != http.StatusOK {
		t.Fatalf("connections: %d %s", w.Code, w.Body.String())
	}
	var connections sysinfo.ConnectionList
	if err := json.Unmarshal(w.Body.Bytes(), &connections); err != nil {
		t.Fatal(err)
	}
	if connections.Timestamp.IsZero() || connections.Connections == nil {
		t.Fatalf("connection payload malformed: %+v", connections)
	}
	s.global.ShowSystemInfo = false
	if w := request(token, "/api/processes"); w.Code != http.StatusNotFound {
		t.Fatalf("disabled monitor exposed data: %d", w.Code)
	}
	if w := request(token, "/api/connections"); w.Code != http.StatusNotFound {
		t.Fatalf("disabled monitor exposed data: %d", w.Code)
	}
}

func TestMonitorMethods(t *testing.T) {
	s := &Server{global: config.GlobalConfig{ShowSystemInfo: true}}
	for _, path := range []string{"/api/processes", "/api/connections"} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", path, nil)
		if path == "/api/processes" {
			s.handleProcesses(w, r)
		} else {
			s.handleConnections(w, r)
		}
		if w.Code != http.StatusMethodNotAllowed || w.Header().Get("Allow") != "GET, HEAD" {
			t.Fatalf("%s accepted an unsupported method: %d", path, w.Code)
		}
	}
}
