package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"serverpanel/scripts"
)

// requestBase 推断客户端访问本服务用的基地址，用于把安装脚本里的下载地址填对。
func requestBase(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if p := r.Header.Get("X-Forwarded-Proto"); p != "" {
		scheme = p
	}
	return scheme + "://" + r.Host
}

// handleInstallScript 下发交互式安装脚本，并把里面的 __PANEL_BASE__ 换成真实地址。
func (s *Server) handleInstallScript(w http.ResponseWriter, r *http.Request) {
	raw, err := scripts.FS.ReadFile("install.sh")
	if err != nil {
		http.Error(w, "安装脚本缺失", http.StatusNotFound)
		return
	}
	body := strings.ReplaceAll(string(raw), "__PANEL_BASE__", requestBase(r))
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(body))
}

func (s *Server) serveScript(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw, err := scripts.FS.ReadFile(name)
		if err != nil {
			http.Error(w, "脚本缺失", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write(raw)
	}
}

// handleReleaseCenter 把正在运行的 center 自身回传，供新机器下载安装。
func (s *Server) handleReleaseCenter(w http.ResponseWriter, r *http.Request) {
	exe, err := os.Executable()
	if err != nil {
		http.Error(w, "无法定位自身程序", http.StatusInternalServerError)
		return
	}
	http.ServeFile(w, r, exe)
}

// handleReleaseCollector 回传同目录下的 vm-collect。install-center 会把它放在 center 旁边。
func (s *Server) handleReleaseCollector(w http.ResponseWriter, r *http.Request) {
	exe, err := os.Executable()
	if err != nil {
		http.Error(w, "无法定位自身程序", http.StatusInternalServerError)
		return
	}
	path := filepath.Join(filepath.Dir(exe), "vm-collect")
	if _, err := os.Stat(path); err != nil {
		http.Error(w, "服务端没有提供 vm-collect，请把它放到 "+filepath.Dir(exe), http.StatusNotFound)
		return
	}
	http.ServeFile(w, r, path)
}
