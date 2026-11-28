package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/TeemoSun/mihomo-deck/internal/database"
)

func result(w http.ResponseWriter, v any, err error) {
	if err != nil {
		fail(w, 500)
		return
	}
	write(w, v)
}
func (s *Server) getPreferences(w http.ResponseWriter, r *http.Request) {
	write(w, s.collector.Preferences())
}
func (s *Server) putPreferences(w http.ResponseWriter, r *http.Request) {
	body := http.MaxBytesReader(w, r.Body, 65536)
	var patch json.RawMessage
	if json.NewDecoder(body).Decode(&patch) != nil {
		fail(w, 400)
		return
	}
	s.prefsMu.Lock()
	defer s.prefsMu.Unlock()
	p := s.collector.Preferences()
	if json.Unmarshal(patch, &p) != nil || p.RetentionDays < 1 || p.RetentionDays > 3650 || p.SnapshotIntervalMin < 0 || p.SnapshotIntervalMin > 10080 || p.DelayTestTimeout < 100 || p.DelayTestTimeout > 60000 || (p.LogPersistLevel != "silent" && p.LogPersistLevel != "error" && p.LogPersistLevel != "warning" && p.LogPersistLevel != "info" && p.LogPersistLevel != "debug") || (p.Theme != "system" && p.Theme != "light" && p.Theme != "dark") {
		fail(w, 400)
		return
	}
	p = p.Normalized()
	if database.UpdatePreferences(s.db, p) != nil {
		fail(w, 500)
		return
	}
	s.collector.SetPreferences(p)
	write(w, p)
}
func (s *Server) overview(w http.ResponseWriter, r *http.Request) {
	state := s.collector.Snapshot()
	summary, err := database.GetTrafficSummary(r.Context(), s.db, time.Now())
	if err != nil {
		fail(w, 500)
		return
	}
	series, err := database.QueryTrafficSeries(s.db, "24h", time.Now())
	if err != nil {
		fail(w, 500)
		return
	}
	top, err := database.TopStats(s.db, time.Now().Add(-24*time.Hour), 10)
	if err != nil {
		fail(w, 500)
		return
	}
	upstreamCtx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	var configs, providers json.RawMessage
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		configs, _ = s.client.Configs(upstreamCtx)
	}()
	go func() {
		defer wg.Done()
		providers, _ = s.client.ProxyProviders(upstreamCtx)
	}()
	wg.Wait()
	write(w, map[string]any{"status": state.Status, "traffic": state.Traffic, "memory": state.Memory, "trafficSummary": summary, "trafficSeries": series, "topStats": top, "configs": rawOrNull(configs), "providers": rawOrNull(providers)})
}
func rawOrNull(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || !json.Valid(raw) {
		return json.RawMessage("null")
	}
	return raw
}
func (s *Server) traffic(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("range")
	if key == "" {
		key = "24h"
	}
	v, err := database.QueryTrafficSeries(s.db, key, time.Now())
	if err != nil {
		fail(w, 400)
		return
	}
	write(w, v)
}
func (s *Server) connections(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	v, e := database.QueryConnections(r.Context(), s.db, database.ConnectionFilter{Q: q.Get("q"), Host: q.Get("host"), Proxy: q.Get("proxy"), SrcIP: q.Get("srcIp"), Rule: q.Get("rule"), From: int64(num(r, "from", 0)), To: int64(num(r, "to", 0)), Page: num(r, "page", 1), PageSize: num(r, "pageSize", 50)})
	result(w, v, e)
}
func (s *Server) clearConnections(w http.ResponseWriter, r *http.Request) {
	if s.collector.ClearConnections(s.db) != nil {
		fail(w, 500)
		return
	}
	w.WriteHeader(204)
}
func (s *Server) logs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	v, e := database.QueryLogs(r.Context(), s.db, database.LogFilter{Level: q.Get("level"), Q: q.Get("q"), From: int64(num(r, "from", 0)), To: int64(num(r, "to", 0)), Page: num(r, "page", 1), PageSize: num(r, "pageSize", 50)})
	result(w, v, e)
}
func (s *Server) clearLogs(w http.ResponseWriter, r *http.Request) {
	if s.collector.ClearLogs(s.db) != nil {
		fail(w, 500)
		return
	}
	w.WriteHeader(204)
}
func (s *Server) delays(w http.ResponseWriter, r *http.Request) {
	v, e := database.QueryDelays(s.db, r.URL.Query().Get("proxyName"), r.URL.Query().Get("providerName"), time.Unix(int64(num(r, "from", 0)), 0), num(r, "limit", 200))
	result(w, v, e)
}
func (s *Server) providers(w http.ResponseWriter, r *http.Request) {
	v, e := database.QueryProviderSnapshots(s.db, r.URL.Query().Get("providerName"), time.Unix(int64(num(r, "from", 0)), 0), num(r, "limit", 200))
	result(w, v, e)
}
func (s *Server) stats(w http.ResponseWriter, r *http.Request) {
	v, e := database.GetDBStats(s.db, s.cfg.DBPath)
	result(w, v, e)
}
func (s *Server) prune(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	if database.PruneTelemetry(s.db, s.collector.Preferences().RetentionDays, now) != nil || database.PruneExpiredRefreshTokens(s.db, now) != nil {
		fail(w, 500)
		return
	}
	w.WriteHeader(204)
}
