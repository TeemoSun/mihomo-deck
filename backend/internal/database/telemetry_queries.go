package database

import (
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"
)

type TrafficSummary struct {
	CumulativeUp   int64 `json:"cumulativeUp"`
	CumulativeDown int64 `json:"cumulativeDown"`
	TodayUp        int64 `json:"todayUp"`
	TodayDown      int64 `json:"todayDown"`
	WeekUp         int64 `json:"weekUp"`
	WeekDown       int64 `json:"weekDown"`
	MonthUp        int64 `json:"monthUp"`
	MonthDown      int64 `json:"monthDown"`
}
type TrafficPoint struct {
	TS          int64 `json:"ts"`
	UpBPS       int64 `json:"upBps"`
	DownBPS     int64 `json:"downBps"`
	MemInuse    int64 `json:"memInuse"`
	MemOslimit  int64 `json:"memOslimit"`
	ActiveConns int64 `json:"activeConns"`
	UpBytes     int64 `json:"upBytes"`
	DownBytes   int64 `json:"downBytes"`
	PeakUpBPS   int64 `json:"peakUpBps"`
	PeakDownBPS int64 `json:"peakDownBps"`
	AvgMem      int64 `json:"avgMem"`
	MaxConns    int64 `json:"maxConns"`
}

func GetTrafficSummary(db *sql.DB, now time.Time) (s TrafficSummary, err error) {
	err = db.QueryRow("SELECT cumulative_up,cumulative_down FROM traffic_cursor WHERE id=1").Scan(&s.CumulativeUp, &s.CumulativeDown)
	if err == sql.ErrNoRows {
		err = nil
	}
	if err != nil {
		return
	}
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	for _, v := range []struct {
		since    time.Time
		up, down *int64
	}{{midnight, &s.TodayUp, &s.TodayDown}, {now.Add(-7 * 24 * time.Hour), &s.WeekUp, &s.WeekDown}, {now.Add(-30 * 24 * time.Hour), &s.MonthUp, &s.MonthDown}} {
		err = db.QueryRow("SELECT coalesce(sum(up_bytes),0),coalesce(sum(down_bytes),0) FROM traffic_hourly WHERE hour_ts>=? AND hour_ts<=?", v.since.Unix(), now.Unix()).Scan(v.up, v.down)
		if err != nil {
			return
		}
	}
	return
}
func QueryTrafficSeries(db *sql.DB, key string, now time.Time) ([]TrafficPoint, error) {
	var duration time.Duration
	switch key {
	case "1h":
		duration = time.Hour
	case "24h":
		duration = 24 * time.Hour
	case "7d":
		duration = 7 * 24 * time.Hour
	case "30d":
		duration = 30 * 24 * time.Hour
	default:
		return nil, fmt.Errorf("invalid traffic range %q", key)
	}
	var rows *sql.Rows
	var err error
	if key == "1h" || key == "24h" {
		rows, err = db.Query("SELECT ts,up_bps,down_bps,mem_inuse,mem_oslimit,active_conns FROM traffic_samples WHERE ts>=? AND ts<=? ORDER BY ts", now.Add(-duration).Unix(), now.Unix())
	} else {
		rows, err = db.Query("SELECT hour_ts,up_bytes,down_bytes,peak_up_bps,peak_down_bps,avg_mem,max_conns FROM traffic_hourly WHERE hour_ts>=? AND hour_ts<=? ORDER BY hour_ts", now.Add(-duration).Unix(), now.Unix())
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	points := []TrafficPoint{}
	for rows.Next() {
		var p TrafficPoint
		if key == "1h" || key == "24h" {
			err = rows.Scan(&p.TS, &p.UpBPS, &p.DownBPS, &p.MemInuse, &p.MemOslimit, &p.ActiveConns)
		} else {
			err = rows.Scan(&p.TS, &p.UpBytes, &p.DownBytes, &p.PeakUpBPS, &p.PeakDownBPS, &p.AvgMem, &p.MaxConns)
		}
		if err != nil {
			return nil, err
		}
		points = append(points, p)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if key == "24h" && len(points) > 360 {
		out := make([]TrafficPoint, 0, 360)
		for i := 0; i < 360; i++ {
			a, b := i*len(points)/360, (i+1)*len(points)/360
			var p TrafficPoint
			for _, x := range points[a:b] {
				p.TS += x.TS
				p.UpBPS += x.UpBPS
				p.DownBPS += x.DownBPS
				p.MemInuse += x.MemInuse
				p.MemOslimit += x.MemOslimit
				p.ActiveConns += x.ActiveConns
			}
			n := int64(b - a)
			p.TS /= n
			p.UpBPS /= n
			p.DownBPS /= n
			p.MemInuse /= n
			p.MemOslimit /= n
			p.ActiveConns /= n
			out = append(out, p)
		}
		points = out
	}
	return points, nil
}

type ClosedConnection struct {
	ID            string `json:"id"`
	Host          string `json:"host"`
	DstIP         string `json:"dstIp"`
	DstPort       string `json:"dstPort"`
	SrcIP         string `json:"srcIp"`
	SrcPort       string `json:"srcPort"`
	Network       string `json:"network"`
	ConnType      string `json:"connType"`
	InboundName   string `json:"inboundName"`
	Rule          string `json:"rule"`
	RulePayload   string `json:"rulePayload"`
	ChainJSON     string `json:"chainJson"`
	PrimaryProxy  string `json:"primaryProxy"`
	ProviderName  string `json:"providerName"`
	UploadBytes   int64  `json:"uploadBytes"`
	DownloadBytes int64  `json:"downloadBytes"`
	StartAt       int64  `json:"startAt"`
	ClosedAt      int64  `json:"closedAt"`
	DurationMS    int64  `json:"durationMs"`
}
type ConnectionFilter struct {
	Q, Host, Proxy, SrcIP, Rule string
	From, To                    int64
	Page, PageSize              int
}
type PagedConnections struct {
	Items    []ClosedConnection `json:"items"`
	Total    int                `json:"total"`
	Page     int                `json:"page"`
	PageSize int                `json:"pageSize"`
}
type StatItem struct {
	Name  string `json:"name"`
	Bytes int64  `json:"bytes"`
	Count int64  `json:"count"`
}
type TopStatsResult struct {
	TopHosts   []StatItem `json:"topHosts"`
	TopProxies []StatItem `json:"topProxies"`
	TopSrcIPs  []StatItem `json:"topSrcIps"`
}

func InsertClosedConnections(db *sql.DB, items []ClosedConnection) error {
	tx, e := db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	for _, x := range items {
		_, e = tx.Exec(`INSERT OR REPLACE INTO connection_history VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, x.ID, x.Host, x.DstIP, x.DstPort, x.SrcIP, x.SrcPort, x.Network, x.ConnType, x.InboundName, x.Rule, x.RulePayload, x.ChainJSON, x.PrimaryProxy, x.ProviderName, x.UploadBytes, x.DownloadBytes, x.StartAt, x.ClosedAt, x.DurationMS)
		if e != nil {
			return e
		}
	}
	return tx.Commit()
}
func pagination(page, size int) (int, int) {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 50
	}
	if size > 200 {
		size = 200
	}
	return page, size
}
func QueryConnections(db *sql.DB, f ConnectionFilter) (p PagedConnections, err error) {
	p.Page, p.PageSize = pagination(f.Page, f.PageSize)
	p.Items = []ClosedConnection{}
	w := []string{"1=1"}
	args := []any{}
	if f.Q != "" {
		w = append(w, "(host LIKE ? OR dst_ip LIKE ? OR src_ip LIKE ? OR primary_proxy LIKE ? OR rule_payload LIKE ?)")
		for i := 0; i < 5; i++ {
			args = append(args, "%"+f.Q+"%")
		}
	}
	for _, x := range []struct{ col, val string }{{"host", f.Host}, {"primary_proxy", f.Proxy}, {"src_ip", f.SrcIP}, {"rule", f.Rule}} {
		if x.val != "" {
			w = append(w, x.col+"=?")
			args = append(args, x.val)
		}
	}
	if f.From != 0 {
		w = append(w, "closed_at>=?")
		args = append(args, f.From)
	}
	if f.To != 0 {
		w = append(w, "closed_at<=?")
		args = append(args, f.To)
	}
	where := strings.Join(w, " AND ")
	if err = db.QueryRow("SELECT count(*) FROM connection_history WHERE "+where, args...).Scan(&p.Total); err != nil {
		return
	}
	args = append(args, p.PageSize, (p.Page-1)*p.PageSize)
	rows, e := db.Query("SELECT id,coalesce(host,''),coalesce(dst_ip,''),coalesce(dst_port,''),coalesce(src_ip,''),coalesce(src_port,''),coalesce(network,''),coalesce(conn_type,''),coalesce(inbound_name,''),coalesce(rule,''),coalesce(rule_payload,''),coalesce(chain_json,''),coalesce(primary_proxy,''),coalesce(provider_name,''),coalesce(upload_bytes,0),coalesce(download_bytes,0),coalesce(start_at,0),coalesce(closed_at,0),coalesce(duration_ms,0) FROM connection_history WHERE "+where+" ORDER BY closed_at DESC,id DESC LIMIT ? OFFSET ?", args...)
	if e != nil {
		err = e
		return
	}
	defer rows.Close()
	for rows.Next() {
		var x ClosedConnection
		err = rows.Scan(&x.ID, &x.Host, &x.DstIP, &x.DstPort, &x.SrcIP, &x.SrcPort, &x.Network, &x.ConnType, &x.InboundName, &x.Rule, &x.RulePayload, &x.ChainJSON, &x.PrimaryProxy, &x.ProviderName, &x.UploadBytes, &x.DownloadBytes, &x.StartAt, &x.ClosedAt, &x.DurationMS)
		if err != nil {
			return
		}
		p.Items = append(p.Items, x)
	}
	err = rows.Err()
	return
}
func TopStats(db *sql.DB, since time.Time, limit int) (r TopStatsResult, err error) {
	if limit <= 0 {
		limit = 10
	}
	for _, v := range []struct {
		col string
		dst *[]StatItem
	}{{"host", &r.TopHosts}, {"primary_proxy", &r.TopProxies}, {"src_ip", &r.TopSrcIPs}} {
		*v.dst = []StatItem{}
		var rows *sql.Rows
		rows, err = db.Query("SELECT "+v.col+",sum(coalesce(upload_bytes,0)+coalesce(download_bytes,0)),count(*) FROM connection_history WHERE closed_at>=? AND "+v.col+" IS NOT NULL AND "+v.col+"<>'' GROUP BY "+v.col+" ORDER BY 2 DESC,1 LIMIT ?", since.Unix(), limit)
		if err != nil {
			return
		}
		for rows.Next() {
			var x StatItem
			if err = rows.Scan(&x.Name, &x.Bytes, &x.Count); err != nil {
				rows.Close()
				return
			}
			*v.dst = append(*v.dst, x)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return
		}
	}
	return
}
func ClearConnectionHistory(db *sql.DB) error {
	_, err := db.Exec("DELETE FROM connection_history")
	return err
}

type LogEntry struct {
	ID      int64  `json:"id"`
	TS      int64  `json:"ts"`
	Level   string `json:"level"`
	Payload string `json:"payload"`
}
type LogFilter struct {
	Level, Q       string
	From, To       int64
	Page, PageSize int
}
type PagedLogs struct {
	Items    []LogEntry `json:"items"`
	Total    int        `json:"total"`
	Page     int        `json:"page"`
	PageSize int        `json:"pageSize"`
}

func InsertLogs(db *sql.DB, entries []LogEntry) error {
	tx, e := db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	for _, x := range entries {
		if _, e = tx.Exec("INSERT INTO log_entries(ts,level,payload) VALUES(?,?,?)", x.TS, x.Level, x.Payload); e != nil {
			return e
		}
	}
	return tx.Commit()
}
func QueryLogs(db *sql.DB, f LogFilter) (p PagedLogs, err error) {
	p.Page, p.PageSize = pagination(f.Page, f.PageSize)
	p.Items = []LogEntry{}
	w := []string{"1=1"}
	a := []any{}
	if f.Level != "" {
		w = append(w, "level=?")
		a = append(a, f.Level)
	}
	if f.Q != "" {
		w = append(w, "payload LIKE ?")
		a = append(a, "%"+f.Q+"%")
	}
	if f.From != 0 {
		w = append(w, "ts>=?")
		a = append(a, f.From)
	}
	if f.To != 0 {
		w = append(w, "ts<=?")
		a = append(a, f.To)
	}
	where := strings.Join(w, " AND ")
	if err = db.QueryRow("SELECT count(*) FROM log_entries WHERE "+where, a...).Scan(&p.Total); err != nil {
		return
	}
	a = append(a, p.PageSize, (p.Page-1)*p.PageSize)
	rows, e := db.Query("SELECT id,ts,level,payload FROM log_entries WHERE "+where+" ORDER BY id DESC LIMIT ? OFFSET ?", a...)
	if e != nil {
		err = e
		return
	}
	defer rows.Close()
	for rows.Next() {
		var x LogEntry
		if err = rows.Scan(&x.ID, &x.TS, &x.Level, &x.Payload); err != nil {
			return
		}
		p.Items = append(p.Items, x)
	}
	err = rows.Err()
	return
}
func ClearLogs(db *sql.DB) error { _, err := db.Exec("DELETE FROM log_entries"); return err }

type DelayRecord struct {
	ID        int64  `json:"id"`
	ProxyName string `json:"proxyName"`
	GroupName string `json:"groupName"`
	TestURL   string `json:"testUrl"`
	DelayMS   int64  `json:"delayMs"`
	OK        bool   `json:"ok"`
	Source    string `json:"source"`
	TestedAt  int64  `json:"testedAt"`
}
type ProviderSnapshot struct {
	ID            int64  `json:"id"`
	ProviderName  string `json:"providerName"`
	UploadBytes   int64  `json:"uploadBytes"`
	DownloadBytes int64  `json:"downloadBytes"`
	TotalBytes    int64  `json:"totalBytes"`
	ExpireTS      int64  `json:"expireTs"`
	NodeCount     int64  `json:"nodeCount"`
	AliveCount    int64  `json:"aliveCount"`
	CapturedAt    int64  `json:"capturedAt"`
}

func InsertDelays(db *sql.DB, items []DelayRecord) error {
	tx, e := db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	for _, x := range items {
		if _, e = tx.Exec("INSERT INTO delay_history(proxy_name,group_name,test_url,delay_ms,ok,source,tested_at) VALUES(?,?,?,?,?,?,?)", x.ProxyName, x.GroupName, x.TestURL, x.DelayMS, x.OK, x.Source, x.TestedAt); e != nil {
			return e
		}
	}
	return tx.Commit()
}
func QueryDelays(db *sql.DB, name string, since time.Time, limit int) ([]DelayRecord, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, e := db.Query("SELECT id,proxy_name,group_name,test_url,delay_ms,ok,source,tested_at FROM delay_history WHERE (?='' OR proxy_name=?) AND tested_at>=? ORDER BY tested_at DESC,id DESC LIMIT ?", name, name, since.Unix(), limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []DelayRecord{}
	for rows.Next() {
		var x DelayRecord
		if e = rows.Scan(&x.ID, &x.ProxyName, &x.GroupName, &x.TestURL, &x.DelayMS, &x.OK, &x.Source, &x.TestedAt); e != nil {
			return nil, e
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
func InsertProviderSnapshots(db *sql.DB, items []ProviderSnapshot) error {
	tx, e := db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	for _, x := range items {
		if _, e = tx.Exec("INSERT INTO provider_snapshots(provider_name,upload_bytes,download_bytes,total_bytes,expire_ts,node_count,alive_count,captured_at) VALUES(?,?,?,?,?,?,?,?)", x.ProviderName, x.UploadBytes, x.DownloadBytes, x.TotalBytes, x.ExpireTS, x.NodeCount, x.AliveCount, x.CapturedAt); e != nil {
			return e
		}
	}
	return tx.Commit()
}
func QueryProviderSnapshots(db *sql.DB, name string, since time.Time, limit int) ([]ProviderSnapshot, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, e := db.Query("SELECT id,provider_name,upload_bytes,download_bytes,total_bytes,expire_ts,node_count,alive_count,captured_at FROM provider_snapshots WHERE (?='' OR provider_name=?) AND captured_at>=? ORDER BY captured_at DESC,id DESC LIMIT ?", name, name, since.Unix(), limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []ProviderSnapshot{}
	for rows.Next() {
		var x ProviderSnapshot
		if e = rows.Scan(&x.ID, &x.ProviderName, &x.UploadBytes, &x.DownloadBytes, &x.TotalBytes, &x.ExpireTS, &x.NodeCount, &x.AliveCount, &x.CapturedAt); e != nil {
			return nil, e
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

type DBStats struct {
	FileSizeBytes     int64 `json:"fileSizeBytes"`
	TrafficSamples    int64 `json:"trafficSamples"`
	TrafficHourly     int64 `json:"trafficHourly"`
	ConnectionHistory int64 `json:"connectionHistory"`
	LogEntries        int64 `json:"logEntries"`
	DelayHistory      int64 `json:"delayHistory"`
	ProviderSnapshots int64 `json:"providerSnapshots"`
}

func GetDBStats(db *sql.DB, path string) (s DBStats, err error) {
	if path != ":memory:" {
		var info os.FileInfo
		info, err = os.Stat(path)
		if err != nil {
			return
		}
		s.FileSizeBytes = info.Size()
		if wal, walErr := os.Stat(path + "-wal"); walErr == nil {
			s.FileSizeBytes += wal.Size()
		} else if !os.IsNotExist(walErr) {
			err = walErr
			return
		}
	}
	for _, x := range []struct {
		table string
		dst   *int64
	}{{"traffic_samples", &s.TrafficSamples}, {"traffic_hourly", &s.TrafficHourly}, {"connection_history", &s.ConnectionHistory}, {"log_entries", &s.LogEntries}, {"delay_history", &s.DelayHistory}, {"provider_snapshots", &s.ProviderSnapshots}} {
		if err = db.QueryRow("SELECT count(*) FROM " + x.table).Scan(x.dst); err != nil {
			return
		}
	}
	return
}
