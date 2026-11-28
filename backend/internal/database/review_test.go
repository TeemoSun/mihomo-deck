package database

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func reviewDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestSyncAdminPreservesAndRotatesSessionsOnEnvChange(t *testing.T) {
	const username, password = "admin", "good-password"
	for _, next := range []struct {
		name, username, password string
		changed                  bool
	}{
		{name: "same credentials", username: username, password: password},
		{name: "changed password", username: username, password: "new-password", changed: true},
		{name: "changed username", username: "new-admin", password: password, changed: true},
	} {
		t.Run(next.name, func(t *testing.T) {
			db := reviewDB(t)
			if err := SyncAdmin(db, username, password); err != nil {
				t.Fatal(err)
			}
			before, err := GetUserByUsername(db, username)
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now()
			token := RefreshToken{ID: "refresh", SessionID: "refresh", UserID: before.ID, TokenHash: "hash", ExpiresAt: now.Add(time.Hour).Unix()}
			if err := CreateRefreshToken(db, token); err != nil {
				t.Fatal(err)
			}
			if err := SyncAdmin(db, next.username, next.password); err != nil {
				t.Fatal(err)
			}
			after, err := GetUserByID(db, before.ID)
			if err != nil {
				t.Fatal(err)
			}
			_, refreshErr := GetRefreshUser(db, token.TokenHash, now)
			if !next.changed {
				if after != before || refreshErr != nil {
					t.Fatalf("unchanged credentials affected the admin or refresh session: %v", refreshErr)
				}
				return
			}
			if after.Username != next.username || after.TokenVersion != before.TokenVersion+1 || after.PasswordHash == before.PasswordHash {
				t.Fatalf("credential update returned username %q and token version %d, want %q and %d with a new hash", after.Username, after.TokenVersion, next.username, before.TokenVersion+1)
			}
			if err := bcrypt.CompareHashAndPassword([]byte(after.PasswordHash), []byte(next.password)); err != nil {
				t.Fatalf("stored hash does not match configured password: %v", err)
			}
			if !errors.Is(refreshErr, sql.ErrNoRows) {
				t.Fatalf("revoked refresh lookup returned %v", refreshErr)
			}
		})
	}
}

func TestTrafficTotalsUseObservationHour(t *testing.T) {
	db := reviewDB(t)
	hour := time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
	now := hour.Add(2*time.Hour + 5*time.Minute)
	totals := []TrafficTotal{
		{Up: 10, Down: 20, ObservedAt: hour.Unix() + 3590},
		{Up: 30, Down: 40, ObservedAt: hour.Unix() + 3605},
		{Up: 5, Down: 8, ObservedAt: hour.Unix() + 3620},
		{Up: 10, Down: 13},
	}
	if err := RecordTrafficTotalsBatch(db, nil, totals, now); err != nil {
		t.Fatal(err)
	}
	for offset, want := range [][2]int64{{10, 20}, {25, 28}, {5, 5}} {
		var got [2]int64
		row := db.QueryRow("SELECT up_bytes,down_bytes FROM traffic_hourly WHERE hour_ts=?", hour.Unix()+int64(offset)*3600)
		if err := row.Scan(&got[0], &got[1]); err != nil || got != want {
			t.Fatalf("hour %d: got %v, want %v, error %v", offset, got, want, err)
		}
	}
	if err := PruneTelemetry(db, 1, hour.Add(24*time.Hour+30*time.Minute)); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow("SELECT count(*) FROM traffic_hourly").Scan(&count); err != nil || count != 3 {
		t.Fatalf("pruned partial retention hour: count %d, error %v", count, err)
	}
}

