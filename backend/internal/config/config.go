package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Port, DBPath, StaticDir, MihomoURL, MihomoSecret, AuthUsername, AuthPassword, JWTSecret string
	TrustProxy                                                                              bool
	TrustedProxies                                                                          string
	TrustedProxyNets                                                                        []*net.IPNet
	CookieSecure                                                                            string
	RetentionDays, SnapshotIntervalMin                                                      int
	LogPersistLevel, TZ                                                                     string
}

func env(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return fallback
}
func integer(key string, fallback int) (int, error) {
	v := env(key, strconv.Itoa(fallback))
	n, e := strconv.Atoi(v)
	if e != nil {
		return 0, fmt.Errorf("%s: %w", key, e)
	}
	return n, nil
}
func Load() (Config, error) {
	c := Config{Port: env("PORT", "8080"), DBPath: env("DB_PATH", "/data/mihomo-deck.db"), StaticDir: env("STATIC_DIR", "/app/static"), MihomoURL: env("MIHOMO_URL", "http://127.0.0.1:9090"), MihomoSecret: env("MIHOMO_SECRET", ""), AuthUsername: env("AUTH_USERNAME", "admin"), AuthPassword: env("AUTH_PASSWORD", ""), JWTSecret: env("JWT_SECRET", ""), CookieSecure: env("COOKIE_SECURE", "false"), LogPersistLevel: env("LOG_PERSIST_LEVEL", "info"), TZ: env("TZ", "Asia/Shanghai")}
	upstream, err := url.Parse(c.MihomoURL)
	if err != nil || (upstream.Scheme != "http" && upstream.Scheme != "https") ||
		upstream.Hostname() == "" || upstream.ForceQuery || upstream.RawQuery != "" ||
		strings.Contains(c.MihomoURL, "#") || upstream.User != nil {
		return c, fmt.Errorf("MIHOMO_URL must be an HTTP(S) URL with a host and without credentials, query, or fragment")
	}
	c.MihomoURL = strings.TrimRight(upstream.String(), "/")
	switch c.LogPersistLevel {
	case "silent", "error", "warning", "info", "debug":
	default:
		return c, fmt.Errorf("invalid LOG_PERSIST_LEVEL")
	}
	if c.TrustProxy, err = strconv.ParseBool(env("TRUST_PROXY", "false")); err != nil {
		return c, fmt.Errorf("TRUST_PROXY: %w", err)
	}
	proxiesDefault := ""
	if c.TrustProxy {
		proxiesDefault = "127.0.0.1/32,::1/128,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16"
	}
	c.TrustedProxies = env("TRUSTED_PROXIES", proxiesDefault)
	if strings.TrimSpace(c.TrustedProxies) != "" {
		for _, entry := range strings.Split(c.TrustedProxies, ",") {
			_, network, err := net.ParseCIDR(strings.TrimSpace(entry))
			if err != nil {
				return c, fmt.Errorf("TRUSTED_PROXIES: %w", err)
			}
			c.TrustedProxyNets = append(c.TrustedProxyNets, network)
		}
	}
	if c.RetentionDays, err = integer("RETENTION_DAYS", 30); err != nil {
		return c, err
	}
	if c.RetentionDays < 1 || c.RetentionDays > 3650 {
		return c, fmt.Errorf("RETENTION_DAYS must be between 1 and 3650")
	}
	if c.SnapshotIntervalMin, err = integer("SNAPSHOT_INTERVAL_MIN", 30); err != nil {
		return c, err
	}
	if c.SnapshotIntervalMin < 0 || c.SnapshotIntervalMin > 10080 {
		return c, fmt.Errorf("SNAPSHOT_INTERVAL_MIN must be between 0 and 10080")
	}
	if c.CookieSecure != "true" && c.CookieSecure != "false" && c.CookieSecure != "auto" {
		return c, fmt.Errorf("invalid COOKIE_SECURE")
	}
	return c, nil
}
func weak(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "admin", "password", "secret", "changeme", "change-me", "123456":
		return true
	}
	return false
}
func ValidPasswordLength(password string) bool {
	return len(password) >= 8 && len(password) <= 72
}

func ValidPassword(password string) bool {
	return ValidPasswordLength(password) && !weak(password)
}

func (c Config) ValidateSecrets(passwordNeeded bool) error {
	if passwordNeeded && !ValidPassword(c.AuthPassword) {
		return fmt.Errorf("AUTH_PASSWORD must be 8 to 72 bytes and not a weak placeholder")
	}
	if weak(c.JWTSecret) || len(c.JWTSecret) < 16 {
		return fmt.Errorf("JWT_SECRET must be at least 16 characters and not a placeholder")
	}
	return nil
}
