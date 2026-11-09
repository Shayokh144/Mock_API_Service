package config

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func getenvFrom(values map[string]string) func(string) string {
	return func(key string) string {
		return values[key]
	}
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(getenvFrom(map[string]string{
		"UPSTREAM_BASE_URL": "https://upstream.example.com/prefix",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Upstream.String() != "https://upstream.example.com/prefix" {
		t.Fatalf("upstream = %s", cfg.Upstream)
	}
	if cfg.ListenAddr != "127.0.0.1:8080" {
		t.Fatalf("listen = %s", cfg.ListenAddr)
	}
	if cfg.RulesPath != DefaultRulesPath {
		t.Fatalf("rules path = %s", cfg.RulesPath)
	}
	if cfg.UpstreamsPath != "data/upstreams.json" {
		t.Fatalf("upstreams path = %s", cfg.UpstreamsPath)
	}
	if cfg.HistoryLimit != DefaultHistoryLimit {
		t.Fatalf("history limit = %d", cfg.HistoryLimit)
	}
	if cfg.UpstreamTimeout != DefaultUpstreamTimeout {
		t.Fatalf("timeout = %s", cfg.UpstreamTimeout)
	}

	lines := StartupLines(cfg)
	if len(lines) != 2 {
		t.Fatalf("lines = %#v", lines)
	}
	if lines[0] != "listening on 127.0.0.1:8080" {
		t.Fatalf("line 0 = %q", lines[0])
	}
	if lines[1] != "dashboard at http://localhost:8080/_admin" {
		t.Fatalf("line 1 = %q", lines[1])
	}
}

func TestLoadOverrides(t *testing.T) {
	cfg, err := Load(getenvFrom(map[string]string{
		"UPSTREAM_BASE_URL": "HTTP://127.0.0.1:4000",
		"LISTEN_ADDR":       "localhost:9090",
		"RULES_PATH":        "tmp/rules.json",
		"HISTORY_LIMIT":     "12",
		"UPSTREAM_TIMEOUT":  "2s",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Upstream.Scheme != "http" || cfg.Upstream.Host != "127.0.0.1:4000" {
		t.Fatalf("upstream = %s", cfg.Upstream)
	}
	if cfg.ListenAddr != "127.0.0.1:9090" {
		t.Fatalf("listen = %s", cfg.ListenAddr)
	}
	if DashboardURL(cfg.ListenAddr) != "http://localhost:9090/_admin" {
		t.Fatalf("dashboard = %s", DashboardURL(cfg.ListenAddr))
	}
	if cfg.RulesPath != "tmp/rules.json" || cfg.HistoryLimit != 12 || cfg.UpstreamTimeout != 2*time.Second {
		t.Fatalf("cfg = %+v", cfg)
	}
	if cfg.UpstreamsPath != filepath.Join("tmp", "upstreams.json") {
		t.Fatalf("upstreams path = %s", cfg.UpstreamsPath)
	}
}

func TestLoadIPv6Loopback(t *testing.T) {
	cfg, err := Load(getenvFrom(map[string]string{
		"UPSTREAM_BASE_URL": "http://127.0.0.1:9",
		"LISTEN_ADDR":       "[::1]:8080",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ListenAddr != "[::1]:8080" {
		t.Fatalf("listen = %s", cfg.ListenAddr)
	}
	if DashboardURL(cfg.ListenAddr) != "http://[::1]:8080/_admin" {
		t.Fatalf("dashboard = %s", DashboardURL(cfg.ListenAddr))
	}
}

func TestLoadAllowsMissingUpstream(t *testing.T) {
	cfg, err := Load(getenvFrom(map[string]string{}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Upstream != nil {
		t.Fatalf("upstream = %s", cfg.Upstream)
	}
	lines := StartupLines(cfg)
	if len(lines) != 3 || !strings.Contains(lines[2], "no upstream configured") {
		t.Fatalf("lines = %#v", lines)
	}
}

func TestLoadRejectsBadSettings(t *testing.T) {
	cases := []struct {
		name   string
		env    map[string]string
		substr string
	}{
		{"bad scheme", map[string]string{"UPSTREAM_BASE_URL": "ftp://upstream.example"}, "UPSTREAM_BASE_URL"},
		{"missing host", map[string]string{"UPSTREAM_BASE_URL": "http:///missing"}, "UPSTREAM_BASE_URL"},
		{"query", map[string]string{"UPSTREAM_BASE_URL": "http://upstream.example/?a=1"}, "UPSTREAM_BASE_URL"},
		{"fragment", map[string]string{"UPSTREAM_BASE_URL": "http://upstream.example/#x"}, "UPSTREAM_BASE_URL"},
		{"public bind", map[string]string{"UPSTREAM_BASE_URL": "http://127.0.0.1:9", "LISTEN_ADDR": "0.0.0.0:8080"}, "LISTEN_ADDR"},
		{"lan bind", map[string]string{"UPSTREAM_BASE_URL": "http://127.0.0.1:9", "LISTEN_ADDR": "192.168.1.10:8080"}, "LISTEN_ADDR"},
		{"bad addr", map[string]string{"UPSTREAM_BASE_URL": "http://127.0.0.1:9", "LISTEN_ADDR": "8080"}, "LISTEN_ADDR"},
		{"bad limit", map[string]string{"UPSTREAM_BASE_URL": "http://127.0.0.1:9", "HISTORY_LIMIT": "0"}, "HISTORY_LIMIT"},
		{"bad timeout", map[string]string{"UPSTREAM_BASE_URL": "http://127.0.0.1:9", "UPSTREAM_TIMEOUT": "soon"}, "UPSTREAM_TIMEOUT"},
		{"zero timeout", map[string]string{"UPSTREAM_BASE_URL": "http://127.0.0.1:9", "UPSTREAM_TIMEOUT": "0s"}, "UPSTREAM_TIMEOUT"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(getenvFrom(tc.env))
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.substr) {
				t.Fatalf("error %q does not name %s", err, tc.substr)
			}
		})
	}
}
