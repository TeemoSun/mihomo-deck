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
	send("connections", ConnectionsSnapshot{UploadTotal: 5, DownloadTotal: 10, Connections: []Connection{}})
	send("logs", map[string]string{"type": "info", "payload": "ignored"})
	send("logs", map[string]string{"type": "error", "payload": "saved"})
	c.flush()
	summary, err := database.GetTrafficSummary(context.Background(), db, time.Now())
	if err != nil || summary.CumulativeUp != 105 || summary.CumulativeDown != 210 {
		t.Fatalf("summary %+v: %v", summary, err)
	}
	connections, err := database.QueryConnections(context.Background(), db, database.ConnectionFilter{})
	if err != nil || len(connections.Items) != 1 || connections.Items[0].PrimaryProxy != "proxy" || connections.Items[0].DurationMS < 59000 {
		t.Fatalf("connections %+v: %v", connections, err)
	}
	logs, err := database.QueryLogs(context.Background(), db, database.LogFilter{})
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

func TestUpsertLiveSample(t *testing.T) {
	const windowEnd int64 = 1000
	const replacedTS int64 = 980
	var history []database.TrafficSample
	for ts := windowEnd - 61; ts < windowEnd; ts++ {
		if ts != replacedTS {
			history = append(history, database.TrafficSample{TS: ts})
		}
	}
	history = upsertLiveSample(history, database.TrafficSample{TS: windowEnd}, windowEnd)
	history = upsertLiveSample(history, database.TrafficSample{TS: replacedTS, UpBPS: 10}, windowEnd)
	replacement := database.TrafficSample{TS: replacedTS, DownBPS: 20}
	history = upsertLiveSample(history, replacement, windowEnd)
	if len(history) != 60 {
		t.Fatalf("live history length = %d, want 60", len(history))
	}
	for i, sample := range history {
		if want := windowEnd - 59 + int64(i); sample.TS != want {
			t.Fatalf("sample %d timestamp = %d, want %d", i, sample.TS, want)
		}
		if sample.TS == replacedTS && sample != replacement {
			t.Fatalf("replacement = %+v, want %+v", sample, replacement)
		}
	}
	// A clock step backwards can leave more than 60 timestamps above the cutoff.
	history = upsertLiveSample(history, database.TrafficSample{TS: windowEnd - 60}, windowEnd-1)
	if len(history) != 60 || history[0].TS != windowEnd-59 {
		t.Fatalf("capped history = %+v", history)
	}
}

func TestConsumeTrafficUpdatesLiveHistoryBeforeFlush(t *testing.T) {
	db, err := database.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	c := New(db, nil, database.DefaultPreferences(30, 30, "info"))
	now := time.Now().Unix()
	recent := database.TrafficSample{TS: now - 5, UpBPS: 7}
	c.state.LiveTrafficHistory = []database.TrafficSample{{TS: now - 60}, recent}
	c.consume("memory", json.RawMessage(`{"inuse":100,"oslimit":200}`))
	c.consume("connections", json.RawMessage(`{"uploadTotal":0,"downloadTotal":0,"connections":[{"id":"active"}]}`))
	events, unsubscribe := c.Subscribe()
	defer unsubscribe()
	c.consume("traffic", json.RawMessage(`{"up":12,"down":34}`))

	snapshot := c.Snapshot()
	want := database.TrafficSample{TS: snapshot.Status.LastSeenAt, UpBPS: 12, DownBPS: 34, MemInuse: 100, MemOslimit: 200, ActiveConns: 1}
	if history := snapshot.LiveTrafficHistory; len(history) != 2 || history[0] != recent || history[1] != want {
		t.Fatalf("live history = %+v, want %+v then %+v", history, recent, want)
	}
	select {
	case event := <-events:
		if event.Type != "traffic" || event.Data != (TrafficBroadcast{Up: 12, Down: 34, TS: want.TS, Sample: want}) {
			t.Fatalf("traffic event = %+v", event)
		}
	default:
		t.Fatal("traffic event missing")
	}
	snapshot.LiveTrafficHistory[1].UpBPS = 0
	if got := c.Snapshot().LiveTrafficHistory[1]; got != want {
		t.Fatalf("snapshot mutated collector sample: %+v", got)
	}
	if len(snapshot.TrafficHistory) != 0 {
		t.Fatal("traffic consume updated persisted history")
	}
	c.flush()
	if len(c.Snapshot().TrafficHistory) != 0 {
		t.Fatal("disconnected flush updated persisted history")
	}
	c.setStatus("traffic", true, nil)
	c.flush()
	if history := c.Snapshot().TrafficHistory; len(history) != 1 || history[0].UpBPS != want.UpBPS {
		t.Fatalf("persisted history after connected flush = %+v", history)
	}
}

