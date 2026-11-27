package database

import (
	"database/sql"
	"encoding/json"
	"time"
)

type Preferences struct {
	Theme                 string   `json:"theme"`
	RetentionDays         int      `json:"retentionDays"`
	SnapshotIntervalMin   int      `json:"snapshotIntervalMin"`
	LogPersistLevel       string   `json:"logPersistLevel"`
	HideInfoNodes         bool     `json:"hideInfoNodes"`
	InfoNodePattern       string   `json:"infoNodePattern"`
	PinnedProxies         []string `json:"pinnedProxies"`
	HiddenGroups          []string `json:"hiddenGroups"`
	ProxySort             string   `json:"proxySort"`
	DelayTestURL          string   `json:"delayTestUrl"`
	DelayTestTimeout      int      `json:"delayTestTimeout"`
	AutoRefreshOverview   bool     `json:"autoRefreshOverview"`
	DefaultConnectionsTab string   `json:"defaultConnectionsTab"`
}

func DefaultPreferences(retention, interval int, level string) Preferences {
	return Preferences{Theme: "system", RetentionDays: retention, SnapshotIntervalMin: interval, LogPersistLevel: level, HideInfoNodes: true, InfoNodePattern: "剩余流量|距离下次|套餐到期|官网|重置|到期|Traffic|Expire|Website", PinnedProxies: []string{}, HiddenGroups: []string{}, ProxySort: "default", DelayTestURL: "https://www.gstatic.com/generate_204", DelayTestTimeout: 5000, AutoRefreshOverview: true, DefaultConnectionsTab: "active"}
}
func GetPreferences(db *sql.DB, defaults Preferences) (Preferences, error) {
	var data string
	err := db.QueryRow("SELECT value_json FROM user_preferences WHERE key='preferences'").Scan(&data)
	if err == sql.ErrNoRows {
		return defaults, nil
	}
	if err != nil {
		return defaults, err
	}
	err = json.Unmarshal([]byte(data), &defaults)
	return defaults, err
}
func UpdatePreferences(db *sql.DB, p Preferences) error {
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = db.Exec("INSERT INTO user_preferences(key,value_json,updated_at) VALUES('preferences',?,?) ON CONFLICT(key) DO UPDATE SET value_json=excluded.value_json,updated_at=excluded.updated_at", string(b), time.Now().Unix())
	return err
}
