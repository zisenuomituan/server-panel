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

	PollSeconds   int    `json:"poll_seconds"`
	Backend       string `json:"backend"` // fake / ssh
	LibvirtURI    string `json:"libvirt_uri"`
	SSHKeyPath    string `json:"ssh_key_path"`
	AllowRegister bool   `json:"allow_register"`
	InviteCode    string `json:"invite_code"`
	EnableExec    bool   `json:"enable_exec"`

	AdminUser     string `json:"admin_user"`
	AdminPassword string `json:"admin_password"`
}

func defaults() *Config {
	return &Config{
		Listen:        "0.0.0.0:8080",
		BaseURL:       "http://127.0.0.1:8080",
		DBPath:        "data/panel.db",
		TokenHours:    12,
		PollSeconds:   5,
		Backend:       "fake",
		LibvirtURI:    "qemu:///system",
		AllowRegister: true,
		EnableExec:    true,
		AdminUser:     "admin",
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
