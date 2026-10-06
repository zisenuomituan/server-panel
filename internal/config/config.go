package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type Config struct {
	Listen     string `json:"listen"`
	BaseURL    string `json:"base_url"`
	DBPath     string `json:"db_path"`
	JWTSecret  string `json:"jwt_secret"`
	TokenHours int    `json:"token_hours"`

	PollSeconds     int    `json:"poll_seconds"`
	Backend         string `json:"backend"` // fake / ssh
	LibvirtURI      string `json:"libvirt_uri"`
	SSHKeyPath      string `json:"ssh_key_path"`
	SSHHostKeyCheck string `json:"ssh_host_key_check"` // tofu（默认）或 insecure
	AllowRegister   bool   `json:"allow_register"`
	InviteCode      string `json:"invite_code"`
	EnableExec      bool   `json:"enable_exec"`

	// 登录防爆破（只针对面板 Web 登录，不涉及 SSH）
	LoginProtect       bool `json:"login_protect"`
	LoginMaxFail       int  `json:"login_max_fail"`       // 窗口内允许的失败次数
	LoginWindowMinutes int  `json:"login_window_minutes"` // 统计窗口（分钟）
	LoginLockMinutes   int  `json:"login_lock_minutes"`   // 触发后锁定时间（分钟）

	AdminUser     string `json:"admin_user"`
	AdminPassword string `json:"admin_password"`

	// 告警：由面板判定并入库，App 端拉取后本地提醒
	AlertEnabled     bool `json:"alert_enabled"`
	AlertCPU         int  `json:"alert_cpu"`         // CPU 阈值（百分比）
	AlertMem         int  `json:"alert_mem"`         // 内存阈值（百分比）
	AlertDisk        int  `json:"alert_disk"`        // 磁盘阈值（百分比）
	AlertConsecutive int  `json:"alert_consecutive"` // 连续命中几次才告警
	AlertOnStop      bool `json:"alert_on_stop"`     // 虚拟机意外停止是否告警

	// 安卓客户端分发的目录（放 apk 和 latest.json）
	AndroidDir string `json:"android_dir"`
}

func defaults() *Config {
	return &Config{
		Listen:             "0.0.0.0:8080",
		BaseURL:            "http://127.0.0.1:8080",
		DBPath:             "data/panel.db",
		TokenHours:         12,
		PollSeconds:        5,
		Backend:            "fake",
		LibvirtURI:         "qemu:///system",
		SSHHostKeyCheck:    "tofu",
		AllowRegister:      true,
		EnableExec:         true,
		LoginProtect:       true,
		LoginMaxFail:       5,
		LoginWindowMinutes: 15,
		LoginLockMinutes:   15,
		AdminUser:          "admin",
		AlertEnabled:       true,
		AlertCPU:           90,
		AlertMem:           90,
		AlertDisk:          90,
		AlertConsecutive:   3,
		AlertOnStop:        true,
		AndroidDir:         "data/android",
	}
}

func Load(path string) (*Config, error) {
	cfg := defaults()

	raw, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(raw, cfg); err != nil {
			return nil, fmt.Errorf("配置文件格式不对: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	if cfg.JWTSecret == "" {
		cfg.JWTSecret = randHex(32)
		if err := cfg.save(path); err != nil {
			return nil, err
		}
	}

	if v := os.Getenv("PANEL_LISTEN"); v != "" {
		cfg.Listen = v
	}
	if v := os.Getenv("PANEL_DB"); v != "" {
		cfg.DBPath = v
	}
	if v := os.Getenv("PANEL_BACKEND"); v != "" {
		cfg.Backend = v
	}
	if v := os.Getenv("PANEL_SSH_KEY"); v != "" {
		cfg.SSHKeyPath = v
	}

	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o755); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, _ := json.MarshalIndent(c, "", "  ")
	return os.WriteFile(path, raw, 0o600)
}

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
