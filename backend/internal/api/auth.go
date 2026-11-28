package api

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"time"

	"github.com/TeemoSun/mihomo-deck/internal/config"
	"github.com/TeemoSun/mihomo-deck/internal/database"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

type claims struct {
	UserID    int64  `json:"uid"`
	Version   int64  `json:"ver"`
	SessionID string `json:"sid,omitempty"`
	jwt.RegisteredClaims
}

type authContext struct {
	user   database.User
	claims *claims
}
type authKey struct{}

func authentication(r *http.Request) authContext {
	return r.Context().Value(authKey{}).(authContext)
}

func user(r *http.Request) database.User { return authentication(r).user }

func failAuth(w http.ResponseWriter) {
	w.Header().Set("X-Deck-Auth-Error", "1")
	fail(w, http.StatusUnauthorized)
}

func authDBError(w http.ResponseWriter, err error) {
	if errors.Is(err, sql.ErrNoRows) {
		failAuth(w)
		return
	}
	fail(w, http.StatusInternalServerError)
}

func (s *Server) accessClaims(r *http.Request) (*claims, error) {
	cookie, err := r.Cookie("deck_access")
	if err != nil {
		return nil, err
	}
	c := &claims{}
	_, err = jwt.ParseWithClaims(cookie.Value, c, func(*jwt.Token) (any, error) {
		return []byte(s.cfg.JWTSecret), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithExpirationRequired())
	return c, err
}

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := s.accessClaims(r)
		if err != nil {
			failAuth(w)
			return
		}
		u, err := database.GetUserByID(s.db, c.UserID)
		if err != nil {
			authDBError(w, err)
			return
		}
		if u.TokenVersion != c.Version {
			failAuth(w)
			return
		}
		ctx := context.WithValue(r.Context(), authKey{}, authContext{user: u, claims: c})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) csrf(u database.User) string {
	mac := hmac.New(sha256.New, []byte(s.cfg.JWTSecret))
	fmt.Fprintf(mac, "%d:%d", u.ID, u.TokenVersion)
	return hex.EncodeToString(mac.Sum(nil))
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

func requireJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/json" {
			fail(w, http.StatusUnsupportedMediaType)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) cookies(w http.ResponseWriter, r *http.Request, u database.User, refresh, sessionID string) error {
	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims{
		UserID: u.ID, Version: u.TokenVersion, SessionID: sessionID,
		RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(now.Add(15 * time.Minute)), IssuedAt: jwt.NewNumericDate(now)},
	})
	access, err := token.SignedString([]byte(s.cfg.JWTSecret))
	if err != nil {
		return err
	}
	s.setSessionCookies(w, r, access, refresh, 900, 30*86400)
	return nil
}

func (s *Server) setSessionCookies(w http.ResponseWriter, r *http.Request, access, refresh string, accessAge, refreshAge int) {
	secure := s.secure(r)
	for _, c := range []http.Cookie{
		{Name: "deck_access", Value: access, Path: "/", MaxAge: accessAge},
		{Name: "deck_refresh", Value: refresh, Path: "/api/auth", MaxAge: refreshAge},
	} {
		c.HttpOnly = true
		c.Secure = secure
		c.SameSite = http.SameSiteLaxMode
		http.SetCookie(w, &c)
	}
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
	return token, database.RefreshToken{ID: id, SessionID: id, UserID: u.ID, TokenHash: hashToken(token), UserAgent: r.UserAgent(), IP: s.ip(r), ExpiresAt: time.Now().Add(30 * 24 * time.Hour).Unix()}, err
}

func (s *Server) issueSession(w http.ResponseWriter, r *http.Request, u database.User, token string, t database.RefreshToken) error {
	if err := database.CreateRefreshToken(s.db, t); err != nil {
		return err
	}
	return s.cookies(w, r, u, token, t.SessionID)
}

const dummyPasswordHash = "$2a$10$92IXUNpkjO0rOQ5byMi.Ye4oKoEa3Ro9llC/.og/at2uheWG/igi"

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body) != nil || !config.ValidPasswordLength(body.Password) {
		fail(w, 400)
		return
	}
	if !s.acquireBcrypt() {
		fail(w, 429)
		return
	}
	defer s.releaseBcrypt()
	ip := s.ip(r)
	if !s.reserveLogin(ip, time.Now()) {
		fail(w, 429)
		return
	}
	invalid, success := false, false
	defer func() { s.finishLogin(ip, invalid, success) }()
	u, err := database.GetUserByUsername(s.db, body.Username)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		fail(w, 500)
		return
	}
	passwordHash := u.PasswordHash
	if err != nil {
		passwordHash = dummyPasswordHash
	}
	if compareErr := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(body.Password)); compareErr != nil || err != nil {
		invalid = true
		failAuth(w)
		return
	}
	token, t, err := s.newRefresh(r, u)
	if err == nil {
		err = s.issueSession(w, r, u, token, t)
	}
	if err != nil {
		authDBError(w, err)
		return
	}
	success = true
	write(w, map[string]any{"username": u.Username, "csrfToken": s.csrf(u)})
}

func (s *Server) refresh(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("deck_refresh")
	if err != nil {
		failAuth(w)
		return
	}
	oldHash := hashToken(cookie.Value)
	u, err := database.GetRefreshUser(s.db, oldHash, time.Now())
	if err != nil {
		authDBError(w, err)
		return
	}
	token, t, err := s.newRefresh(r, u)
	if err == nil {
		var sessionID string
		sessionID, err = database.RotateRefreshToken(s.db, oldHash, t, time.Now())
		if err == nil {
			err = s.cookies(w, r, u, token, sessionID)
		}
	}
	if err != nil {
		authDBError(w, err)
		return
	}
	write(w, map[string]any{"username": u.Username, "csrfToken": s.csrf(u)})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	var userID int64
	if c, err := r.Cookie("deck_refresh"); err == nil {
		userID, err = database.RevokeRefreshSession(s.db, hashToken(c.Value))
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			fail(w, 500)
			return
		}
	}
	if userID == 0 {
		if c, err := s.accessClaims(r); err == nil {
			userID = c.UserID
			if c.SessionID != "" && database.RevokeSessionByID(s.db, userID, c.SessionID) != nil {
				fail(w, 500)
				return
			}
		}
	}
	if userID != 0 {
		s.CloseUserSockets(userID)
	}
	s.setSessionCookies(w, r, "", "", -1, -1)
	w.WriteHeader(204)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	u := user(r)
	write(w, map[string]any{"username": u.Username, "csrfToken": s.csrf(u)})
}
