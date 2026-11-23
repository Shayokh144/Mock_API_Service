package proxy

import (
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/Shayokh144/Mock_API_Service/internal/history"
	"github.com/Shayokh144/Mock_API_Service/internal/upstreams"
)

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestLocationRewriteEdgeCases(t *testing.T) {
	auth := redirectTarget{prefix: "/okya-auth", base: mustURL(t, "https://auth.okya.example")}
	api := redirectTarget{prefix: "/okya", base: mustURL(t, "https://api.okya.example/api/v1")}
	app := redirectTarget{prefix: "/okya-app", base: mustURL(t, "https://app.okya.example")}
	proxy := "10.0.2.2:8080"
	cases := []struct {
		name    string
		raw     string
		targets []redirectTarget
		want    string
		changed bool
	}{
		{"root relative gets prefix once", "/dashboard", []redirectTarget{auth}, "/okya-auth/dashboard", true},
		{"root path maps to the bare prefix", "/", []redirectTarget{auth}, "/okya-auth", true},
		{"already prefixed is not doubled", "/okya-auth/dashboard", []redirectTarget{auth}, "/okya-auth/dashboard", false},
		{"prefix-like segment is not treated as the prefix", "/okya-authx/a", []redirectTarget{auth}, "/okya-auth/okya-authx/a", true},
		{"query and fragment kept", "/home?x=1#top", []redirectTarget{auth}, "/okya-auth/home?x=1#top", true},
		{"absolute same host", "https://auth.okya.example/dashboard?x=1", []redirectTarget{auth}, "http://10.0.2.2:8080/okya-auth/dashboard?x=1", true},
		{"absolute already prefixed is not doubled", "https://auth.okya.example/okya-auth/x", []redirectTarget{auth}, "http://10.0.2.2:8080/okya-auth/x", true},
		{"scheme-relative same host", "//auth.okya.example/dashboard", []redirectTarget{auth}, "http://10.0.2.2:8080/okya-auth/dashboard", true},
		{"base path is not doubled on absolute", "https://api.okya.example/api/v1/me", []redirectTarget{api}, "http://10.0.2.2:8080/okya/me", true},
		{"base path is not doubled on root relative", "/api/v1/me", []redirectTarget{api}, "/okya/me", true},
		{"path outside base keeps its path", "/health", []redirectTarget{api}, "/okya/health", true},
		{"redirect to another configured upstream", "https://app.okya.example/home", []redirectTarget{auth, app}, "http://10.0.2.2:8080/okya-app/home", true},
		{"answering upstream wins a shared host", "https://auth.okya.example/x", []redirectTarget{auth, {prefix: "/other", base: mustURL(t, "https://auth.okya.example")}}, "http://10.0.2.2:8080/okya-auth/x", true},
		{"unknown host untouched", "https://elsewhere.example/x", []redirectTarget{auth, app}, "https://elsewhere.example/x", false},
		{"scheme mismatch untouched", "http://auth.okya.example/x", []redirectTarget{auth}, "http://auth.okya.example/x", false},
		{"app scheme untouched", "okya://callback?code=1", []redirectTarget{auth}, "okya://callback?code=1", false},
		{"path relative untouched", "dashboard", []redirectTarget{auth}, "dashboard", false},
	}
	for _, tc := range cases {
		got, changed := rewriteLocationTo(tc.raw, proxy, tc.targets)
		if got != tc.want || changed != tc.changed {
			t.Errorf("%s: rewrite(%q) = %q changed=%v, want %q changed=%v", tc.name, tc.raw, got, changed, tc.want, tc.changed)
		}
	}
}

func TestCookiePathDropsBasePath(t *testing.T) {
	got := rewriteSetCookie("s=1; Path=/api/v1", "/okya", "/api/v1")
	if got != "s=1; Path=/okya" {
		t.Fatalf("cookie = %q", got)
	}
	got = rewriteSetCookie("s=1; Path=/api/v1/users", "", "/api/v1")
	if got != "s=1; Path=/users" {
		t.Fatalf("unprefixed cookie = %q", got)
	}
	got = rewriteSetCookie("s=1", "/okya", "/api/v1")
	if got != "s=1" {
		t.Fatalf("no path = %q", got)
	}
}

// After a cookie-routed POST /login, the redirect must land on the same
// upstream once, whether the Location is relative or absolute, and a
// redirect to another configured upstream must move the sticky cookie.
func TestLoginRedirectStaysOnProxy(t *testing.T) {
	var appPaths []string
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		appPaths = append(appPaths, r.URL.Path)
		_, _ = io.WriteString(w, "app home")
	}))
	defer app.Close()
	var authPaths []string
	var authURL string
	auth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authPaths = append(authPaths, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/login":
			_, _ = io.WriteString(w, "form")
		case r.Method == http.MethodPost && r.URL.Path == "/login":
			if r.URL.Query().Get("to") == "app" {
				w.Header().Set("Location", app.URL+"/home")
			} else {
				w.Header().Set("Location", authURL+"/dashboard")
			}
			w.WriteHeader(http.StatusSeeOther)
		case r.URL.Path == "/dashboard":
			_, _ = io.WriteString(w, "dashboard")
		default:
			http.NotFound(w, r)
		}
	}))
	defer auth.Close()
	authURL = auth.URL

	ups := upstreams.NewMemory()
	if _, err := ups.Create("OKYA_AUTH", auth.URL, "/okya-auth"); err != nil {
		t.Fatal(err)
	}
	appUp, err := ups.Create("OKYA_APP", app.URL, "/okya-app")
	if err != nil {
		t.Fatal(err)
	}
	log := history.New(20)
	proxySrv := httptest.NewServer(New(Options{Upstreams: ups, History: log, Timeout: time.Second}))
	defer proxySrv.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	do := func(method, path, referer string) (*http.Response, string) {
		t.Helper()
		req, _ := http.NewRequest(method, proxySrv.URL+path, nil)
		if referer != "" {
			req.Header.Set("Referer", proxySrv.URL+referer)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return resp, string(body)
	}

	do("GET", "/okya-auth/start", "")
	if resp, _ := do("GET", "/login", "/okya-auth/start"); resp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d", resp.StatusCode)
	}
	resp, body := do("POST", "/login", "/login")
	if resp.StatusCode != http.StatusOK || body != "dashboard" || resp.Request.URL.Path != "/okya-auth/dashboard" {
		t.Fatalf("after login: status=%d body=%q url=%s auth=%v", resp.StatusCode, body, resp.Request.URL, authPaths)
	}

	resp, body = do("POST", "/login?to=app", "/login")
	if resp.StatusCode != http.StatusOK || body != "app home" || resp.Request.URL.Path != "/okya-app/home" {
		t.Fatalf("cross upstream: status=%d body=%q url=%s", resp.StatusCode, body, resp.Request.URL)
	}
	if len(appPaths) == 0 || appPaths[len(appPaths)-1] != "/home" {
		t.Fatalf("app saw %v", appPaths)
	}
	u, _ := url.Parse(proxySrv.URL)
	for _, cookie := range jar.Cookies(u) {
		if cookie.Name == stickyCookieName && cookie.Value != appUp.ID {
			t.Fatalf("sticky cookie = %s, want the app upstream", cookie.Value)
		}
	}
}
