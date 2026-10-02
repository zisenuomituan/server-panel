package server

import (
	"context"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"serverpanel/internal/model"
)

func (s *Server) handleServerDetail(w http.ResponseWriter, r *http.Request) {
	c := claimsOf(r)
	sv, ok := s.authorizedServer(w, r, c.UserID, c.Role)
	if !ok {
		return
	}
	host, _ := s.st.HostByID(sv.HostID)
	writeJSON(w, http.StatusOK, map[string]any{
		"server": sv,
		"host":   host,
		"live":   s.liveFor(sv.ID),
	})
}

func (s *Server) handleServerHistory(w http.ResponseWriter, r *http.Request) {
	c := claimsOf(r)
	sv, ok := s.authorizedServer(w, r, c.UserID, c.Role)
	if !ok {
		return
	}

	limit := 120
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 2000 {
			limit = n
		}
	}
	points, err := s.st.History(sv.ID, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读取历史指标失败")
		return
	}
	if points == nil {
		points = []model.Metric{}
	}
	writeJSON(w, http.StatusOK, points)
}

type powerReq struct {
	Action string `json:"action"`
}

func (s *Server) handlePower(w http.ResponseWriter, r *http.Request) {
	c := claimsOf(r)
	sv, ok := s.authorizedServer(w, r, c.UserID, c.Role)
	if !ok {
		return
	}

	var req powerReq
	if !decode(w, r, &req) {
		return
	}

	host, err := s.st.HostByID(sv.HostID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "宿主机不存在")
		return
	}
	backend := s.backendFor(hostConn{Host: host.SSHHost, Port: host.SSHPort, User: host.SSHUser, URI: host.LibvirtURI})

	ctx, cancel := context.WithTimeout(r.Context(), 30e9)
	defer cancel()

	var actErr error
	switch req.Action {
	case "start":
		actErr = backend.Start(ctx, sv.DomainName)
	case "shutdown":
		actErr = backend.Shutdown(ctx, sv.DomainName)
	case "force-off":
		actErr = backend.ForceOff(ctx, sv.DomainName)
	case "reboot":
		actErr = backend.Reboot(ctx, sv.DomainName)
	default:
		writeErr(w, http.StatusBadRequest, "不认识的操作: "+req.Action)
		return
	}

	if actErr != nil {
		s.audit(r, c.UserID, "power", sv.HostID, sv.ID, req.Action, "失败: "+actErr.Error())
		writeErr(w, http.StatusBadGateway, actErr.Error())
		return
	}
	s.audit(r, c.UserID, "power", sv.HostID, sv.ID, req.Action, "成功")

	// 立刻重新采集一次，别让前端等下一个轮询周期才看到状态变化
	go s.pollOnce(context.Background())

	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "action": req.Action})
}

// authorizedServer 取出一台虚拟机并确认当前用户有权限。
func (s *Server) authorizedServer(w http.ResponseWriter, r *http.Request, userID int64, role string) (*model.Server, bool) {
	id := parseID(chi.URLParam(r, "id"))
	sv, err := s.st.ServerByID(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "虚拟机不存在")
		return nil, false
	}
	if !s.canAccessServer(userID, role, sv) {
		writeErr(w, http.StatusForbidden, "没有权限")
		return nil, false
	}
	return sv, true
}
