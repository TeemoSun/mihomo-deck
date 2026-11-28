package tests

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/TeemoSun/mihomo-deck/internal/api"
	"github.com/TeemoSun/mihomo-deck/internal/collector"
	"github.com/TeemoSun/mihomo-deck/internal/config"
	"github.com/TeemoSun/mihomo-deck/internal/database"
	"github.com/TeemoSun/mihomo-deck/internal/mihomo"
	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
)

const testJWTSecret = "a-strong-test-secret"

func newTestDeck(t *testing.T, cfg config.Config, upstream http.Handler) (*api.Server, *sql.DB) {
	t.Helper()
	db, err := database.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	if err := database.EnsureAdminSeed(db, "admin", "good-password"); err != nil {
		t.Fatal(err)
	}
	var client *mihomo.Client
	if upstream != nil {
		server := httptest.NewServer(upstream)
		t.Cleanup(server.Close)
		client = mihomo.NewClient(server.URL, "")
	}
	cfg.JWTSecret = testJWTSecret
	c := collector.New(db, client, database.DefaultPreferences(30, 30, "info"))
	deck := api.NewRouter(db, cfg, client, c)
	t.Cleanup(deck.CloseAllSockets)
	return deck, db
}

func loginToDeck(deck http.Handler, username, password string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	return serveDeck(deck, jsonRequest("POST", "/api/auth/login", strings.NewReader(string(body))))
}

func serveDeck(deck http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	deck.ServeHTTP(rec, req)
	return rec
}

func withSession(req *http.Request, login *httptest.ResponseRecorder, csrf string) *http.Request {
	for _, c := range (&http.Response{Header: login.Header()}).Cookies() {
		req.AddCookie(c)
	}
	req.Header.Set("X-CSRF-Token", csrf)
	return req
}

func TestConcurrentLoginAndPasswordChanges(t *testing.T) {
	deck, db := newTestDeck(t, config.Config{}, nil)
	initial := loginToDeck(deck, "admin", "good-password")
	csrf := loginCSRF(t, initial)
	start := make(chan struct{})
	results := make(chan *httptest.ResponseRecorder, 3)
	for _, next := range []string{"new-password-one", "new-password-two"} {
		req := jsonRequest("POST", "/api/auth/password", strings.NewReader(fmt.Sprintf("{\"currentPassword\":\"good-password\",\"newPassword\":%q}", next)))
		withSession(req, initial, csrf)
		go func() {
			<-start
			results <- serveDeck(deck, req)
		}()
	}
	loginResult := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		<-start
		loginResult <- loginToDeck(deck, "admin", "good-password")
	}()
	close(start)
	successes := 0
	var winner *httptest.ResponseRecorder
	for i := 0; i < 2; i++ {
		rec := <-results
		switch rec.Code {
		case http.StatusOK:
			successes++
			winner = rec
		case http.StatusUnauthorized:
			if rec.Header().Get("X-Deck-Auth-Error") != "1" {
				t.Fatal("missing auth error header")
			}
		default:
			t.Fatalf("password change returned %d: %s", rec.Code, rec.Body)
		}
	}
	stale := <-loginResult
	if successes != 1 {
		t.Fatalf("%d concurrent password changes succeeded", successes)
	}
	switch stale.Code {
	case http.StatusOK:
		rr := serveDeck(deck, withSession(httptest.NewRequest("POST", "/api/auth/refresh", nil), stale, ""))
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("old-password login kept a refresh token: %d", rr.Code)
		}
	case http.StatusUnauthorized:
	default:
		t.Fatalf("old-password login returned %d: %s", stale.Code, stale.Body)
	}
	if rr := serveDeck(deck, withSession(httptest.NewRequest("POST", "/api/auth/refresh", nil), winner, "")); rr.Code != http.StatusOK {
		t.Fatalf("winning change did not issue a refresh token: %d", rr.Code)
	}
	var live int
	if err := db.QueryRow("SELECT count(*) FROM refresh_tokens WHERE revoked_at IS NULL").Scan(&live); err != nil || live != 1 {
		t.Fatalf("active refresh tokens %d, error %v", live, err)
	}
}

