# Mihomo Deck 技术设计方案 (`docs/DESIGN.md`)

## 1. 项目定位与目标

**Mihomo Deck** 是面向 Mihomo（原 Clash Meta）的现代化自托管 Web 控制台，采用 **React 19 + Go + SQLite** 单容器架构。
相比 `metacubexd` 等纯静态前端直连 `9090` 端口的方案，Mihomo Deck 引入 Go 后端作为 BFF（Backend for Frontend）与持久化采集引擎，解决以下痛点：
1. **认证保护**：提供完整的账号密码登录、HttpOnly Cookie 双 Token 鉴权、CSRF 防护与防爆破限流，浏览器侧永不暴露 Mihomo `secret`。
2. **单一 Mihomo 上游**由服务端环境变量配置。示例 Compose 默认连接同一网络内的 `http://mihomo:9090`。
3. **跨重启与跨设备持久化**：Mihomo 重启或浏览器刷新不会丢失流量曲线、累计流量、已关闭连接记录、运行日志、节点测速历史、订阅余量快照与个人界面偏好。
4. **Mihomo 控制与高危操作确认**涵盖节点切换、测速、策略/规则提供者更新、规则命中统计、连接审计与断开、内核参数修改、DNS 诊断、缓存清理、Geo 更新与内核重启。
5. **桌面与移动端同等流畅**：极简高信息密度专业控制台风格（Linear / Cloudflare 风格，浅色/深色主题），纯简体中文，桌面侧边栏 + 移动端底部导航/抽屉适配。

---

## 2. 总体架构

```text
+---------------------------+        HTTPS / LAN HTTP         +------------------------------------+
|  Browser (Desktop/Mobile) | <-----------------------------> |  Go Server (mihomo-deck :8080)     |
|  React 19 + TS + Tailwind |   HttpOnly Cookie + CSRF + WS   |  - Static SPA (/app/static)        |
+---------------------------+                                 |  - Auth & Preferences API          |
                                                              |  - Telemetry Query API             |
                                                              |  - Mihomo BFF Proxy & WS Hub       |
                                                              |  - Background Collector & Pruner   |
                                                              |  - Pure-Go SQLite (WAL /data/*.db) |
                                                              +-----------------+------------------+
                                                                                |
                                                                 Bearer Secret  |  REST + WS
                                                                                v
                                                              +------------------------------------+
                                                              |  Mihomo Core (:9090)               |
                                                              +------------------------------------+
```

### 核心模块划分
- `backend/cmd/server/main.go`：配置加载、密钥校验、SQLite 初始化、Mihomo 客户端与采集器启动、HTTP/WS 服务与优雅停机、`-healthcheck` 探针。
- `backend/internal/config` 负责读取环境变量与校验启动配置。每次启动都校验 `AUTH_USERNAME`、`AUTH_PASSWORD` 和 `JWT_SECRET`。`MIHOMO_SECRET` 可以为空。
- `backend/internal/database`：基于 `modernc.org/sqlite` 的纯 Go SQLite 层（`CGO_ENABLED=0`），开启 WAL、外键约束、`busy_timeout=5000`、`MaxOpenConns(1)`。
- `backend/internal/mihomo` 封装控制与采集所需的 Mihomo REST 请求和 WebSocket 流。
- `backend/internal/collector`：常驻后台采集引擎，负责断线指数退避重连、连接关闭 diff 落盘、跨重启流量差值累加、小时级聚合、低频订阅/节点快照与 30 天（可配）过期数据自动清理。
- `backend/internal/api`：Chi v5 路由、鉴权中间件、滑动窗口登录限流、CSRF 校验、安全响应头（CSP/nosniff/X-Frame-Options）、WebSocket 实时分发 Hub。
- `frontend/`：React 19 + TypeScript + Vite + Tailwind CSS v4 + Zustand + Recharts + Lucide Icons。

---

## 3. SQLite 数据模型（`backend/internal/database/migrations.go`）

1. **`users`**：单管理员账号表
   - `id` INTEGER PK, `username` TEXT UNIQUE, `password_hash` TEXT, `token_version` INTEGER, `created_at` INTEGER, `updated_at` INTEGER
   - 每次启动都将 `.env` 提供的 `AUTH_USERNAME` 和 `AUTH_PASSWORD` 同步到本表。用户名或密码变更时，在同一事务中更新用户名和 bcrypt 哈希、递增 `token_version` 并撤销刷新令牌，使旧会话失效。凭据相同时保留账号记录和会话。
