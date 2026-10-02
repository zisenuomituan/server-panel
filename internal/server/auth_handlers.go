package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"serverpanel/internal/auth"
	"serverpanel/internal/bindkey"
	"serverpanel/internal/model"
	"serverpanel/internal/store"
)

type registerReq struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
	Invite   string `json:"invite"`
}

type loginReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
	BindKey  string `json:"bind_key"`
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req registerReq
	if !decode(w, r, &req) {
		return
	}
	if !s.cfg.AllowRegister {
		writeErr(w, http.StatusForbidden, "当前未开放注册")
		return
	}
	if s.cfg.InviteCode != "" && req.Invite != s.cfg.InviteCode {
		writeErr(w, http.StatusForbidden, "邀请码不对")
		return
	}

	req.Username = strings.TrimSpace(req.Username)
	if len(req.Username) < 3 {
		writeErr(w, http.StatusBadRequest, "用户名至少 3 位")
		return
	}
	if len(req.Password) < 8 {
		writeErr(w, http.StatusBadRequest, "密码至少 8 位")
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "密码处理失败")
		return
	}

	role := model.RoleOperator
	// 第一个注册的人直接是管理员
	if n, _ := s.st.CountUsers(); n == 0 {
		role = model.RoleAdmin
	}

	u := &model.User{Username: req.Username, Email: req.Email, PasswordHash: hash, Role: role}
	if err := s.st.CreateUser(u); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			writeErr(w, http.StatusConflict, "用户名已被占用")
			return
		}
		writeErr(w, http.StatusInternalServerError, "创建用户失败")
		return
	}

	s.audit(r, u.ID, "register", 0, 0, u.Username, "成功")

	token, _ := s.tok.Issue(u.ID, u.Username, u.Role)
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "user": u})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginReq
	if !decode(w, r, &req) {
		return
	}

	name := strings.TrimSpace(req.Username)
	ip := clientIP(r)
	userKey := "u:" + name
	ipKey := "ip:" + ip

	// 登录防爆破：账号或来源 IP 任一被锁就拒绝
	if s.login != nil {
		for _, key := range []string{userKey, ipKey} {
			if ok, wait := s.login.Allowed(key); !ok {
				s.audit(r, 0, "login", 0, 0, name, "拒绝: 失败过多临时锁定")
				writeErr(w, http.StatusTooManyRequests,
					fmt.Sprintf("失败次数过多，请 %d 分钟后再试", ceilMinutes(wait)))
				return
			}
		}
	}

	u, err := s.st.UserByName(name)
	if err != nil || !auth.CheckPassword(req.Password, u.PasswordHash) {
		if s.login != nil {
			s.login.Fail(userKey)
			s.login.Fail(ipKey)
		}
		s.audit(r, 0, "login", 0, 0, name, "失败: 用户名或密码不对")
		writeErr(w, http.StatusUnauthorized, "用户名或密码不对")
		return
	}
	if s.login != nil {
		s.login.Success(userKey)
		s.login.Success(ipKey)
	}

	bound := ""
	if req.BindKey != "" {
		msg, err := s.bindWithKey(u.ID, req.BindKey)
		if err != nil {
			s.audit(r, u.ID, "bind", 0, 0, "", "失败: "+err.Error())
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		bound = msg
		s.audit(r, u.ID, "bind", 0, 0, msg, "成功")
	}

	s.audit(r, u.ID, "login", 0, 0, u.Username, "成功")

	token, _ := s.tok.Issue(u.ID, u.Username, u.Role)
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "user": u, "bound": bound})
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	c := claimsOf(r)

	var req struct {
		Old string `json:"old"`
		New string `json:"new"`
	}
	if !decode(w, r, &req) {
		return
	}
	if len(req.New) < 8 {
		writeErr(w, http.StatusBadRequest, "新密码至少 8 位")
		return
	}

	u, err := s.st.UserByID(c.UserID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "用户不存在")
		return
	}
	if !auth.CheckPassword(req.Old, u.PasswordHash) {
		s.audit(r, c.UserID, "change_password", 0, 0, "", "失败: 原密码不对")
		writeErr(w, http.StatusBadRequest, "原密码不对")
		return
	}
	hash, err := auth.HashPassword(req.New)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "密码处理失败")
		return
	}
	if err := s.st.UpdatePassword(c.UserID, hash); err != nil {
		writeErr(w, http.StatusInternalServerError, "保存失败")
		return
	}
	s.audit(r, c.UserID, "change_password", 0, 0, "", "成功")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	c := claimsOf(r)
	u, err := s.st.UserByID(c.UserID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "用户不存在")
		return
	}
	writeJSON(w, http.StatusOK, u)
}

type bindReq struct {
	Key string `json:"key"`
}

func (s *Server) handleBind(w http.ResponseWriter, r *http.Request) {
	var req bindReq
	if !decode(w, r, &req) {
		return
	}
	c := claimsOf(r)

	msg, err := s.bindWithKey(c.UserID, req.Key)
	if err != nil {
		s.audit(r, c.UserID, "bind", 0, 0, "", "失败: "+err.Error())
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	s.audit(r, c.UserID, "bind", 0, 0, msg, "成功")
	writeJSON(w, http.StatusOK, map[string]any{"message": msg})
}

// bindWithKey 校验密钥并按作用域绑定：宿主级密钥绑宿主，单机密钥绑那台虚拟机。
func (s *Server) bindWithKey(userID int64, raw string) (string, error) {
	key := bindkey.Normalize(raw)
	if !bindkey.Valid(key) {
		return "", errors.New("密钥格式不对")
	}

	k, err := s.st.FindActiveKey(bindkey.Hash(key))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return "", errors.New("密钥无效或已失效")
		}
		return "", errors.New("校验密钥出错了")
	}

	if k.ServerID > 0 {
		sv, err := s.st.ServerByID(k.ServerID)
		if err != nil {
			return "", errors.New("密钥对应的虚拟机已不存在")
		}
		if err := s.st.BindServer(userID, k.ServerID, k.ID); err != nil {
			return "", errors.New("绑定失败")
		}
		return "已绑定虚拟机 " + sv.Name, nil
	}

	if err := s.st.Bind(userID, k.HostID, k.ID); err != nil {
		return "", errors.New("绑定失败")
	}
	host, _ := s.st.HostByID(k.HostID)
	name := ""
	if host != nil {
		name = " " + host.Name
	}
	return "已绑定宿主机" + name, nil
}

// ceilMinutes 向上取整到分钟，至少 1。
func ceilMinutes(d time.Duration) int {
	m := int(d.Minutes())
	if d%time.Minute != 0 {
		m++
	}
	if m < 1 {
		m = 1
	}
	return m
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式不对")
		return false
	}
	return true
}
