package server

import (
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"serverpanel/internal/bindkey"
	"serverpanel/internal/model"
)

type serverView struct {
	model.Server
	Live *LiveMetric `json:"live"`
}

type hostView struct {
	model.Host
	Servers []serverView `json:"servers"`
}

// hostAccess 描述用户对某台宿主能看到哪些虚拟机。
// all=true 表示整台宿主（宿主级绑定）；否则只包含 only 里的那几台。
type hostAccess struct {
	host model.Host
	all  bool
	only map[int64]bool
}

func (s *Server) accessMap(userID int64, role string) (map[int64]*hostAccess, error) {
	out := map[int64]*hostAccess{}

	if role == model.RoleAdmin {
		hosts, err := s.st.Hosts()
		if err != nil {
			return nil, err
		}
		for _, h := range hosts {
			out[h.ID] = &hostAccess{host: h, all: true}
		}
		return out, nil
	}

	hosts, err := s.st.HostsForUser(userID)
	if err != nil {
		return nil, err
	}
	for _, h := range hosts {
		out[h.ID] = &hostAccess{host: h, all: true}
	}

	servers, err := s.st.ServersForUser(userID)
	if err != nil {
		return nil, err
	}
	for _, sv := range servers {
		a, ok := out[sv.HostID]
		if !ok {
			host, err := s.st.HostByID(sv.HostID)
			if err != nil {
				continue
			}
			a = &hostAccess{host: *host, only: map[int64]bool{}}
			out[sv.HostID] = a
		}
		if a.all {
			continue
		}
		a.only[sv.ID] = true
	}
	return out, nil
}

func (s *Server) hostViews(userID int64, role string) ([]hostView, error) {
	access, err := s.accessMap(userID, role)
	if err != nil {
		return nil, err
	}

	ids := make([]int64, 0, len(access))
	for id := range access {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	out := make([]hostView, 0, len(ids))
	for _, id := range ids {
		a := access[id]
		servers, _ := s.st.ServersByHost(id)
		views := make([]serverView, 0, len(servers))
		for _, sv := range servers {
			if !a.all && !a.only[sv.ID] {
				continue
			}
			views = append(views, serverView{Server: sv, Live: s.liveFor(sv.ID)})
		}
		out = append(out, hostView{Host: a.host, Servers: views})
	}
	return out, nil
}

func (s *Server) handleHosts(w http.ResponseWriter, r *http.Request) {
	c := claimsOf(r)
	out, err := s.hostViews(c.UserID, c.Role)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读取宿主机失败")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleHostServers(w http.ResponseWriter, r *http.Request) {
	c := claimsOf(r)
	hostID := parseID(chi.URLParam(r, "id"))

	wholeHost := s.canAccessHost(c.UserID, c.Role, hostID)

	only := map[int64]bool{}
	if !wholeHost {
		bound, _ := s.st.ServersForUser(c.UserID)
		for _, sv := range bound {
			if sv.HostID == hostID {
				only[sv.ID] = true
			}
		}
		if len(only) == 0 {
			writeErr(w, http.StatusForbidden, "没有绑定这台宿主机")
			return
		}
	}

	servers, _ := s.st.ServersByHost(hostID)
	views := make([]serverView, 0, len(servers))
	for _, sv := range servers {
		if !wholeHost && !only[sv.ID] {
			continue
		}
		views = append(views, serverView{Server: sv, Live: s.liveFor(sv.ID)})
	}
	writeJSON(w, http.StatusOK, views)
}

func (s *Server) handleUnbind(w http.ResponseWriter, r *http.Request) {
	c := claimsOf(r)
	hostID := parseID(chi.URLParam(r, "id"))
	if err := s.st.UnbindHostAndServers(c.UserID, hostID); err != nil {
		writeErr(w, http.StatusInternalServerError, "解绑失败")
		return
	}
	s.audit(r, c.UserID, "unbind", hostID, 0, "", "成功")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	c := claimsOf(r)

	var logs []model.AuditLog
	var err error
	if c.Role == model.RoleAdmin {
		logs, err = s.st.Audits(300)
	} else {
		logs, err = s.st.AuditsForUser(c.UserID, 300)
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读取审计日志失败")
		return
	}
	if logs == nil {
		logs = []model.AuditLog{}
	}
	writeJSON(w, http.StatusOK, logs)
}

// ---------- 管理员 ----------

type createHostReq struct {
	Name       string `json:"name"`
	SSHHost    string `json:"ssh_host"`
	SSHPort    int    `json:"ssh_port"`
	SSHUser    string `json:"ssh_user"`
	LibvirtURI string `json:"libvirt_uri"`
	Days       int    `json:"expires_days"`
}

// 登记一台宿主机，并生成一把 32 位绑定密钥。明文只在这里返回一次。
func (s *Server) handleCreateHost(w http.ResponseWriter, r *http.Request) {
	var req createHostReq
	if !decode(w, r, &req) {
		return
	}
	if req.Name == "" || req.SSHHost == "" || req.SSHPort == 0 {
		writeErr(w, http.StatusBadRequest, "名称、地址、端口都要填")
		return
	}
	if req.SSHUser == "" {
		req.SSHUser = "panel"
	}
	uri := req.LibvirtURI
	if uri == "" {
		uri = s.cfg.LibvirtURI
	}

	h := &model.Host{
		Name:       req.Name,
		SSHHost:    req.SSHHost,
		SSHPort:    req.SSHPort,
		SSHUser:    req.SSHUser,
		LibvirtURI: uri,
	}
	if err := s.st.CreateHost(h); err != nil {
		writeErr(w, http.StatusInternalServerError, "创建宿主机失败")
		return
	}

	plain, expires, err := s.issueKey(h.ID, 0, req.Days)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "生成密钥失败")
		return
	}

	s.audit(r, 0, "create_host", h.ID, 0, h.Name, "成功")

	writeJSON(w, http.StatusOK, map[string]any{
		"host":       h,
		"bind_key":   plain,
		"formatted":  bindkey.Format(plain),
		"expires_at": expires,
	})
}

