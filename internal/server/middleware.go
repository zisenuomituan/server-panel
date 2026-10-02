package server

import (
	"context"
	"net/http"
	"strings"

	"serverpanel/internal/auth"
	"serverpanel/internal/model"
)

type ctxKey string

const userKey ctxKey = "user"

func (s *Server) parseToken(r *http.Request) (*auth.Claims, bool) {
	raw := ""
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		raw = strings.TrimPrefix(h, "Bearer ")
	} else if q := r.URL.Query().Get("token"); q != "" {
		raw = q
	}
	if raw == "" {
		return nil, false
	}
	claims, err := s.tok.Parse(raw)
	if err != nil {
		return nil, false
	}
	return claims, true
}

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := s.parseToken(r)
		if !ok {
			writeErr(w, http.StatusUnauthorized, "请先登录")
			return
		}
		ctx := context.WithValue(r.Context(), userKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims := claimsOf(r)
		if claims == nil || claims.Role != model.RoleAdmin {
			writeErr(w, http.StatusForbidden, "需要管理员权限")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func claimsOf(r *http.Request) *auth.Claims {
	c, _ := r.Context().Value(userKey).(*auth.Claims)
	return c
}
