package proxy

import (
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Shayokh144/Mock_API_Service/internal/history"
	"github.com/Shayokh144/Mock_API_Service/internal/upstreams"
)

// railsLike behaves like a Rails app with protect_from_forgery and
// forgery_protection_origin_check. GET /login sets the session cookie that
// holds the CSRF secret. POST /login returns 422 unless Origin equals the
// scheme and host the app sees, X-Forwarded-Host does not move that host,
// and the session cookie and its token come back.
func railsLike(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/login":
			http.SetCookie(w, &http.Cookie{Name: "_shop_session", Value: "sid-1", Path: "/", Domain: "shop.example", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
			http.SetCookie(w, &http.Cookie{Name: "__Host-csrf", Value: "c1", Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteNoneMode})
			_, _ = io.WriteString(w, `<form method="post" action="/login"><input name="authenticity_token" value="tok-sid-1">`)
		case r.Method == http.MethodPost && r.URL.Path == "/login":
			host := r.Host
			if forwarded := r.Header.Get("X-Forwarded-Host"); forwarded != "" {
				host = forwarded
			}
			baseURL := "http://" + host
			if got := r.Header.Get("Origin"); got != baseURL {
				http.Error(w, "origin "+got+" does not match "+baseURL, http.StatusUnprocessableEntity)
				return
			}
			if ref := r.Header.Get("Referer"); !strings.HasPrefix(ref, baseURL+"/") {
				http.Error(w, "referer "+ref, http.StatusUnprocessableEntity)
				return
			}
			session, err := r.Cookie("_shop_session")
			if err != nil || session.Value != "sid-1" {
				http.Error(w, "no session", http.StatusUnprocessableEntity)
				return
			}
			if csrf, err := r.Cookie("__Host-csrf"); err != nil || csrf.Value != "c1" {
				http.Error(w, "no __Host-csrf", http.StatusUnprocessableEntity)
				return
			}
			if _, err := r.Cookie(stickyCookieName); err == nil {
				http.Error(w, "proxy cookie leaked", http.StatusBadRequest)
				return
			}
			if err := r.ParseForm(); err != nil || r.PostForm.Get("authenticity_token") != "tok-"+session.Value {
				http.Error(w, "InvalidAuthenticityToken", http.StatusUnprocessableEntity)
				return
			}
			w.Header().Set("Location", "/dashboard")
			w.WriteHeader(http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestRailsLoginPassesCSRFThroughProxy(t *testing.T) {
	upstream := railsLike(t)
	defer upstream.Close()
	ups := upstreams.NewMemory()
	shop, err := ups.Create("SHOP_AUTH_DEV", upstream.URL, "/shop-auth")
	if err != nil {
		t.Fatal(err)
	}
	log := history.New(10)
	proxySrv := httptest.NewServer(New(Options{Upstreams: ups, History: log, Timeout: time.Second}))
	defer proxySrv.Close()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := noRedirectClient()
	client.Jar = jar

	page, err := client.Get(proxySrv.URL + "/shop-auth/login")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, page.Body)
	_ = page.Body.Close()
	names := map[string]*http.Cookie{}
	for _, cookie := range page.Cookies() {
		names[cookie.Name] = cookie
	}
	session := names["mp_"+shop.ID+"__shop_session"]
	if session == nil || session.Domain != "" || session.Secure {
		t.Fatalf("session cookie = %+v", session)
	}
	csrf := names["mp_"+shop.ID+"___Host-csrf"]
	if csrf == nil || csrf.Secure || csrf.SameSite != http.SameSiteLaxMode {
		t.Fatalf("csrf cookie = %+v (all %v)", csrf, page.Cookies())
	}

	form := url.Values{"authenticity_token": {"tok-sid-1"}, "email": {"a@b.c"}, "password": {"secret"}}
	req, err := http.NewRequest(http.MethodPost, proxySrv.URL+"/shop-auth/login", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", proxySrv.URL)
	req.Header.Set("Referer", proxySrv.URL+"/shop-auth/login")
	req.Header.Set("X-Forwarded-Host", "evil.example")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d body = %s", resp.StatusCode, body)
	}
	if got := resp.Header.Get("Location"); got != "/shop-auth/dashboard" {
		t.Fatalf("Location = %s", got)
	}

	base, _ := url.Parse(upstream.URL)
	var post history.Entry
	for _, entry := range log.List() {
		if entry.Method == http.MethodPost {
			post = entry
		}
	}
	if post.RequestHeaders.Get("Origin") != proxySrv.URL {
		t.Fatalf("history should keep the app's Origin, got %q", post.RequestHeaders.Get("Origin"))
	}
	sent := post.UpstreamHeaders
	if sent.Get("Origin") != "http://"+base.Host || sent.Get("Referer") != "http://"+base.Host+"/login" || sent.Get("Host") != base.Host {
		t.Fatalf("sent upstream = %v", sent)
	}
	if sent.Get("Cookie") != "" {
		t.Fatal("cookies must not be copied into the sent-upstream headers")
	}
}

func TestOutboundLeavesForeignOriginAlone(t *testing.T) {
	upstream, _ := url.Parse("https://auth.shop.example:443/base")
	header := http.Header{}
	header.Set("Origin", "https://other.example")
	header.Set("Referer", "https://other.example/page")
	rewriteOutbound(header, "10.0.2.2:8080", upstream, "/shop", "")
	if header.Get("Origin") != "https://other.example" || header.Get("Referer") != "https://other.example/page" {
		t.Fatalf("foreign values changed: %v", header)
	}
	header.Set("Origin", "http://10.0.2.2:8080")
	header.Set("Referer", "http://10.0.2.2:8080/shop/login?step=2#x")
	header.Set("Forwarded", "host=evil")
	rewriteOutbound(header, "10.0.2.2:8080", upstream, "/shop", "")
	if got := header.Get("Origin"); got != "https://auth.shop.example" {
		t.Fatalf("Origin = %s", got)
	}
	if got := header.Get("Referer"); got != "https://auth.shop.example/base/login?step=2" {
		t.Fatalf("Referer = %s", got)
	}
	if header.Get("Forwarded") != "" {
		t.Fatal("Forwarded should be dropped")
	}
	header.Set("Referer", "http://10.0.2.2:8080/login")
	rewriteOutbound(header, "10.0.2.2:8080", upstream, "/shop", "")
	if got := header.Get("Referer"); got != "https://auth.shop.example/base/login" {
		t.Fatalf("unprefixed Referer = %s", got)
	}
}

func TestRequestCookieNamesAreRestored(t *testing.T) {
	header := http.Header{"Cookie": {"__mp_Host-csrf=c1; a=1", "__mp_Secure-id=2"}}
	rewriteRequestCookies(header, "")
	if got := header.Get("Cookie"); got != "__Host-csrf=c1; a=1; __Secure-id=2" {
		t.Fatalf("cookie = %q", got)
	}
}
