package proxy

import (
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Shayokh144/Mock_API_Service/internal/history"
	"github.com/Shayokh144/Mock_API_Service/internal/upstreams"
)

const authorizeQuery = "response_type=code&client_id=pos&redirect_uri=co.okya.pos.dev%3A%2F%2Fcallback&scope=openid+profile+email&prompt=login"

// okyaAuth behaves like the OKYA Rails auth server. The session cookie
// stores return_to on /oauth/authorize. Without that session, a sign-in
// redirects to /, which is a 404, which is what the user saw.
type okyaAuth struct {
	mu       sync.Mutex
	sessions map[string]*okyaSession
	next     int
	base     string
	posts    []string
}

type okyaSession struct {
	returnTo string
	user     bool
}

func (a *okyaAuth) session(w http.ResponseWriter, r *http.Request) *okyaSession {
	a.mu.Lock()
	defer a.mu.Unlock()
	if cookie, err := r.Cookie("_okya_auth_session"); err == nil {
		if s, ok := a.sessions[cookie.Value]; ok {
			return s
		}
	}
	a.next++
	id := fmt.Sprintf("sid-%d", a.next)
	s := &okyaSession{}
	a.sessions[id] = s
	http.SetCookie(w, &http.Cookie{Name: "_okya_auth_session", Value: id, Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	return s
}

func (a *okyaAuth) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "AWSALB", Value: "lb", Path: "/"})
	switch {
	case r.URL.Path == "/oauth/authorize":
		s := a.session(w, r)
		if s.user {
			w.Header().Set("Location", "co.okya.pos.dev://callback?code=abc&state=xyz")
		} else {
			s.returnTo = r.URL.RequestURI()
			w.Header().Set("Location", "/login/otp")
		}
		w.WriteHeader(http.StatusFound)
	case r.URL.Path == "/login/otp":
		a.session(w, r)
		_, _ = io.WriteString(w, `<script>location.href="/login"</script>`)
	case r.Method == http.MethodGet && r.URL.Path == "/login":
		a.session(w, r)
		_, _ = io.WriteString(w, `<form method="post" action="/login">`)
	case r.Method == http.MethodPost && r.URL.Path == "/login":
		a.mu.Lock()
		a.posts = append(a.posts, r.Header.Get("Cookie"))
		a.mu.Unlock()
		if strings.Count(r.Header.Get("Cookie"), "_okya_auth_session=") > 1 {
			http.Error(w, "two session cookies", http.StatusBadRequest)
			return
		}
		s := a.session(w, r)
		s.user = true
		target := "/"
		if s.returnTo != "" {
			target = s.returnTo
		}
		w.Header().Set("Location", a.base+target)
		w.WriteHeader(http.StatusFound)
	default:
		http.Error(w, "The page you were looking for doesn't exist.", http.StatusNotFound)
	}
}

