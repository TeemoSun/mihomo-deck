# Mihomo Deck 技术设计方案 (`docs/DESIGN.md`)

## 1. 项目定位与目标

**Mihomo Deck** 是面向 Mihomo（原 Clash Meta）的现代化自托管 Web 控制台，采用 **React 19 + Go + SQLite** 单容器架构。
相比 `metacubexd` 等纯静态前端直连 `9090` 端口的方案，Mihomo Deck 引入 Go 后端作为 BFF（Backend for Frontend）与持久化采集引擎，解决以下痛点：
1. **认证保护**：提供完整的账号密码登录、HttpOnly Cookie 双 Token 鉴权、CSRF 防护与防爆破限流，浏览器侧永不暴露 Mihomo `secret`。
2. **固定后端，开箱即用**：前端彻底移除多后端配置弹窗，由服务端通过 Compose 环境变量直连单一 Mihomo 管理接口（默认示例指向 `http://192.168.5.2:9090`）。
3. **跨重启与跨设备持久化**：Mihomo 重启或浏览器刷新不会丢失流量曲线、累计流量、已关闭连接记录、运行日志、节点测速历史、订阅余量快照与个人界面偏好。
4. **全量能力覆盖与安全护栏**：覆盖节点切换、测速、策略/规则提供者更新、规则命中统计、连接审计与断开、内核参数修改、DNS 诊断、缓存清理、Geo 更新、内核重启，并对高危操作提供二次确认弹窗。
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
- `backend/internal/config`：环境变量读取、默认配置与安全校验（`AUTH_PASSWORD`、`JWT_SECRET` 禁止空值或占位弱密码；`MIHOMO_SECRET` 允许任意配置值含空或简短内网密码）。
- `backend/internal/database`：基于 `modernc.org/sqlite` 的纯 Go SQLite 层（`CGO_ENABLED=0`），开启 WAL、外键约束、`busy_timeout=5000`、`MaxOpenConns(1)`。
- `backend/internal/mihomo`：封装 Mihomo v1.19.x 全量 REST API 与 WebSocket 流连接器。
- `backend/internal/collector`：常驻后台采集引擎，负责断线指数退避重连、连接关闭 diff 落盘、跨重启流量差值累加、小时级聚合、低频订阅/节点快照与 30 天（可配）过期数据自动清理。
- `backend/internal/api`：Chi v5 路由、鉴权中间件、滑动窗口登录限流、CSRF 校验、安全响应头（CSP/nosniff/X-Frame-Options）、WebSocket 实时分发 Hub。
- `frontend/`：React 19 + TypeScript + Vite + Tailwind CSS v4 + Zustand + Recharts + Lucide Icons。

---

## 3. SQLite 数据模型（`backend/internal/database/migrations.go`）

1. **`users`**：单管理员账号表
   - `id` INTEGER PK, `username` TEXT UNIQUE, `password_hash` TEXT, `token_version` INTEGER, `created_at` INTEGER, `updated_at` INTEGER
   - 仅当表为空时由 `AUTH_USERNAME` / `AUTH_PASSWORD` 初始化；网页修改密码后持久化到本表并递增 `token_version`，重启容器保留新密码。
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

- **登录态**：`POST /api/auth/login` 校验 bcrypt 密码后下发：
  - `deck_access` Cookie（HttpOnly, SameSite=Lax, Path=/, 15 分钟）
  - `deck_refresh` Cookie（HttpOnly, SameSite=Lax, Path=/api/auth, 30 天）
  - 响应体返回 CSRF Token（由 `JWT_SECRET` 对当前会话派生 HMAC-SHA256，前端内存/Zustand 保存并在所有变更请求附带 `X-CSRF-Token` 头）。
- **HTTP / HTTPS 兼容**：
  - 默认 `COOKIE_SECURE=false`，开箱适配局域网 `http://192.168.5.2:<port>` 访问。
  - 若部署在 HTTPS 反向代理后，可通过 `TRUST_PROXY=true` 与 `COOKIE_SECURE=true`（或 `auto`，根据 `X-Forwarded-Proto: https` 动态开启 `Secure`）。
- **防爆破与安全头**：
  - 同一 IP 5 分钟内连续失败 5 次触发 `429` 限流。
  - 全局注入 `Content-Security-Policy`（`script-src 'self'`，主题初始化脚本外置在 `public/theme-init.js`）、`X-Content-Type-Options: nosniff`、`X-Frame-Options: DENY`、`Referrer-Policy: no-referrer`。

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
  6. **内核与设置（`/settings`）**：Mihomo 运行参数在线修改、诊断工具（DNS 查询、清空 FakeIP/DNS 缓存、更新 Geo 数据库、重载配置、重启内核——高危项均带二次确认）、持久化清理策略与 DB 状态查看、前端偏好管理、管理员改密与登出。

---

## 7. 安全测试隔离原则

- 严禁在测试中修改 `192.168.5.2` 上的生产代理或提交任何真实订阅链接、密码与内网私有密钥到仓库。
- 单元测试使用内置 Mock Mihomo HTTP+WS 服务与临时 SQLite。
- 集成测试在本地单独启动隔离的 `metacubex/mihomo` 测试容器与临时的 gitignored 配置文件，完整验证切换节点/模式、测速、跨重启流量累加、历史连接落盘等全流程后销毁清理。
