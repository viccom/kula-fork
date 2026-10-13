package web

import (
	"encoding/json"
	"net/http"
)

// monitorJSON serialises a read-only monitor payload, enforcing the same
// GET/HEAD-only contract and show_system_info gate as /api/system-info. The
// process and connection monitors expose live host state, so they follow the
// same opt-out switch the inventory page uses.
func (s *Server) monitorJSON(w http.ResponseWriter, r *http.Request, payload func() any) {
	w.Header().Set("Cache-Control", "no-store")
	if !s.global.ShowSystemInfo {
		jsonError(w, "System information is disabled", http.StatusNotFound)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodHead {
		return
	}
	if s.systemInfo == nil {
		jsonError(w, "System information is unavailable", http.StatusServiceUnavailable)
		return
	}
	_ = json.NewEncoder(w).Encode(payload())
}

// handleProcesses serves the process monitor table.
func (s *Server) handleProcesses(w http.ResponseWriter, r *http.Request) {
	s.monitorJSON(w, r, func() any { return s.systemInfo.Processes() })
}

// handleConnections serves the network monitor table.
func (s *Server) handleConnections(w http.ResponseWriter, r *http.Request) {
	s.monitorJSON(w, r, func() any { return s.systemInfo.Connections() })
}
