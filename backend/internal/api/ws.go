package api

import (
	"net/http"
	"time"

	"github.com/TeemoSun/mihomo-deck/internal/collector"
	"github.com/TeemoSun/mihomo-deck/internal/database"
	"github.com/gorilla/websocket"
)

const (
	livePingInterval = 30 * time.Second
	livePongTimeout  = 2 * livePingInterval
	liveWriteTimeout = 10 * time.Second
)

type liveSocket struct {
	userID, tokenVersion int64
	expiresAt            time.Time
}

var liveUpgrader = websocket.Upgrader{CheckOrigin: sameOrigin, HandshakeTimeout: 5 * time.Second}

func (s *Server) registerSocket(conn *websocket.Conn, socket liveSocket, sessionID string) bool {
	s.wsMu.Lock()
	defer s.wsMu.Unlock()
	if s.wsClosing || !time.Now().Before(socket.expiresAt) {
		return false
	}
	// Authentication can change between the middleware and the WebSocket upgrade.
	u, err := database.GetUserByID(s.db, socket.userID)
	if err != nil || u.TokenVersion != socket.tokenVersion {
		return false
	}
	if sessionID != "" {
		active, err := database.ActiveRefreshSession(s.db, socket.userID, sessionID, time.Now())
		if err != nil || !active {
			return false
		}
	}
	s.ws[conn] = socket
	return true
}

func (s *Server) CloseUserSockets(userID int64) {
	s.wsMu.Lock()
	defer s.wsMu.Unlock()
	for conn, info := range s.ws {
		if info.userID == userID {
			delete(s.ws, conn)
			_ = conn.Close()
		}
	}
}

func (s *Server) CloseAllSockets() {
	s.wsMu.Lock()
	defer s.wsMu.Unlock()
	s.wsClosing = true
	for conn := range s.ws {
		delete(s.ws, conn)
		_ = conn.Close()
	}
}

func (s *Server) live(w http.ResponseWriter, r *http.Request) {
	conn, err := liveUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	c := authentication(r).claims
	socket := liveSocket{userID: c.UserID, tokenVersion: c.Version, expiresAt: c.ExpiresAt.Time}
	if !s.registerSocket(conn, socket, c.SessionID) {
		return
	}
	defer func() {
		s.wsMu.Lock()
		delete(s.ws, conn)
		s.wsMu.Unlock()
	}()
	expiration := time.AfterFunc(time.Until(socket.expiresAt), func() { _ = conn.Close() })
	defer expiration.Stop()

	conn.SetReadLimit(4096)
	if err := conn.SetReadDeadline(time.Now().Add(livePongTimeout)); err != nil {
		return
	}
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(livePongTimeout))
	})

	snapshot, events, unsubscribe := s.collector.SubscribeWithSnapshot()
	defer unsubscribe()
	if writeLiveEvent(conn, collector.Event{Type: "init", Data: snapshot}) != nil {
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
	ping := time.NewTicker(livePingInterval)
	defer ping.Stop()
	for {
		select {
		case <-done:
			return
		case <-ping.C:
			if conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(liveWriteTimeout)) != nil {
				return
			}
		case event, ok := <-events:
			if !ok || writeLiveEvent(conn, event) != nil {
				return
			}
		}
	}
}

func writeLiveEvent(conn *websocket.Conn, event collector.Event) error {
	if err := conn.SetWriteDeadline(time.Now().Add(liveWriteTimeout)); err != nil {
		return err
	}
	return conn.WriteJSON(event)
}
