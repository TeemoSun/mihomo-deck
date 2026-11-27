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
  - 单管理员账号密码登录（环境变量初始化引导，网页端改密后写入 SQLite 跨重启持久化）
  - HttpOnly 双 Token Cookie（`deck_access` 15 分钟 + `deck_refresh` 30 天轮换）与派生 `X-CSRF-Token` 校验
  - 登录失败滑动窗口限流（5 分钟连续失败 5 次触发 `429`）与严格安全响应头（CSP `script-src 'self'`、nosniff、DENY）
  - 服务端环境变量固定上游 `MIHOMO_URL` / `MIHOMO_SECRET`，前端无多后端弹窗，开箱即用

- **💾 后台持久化采集（解决重启清零痛点）**
  - **跨重启累计流量**：自动检测 Mihomo 重启归零并做差值累加，精准统计今日 / 近 7 天 / 近 30 天 / 历史累计流量
  - **细粒度与小时级时序曲线**：5 秒采样支撑 1h / 24h 曲线，小时聚合支撑 7d / 30d 趋势与内存监控
  - **历史连接审计**：连接关闭时自动记录目标、来源 IP、命中规则、完整代理链、持续时间与上下行字节数，支持按条件检索
  - **运行日志持久化**：后台订阅日志流并按设定级别持久化落盘，支持实时与历史双模式检索
  - **节点延迟与订阅快照**：手动及定时低频记录选中节点延迟曲线与订阅流量/到期快照，避免高频全量扫测
  - **服务端个人偏好**：收藏置顶节点、隐藏信息伪节点开关与正则、隐藏策略组、测速参数、主题等全端同步

- **🎛️ 全量 Mihomo 控制能力与高危保护**
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

# 2. 编辑 .env，至少填写：
#    - MIHOMO_URL（如 http://192.168.5.2:9090 或同 Compose 下 http://mihomo:9090）
#    - MIHOMO_SECRET（与 mihomo config.yaml 中 secret 一致）
#    - AUTH_PASSWORD（强密码，禁止 admin/password/123456 等弱占位值）
#    - JWT_SECRET（至少 16 位随机密钥，例如执行 openssl rand -hex 32）

# 3. 启动服务
docker compose up -d
```

启动后访问 `http://<NAS-IP>:18080`（默认端口 `18080`，可通过 `DECK_PORT` 调整）。

> **安全说明**
> - 程序内置强密钥启动校验：`AUTH_PASSWORD`（仅首次建库空表时引导）与 `JWT_SECRET` 若为空或弱占位值将直接拒绝启动。
> - `users` 表非空后，环境变量 `AUTH_PASSWORD` 不会覆盖已在控制台修改的新密码。
> - 若通过 HTTPS 反向代理暴露到外网，请设置 `TRUST_PROXY=true` 与 `COOKIE_SECURE=auto`（或 `true`）。

---

## ⚙️ 环境变量说明

| 变量名 | 说明 | 默认值 |
| --- | --- | --- |
| `PORT` | Web 服务监听端口 | `8080` |
| `DB_PATH` | SQLite 数据库文件路径 | `/data/mihomo-deck.db` |
| `STATIC_DIR` | 前端静态资源目录 | `/app/static` |
| `MIHOMO_URL` | Mihomo external-controller 地址 | `http://127.0.0.1:9090` |
| `MIHOMO_SECRET` | Mihomo external-controller `secret`（可为空） | `""` |
| `AUTH_USERNAME` | 初始管理员用户名 | `admin` |
| `AUTH_PASSWORD` | 初始管理员密码（仅首次建库使用，不可为弱密码） | 必填 |
| `JWT_SECRET` | JWT 与 CSRF 签名密钥（至少 16 位） | 必填 |
| `TRUST_PROXY` | 是否信任 `X-Forwarded-For` / `X-Forwarded-Proto` | `false` |
| `COOKIE_SECURE` | Cookie `Secure` 属性策略（`false` / `true` / `auto`） | `false` |
| `RETENTION_DAYS` | 默认遥测数据保留天数 | `30` |
| `SNAPSHOT_INTERVAL_MIN` | 默认后台定时快照间隔（分钟，`0` 表示关闭） | `30` |
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

更多架构与数据库设计细节请参阅 [docs/DESIGN.md](./docs/DESIGN.md)。

## 📄 License

[MIT](./LICENSE)