2. **`refresh_tokens`**：长期免登会话（默认 30 天）
   - `id` TEXT PK, `user_id` INTEGER FK, `token_hash` TEXT UNIQUE, `user_agent` TEXT, `ip` TEXT, `expires_at` INTEGER, `revoked_at` INTEGER, `created_at` INTEGER
3. **`user_preferences`**：前端偏好与可调系统策略（KV JSON）
   - `key` TEXT PK, `value_json` TEXT, `updated_at` INTEGER
   - 存储：收藏/置顶节点、隐藏信息伪节点开关与正则、自定义测速 URL 与超时、默认节点排序、主题（light/dark/system）、数据保留天数（默认 `30`）、快照间隔（默认 `30` 分钟）等。
4. **`traffic_cursor`**：跨重启累计流量游标（单行 `id = 1`）
   - `id` INTEGER PK, `last_up_total` INTEGER, `last_down_total` INTEGER, `cumulative_up` INTEGER, `cumulative_down` INTEGER, `updated_at` INTEGER
   - 算法：若 Mihomo 返回的 `curr >= last`，增量 `delta = curr - last`；若 `curr < last`（说明 Mihomo 重启计数清零），则 `delta = curr`，从而保证累计总流量永不回退或丢失。
5. **`traffic_samples`**：细粒度实时采样（每 5 秒一条，用于 1h / 24h 精确曲线）
   - `ts` INTEGER PK, `up_bps` INTEGER, `down_bps` INTEGER, `mem_inuse` INTEGER, `mem_oslimit` INTEGER, `active_conns` INTEGER
6. **`traffic_hourly`**：小时级流量聚合（用于 7d / 30d / 历史总览）
   - `hour_ts` INTEGER PK, `up_bytes` INTEGER, `down_bytes` INTEGER, `peak_up_bps` INTEGER, `peak_down_bps` INTEGER, `avg_mem` INTEGER, `max_conns` INTEGER
7. **`connection_history`**：已关闭连接持久化记录
   - `id` TEXT PK, `host` TEXT, `dst_ip` TEXT, `dst_port` TEXT, `src_ip` TEXT, `src_port` TEXT, `network` TEXT, `conn_type` TEXT, `inbound_name` TEXT, `rule` TEXT, `rule_payload` TEXT, `chain_json` TEXT, `primary_proxy` TEXT, `provider_name` TEXT, `upload_bytes` INTEGER, `download_bytes` INTEGER, `start_at` INTEGER, `closed_at` INTEGER, `duration_ms` INTEGER
   - 索引：`idx_conn_closed_at`, `idx_conn_host`, `idx_conn_proxy`, `idx_conn_src_ip`
8. **`log_entries`**：运行日志历史
   - `id` INTEGER PK AUTOINCREMENT, `ts` INTEGER, `level` TEXT, `payload` TEXT
   - 索引：`idx_logs_ts`, `idx_logs_level`
9. **`delay_history`**：节点测速延迟历史（含手动测速与后台低频快照）
   - `id` INTEGER PK AUTOINCREMENT, `proxy_name` TEXT, `group_name` TEXT, `test_url` TEXT, `delay_ms` INTEGER, `ok` INTEGER, `source` TEXT, `tested_at` INTEGER
   - 索引：`idx_delay_proxy_time`
10. **`provider_snapshots`**：订阅套餐余量历史快照
    - `id` INTEGER PK AUTOINCREMENT, `provider_name` TEXT, `upload_bytes` INTEGER, `download_bytes` INTEGER, `total_bytes` INTEGER, `expire_ts` INTEGER, `node_count` INTEGER, `alive_count` INTEGER, `captured_at` INTEGER

---

## 4. 鉴权与安全设计

- **登录态**。`POST /api/auth/login` 校验 bcrypt 密码后下发以下 Cookie，并在响应体返回 `csrfToken`。
  - `deck_access` Cookie（HttpOnly, SameSite=Lax, Path=/, 15 分钟）
  - `deck_refresh` Cookie（HttpOnly, SameSite=Lax, Path=/api/auth, 30 天）
