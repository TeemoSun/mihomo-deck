package collector

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/TeemoSun/mihomo-deck/internal/database"
	"github.com/TeemoSun/mihomo-deck/internal/mihomo"
)

func TestCollectionResetConnectionsAndLogLevel(t *testing.T) {
	db, err := database.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	c := New(db, nil, database.DefaultPreferences(30, 30, "warning"))
	start := time.Now().Add(-time.Minute).Format(time.RFC3339Nano)
	send := func(path string, v any) {
		b, e := json.Marshal(v)
		if e != nil {
			t.Fatal(e)
		}
		c.consume(path, b)
	}
	send("connections", ConnectionsSnapshot{UploadTotal: 100, DownloadTotal: 200, Connections: []Connection{{ID: "one", Start: start, Metadata: Metadata{Host: "example.test"}, Chains: []string{"proxy"}}}})
	send("connections", ConnectionsSnapshot{UploadTotal: 5, DownloadTotal: 10})
	send("logs", map[string]string{"type": "info", "payload": "ignored"})
	send("logs", map[string]string{"type": "error", "payload": "saved"})
	c.flush()
	summary, err := database.GetTrafficSummary(db, time.Now())
	if err != nil || summary.CumulativeUp != 105 || summary.CumulativeDown != 210 {
		t.Fatalf("summary %+v: %v", summary, err)
	}
	connections, err := database.QueryConnections(db, database.ConnectionFilter{})
	if err != nil || len(connections.Items) != 1 || connections.Items[0].PrimaryProxy != "proxy" || connections.Items[0].DurationMS < 59000 {
		t.Fatalf("connections %+v: %v", connections, err)
	}
	logs, err := database.QueryLogs(db, database.LogFilter{})
	if err != nil || len(logs.Items) != 1 || logs.Items[0].Payload != "saved" {
		t.Fatalf("logs %+v: %v", logs, err)
	}
}

func TestNewPreloadsLatestTrafficHistory(t *testing.T) {
	db, err := database.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	samples := make([]database.TrafficSample, 70)
	for i := range samples {
		samples[i] = database.TrafficSample{TS: now.Add(-time.Duration(70-i) * time.Second).Unix(), UpBPS: int64(i)}
	}
	if err = database.RecordTrafficBatch(db, samples, 0, 0, false, now); err != nil {
		t.Fatal(err)
	}
	c := New(db, nil, database.DefaultPreferences(30, 30, "info"))
	history := c.Snapshot().TrafficHistory
	if len(history) != 60 {
		t.Fatalf("history len %d", len(history))
	}
	if history[0].UpBPS != 10 || history[len(history)-1].UpBPS != 69 {
		t.Fatalf("unexpected range: first=%+v last=%+v", history[0], history[len(history)-1])
	}
}

func TestSchedulerCapturePersistsProviderAndScheduledDelay(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/version":
			_, _ = w.Write([]byte(`{"version":"test-version"}`))
		case "/providers/proxies":
			_, _ = w.Write([]byte(`{"providers":{"provider-a":{"subscriptionInfo":{"Upload":11,"Download":22,"Total":33,"Expire":44},"proxies":[{"alive":true},{"alive":false}]}}}`))
		case "/proxies":
			_, _ = w.Write([]byte(`{"proxies":{"group-a":{"type":"Selector","now":"node-a"}}}`))
		case "/proxies/node-a/delay":
			_, _ = w.Write([]byte(`{"delay":123}`))
		default:
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
	client := mihomo.NewClient(up.URL, "")
	c := New(db, client, database.DefaultPreferences(30, 30, "info"))
	c.capture(context.Background())
	providers, err := database.QueryProviderSnapshots(db, "provider-a", time.Unix(0, 0), 10)
	if err != nil || len(providers) != 1 {
		t.Fatalf("providers %+v %v", providers, err)
	}
	if providers[0].UploadBytes != 11 || providers[0].AliveCount != 1 || providers[0].NodeCount != 2 {
		t.Fatalf("provider snapshot %+v", providers[0])
	}
	delays, err := database.QueryDelays(db, "node-a", time.Unix(0, 0), 10)
	if err != nil || len(delays) != 1 {
		t.Fatalf("delays %+v %v", delays, err)
	}
	if delays[0].DelayMS != 123 || delays[0].Source != "scheduled" || delays[0].GroupName != "group-a" || !delays[0].OK {
		t.Fatalf("scheduled delay %+v", delays[0])
	}
	if c.Snapshot().Status.Version != "test-version" {
		t.Fatalf("version %q", c.Snapshot().Status.Version)
	}
}

func TestPruneTelemetryKeepsRecentRecords(t *testing.T) {
	db, err := database.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Truncate(time.Second)
	old := now.Add(-31 * 24 * time.Hour).Unix()
	recent := now.Add(-29 * 24 * time.Hour).Unix()
	for _, ts := range []int64{old, recent} {
		if _, err = db.Exec("INSERT INTO traffic_samples VALUES(?,?,0,0,0,0)", ts, ts); err != nil {
			t.Fatal(err)
		}
		if err = database.InsertDelays(db, []database.DelayRecord{{ProxyName: "node", TestedAt: ts}}); err != nil {
			t.Fatal(err)
		}
		if err = database.InsertProviderSnapshots(db, []database.ProviderSnapshot{{ProviderName: "provider", CapturedAt: ts}}); err != nil {
			t.Fatal(err)
		}
	}
	if err = database.PruneTelemetry(db, 30, now); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"traffic_samples", "delay_history", "provider_snapshots"} {
		var n int
		if err = db.QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("%s kept %d rows", table, n)
		}
	}
}