func (s *Server) handleDeleteHost(w http.ResponseWriter, r *http.Request) {
	hostID := parseID(chi.URLParam(r, "id"))
	if _, err := s.st.HostByID(hostID); err != nil {
		writeErr(w, http.StatusNotFound, "宿主机不存在")
		return
	}
	if err := s.st.DeleteHost(hostID); err != nil {
		writeErr(w, http.StatusInternalServerError, "删除失败")
		return
	}
	s.audit(r, 0, "delete_host", hostID, 0, "", "成功")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleAdminHosts(w http.ResponseWriter, r *http.Request) {
	hosts, err := s.st.Hosts()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读取宿主机失败")
		return
	}
	if hosts == nil {
		hosts = []model.Host{}
	}
	writeJSON(w, http.StatusOK, hosts)
}

// keyMeta 去掉哈希，只给前端展示需要的信息。
func keyMeta(k model.BindKey) map[string]any {
	return map[string]any{
		"id":         k.ID,
		"prefix":     k.Prefix,
		"status":     k.Status,
		"server_id":  k.ServerID,
		"created_at": k.CreatedAt,
		"expires_at": k.ExpiresAt,
	}
}

func (s *Server) handleAdminKeys(w http.ResponseWriter, r *http.Request) {
	hosts, _ := s.st.Hosts()
	out := make([]map[string]any, 0)

	for _, h := range hosts {
		servers, _ := s.st.ServersByHost(h.ID)

		serverItems := make([]map[string]any, 0, len(servers))
		for _, sv := range servers {
			keys, _ := s.st.KeysForServer(sv.ID)
			items := make([]map[string]any, 0, len(keys))
			for _, k := range keys {
				items = append(items, keyMeta(k))
			}
			serverItems = append(serverItems, map[string]any{
				"id":   sv.ID,
				"name": sv.Name,
				"keys": items,
			})
		}

		// 宿主级密钥（server_id = 0）
		allKeys, _ := s.st.KeysForHost(h.ID)
		hostItems := make([]map[string]any, 0)
		for _, k := range allKeys {
			if k.ServerID == 0 {
				hostItems = append(hostItems, keyMeta(k))
			}
		}

		out = append(out, map[string]any{
			"host":      h,
			"host_keys": hostItems,
			"servers":   serverItems,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleRotateKey(w http.ResponseWriter, r *http.Request) {
	hostID := parseID(chi.URLParam(r, "id"))
	if _, err := s.st.HostByID(hostID); err != nil {
		writeErr(w, http.StatusNotFound, "宿主机不存在")
		return
	}
	_ = s.st.RevokeKeysForHost(hostID)

	var body struct {
		ExpiresDays int `json:"expires_days"`
	}
	_ = decode(w, r, &body)
	days := body.ExpiresDays
	if days <= 0 {
		days = 365
	}

	plain, expires, err := s.issueKey(hostID, 0, days)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "生成密钥失败")
		return
	}
	s.audit(r, 0, "rotate_key", hostID, 0, "", "成功")
	writeJSON(w, http.StatusOK, map[string]any{
		"bind_key":   plain,
		"formatted":  bindkey.Format(plain),
		"expires_at": expires,
	})
}

// handleRotateServerKey 给单台虚拟机生成/轮换绑定密钥。
func (s *Server) handleRotateServerKey(w http.ResponseWriter, r *http.Request) {
	serverID := parseID(chi.URLParam(r, "id"))
	sv, err := s.st.ServerByID(serverID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "虚拟机不存在")
		return
	}
	_ = s.st.RevokeKeysForServer(serverID)

	var body struct {
		ExpiresDays int `json:"expires_days"`
	}
	_ = decode(w, r, &body)
	days := body.ExpiresDays
	if days <= 0 {
		days = 365
	}

	plain, expires, err := s.issueKey(sv.HostID, sv.ID, days)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "生成密钥失败")
		return
	}
	s.audit(r, 0, "rotate_key", sv.HostID, sv.ID, sv.Name, "成功")
	writeJSON(w, http.StatusOK, map[string]any{
		"bind_key":   plain,
		"formatted":  bindkey.Format(plain),
		"expires_at": expires,
	})
}

