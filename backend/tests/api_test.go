package tests

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net"
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
)

func TestAuthAndProxy(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.EscapedPath() {
		case "/proxies/%E8%8A%82%E7%82%B9%20%F0%9F%98%80/delay", "/proxies/HK%2F01%20%F0%9F%98%80/delay", "/proxies/HK%2F01%20%F0%9F%98%80", "/proxies/100%25/delay":
		default:
			t.Errorf("upstream path %q", r.URL.EscapedPath())
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
	if err = database.SyncAdmin(db, "admin", "good-password"); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{JWTSecret: "a-strong-test-secret", DBPath: ":memory:"}
	client := mihomo.NewClient(upstream.URL, "")
	c := collector.New(db, client, database.DefaultPreferences(30, 30, "info"))
	h := api.NewRouter(db, cfg, client, c)
	login := httptest.NewRecorder()
	h.ServeHTTP(login, jsonRequest("POST", "/api/auth/login", strings.NewReader(`{"username":"admin","password":"good-password"}`)))
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
	rec := request("POST", "/api/mihomo/proxies/"+url.PathEscape("节点 😀")+"/delay", true)
	if rec.Code != 200 {
		t.Fatalf("delay: %d %s", rec.Code, rec.Body.String())
	}
	records, err := database.QueryDelays(db, "", "", time.Unix(0, 0), 10)
	if err != nil || len(records) != 1 || records[0].ProxyName != "节点 😀" {
		t.Fatalf("delays: %+v %v", records, err)
	}
	for _, name := range []string{"HK/01 😀", "100%"} {
		rec := request("POST", "/api/mihomo/proxies/"+url.PathEscape(name)+"/delay", true)
		if rec.Code != 200 {
			t.Fatalf("delay %q: %d %s", name, rec.Code, rec.Body.String())
		}
	}
	selectReq := httptest.NewRequest("PUT", "/api/mihomo/proxies/"+url.PathEscape("HK/01 😀"), strings.NewReader(`{"name":"node"}`))
	for _, cookie := range cookies {
		selectReq.AddCookie(cookie)
	}
	selectReq.Header.Set("X-CSRF-Token", body.CSRF)
	selected := httptest.NewRecorder()
	h.ServeHTTP(selected, selectReq)
	if selected.Code != 200 {
		t.Fatalf("select: %d %s", selected.Code, selected.Body.String())
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
	valid := config.Config{AuthUsername: "admin", AuthPassword: "good-password", JWTSecret: testJWTSecret}
	for _, username := range []string{"", " \t\n"} {
		cfg := valid
		cfg.AuthUsername = username
		if err := cfg.ValidateSecrets(); err == nil || !strings.Contains(err.Error(), "AUTH_USERNAME") {
			t.Errorf("ValidateSecrets returned %v for username %q", err, username)
		}
	}
	for _, password := range []string{"", "admin", "password", "secret", "changeme", "change-me", "123456", "Short7!", strings.Repeat("界", 24) + "a"} {
		cfg := valid
		cfg.AuthPassword = password
		if err := cfg.ValidateSecrets(); err == nil || !strings.Contains(err.Error(), "AUTH_PASSWORD") {
			t.Errorf("ValidateSecrets returned %v for password %q", err, password)
		}
	}
	for _, secret := range []string{"", "admin", "password", "secret", "changeme", "change-me", "123456", "    ChAnGeMe    ", "123456789012345"} {
		cfg := valid
		cfg.JWTSecret = secret
		if err := cfg.ValidateSecrets(); err == nil || !strings.Contains(err.Error(), "JWT_SECRET") {
			t.Errorf("ValidateSecrets returned %v for JWT secret %q", err, secret)
		}
	}
	for _, secret := range []string{"", "x"} {
		cfg := valid
		cfg.MihomoSecret = secret
		if err := cfg.ValidateSecrets(); err != nil {
			t.Errorf("ValidateSecrets rejected Mihomo secret %q: %v", secret, err)
		}
	}
	for _, password := range []string{"界界ab", strings.Repeat("界", 24)} {
		cfg := valid
		cfg.AuthPassword = password
		cfg.JWTSecret = strings.Repeat("界", 5) + "a"
		if err := cfg.ValidateSecrets(); err != nil {
			t.Errorf("ValidateSecrets rejected a password of %d bytes and a JWT secret of %d bytes: %v", len(password), len(cfg.JWTSecret), err)
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
	if err = database.SyncAdmin(db, "admin", "good-password"); err != nil {
		t.Fatal(err)
	}
	client := mihomo.NewClient(upstream.URL, "")
	_, trusted, err := net.ParseCIDR("10.0.0.0/8")
	if err != nil {
		t.Fatal(err)
	}
	h := api.NewRouter(db, config.Config{JWTSecret: "a-strong-test-secret", TrustProxy: true, TrustedProxyNets: []*net.IPNet{trusted}}, client, collector.New(db, client, database.DefaultPreferences(30, 30, "info")))
	login := func(xff string) int {
		req := jsonRequest("POST", "/api/auth/login", strings.NewReader(`{"username":"admin","password":"bad-password"}`))
		req.RemoteAddr = "10.0.0.1:1234"
		req.Header.Set("X-Forwarded-For", xff)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	for i := 0; i < 5; i++ {
		if got := login("198.51.100." + string(rune('1'+i)) + ", 203.0.113.7, 10.1.2.3"); got != 401 {
			t.Fatalf("attempt %d status %d", i+1, got)
		}
	}
	if got := login("203.0.113.99, 203.0.113.7, 10.1.2.3"); got != 429 {
		t.Fatalf("rotated left XFF status %d", got)
	}
}

func TestEnvCredentialsSyncAcrossRestartAndRevokeOldSessions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.db")
	const username, oldPassword, newPassword = "admin", "good-password", "new-good-password"
	var db *sql.DB
	t.Cleanup(func() {
		if db != nil {
			db.Close()
		}
	})
	restart := func(password string) *api.Server {
		t.Helper()
		if db != nil {
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
		}
		cfg := config.Config{AuthUsername: username, AuthPassword: password, JWTSecret: testJWTSecret}
		if err := cfg.ValidateSecrets(); err != nil {
			t.Fatal(err)
		}
		var err error
		db, err = database.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := database.Migrate(db); err != nil {
			t.Fatal(err)
		}
		if err := database.SyncAdmin(db, cfg.AuthUsername, cfg.AuthPassword); err != nil {
			t.Fatal(err)
		}
		c := collector.New(db, nil, database.DefaultPreferences(30, 30, "info"))
		return api.NewRouter(db, cfg, nil, c)
	}

	deck := restart(oldPassword)
	login := loginToDeck(deck, username, oldPassword)
	loginCSRF(t, login)

	deck = restart(oldPassword)
	me := httptest.NewRequest("GET", "/api/auth/me", nil)
	if rec := serveDeck(deck, withSession(me, login, "")); rec.Code != http.StatusOK {
		t.Fatalf("access after restart with unchanged credentials: %d", rec.Code)
	}
	refresh := httptest.NewRequest("POST", "/api/auth/refresh", nil)
	rotated := serveDeck(deck, withSession(refresh, login, ""))
	if rotated.Code != http.StatusOK {
		t.Fatalf("refresh after restart with unchanged credentials: %d", rotated.Code)
	}

	deck = restart(newPassword)
	me = httptest.NewRequest("GET", "/api/auth/me", nil)
	if rec := serveDeck(deck, withSession(me, rotated, "")); rec.Code != http.StatusUnauthorized {
		t.Errorf("old access after credential change: %d", rec.Code)
	}
	refresh = httptest.NewRequest("POST", "/api/auth/refresh", nil)
	if rec := serveDeck(deck, withSession(refresh, rotated, "")); rec.Code != http.StatusUnauthorized {
		t.Errorf("old refresh after credential change: %d", rec.Code)
	}
	if rec := loginToDeck(deck, username, oldPassword); rec.Code != http.StatusUnauthorized {
		t.Errorf("login with old password: %d", rec.Code)
	}
	login = loginToDeck(deck, username, newPassword)
	csrf := loginCSRF(t, login)
	removed := jsonRequest("POST", "/api/auth/password", strings.NewReader(`{"currentPassword":"new-good-password","newPassword":"another-password"}`))
	if rec := serveDeck(deck, withSession(removed, login, csrf)); rec.Code != http.StatusNotFound {
		t.Errorf("removed password route: %d", rec.Code)
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
	if err := database.SyncAdmin(db, "admin", "good-password"); err != nil {
		t.Fatal(err)
	}
	up := httptest.NewServer(http.NotFoundHandler())
	defer up.Close()
	client := mihomo.NewClient(up.URL, "")
	c := collector.New(db, client, database.DefaultPreferences(30, 30, "info"))
	h := api.NewRouter(db, config.Config{JWTSecret: "a-strong-test-secret"}, client, c)
	login := httptest.NewRecorder()
	h.ServeHTTP(login, jsonRequest("POST", "/api/auth/login", strings.NewReader(`{"username":"admin","password":"good-password"}`)))
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
	if err := database.SyncAdmin(db, "admin", "good-password"); err != nil {
		t.Fatal(err)
	}
	client := mihomo.NewClient(up.URL, "")
	h := api.NewRouter(db, config.Config{JWTSecret: "a-strong-test-secret"}, client, collector.New(db, client, database.DefaultPreferences(30, 30, "info")))
	login := httptest.NewRecorder()
	h.ServeHTTP(login, jsonRequest("POST", "/api/auth/login", strings.NewReader(`{"username":"admin","password":"good-password"}`)))
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
	if err := database.SyncAdmin(db, "admin", "good-password"); err != nil {
		t.Fatal(err)
	}
	client := mihomo.NewClient(up.URL, "")
	h := api.NewRouter(db, config.Config{JWTSecret: "a-strong-test-secret"}, client, collector.New(db, client, database.DefaultPreferences(30, 30, "info")))
	login := httptest.NewRecorder()
	h.ServeHTTP(login, jsonRequest("POST", "/api/auth/login", strings.NewReader(`{"username":"admin","password":"good-password"}`)))
	var x struct {
		CSRF string `json:"csrfToken"`
	}
	_ = json.Unmarshal(login.Body.Bytes(), &x)
	r := httptest.NewRequest("POST", "/api/mihomo/proxies/node/delay", nil)
	r.Header.Set("X-CSRF-Token", x.CSRF)
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
	if err = database.SyncAdmin(db, "admin", "good-password"); err != nil {
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
	deadline := time.Now().Add(3 * time.Second)
	for len(c.Snapshot().LiveTrafficHistory) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("collector did not receive live traffic")
		}
		time.Sleep(10 * time.Millisecond)
	}
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/api/ws/live", header)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var event struct {
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	}
	if err = conn.ReadJSON(&event); err != nil {
		t.Fatal(err)
	}
	var init struct {
		LiveTrafficHistory []database.TrafficSample `json:"liveTrafficHistory"`
	}
	if event.Type != "init" || json.Unmarshal(event.Data, &init) != nil || len(init.LiveTrafficHistory) == 0 {
		t.Fatalf("init event %+v", event)
	}
	last := init.LiveTrafficHistory[len(init.LiveTrafficHistory)-1]
	if last.TS == 0 || last.UpBPS != 123 || last.DownBPS != 456 {
		t.Fatalf("init live sample %+v", last)
	}
	for {
		if err = conn.ReadJSON(&event); err != nil {
			t.Fatal(err)
		}
		if event.Type == "traffic" {
			var traffic collector.TrafficBroadcast
			if json.Unmarshal(event.Data, &traffic) != nil || traffic.Up != 123 || traffic.Down != 456 ||
				traffic.TS < last.TS || traffic.Sample.TS != traffic.TS ||
				traffic.Sample.UpBPS != traffic.Up || traffic.Sample.DownBPS != traffic.Down {
				t.Fatalf("event %s", event.Data)
			}
			break
		}
	}
}

func TestProviderProxyDelay(t *testing.T) {
	var paths []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.EscapedPath())
		switch r.URL.EscapedPath() {
		case "/proxies/node/delay":
			http.NotFound(w, r)
		case "/providers/proxies":
			_, _ = w.Write([]byte(`{"providers":{"default":{"vehicleType":"Compatible","proxies":[{"name":"node","provider-name":"wrong"}]},"sub":{"vehicleType":"HTTP","proxies":[{"name":"node"}]}}}`))
		case "/providers/proxies/sub/node/healthcheck":
			if r.URL.Query().Get("url") != "https://example.test/check?a=1" || r.URL.Query().Get("timeout") != "1234" {
				t.Errorf("query: %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"delay":42}`))
		default:
			t.Errorf("unexpected upstream path %s", r.URL.EscapedPath())
			http.NotFound(w, r)
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
	if err = database.SyncAdmin(db, "admin", "good-password"); err != nil {
		t.Fatal(err)
	}
	client := mihomo.NewClient(up.URL, "")
	h := api.NewRouter(db, config.Config{JWTSecret: "a-strong-test-secret"}, client, collector.New(db, client, database.DefaultPreferences(30, 30, "info")))
	login := httptest.NewRecorder()
	h.ServeHTTP(login, jsonRequest("POST", "/api/auth/login", strings.NewReader(`{"username":"admin","password":"good-password"}`)))
	if login.Code != http.StatusOK {
		t.Fatalf("login: %d", login.Code)
	}
	csrf := loginCSRF(t, login)
	for _, provider := range []string{"sub", ""} {
		paths = nil
		query := url.Values{"url": {"https://example.test/check?a=1"}, "timeout": {"1234"}}
		if provider != "" {
			query.Set("provider", provider)
		}
		req := httptest.NewRequest("POST", "/api/mihomo/proxies/node/delay?"+query.Encode(), nil)
		req.Header.Set("X-CSRF-Token", csrf)
		for _, cookie := range login.Result().Cookies() {
			req.AddCookie(cookie)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || rec.Body.String() != `{"delay":42}` {
			t.Fatalf("provider %q: %d %s", provider, rec.Code, rec.Body.String())
		}
		want := "/providers/proxies/sub/node/healthcheck"
		if provider == "" {
			want = "/proxies/node/delay,/providers/proxies," + want
		}
		if got := strings.Join(paths, ","); got != want {
			t.Fatalf("provider %q: paths %s", provider, got)
		}
	}
	records, err := database.QueryDelays(db, "node", "", time.Unix(0, 0), 10)
	if err != nil || len(records) != 2 {
		t.Fatalf("delay records: %+v %v", records, err)
	}
	for _, record := range records {
		if record.ProviderName != "sub" || record.DelayMS != 42 || !record.OK || record.Source != "manual" || record.TestURL != "https://example.test/check?a=1" {
			t.Errorf("delay record: %+v", record)
		}
	}
}

func jsonRequest(method, path string, body io.Reader) *http.Request {
	req := httptest.NewRequest(method, path, body)
	req.Header.Set("Content-Type", "application/json")
	return req
}

func loginCSRF(t *testing.T, login *httptest.ResponseRecorder) string {
	t.Helper()
	var payload struct {
		CSRF string `json:"csrfToken"`
	}
	if login.Code != http.StatusOK || json.Unmarshal(login.Body.Bytes(), &payload) != nil || payload.CSRF == "" {
		t.Fatalf("login: %d %s", login.Code, login.Body.String())
	}
	return payload.CSRF
}