func TestOkyaSignInReturnsToAuthorizeAndApp(t *testing.T) {
	auth := &okyaAuth{sessions: map[string]*okyaSession{}}
	upstream := httptest.NewServer(auth)
	defer upstream.Close()
	auth.base = upstream.URL

	ups := upstreams.NewMemory()
	if _, err := ups.Create("OKYA_AUTH_DEV", upstream.URL, "/okya-auth"); err != nil {
		t.Fatal(err)
	}
	if _, err := ups.Create("EasyHotel_STAGING", "http://127.0.0.1:9", "/easyhotel"); err != nil {
		t.Fatal(err)
	}
	log := history.New(40)
	proxySrv := httptest.NewServer(New(Options{Upstreams: ups, History: log, Timeout: time.Second}))
	defer proxySrv.Close()
	base := proxySrv.URL
	jar, _ := cookiejar.New(nil)
	client := noRedirectClient()
	client.Jar = jar

	step := func(method, path, referer string, body url.Values) *http.Response {
		t.Helper()
		var reader io.Reader
		if body != nil {
			reader = strings.NewReader(body.Encode())
		}
		req, _ := http.NewRequest(method, base+path, reader)
		if referer != "" {
			req.Header.Set("Referer", base+referer)
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("Origin", base)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		return resp
	}

	// 1. The app opens the prefixed authorize URL.
	resp := step("GET", "/okya-auth/oauth/authorize?"+authorizeQuery, "", nil)
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/okya-auth/login/otp" {
		t.Fatalf("authorize: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	for _, cookie := range resp.Cookies() {
		if cookie.Path != "/" {
			t.Fatalf("cookie %s stored on %s, want /", cookie.Name, cookie.Path)
		}
	}
	// 2. Prefixed OTP page, then 3. /login via Referer.
	if resp := step("GET", "/okya-auth/login/otp", "", nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("otp: %d", resp.StatusCode)
	}
	if resp := step("GET", "/login", "/okya-auth/login/otp", nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("login page: %d", resp.StatusCode)
	}
	// 4. The cookie-routed form post must carry the session from step 1.
	resp = step("POST", "/login", "/login", url.Values{"email": {"a@b.c"}, "password": {"x"}})
	want := base + "/okya-auth/oauth/authorize?" + authorizeQuery
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != want {
		t.Fatalf("post: %d Location=%q, want %q (upstream cookies %q)", resp.StatusCode, resp.Header.Get("Location"), want, auth.posts)
	}
	// 5. Back to authorize, which hands the code to the app untouched.
	u, _ := url.Parse(resp.Header.Get("Location"))
	resp = step("GET", u.RequestURI(), "/login", nil)
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "co.okya.pos.dev://callback?code=abc&state=xyz" {
		t.Fatalf("callback: %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	if len(auth.sessions) != 1 {
		t.Fatalf("upstream made %d sessions, want 1", len(auth.sessions))
	}

	var post history.Entry
	for _, entry := range log.List() {
		if entry.Method == http.MethodPost {
			post = entry
		}
	}
	if names := post.UpstreamHeaders.Get("Cookie-Names"); !strings.Contains(names, "_okya_auth_session") || strings.Contains(names, stickyCookieName) {
		t.Fatalf("Cookie-Names = %q", names)
	}
	if strings.Contains(post.UpstreamHeaders.Get("Cookie-Names"), "sid-") {
		t.Fatal("cookie values must not be recorded")
	}
}

// Two upstreams on one proxy origin can both use _session. Each sees only
// its own, and a stale plain cookie from an older proxy does not shadow it.
func TestUpstreamCookiesAreIsolated(t *testing.T) {
	seen := map[string]string{}
	var mu sync.Mutex
	makeUpstream := func(name string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/set" {
				http.SetCookie(w, &http.Cookie{Name: "_session", Value: name, Path: "/", HttpOnly: true})
				return
			}
			mu.Lock()
			seen[name] = r.Header.Get("Cookie")
			mu.Unlock()
		}))
	}
	okyaSrv := makeUpstream("okya")
	defer okyaSrv.Close()
	hotelSrv := makeUpstream("hotel")
	defer hotelSrv.Close()
	ups := upstreams.NewMemory()
	if _, err := ups.Create("OKYA", okyaSrv.URL, "/okya"); err != nil {
		t.Fatal(err)
	}
	if _, err := ups.Create("Hotel", hotelSrv.URL, "/hotel"); err != nil {
		t.Fatal(err)
	}
	proxySrv := httptest.NewServer(New(Options{Upstreams: ups, Timeout: time.Second}))
	defer proxySrv.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	pu, _ := url.Parse(proxySrv.URL)
	jar.SetCookies(pu, []*http.Cookie{{Name: "_session", Value: "stale", Path: "/"}})
	for _, path := range []string{"/okya/set", "/hotel/set", "/okya/read", "/hotel/read"} {
		resp, err := client.Get(proxySrv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
	}
	if seen["okya"] != "_session=okya" {
		t.Fatalf("okya saw %q", seen["okya"])
	}
	if seen["hotel"] != "_session=hotel" {
		t.Fatalf("hotel saw %q", seen["hotel"])
	}
}
