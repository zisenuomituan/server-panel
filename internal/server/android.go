package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
)

// 安卓客户端分发。
//
// 把安装包和 latest.json 放进 android_dir，面板直接对外提供，并显式禁用缓存：
// 一来任何实例（正式/演示）都能用，二来不会被 CDN 缓存住旧安装包。
//
//	/android/download       当前最新安装包
//	/android/app-latest.apk 同上
//	/android/latest.json    更新清单
//	/android/<文件名>        目录里的任意文件
func (s *Server) handleAndroidLatest(w http.ResponseWriter, r *http.Request) {
	s.serveAndroid(w, r, s.latestApkName())
}

func (s *Server) handleAndroidFile(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "app-latest.apk" {
		name = s.latestApkName()
	}
	s.serveAndroid(w, r, name)
}

func (s *Server) serveAndroid(w http.ResponseWriter, r *http.Request, name string) {
	if name == "" || strings.HasPrefix(name, ".") || strings.ContainsAny(name, `/\`) {
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(s.cfg.AndroidDir, name)
	if _, err := os.Stat(path); err != nil {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	if strings.HasSuffix(name, ".apk") {
		w.Header().Set("Content-Type", "application/vnd.android.package-archive")
		// 带上文件名，浏览器存下来才是 xxx.apk；否则存成无后缀文件会装不上
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	}
	http.ServeFile(w, r, path)
}

// latestApkName 取目录里最新的安装包（按修改时间），没有就回退到 app-latest.apk。
func (s *Server) latestApkName() string {
	entries, err := os.ReadDir(s.cfg.AndroidDir)
	if err != nil {
		return "app-latest.apk"
	}
	best := ""
	var bestMod int64
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || name == "app-latest.apk" || !strings.HasSuffix(name, ".apk") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Unix() >= bestMod {
			best, bestMod = name, info.ModTime().Unix()
		}
	}
	if best != "" {
		return best
	}
	return "app-latest.apk"
}
