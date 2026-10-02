package server

import (
	"net/http"

	"serverpanel/internal/version"
)

// handleGetConfig 给已登录用户看的少量运行时配置。
func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"enable_exec": s.ExecEnabled(),
		"version":     version.Version,
	})
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"enable_exec": s.ExecEnabled(),
	})
}

func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		EnableExec *bool `json:"enable_exec"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.EnableExec != nil {
		s.execEnabled.Store(*req.EnableExec)
		v := "false"
		if *req.EnableExec {
			v = "true"
		}
		if err := s.st.SetSetting("enable_exec", v); err != nil {
			writeErr(w, http.StatusInternalServerError, "保存失败")
			return
		}
		s.audit(r, 0, "settings", 0, 0, "enable_exec="+v, "成功")
	}
	writeJSON(w, http.StatusOK, map[string]any{"enable_exec": s.ExecEnabled()})
}