func TestConcurrentLoginFailuresReserveAttemptSlots(t *testing.T) {
	deck, _ := newTestDeck(t, config.Config{}, nil)
	start := make(chan struct{})
	results := make(chan *httptest.ResponseRecorder, 16)
	for i := 0; i < cap(results); i++ {
		go func() {
			<-start
			results <- loginToDeck(deck, "unknown-user", "wrong-password")
		}()
	}
	close(start)
	failures := 0
	for i := 0; i < cap(results); i++ {
		rec := <-results
		switch rec.Code {
		case http.StatusUnauthorized:
			failures++
			if rec.Header().Get("X-Deck-Auth-Error") != "1" {
				t.Fatal("missing auth error header")
			}
		case http.StatusTooManyRequests:
		default:
			t.Fatalf("login returned %d", rec.Code)
		}
	}
	if failures == 0 || failures > 5 {
		t.Fatalf("%d concurrent attempts reached authentication", failures)
	}
	for i := failures; i < 5; i++ {
		if rec := loginToDeck(deck, "admin", "wrong-password"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d returned %d", i+1, rec.Code)
		}
	}
	if rec := loginToDeck(deck, "admin", "good-password"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("sixth attempt returned %d", rec.Code)
	}
}

func TestUntrustedProxyHeadersAreIgnored(t *testing.T) {
	_, trusted, err := net.ParseCIDR("10.0.0.0/8")
	if err != nil {
		t.Fatal(err)
	}
	deck, db := newTestDeck(t, config.Config{TrustProxy: true, TrustedProxyNets: []*net.IPNet{trusted}, CookieSecure: "auto"}, nil)
	spoofed := func(i int, password string) *http.Request {
		req := jsonRequest("POST", "/api/auth/login", strings.NewReader(fmt.Sprintf("{\"username\":\"admin\",\"password\":%q}", password)))
		req.RemoteAddr = "203.0.113.10:1234"
		req.Header.Set("X-Forwarded-For", fmt.Sprintf("198.51.100.%d", i))
		req.Header.Set("X-Real-IP", "198.51.100.99")
		req.Header.Set("X-Forwarded-Proto", "https")
		return req
	}
	login := serveDeck(deck, spoofed(1, "good-password"))
	loginCSRF(t, login)
	for _, cookie := range login.Result().Cookies() {
		if cookie.Secure {
			t.Errorf("%s trusted spoofed HTTPS", cookie.Name)
		}
	}
	var ip string
	if err := db.QueryRow("SELECT ip FROM refresh_tokens").Scan(&ip); err != nil || ip != "203.0.113.10" {
		t.Fatalf("refresh IP %q, error %v", ip, err)
	}
	for i := 1; i <= 6; i++ {
		want := http.StatusUnauthorized
		if i == 6 {
			want = http.StatusTooManyRequests
		}
		if rec := serveDeck(deck, spoofed(i, "wrong-password")); rec.Code != want {
			t.Fatalf("login %d status %d, want %d", i, rec.Code, want)
		}
	}
}

func openLive(t *testing.T, server *httptest.Server, access *http.Cookie) *websocket.Conn {
	t.Helper()
	headers := http.Header{}
	headers.Set("Cookie", access.Name+"="+access.Value)
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/api/ws/live", headers)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var event collector.Event
	if err := conn.ReadJSON(&event); err != nil || event.Type != "init" {
		t.Fatalf("init event %+v, error %v", event, err)
	}
	return conn
}

func accessCookie(t *testing.T, login *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, cookie := range login.Result().Cookies() {
		if cookie.Name == "deck_access" {
			return cookie
		}
	}
	t.Fatal("missing access cookie")
	return nil
}

func expectSocketClosed(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("WebSocket delivered a message after session ended")
	} else if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
		t.Fatal("WebSocket stayed open")
	}
}

func TestLiveSocketClosesOnPasswordChange(t *testing.T) {
	deck, _ := newTestDeck(t, config.Config{}, nil)
	server := httptest.NewServer(deck)
	defer server.Close()
	login := loginToDeck(deck, "admin", "good-password")
	csrf := loginCSRF(t, login)
	conn := openLive(t, server, accessCookie(t, login))
	req := jsonRequest("POST", "/api/auth/password", strings.NewReader("{\"currentPassword\":\"good-password\",\"newPassword\":\"new-password\"}"))
	if rec := serveDeck(deck, withSession(req, login, csrf)); rec.Code != http.StatusOK {
		t.Fatalf("password change: %d %s", rec.Code, rec.Body)
	}
	expectSocketClosed(t, conn)
}

