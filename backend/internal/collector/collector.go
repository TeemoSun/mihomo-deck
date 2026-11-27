package collector

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/TeemoSun/mihomo-deck/internal/database"
	"github.com/TeemoSun/mihomo-deck/internal/mihomo"
	"github.com/gorilla/websocket"
)

type Event struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}
type Traffic struct {
	Up   int64 `json:"up"`
	Down int64 `json:"down"`
}
type Memory struct {
	Inuse   int64 `json:"inuse"`
	Oslimit int64 `json:"oslimit"`
}
type Metadata struct {
	Host            string `json:"host"`
	DestinationIP   string `json:"destinationIP"`
	SniffHost       string `json:"sniffHost"`
	DestinationPort string `json:"destinationPort"`
	SourceIP        string `json:"sourceIP"`
	SourcePort      string `json:"sourcePort"`
	Network         string `json:"network"`
	Type            string `json:"type"`
	InboundName     string `json:"inboundName"`
}
type Connection struct {
	ID             string   `json:"id"`
	Metadata       Metadata `json:"metadata"`
	Upload         int64    `json:"upload"`
	Download       int64    `json:"download"`
	Start          string   `json:"start"`
	Chains         []string `json:"chains"`
	ProviderChains []string `json:"providerChains"`
	Rule           string   `json:"rule"`
	RulePayload    string   `json:"rulePayload"`
}
type ConnectionsSnapshot struct {
	DownloadTotal int64        `json:"downloadTotal"`
	UploadTotal   int64        `json:"uploadTotal"`
	Connections   []Connection `json:"connections"`
}
type Status struct {
	Connected  bool   `json:"connected"`
	LastErr    string `json:"lastErr"`
	Version    string `json:"version"`
	LastSeenAt int64  `json:"lastSeenAt"`
}
type State struct {
	Status         Status                   `json:"status"`
	Traffic        Traffic                  `json:"traffic"`
	Memory         Memory                   `json:"memory"`
	Connections    ConnectionsSnapshot      `json:"connections"`
	TrafficHistory []database.TrafficSample `json:"trafficHistory"`
	Logs           []database.LogEntry      `json:"logs"`
}
type Collector struct {
	db          *sql.DB
	client      *mihomo.Client
	mu          sync.Mutex
	state       State
	prefs       database.Preferences
	active      map[string]Connection
	closed      []database.ClosedConnection
	logs        []database.LogEntry
	subscribers map[chan Event]struct{}
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	totals      []ConnectionsSnapshot
	initial     chan struct{}
	logConn     *websocket.Conn
	connected   map[string]bool
	once        sync.Once
}

