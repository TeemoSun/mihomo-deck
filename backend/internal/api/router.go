package api

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/TeemoSun/mihomo-deck/internal/collector"
	"github.com/TeemoSun/mihomo-deck/internal/config"
	"github.com/TeemoSun/mihomo-deck/internal/database"
	"github.com/TeemoSun/mihomo-deck/internal/mihomo"
	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

type Server struct {
	db        *sql.DB
	cfg       config.Config
	client    *mihomo.Client
	collector *collector.Collector
	mu        sync.Mutex
	failures  map[string][]time.Time
}
type claims struct {
	UserID  int64 `json:"uid"`
	Version int64 `json:"ver"`
	jwt.RegisteredClaims
}

func NewRouter(db *sql.DB, cfg config.Config, client *mihomo.Client, c *collector.Collector) http.Handler {
	s := &Server{db: db, cfg: cfg, client: client, collector: c, failures: map[string][]time.Time{}}
	r := chi.NewRouter()
	r.Use(SecurityHeaders)
	r.Get("/api/health", s.health)
	r.Post("/api/auth/login", s.login)
	r.Post("/api/auth/refresh", s.refresh)
	r.Post("/api/auth/logout", s.logout)
	r.Group(func(r chi.Router) {
		r.Use(s.requireAuth)
		r.Use(s.requireCSRF)
		r.Get("/api/auth/me", s.me)
		r.Post("/api/auth/password", s.password)
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
	return r
}
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self' ws: wss:; frame-ancestors 'none'")
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
func (s *Server) ip(r *http.Request) string {
	if s.cfg.TrustProxy {
		xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For"))
		if xff != "" {
			parts := strings.Split(xff, ",")
			for i := len(parts) - 1; i >= 0; i-- {
				if ip := net.ParseIP(strings.TrimSpace(parts[i])); ip != nil {
					return ip.String()
				}
			}
		} else if ip := net.ParseIP(strings.TrimSpace(r.Header.Get("X-Real-IP"))); ip != nil {
			return ip.String()
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		if ip := net.ParseIP(host); ip != nil {
			return ip.String()
		}
		return host
	}
	return r.RemoteAddr
}
func (s *Server) csrf(u database.User) string {
	mac := hmac.New(sha256.New, []byte(s.cfg.JWTSecret))
	fmt.Fprintf(mac, "%d:%d", u.ID, u.TokenVersion)
	return hex.EncodeToString(mac.Sum(nil))
}

type userKey struct{}

func user(r *http.Request) database.User { return r.Context().Value(userKey{}).(database.User) }
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("deck_access")
		if err != nil {
			fail(w, 401)
			return
		}
		c := &claims{}
		token, err := jwt.ParseWithClaims(cookie.Value, c, func(t *jwt.Token) (any, error) {
			if t.Method != jwt.SigningMethodHS256 {
				return nil, fmt.Errorf("invalid signing method")
			}
			return []byte(s.cfg.JWTSecret), nil
		})
		if err != nil || !token.Valid {
			fail(w, 401)
			return
		}
		u, err := database.GetUserByID(s.db, c.UserID)
		if err != nil || u.TokenVersion != c.Version {
			fail(w, 401)
			return
		}
		next.ServeHTTP(w, r.WithContext(contextWithUser(r, u)))
	})
}
func (s *Server) requireCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS" && !hmac.Equal([]byte(r.Header.Get("X-CSRF-Token")), []byte(s.csrf(user(r)))) {
			fail(w, 403)
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (s *Server) secure(r *http.Request) bool {
	return s.cfg.CookieSecure == "true" || s.cfg.CookieSecure == "auto" && (r.TLS != nil || s.cfg.TrustProxy && strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https"))
}
func (s *Server) cookies(w http.ResponseWriter, r *http.Request, u database.User, refresh string) error {
	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims{UserID: u.ID, Version: u.TokenVersion, RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(now.Add(15 * time.Minute)), IssuedAt: jwt.NewNumericDate(now)}})
	access, err := token.SignedString([]byte(s.cfg.JWTSecret))
	if err != nil {
		return err
	}
	for _, c := range []*http.Cookie{{Name: "deck_access", Value: access, Path: "/", MaxAge: 900}, {Name: "deck_refresh", Value: refresh, Path: "/api/auth", MaxAge: 30 * 86400}} {
		c.HttpOnly = true
		c.Secure = s.secure(r)
		c.SameSite = http.SameSiteLaxMode
		http.SetCookie(w, c)
	}
	return nil
}
func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
func hashToken(v string) string { sum := sha256.Sum256([]byte(v)); return hex.EncodeToString(sum[:]) }
func (s *Server) newRefresh(r *http.Request, u database.User) (string, database.RefreshToken, error) {
	token, err := randomToken()
	if err != nil {
		return "", database.RefreshToken{}, err
	}
	id, err := randomToken()
	return token, database.RefreshToken{ID: id, UserID: u.ID, TokenHash: hashToken(token), UserAgent: r.UserAgent(), IP: s.ip(r), ExpiresAt: time.Now().Add(30 * 24 * time.Hour).Unix()}, err
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	ip := s.ip(r)
	s.mu.Lock()
	now := time.Now()
	recent := s.failures[ip][:0]
	for _, t := range s.failures[ip] {
		if now.Sub(t) < 5*time.Minute {
			recent = append(recent, t)
		}
	}
	s.failures[ip] = recent
	blocked := len(recent) >= 5
	s.mu.Unlock()
	if blocked {
		fail(w, 429)
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body) != nil {
		fail(w, 400)
		return
	}
	u, err := database.GetUserByUsername(s.db, body.Username)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(body.Password)) != nil {
		s.mu.Lock()
		s.failures[ip] = append(s.failures[ip], now)
		s.mu.Unlock()
		fail(w, 401)
		return
	}
	token, t, err := s.newRefresh(r, u)
	if err == nil {
		err = database.CreateRefreshToken(s.db, t)
	}
	if err == nil {
		err = s.cookies(w, r, u, token)
	}
	if err != nil {
		fail(w, 500)
		return
	}
	s.mu.Lock()
	delete(s.failures, ip)
	s.mu.Unlock()
	write(w, map[string]any{"username": u.Username, "csrfToken": s.csrf(u)})
}
func (s *Server) refresh(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("deck_refresh")
	if err != nil {
		fail(w, 401)
		return
	}
	var id int64
	err = s.db.QueryRow("SELECT user_id FROM refresh_tokens WHERE token_hash=? AND revoked_at IS NULL AND expires_at>?", hashToken(cookie.Value), time.Now().Unix()).Scan(&id)
	if err != nil {
		fail(w, 401)
		return
	}
	u, err := database.GetUserByID(s.db, id)
	if err != nil {
		fail(w, 401)
		return
	}
	token, t, err := s.newRefresh(r, u)
	if err == nil {
		err = database.RotateRefreshToken(s.db, hashToken(cookie.Value), t, time.Now())
	}
	if err != nil {
		fail(w, 401)
		return
	}
	if s.cookies(w, r, u, token) != nil {
		fail(w, 500)
		return
	}
	write(w, map[string]any{"username": u.Username, "csrfToken": s.csrf(u)})
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("deck_refresh"); err == nil {
		_ = database.RevokeRefreshToken(s.db, hashToken(c.Value))
	}
	for _, c := range []*http.Cookie{{Name: "deck_access", Path: "/"}, {Name: "deck_refresh", Path: "/api/auth"}} {
		c.MaxAge = -1
		c.HttpOnly = true
		c.Secure = s.secure(r)
		c.SameSite = http.SameSiteLaxMode
		http.SetCookie(w, c)
	}
	w.WriteHeader(204)
}
func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	u := user(r)
	write(w, map[string]any{"username": u.Username, "csrfToken": s.csrf(u)})
}
func (s *Server) password(w http.ResponseWriter, r *http.Request) {
	var b struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&b) != nil {
		fail(w, 400)
		return
	}
	u := user(r)
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(b.CurrentPassword)) != nil {
		fail(w, 401)
		return
	}
	weak := map[string]bool{"admin": true, "password": true, "secret": true, "changeme": true, "change-me": true, "123456": true}
	if len(b.NewPassword) < 8 || weak[strings.ToLower(strings.TrimSpace(b.NewPassword))] {
		fail(w, 400)
		return
	}
	if database.UpdatePassword(s.db, u.ID, b.NewPassword) != nil {
		fail(w, 500)
		return
	}
	u, err := database.GetUserByID(s.db, u.ID)
	if err != nil {
		fail(w, 500)
		return
	}
	token, t, err := s.newRefresh(r, u)
	if err == nil {
		err = database.CreateRefreshToken(s.db, t)
	}
	if err == nil {
		err = s.cookies(w, r, u, token)
	}
	if err != nil {
		fail(w, 500)
		return
	}
	write(w, map[string]any{"csrfToken": s.csrf(u)})
}
func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	var n int
	if s.db.QueryRowContext(r.Context(), "SELECT 1").Scan(&n) != nil {
		fail(w, 503)
		return
	}
	write(w, map[string]any{"ok": true, "upstream": s.collector.Snapshot().Status.Connected})
}
func (s *Server) static(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/api" {
		fail(w, 404)
		return
	}
	path := filepath.Clean("/" + r.URL.Path)
	file := filepath.Join(s.cfg.StaticDir, path)
	root, err := filepath.Abs(s.cfg.StaticDir)
	if err != nil {
		fail(w, 404)
		return
	}
	resolved, err := filepath.EvalSymlinks(file)
	if err == nil {
		rel, e := filepath.Rel(root, resolved)
		if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			fail(w, 404)
			return
		}
		if info, e := os.Stat(resolved); e == nil && !info.IsDir() {
			http.ServeFile(w, r, resolved)
			return
		}
	}
	http.ServeFile(w, r, filepath.Join(root, "index.html"))
}
func num(r *http.Request, key string, def int) int {
	v, e := strconv.Atoi(r.URL.Query().Get(key))
	if e != nil {
		return def
	}
	return v
}