func TestLogoutRevokesRotatedRefreshSessionAndClosesLiveSocket(t *testing.T) {
	deck, _ := newTestDeck(t, config.Config{}, nil)
	server := httptest.NewServer(deck)
	defer server.Close()
	login := loginToDeck(deck, "admin", "good-password")
	other := loginToDeck(deck, "admin", "good-password")
	rotated := serveDeck(deck, withSession(httptest.NewRequest("POST", "/api/auth/refresh", nil), login, ""))
	loginCSRF(t, rotated)
	conn := openLive(t, server, accessCookie(t, rotated))
	if rec := serveDeck(deck, withSession(httptest.NewRequest("POST", "/api/auth/logout", nil), login, "")); rec.Code != http.StatusNoContent {
		t.Fatalf("logout with original cookie: %d", rec.Code)
	}
	expectSocketClosed(t, conn)
	if rec := serveDeck(deck, withSession(httptest.NewRequest("POST", "/api/auth/refresh", nil), rotated, "")); rec.Code != http.StatusUnauthorized {
		t.Fatalf("rotated cookie authenticated after logout: %d", rec.Code)
	}
	if rec := serveDeck(deck, withSession(httptest.NewRequest("POST", "/api/auth/refresh", nil), other, "")); rec.Code != http.StatusOK {
		t.Fatalf("unrelated refresh session revoked: %d", rec.Code)
	}
}

func TestLiveSocketExpiresWithJWT(t *testing.T) {
	deck, _ := newTestDeck(t, config.Config{}, nil)
	server := httptest.NewServer(deck)
	defer server.Close()
	expires := time.Now().Add(2 * time.Second)
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"uid": 1, "ver": 0, "exp": expires.Unix()})
	signed, err := token.SignedString([]byte(testJWTSecret))
	if err != nil {
		t.Fatal(err)
	}
	conn := openLive(t, server, &http.Cookie{Name: "deck_access", Value: signed})
	expectSocketClosed(t, conn)
	if time.Now().Before(time.Unix(expires.Unix(), 0)) {
		t.Fatal("WebSocket closed before JWT expiry")
	}
}

func TestConcurrentPreferencesMergeAndNormalize(t *testing.T) {
	deck, db := newTestDeck(t, config.Config{}, nil)
	login := loginToDeck(deck, "admin", "good-password")
	csrf := loginCSRF(t, login)
	put := func(patch string) *httptest.ResponseRecorder {
		return serveDeck(deck, withSession(jsonRequest("PUT", "/api/preferences", strings.NewReader(patch)), login, csrf))
	}
	for i := 1; i <= 12; i++ {
		start := make(chan struct{})
		done := make(chan *httptest.ResponseRecorder, 2)
		for _, patch := range []string{
			fmt.Sprintf("{\"retentionDays\":%d,\"pinnedProxies\":null}", i),
			fmt.Sprintf("{\"snapshotIntervalMin\":%d,\"hiddenGroups\":null}", i),
		} {
			go func() {
				<-start
				done <- put(patch)
			}()
		}
		close(start)
		for j := 0; j < 2; j++ {
			rec := <-done
			if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "\"pinnedProxies\":[]") || !strings.Contains(rec.Body.String(), "\"hiddenGroups\":[]") {
				t.Fatalf("preferences PUT: %d %s", rec.Code, rec.Body)
			}
		}
		p, err := database.GetPreferences(db, database.DefaultPreferences(30, 30, "info"))
		if err != nil || p.RetentionDays != i || p.SnapshotIntervalMin != i {
			t.Fatalf("preferences did not merge at iteration %d: %+v, %v", i, p, err)
		}
		get := serveDeck(deck, withSession(httptest.NewRequest("GET", "/api/preferences", nil), login, csrf))
		var live database.Preferences
		if err := json.Unmarshal(get.Body.Bytes(), &live); err != nil || live.RetentionDays != i || live.SnapshotIntervalMin != i ||
			live.PinnedProxies == nil || live.HiddenGroups == nil {
			t.Fatalf("live preferences did not match database: %s, %v", get.Body, err)
		}
	}
}

