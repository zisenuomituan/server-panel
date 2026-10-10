package server

import (
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"serverpanel/internal/auth"
	"serverpanel/internal/config"
	"serverpanel/internal/libvirt"
	"serverpanel/internal/loginlimit"
	"serverpanel/internal/sshpool"
	"serverpanel/internal/store"
)

type Server struct {
	cfg  *config.Config
	st   *store.Store
	tok  *auth.TokenIssuer
	pool *sshpool.Pool
	hub  *Hub

	// 登录防爆破
	login *loginlimit.Limiter

	// 网页命令行开关，可在管理页随时改
	execEnabled atomic.Bool

	// fake 后端在开发模式下全局共用一份
	fake *libvirt.FakeBackend

	mu      sync.RWMutex
	live    map[int64]*LiveMetric // serverID -> 最新指标
	cpuPrev map[string]cpuSample  // domain -> 上次 cpu.time

	// 告警：连续超阈值的计数、上次看到的运行状态
	alertMu     sync.Mutex
	alertStreak map[int64]*alertStreak
	alertState  map[int64]string
}

func New(cfg *config.Config, st *store.Store) *Server {
	s := &Server{
		cfg:     cfg,
		st:      st,
		tok:     auth.NewTokenIssuer(cfg.JWTSecret, time.Duration(cfg.TokenHours)*time.Hour),
		hub:     newHub(),
		live:    map[int64]*LiveMetric{},
		cpuPrev: map[string]cpuSample{},

		alertStreak: map[int64]*alertStreak{},
		alertState:  map[int64]string{},
	}
	// 网页命令行：默认取配置，若数据库里有运行时设置则以它为准
	s.execEnabled.Store(cfg.EnableExec)
	if v, ok, err := st.GetSetting("enable_exec"); err == nil && ok {
		s.execEnabled.Store(v == "true" || v == "1")
	}

	if cfg.LoginProtect {
		s.login = loginlimit.New(
			cfg.LoginMaxFail,
			time.Duration(cfg.LoginWindowMinutes)*time.Minute,
			time.Duration(cfg.LoginLockMinutes)*time.Minute,
		)
	}

	if cfg.Backend == "ssh" {
		s.pool = sshpool.New(cfg.SSHKeyPath)
		// 默认 TOFU：第一次连接记住主机密钥，之后校验
		if cfg.SSHHostKeyCheck != "insecure" {
			s.pool.UseKnownHosts(st)
		}
	} else {
		s.fake = libvirt.NewFake()
	}
	return s
}

func (s *Server) Routes(webFS http.FileSystem) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)

	r.Get("/healthz", s.handleHealth)

	r.Route("/api", func(r chi.Router) {
		r.Post("/auth/register", s.handleRegister)
		r.Post("/auth/login", s.handleLogin)

		r.Group(func(r chi.Router) {
			r.Use(s.requireAuth)
			r.Get("/me", s.handleMe)
			r.Post("/auth/renew", s.handleRenew)
			r.Get("/config", s.handleGetConfig)
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
			r.Get("/alerts", s.handleAlerts)
			r.Get("/alerts/summary", s.handleAlertSummary)
			r.Post("/alerts/ack", s.handleAckAlerts)
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

				r.Get("/settings", s.handleGetSettings)
				r.Post("/settings", s.handleUpdateSettings)

				r.Get("/users", s.handleAdminUsers)
				r.Post("/users", s.handleAdminCreateUser)
				r.Patch("/users/{id}", s.handleAdminUpdateUser)
				r.Delete("/users/{id}", s.handleAdminDeleteUser)
				r.Post("/users/{id}/password", s.handleAdminResetPassword)
			})
		})
	})

	// 安卓客户端分发（安装包放在 android_dir）
	r.Get("/android/download", s.handleAndroidLatest)
	r.Head("/android/download", s.handleAndroidLatest)
	r.Get("/android/{name}", s.handleAndroidFile)
	r.Head("/android/{name}", s.handleAndroidFile)

	// 一键安装分发
	r.Get("/install.sh", s.handleInstallScript)
	r.Get("/setup-host.sh", s.serveScript("setup-host.sh"))
	r.Get("/setup-target.sh", s.serveScript("setup-target.sh"))
	r.Get("/release/center", s.handleReleaseCenter)
	r.Get("/release/vm-collect", s.handleReleaseCollector)

	// 前端静态资源
	r.Handle("/*", staticHandler(webFS))
	return r
}

// staticHandler 给前端资源加上"每次回源校验"。
// 否则 CDN / 浏览器可能长时间缓存旧脚本，更新后页面还是一堆错。
func staticHandler(fs http.FileSystem) http.Handler {
	files := http.FileServer(fs)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		files.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// ExecEnabled 返回网页命令行是否启用。
func (s *Server) ExecEnabled() bool { return s.execEnabled.Load() }

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
