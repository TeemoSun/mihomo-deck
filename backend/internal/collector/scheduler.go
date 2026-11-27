package collector

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/TeemoSun/mihomo-deck/internal/database"
)

func (c *Collector) scheduler(ctx context.Context) {
	defer c.wg.Done()
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	prune := time.NewTicker(time.Hour)
	defer prune.Stop()
	last := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.initial:
			c.capture(ctx)
		case now := <-tick.C:
			p := c.Preferences()
			if p.SnapshotIntervalMin > 0 && now.Sub(last) >= time.Duration(p.SnapshotIntervalMin)*time.Minute {
				c.capture(ctx)
				last = now
			}
		case now := <-prune.C:
			p := c.Preferences()
			if err := database.PruneTelemetry(c.db, p.RetentionDays, now); err != nil {
				log.Printf("prune telemetry: %v", err)
			}
			if err := database.PruneExpiredRefreshTokens(c.db, now); err != nil {
				log.Printf("prune tokens: %v", err)
			}
		}
	}
}
func (c *Collector) capture(ctx context.Context) {
	now := time.Now().Unix()
	raw, err := c.client.Version(ctx)
	if err == nil {
		var v struct {
			Version string `json:"version"`
		}
		if json.Unmarshal(raw, &v) == nil {
			c.mu.Lock()
			c.state.Status.Version = v.Version
			c.broadcast("status", c.state.Status)
			c.mu.Unlock()
		}
	}
	providerByProxy := map[string]string{}
	raw, err = c.client.ProxyProviders(ctx)
	if err == nil {
		var data struct {
			Providers map[string]struct {
				VehicleType      string `json:"vehicleType"`
				SubscriptionInfo struct {
					Upload   int64 `json:"Upload"`
					Download int64 `json:"Download"`
					Total    int64 `json:"Total"`
					Expire   int64 `json:"Expire"`
				} `json:"subscriptionInfo"`
				Proxies []struct {
					Name         string `json:"name"`
					ProviderName string `json:"provider-name"`
					Alive        bool   `json:"alive"`
				} `json:"proxies"`
			} `json:"providers"`
		}
		if json.Unmarshal(raw, &data) == nil {
			var out []database.ProviderSnapshot
			for name, p := range data.Providers {
				if p.VehicleType == "Compatible" {
					continue
				}
				alive := int64(0)
				for _, node := range p.Proxies {
					provider := node.ProviderName
					if provider == "" {
						provider = name
					}
					providerByProxy[node.Name] = provider
					if node.Alive {
						alive++
					}
				}
				out = append(out, database.ProviderSnapshot{ProviderName: name, UploadBytes: p.SubscriptionInfo.Upload, DownloadBytes: p.SubscriptionInfo.Download, TotalBytes: p.SubscriptionInfo.Total, ExpireTS: p.SubscriptionInfo.Expire, NodeCount: int64(len(p.Proxies)), AliveCount: alive, CapturedAt: now})
			}
			if err = database.InsertProviderSnapshots(c.db, out); err != nil {
				log.Printf("provider snapshot: %v", err)
			}
		}
	}
	raw, err = c.client.Proxies(ctx)
	if err != nil {
		return
	}
	var data struct {
		Proxies map[string]struct {
			Type string `json:"type"`
			Now  string `json:"now"`
		} `json:"proxies"`
	}
	if json.Unmarshal(raw, &data) != nil {
		return
	}
	seen := map[string]bool{}
	p := c.Preferences()
	for group, v := range data.Proxies {
		if v.Type != "Selector" && v.Type != "URLTest" && v.Type != "Fallback" {
			continue
		}
		name := v.Now
		if name == "" || seen[name] || name == "DIRECT" || name == "REJECT" || name == "REJECT-DROP" || name == "PASS" || name == "COMPATIBLE" {
			continue
		}
		seen[name] = true
		if prov, ok := providerByProxy[name]; ok && prov != "" {
			raw, err = c.client.TestProviderProxyDelay(ctx, prov, name, p.DelayTestURL, p.DelayTestTimeout)
		} else {
			raw, err = c.client.TestProxyDelay(ctx, name, p.DelayTestURL, p.DelayTestTimeout)
		}
		var result struct {
			Delay int64 `json:"delay"`
		}
		if err == nil {
			err = json.Unmarshal(raw, &result)
		}
		if e := database.InsertDelays(c.db, []database.DelayRecord{{ProxyName: name, GroupName: group, TestURL: p.DelayTestURL, DelayMS: result.Delay, OK: err == nil, Source: "scheduled", TestedAt: now}}); e != nil {
			log.Printf("scheduled delay: %v", e)
		}
	}
}