func TestSchedulerCapturePersistsProviderAndScheduledDelay(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/version":
			_, _ = w.Write([]byte(`{"version":"test-version"}`))
		case "/providers/proxies":
			_, _ = w.Write([]byte(`{"providers":{"default":{"vehicleType":"Compatible","proxies":[{"name":"node-a"}]},"provider-a":{"vehicleType":"HTTP","subscriptionInfo":{"Upload":11,"Download":22,"Total":33,"Expire":44},"proxies":[{"name":"node-a","provider-name":"actual-sub","alive":true},{"alive":false}]}}}`))
		case "/proxies":
			_, _ = w.Write([]byte(`{"proxies":{"group-a":{"type":"Selector","now":"node-a"}}}`))
		case "/providers/proxies/actual-sub/node-a/healthcheck":
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
	events, unsubscribe := c.Subscribe()
	defer unsubscribe()
	c.capture(context.Background())
	select {
	case event := <-events:
		status, ok := event.Data.(Status)
		if event.Type != "status" || !ok || status.Version != "test-version" {
			t.Fatalf("event: %+v", event)
		}
	default:
		t.Fatal("missing status event")
	}
	providers, err := database.QueryProviderSnapshots(db, "", time.Unix(0, 0), 10)
	if err != nil || len(providers) != 1 {
		t.Fatalf("providers %+v %v", providers, err)
	}
	if providers[0].ProviderName != "provider-a" || providers[0].UploadBytes != 11 || providers[0].AliveCount != 1 || providers[0].NodeCount != 2 {
		t.Fatalf("provider snapshot %+v", providers[0])
	}
	delays, err := database.QueryDelays(db, "node-a", "", time.Unix(0, 0), 10)
	if err != nil || len(delays) != 1 {
		t.Fatalf("delays %+v %v", delays, err)
	}
	if delays[0].ProviderName != "actual-sub" || delays[0].DelayMS != 123 || delays[0].Source != "scheduled" || delays[0].GroupName != "group-a" || !delays[0].OK {
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

func TestFlushDisconnectedAndRetry(t *testing.T) {
	db, err := database.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	c := New(db, nil, database.DefaultPreferences(30, 30, "info"))
	c.totals = []database.TrafficTotal{{Up: 12}}
	c.logs = []database.LogEntry{{TS: time.Now().Unix(), Level: "error", Payload: "retry"}}
	c.closed = []database.ClosedConnection{{ID: "retry"}}
	if _, err = db.Exec(`CREATE TRIGGER reject_total BEFORE INSERT ON traffic_cursor BEGIN SELECT RAISE(ABORT, 'rejected'); END`); err != nil {
		t.Fatal(err)
	}
	c.flush()
	if len(c.totals) != 1 || len(c.Snapshot().TrafficHistory) != 0 {
		t.Fatal("failed totals not retained or disconnected history recorded")
	}
	if _, err = db.Exec("DROP TRIGGER reject_total"); err != nil {
		t.Fatal(err)
	}
	c.flush()
	summary, err := database.GetTrafficSummary(context.Background(), db, time.Now())
	if err != nil || summary.CumulativeUp != 12 || len(c.totals) != 0 {
		t.Fatalf("summary %+v: %v", summary, err)
	}
	var n int
	if err = db.QueryRow("SELECT count(*) FROM traffic_samples").Scan(&n); err != nil || n != 0 {
		t.Fatalf("disconnected samples %d: %v", n, err)
	}
	c.setStatus("traffic", true, nil)
	c.flush()
	if len(c.Snapshot().TrafficHistory) != 1 {
		t.Fatal("connected sample missing")
	}
}

func TestInvalidConnectionsFramesKeepLastValidSnapshot(t *testing.T) {
	c := New(nil, nil, database.DefaultPreferences(30, 30, "info"))
	c.consume("connections", json.RawMessage(`{"downloadTotal":20,"uploadTotal":10,"connections":[{"id":"active"}]}`))
	for _, frame := range []string{
		`null`,
		`{"downloadTotal":null,"uploadTotal":0,"connections":[]}`,
		`{"downloadTotal":0,"connections":[]}`,
		`{"downloadTotal":-1,"uploadTotal":0,"connections":[]}`,
		`{"downloadTotal":0,"uploadTotal":-1,"connections":[]}`,
		`{"downloadTotal":0,"uploadTotal":0}`,
		`{"downloadTotal":0,"uploadTotal":0,"connections":{}}`,
		`{"downloadTotal":0,"uploadTotal":0,"connections":"bad"}`,
	} {
		c.consume("connections", json.RawMessage(frame))
		if len(c.active) != 1 || c.active["active"].ID != "active" || len(c.totals) != 1 || len(c.closed) != 0 {
			t.Fatalf("invalid frame mutated state: %s", frame)
		}
	}
	if total := c.totals[0]; total.Up != 10 || total.Down != 20 || total.ObservedAt == 0 {
		t.Fatalf("valid frame totals %+v", total)
	}
}

func TestNullConnectionsFrameClosesActiveConnection(t *testing.T) {
	c := New(nil, nil, database.DefaultPreferences(30, 30, "info"))
	c.consume("connections", json.RawMessage(`{"downloadTotal":20,"uploadTotal":10,"connections":[{"id":"active","upload":10,"download":20}]}`))
	c.consume("connections", json.RawMessage(`{"downloadTotal":25,"uploadTotal":15,"connections":null}`))

	if len(c.active) != 0 {
		t.Fatalf("active connections after empty snapshot: %+v", c.active)
	}
	snapshot := c.Snapshot().Connections
	if snapshot.Connections == nil || len(snapshot.Connections) != 0 || snapshot.UploadTotal != 15 || snapshot.DownloadTotal != 25 {
		t.Fatalf("empty snapshot: %+v", snapshot)
	}
	if len(c.closed) != 1 {
		t.Fatalf("expected one closed connection, got %+v", c.closed)
	}
	if closed := c.closed[0]; closed.ID != "active" || closed.UploadBytes != 10 || closed.DownloadBytes != 20 || closed.ClosedAt == 0 {
		t.Fatalf("closed connection: %+v", closed)
	}
	if len(c.totals) != 2 {
		t.Fatalf("expected both traffic totals, got %+v", c.totals)
	}
	if total := c.totals[1]; total.Up != 15 || total.Down != 25 || total.ObservedAt == 0 {
		t.Fatalf("closing frame totals: %+v", total)
	}
}

func TestClearHistoryDiscardsPendingBuffers(t *testing.T) {
	db, err := database.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	c := New(db, nil, database.DefaultPreferences(30, 30, "info"))
	if err := database.InsertClosedConnections(db, []database.ClosedConnection{{ID: "stored"}}); err != nil {
		t.Fatal(err)
	}
	if err := database.InsertLogs(db, []database.LogEntry{{Payload: "stored"}}); err != nil {
		t.Fatal(err)
	}
	c.consume("connections", json.RawMessage("{\"downloadTotal\":20,\"uploadTotal\":10,\"connections\":[{\"id\":\"closed\"},{\"id\":\"active\"}]}"))
	c.consume("connections", json.RawMessage("{\"downloadTotal\":22,\"uploadTotal\":12,\"connections\":[{\"id\":\"active\"}]}"))
	c.consume("logs", json.RawMessage("{\"type\":\"error\",\"payload\":\"pending\"}"))
	if len(c.closed) != 1 || len(c.logs) != 1 {
		t.Fatal("test did not queue both kinds of history")
	}
	if err := c.ClearConnections(db); err != nil {
		t.Fatal(err)
	}
	if err := c.ClearLogs(db); err != nil {
		t.Fatal(err)
	}
	c.consume("logs", json.RawMessage("{\"type\":\"error\",\"payload\":\"after clear\"}"))
	c.Stop()

	conns, err := database.QueryConnections(context.Background(), db, database.ConnectionFilter{})
	if err != nil || conns.Total != 0 {
		t.Fatalf("cleared or active connections were persisted: %+v, %v", conns, err)
	}
	logs, err := database.QueryLogs(context.Background(), db, database.LogFilter{})
	if err != nil || len(logs.Items) != 1 || logs.Items[0].Payload != "after clear" {
		t.Fatalf("logs after final flush: %+v, %v", logs, err)
	}
	summary, err := database.GetTrafficSummary(context.Background(), db, time.Now())
	if err != nil || summary.CumulativeUp != 12 {
		t.Fatalf("final traffic flush: %+v, %v", summary, err)
	}
}

func TestPendingTotalsCoalesceWithinHourWithoutHidingReset(t *testing.T) {
	totals := make([]database.TrafficTotal, 0, maxPendingTotals)
	for i := int64(1); i <= maxPendingTotals; i++ {
		totals = appendTrafficTotal(totals, database.TrafficTotal{Up: i, Down: i, ObservedAt: 3600})
	}
	totals = appendTrafficTotal(totals, database.TrafficTotal{Up: 1, Down: 1, ObservedAt: 7200})
	totals = appendTrafficTotal(totals, database.TrafficTotal{Up: maxPendingTotals + 1, Down: maxPendingTotals + 1, ObservedAt: 7200})
	if len(totals) != maxPendingTotals {
		t.Fatalf("buffer grew to %d", len(totals))
	}
	for i, want := range []int64{maxPendingTotals, 1, maxPendingTotals + 1} {
		if got := totals[len(totals)-3+i].Up; got != want {
			t.Fatalf("reset sequence index %d: got %d, want %d", i, got, want)
		}
	}
}

func TestDisabledSchedulerStillFetchesVersion(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/version" {
			t.Errorf("disabled scheduler requested %s", r.URL.Path)
		}
		_, _ = w.Write([]byte("{\"version\":\"test-version\"}"))
	}))
	defer upstream.Close()
	c := New(nil, mihomo.NewClient(upstream.URL, ""), database.DefaultPreferences(30, 0, "info"))
	c.capture(context.Background())
	if status := c.Status(); status.Version != "test-version" {
		t.Fatalf("status %+v", status)
	}
}
