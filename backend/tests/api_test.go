package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/TeemoSun/mihomo-deck/internal/api"
	"github.com/TeemoSun/mihomo-deck/internal/collector"
	"github.com/TeemoSun/mihomo-deck/internal/config"
	"github.com/TeemoSun/mihomo-deck/internal/database"
	"github.com/TeemoSun/mihomo-deck/internal/mihomo"
	"github.com/gorilla/websocket"
	"golang.org/x/crypto/bcrypt"
)

func TestAuthAndProxy(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/proxies/节点 😀/delay" {
			t.Errorf("upstream path %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"delay":42}`))
	}))
	defer upstream.Close()
	db, err := database.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	if err = database.EnsureAdminSeed(db, "admin", "good-password"); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{JWTSecret: "a-strong-test-secret", DBPath: ":memory:"}
	client := mihomo.NewClient(upstream.URL, "")
	c := collector.New(db, client, database.DefaultPreferences(30, 30, "info"))
	h := api.NewRouter(db, cfg, client, c)
	login := httptest.NewRecorder()
	h.ServeHTTP(login, httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"username":"admin","password":"good-password"}`)))
	if login.Code != 200 {
		t.Fatalf("login: %d %s", login.Code, login.Body.String())
	}
	var body struct {
		CSRF string `json:"csrfToken"`
	}
	if err = json.Unmarshal(login.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	cookies := login.Result().Cookies()
	request := func(method, path string, csrf bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewReader(nil))
		for _, cookie := range cookies {
			req.AddCookie(cookie)
		}
		if csrf {
			req.Header.Set("X-CSRF-Token", body.CSRF)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	if got := request("DELETE", "/api/telemetry/logs", false).Code; got != 403 {
		t.Fatalf("csrf status %d", got)
	}
	if got := request("DELETE", "/api/telemetry/logs", true).Code; got != 204 {
		t.Fatalf("delete status %d", got)
	}
	rec := request("GET", "/api/mihomo/proxies/"+url.PathEscape("节点 😀")+"/delay", false)
	if rec.Code != 200 {
		t.Fatalf("delay: %d %s", rec.Code, rec.Body.String())
	}
	records, err := database.QueryDelays(db, "", time.Unix(0, 0), 10)
	if err != nil || len(records) != 1 || records[0].ProxyName != "节点 😀" {
		t.Fatalf("delays: %+v %v", records, err)
	}
	refresh := request("POST", "/api/auth/refresh", false)
	if refresh.Code != 200 {
		t.Fatalf("refresh: %d", refresh.Code)
	}
	if got := request("POST", "/api/auth/refresh", false).Code; got != 401 {
		t.Fatalf("replay: %d", got)
	}
}

func TestValidateSecrets(t *testing.T) {
	for _, password := range []string{"", "admin", "password", "secret", "changeme", "change-me", "123456"} {
		if err := (config.Config{AuthPassword: password, JWTSecret: "a-strong-test-secret"}).ValidateSecrets(true); err == nil {
			t.Errorf("ValidateSecrets accepted password %q", password)
		}
	}
	for _, secret := range []string{"", "admin", "password", "secret", "changeme", "change-me", "123456"} {
		if err := (config.Config{AuthPassword: "good-password", JWTSecret: secret}).ValidateSecrets(true); err == nil {
			t.Errorf("ValidateSecrets accepted JWT secret %q", secret)
		}
	}
	for _, secret := range []string{"", "x"} {
		if err := (config.Config{AuthPassword: "good-password", JWTSecret: "a-strong-test-secret", MihomoSecret: secret}).ValidateSecrets(true); err != nil {
			t.Errorf("ValidateSecrets rejected Mihomo secret %q: %v", secret, err)
		}
	}
}

func TestLoginRateLimitAndTrustedProxyIP(t *testing.T) {
	upstream := httptest.NewServer(http.NotFoundHandler())
	defer upstream.Close()
	db, err := database.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	if err = database.EnsureAdminSeed(db, "admin", "good-password"); err != nil {
		t.Fatal(err)
	}
	client := mihomo.NewClient(upstream.URL, "")
	h := api.NewRouter(db, config.Config{JWTSecret: "a-strong-test-secret", TrustProxy: true}, client, collector.New(db, client, database.DefaultPreferences(30, 30, "info")))
	login := func(xff string) int {
		req := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"username":"admin","password":"bad"}`))
		req.Header.Set("X-Forwarded-For", xff)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	for i := 0; i < 5; i++ {
		if got := login("198.51.100." + string(rune('1'+i)) + ", 203.0.113.7"); got != 401 {
			t.Fatalf("attempt %d status %d", i+1, got)
		}
	}
	if got := login("203.0.113.99, 203.0.113.7"); got != 429 {
		t.Fatalf("rotated left XFF status %d", got)
	}
}

