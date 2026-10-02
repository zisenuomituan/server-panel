package server

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"serverpanel/internal/auth"
	"serverpanel/internal/model"
)

func validRole(r string) bool {
	return r == model.RoleAdmin || r == model.RoleOperator || r == model.RoleViewer
}

func (s *Server) handleAdminUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.st.Users()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读取用户失败")
		return
	}
	if users == nil {
		users = []model.User{}
	}
	writeJSON(w, http.StatusOK, users)
}

type createUserReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
	Email    string `json:"email"`
}

func (s *Server) handleAdminCreateUser(w http.ResponseWriter, r *http.Request) {
	var req createUserReq
	if !decode(w, r, &req) {
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
	if req.Role == "" {
		req.Role = model.RoleOperator
	}
	if !validRole(req.Role) {
		writeErr(w, http.StatusBadRequest, "角色不合法")
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "密码处理失败")
		return
	}
	u := &model.User{Username: req.Username, Email: req.Email, PasswordHash: hash, Role: req.Role}
	if err := s.st.CreateUser(u); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			writeErr(w, http.StatusConflict, "用户名已被占用")
			return
		}
		writeErr(w, http.StatusInternalServerError, "创建用户失败")
		return
	}
	s.audit(r, 0, "user_create", 0, 0, u.Username+" ("+u.Role+")", "成功")
	writeJSON(w, http.StatusOK, u)
}

func (s *Server) handleAdminUpdateUser(w http.ResponseWriter, r *http.Request) {
	id := parseID(chi.URLParam(r, "id"))
	target, err := s.st.UserByID(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "用户不存在")
		return
	}

	var req struct {
		Role string `json:"role"`
	}
	if !decode(w, r, &req) {
		return
	}
	if !validRole(req.Role) {
		writeErr(w, http.StatusBadRequest, "角色不合法")
		return
	}

	// 不能把最后一个管理员降级
	if target.Role == model.RoleAdmin && req.Role != model.RoleAdmin {
		if n, _ := s.st.CountAdmins(); n <= 1 {
			writeErr(w, http.StatusBadRequest, "至少要保留一个管理员")
			return
		}
	}
	if err := s.st.UpdateRole(id, req.Role); err != nil {
		writeErr(w, http.StatusInternalServerError, "修改失败")
		return
	}
	s.audit(r, 0, "user_role", 0, 0, target.Username+" -> "+req.Role, "成功")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleAdminDeleteUser(w http.ResponseWriter, r *http.Request) {
	c := claimsOf(r)
	id := parseID(chi.URLParam(r, "id"))
	target, err := s.st.UserByID(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "用户不存在")
		return
	}
	if id == c.UserID {
		writeErr(w, http.StatusBadRequest, "不能删除自己")
		return
	}
	if target.Role == model.RoleAdmin {
		if n, _ := s.st.CountAdmins(); n <= 1 {
			writeErr(w, http.StatusBadRequest, "至少要保留一个管理员")
			return
		}
	}
	if err := s.st.DeleteUser(id); err != nil {
		writeErr(w, http.StatusInternalServerError, "删除失败")
		return
	}
	s.audit(r, 0, "user_delete", 0, 0, target.Username, "成功")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleAdminResetPassword(w http.ResponseWriter, r *http.Request) {
	id := parseID(chi.URLParam(r, "id"))
	target, err := s.st.UserByID(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "用户不存在")
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if !decode(w, r, &req) {
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
	if err := s.st.UpdatePassword(id, hash); err != nil {
		writeErr(w, http.StatusInternalServerError, "保存失败")
		return
	}
	s.audit(r, 0, "user_passwd", 0, 0, target.Username, "成功")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
