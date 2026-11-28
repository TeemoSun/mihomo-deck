<div align="center">

# Mihomo Deck

**面向 Mihomo（Clash Meta）的自托管持久化 Web 控制台**

[![Go](https://img.shields.io/badge/Go-1.23-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![React](https://img.shields.io/badge/React-19-61DAFB?logo=react&logoColor=black)](https://react.dev/)
[![SQLite](https://img.shields.io/badge/SQLite-Pure%20Go-003B57?logo=sqlite&logoColor=white)](https://pkg.go.dev/modernc.org/sqlite)
[![Docker](https://img.shields.io/badge/Docker-ghcr.io-2496ED?logo=docker&logoColor=white)](https://github.com/TeemoSun/mihomo-deck/pkgs/container/mihomo-deck)
[![License: MIT](https://img.shields.io/badge/License-MIT-3fb950)](./LICENSE)

</div>

---

**Mihomo Deck** 采用 **React 19 + Go + 纯 Go SQLite** 单容器架构，将原本直连控制端口的前端升级为带服务端鉴权与后台持续采样的私有控制台：浏览器侧永不暴露 Mihomo `secret`，且节点切换、历史连接、流量统计、测速曲线与个人偏好全部跨重启持久化保留。

## ✨ 核心特性

- **🔐 独立登录认证与安全代理**
  - 单管理员账号密码登录，凭据由 `.env` 配置，每次启动时校验并同步到 SQLite
  - HttpOnly 双 Token Cookie（`deck_access` 15 分钟 + `deck_refresh` 30 天轮换）与派生 `X-CSRF-Token` 校验
  - 登录失败滑动窗口限流（同一 IP 在 5 分钟内累计 5 次失败后，后续尝试返回 `429`）与严格安全响应头（CSP `script-src 'self'`、nosniff、DENY）
  - 服务端环境变量固定上游 `MIHOMO_URL` / `MIHOMO_SECRET`，前端无多后端弹窗，开箱即用

- **💾 后台持久化采集（解决重启清零痛点）**
  - **累计流量**：自动检测 Mihomo 重启归零并做差值累加，精准统计今日 / 近 7 天 / 近 30 天 / 历史累计流量
  - **细粒度与小时级时序曲线**：5 秒采样支撑 1h / 24h 曲线，小时聚合支撑 7d / 30d 趋势与内存监控
  - **历史连接审计**：连接关闭时自动记录目标、来源 IP、命中规则、完整代理链、持续时间与上下行字节数，支持按条件检索
  - **运行日志持久化**：后台订阅日志流并按设定级别持久化落盘，支持实时与历史双模式检索
  - **节点延迟与订阅快照**：手动及定时低频记录选中节点延迟曲线与订阅流量/到期快照，避免高频全量扫测
  - **服务端个人偏好**：收藏置顶节点、隐藏信息伪节点开关与正则、隐藏策略组、测速参数、主题等全端同步

- **🎛️ Mihomo 控制与高危操作确认**
  - 概览仪表盘（实时速率、内存、24h Top 域名/节点/客户端 IP、快速模式与策略组切换）
  - 策略组与节点管理（Rule / Global / Direct 切换、测速、取消固定、隐藏信息伪节点、收藏置顶、单节点历史延迟走势）
  - 订阅与规则（Proxy Providers 更新与健康检查、Rule Providers 更新、路由规则搜索与命中计数）
  - 内核设置与诊断（运行时参数编辑、DNS 查询、清理 FakeIP/DNS 缓存、Geo 更新、重载配置、重启内核，高危操作均带二次确认弹窗）
  - 桌面侧边栏 + 移动端底部导航，深浅色主题无缝切换

---

## 🐳 快速部署（Docker Compose）

仓库已提供完整生产编排示例 [docker-compose.sample.yml](./docker-compose.sample.yml) 与环境变量模板 [.env.example](./.env.example)。

```bash
# 1. 下载编排示例与环境变量模板
cp docker-compose.sample.yml docker-compose.yml
cp .env.example .env

# 2. 编辑 .env，设置 JWT_SECRET（例如执行 openssl rand -hex 32）。
#    设置 AUTH_USERNAME 和 AUTH_PASSWORD（密码为 8–72 个 UTF-8 字节，避开弱占位值）。
#    将 MIHOMO_SECRET 设置为 Mihomo config.yaml 中的 secret。控制端未配置 secret 时留空。

# 3. 启动服务
docker compose up -d
```

启动后访问 `http://<NAS-IP>:18080`（默认端口 `18080`，可通过 `DECK_PORT` 调整）。

同一 Compose 项目内，Mihomo 从 `./data/config.yaml` 读取配置。用 `external-controller: 0.0.0.0:9090` 监听容器网络，Deck 默认连接 `http://mihomo:9090`。连接 Compose 网络外的控制器时，在 `.env` 中将 `MIHOMO_URL` 改为 Deck 容器能访问的地址，例如 `http://192.168.5.2:9090`。

> **安全说明**
> - Compose 默认只在项目网络内提供 `9090` 控制端口。启用示例中注释的 `9090:9090` 会允许客户端直连 Mihomo，绕过 Deck 登录。
> - 管理员凭据仅由 `.env` 中的 `AUTH_USERNAME` 和 `AUTH_PASSWORD` 配置。修改后运行 `docker compose up -d mihomo-deck`，让 Compose 用新环境变量重建容器。Deck 每次启动时校验凭据并自动同步到 SQLite。用户名或密码变更后，旧会话失效，需要重新登录。
> - 通过 HTTPS 反向代理部署时，设置 `TRUST_PROXY=true`，将 `TRUSTED_PROXIES` 限定为实际反向代理的 IP 网段，并设置 `COOKIE_SECURE=auto` 或 `true`。`auto` 在直接 TLS 连接或可信代理发送 `X-Forwarded-Proto: https` 时启用 `Secure`。
> - Deck 的节点测速、策略组测速和代理提供者健康检查均使用 `POST`，并校验 `X-CSRF-Token`。Deck 的认证失败以 `401` 和 `X-Deck-Auth-Error: 1` 标识。支持的 Mihomo API 子集见[路由表](./docs/DESIGN.md#mihomo-api-子集)。

---

## ⚙️ 环境变量说明

| 变量名 | 说明 | 默认值 |
| --- | --- | --- |
| `DECK_PORT` | 示例 Compose 发布的 Deck 宿主机端口 | `18080` |
| `PORT` | Web 服务监听端口，示例 Compose 固定为 `8080` | `8080` |
| `DB_PATH` | SQLite 数据库文件路径，示例 Compose 固定为 `/data/mihomo-deck.db` | `/data/mihomo-deck.db` |
| `STATIC_DIR` | 前端静态资源目录，示例 Compose 固定为 `/app/static` | `/app/static` |
| `MIHOMO_URL` | Mihomo external-controller 的 HTTP(S) 地址，支持路径前缀，禁止内嵌凭据、查询串和片段 | Compose 为 `http://mihomo:9090`，独立运行 Go 服务为 `http://127.0.0.1:9090` |
| `MIHOMO_SECRET` | Mihomo external-controller 配置的 `secret`，由 Deck 服务端使用 | 空 |
| `AUTH_USERNAME` | 管理员用户名，每次启动时校验，须包含非空白字符 | `admin` |
| `AUTH_PASSWORD` | 管理员密码，每次启动必填，须为 8–72 个 UTF-8 字节且避开弱占位值 | 无可用默认值 |
| `JWT_SECRET` | JWT 与 CSRF 签名密钥，必填，至少 16 字节且避开弱占位值 | 无可用默认值 |
| `TRUST_PROXY` | 是否接受可信代理的 `X-Forwarded-For`、`X-Real-IP` 和 `X-Forwarded-Proto` | `false` |
| `TRUSTED_PROXIES` | `TRUST_PROXY=true` 时使用的逗号分隔 CIDR 列表 | 默认列表为 `127.0.0.1/32,::1/128,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16`。Compose 在变量未设置或为空时使用此列表。独立运行 Go 服务且未设置本变量时，`TRUST_PROXY=true` 使用此列表，`false` 时为空 |
| `COOKIE_SECURE` | Cookie `Secure` 属性策略（`false` / `true` / `auto`） | `false` |
| `RETENTION_DAYS` | 默认遥测数据保留天数（`1`–`3650`） | `30` |
| `SNAPSHOT_INTERVAL_MIN` | 默认后台定时快照间隔（`0`–`10080` 分钟，`0` 表示关闭） | `30` |
| `LOG_PERSIST_LEVEL` | 默认日志持久化级别（`silent`/`error`/`warning`/`info`/`debug`） | `info` |
| `TZ` | 时区（用于每日流量零点统计与日志时间） | `Asia/Shanghai` |

---

## 🛠️ 本地开发与构建

### 后端（Go 1.23+，纯 Go 无 CGO）

```bash
cd backend
go vet ./...
go test -v -count=1 ./...
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o mihomo-deck ./cmd/server
```

### 前端（Node 22+ / React 19 / Vite）

```bash
cd frontend
npm ci
npm run dev      # 开发模式 (:5173)，自动代理 /api 到 127.0.0.1:8080
npm run build    # TypeScript 类型检查 + 生产构建
```

### 本地构建 Docker 镜像

```bash
docker build -t mihomo-deck:local .
```

通过 BuildKit 的 `--platform` 指定目标时，Dockerfile 使用 `TARGETOS` 和 `TARGETARCH` 设置 Go 的 `GOOS` 和 `GOARCH`。在 AMD64 主机上构建 ARM64 镜像时，运行阶段需要可执行 ARM64 指令的构建节点或模拟器。

```bash
docker buildx build --platform linux/arm64 --load -t mihomo-deck:local-arm64 .
```

镜像发布工作流在 Go 检查和测试以及前端构建通过后发布 `linux/amd64` 镜像。

更多架构与数据库设计细节请参阅 [docs/DESIGN.md](./docs/DESIGN.md)。

## 📄 License

[MIT](./LICENSE)
