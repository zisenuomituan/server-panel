package server

import (
	"net/http"
	"time"

	"serverpanel/internal/version"
)

// handleHealth 给外部监控探活用，不需要登录。
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"version": version.Version,
		"time":    time.Now(),
	})
}
