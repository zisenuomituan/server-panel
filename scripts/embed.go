// Package scripts 把安装脚本编译进 center，方便对外用一条 curl 分发。
package scripts

import "embed"

//go:embed install.sh install-center.sh setup-host.sh setup-target.sh
var FS embed.FS
