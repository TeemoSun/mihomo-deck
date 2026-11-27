package api

import (
	"encoding/json"
	"errors"
	"github.com/TeemoSun/mihomo-deck/internal/database"
	"github.com/TeemoSun/mihomo-deck/internal/mihomo"
	"github.com/go-chi/chi/v5"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

func (s *Server) proxyRouter() http.Handler {
	r := chi.NewRouter()
	r.Get("/version", s.proxy)
	r.Get("/configs", s.proxy)
	r.Patch("/configs", s.proxy)
	r.Put("/configs/reload", s.proxy)
	r.Post("/configs/geo", s.proxy)
	r.Post("/restart", s.proxy)
	r.Get("/proxies", s.proxy)
	r.Put("/proxies/{name}", s.proxy)
	r.Delete("/proxies/{name}", s.proxy)
	r.Get("/proxies/{name}/delay", s.proxy)
	r.Get("/group/{name}/delay", s.proxy)
	r.Get("/providers/proxies", s.proxy)
	r.Put("/providers/proxies/{name}", s.proxy)
	r.Get("/providers/proxies/{name}/healthcheck", s.proxy)
	r.Get("/providers/rules", s.proxy)
	r.Put("/providers/rules/{name}", s.proxy)
	r.Get("/rules", s.proxy)
	r.Get("/connections", s.proxy)
	r.Delete("/connections", s.proxy)
	r.Delete("/connections/{id}", s.proxy)
	r.Get("/dns/query", s.proxy)
	r.Post("/cache/fakeip/flush", s.proxy)
	r.Post("/cache/dns/flush", s.proxy)
	return r
}
func (s *Server) proxy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	name := proxyParam(r, "name")
	id := proxyParam(r, "id")
	pattern := chi.RouteContext(ctx).RoutePattern()
	var data json.RawMessage
	var err error
	c := s.client
	switch pattern {
	case "/api/mihomo/version":
		data, err = c.Version(ctx)
	case "/api/mihomo/configs":
		if r.Method == "GET" {
			data, err = c.Configs(ctx)
		} else {
			var b []byte
			b, err = io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
			if err == nil && !json.Valid(b) {
				fail(w, 400)
				return
			}
			if err == nil {
				data, err = c.PatchConfigs(ctx, b)
			}
		}
	case "/api/mihomo/configs/reload":
		data, err = c.ReloadConfigs(ctx, r.URL.Query().Get("force") == "true")
	case "/api/mihomo/configs/geo":
		data, err = c.UpdateGeo(ctx)
	case "/api/mihomo/restart":
		data, err = c.Restart(ctx)
	case "/api/mihomo/proxies":
		data, err = c.Proxies(ctx)
	case "/api/mihomo/proxies/{name}":
		if r.Method == "DELETE" {
			data, err = c.UnfixProxy(ctx, name)
		} else {
			var b struct {
				Name string `json:"name"`
			}
			if json.NewDecoder(http.MaxBytesReader(w, r.Body, 65536)).Decode(&b) != nil {
				fail(w, 400)
				return
			}
			data, err = c.SelectProxy(ctx, name, b.Name)
		}
	case "/api/mihomo/proxies/{name}/delay", "/api/mihomo/group/{name}/delay":
		p := s.collector.Preferences()
		testURL := r.URL.Query().Get("url")
		if testURL == "" {
			testURL = p.DelayTestURL
		}
		timeout := p.DelayTestTimeout
		if n, e := strconv.Atoi(r.URL.Query().Get("timeout")); e == nil && n > 0 && n <= 60000 {
			timeout = n
		}
		if pattern == "/api/mihomo/group/{name}/delay" {
			data, err = c.TestGroupDelay(ctx, name, testURL, timeout)
			var delays map[string]int64
			if err == nil && json.Unmarshal(data, &delays) == nil {
				var records []database.DelayRecord
				for node, delay := range delays {
					records = append(records, database.DelayRecord{ProxyName: node, GroupName: name, TestURL: testURL, DelayMS: delay, OK: delay > 0, Source: "manual", TestedAt: time.Now().Unix()})
				}
				_ = database.InsertDelays(s.db, records)
			}
		} else {
			provider := r.URL.Query().Get("provider")
			if provider != "" {
				data, err = c.TestProviderProxyDelay(ctx, provider, name, testURL, timeout)
			} else {
				data, err = c.TestProxyDelay(ctx, name, testURL, timeout)
				var upstream *mihomo.UpstreamError
				if errors.As(err, &upstream) && upstream.StatusCode == http.StatusNotFound {
					if providers, e := c.ProxyProviders(ctx); e == nil {
						var result struct {
							Providers map[string]struct {
								VehicleType string `json:"vehicleType"`
								Proxies     []struct {
									Name         string `json:"name"`
									ProviderName string `json:"provider-name"`
								} `json:"proxies"`
							} `json:"providers"`
						}
						if json.Unmarshal(providers, &result) == nil {
							for key, p := range result.Providers {
								if p.VehicleType == "Compatible" {
									continue
								}
								for _, node := range p.Proxies {
									if node.Name == name {
										provider = node.ProviderName
										if provider == "" {
											provider = key
										}
										break
									}
								}
								if provider != "" {
									break
								}
							}
							if provider != "" {
								data, err = c.TestProviderProxyDelay(ctx, provider, name, testURL, timeout)
							}
						}
					}
				}
			}
			var v struct {
				Delay int64 `json:"delay"`
			}
			if err == nil {
				_ = json.Unmarshal(data, &v)
			}
			_ = database.InsertDelays(s.db, []database.DelayRecord{{ProxyName: name, TestURL: testURL, DelayMS: v.Delay, OK: err == nil && v.Delay > 0, Source: "manual", TestedAt: time.Now().Unix()}})
		}
	case "/api/mihomo/providers/proxies":
		data, err = c.ProxyProviders(ctx)
	case "/api/mihomo/providers/proxies/{name}":
		data, err = c.UpdateProxyProvider(ctx, name)
	case "/api/mihomo/providers/proxies/{name}/healthcheck":
		data, err = c.HealthCheckProxyProvider(ctx, name)
	case "/api/mihomo/providers/rules":
		data, err = c.RuleProviders(ctx)
	case "/api/mihomo/providers/rules/{name}":
		data, err = c.UpdateRuleProvider(ctx, name)
	case "/api/mihomo/rules":
		data, err = c.Rules(ctx)
	case "/api/mihomo/connections":
		if r.Method == "GET" {
			data, err = c.Connections(ctx)
		} else {
			data, err = c.CloseAllConnections(ctx)
		}
	case "/api/mihomo/connections/{id}":
		data, err = c.CloseConnection(ctx, id)
	case "/api/mihomo/dns/query":
		data, err = c.DNSQuery(ctx, r.URL.Query().Get("name"), r.URL.Query().Get("type"))
	case "/api/mihomo/cache/fakeip/flush":
		data, err = c.FlushFakeIPCache(ctx)
	case "/api/mihomo/cache/dns/flush":
		data, err = c.FlushDNSCache(ctx)
	default:
		fail(w, 404)
		return
	}
	if err != nil {
		var upstream *mihomo.UpstreamError
		if errors.As(err, &upstream) && upstream.StatusCode >= 400 {
			contentType := "text/plain"
			if json.Valid(upstream.Body) {
				contentType = "application/json"
			}
			w.Header().Set("Content-Type", contentType)
			w.WriteHeader(upstream.StatusCode)
			_, _ = w.Write(upstream.Body)
			return
		}
		fail(w, 502)
		return
	}
	if len(data) == 0 {
		w.WriteHeader(204)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

// Chi matches RawPath when it is present, so its parameters still need decoding.
func proxyParam(r *http.Request, key string) string {
	value := chi.URLParam(r, key)
	if r.URL.RawPath != "" {
		if decoded, err := url.PathUnescape(value); err == nil {
			return decoded
		}
	}
	return value
}
