package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Shayokh144/Mock_API_Service/internal/history"
	"github.com/Shayokh144/Mock_API_Service/internal/upstreams"
)

const (
	appCallback = "co.okya.pos.dev://callback?code=abc&state=xyz"
	intentURL   = "intent://callback?code=abc#Intent;scheme=co.okya.pos.dev;package=co.okya.pos.dev;end"
	appBody     = `<a href="co.okya.pos.dev://callback?code=abc&amp;state=xyz">Open app</a>` +
		`<form action="/oauth/authorize?redirect_uri=co.okya.pos.dev%3A%2F%2Fcallback&amp;state=xyz">` +
		`<script>location.href="intent://callback#Intent;scheme=co.okya.pos.dev;end"</script>`
)

// An OAuth sign-in ends with a redirect to the Flutter app's own scheme.
// That URL, its query, the Refresh header, and the body must reach the
// browser byte for byte, however the request was routed.
func TestAppSchemeRedirectPassesThrough(t *testing.T) {
	var authURL string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/callback-redirect":
			w.Header().Set("Location", appCallback)
			w.WriteHeader(http.StatusFound)
		case "/intent-redirect":
			w.Header().Set("Location", intentURL)
			w.WriteHeader(http.StatusFound)
		case "/authorize":
			w.Header().Set("Location", authURL+"/oauth/authorize?redirect_uri=co.okya.pos.dev%3A%2F%2Fcallback&state=xyz")
			w.WriteHeader(http.StatusFound)
		case "/page":
			w.Header().Set("Refresh", "0; url="+appCallback)
			w.Header().Set("Content-Type", "text/html")
			_, _ = io.WriteString(w, appBody)
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	authURL = upstream.URL

	ups := upstreams.NewMemory()
	okya, err := ups.Create("OKYA_AUTH", upstream.URL, "/okya-auth")
	if err != nil {
		t.Fatal(err)
	}
	log := history.New(20)
	proxySrv := httptest.NewServer(New(Options{Upstreams: ups, History: log, Timeout: time.Second}))
	defer proxySrv.Close()
	client := noRedirectClient()

	send := func(path, referer string, sticky bool) (*http.Response, string) {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, proxySrv.URL+path, nil)
		if referer != "" {
			req.Header.Set("Referer", proxySrv.URL+referer)
		}
		if sticky {
			req.AddCookie(&http.Cookie{Name: stickyCookieName, Value: okya.ID})
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return resp, string(body)
	}

	routes := []struct {
		name, prefix, referer string
		sticky                bool
	}{
		{"prefixed", "/okya-auth", "", false},
		{"via Referer", "", "/okya-auth/login", false},
		{"via cookie", "", "/login", true},
	}
	for _, route := range routes {
		resp, _ := send(route.prefix+"/callback-redirect", route.referer, route.sticky)
		if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != appCallback {
			t.Fatalf("%s: status=%d Location=%q", route.name, resp.StatusCode, resp.Header.Get("Location"))
		}
		resp, _ = send(route.prefix+"/intent-redirect", route.referer, route.sticky)
		if resp.Header.Get("Location") != intentURL {
			t.Fatalf("%s: intent Location=%q", route.name, resp.Header.Get("Location"))
		}
		resp, _ = send(route.prefix+"/authorize", route.referer, route.sticky)
		want := proxySrv.URL[len("http://"):]
		if got := resp.Header.Get("Location"); got != "http://"+want+"/okya-auth/oauth/authorize?redirect_uri=co.okya.pos.dev%3A%2F%2Fcallback&state=xyz" {
			t.Fatalf("%s: authorize Location=%q, the redirect_uri query must be kept byte for byte", route.name, got)
		}
		resp, body := send(route.prefix+"/page", route.referer, route.sticky)
		if body != appBody {
			t.Fatalf("%s: body changed:\n%s", route.name, body)
		}
		if got := resp.Header.Get("Refresh"); got != "0; url="+appCallback {
			t.Fatalf("%s: Refresh=%q", route.name, got)
		}
	}

	for _, entry := range log.List() {
		if entry.Path == "/okya-auth/callback-redirect" && entry.ResponseHeaders.Get("Location") != appCallback {
			t.Fatalf("history Location = %q", entry.ResponseHeaders.Get("Location"))
		}
	}
}

func TestNonWebSchemesAreNeverRewritten(t *testing.T) {
	base := mustURL(t, "https://auth.okya.example")
	targets := []redirectTarget{{prefix: "/okya-auth", base: base}}
	for _, raw := range []string{
		appCallback,
		"CO.OKYA.POS.DEV://callback?code=abc",
		intentURL,
		"co.okya.pos.dev:/callback?code=abc",
		"co.okya.pos.dev:callback",
		"mailto:help@okya.example",
		"tel:+6600000000",
		"javascript:void(0)",
		"data:text/plain,hi",
		"ftp://auth.okya.example/x",
		"ws://auth.okya.example/x",
	} {
		got, changed := rewriteLocationTo(raw, "10.0.2.2:8080", targets)
		if changed || got != raw {
			t.Errorf("rewrite(%q) = %q changed=%v", raw, got, changed)
		}
	}
}
