package api

import (
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func sameOrigin(r *http.Request) bool {
	origins := r.Header.Values("Origin")
	if len(origins) == 0 {
		return true
	}
	if len(origins) != 1 {
		return false
	}
	u, err := url.Parse(origins[0])
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.User == nil &&
		u.Path == "" && u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" && strings.EqualFold(u.Host, r.Host)
}

func requireSameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(r) {
			fail(w, 403)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func peerHost(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

func (s *Server) trustedIP(ip net.IP) bool {
	if !s.cfg.TrustProxy || ip == nil {
		return false
	}
	for _, network := range s.cfg.TrustedProxyNets {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func (s *Server) ip(r *http.Request) string {
	host := peerHost(r)
	peer := net.ParseIP(host)
	if peer != nil {
		host = peer.String()
	}
	if !s.trustedIP(peer) {
		return host
	}
	xff := strings.Join(r.Header.Values("X-Forwarded-For"), ",")
	if strings.TrimSpace(xff) == "" {
		if ip := net.ParseIP(strings.TrimSpace(r.Header.Get("X-Real-IP"))); ip != nil {
			return ip.String()
		}
		return host
	}
	parts := strings.Split(xff, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		ip := net.ParseIP(strings.TrimSpace(parts[i]))
		if ip == nil {
			return host
		}
		if !s.trustedIP(ip) || i == 0 {
			return ip.String()
		}
	}
	return host
}

func (s *Server) secure(r *http.Request) bool {
	return s.cfg.CookieSecure == "true" || s.cfg.CookieSecure == "auto" &&
		(r.TLS != nil || s.trustedIP(net.ParseIP(peerHost(r))) && strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https"))
}

const (
	loginWindow  = 5 * time.Minute
	loginLimit   = 5
	loginIPLimit = 4096
)

type loginAttempts struct {
	pending int
	failed  []time.Time
}

func (a *loginAttempts) prune(now time.Time) {
	keep := a.failed[:0]
	for _, t := range a.failed {
		if now.Sub(t) < loginWindow {
			keep = append(keep, t)
		}
	}
	a.failed = keep
}

func (s *Server) acquireBcrypt() bool {
	select {
	case s.bcryptSlots <- struct{}{}:
		return true
	default:
		return false
	}
}

func (s *Server) releaseBcrypt() { <-s.bcryptSlots }

func (s *Server) reserveLogin(ip string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !now.Before(s.nextAttemptPrune) {
		for key, attempts := range s.attempts {
			attempts.prune(now)
			if attempts.pending == 0 && len(attempts.failed) == 0 {
				delete(s.attempts, key)
			}
		}
		s.nextAttemptPrune = now.Add(time.Minute)
	}
	attempts := s.attempts[ip]
	if attempts == nil {
		if len(s.attempts) >= loginIPLimit {
			return false
		}
		attempts = &loginAttempts{}
		s.attempts[ip] = attempts
	}
	attempts.prune(now)
	if attempts.pending+len(attempts.failed) >= loginLimit {
		return false
	}
	attempts.pending++
	return true
}

func (s *Server) finishLogin(ip string, invalid, success bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	attempts := s.attempts[ip]
	attempts.pending--
	if invalid {
		attempts.failed = append(attempts.failed, time.Now())
	} else if success {
		attempts.failed = nil
	}
	if attempts.pending == 0 && len(attempts.failed) == 0 {
		delete(s.attempts, ip)
	}
}
