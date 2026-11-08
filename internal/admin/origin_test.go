package admin

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Shayokh144/Mock_API_Service/internal/rules"
)

func TestSameOriginLoopbackNames(t *testing.T) {
	cases := []struct {
		name    string
		host    string
		origin  string
		referer string
		ok      bool
	}{
		{name: "localhost origin against 127.0.0.1", host: "127.0.0.1:8080", origin: "http://localhost:8080", ok: true},
		{name: "127.0.0.1 origin against localhost", host: "localhost:8080", origin: "http://127.0.0.1:8080", ok: true},
		{name: "ipv6 origin against localhost", host: "localhost:8080", origin: "http://[::1]:8080", ok: true},
		{name: "127.0.0.1 origin against ipv6 host", host: "[::1]:8080", origin: "http://127.0.0.1:8080", ok: true},
		{name: "missing origin with same-origin referer", host: "127.0.0.1:8080", referer: "http://localhost:8080/_admin/rules/new", ok: true},
		{name: "null origin with same-origin referer", host: "localhost:8080", origin: "null", referer: "http://[::1]:8080/_admin/rules/new", ok: true},
		{name: "foreign origin", host: "localhost:8080", origin: "http://evil.example", ok: false},
		{name: "foreign origin on the same port", host: "127.0.0.1:8080", origin: "http://evil.example:8080", ok: false},
		{name: "foreign origin wins over local referer", host: "127.0.0.1:8080", origin: "http://evil.example", referer: "http://localhost:8080/_admin", ok: false},
		{name: "foreign referer without origin", host: "localhost:8080", referer: "http://evil.example/phish", ok: false},
		{name: "different loopback port", host: "localhost:8080", origin: "http://127.0.0.1:9090", ok: false},
		{name: "neither header", host: "127.0.0.1:8080", ok: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "http://"+tc.host+"/_admin/rules", nil)
			req.Host = tc.host
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			if tc.referer != "" {
				req.Header.Set("Referer", tc.referer)
			}
			if sameOrigin(req) != tc.ok {
				t.Fatalf("sameOrigin() = %v, want %v", sameOrigin(req), tc.ok)
			}
		})
	}
}

func TestCreateRuleAcceptsLocalhostOrigin(t *testing.T) {
	dir := t.TempDir()
	store, err := rules.Load(filepath.Join(dir, "rules.json"))
	if err != nil {
		t.Fatal(err)
	}
	dashboard, err := New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(dashboard)
	defer srv.Close()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	body := url.Values{
		"name":         {"From browser"},
		"enabled":      {"on"},
		"method":       {"GET"},
		"path_pattern": {"/v1/items"},
		"status_code":  {"200"},
		"body":         {`{"ok":true}`},
	}
	target, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/_admin/rules", strings.NewReader(body.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Host = target.Host
	req.Header.Set("Origin", "http://localhost:"+target.Port())
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if len(store.List()) != 1 {
		t.Fatalf("rules = %d", len(store.List()))
	}
}
