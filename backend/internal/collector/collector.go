package collector

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"maps"
	"slices"
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
type StreamStatus struct {
	Connected bool   `json:"connected"`
	LastErr   string `json:"lastErr"`
}
type Status struct {
	Connected  bool                    `json:"connected"`
	LastErr    string                  `json:"lastErr"`
	Version    string                  `json:"version"`
	LastSeenAt int64                   `json:"lastSeenAt"`
	Streams    map[string]StreamStatus `json:"streams"`
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
	flushMu     sync.Mutex
	state       State
	prefs       database.Preferences
	active      map[string]Connection
	closed      []database.ClosedConnection
	logs        []database.LogEntry
	subscribers map[chan Event]struct{}
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	totals      []database.TrafficTotal
	initial     chan struct{}
	logConn     *websocket.Conn
	once        sync.Once
}

func New(db *sql.DB, client *mihomo.Client, prefs database.Preferences) *Collector {
	c := &Collector{db: db, client: client, prefs: copyPreferences(prefs), active: make(map[string]Connection), subscribers: make(map[chan Event]struct{}), initial: make(chan struct{}, 1)}
	c.state.Status.Streams = map[string]StreamStatus{"traffic": {}, "connections": {}, "memory": {}, "logs": {}}
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
	c.prefs = copyPreferences(p)
	conn := c.logConn
	c.mu.Unlock()
	if changed && conn != nil {
		_ = conn.Close()
	}
}
func (c *Collector) Preferences() database.Preferences {
	c.mu.Lock()
	defer c.mu.Unlock()
	return copyPreferences(c.prefs)
}
func copyPreferences(p database.Preferences) database.Preferences {
	p.PinnedProxies = slices.Clone(p.PinnedProxies)
	p.HiddenGroups = slices.Clone(p.HiddenGroups)
	return p.Normalized()
}

func (c *Collector) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	status := c.state.Status
	status.Streams = maps.Clone(status.Streams)
	return status
}

func (c *Collector) Snapshot() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.snapshotLocked()
}

func (c *Collector) snapshotLocked() State {
	s := c.state
	s.Status.Streams = maps.Clone(s.Status.Streams)
	s.TrafficHistory = slices.Clone(s.TrafficHistory)
	s.Logs = slices.Clone(s.Logs)
	s.Connections.Connections = slices.Clone(s.Connections.Connections)
	for i := range s.Connections.Connections {
		conn := &s.Connections.Connections[i]
		conn.Chains = slices.Clone(conn.Chains)
		conn.ProviderChains = slices.Clone(conn.ProviderChains)
	}
	return s
}

func (c *Collector) Subscribe() (<-chan Event, func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.subscribeLocked()
}

func (c *Collector) SubscribeWithSnapshot() (State, <-chan Event, func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch, unsubscribe := c.subscribeLocked()
	return c.snapshotLocked(), ch, unsubscribe
}

