// Package config loads the proxy's local settings from the environment.
package config

import (
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultListenAddr      = "127.0.0.1:8080"
	DefaultAdminListenAddr = "127.0.0.1:8081"
	DefaultRulesPath       = "data/rules.json"
	DefaultHistoryLimit    = 500
	DefaultUpstreamTimeout = 30 * time.Second
	ReadHeaderTimeout      = 10 * time.Second

	// MaxMockBody is the largest JSON body a rule may store.
	MaxMockBody = 1 << 20
	// MaxBufferedBody is the largest request or upstream response the proxy buffers.
	MaxBufferedBody = 8 << 20
	// HistoryBodyPreview is how much of a body is kept on a history entry.
	HistoryBodyPreview = 64 << 10
)

// Config is the validated process configuration.
type Config struct {
	Upstream        *url.URL
	ListenAddr      string
	AdminListenAddr string
	RulesPath       string
	UpstreamsPath   string
	HistoryLimit    int
	UpstreamTimeout time.Duration
}

// Load reads configuration from getenv. Empty optional variables use defaults.
// A returned error names the environment variable that failed.
func Load(getenv func(string) string) (Config, error) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}

	upstream, err := parseUpstream(getenv("UPSTREAM_BASE_URL"))
	if err != nil {
		return Config{}, err
	}
	listenAddr, err := resolveListenAddr(getenv("LISTEN_ADDR"), "LISTEN_ADDR", DefaultListenAddr)
	if err != nil {
		return Config{}, err
	}
	adminAddr, err := resolveListenAddr(getenv("ADMIN_LISTEN_ADDR"), "ADMIN_LISTEN_ADDR", DefaultAdminListenAddr)
	if err != nil {
		return Config{}, err
	}
	if listenAddr == adminAddr {
		return Config{}, fmt.Errorf("ADMIN_LISTEN_ADDR must use a different address from LISTEN_ADDR")
	}
	historyLimit, err := parseHistoryLimit(getenv("HISTORY_LIMIT"))
	if err != nil {
		return Config{}, err
	}
	timeout, err := parseTimeout(getenv("UPSTREAM_TIMEOUT"))
	if err != nil {
		return Config{}, err
	}

	rulesPath := getenv("RULES_PATH")
	if strings.TrimSpace(rulesPath) == "" {
		rulesPath = DefaultRulesPath
	}

	return Config{
		Upstream:        upstream,
		ListenAddr:      listenAddr,
		AdminListenAddr: adminAddr,
		RulesPath:       rulesPath,
		UpstreamsPath:   UpstreamsPath(rulesPath),
		HistoryLimit:    historyLimit,
		UpstreamTimeout: timeout,
	}, nil
}

// UpstreamsPath is the upstream file stored next to the rules file.
func UpstreamsPath(rulesPath string) string {
	if strings.TrimSpace(rulesPath) == "" {
		rulesPath = DefaultRulesPath
	}
	return filepath.Join(filepath.Dir(rulesPath), "upstreams.json")
}

// StartupLines are the human-readable lines printed before the process listens.
func StartupLines(cfg Config) []string {
	lines := []string{
		"listening on " + cfg.ListenAddr,
		"admin listening on " + cfg.AdminListenAddr,
		"dashboard at " + DashboardURL(cfg.AdminListenAddr),
	}
	if cfg.Upstream == nil {
		lines = append(lines, "no upstream configured; add one at the dashboard")
	}
	return lines
}

// DashboardURL is the local browser URL for the resolved listen address.
func DashboardURL(listenAddr string) string {
	host, port, err := net.SplitHostPort(listenAddr)
	if err != nil {
		return "http://" + listenAddr + "/_admin"
	}
	if host == "127.0.0.1" {
		host = "localhost"
	}
	return "http://" + net.JoinHostPort(host, port) + "/_admin"
}

func parseUpstream(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	u, err := url.Parse(raw)
	if err != nil || u == nil || (u.Host == "" && u.Scheme == "") {
		return nil, fmt.Errorf("UPSTREAM_BASE_URL is invalid")
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("UPSTREAM_BASE_URL must be http or https")
	}
	if u.Hostname() == "" {
		return nil, fmt.Errorf("UPSTREAM_BASE_URL must include a host")
	}
	if u.RawQuery != "" {
		return nil, fmt.Errorf("UPSTREAM_BASE_URL must not include a query")
	}
	if u.Fragment != "" {
		return nil, fmt.Errorf("UPSTREAM_BASE_URL must not include a fragment")
	}
	u.Scheme = scheme
	return u, nil
}

func resolveListenAddr(raw, envName, fallback string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = fallback
	}
	host, port, err := net.SplitHostPort(raw)
	if err != nil || port == "" {
		return "", fmt.Errorf("%s is invalid", envName)
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return "", fmt.Errorf("%s is invalid", envName)
	}

	switch strings.ToLower(host) {
	case "localhost":
		host = "127.0.0.1"
	case "":
		return "", fmt.Errorf("%s must be a loopback address", envName)
	}

	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return "", fmt.Errorf("%s must be a loopback address", envName)
	}
	return net.JoinHostPort(ip.String(), port), nil
}

func parseHistoryLimit(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return DefaultHistoryLimit, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("HISTORY_LIMIT must be a positive integer")
	}
	return n, nil
}

func parseTimeout(raw string) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return DefaultUpstreamTimeout, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("UPSTREAM_TIMEOUT is invalid")
	}
	if d <= 0 {
		return 0, fmt.Errorf("UPSTREAM_TIMEOUT must be greater than zero")
	}
	return d, nil
}