- **密码校验**。启动时要求 `AUTH_PASSWORD` 为 8–72 个 UTF-8 字节，且避开弱占位值。登录时也检查密码的字节长度。
- **CSRF**。`csrfToken` 由 `JWT_SECRET` 对用户 ID 与 `token_version` 做 HMAC-SHA256 派生。鉴权后的 `POST`、`PUT`、`PATCH`、`DELETE` 请求须发送 `X-CSRF-Token`，校验失败返回 `403`。这包括节点测速、策略组测速和代理提供者健康检查。`POST /api/auth/login`、`POST /api/auth/refresh` 和 `POST /api/auth/logout` 在提供 `Origin` 时校验其主机与端口是否匹配请求的 `Host`。
- **认证失败标记**。Deck 的认证失败返回 `401`，附带 `X-Deck-Auth-Error: 1`。转发的 Mihomo `401` 保留状态码，前端根据响应头识别 Deck 的认证失败。
- **HTTP / HTTPS 兼容**。
  - `COOKIE_SECURE=false` 支持局域网 HTTP。`true` 始终启用 `Secure`，`auto` 在直接 TLS 连接或可信代理发送 `X-Forwarded-Proto: https` 时启用 `Secure`。
  - `TRUST_PROXY=true` 时，Deck 仅对来源 IP 命中 `TRUSTED_PROXIES` 的直接连接采用代理头。客户端 IP 优先取 `X-Forwarded-For` 中从右往左第一个不可信的地址。若链中地址均可信，取最左侧地址。`X-Forwarded-For` 为空时尝试 `X-Real-IP`。
- **防爆破与安全头**。
  - 同一 IP 在 5 分钟内累计 5 次登录失败后，后续尝试返回 `429`。同一 IP 等待校验的请求也占用窗口配额，成功登录会清除该 IP 的失败记录。
  - 全局注入 `Content-Security-Policy`（`script-src 'self'`，主题初始化脚本外置在 `public/theme-init.js`）、`X-Content-Type-Options: nosniff`、`X-Frame-Options: DENY`、`Referrer-Policy: no-referrer`。

### Mihomo API 子集

Deck 在 `/api/mihomo` 下公开以下 REST 路由，均要求 Deck 登录。表中的路径相对于 `/api/mihomo`。

| 方法 | 路径 |
| --- | --- |
| `GET` | `/version` |
| `GET`, `PATCH` | `/configs` |
| `PUT` | `/configs/reload` |
| `POST` | `/configs/geo` |
| `POST` | `/restart` |
| `GET` | `/proxies` |
| `PUT`, `DELETE` | `/proxies/{name}` |
| `POST` | `/proxies/{name}/delay` |
| `POST` | `/group/{name}/delay` |
| `GET` | `/providers/proxies` |
| `PUT` | `/providers/proxies/{name}` |
| `POST` | `/providers/proxies/{name}/healthcheck` |
| `GET` | `/providers/rules` |
| `PUT` | `/providers/rules/{name}` |
| `GET` | `/rules` |
| `GET`, `DELETE` | `/connections` |
| `DELETE` | `/connections/{id}` |
| `GET` | `/dns/query` |
| `POST` | `/cache/fakeip/flush` |
| `POST` | `/cache/dns/flush` |

Deck 收到测速和代理提供者健康检查的 `POST` 后，向 Mihomo 发起相应的 `GET`。浏览器从 Deck 的鉴权 WebSocket `GET /api/ws/live` 获取实时数据。容器探针 `-healthcheck` 在本机请求无需登录的 `GET /api/health`，以 HTTP `200` 判定探测成功。该接口在数据库不可用时返回 `503`，成功响应的 `upstream` 字段报告采集器连接状态。

---

## 5. 后台采集与低频快照设计

- **WebSocket 采集循环**：分别维持 Mihomo `/traffic`、`/memory`、`/connections`、`/logs?level=<level>` 4 条持久化订阅，断线后 1s~30s 指数退避重连。
- **连接 diff 闭环**：每次收到 `/connections` 快照后，比对上一帧活跃连接 map；对于消失的连接 ID，将最终字节数与持续时长批量写入 `connection_history`。
- **低频定时快照**：默认每 30 分钟采集一次（可在设置页调整或关闭）：
  1. 拉取 `/providers/proxies` 并记录各订阅的 `subscriptionInfo`（上传/下载/总量/到期时间）与可用节点数到 `provider_snapshots`。
  2. 对当前各策略组正在使用的节点（`now`）执行轻量延迟探测，记录长期可用性与延迟趋势到 `delay_history`（避免对全部几十上百个节点做高频全量扫测触发机场风控）。
- **过期清理**：每小时执行一次，按用户配置的 `retention_days`（默认 30 天）批量清理 `traffic_samples`、`traffic_hourly`（保留更久或按同策略）、`connection_history`、`log_entries`、`delay_history`、`provider_snapshots` 与过期 `refresh_tokens`。

---

## 6. 前端页面结构与体验规范

- **导航架构**：
  - 桌面端：左侧固定精简导航栏（220px）+ 顶部全局状态条（核心连通性指示灯、当前模式、实时速率、主题切换、当前用户）。
  - 移动端：顶部紧凑状态条 + 底部 5 项固定导航（概览 / 节点 / 连接 / 日志 / 更多，其中“更多”抽屉或标签页切换进入订阅规则与设置），大尺寸触控热区支持单手操作。
