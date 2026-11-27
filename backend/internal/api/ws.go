package api

import (
	"github.com/TeemoSun/mihomo-deck/internal/collector"
	"github.com/gorilla/websocket"
	"net/http"
	"net/url"
	"strings"
)

func (s *Server) live(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true
		}
		u, err := url.Parse(origin)
		return err == nil && (u.Scheme == "http" || u.Scheme == "https") && strings.EqualFold(u.Host, r.Host)
	}}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	events, unsubscribe := s.collector.Subscribe()
	defer unsubscribe()
	if conn.WriteJSON(collector.Event{Type: "init", Data: s.collector.Snapshot()}) != nil {
		return
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()
	for {
		select {
		case <-done:
			return
		case event, ok := <-events:
			if !ok || conn.WriteJSON(event) != nil {
				return
			}
		}
	}
}
