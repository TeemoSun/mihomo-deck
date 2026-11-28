package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/TeemoSun/mihomo-deck/internal/collector"
	"github.com/TeemoSun/mihomo-deck/internal/config"
	"github.com/TeemoSun/mihomo-deck/internal/mihomo"
	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
)

type Server struct {
	handler   http.Handler
	db        *sql.DB
	cfg       config.Config
	client    *mihomo.Client
	collector *collector.Collector

	mu               sync.Mutex
	attempts         map[string]*loginAttempts
	nextAttemptPrune time.Time
	bcryptSlots      chan struct{}
	prefsMu          sync.Mutex
	wsMu             sync.Mutex
	ws               map[*websocket.Conn]liveSocket
	wsClosing        bool
}

func NewRouter(db *sql.DB, cfg config.Config, client *mihomo.Client, c *collector.Collector) *Server {
	s := &Server{
		db: db, cfg: cfg, client: client, collector: c,
		attempts: make(map[string]*loginAttempts), bcryptSlots: make(chan struct{}, 4),
		ws: make(map[*websocket.Conn]liveSocket),
	}
	r := chi.NewRouter()
	r.Use(SecurityHeaders, noStore)
	r.Get("/api/health", s.health)
	r.Group(func(r chi.Router) {
		r.Use(requireSameOrigin)
		r.With(requireJSON).Post("/api/auth/login", s.login)
		r.Post("/api/auth/refresh", s.refresh)
		r.Post("/api/auth/logout", s.logout)
	})
	r.Group(func(r chi.Router) {
		r.Use(s.requireAuth)
		r.Use(s.requireCSRF)
		r.Get("/api/auth/me", s.me)
		r.Get("/api/preferences", s.getPreferences)
		r.Put("/api/preferences", s.putPreferences)
		r.Get("/api/overview", s.overview)
		r.Route("/api/telemetry", func(r chi.Router) {
			r.Get("/traffic", s.traffic)
			r.Get("/connections", s.connections)
			r.Delete("/connections", s.clearConnections)
			r.Get("/logs", s.logs)
			r.Delete("/logs", s.clearLogs)
			r.Get("/delays", s.delays)
			r.Get("/providers", s.providers)
			r.Get("/stats", s.stats)
			r.Post("/prune", s.prune)
		})
		r.Mount("/api/mihomo", s.proxyRouter())
		r.Get("/api/ws/live", s.live)
	})
	r.NotFound(s.static)
	s.handler = r
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.HasPrefix(path, "/api/auth/") || strings.HasPrefix(path, "/api/telemetry/") ||
			path == "/api/overview" || path == "/api/preferences" {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self' ws: wss:; frame-ancestors 'none'; base-uri 'self'; object-src 'none'; form-action 'self'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}
func write(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, code int) { http.Error(w, http.StatusText(code), code) }
func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	var n int
	if s.db.QueryRowContext(r.Context(), "SELECT 1").Scan(&n) != nil {
		fail(w, 503)
		return
	}
	write(w, map[string]any{"ok": true, "upstream": s.collector.Status().Connected})
}
func (s *Server) static(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/api" {
		fail(w, 404)
		return
	}
	root, err := filepath.Abs(s.cfg.StaticDir)
	if err == nil {
		root, err = filepath.EvalSymlinks(root)
	}
	if err != nil {
		fail(w, 404)
		return
	}
	path := filepath.Clean("/" + r.URL.Path)
	file, err := filepath.EvalSymlinks(filepath.Join(root, path))
	if err == nil {
		if !insideRoot(root, file) {
			fail(w, 404)
			return
		}
		if info, err := os.Stat(file); err == nil && info.Mode().IsRegular() {
			http.ServeFile(w, r, file)
			return
		}
	}
	if path == "/assets" || strings.HasPrefix(path, "/assets/") || filepath.Ext(path) != "" {
		fail(w, 404)
		return
	}
	index, err := filepath.EvalSymlinks(filepath.Join(root, "index.html"))
	if err != nil || !insideRoot(root, index) {
		fail(w, 404)
		return
	}
	if info, err := os.Stat(index); err != nil || !info.Mode().IsRegular() {
		fail(w, 404)
		return
	}
	http.ServeFile(w, r, index)
}

func insideRoot(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func num(r *http.Request, key string, def int) int {
	v, e := strconv.Atoi(r.URL.Query().Get(key))
	if e != nil {
		return def
	}
	return v
}
