package database

import "database/sql"

func Migrate(db *sql.DB) error {
	const schema = `
CREATE TABLE IF NOT EXISTS users (id INTEGER PRIMARY KEY, username TEXT NOT NULL UNIQUE, password_hash TEXT NOT NULL, token_version INTEGER NOT NULL DEFAULT 0, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS refresh_tokens (id TEXT PRIMARY KEY, user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE, token_hash TEXT NOT NULL UNIQUE, user_agent TEXT NOT NULL, ip TEXT NOT NULL, expires_at INTEGER NOT NULL, revoked_at INTEGER, created_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS user_preferences (key TEXT PRIMARY KEY, value_json TEXT NOT NULL, updated_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS traffic_cursor (id INTEGER PRIMARY KEY CHECK(id=1), last_up_total INTEGER NOT NULL, last_down_total INTEGER NOT NULL, cumulative_up INTEGER NOT NULL, cumulative_down INTEGER NOT NULL, updated_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS traffic_samples (ts INTEGER PRIMARY KEY, up_bps INTEGER NOT NULL, down_bps INTEGER NOT NULL, mem_inuse INTEGER NOT NULL, mem_oslimit INTEGER NOT NULL, active_conns INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS traffic_hourly (hour_ts INTEGER PRIMARY KEY, up_bytes INTEGER NOT NULL DEFAULT 0, down_bytes INTEGER NOT NULL DEFAULT 0, peak_up_bps INTEGER NOT NULL DEFAULT 0, peak_down_bps INTEGER NOT NULL DEFAULT 0, avg_mem INTEGER NOT NULL DEFAULT 0, max_conns INTEGER NOT NULL DEFAULT 0);
CREATE TABLE IF NOT EXISTS connection_history (id TEXT PRIMARY KEY, host TEXT, dst_ip TEXT, dst_port TEXT, src_ip TEXT, src_port TEXT, network TEXT, conn_type TEXT, inbound_name TEXT, rule TEXT, rule_payload TEXT, chain_json TEXT, primary_proxy TEXT, provider_name TEXT, upload_bytes INTEGER, download_bytes INTEGER, start_at INTEGER, closed_at INTEGER, duration_ms INTEGER);
CREATE INDEX IF NOT EXISTS idx_conn_closed_at ON connection_history(closed_at); CREATE INDEX IF NOT EXISTS idx_conn_host ON connection_history(host); CREATE INDEX IF NOT EXISTS idx_conn_proxy ON connection_history(primary_proxy); CREATE INDEX IF NOT EXISTS idx_conn_src_ip ON connection_history(src_ip);
CREATE TABLE IF NOT EXISTS log_entries (id INTEGER PRIMARY KEY AUTOINCREMENT, ts INTEGER NOT NULL, level TEXT NOT NULL, payload TEXT NOT NULL); CREATE INDEX IF NOT EXISTS idx_logs_ts ON log_entries(ts); CREATE INDEX IF NOT EXISTS idx_logs_level ON log_entries(level);
CREATE TABLE IF NOT EXISTS delay_history (id INTEGER PRIMARY KEY AUTOINCREMENT, proxy_name TEXT, group_name TEXT, test_url TEXT, delay_ms INTEGER, ok INTEGER, source TEXT, tested_at INTEGER); CREATE INDEX IF NOT EXISTS idx_delay_proxy_time ON delay_history(proxy_name,tested_at);
CREATE TABLE IF NOT EXISTS provider_snapshots (id INTEGER PRIMARY KEY AUTOINCREMENT, provider_name TEXT, upload_bytes INTEGER, download_bytes INTEGER, total_bytes INTEGER, expire_ts INTEGER, node_count INTEGER, alive_count INTEGER, captured_at INTEGER);
`
	if _, err := db.Exec(schema); err != nil {
		return err
	}
	rows, err := db.Query("PRAGMA table_info(traffic_hourly)")
	if err != nil {
		return err
	}
	found := false
	foundSum := false
	for rows.Next() {
		var cid, notnull, pk int
		var name, typ string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			rows.Close()
			return err
		}
		if name == "mem_count" {
			found = true
		}
		if name == "mem_sum" {
			foundSum = true
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if !found {
		_, err = db.Exec("ALTER TABLE traffic_hourly ADD COLUMN mem_count INTEGER NOT NULL DEFAULT 0")
	}
	if err != nil {
		return err
	}
	if !foundSum {
		_, err = db.Exec("ALTER TABLE traffic_hourly ADD COLUMN mem_sum INTEGER NOT NULL DEFAULT 0")
		if err != nil {
			return err
		}
		_, err = db.Exec("UPDATE traffic_hourly SET mem_sum=avg_mem*mem_count")
	}
	return err
}
