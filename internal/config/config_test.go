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
	if cfg.AdminListenAddr != "127.0.0.1:8081" {
		t.Fatalf("admin listen = %s", cfg.AdminListenAddr)
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
	if cfg.MaxBufferedBody != MaxBufferedBody {
		t.Fatalf("max body = %d", cfg.MaxBufferedBody)
	}
	if cfg.UpstreamPrefix != "/app" {
		t.Fatalf("prefix = %s", cfg.UpstreamPrefix)
	}

	lines := StartupLines(cfg)
	if len(lines) != 4 {
		t.Fatalf("lines = %#v", lines)
	}
	if lines[0] != "listening on 127.0.0.1:8080" {
		t.Fatalf("line 0 = %q", lines[0])
	}
	if lines[1] != "admin listening on 127.0.0.1:8081" {
		t.Fatalf("line 1 = %q", lines[1])
	}
	if lines[2] != "dashboard at http://localhost:8081/_admin" {
		t.Fatalf("line 2 = %q", lines[2])
	}
	if lines[3] != "admin token file data/admin.token" {
		t.Fatalf("line 3 = %q", lines[3])
	}
}

func TestLoadOverrides(t *testing.T) {
	cfg, err := Load(getenvFrom(map[string]string{
		"UPSTREAM_BASE_URL": "HTTP://127.0.0.1:4000",
		"LISTEN_ADDR":       "localhost:9090",
		"RULES_PATH":        "tmp/rules.json",
		"HISTORY_LIMIT":     "12",
		"UPSTREAM_TIMEOUT":  "2s",
		"MAX_BODY_BYTES":    "4096",
		"UPSTREAM_PREFIX":   "/shop",
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
	if cfg.AdminListenAddr != DefaultAdminListenAddr {
		t.Fatalf("admin listen = %s", cfg.AdminListenAddr)
	}
	if DashboardURL(cfg.AdminListenAddr) != "http://localhost:8081/_admin" {
		t.Fatalf("dashboard = %s", DashboardURL(cfg.AdminListenAddr))
	}
	if cfg.RulesPath != "tmp/rules.json" || cfg.HistoryLimit != 12 || cfg.UpstreamTimeout != 2*time.Second || cfg.MaxBufferedBody != 4096 {
		t.Fatalf("cfg = %+v", cfg)
	}
	if cfg.UpstreamPrefix != "/shop" {
		t.Fatalf("prefix = %s", cfg.UpstreamPrefix)
	}
	if cfg.UpstreamsPath != filepath.Join("tmp", "upstreams.json") {
		t.Fatalf("upstreams path = %s", cfg.UpstreamsPath)
	}
	if cfg.TokenPath != filepath.Join("tmp", "admin.token") {
		t.Fatalf("token path = %s", cfg.TokenPath)
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
	if cfg.AdminListenAddr != DefaultAdminListenAddr {
		t.Fatalf("admin listen = %s", cfg.AdminListenAddr)
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
	if len(lines) != 4 {
		t.Fatalf("lines = %#v", lines)
	}
	for _, line := range lines {
		if strings.Contains(line, "upstream") {
			t.Fatalf("startup line mentions upstreams before they are loaded: %q", line)
		}
	}
}

func TestLoadProxyAllowedHosts(t *testing.T) {
	cfg, err := Load(getenvFrom(map[string]string{"PROXY_ALLOWED_HOSTS": " Mac.Local , 192.168.1.20,,[fe80::1] "}))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"mac.local", "192.168.1.20", "fe80::1"}
	if strings.Join(cfg.ProxyAllowedHosts, ",") != strings.Join(want, ",") {
		t.Fatalf("hosts = %v", cfg.ProxyAllowedHosts)
	}
	cfg, err = Load(getenvFrom(nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.ProxyAllowedHosts) != 0 {
		t.Fatalf("default hosts = %v", cfg.ProxyAllowedHosts)
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
		{"public admin", map[string]string{"ADMIN_LISTEN_ADDR": "0.0.0.0:8081"}, "ADMIN_LISTEN_ADDR"},
		{"same port", map[string]string{"LISTEN_ADDR": "127.0.0.1:8080", "ADMIN_LISTEN_ADDR": "127.0.0.1:8080"}, "ADMIN_LISTEN_ADDR"},
		{"bad limit", map[string]string{"UPSTREAM_BASE_URL": "http://127.0.0.1:9", "HISTORY_LIMIT": "0"}, "HISTORY_LIMIT"},
		{"bad timeout", map[string]string{"UPSTREAM_BASE_URL": "http://127.0.0.1:9", "UPSTREAM_TIMEOUT": "soon"}, "UPSTREAM_TIMEOUT"},
		{"zero timeout", map[string]string{"UPSTREAM_BASE_URL": "http://127.0.0.1:9", "UPSTREAM_TIMEOUT": "0s"}, "UPSTREAM_TIMEOUT"},
		{"zero body", map[string]string{"MAX_BODY_BYTES": "0"}, "MAX_BODY_BYTES"},
		{"negative body", map[string]string{"MAX_BODY_BYTES": "-1"}, "MAX_BODY_BYTES"},
		{"text body", map[string]string{"MAX_BODY_BYTES": "eight"}, "MAX_BODY_BYTES"},
		{"bad prefix", map[string]string{"UPSTREAM_PREFIX": "/shop/"}, "UPSTREAM_PREFIX"},
		{"admin prefix", map[string]string{"UPSTREAM_PREFIX": "/_admin"}, "UPSTREAM_PREFIX"},
		{"host wildcard", map[string]string{"PROXY_ALLOWED_HOSTS": "*"}, "PROXY_ALLOWED_HOSTS"},
		{"host with port", map[string]string{"PROXY_ALLOWED_HOSTS": "dev.example:8080"}, "PROXY_ALLOWED_HOSTS"},
		{"host url", map[string]string{"PROXY_ALLOWED_HOSTS": "http://dev.example"}, "PROXY_ALLOWED_HOSTS"},
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
