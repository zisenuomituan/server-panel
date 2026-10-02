// Package web 把前端静态文件打进二进制，部署时只需要一个文件。
package web

import "embed"

//go:embed index.html app.js style.css vendor
var FS embed.FS
