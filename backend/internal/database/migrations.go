package database

import "database/sql"

func Migrate(db *sql.DB) error {
	const schema = `
CREATE TABLE IF NOT EXISTS users (id INTEGER PRIMARY KEY, username TEXT NOT NULL UNIQUE, password_hash TEXT NOT NULL, token_version INTEGER NOT NULL DEFAULT 0, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS refresh_tokens (id TEXT PRIMARY KEY, user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE, token_hash TEXT NOT NULL UNIQUE, user_agent TEXT NOT NULL, ip TEXT NOT NULL, expires_at INTEGER NOT NULL, revoked_at INTEGER, created_at INTEGER NOT NULL, session_id TEXT NOT NULL DEFAULT '');
CREATE TABLE IF NOT EXISTS user_preferences (key TEXT PRIMARY KEY, value_json TEXT NOT NULL, updated_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS traffic_cursor (id INTEGER PRIMARY KEY CHECK(id=1), last_up_total INTEGER NOT NULL, last_down_total INTEGER NOT NULL, cumulative_up INTEGER NOT NULL, cumulative_down INTEGER NOT NULL, updated_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS traffic_samples (ts INTEGER PRIMARY KEY, up_bps INTEGER NOT NULL, down_bps INTEGER NOT NULL, mem_inuse INTEGER NOT NULL, mem_oslimit INTEGER NOT NULL, active_conns INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS traffic_hourly (hour_ts INTEGER PRIMARY KEY, up_bytes INTEGER NOT NULL DEFAULT 0, down_bytes INTEGER NOT NULL DEFAULT 0, peak_up_bps INTEGER NOT NULL DEFAULT 0, peak_down_bps INTEGER NOT NULL DEFAULT 0, avg_mem INTEGER NOT NULL DEFAULT 0, max_conns INTEGER NOT NULL DEFAULT 0);
CREATE TABLE IF NOT EXISTS connection_history (id TEXT PRIMARY KEY, host TEXT, dst_ip TEXT, dst_port TEXT, src_ip TEXT, src_port TEXT, network TEXT, conn_type TEXT, inbound_name TEXT, rule TEXT, rule_payload TEXT, chain_json TEXT, primary_proxy TEXT, provider_name TEXT, upload_bytes INTEGER, download_bytes INTEGER, start_at INTEGER, closed_at INTEGER, duration_ms INTEGER);
CREATE INDEX IF NOT EXISTS idx_conn_closed_at ON connection_history(closed_at); CREATE INDEX IF NOT EXISTS idx_conn_host ON connection_history(host); CREATE INDEX IF NOT EXISTS idx_conn_proxy ON connection_history(primary_proxy); CREATE INDEX IF NOT EXISTS idx_conn_src_ip ON connection_history(src_ip);
CREATE TABLE IF NOT EXISTS log_entries (id INTEGER PRIMARY KEY AUTOINCREMENT, ts INTEGER NOT NULL, level TEXT NOT NULL, payload TEXT NOT NULL); CREATE INDEX IF NOT EXISTS idx_logs_ts ON log_entries(ts); CREATE INDEX IF NOT EXISTS idx_logs_level ON log_entries(level);
CREATE TABLE IF NOT EXISTS delay_history (id INTEGER PRIMARY KEY AUTOINCREMENT, proxy_name TEXT, group_name TEXT, test_url TEXT, delay_ms INTEGER, ok INTEGER, source TEXT, tested_at INTEGER, provider_name TEXT NOT NULL DEFAULT ''); CREATE INDEX IF NOT EXISTS idx_delay_proxy_time ON delay_history(proxy_name,tested_at);
CREATE TABLE IF NOT EXISTS provider_snapshots (id INTEGER PRIMARY KEY AUTOINCREMENT, provider_name TEXT, upload_bytes INTEGER, download_bytes INTEGER, total_bytes INTEGER, expire_ts INTEGER, node_count INTEGER, alive_count INTEGER, captured_at INTEGER);
`
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(schema); err != nil {
		return err
	}
	for _, column := range []struct {
		table, name, definition, backfill string
	}{
		{"traffic_hourly", "mem_count", "INTEGER NOT NULL DEFAULT 0", ""},
		{"traffic_hourly", "mem_sum", "INTEGER NOT NULL DEFAULT 0", "UPDATE traffic_hourly SET mem_sum=avg_mem*mem_count"},
		{"refresh_tokens", "session_id", "TEXT NOT NULL DEFAULT ''", ""},
		{"delay_history", "provider_name", "TEXT NOT NULL DEFAULT ''", ""},
	} {
		var exists bool
		if err := tx.QueryRow("SELECT EXISTS (SELECT 1 FROM pragma_table_info(?) WHERE name=?)", column.table, column.name).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		if _, err := tx.Exec("ALTER TABLE " + column.table + " ADD COLUMN " + column.name + " " + column.definition); err != nil {
			return err
		}
		if column.backfill != "" {
			if _, err := tx.Exec(column.backfill); err != nil {
				return err
			}
		}
	}
	const indexes = `
CREATE INDEX IF NOT EXISTS idx_delay_tested_at ON delay_history(tested_at);
CREATE INDEX IF NOT EXISTS idx_provider_snap_name_time ON provider_snapshots(provider_name,captured_at);
CREATE INDEX IF NOT EXISTS idx_refresh_session ON refresh_tokens(session_id) WHERE revoked_at IS NULL;
`
	if _, err := tx.Exec(indexes); err != nil {
		return err
	}
	return tx.Commit()
}
