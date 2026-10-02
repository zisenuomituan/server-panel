package server

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"serverpanel/internal/model"
)

const (
	execTimeout = 20 * time.Second
	maxOutput   = 64 << 10
)

type execReq struct {
	Command string `json:"command"`
}

type execResp struct {
	Output   string `json:"output"`
	ExitCode int    `json:"exit_code"`
}

// handleExec 在指定虚拟机上执行一条命令（走它的 Guest SSH 通道）。
func (s *Server) handleExec(w http.ResponseWriter, r *http.Request) {
	c := claimsOf(r)
	if !s.cfg.EnableExec {
		writeErr(w, http.StatusForbidden, "命令功能已关闭")
		return
	}
	if c.Role == model.RoleViewer {
		writeErr(w, http.StatusForbidden, "只读账号不能执行命令")
		return
	}

	sv, ok := s.authorizedServer(w, r, c.UserID, c.Role)
	if !ok {
		return
	}

	var req execReq
	if !decode(w, r, &req) {
		return
	}
	cmd := strings.TrimSpace(req.Command)
	if cmd == "" {
		writeErr(w, http.StatusBadRequest, "命令不能为空")
		return
	}

	if s.pool == nil {
		s.audit(r, c.UserID, "exec", sv.HostID, sv.ID, cmd, "成功(开发模式)")
		writeJSON(w, http.StatusOK, execResp{Output: fakeExec(cmd), ExitCode: 0})
		return
	}

	if sv.SSHPort == 0 {
		writeErr(w, http.StatusBadRequest, "这台虚拟机还没配置命令通道（Guest SSH 端口）")
		return
	}

	host, err := s.st.HostByID(sv.HostID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "宿主机不存在")
		return
	}
	user := sv.SSHUser
	if user == "" {
		user = "root"
	}
	addr := fmt.Sprintf("%s:%d", host.SSHHost, sv.SSHPort)

	out, code, err := s.pool.RunShell(addr, user, cmd, execTimeout)
	out = truncate(out, maxOutput)

	if err != nil {
		s.audit(r, c.UserID, "exec", sv.HostID, sv.ID, cmd, "失败: "+err.Error())
		writeJSON(w, http.StatusOK, execResp{Output: out + "\n" + err.Error(), ExitCode: -1})
		return
	}
	s.audit(r, c.UserID, "exec", sv.HostID, sv.ID, cmd, fmt.Sprintf("退出码 %d", code))
	writeJSON(w, http.StatusOK, execResp{Output: out, ExitCode: code})
}

// handleHostExec 在宿主机上执行命令，只有管理员可用。
func (s *Server) handleHostExec(w http.ResponseWriter, r *http.Request) {
	c := claimsOf(r)
	if !s.cfg.EnableExec {
		writeErr(w, http.StatusForbidden, "命令功能已关闭")
		return
	}

	hostID := parseID(chi.URLParam(r, "id"))
	host, err := s.st.HostByID(hostID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "宿主机不存在")
		return
	}

	var req execReq
	if !decode(w, r, &req) {
		return
	}
	cmd := strings.TrimSpace(req.Command)
	if cmd == "" {
		writeErr(w, http.StatusBadRequest, "命令不能为空")
		return
	}

	if s.pool == nil {
		s.audit(r, c.UserID, "host_exec", hostID, 0, cmd, "成功(开发模式)")
		writeJSON(w, http.StatusOK, execResp{Output: fakeExec(cmd), ExitCode: 0})
		return
	}

	user := host.SSHUser
	if user == "" {
		user = "root"
	}
	addr := fmt.Sprintf("%s:%d", host.SSHHost, host.SSHPort)

	out, code, err := s.pool.RunShell(addr, user, cmd, execTimeout)
	out = truncate(out, maxOutput)

	if err != nil {
		s.audit(r, c.UserID, "host_exec", hostID, 0, cmd, "失败: "+err.Error())
		writeJSON(w, http.StatusOK, execResp{Output: out + "\n" + err.Error(), ExitCode: -1})
		return
	}
	s.audit(r, c.UserID, "host_exec", hostID, 0, cmd, fmt.Sprintf("退出码 %d", code))
	writeJSON(w, http.StatusOK, execResp{Output: out, ExitCode: code})
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "\n...(输出过长已截断)"
}

// fakeExec 在没有真实后端的开发模式下给个像样的回应。
func fakeExec(cmd string) string {
	switch {
	case strings.HasPrefix(cmd, "uname"):
		return "Linux fake-host 6.1.0 #1 SMP x86_64 GNU/Linux"
	case strings.HasPrefix(cmd, "uptime"):
		return " 14:22:01 up 12 days,  3:14,  1 user,  load average: 0.31, 0.28, 0.21"
	case strings.HasPrefix(cmd, "df"):
		return "Filesystem      Size  Used Avail Use% Mounted on\n/dev/vda1        40G   12G   26G  32% /"
	default:
		return fmt.Sprintf("[$ %s]\n开发模式（fake backend）下命令不会真正执行。", cmd)
	}
}
