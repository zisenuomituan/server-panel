# 第三方组件

前端运行时以静态文件形式放在 `web/vendor/`，随二进制一起分发：

| 组件 | 版本 | 许可证 |
|---|---|---|
| Vue | 3.4.38 | MIT |
| Apache ECharts | 5.5.1 | Apache-2.0 |

Go 依赖及其许可证见 `go.mod`，各模块仓库中自带 LICENSE。

主要依赖：

- `github.com/go-chi/chi/v5` — MIT
- `github.com/golang-jwt/jwt/v5` — MIT
- `github.com/gorilla/websocket` — BSD-3-Clause
- `github.com/shirou/gopsutil/v4` — BSD-3-Clause
- `golang.org/x/crypto` — BSD-3-Clause
- `modernc.org/sqlite` — BSD-3-Clause
