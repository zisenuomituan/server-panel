# 更新日志

版本号语义化，发布产物见 GitHub / Gitee 的 Releases。
`VERSION` 文件是唯一版本来源，构建时通过 `-ldflags` 注入 `center version`。

## [未发布]

### 计划中

- noVNC 网页控制台
- HTTPS 一键配置（内置 Caddy 自动证书）
- 告警与通知：机器离线、CPU/内存/磁盘超阈值
- qemu-guest-agent 可选启用，补全文件系统与登录用户信息
- 指标长期存储与聚合，可选 Prometheus 导出

## [0.1.1] - 2026-10-02

### 新增

- `center` 二合一：既是 Web 面板，也是命令行工具。
- 命令行管理：`user add|list|passwd|rm`、`host add|list|rm`、
  `key issue|list|revoke`，支持宿主级与单机级密钥。
- 安装脚本支持直接从 Release 安装（`--release-base`），按 CPU 架构自动选择二进制，
  不再依赖已有面板。
- 版本号由 `VERSION` 文件注入，`center version` 可查。

## [0.1.0] - 2026-10-02

### 新增

- 首个开源版本。
- 账号体系：注册 / 登录，JWT + argon2id，角色 admin / operator / viewer。
- 绑定密钥：32 位、只存哈希、可多账号共用、可撤销 / 轮换 / 设有效期；
  分宿主级（管整台宿主）与单机级（只管一台虚拟机）。
- 实时监控：CPU、内存、磁盘、网络上下行、开机时长、在线状态，WebSocket 推送与历史曲线。
- 电源控制：开机 / 关机 / 重启 / 强制关机，走 libvirt，带二次确认和审计。
- 网页命令行：在详情页对虚拟机执行命令。
- 操作日志：登录、注册、绑定、开关机、执行命令、密钥轮换等。
- 单二进制部署（Go + SQLite + Vue3），支持 linux/amd64 与 linux/arm64。
- 一键安装脚本、systemd 服务、面板自举分发。