func (s *Server) handleRevokeKey(w http.ResponseWriter, r *http.Request) {
	keyID := parseID(chi.URLParam(r, "id"))
	if err := s.st.RevokeKey(keyID); err != nil {
		writeErr(w, http.StatusInternalServerError, "撤销失败")
		return
	}
	s.audit(r, 0, "revoke_key", 0, 0, "key#"+chi.URLParam(r, "id"), "成功")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// issueKey 生成密钥。serverID 为 0 是宿主级，大于 0 是单机级。
func (s *Server) issueKey(hostID, serverID int64, days int) (string, *time.Time, error) {
	plain, err := bindkey.New()
	if err != nil {
		return "", nil, err
	}
	var expires *time.Time
	if days > 0 {
		t := time.Now().AddDate(0, 0, days)
		expires = &t
	}
	k := &model.BindKey{
		HostID:    hostID,
		ServerID:  serverID,
		Prefix:    bindkey.Prefix(plain),
		KeyHash:   bindkey.Hash(plain),
		ExpiresAt: expires,
	}
	if err := s.st.CreateBindKey(k); err != nil {
		return "", nil, err
	}
	return plain, expires, nil
}

func (s *Server) canAccessHost(userID int64, role string, hostID int64) bool {
	if role == model.RoleAdmin {
		return true
	}
	ok, _ := s.st.IsBound(userID, hostID)
	return ok
}

// canAccessServer 宿主级绑定能看整台，单机绑定只能看那一台。
func (s *Server) canAccessServer(userID int64, role string, sv *model.Server) bool {
	if role == model.RoleAdmin {
		return true
	}
	if ok, _ := s.st.IsBound(userID, sv.HostID); ok {
		return true
	}
	ok, _ := s.st.IsServerBound(userID, sv.ID)
	return ok
}

func parseID(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}
