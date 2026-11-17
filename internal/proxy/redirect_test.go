package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Shayokh144/Mock_API_Service/internal/rules"
	"github.com/Shayokh144/Mock_API_Service/internal/upstreams"
)

func TestUpstreamRedirectAndCookieRewrite(t *testing.T) {
	var upstreamBase *url.URL
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/abs":
			w.Header().Set("Location", upstreamBase.String()+"/next")
			w.Header().Set("Content-Location", upstreamBase.String()+"/file")
		case "/other":
			w.Header().Set("Location", "https://other.example/away")
		case "/rel":
			w.Header().Set("Location", "/next")
		case "/plain":
			w.Header().Set("Location", "next")
		case "/cookie":
			http.SetCookie(w, &http.Cookie{
				Name: "session", Value: "abc", Domain: upstreamBase.Hostname(),
				Path: "/", Secure: true, HttpOnly: true,
			})
			http.SetCookie(w, &http.Cookie{
				Name: "cdn", Value: "xyz", Domain: "cdn.example",
				Path: "/assets", Secure: true,
			})
		default:
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusFound)
	}))
	defer upstream.Close()
	var err error
	upstreamBase, err = url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	store := upstreams.NewMemory()
	if _, err := store.Create("staging", upstreamBase.String(), "/okya"); err != nil {
		t.Fatal(err)
	}
	proxySrv := httptest.NewServer(New(Options{Upstreams: store, Timeout: time.Second}))
	defer proxySrv.Close()
	client := noRedirectClient()

	abs := getNoRedirect(t, client, proxySrv.URL+"/okya/abs")
	if abs.StatusCode != http.StatusFound {
		t.Fatalf("abs status = %d", abs.StatusCode)
	}
	if got := abs.Header.Get("Location"); got != proxySrv.URL+"/okya/next" {
		t.Fatalf("Location = %s", got)
	}
	if got := abs.Header.Get("Content-Location"); got != proxySrv.URL+"/okya/file" {
		t.Fatalf("Content-Location = %s", got)
	}

	other := getNoRedirect(t, client, proxySrv.URL+"/okya/other")
	if other.Header.Get("Location") != "https://other.example/away" {
		t.Fatalf("third-party Location = %s", other.Header.Get("Location"))
	}
	rel := getNoRedirect(t, client, proxySrv.URL+"/okya/rel")
	if rel.Header.Get("Location") != "/next" {
		t.Fatalf("relative Location = %s", rel.Header.Get("Location"))
	}
	plain := getNoRedirect(t, client, proxySrv.URL+"/okya/plain")
	if plain.Header.Get("Location") != "next" {
		t.Fatalf("scheme-relative Location = %s", plain.Header.Get("Location"))
	}

	cookies := getNoRedirect(t, client, proxySrv.URL+"/okya/cookie")
	found := map[string]*http.Cookie{}
	for _, cookie := range cookies.Cookies() {
		found[cookie.Name] = cookie
	}
	session := found["session"]
	if session == nil || session.Domain != "" || session.Secure || session.Path != "/okya" || !session.HttpOnly || session.Value != "abc" {
		t.Fatalf("session cookie = %+v", session)
	}
	cdn := found["cdn"]
	if cdn == nil || cdn.Domain != "cdn.example" || cdn.Secure || cdn.Path != "/okya/assets" || cdn.Value != "xyz" {
		t.Fatalf("cdn cookie = %+v", cdn)
	}
}

func TestMockedLocationIsNotRewritten(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "should not be called", http.StatusBadGateway)
	}))
	defer upstream.Close()
	base, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	rulesStore := rules.NewMemory()
	if _, err := rulesStore.Create(rules.Draft{
		Name:        "Go away",
		Enabled:     true,
		Method:      "GET",
		PathPattern: "/v1/login",
		StatusCode:  http.StatusFound,
		Headers:     map[string]string{"Location": base.String() + "/next"},
		Body:        "",
	}); err != nil {
		t.Fatal(err)
	}
	proxySrv := httptest.NewServer(New(Options{Upstream: base, Timeout: time.Second, Rules: rulesStore}))
	defer proxySrv.Close()
	resp := getNoRedirect(t, noRedirectClient(), proxySrv.URL+"/v1/login")
	if resp.Header.Get("Location") != base.String()+"/next" {
		t.Fatalf("mocked Location = %s", resp.Header.Get("Location"))
	}
	if resp.Header.Get("X-Mock-Response") != "true" {
		t.Fatal("expected a mock")
	}
}

func TestRewriteLeavesDifferentSchemeAlone(t *testing.T) {
	upstream, err := url.Parse("http://staging.example.com")
	if err != nil {
		t.Fatal(err)
	}
	got, ok := rewriteAbsoluteURL("https://staging.example.com/next", "127.0.0.1:8080", upstream, "/okya")
	if ok || got != "https://staging.example.com/next" {
		t.Fatalf("rewritten = %q ok=%v", got, ok)
	}
	got, ok = rewriteAbsoluteURL("http://staging.example.com:80/next?x=1#y", "127.0.0.1:8080", upstream, "")
	if !ok || got != "http://127.0.0.1:8080/next?x=1#y" {
		t.Fatalf("default port rewrite = %q ok=%v", got, ok)
	}
	if !domainMatchesUpstream(".Staging.Example.COM", "staging.example.com") {
		t.Fatal("leading-dot domain should match")
	}
	if domainMatchesUpstream("example.com", "staging.example.com") {
		t.Fatal("parent domain should not match")
	}
}

func noRedirectClient() *http.Client {
	return &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
}

func getNoRedirect(t *testing.T, client *http.Client, rawURL string) *http.Response {
	t.Helper()
	resp, err := client.Get(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode == 0 {
		t.Fatal(strings.TrimSpace(rawURL))
	}
	return resp
}
