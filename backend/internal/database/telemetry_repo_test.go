package database

import (
	"context"
	"testing"
	"time"
)

func TestTrafficResetAndAverage(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = Migrate(db); err != nil {
		t.Fatal(err)
	}
	if err = Migrate(db); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	samples := []TrafficSample{{TS: now.Unix(), MemInuse: 1}, {TS: now.Unix() + 1, MemInuse: 2}, {TS: now.Unix() + 2, MemInuse: 3}}
	if err = RecordTrafficBatch(db, samples, 100, 200, true, now); err != nil {
		t.Fatal(err)
	}
	if err = RecordTrafficBatch(db, nil, 5, 230, true, now); err != nil {
		t.Fatal(err)
	}
	var up, down, avg int64
	if err = db.QueryRow("SELECT up_bytes,down_bytes,avg_mem FROM traffic_hourly").Scan(&up, &down, &avg); err != nil {
		t.Fatal(err)
	}
	if up != 105 || down != 230 || avg != 2 {
		t.Fatalf("got %d %d %d", up, down, avg)
	}
	summary, err := GetTrafficSummary(context.Background(), db, now)
	if err != nil {
		t.Fatal(err)
	}
	if summary.CumulativeUp != 105 || summary.CumulativeDown != 230 {
		t.Fatalf("summary: %+v", summary)
	}
}

func TestTrafficTotalsBatchAndRollback(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = Migrate(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	totals := []TrafficTotal{{Up: 100, Down: 200}, {Up: 150, Down: 250}, {Up: 5, Down: 20}, {Up: 10, Down: 30}}
	if err = RecordTrafficTotalsBatch(db, []TrafficSample{{TS: now.Unix(), UpBPS: 9}}, totals, now); err != nil {
		t.Fatal(err)
	}
	var up, down, lastUp, lastDown int64
	if err = db.QueryRow("SELECT cumulative_up,cumulative_down,last_up_total,last_down_total FROM traffic_cursor").Scan(&up, &down, &lastUp, &lastDown); err != nil {
		t.Fatal(err)
	}
	if up != 160 || down != 280 || lastUp != 10 || lastDown != 30 {
		t.Fatalf("cursor: %d %d %d %d", up, down, lastUp, lastDown)
	}
	if _, err = db.Exec(`CREATE TRIGGER reject_cursor BEFORE UPDATE ON traffic_cursor BEGIN SELECT RAISE(ABORT, 'rejected'); END`); err != nil {
		t.Fatal(err)
	}
	if err = RecordTrafficTotalsBatch(db, []TrafficSample{{TS: now.Unix() + 1}}, []TrafficTotal{{Up: 20, Down: 40}}, now); err == nil {
		t.Fatal("expected failure")
	}
	var count int
	if err = db.QueryRow("SELECT count(*) FROM traffic_samples").Scan(&count); err != nil || count != 1 {
		t.Fatalf("samples after rollback: %d, %v", count, err)
	}
}

func TestQueryConnectionsSearchesRule(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = Migrate(db); err != nil {
		t.Fatal(err)
	}
	if err = InsertClosedConnections(db, []ClosedConnection{{ID: "a", Rule: "DOMAIN-SUFFIX"}, {ID: "b", Rule: "MATCH"}}); err != nil {
		t.Fatal(err)
	}
	result, err := QueryConnections(context.Background(), db, ConnectionFilter{Q: "SUFFIX"})
	if err != nil || result.Total != 1 || len(result.Items) != 1 || result.Items[0].ID != "a" {
		t.Fatalf("result %+v: %v", result, err)
	}
}
