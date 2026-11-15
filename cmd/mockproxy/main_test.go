package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunRejectsNonLoopbackBeforeListen(t *testing.T) {
	called := false
	err := run(func(key string) string {
		switch key {
		case "UPSTREAM_BASE_URL":
			return "http://127.0.0.1:9"
		case "LISTEN_ADDR":
			return "0.0.0.0:8080"
		default:
			return ""
		}
	}, io.Discard, func(string, http.Handler) error {
		called = true
		return nil
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if called {
		t.Fatal("listen was called")
	}
	if !strings.Contains(err.Error(), "LISTEN_ADDR") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunLogsListenAddressAndDashboard(t *testing.T) {
	var stderr bytes.Buffer
	var mu sync.Mutex
	var addrs []string
	dir := t.TempDir()
	err := run(func(key string) string {
		switch key {
		case "UPSTREAM_BASE_URL":
			return "https://upstream.example.com"
		case "RULES_PATH":
			return filepath.Join(dir, "rules.json")
		default:
			return ""
		}
	}, &stderr, func(addr string, h http.Handler) error {
		if h == nil {
			t.Fatal("nil handler")
		}
		mu.Lock()
		addrs = append(addrs, addr)
		mu.Unlock()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(addrs, " ")
	if !strings.Contains(joined, "127.0.0.1:8080") || !strings.Contains(joined, "127.0.0.1:8081") {
		t.Fatalf("addrs = %v", addrs)
	}
	text := stderr.String()
	if !strings.Contains(text, "listening on 127.0.0.1:8080") {
		t.Fatalf("stderr = %q", text)
	}
	if !strings.Contains(text, "admin listening on 127.0.0.1:8081") {
		t.Fatalf("stderr = %q", text)
	}
	if !strings.Contains(text, "dashboard at http://localhost:8081/_admin") {
		t.Fatalf("stderr = %q", text)
	}
}

func TestServersSetReadHeaderTimeout(t *testing.T) {
	proxySrv := httpServer("127.0.0.1:8080", http.NewServeMux())
	adminSrv := httpServer("127.0.0.1:8081", http.NewServeMux())
	if proxySrv.ReadHeaderTimeout != 10*time.Second || adminSrv.ReadHeaderTimeout != 10*time.Second {
		t.Fatalf("timeouts = %s %s", proxySrv.ReadHeaderTimeout, adminSrv.ReadHeaderTimeout)
	}
	if proxySrv.Addr == adminSrv.Addr {
		t.Fatal("listeners share an address")
	}
}

func TestRunInvalidRulesFileDoesNotListenOrRewrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.json")
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	called := false
	err := run(func(key string) string {
		switch key {
		case "UPSTREAM_BASE_URL":
			return "http://127.0.0.1:9"
		case "RULES_PATH":
			return path
		default:
			return ""
		}
	}, io.Discard, func(string, http.Handler) error {
		called = true
		return nil
	})
	if err == nil || called {
		t.Fatalf("err=%v called=%v", err, called)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "{" {
		t.Fatalf("file = %s", raw)
	}
}

func TestRunMissingUpstreamStillListens(t *testing.T) {
	var stderr bytes.Buffer
	var called atomic.Bool
	dir := t.TempDir()
	err := run(func(key string) string {
		if key == "RULES_PATH" {
			return filepath.Join(dir, "rules.json")
		}
		return ""
	}, &stderr, func(string, http.Handler) error {
		called.Store(true)
		return nil
	})
	if err != nil || !called.Load() {
		t.Fatalf("err=%v called=%v", err, called.Load())
	}
	if !strings.Contains(stderr.String(), "no upstream configured") {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "upstreams.json")); !os.IsNotExist(err) {
		t.Fatal("missing env created an upstreams file")
	}
}

func TestRunSeedsUpstreamOnce(t *testing.T) {
	dir := t.TempDir()
	rulesPath := filepath.Join(dir, "rules.json")
	getenv := func(key string) string {
		switch key {
		case "UPSTREAM_BASE_URL":
			return "https://staging.example.com/v1"
		case "RULES_PATH":
			return rulesPath
		default:
			return ""
		}
	}
	if err := run(getenv, io.Discard, func(string, http.Handler) error { return nil }); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "upstreams.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "https://staging.example.com/v1") || !strings.Contains(string(raw), "staging.example.com") {
		t.Fatalf("file = %s", raw)
	}
	getenv = func(key string) string {
		switch key {
		case "UPSTREAM_BASE_URL":
			return "https://other.example.com"
		case "RULES_PATH":
			return rulesPath
		default:
			return ""
		}
	}
	if err := run(getenv, io.Discard, func(string, http.Handler) error { return nil }); err != nil {
		t.Fatal(err)
	}
	again, err := os.ReadFile(filepath.Join(dir, "upstreams.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(raw) {
		t.Fatalf("seed replaced the file:\n%s", again)
	}
}

func TestScenarioFlagMergesWithoutWritingRules(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "demo.yaml")
	raw := []byte("version: 1\nname: demo\ndescription: sample\nrules:\n  - name: ping\n    method: GET\n    path: /v1/ping\n    status: 200\n    body:\n      ok: true\n")
	if err := os.WriteFile(file, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	scenarioFile = file
	scenarioPersist = false
	t.Cleanup(func() {
		scenarioFile = ""
		scenarioPersist = false
	})
	rulesPath := filepath.Join(dir, "rules.json")
	var proxy http.Handler
	err := run(func(key string) string {
		switch key {
		case "RULES_PATH":
			return rulesPath
		case "UPSTREAMS_PATH":
			return filepath.Join(dir, "upstreams.json")
		default:
			return ""
		}
	}, io.Discard, func(addr string, handler http.Handler) error {
		if strings.HasSuffix(addr, ":8080") {
			proxy = handler
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if proxy == nil {
		t.Fatal("proxy did not start")
	}
	rr := httptest.NewRecorder()
	proxy.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/ping", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	if _, err := os.Stat(rulesPath); !os.IsNotExist(err) {
		t.Fatal("scenario wrote rules.json")
	}
}