func (c *Collector) subscribeLocked() (<-chan Event, func()) {
	ch := make(chan Event, 64)
	c.subscribers[ch] = struct{}{}
	return ch, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		delete(c.subscribers, ch)
		close(ch)
	}
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
	c.wg.Add(3)
	go c.flushLoop(ctx)
	go c.scheduler(ctx)
	go c.pruneLoop(ctx)
}
func (c *Collector) Stop() {
	if c.cancel != nil {
		c.cancel()
		c.wg.Wait()
	}
	c.flush()
}
func (c *Collector) loop(ctx context.Context, path string) {
	defer c.wg.Done()
	backoff := time.Second
	for ctx.Err() == nil {
		p := "/" + path
		var persistLevel string
		if path == "logs" {
			persistLevel = c.Preferences().LogPersistLevel
			level := "info"
			if persistLevel == "debug" {
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
				if c.prefs.LogPersistLevel != persistLevel {
					c.mu.Unlock()
					_ = conn.Close()
					continue
				}
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
			c.setStatus(path, false, nil)
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
	stream := StreamStatus{Connected: connected}
	if err != nil {
		stream.LastErr = err.Error()
	}
	// Status events keep their own stream map as later updates arrive.
	streams := maps.Clone(c.state.Status.Streams)
	streams[path] = stream
	c.state.Status.Streams = streams
	c.state.Status.Connected = streams["traffic"].Connected || streams["connections"].Connected
	c.state.Status.LastErr = ""
	for _, streamName := range []string{"traffic", "connections", "memory", "logs"} {
		if message := streams[streamName].LastErr; message != "" {
			c.state.Status.LastErr = message
			break
		}
	}
	if connected {
		c.state.Status.LastSeenAt = time.Now().Unix()
	}
	coreConnected := c.state.Status.Connected
	status := c.state.Status
	c.broadcast("status", status)
	c.mu.Unlock()
	if coreConnected {
		c.once.Do(func() { c.initial <- struct{}{} })
	}
}
func (c *Collector) consume(path string, raw json.RawMessage) {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
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
		var frame struct {
			DownloadTotal *int64          `json:"downloadTotal"`
			UploadTotal   *int64          `json:"uploadTotal"`
			Connections   json.RawMessage `json:"connections"`
		}
		if json.Unmarshal(raw, &frame) != nil || frame.DownloadTotal == nil || frame.UploadTotal == nil ||
			*frame.DownloadTotal < 0 || *frame.UploadTotal < 0 || len(frame.Connections) == 0 {
			return
		}
		var connections []Connection
		if bytes.Equal(bytes.TrimSpace(frame.Connections), []byte("null")) {
			connections = []Connection{}
		} else if json.Unmarshal(frame.Connections, &connections) != nil {
			return
		}
		v := ConnectionsSnapshot{DownloadTotal: *frame.DownloadTotal, UploadTotal: *frame.UploadTotal, Connections: connections}
		next := make(map[string]Connection, len(v.Connections))
		for _, x := range v.Connections {
			next[x.ID] = x
		}
		for id, x := range c.active {
			if _, ok := next[id]; !ok {
				c.closeConnection(x, now)
			}
		}
		c.active = next
		c.state.Connections = v
		c.totals = appendTrafficTotal(c.totals, database.TrafficTotal{Up: v.UploadTotal, Down: v.DownloadTotal, ObservedAt: now.Unix()})
		c.broadcast(path, v)
	case "logs":
		var v struct {
			Type    string `json:"type"`
			Payload string `json:"payload"`
		}
		if json.Unmarshal(raw, &v) == nil {
			entry := database.LogEntry{TS: now.Unix(), Level: v.Type, Payload: v.Payload}
			c.state.Logs = append(c.state.Logs, entry)
			if len(c.state.Logs) > 200 {
				c.state.Logs = c.state.Logs[len(c.state.Logs)-200:]
			}
			levels := map[string]int{"debug": 1, "info": 2, "warning": 3, "error": 4}
			if threshold := levels[c.prefs.LogPersistLevel]; threshold > 0 && levels[v.Type] >= threshold {
				c.logs = newest(append(c.logs, entry), maxPendingHistory)
			}
			c.broadcast("log", entry)
		}
	}
	c.state.Status.LastSeenAt = now.Unix()
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
	c.closed = newest(append(c.closed, x), maxPendingHistory)
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
	if c.db == nil {
		return
	}
	c.flushMu.Lock()
	defer c.flushMu.Unlock()
	now := time.Now()
	c.mu.Lock()
	closed, logs, totals := c.closed, c.logs, c.totals
	c.closed, c.logs, c.totals = nil, nil, nil
	connected := c.state.Status.Connected
	sample := database.TrafficSample{TS: now.Unix(), UpBPS: c.state.Traffic.Up, DownBPS: c.state.Traffic.Down, MemInuse: c.state.Memory.Inuse, MemOslimit: c.state.Memory.Oslimit, ActiveConns: int64(len(c.active))}
	c.mu.Unlock()
	var samples []database.TrafficSample
	if connected {
		samples = append(samples, sample)
	}
	if len(samples) > 0 || len(totals) > 0 {
		if err := database.RecordTrafficTotalsBatch(c.db, samples, totals, now); err != nil {
			log.Printf("flush traffic: %v", err)
			c.mu.Lock()
			for _, total := range c.totals {
				totals = appendTrafficTotal(totals, total)
			}
			c.totals = totals
			c.mu.Unlock()
		} else if connected {
			c.mu.Lock()
			c.state.TrafficHistory = append(c.state.TrafficHistory, sample)
			if len(c.state.TrafficHistory) > 60 {
				c.state.TrafficHistory = c.state.TrafficHistory[len(c.state.TrafficHistory)-60:]
			}
			c.mu.Unlock()
		}
	}
	if len(closed) > 0 {
		if err := database.InsertClosedConnections(c.db, closed); err != nil {
			log.Printf("flush connections: %v", err)
			c.mu.Lock()
			c.closed = newest(append(closed, c.closed...), maxPendingHistory)
			c.mu.Unlock()
		}
	}
	if len(logs) > 0 {
		if err := database.InsertLogs(c.db, logs); err != nil {
			log.Printf("flush logs: %v", err)
			c.mu.Lock()
			c.logs = newest(append(logs, c.logs...), maxPendingHistory)
			c.mu.Unlock()
		}
	}
}
