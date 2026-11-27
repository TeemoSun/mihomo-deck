package database

import (
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
	summary, err := GetTrafficSummary(db, now)
	if err != nil {
		t.Fatal(err)
	}
	if summary.CumulativeUp != 105 || summary.CumulativeDown != 230 {
		t.Fatalf("summary: %+v", summary)
	}
}