func TestHistorySearchTreatsWildcardsLiterally(t *testing.T) {
	db := reviewDB(t)
	for _, value := range []string{"prefix-%_-suffix", "prefix-ab-suffix", "prefix-\\-suffix"} {
		if err := InsertClosedConnections(db, []ClosedConnection{{ID: value, Host: value}}); err != nil {
			t.Fatal(err)
		}
		if err := InsertLogs(db, []LogEntry{{Payload: value}}); err != nil {
			t.Fatal(err)
		}
	}
	for q, want := range map[string]string{"%": "prefix-%_-suffix", "_": "prefix-%_-suffix", "\\": "prefix-\\-suffix"} {
		ctx := context.Background()
		conns, err := QueryConnections(ctx, db, ConnectionFilter{Q: q})
		if err != nil || conns.Total != 1 || len(conns.Items) != 1 || conns.Items[0].Host != want {
			t.Fatalf("connections search %q: %+v, error %v", q, conns, err)
		}
		logs, err := QueryLogs(ctx, db, LogFilter{Q: q})
		if err != nil || logs.Total != 1 || len(logs.Items) != 1 || logs.Items[0].Payload != want {
			t.Fatalf("logs search %q: %+v, error %v", q, logs, err)
		}
	}
}

func TestMigrationPreservesLegacyRowsAndReopenedPragmas(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "test ? file.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(
		"CREATE TABLE traffic_hourly (hour_ts INTEGER PRIMARY KEY, avg_mem INTEGER, mem_count INTEGER);" +
			"INSERT INTO traffic_hourly VALUES(3600,7,3);" +
			"CREATE TABLE delay_history (id INTEGER PRIMARY KEY, proxy_name TEXT, group_name TEXT, test_url TEXT, delay_ms INTEGER, ok INTEGER, source TEXT, tested_at INTEGER);" +
			"INSERT INTO delay_history VALUES(1,'node','','',25,1,'manual',3600);" +
			"CREATE TABLE refresh_tokens (id TEXT PRIMARY KEY, user_id INTEGER, token_hash TEXT, user_agent TEXT, ip TEXT, expires_at INTEGER, revoked_at INTEGER, created_at INTEGER);" +
			"INSERT INTO refresh_tokens VALUES('old',1,'hash','','',3600,NULL,0);",
	)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := Migrate(db); err != nil {
			t.Fatal(err)
		}
	}
	var sum int
	if err := db.QueryRow("SELECT mem_sum FROM traffic_hourly").Scan(&sum); err != nil || sum != 21 {
		t.Fatalf("mem_sum %d, error %v", sum, err)
	}
	var session string
	if err := db.QueryRow("SELECT session_id FROM refresh_tokens").Scan(&session); err != nil || session != "" {
		t.Fatalf("legacy session %q, error %v", session, err)
	}
	delays, err := QueryDelays(db, "node", "subscription", time.Unix(0, 0), 10)
	if err != nil || len(delays) != 1 || delays[0].ProviderName != "" {
		t.Fatalf("legacy delay %+v, error %v", delays, err)
	}
	db.SetMaxIdleConns(0)
	for pragma, expected := range map[string]string{"journal_mode": "wal", "synchronous": "1", "foreign_keys": "1", "busy_timeout": "5000"} {
		var got string
		if err := db.QueryRow("PRAGMA " + pragma).Scan(&got); err != nil || got != expected {
			t.Errorf("reopened %s: got %s, want %s, error %v", pragma, got, expected, err)
		}
	}
}

func TestStoredNullPreferencesNormalize(t *testing.T) {
	db := reviewDB(t)
	if _, err := db.Exec("INSERT INTO user_preferences VALUES('preferences',?,0)", "{\"pinnedProxies\":null,\"hiddenGroups\":null}"); err != nil {
		t.Fatal(err)
	}
	p, err := GetPreferences(db, DefaultPreferences(30, 30, "info"))
	if err != nil || p.PinnedProxies == nil || p.HiddenGroups == nil {
		t.Fatalf("legacy preferences %+v, error %v", p, err)
	}
	p.PinnedProxies, p.HiddenGroups = nil, nil
	if err := UpdatePreferences(db, p); err != nil {
		t.Fatal(err)
	}
	var stored string
	err = db.QueryRow("SELECT value_json FROM user_preferences WHERE key='preferences'").Scan(&stored)
	if err != nil || !strings.Contains(stored, "\"pinnedProxies\":[]") || !strings.Contains(stored, "\"hiddenGroups\":[]") {
		t.Fatalf("stored preferences %q, error %v", stored, err)
	}
}
