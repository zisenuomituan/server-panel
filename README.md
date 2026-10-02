# server-panel · KVM 服务器面板

一个自托管的服务器管理面板。中心节点用 SSH 连到 KVM 宿主机执行 `virsh`，
在各虚拟机里采集运行指标，浏览器里统一查看和管理。

## 功能

- **账号体系**：注册 / 登录，JWT + argon2id，角色分 admin / operator / viewer。
- **绑定密钥**：每个宿主可生成 32 位密钥（Crockford Base32）。分两级：
  - 宿主级：绑定后可管理该宿主下所有虚拟机；
  - 单机级：只绑定某一台虚拟机。
  密钥只存哈希，可多账号共用、可撤销 / 轮换 / 设有效期。
- **实时监控**：CPU、内存、磁盘各挂载点、网络上行/下行速率与累计、开机时长、
  在线状态，WebSocket 推送，历史曲线用 ECharts 画。
- **系统信息**：主机名、发行版、内核、架构、内网 IP、当前登录用户。
- **电源控制**：开机 / 关机 / 重启 / 强制关机，走 `virsh`，带二次确认和审计。
- **网页命令行**：在详情页直接对虚拟机执行命令，返回真实输出和退出码。
- **操作日志**：登录、注册、绑定、开关机、执行命令、密钥轮换等全部入库可查。
- **一键安装**：面板自身通过 HTTP 分发安装脚本和二进制，一条 curl 即可部署新节点。

## 架构

```
浏览器 ──HTTPS/WS──► center（Go 单二进制 + SQLite）
                        │
                        ├─ SSH ─► KVM 宿主机（virsh：列表/规格/开关机）
                        └─ SSH ─► 各虚拟机（vm-collect：CPU/内存/磁盘/网络/用户）
```

- `center`：对外提供页面和 API，后台按周期通过 SSH 采集。
- `vm-collect`：一个静态小工具，被 center 通过 SSH 调用，输出一行 JSON 指标。
- 目标机只需要开 SSH；虚拟机内不需要常驻 agent。

## 安装

两种一键方式，脚本都会交互式询问管理员账号、监听端口，以及要登记的
KVM 宿主机信息，然后自动下载二进制、写配置、装 systemd 服务，
并把宿主机登记进面板，最后打印宿主机的绑定密钥。

### 方式一：直接从 Release 安装（不需要已有面板）

```bash
# GitHub
curl -fsSL https://github.com/zisenuomituan/server-panel/releases/download/v0.1.1/install.sh | sudo bash
# Gitee
curl -fsSL https://gitee.com/xianyunb123/server-panel/releases/download/v0.1.1/install.sh | sudo bash
```

脚本会按当前架构自动下载对应的 `center` 和 `vm-collect`。
把上面的版本号换成新版本即可升级安装。

### 方式二：从已有面板分发

先有一台已经跑起来的面板，然后：

```bash
curl -fsSL http://<面板地址>/install.sh | sudo bash
```

这种方式下二进制由现有面板的 `/release/` 提供。

非交互方式（任意一种来源）：

```bash
PANEL_BASE=http://<面板地址> \
PANEL_PORT=8080 \
PANEL_ADMIN_USER=admin PANEL_ADMIN_PASSWORD='改成你的密码' \
PANEL_HOST_NAME=机房A PANEL_HOST_ADDR=1.2.3.4 PANEL_HOST_PORT=22 \
PANEL_HOST_USER=panel PANEL_LIBVIRT_URI=qemu:///system \
bash install.sh
```

### 方式三：手动安装

```bash
make release                      # 生成 dist/linux-amd64/{center,vm-collect}
scp dist/linux-amd64/* root@<服务器>:/root/
# 在服务器上：
./install-center.sh ./center      # 安装 center + vm-collect 并启动
```

然后浏览器打开 `http://<服务器>:8080` 注册第一个账号（即管理员）。

## 命令行管理

`center` 同时是命令行工具，可以直接在终端里管理，不必打开网页：

```bash
center version
center user list
center user add admin -role admin -password '改成你的密码'
center user passwd admin
center host add -name 机房A -host 1.2.3.4 -port 22 -user panel
center host list
center key issue -host 1             # 宿主级密钥，可管整台宿主
center key issue -server 3 -days 30  # 单机级密钥，只管这一台
center key list
center key revoke 2
center serve                         # 启动 Web 面板（不加子命令也是这个）
```

命令行和面板共用同一个数据库，终端里改完，刷新网页即可看到。

## 被管理的机器

### KVM 宿主机

在宿主机上执行一次 `setup-host.sh`，会创建一个受限的 `panel` 用户，
加入 `libvirt` 组并写入相应的 sudo 白名单：

```bash
curl -fsSL http://<面板地址>/setup-host.sh | sudo bash
# 或本地：sudo ./setup-host.sh /path/to/center_id_ed25519.pub
```

### 虚拟机 / 被监控机器

```bash
curl -fsSL http://<面板地址>/setup-target.sh | sudo bash
```

会安装 `vm-collect` 到 `/usr/local/bin`，之后面板就能通过 SSH 取到机器指标。

## 从源码构建

需要 Go 1.23+。项目只依赖纯 Go 库（SQLite 用 modernc.org/sqlite），
`CGO_ENABLED=0` 即可交叉编译。

```bash
make build      # 本机架构，产物在 bin/
make release    # linux/amd64 和 linux/arm64，产物在 dist/
make test
```

前端（Vue 3 + ECharts）以静态文件形式通过 `go:embed` 打进二进制，
所以最终只需要一个可执行文件。

## 配置

参考 `config.example.json`。几种常用项：

| 字段 | 说明 |
|---|---|
| `listen` | 监听地址，如 `0.0.0.0:8080` |
| `db_path` | SQLite 文件路径 |
| `backend` | `fake`（内置模拟数据，开发用）或 `ssh`（真实） |
| `ssh_key_path` | 连接目标机用的私钥 |
| `libvirt_uri` | 默认 libvirt 地址，登记宿主时可单独覆盖 |
| `allow_register` / `invite_code` | 是否开放注册、是否要邀请码 |
| `enable_exec` | 是否允许网页命令行 |
| `admin_user` / `admin_password` | 首次启动时自动创建的管理员 |

## 安全

- 面板只通过 SSH 操作宿主机，不在目标机装常驻服务；宿主机建议用受限的
  `panel` 用户（见 `setup-host.sh`）。
- 密码用 argon2id；绑定密钥只存 SHA-256 哈希。
- 生产环境请套 HTTPS 反向代理，并限制面板端口来源 IP。
- 命令执行、开关机等敏感操作都有审计日志。

## 目录结构

```
cmd/center          面板服务端
cmd/vm-collect      被监控机器上的采集工具
internal/auth       JWT、密码哈希
internal/bindkey    绑定密钥生成与校验
internal/libvirt    libvirt 后端（fake / ssh）
internal/collector  本机指标采集
internal/server     HTTP API、WebSocket、采集调度
internal/store      SQLite 存储
scripts             安装脚本（会随二进制一起分发）
web                 前端页面
```

## 版本迭代

改动都记在 [CHANGELOG.md](CHANGELOG.md)。发一个新版本：

1. 修改 `VERSION` 文件；
2. 在 `CHANGELOG.md` 写下这个版本的变化；
3. `make release` 生成 `dist/` 下的 amd64 / arm64 二进制；
4. 打 tag 并推送，然后把二进制和 `install.sh` 作为附件发到 GitHub / Gitee 的 Release。

## 许可证

Apache License 2.0，见 `LICENSE`，版权与第三方组件见 `NOTICE` / `THIRD_PARTY.md`。
