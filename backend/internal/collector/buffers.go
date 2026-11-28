package collector

import (
	"database/sql"

	"github.com/TeemoSun/mihomo-deck/internal/database"
)

const (
	maxPendingTotals  = 4096
	maxPendingHistory = 5000
)

func newest[T any](entries []T, limit int) []T {
	if extra := len(entries) - limit; extra > 0 {
		clear(entries[:extra])
		return entries[extra:]
	}
	return entries
}

func monotonic(a, b database.TrafficTotal) bool {
	return a.Up <= b.Up && a.Down <= b.Down
}

func sameHour(a, b database.TrafficTotal) bool {
	return a.ObservedAt/3600 == b.ObservedAt/3600
}

func appendTrafficTotal(totals []database.TrafficTotal, next database.TrafficTotal) []database.TrafficTotal {
	if len(totals) < maxPendingTotals {
		return append(totals, next)
	}
	last := len(totals) - 1
	if monotonic(totals[last-1], totals[last]) && monotonic(totals[last], next) && sameHour(totals[last], next) {
		totals[last] = next
		return totals
	}
	// Removing a middle reading is lossless when both adjacent steps are
	// monotonic and its delta will still be charged to the same hour.
	for i := last - 1; i > 0; i-- {
		if monotonic(totals[i-1], totals[i]) && monotonic(totals[i], totals[i+1]) && sameHour(totals[i], totals[i+1]) {
			copy(totals[i:], totals[i+1:])
			totals[last] = next
			return totals
		}
	}
	// If every reading marks a reset or an hour boundary, keep the most recent ones.
	copy(totals, totals[1:])
	totals[last] = next
	return totals
}

func (c *Collector) ClearConnections(db *sql.DB) error {
	c.flushMu.Lock()
	defer c.flushMu.Unlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := database.ClearConnectionHistory(db); err != nil {
		return err
	}
	c.closed = nil
	return nil
}

func (c *Collector) ClearLogs(db *sql.DB) error {
	c.flushMu.Lock()
	defer c.flushMu.Unlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := database.ClearLogs(db); err != nil {
		return err
	}
	c.logs = nil
	c.state.Logs = nil
	return nil
}
