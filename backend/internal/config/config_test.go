package config

import (
	"os"
	"strings"
	"testing"
)

func TestLoadValidatesMihomoURL(t *testing.T) {
	for _, value := range []string{
		"", "/socket", "ftp://controller.test", "http://:9090", "http://controller.test?",
		"http://controller.test?token=sample", "http://controller.test#", "http://controller.test#version",
		"http://user:pass@controller.test", "http://bad host",
	} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("MIHOMO_URL", value)
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), "MIHOMO_URL") {
				t.Fatalf("unexpected error %v", err)
			}
		})
	}
	t.Setenv("MIHOMO_URL", "https://[::1]:9090/controller/")
	if cfg, err := Load(); err != nil || cfg.MihomoURL != "https://[::1]:9090/controller" {
		t.Fatalf("valid URL: %q, error %v", cfg.MihomoURL, err)
	}
}

func TestLoadValidatesProxyCIDRsAndTelemetryBounds(t *testing.T) {
	t.Setenv("TRUST_PROXY", "true")
	t.Setenv("TRUSTED_PROXIES", "")
	if err := os.Unsetenv("TRUSTED_PROXIES"); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil || len(cfg.TrustedProxyNets) != 5 {
		t.Fatalf("default trusted proxy ranges: %q, error %v", cfg.TrustedProxies, err)
	}
	for _, tc := range []struct{ key, value string }{
		{"TRUSTED_PROXIES", "127.0.0.1/35"},
		{"RETENTION_DAYS", "0"},
		{"RETENTION_DAYS", "3651"},
		{"SNAPSHOT_INTERVAL_MIN", "-1"},
		{"SNAPSHOT_INTERVAL_MIN", "10081"},
	} {
		t.Run(tc.key+"/"+tc.value, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), tc.key) {
				t.Fatalf("unexpected error %v", err)
			}
		})
	}
}
