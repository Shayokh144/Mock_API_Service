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
		site    string
		ok      bool
	}{
		{name: "localhost origin against 127.0.0.1", host: "127.0.0.1:8080", origin: "http://localhost:8080", ok: true},
		{name: "127.0.0.1 origin against localhost", host: "localhost:8080", origin: "http://127.0.0.1:8080", ok: true},
		{name: "ipv6 origin against localhost", host: "localhost:8080", origin: "http://[::1]:8080", ok: true},
		{name: "127.0.0.1 origin against ipv6 host", host: "[::1]:8080", origin: "http://127.0.0.1:8080", ok: true},
		{name: "missing origin with same-origin referer", host: "127.0.0.1:8080", referer: "http://localhost:8080/_admin/rules/new", ok: true},
		{name: "null origin with same-origin referer", host: "localhost:8080", origin: "null", referer: "http://[::1]:8080/_admin/rules/new", ok: true},
		{name: "null origin with Sec-Fetch-Site same-origin", host: "127.0.0.1:8080", origin: "null", site: "same-origin", ok: true},
		{name: "null origin with Sec-Fetch-Site none", host: "127.0.0.1:8080", origin: "null", site: "none", ok: true},
		{name: "null origin with Sec-Fetch-Site cross-site", host: "127.0.0.1:8080", origin: "null", site: "cross-site", ok: false},
		{name: "null origin alone", host: "127.0.0.1:8080", origin: "null", ok: false},
		{name: "foreign origin", host: "localhost:8080", origin: "http://evil.example", ok: false},
		{name: "foreign origin on the same port", host: "127.0.0.1:8080", origin: "http://evil.example:8080", ok: false},
		{name: "foreign origin wins over local referer", host: "127.0.0.1:8080", origin: "http://evil.example", referer: "http://localhost:8080/_admin", ok: false},
		{name: "foreign origin with same-origin fetch site", host: "127.0.0.1:8080", origin: "http://evil.example:8080", site: "same-origin", ok: false},
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
			if tc.site != "" {
				req.Header.Set("Sec-Fetch-Site", tc.site)
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

func TestCreateRuleAcceptsChromeSameOriginPost(t *testing.T) {
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

	form, err := client.Get(srv.URL + "/_admin/rules/new")
	if err != nil {
		t.Fatal(err)
	}
	form.Body.Close()
	policy := form.Header.Get("Referrer-Policy")
	if policy != "same-origin" {
		t.Fatalf("Referrer-Policy = %q", policy)
	}

	page, err := url.Parse(srv.URL + "/_admin/rules/new")
	if err != nil {
		t.Fatal(err)
	}
	headers := chromeSameOriginFormHeaders(page)
	if headers.Get("Origin") != srv.URL || headers.Get("Origin") == "null" {
		t.Fatalf("Origin = %q, want %s", headers.Get("Origin"), srv.URL)
	}
	if headers.Get("Referer") != page.String() {
		t.Fatalf("Referer = %q, want %s", headers.Get("Referer"), page.String())
	}
	body := url.Values{
		"name":         {"Browser"},
		"enabled":      {"on"},
		"method":       {"GET"},
		"path_pattern": {"/v1/browser"},
		"status_code":  {"201"},
		"body":         {`{"ok":true}`},
	}
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/_admin/rules", strings.NewReader(body.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Host = page.Host
	req.Header = headers
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("Chrome post status = %d, origin %q", resp.StatusCode, headers.Get("Origin"))
	}
	if len(store.List()) != 1 {
		t.Fatalf("rules = %d", len(store.List()))
	}
}

// chromeSameOriginFormHeaders is what Google Chrome on macOS sends for a
// clicked same-origin form POST when the form page's Referrer-Policy is
// same-origin.
//
// Origin is the page origin, not "null". That policy serializes Origin as
// null only for a cross-origin request. Referer is the full document URL
// with the fragment removed. Chrome omits Referer for a cross-origin request
// under this policy. Sec-Fetch-User is ?1 because the submit is a user gesture.
func chromeSameOriginFormHeaders(page *url.URL) http.Header {
	origin := &url.URL{Scheme: page.Scheme, Host: page.Host}
	referer := *page
	referer.Fragment = ""
	referer.User = nil
	header := make(http.Header)
	header.Set("Content-Type", "application/x-www-form-urlencoded")
	header.Set("Origin", origin.String())
	header.Set("Referer", referer.String())
	header.Set("Sec-Fetch-Site", "same-origin")
	header.Set("Sec-Fetch-Mode", "navigate")
	header.Set("Sec-Fetch-Dest", "document")
	header.Set("Sec-Fetch-User", "?1")
	return header
}