func TestStaticMissingAssetsAndSymlinkContainment(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "www")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	index, outside := filepath.Join(root, "index.html"), filepath.Join(base, "other.html")
	for path, data := range map[string]string{index: "SPA shell", outside: "outside", filepath.Join(root, "app.js"): "js file"} {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for link, target := range map[string]string{filepath.Join(base, "static"): root, filepath.Join(root, "secret.svg"): outside} {
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
	}
	deck := api.NewRouter(nil, config.Config{StaticDir: filepath.Join(base, "static")}, nil, nil)
	for _, path := range []string{"/missing.js", "/missing.css", "/missing.svg", "/missing.png", "/missing.ico", "/missing.map", "/missing.json", "/assets/no-extension", "/secret.svg"} {
		if rec := serveDeck(deck, httptest.NewRequest("GET", path, nil)); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s returned %d", path, rec.Code)
		}
	}
	for path, want := range map[string]string{"/overview": "SPA shell", "/app.js": "js file"} {
		if rec := serveDeck(deck, httptest.NewRequest("GET", path, nil)); rec.Code != http.StatusOK || rec.Body.String() != want {
			t.Errorf("GET %s returned %d %q", path, rec.Code, rec.Body)
		}
	}
	if err := os.Remove(index); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, index); err != nil {
		t.Fatal(err)
	}
	if rec := serveDeck(deck, httptest.NewRequest("GET", "/overview", nil)); rec.Code != http.StatusNotFound {
		t.Fatalf("symlinked index escaped root: %d %s", rec.Code, rec.Body)
	}
}

func TestMihomoActionsRequirePOSTAndCSRF(t *testing.T) {
	deck, _ := newTestDeck(t, config.Config{}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("unexpected Mihomo method %s for %s", r.Method, r.URL.Path)
		}
		switch r.URL.Path {
		case "/proxies/node/delay":
			_, _ = w.Write([]byte("{\"delay\":42}"))
		case "/group/group/delay":
			_, _ = w.Write([]byte("{\"node\":42}"))
		case "/providers/proxies/sub/healthcheck":
			_, _ = w.Write([]byte("health check started"))
		case "/version":
			w.WriteHeader(http.StatusUnauthorized)
		case "/configs":
			_, _ = w.Write([]byte("non-JSON configs"))
		default:
			t.Errorf("unexpected upstream request %s", r.URL.Path)
		}
	}))
	login := loginToDeck(deck, "admin", "good-password")
	csrf := loginCSRF(t, login)
	for path, status := range map[string]int{
		"/api/mihomo/proxies/node/delay":                http.StatusOK,
		"/api/mihomo/group/group/delay":                 http.StatusOK,
		"/api/mihomo/providers/proxies/sub/healthcheck": http.StatusNoContent,
	} {
		for _, tc := range []struct {
			method, token string
			want          int
		}{
			{"GET", "", http.StatusMethodNotAllowed},
			{"POST", "", http.StatusForbidden},
			{"POST", csrf, status},
		} {
			rec := serveDeck(deck, withSession(httptest.NewRequest(tc.method, path, nil), login, tc.token))
			if rec.Code != tc.want {
				t.Errorf("%s %s returned %d, want %d", tc.method, path, rec.Code, tc.want)
			}
		}
	}
	badURL := httptest.NewRequest("POST", "/api/mihomo/proxies/node/delay?url=file%3A%2F%2F%2Ftest", nil)
	if rec := serveDeck(deck, withSession(badURL, login, csrf)); rec.Code != http.StatusBadRequest {
		t.Errorf("invalid delay URL returned %d", rec.Code)
	}
	unauth := serveDeck(deck, httptest.NewRequest("GET", "/api/mihomo/version", nil))
	if unauth.Code != http.StatusUnauthorized || unauth.Header().Get("X-Deck-Auth-Error") != "1" {
		t.Errorf("Deck auth response %d %v", unauth.Code, unauth.Header())
	}
	upstream401 := serveDeck(deck, withSession(httptest.NewRequest("GET", "/api/mihomo/version", nil), login, csrf))
	if upstream401.Code != http.StatusUnauthorized || upstream401.Header().Get("X-Deck-Auth-Error") != "" {
		t.Errorf("Mihomo auth response %d %v", upstream401.Code, upstream401.Header())
	}
	if rec := serveDeck(deck, withSession(httptest.NewRequest("GET", "/api/mihomo/configs", nil), login, "")); rec.Code != http.StatusBadGateway {
		t.Errorf("malformed Mihomo configs returned %d", rec.Code)
	}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	req := httptest.NewRequest("POST", "/api/mihomo/proxies/node/delay", nil).WithContext(ctx)
	if rec := serveDeck(deck, withSession(req, login, csrf)); rec.Code != http.StatusGatewayTimeout {
		t.Errorf("upstream timeout returned %d", rec.Code)
	}
}