func New(db *sql.DB, client *mihomo.Client, prefs database.Preferences) *Collector {
	c := &Collector{db: db, client: client, prefs: prefs, active: make(map[string]Connection), subscribers: make(map[chan Event]struct{}), connected: make(map[string]bool), initial: make(chan struct{}, 1)}
	if db != nil {
		if history, err := database.QueryTrafficSeries(db, "1h", time.Now()); err == nil {
			if len(history) > 60 {
				history = history[len(history)-60:]
			}
			c.state.TrafficHistory = make([]database.TrafficSample, 0, len(history))
			for _, p := range history {
				c.state.TrafficHistory = append(c.state.TrafficHistory, database.TrafficSample{TS: p.TS, UpBPS: p.UpBPS, DownBPS: p.DownBPS, MemInuse: p.MemInuse, MemOslimit: p.MemOslimit, ActiveConns: p.ActiveConns})
			}
		}
	}
	return c
}
func (c *Collector) SetPreferences(p database.Preferences) {
	c.mu.Lock()
	changed := (c.prefs.LogPersistLevel == "debug") != (p.LogPersistLevel == "debug")
	c.prefs = p
	conn := c.logConn
	c.mu.Unlock()
	if changed && conn != nil {
		_ = conn.Close()
	}
}
func (c *Collector) Preferences() database.Preferences {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.prefs
}
func (c *Collector) Snapshot() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, _ := json.Marshal(c.state)
	var s State
	_ = json.Unmarshal(b, &s)
	return s
}
func (c *Collector) Subscribe() (<-chan Event, func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan Event, 64)
	c.subscribers[ch] = struct{}{}
	return ch, func() { c.mu.Lock(); defer c.mu.Unlock(); delete(c.subscribers, ch); close(ch) }
}
func (c *Collector) broadcast(t string, data any) {
	for ch := range c.subscribers {
		select {
		case ch <- Event{t, data}:
		default:
		}
	}
}
func (c *Collector) Start(ctx context.Context) {
	ctx, c.cancel = context.WithCancel(ctx)
	for _, path := range []string{"traffic", "memory", "connections", "logs"} {
		c.wg.Add(1)
		go c.loop(ctx, path)
	}
	c.wg.Add(2)
	go c.flushLoop(ctx)
	go c.scheduler(ctx)
}
func (c *Collector) Stop() {
	if c.cancel != nil {
		c.cancel()
		c.wg.Wait()
	}
	c.mu.Lock()
	now := time.Now()
	for _, v := range c.active {
		c.closeConnection(v, now)
	}
	c.active = make(map[string]Connection)
	c.mu.Unlock()
	c.flush()
}
func (c *Collector) loop(ctx context.Context, path string) {
	defer c.wg.Done()
	backoff := time.Second
	for ctx.Err() == nil {
		p := "/" + path
		if path == "logs" {
			level := "info"
			if c.Preferences().LogPersistLevel == "debug" {
				level = "debug"
			}
			p += "?level=" + level
		}
		conn, resp, err := c.client.DialWS(ctx, p)
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
		if err == nil {
			backoff = time.Second
			if path == "logs" {
				c.mu.Lock()
				c.logConn = conn
				c.mu.Unlock()
			}
			c.setStatus(path, true, nil)
			done := make(chan struct{})
			go func() {
				select {
				case <-ctx.Done():
					_ = conn.Close()
				case <-done:
				}
			}()
			for {
				var raw json.RawMessage
				if err = conn.ReadJSON(&raw); err != nil {
					break
				}
				c.consume(path, raw)
			}
			close(done)
			if path == "logs" {
				c.mu.Lock()
				if c.logConn == conn {
					c.logConn = nil
				}
				c.mu.Unlock()
			}
			_ = conn.Close()
		}
		if ctx.Err() != nil {
			return
		}
		c.setStatus(path, false, err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > 30*time.Second {
			backoff = 30 * time.Second
		}
	}
}
func (c *Collector) setStatus(path string, connected bool, err error) {
	c.mu.Lock()
	c.connected[path] = connected
	c.state.Status.Connected = false
	for _, v := range c.connected {
		if v {
			c.state.Status.Connected = true
		}
	}
	if err != nil {
		c.state.Status.LastErr = err.Error()
	} else {
		c.state.Status.LastErr = ""
		c.state.Status.LastSeenAt = time.Now().Unix()
	}
	status := c.state.Status
	c.broadcast("status", status)
	c.mu.Unlock()
	if connected {
		c.once.Do(func() { c.initial <- struct{}{} })
	}
}
func (c *Collector) consume(path string, raw json.RawMessage) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.state.Status.LastSeenAt = time.Now().Unix()
	switch path {
	case "traffic":
		var v Traffic
		if json.Unmarshal(raw, &v) == nil {
			c.state.Traffic = v
			c.broadcast(path, v)
		}
	case "memory":
		var v Memory
		if json.Unmarshal(raw, &v) == nil {
			c.state.Memory = v
			c.broadcast(path, v)
		}
	case "connections":
		var v ConnectionsSnapshot
		if json.Unmarshal(raw, &v) == nil {
			next := make(map[string]Connection)
			for _, x := range v.Connections {
				next[x.ID] = x
			}
			for id, x := range c.active {
				if _, ok := next[id]; !ok {
					c.closeConnection(x, time.Now())
				}
			}
			c.active = next
			c.state.Connections = v
			c.totals = append(c.totals, ConnectionsSnapshot{UploadTotal: v.UploadTotal, DownloadTotal: v.DownloadTotal})
			c.broadcast(path, v)
		}
	case "logs":
		var v struct {
			Type    string `json:"type"`
			Payload string `json:"payload"`
		}
		if json.Unmarshal(raw, &v) == nil {
			entry := database.LogEntry{TS: time.Now().Unix(), Level: v.Type, Payload: v.Payload}
			c.state.Logs = append(c.state.Logs, entry)
			if len(c.state.Logs) > 200 {
				c.state.Logs = c.state.Logs[len(c.state.Logs)-200:]
			}
			levels := map[string]int{"debug": 1, "info": 2, "warning": 3, "error": 4}
			if threshold := levels[c.prefs.LogPersistLevel]; threshold > 0 && levels[v.Type] >= threshold {
				c.logs = append(c.logs, entry)
			}
			c.broadcast("log", entry)
		}
	}
}
func (c *Collector) closeConnection(v Connection, now time.Time) {
	start, err := time.Parse(time.RFC3339Nano, v.Start)
	if err != nil {
		start = now
	}
	duration := now.Sub(start).Milliseconds()
	if duration < 0 {
		duration = 0
	}
	chain, _ := json.Marshal(v.Chains)
	host := v.Metadata.Host
	if host == "" {
		host = v.Metadata.DestinationIP
	}
	if host == "" {
		host = v.Metadata.SniffHost
	}
	x := database.ClosedConnection{ID: v.ID, Host: host, DstIP: v.Metadata.DestinationIP, DstPort: v.Metadata.DestinationPort, SrcIP: v.Metadata.SourceIP, SrcPort: v.Metadata.SourcePort, Network: v.Metadata.Network, ConnType: v.Metadata.Type, InboundName: v.Metadata.InboundName, Rule: v.Rule, RulePayload: v.RulePayload, ChainJSON: string(chain), UploadBytes: v.Upload, DownloadBytes: v.Download, StartAt: start.Unix(), ClosedAt: now.Unix(), DurationMS: duration}
	if len(v.Chains) > 0 {
		x.PrimaryProxy = v.Chains[0]
	}
	if len(v.ProviderChains) > 0 {
		x.ProviderName = v.ProviderChains[0]
	}
	c.closed = append(c.closed, x)
}
func (c *Collector) flushLoop(ctx context.Context) {
	defer c.wg.Done()
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.flush()
		}
	}
}
func (c *Collector) flush() {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	s := database.TrafficSample{TS: now.Unix(), UpBPS: c.state.Traffic.Up, DownBPS: c.state.Traffic.Down, MemInuse: c.state.Memory.Inuse, MemOslimit: c.state.Memory.Oslimit, ActiveConns: int64(len(c.active))}
	err := database.RecordTrafficBatch(c.db, []database.TrafficSample{s}, 0, 0, false, now)
	if err != nil {
		log.Printf("flush traffic: %v", err)
	} else {
		for len(c.totals) > 0 {
			total := c.totals[0]
			if err = database.RecordTrafficBatch(c.db, nil, total.UploadTotal, total.DownloadTotal, true, now); err != nil {
				log.Printf("flush totals: %v", err)
				break
			}
			c.totals = c.totals[1:]
		}
		c.state.TrafficHistory = append(c.state.TrafficHistory, s)
		if len(c.state.TrafficHistory) > 60 {
			c.state.TrafficHistory = c.state.TrafficHistory[len(c.state.TrafficHistory)-60:]
		}
	}
	if len(c.closed) > 0 {
		if err := database.InsertClosedConnections(c.db, c.closed); err != nil {
			log.Printf("flush connections: %v", err)
		} else {
			c.closed = nil
		}
	}
	if len(c.logs) > 0 {
		if err := database.InsertLogs(c.db, c.logs); err != nil {
			log.Printf("flush logs: %v", err)
		} else {
			c.logs = nil
		}
	}
}