- **核心页面**：
  1. **概览（`/`）**：实时速率与内存曲线、今日/7天/30天/累计总流量卡片、订阅套餐余量与到期进度条、快速模式与主力策略组节点切换、Top 域名/节点/来源 IP 榜单。
  2. **节点与策略组（`/proxies`）**：Rule/Global/Direct 切换、策略组折叠与延迟测试、节点置顶收藏（持久化）、自动过滤“剩余流量/官网/到期”等信息伪节点（持久化开关与规则）、延迟颜色分级与单节点历史延迟走势弹窗、URLTest 固定/自动释放。
  3. **连接审计（`/connections`）**：双 Tab（实时活跃连接 / 历史已关闭连接），支持筛选、详情抽屉（完整策略链、规则、协议元数据）、单条/全部断开（全部断开二次确认）。
  4. **订阅与规则（`/providers`）**：Proxy Providers 订阅列表与一键更新/健康检查、Rule Providers 更新、Rules 搜索与命中计数。
  5. **运行日志（`/logs`）**：双 Tab（实时日志流 / 历史持久化日志），级别过滤、关键字搜索、暂停滚动与复制。
  6. **内核与设置（`/settings`）**：Mihomo 运行参数在线修改、诊断工具（DNS 查询、清空 FakeIP/DNS 缓存、更新 Geo 数据库、重载配置、重启内核——高危项均带二次确认）、持久化清理策略与 DB 状态查看、前端偏好管理、当前账号信息与登出。

---

## 7. 安全测试隔离原则

- 测试仅使用本地模拟服务或隔离容器，禁止连接 `192.168.5.2` 上的生产代理。仓库禁止提交真实订阅链接、密码与内网私有密钥。
- 单元测试使用内置 Mock Mihomo HTTP+WS 服务与临时 SQLite。
- 集成测试在本地单独启动隔离的 `metacubex/mihomo` 测试容器与临时的 gitignored 配置文件，完整验证切换节点/模式、测速、跨重启流量累加、历史连接落盘等全流程后销毁清理。

---

## 8. Docker 与 Compose

示例 Compose 通过 `./data` 挂载 Mihomo 配置，并通过 `./deck-data` 挂载 Deck 的 SQLite 数据。Mihomo 的 `9090` 默认仅供同网络容器访问，发布该端口会提供一条绕过 Deck 鉴权的直连路径。启动步骤和完整变量表见 [README](../README.md#-快速部署docker-compose)。

| 变量 | 部署行为 |
| --- | --- |
| `MIHOMO_URL` | 示例 Compose 默认为 `http://mihomo:9090`，可在 `.env` 中设为网络外可达的控制器地址。独立运行 Go 服务时默认为 `http://127.0.0.1:9090` |
| `MIHOMO_SECRET` | 默认为空，控制器配置了 `secret` 时填入相同值 |
| `AUTH_USERNAME` | 管理员用户名，由 `.env` 配置，默认 `admin`，每次启动校验非空白 |
| `AUTH_PASSWORD` | 管理员密码，由 `.env` 配置，Compose 要求非空，Go 每次启动校验长度和弱占位值 |
| `JWT_SECRET` | Compose 要求非空，Go 每次启动校验至少 16 字节且避开弱占位值 |
| `TRUST_PROXY` | 默认为 `false`，部署在可信反向代理之后可设为 `true` |
| `TRUSTED_PROXIES` | 建议设为实际反向代理的 CIDR 列表。Compose 在变量未设置或为空时传入 `127.0.0.1/32,::1/128,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16`。独立运行 Go 服务且未设置本变量时，仅 `TRUST_PROXY=true` 会使用该列表 |

修改 `.env` 中的管理员凭据后，运行 `docker compose up -d mihomo-deck` 以应用新的环境变量。Deck 在启动时同步凭据并撤销旧会话。

容器启动时检查 `appuser`（UID/GID `1000`）对 `/data` 的写入和访问权限，必要时调整所有权。应用进程和容器健康探针均以 `appuser` 运行。

本地可用 `docker build -t mihomo-deck:local .` 构建。通过 `--platform` 选择目标平台时，BuildKit 提供 `TARGETOS` 和 `TARGETARCH`，供 Go 设置 `GOOS` 和 `GOARCH`。例如，使用 `docker buildx build --platform linux/arm64 --load -t mihomo-deck:local-arm64 .` 构建 ARM64 镜像。CI 在后端 `go vet`、`go test` 和前端 `npm ci`、`npm run build` 通过后才构建并推送 `linux/amd64` 镜像。
