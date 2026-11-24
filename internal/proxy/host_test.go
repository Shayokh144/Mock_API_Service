package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Shayokh144/Mock_API_Service/internal/history"
)

func TestLocalHosts(t *testing.T) {
	allowed := LocalHosts([]string{"Mac.Local.", "[fe80::1]"})
	cases := []struct {
		host string
		want bool
	}{
		{"localhost:8080", true},
		{"LOCALHOST", true},
		{"app.localhost:8080", true},
		{"127.0.0.1:8080", true},
		{"127.0.0.2:8080", true},
		{"[::1]:8080", true},
		{"10.0.2.2:8080", true},
		{"10.0.3.2:8080", true},
		{"mac.local:8080", true},
		{"[fe80::1]:8080", true},
		{"", false},
		{"evil.example:8080", false},
		{"localhost.evil.example:8080", false},
		{"10.0.2.3:8080", false},
		{"192.168.1.20:8080", false},
	}
	for _, tc := range cases {
		if got := allowed(tc.host); got != tc.want {
			t.Errorf("Host %q allowed = %v, want %v", tc.host, got, tc.want)
		}
	}
}

func TestProxyRejectsRebindingHost(t *testing.T) {
	called := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		_, _ = io.WriteString(w, "secret")
	}))
	defer upstream.Close()
	base, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	log := history.New(10)
	proxySrv := httptest.NewServer(New(Options{Upstream: base, History: log, Timeout: time.Second, HostAllowed: LocalHosts(nil)}))
	defer proxySrv.Close()

	req, err := http.NewRequest(http.MethodGet, proxySrv.URL+"/v1/me", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "evil.example:8080"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden || !strings.Contains(string(body), "PROXY_ALLOWED_HOSTS") {
		t.Fatalf("rebinding Host = %d %s", resp.StatusCode, body)
	}
	if called {
		t.Fatal("a refused Host reached the upstream")
	}
	entries := log.List()
	if len(entries) != 1 || entries[0].Source != history.SourceRejected || entries[0].Status != http.StatusForbidden {
		t.Fatalf("history = %+v", entries)
	}

	req.Host = "10.0.2.2:8080"
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "secret" {
		t.Fatalf("emulator Host = %d %s", resp.StatusCode, body)
	}
}