func TestPasswordChangePersistsAndRotatesSessions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.db")
	db, err := database.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	if err = database.EnsureAdminSeed(db, "admin", "good-password"); err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.NotFoundHandler())
	defer upstream.Close()
	client := mihomo.NewClient(upstream.URL, "")
	c := collector.New(db, client, database.DefaultPreferences(30, 30, "info"))
	h := api.NewRouter(db, config.Config{JWTSecret: "a-strong-test-secret"}, client, c)
	loginReq := func(password string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"username":"admin","password":"`+password+`"}`))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	login := loginReq("good-password")
	if login.Code != 200 {
		t.Fatalf("login %d", login.Code)
	}
	var payload struct {
		CSRF string `json:"csrfToken"`
	}
	_ = json.Unmarshal(login.Body.Bytes(), &payload)
	oldCookies := login.Result().Cookies()
	change := httptest.NewRequest("POST", "/api/auth/password", strings.NewReader(`{"currentPassword":"good-password","newPassword":"new-good-password"}`))
	for _, cookie := range oldCookies {
		change.AddCookie(cookie)
	}
	change.Header.Set("X-CSRF-Token", payload.CSRF)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, change)
	if rec.Code != 200 {
		t.Fatalf("change %d %s", rec.Code, rec.Body.String())
	}
	newCookies := rec.Result().Cookies()
	_ = json.Unmarshal(rec.Body.Bytes(), &payload)
	requestWith := func(path string, cookies []*http.Cookie, csrf string) int {
		r := httptest.NewRequest("GET", path, nil)
		for _, cookie := range cookies {
			r.AddCookie(cookie)
		}
		r.Header.Set("X-CSRF-Token", csrf)
		out := httptest.NewRecorder()
		h.ServeHTTP(out, r)
		return out.Code
	}
	if got := requestWith("/api/auth/me", oldCookies, payload.CSRF); got != 401 {
		t.Fatalf("old access cookie status %d", got)
	}
	refresh := httptest.NewRequest("POST", "/api/auth/refresh", nil)
	for _, cookie := range oldCookies {
		if cookie.Name == "deck_refresh" {
			refresh.AddCookie(cookie)
		}
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, refresh)
	if rr.Code != 401 {
		t.Fatalf("old refresh cookie status %d", rr.Code)
	}
	if got := requestWith("/api/auth/me", newCookies, payload.CSRF); got != 200 {
		t.Fatalf("new session status %d", got)
	}
	db.Close()
	db, err = database.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	if err = database.EnsureAdminSeed(db, "admin", "some-other-seed"); err != nil {
		t.Fatal(err)
	}
	u, err := database.GetUserByUsername(db, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err = bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte("new-good-password")); err != nil {
		t.Fatalf("password did not persist: %v", err)
	}
	newClient := mihomo.NewClient(upstream.URL, "")
	newHandler := api.NewRouter(db, config.Config{JWTSecret: "a-strong-test-secret"}, newClient, collector.New(db, newClient, database.DefaultPreferences(30, 30, "info")))
	after := httptest.NewRecorder()
	newHandler.ServeHTTP(after, httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"username":"admin","password":"new-good-password"}`)))
	if after.Code != 200 {
		t.Fatalf("new password did not authenticate after reopening database: %d", after.Code)
	}
}

