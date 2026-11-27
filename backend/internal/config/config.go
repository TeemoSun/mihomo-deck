package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Port, DBPath, StaticDir, MihomoURL, MihomoSecret, AuthUsername, AuthPassword, JWTSecret string
	TrustProxy                                                                              bool
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
	c.MihomoURL = strings.TrimRight(c.MihomoURL, "/")
	switch c.LogPersistLevel {
	case "silent", "error", "warning", "info", "debug":
	default:
		return c, fmt.Errorf("invalid LOG_PERSIST_LEVEL")
	}
	var err error
	if c.TrustProxy, err = strconv.ParseBool(env("TRUST_PROXY", "false")); err != nil {
		return c, fmt.Errorf("TRUST_PROXY: %w", err)
	}
	if c.RetentionDays, err = integer("RETENTION_DAYS", 30); err != nil {
		return c, err
	}
	if c.RetentionDays < 1 {
		return c, fmt.Errorf("RETENTION_DAYS must be positive")
	}
	if c.SnapshotIntervalMin, err = integer("SNAPSHOT_INTERVAL_MIN", 30); err != nil {
		return c, err
	}
	if c.SnapshotIntervalMin < 0 {
		return c, fmt.Errorf("SNAPSHOT_INTERVAL_MIN must not be negative")
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
func (c Config) ValidateSecrets(passwordNeeded bool) error {
	if passwordNeeded && weak(c.AuthPassword) {
		return fmt.Errorf("AUTH_PASSWORD is empty or a weak placeholder")
	}
	if weak(c.JWTSecret) || len(c.JWTSecret) < 16 {
		return fmt.Errorf("JWT_SECRET must be at least 16 characters and not a placeholder")
	}
	return nil
}
