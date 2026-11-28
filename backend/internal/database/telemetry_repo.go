package database

import (
	"database/sql"
	"time"
)

type TrafficSample struct {
	TS          int64 `json:"ts"`
	UpBPS       int64 `json:"upBps"`
	DownBPS     int64 `json:"downBps"`
	MemInuse    int64 `json:"memInuse"`
	MemOslimit  int64 `json:"memOslimit"`
	ActiveConns int64 `json:"activeConns"`
}

type TrafficTotal struct {
	Up, Down   int64
	ObservedAt int64
}

func RecordTrafficBatch(db *sql.DB, samples []TrafficSample, up, down int64, hasTotals bool, now time.Time) error {
	var totals []TrafficTotal
	if hasTotals {
		totals = append(totals, TrafficTotal{Up: up, Down: down})
	}
	return RecordTrafficTotalsBatch(db, samples, totals, now)
}

func RecordTrafficTotalsBatch(db *sql.DB, samples []TrafficSample, totals []TrafficTotal, now time.Time) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, s := range samples {
		var oldMem int64
		oldErr := tx.QueryRow("SELECT mem_inuse FROM traffic_samples WHERE ts=?", s.TS).Scan(&oldMem)
		if oldErr != nil && oldErr != sql.ErrNoRows {
			return oldErr
		}
		if _, err = tx.Exec("INSERT OR REPLACE INTO traffic_samples VALUES(?,?,?,?,?,?)", s.TS, s.UpBPS, s.DownBPS, s.MemInuse, s.MemOslimit, s.ActiveConns); err != nil {
			return err
		}
		hour := s.TS - s.TS%3600
		if oldErr == sql.ErrNoRows {
			_, err = tx.Exec(`INSERT INTO traffic_hourly(hour_ts,peak_up_bps,peak_down_bps,avg_mem,max_conns,mem_count,mem_sum) VALUES(?,?,?,?,?,1,?) ON CONFLICT(hour_ts) DO UPDATE SET peak_up_bps=max(peak_up_bps,excluded.peak_up_bps),peak_down_bps=max(peak_down_bps,excluded.peak_down_bps),avg_mem=(mem_sum+excluded.mem_sum)/(mem_count+1),mem_sum=mem_sum+excluded.mem_sum,mem_count=mem_count+1,max_conns=max(max_conns,excluded.max_conns)`, hour, s.UpBPS, s.DownBPS, s.MemInuse, s.ActiveConns, s.MemInuse)
		} else {
			_, err = tx.Exec(`UPDATE traffic_hourly SET peak_up_bps=max(peak_up_bps,?),peak_down_bps=max(peak_down_bps,?),avg_mem=CASE WHEN mem_count>0 THEN (mem_sum+?-?)/mem_count ELSE ? END,mem_sum=mem_sum+?-?,max_conns=max(max_conns,?) WHERE hour_ts=?`, s.UpBPS, s.DownBPS, s.MemInuse, oldMem, s.MemInuse, s.MemInuse, oldMem, s.ActiveConns, hour)
		}
		if err != nil {
			return err
		}
	}
	for _, total := range totals {
		up, down := total.Up, total.Down
		var lastUp, lastDown, cumUp, cumDown int64
		err = tx.QueryRow("SELECT last_up_total,last_down_total,cumulative_up,cumulative_down FROM traffic_cursor WHERE id=1").Scan(&lastUp, &lastDown, &cumUp, &cumDown)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		du, dd := up, down
		if err == nil {
			if up >= lastUp {
				du = up - lastUp
			}
			if down >= lastDown {
				dd = down - lastDown
			}
		}
		ts := total.ObservedAt
		if ts == 0 {
			ts = now.Unix()
		}
		if _, err = tx.Exec(`INSERT INTO traffic_cursor VALUES(1,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET last_up_total=excluded.last_up_total,last_down_total=excluded.last_down_total,cumulative_up=excluded.cumulative_up,cumulative_down=excluded.cumulative_down,updated_at=excluded.updated_at`, up, down, cumUp+du, cumDown+dd, ts); err != nil {
			return err
		}
		hour := ts - ts%3600
		if _, err = tx.Exec(`INSERT INTO traffic_hourly(hour_ts,up_bytes,down_bytes) VALUES(?,?,?) ON CONFLICT(hour_ts) DO UPDATE SET up_bytes=up_bytes+excluded.up_bytes,down_bytes=down_bytes+excluded.down_bytes`, hour, du, dd); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func PruneTelemetry(db *sql.DB, days int, now time.Time) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	cutoff := now.Unix() - int64(days)*86400
	if _, err = tx.Exec("DELETE FROM traffic_hourly WHERE hour_ts<?", cutoff-cutoff%3600); err != nil {
		return err
	}
	for _, q := range []string{"DELETE FROM traffic_samples WHERE ts<?", "DELETE FROM connection_history WHERE closed_at<?", "DELETE FROM log_entries WHERE ts<?", "DELETE FROM delay_history WHERE tested_at<?", "DELETE FROM provider_snapshots WHERE captured_at<?"} {
		if _, err = tx.Exec(q, cutoff); err != nil {
			return err
		}
	}
	return tx.Commit()
}
