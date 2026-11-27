# AGENTS.md — Mihomo Deck

Self-hosted persistent Web console for Mihomo (Clash Meta): React 19 frontend + Go (Chi + pure-Go SQLite) backend + single-container Docker deployment.
See `docs/DESIGN.md` for full architecture, schema, and security specifications.

## Repository Layout

- `backend/` — Go 1.23+ (`CGO_ENABLED=0`), Chi v5, `modernc.org/sqlite`, `gorilla/websocket`, `golang-jwt/jwt/v5`, `golang.org/x/crypto`.
  - `cmd/server/main.go` — entrypoint, `-healthcheck` CLI probe, collector + HTTP server lifecycle.
  - `internal/config/` — env config loader (`MIHOMO_URL`, `MIHOMO_SECRET`, `AUTH_USERNAME`, `AUTH_PASSWORD`, `JWT_SECRET`, `DB_PATH`, `STATIC_DIR`, etc.) and secret validation.
  - `internal/database/` — pure-Go SQLite (`WAL`, `busy_timeout=5000`, `MaxOpenConns(1)`), idempotent migrations, repositories.
  - `internal/mihomo/` — typed REST + WebSocket client for Mihomo external-controller API.
  - `internal/collector/` — persistent background WS collectors (traffic, memory, connections diff, logs), reset-safe cumulative rollups, periodic snapshots, retention pruner.
  - `internal/api/` — HTTP handlers, auth/CSRF/rate-limit middleware, WS live hub, SPA static fallback.
  - `tests/` — Go unit & integration tests (using mock Mihomo server and temp SQLite).
- `frontend/` — React 19 + TypeScript + Vite + Tailwind CSS v4 + Zustand + Recharts + `lucide-react`.
  - `public/theme-init.js` — external CSP-safe theme bootstrapper (never add inline `<script>` to `index.html`).
  - `src/` — `api/`, `components/`, `hooks/`, `pages/`, `stores/`, `lib/`.
- `docker-compose.sample.yml` — production NAS Compose sample merging `mihomo` + `mihomo-deck`.
- `Dockerfile` — multi-stage Node + Go (`CGO_ENABLED=0`) + Alpine runtime (non-root `appuser`, `-healthcheck`).
- `.github/workflows/docker-publish.yml` — builds `linux/amd64` image and pushes to `ghcr.io/teemosun/mihomo-deck`.

## Commands

Backend (from `backend/`):
```bash
go vet ./...
go test -v -count=1 ./...
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o mihomo-deck ./cmd/server
```

Frontend (from `frontend/`):
```bash
npm ci
npm run dev       # Vite dev server (:5173), proxies /api to 127.0.0.1:8080
npm run build     # tsc -b && vite build (must pass strictly with zero unused locals/imports)
```

Docker build (from repo root):
```bash
docker build -t mihomo-deck:local .
```

## Critical Conventions & Gotchas

1. **Public repo hygiene**: NEVER commit `.env`, `*.db`, real `MIHOMO_SECRET`, real passwords, or subscription URLs.
2. **No CGO**: SQLite MUST remain `modernc.org/sqlite` so `CGO_ENABLED=0` static builds work cleanly.
3. **Secret validation**: Reject empty or weak placeholder `AUTH_PASSWORD` / `JWT_SECRET` (`admin`, `password`, `secret`, `changeme`, `change-me`, `123456`). Do NOT reject short/simple `MIHOMO_SECRET` (some users configure simple LAN controller secrets or leave it empty).
4. **Password persistence**: Seed admin from `AUTH_USERNAME`/`AUTH_PASSWORD` only when `users` table is empty. Changing password in the UI persists in SQLite across container restarts.
5. **No frontend backend-URL modal**: Mihomo upstream is fixed server-side via `MIHOMO_URL` & `MIHOMO_SECRET`; the browser never receives `MIHOMO_SECRET`.
6. **CSP & Security Headers**: Keep `script-src 'self'` (no inline scripts in `index.html`). Cookies default to `Secure=false` for LAN HTTP compatibility, toggled via `COOKIE_SECURE` / `TRUST_PROXY`.
7. **Reset-safe traffic tracking**: Always handle `currTotal < lastTotal` (Mihomo restart) cleanly without losing or negative-wrapping `cumulative_up` / `cumulative_down`.
8. **All UI strings in Simplified Chinese**, high-density Linear/Cloudflare pro console style, responsive desktop sidebar + mobile bottom navigation, and confirmation dialogs for high-risk Mihomo actions.
9. **Never touch `192.168.5.2` during tests**: All tests must use local mocks or isolated local containers.