func TestAuthOriginJSONAndNoStore(t *testing.T) {
	deck, _ := newTestDeck(t, config.Config{}, nil)
	for _, path := range []string{"login", "refresh", "logout"} {
		req := jsonRequest("POST", "/api/auth/"+path, nil)
		req.Header.Set("Origin", "https://foreign.test")
		rec := serveDeck(deck, req)
		if rec.Code != http.StatusForbidden || rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("cross-site %s: %d %v", path, rec.Code, rec.Header())
		}
	}
	req := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader("{\"username\":\"admin\",\"password\":\"good-password\"}"))
	req.Header.Set("Content-Type", "text/plain")
	if rec := serveDeck(deck, req); rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("plain-text login returned %d", rec.Code)
	}

	req = jsonRequest("POST", "http://deck.test/api/auth/login", strings.NewReader("{\"username\":\"admin\",\"password\":\"good-password\"}"))
	req.Header.Set("Origin", "http://deck.test")
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	login := serveDeck(deck, req)
	csrf := loginCSRF(t, login)
	pw := httptest.NewRequest("POST", "/api/auth/password", strings.NewReader("{}"))
	if rec := serveDeck(deck, withSession(pw, login, csrf)); rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("non-JSON password change returned %d", rec.Code)
	}
	oversized := strings.Repeat("界", 25)
	if rec := loginToDeck(deck, "admin", oversized); rec.Code != http.StatusBadRequest {
		t.Errorf("oversized login password returned %d", rec.Code)
	}
	pw = jsonRequest("POST", "/api/auth/password", strings.NewReader(fmt.Sprintf("{\"currentPassword\":\"good-password\",\"newPassword\":%q}", oversized)))
	if rec := serveDeck(deck, withSession(pw, login, csrf)); rec.Code != http.StatusBadRequest {
		t.Errorf("oversized new password returned %d", rec.Code)
	}
	for _, path := range []string{"/api/auth/me", "/api/overview", "/api/preferences", "/api/telemetry/delays"} {
		rec := serveDeck(deck, httptest.NewRequest("GET", path, nil))
		if rec.Code != http.StatusUnauthorized || rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("X-Deck-Auth-Error") != "1" {
			t.Errorf("%s returned %d %v", path, rec.Code, rec.Header())
		}
	}
}

func TestDelayHistoryProviderFilterIncludesLegacyRows(t *testing.T) {
	deck, db := newTestDeck(t, config.Config{}, nil)
	login := loginToDeck(deck, "admin", "good-password")
	loginCSRF(t, login)
	if err := database.InsertDelays(db, []database.DelayRecord{
		{ProxyName: "node", ProviderName: "sub", TestedAt: 3},
		{ProxyName: "node", ProviderName: "other", TestedAt: 2},
		{ProxyName: "node", TestedAt: 1},
	}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/api/telemetry/delays?proxyName=node&providerName=sub", nil)
	rec := serveDeck(deck, withSession(req, login, ""))
	var records []database.DelayRecord
	if err := json.Unmarshal(rec.Body.Bytes(), &records); rec.Code != http.StatusOK || err != nil ||
		len(records) != 2 || records[0].ProviderName != "sub" || records[1].ProviderName != "" {
		t.Fatalf("filtered delay history returned %d %s, %v", rec.Code, rec.Body, err)
	}
}