func TestPreferencesPersistenceAndBounds(t *testing.T) {
	db, err := database.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	_ = database.EnsureAdminSeed(db, "admin", "good-password")
	up := httptest.NewServer(http.NotFoundHandler())
	defer up.Close()
	client := mihomo.NewClient(up.URL, "")
	c := collector.New(db, client, database.DefaultPreferences(30, 30, "info"))
	h := api.NewRouter(db, config.Config{JWTSecret: "a-strong-test-secret"}, client, c)
	login := httptest.NewRecorder()
	h.ServeHTTP(login, httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"username":"admin","password":"good-password"}`)))
	var x struct {
		CSRF string `json:"csrfToken"`
	}
	_ = json.Unmarshal(login.Body.Bytes(), &x)
	cookies := login.Result().Cookies()
	do := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		for _, cookie := range cookies {
			r.AddCookie(cookie)
		}
		r.Header.Set("X-CSRF-Token", x.CSRF)
		out := httptest.NewRecorder()
		h.ServeHTTP(out, r)
		return out
	}
	put := do("PUT", "/api/preferences", `{"theme":"dark","retentionDays":42,"snapshotIntervalMin":15,"logPersistLevel":"debug","delayTestTimeout":5000}`)
	if put.Code != 200 {
		t.Fatalf("put %d %s", put.Code, put.Body.String())
	}
	got := do("GET", "/api/preferences", "")
	if got.Code != 200 || !strings.Contains(got.Body.String(), `"retentionDays":42`) || !strings.Contains(got.Body.String(), `"theme":"dark"`) {
		t.Fatalf("preferences %d %s", got.Code, got.Body.String())
	}
	persisted, err := database.GetPreferences(db, database.DefaultPreferences(30, 30, "info"))
	if err != nil || persisted.RetentionDays != 42 || persisted.Theme != "dark" {
		t.Fatalf("preferences not persisted: %+v %v", persisted, err)
	}
	for _, bad := range []string{`{"retentionDays":0}`, `{"retentionDays":3651}`, `{"snapshotIntervalMin":10081}`, `{"delayTestTimeout":99}`, `{"delayTestTimeout":60001}`, `{"theme":"neon"}`, `{"logPersistLevel":"trace"}`} {
		if r := do("PUT", "/api/preferences", bad); r.Code != 400 {
			t.Errorf("invalid prefs %s status %d", bad, r.Code)
		}
	}
}

func TestOverviewUpstreamTimeoutFallback(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(4 * time.Second):
		}
	}))
	defer up.Close()
	db, err := database.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	_ = database.EnsureAdminSeed(db, "admin", "good-password")
	client := mihomo.NewClient(up.URL, "")
	h := api.NewRouter(db, config.Config{JWTSecret: "a-strong-test-secret"}, client, collector.New(db, client, database.DefaultPreferences(30, 30, "info")))
	login := httptest.NewRecorder()
	h.ServeHTTP(login, httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"username":"admin","password":"good-password"}`)))
	var x struct {
		CSRF string `json:"csrfToken"`
	}
	_ = json.Unmarshal(login.Body.Bytes(), &x)
	r := httptest.NewRequest("GET", "/api/overview", nil)
	for _, cookie := range login.Result().Cookies() {
		r.AddCookie(cookie)
	}
	r.Header.Set("X-CSRF-Token", x.CSRF)
	start := time.Now()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	elapsed := time.Since(start)
	if rec.Code != 200 {
		t.Fatalf("overview %d %s", rec.Code, rec.Body.String())
	}
	if elapsed > 3500*time.Millisecond {
		t.Fatalf("overview took %s", elapsed)
	}
}

func TestProxyPreservesUpstream504(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(504)
		_, _ = w.Write([]byte(`{"message":"delay timeout"}`))
	}))
	defer up.Close()
	db, err := database.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	_ = database.EnsureAdminSeed(db, "admin", "good-password")
	client := mihomo.NewClient(up.URL, "")
	h := api.NewRouter(db, config.Config{JWTSecret: "a-strong-test-secret"}, client, collector.New(db, client, database.DefaultPreferences(30, 30, "info")))
	login := httptest.NewRecorder()
	h.ServeHTTP(login, httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"username":"admin","password":"good-password"}`)))
	var x struct {
		CSRF string `json:"csrfToken"`
	}
	_ = json.Unmarshal(login.Body.Bytes(), &x)
	r := httptest.NewRequest("GET", "/api/mihomo/proxies/node/delay", nil)
	for _, cookie := range login.Result().Cookies() {
		r.AddCookie(cookie)
	}
	r.Header.Set("X-CSRF-Token", x.CSRF)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != 504 || rec.Body.String() != `{"message":"delay timeout"}` || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("upstream response %d %q %q", rec.Code, rec.Body.String(), rec.Header().Get("Content-Type"))
	}
}

func TestLiveTrafficStream(t *testing.T) {
	upgrader := websocket.Upgrader{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !websocket.IsWebSocketUpgrade(r) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		if r.URL.Path == "/traffic" {
			ticker := time.NewTicker(50 * time.Millisecond)
			defer ticker.Stop()
			for range ticker.C {
				if conn.WriteJSON(map[string]int{"up": 123, "down": 456}) != nil {
					return
				}
			}
		}
		for {
			if _, _, err = conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer upstream.Close()
	db, err := database.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	if err = database.EnsureAdminSeed(db, "admin", "good-password"); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{JWTSecret: "a-strong-test-secret", DBPath: ":memory:"}
	client := mihomo.NewClient(upstream.URL, "")
	c := collector.New(db, client, database.DefaultPreferences(30, 30, "info"))
	ctx, cancel := context.WithCancel(context.Background())
	c.Start(ctx)
	defer func() { cancel(); c.Stop() }()
	server := httptest.NewServer(api.NewRouter(db, cfg, client, c))
	defer server.Close()
	resp, err := http.Post(server.URL+"/api/auth/login", "application/json", strings.NewReader(`{"username":"admin","password":"good-password"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	header := http.Header{}
	for _, cookie := range resp.Cookies() {
		if cookie.Name == "deck_access" {
			header.Add("Cookie", cookie.String())
		}
	}
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/api/ws/live", header)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	found := false
	for !found {
		var event struct {
			Type string          `json:"type"`
			Data json.RawMessage `json:"data"`
		}
		if err = conn.ReadJSON(&event); err != nil {
			t.Fatal(err)
		}
		if event.Type == "traffic" {
			var traffic struct {
				Up int `json:"up"`
			}
			if json.Unmarshal(event.Data, &traffic) != nil || traffic.Up != 123 {
				t.Fatalf("event %s", event.Data)
			}
			found = true
		}
	}
}
