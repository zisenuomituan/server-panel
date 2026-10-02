package server

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"serverpanel/internal/auth"
	"serverpanel/internal/config"
	"serverpanel/internal/libvirt"
	"serverpanel/internal/sshpool"
	"serverpanel/internal/store"
)

type Server struct {
	cfg  *config.Config
	st   *store.Store
	tok  *auth.TokenIssuer
	pool *sshpool.Pool
	hub  *Hub

	// fake 后端在开发模式下全局共用一份
	fake *libvirt.FakeBackend

	mu      sync.RWMutex
	live    map[int64]*LiveMetric // serverID -> 最新指标
	cpuPrev map[string]cpuSample  // domain -> 上次 cpu.time
}

func New(cfg *config.Config, st *store.Store) *Server {
	s := &Server{
		cfg:     cfg,
		st:      st,
		tok:     auth.NewTokenIssuer(cfg.JWTSecret, time.Duration(cfg.TokenHours)*time.Hour),
		hub:     newHub(),
		live:    map[int64]*LiveMetric{},
		cpuPrev: map[string]cpuSample{},
	}
	if cfg.Backend == "ssh" {
		s.pool = sshpool.New(cfg.SSHKeyPath)
	} else {
		s.fake = libvirt.NewFake()
	}
	return s
}

func (s *Server) Routes(webFS http.FileSystem) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)

	r.Route("/api", func(r chi.Router) {
		r.Post("/auth/register", s.handleRegister)
		r.Post("/auth/login", s.handleLogin)

		r.Group(func(r chi.Router) {
			r.Use(s.requireAuth)
			r.Get("/me", s.handleMe)
			r.Post("/me/password", s.handleChangePassword)
			r.Post("/bind", s.handleBind)
			r.Get("/hosts", s.handleHosts)
			r.Get("/hosts/{id}/servers", s.handleHostServers)
			r.Post("/hosts/{id}/unbind", s.handleUnbind)
			r.Get("/servers/{id}", s.handleServerDetail)
			r.Get("/servers/{id}/history", s.handleServerHistory)
			r.Post("/servers/{id}/power", s.handlePower)
			r.Post("/servers/{id}/exec", s.handleExec)
			r.Get("/audit", s.handleAudit)
			r.Get("/ws", s.hub.serveWS(s))

			r.Route("/admin", func(r chi.Router) {
				r.Use(s.requireAdmin)
				r.Post("/hosts", s.handleCreateHost)
				r.Get("/hosts", s.handleAdminHosts)
				r.Delete("/hosts/{id}", s.handleDeleteHost)
				r.Post("/hosts/{id}/rotate", s.handleRotateKey)
				r.Post("/servers/{id}/rotate", s.handleRotateServerKey)
				r.Post("/hosts/{id}/exec", s.handleHostExec)
				r.Post("/keys/{id}/revoke", s.handleRevokeKey)
				r.Get("/keys", s.handleAdminKeys)
			})
		})
	})

	// 一键安装分发
	r.Get("/install.sh", s.handleInstallScript)
	r.Get("/setup-host.sh", s.serveScript("setup-host.sh"))
	r.Get("/setup-target.sh", s.serveScript("setup-target.sh"))
	r.Get("/release/center", s.handleReleaseCenter)
	r.Get("/release/vm-collect", s.handleReleaseCollector)

	// 前端静态资源
	r.Handle("/*", http.FileServer(webFS))
	return r
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// backendFor 返回操作某台宿主机的 libvirt 后端。
func (s *Server) backendFor(h hostConn) libvirt.Backend {
	if s.fake != nil {
		return s.fake
	}
	return libvirt.NewSSH(s.pool, h.Host, h.Port, h.User, h.URI)
}

type hostConn struct {
	Host string
	Port int
	User string
	URI  string
}
