package proxy

import (
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Shayokh144/Mock_API_Service/internal/history"
	"github.com/Shayokh144/Mock_API_Service/internal/upstreams"
)

// A sign-in page reached through the Referer lives at /login, without the
// prefix. Its form post and the images named by its CSS carry a Referer of
// /login or /assets/..., so only the sticky cookie can route them.
func TestStickyCookieRoutesLoginFormPost(t *testing.T) {
	var postCookie string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/start":
			_, _ = io.WriteString(w, `<a href="/login">sign in</a>`)
		case r.Method == http.MethodGet && r.URL.Path == "/login":
			http.SetCookie(w, &http.Cookie{Name: "csrf", Value: "tok", Path: "/", Secure: true})
			_, _ = io.WriteString(w, `<link rel="stylesheet" href="/assets/app.css"><form method="post" action="/login">`)
		case r.Method == http.MethodGet && r.URL.Path == "/assets/app.css":
			_, _ = io.WriteString(w, `.flag{background:url(/assets/flags@2x.webp)}`)
		case r.Method == http.MethodGet && r.URL.Path == "/assets/flags@2x.webp":
			_, _ = io.WriteString(w, "webp")
		case r.Method == http.MethodPost && r.URL.Path == "/login":
			postCookie = r.Header.Get("Cookie")
			w.Header().Set("Location", "/home")
			w.WriteHeader(http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	ups := upstreams.NewMemory()
	okya, err := ups.Create("OKYA_AUTH_DEV", upstream.URL, "/okya-auth")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ups.Create("EasyHotel_STAGING", "http://127.0.0.1:9", "/easyhotel"); err != nil {
		t.Fatal(err)
	}
	log := history.New(20)
	proxySrv := httptest.NewServer(New(Options{Upstreams: ups, History: log, Timeout: time.Second}))
	defer proxySrv.Close()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := noRedirectClient()
	client.Jar = jar
	base := proxySrv.URL

	send := func(method, path, referer, body string) *http.Response {
		t.Helper()
		req, err := http.NewRequest(method, base+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if referer != "" {
			req.Header.Set("Referer", base+referer)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		return resp
	}

	if resp := send("GET", "/okya-auth/start", "", ""); resp.StatusCode != http.StatusOK {
		t.Fatalf("start status = %d", resp.StatusCode)
	}
	login := send("GET", "/login", "/okya-auth/start", "")
	if login.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d", login.StatusCode)
	}
	for _, cookie := range login.Cookies() {
		if cookie.Name == "csrf" && cookie.Path != "/" {
			t.Fatalf("csrf path = %q, the form post on /login would not send it", cookie.Path)
		}
	}
	if resp := send("GET", "/assets/app.css", "/login", ""); resp.StatusCode != http.StatusOK {
		t.Fatalf("css status = %d", resp.StatusCode)
	}
	if resp := send("GET", "/assets/flags@2x.webp", "/assets/app.css", ""); resp.StatusCode != http.StatusOK {
		t.Fatalf("flag status = %d", resp.StatusCode)
	}
	post := send("POST", "/login", "/login", "email=a%40b.c&password=x")
	if post.StatusCode != http.StatusFound {
		t.Fatalf("post status = %d", post.StatusCode)
	}
	if got := post.Header.Get("Location"); got != "/okya-auth/home" {
		t.Fatalf("Location = %s", got)
	}
	if !strings.Contains(postCookie, "csrf=tok") {
		t.Fatalf("upstream cookie = %q, want csrf", postCookie)
	}
	if strings.Contains(postCookie, stickyCookieName) {
		t.Fatalf("upstream saw the proxy cookie: %q", postCookie)
	}

	entries := log.List()
	labels := map[string]string{}
	for _, entry := range entries {
		if entry.UpstreamID != okya.ID {
			t.Fatalf("%s %s upstream = %q", entry.Method, entry.Path, entry.UpstreamID)
		}
		labels[entry.Method+" "+entry.Path] = entry.UpstreamLabel()
	}
	want := map[string]string{
		"GET /okya-auth/start":      "OKYA_AUTH_DEV",
		"GET /login":                "OKYA_AUTH_DEV (via Referer)",
		"GET /assets/app.css":       "OKYA_AUTH_DEV (via cookie)",
		"GET /assets/flags@2x.webp": "OKYA_AUTH_DEV (via cookie)",
		"POST /login":               "OKYA_AUTH_DEV (via cookie)",
	}
	for key, label := range want {
		if labels[key] != label {
			t.Fatalf("%s label = %q, want %q (all %v)", key, labels[key], label, labels)
		}
	}
}

func TestStickyCookieFollowsLastPrefix(t *testing.T) {
	ups := upstreams.NewMemory()
	okya, err := ups.Create("OKYA", "http://127.0.0.1:9", "/okya")
	if err != nil {
		t.Fatal(err)
	}
	hotel, err := ups.Create("Hotel", "http://127.0.0.1:9", "/hotel")
	if err != nil {
		t.Fatal(err)
	}
	h := New(Options{Upstreams: ups, Timeout: time.Second})
	req := httptest.NewRequest("GET", "/hotel/x", nil)
	req.AddCookie(&http.Cookie{Name: stickyCookieName, Value: okya.ID})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	set := rec.Result().Cookies()
	if len(set) != 1 || set[0].Value != hotel.ID || set[0].Path != "/" || !set[0].HttpOnly {
		t.Fatalf("cookies = %+v", set)
	}

	same := httptest.NewRequest("GET", "/hotel/y", nil)
	same.AddCookie(&http.Cookie{Name: stickyCookieName, Value: hotel.ID})
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, same)
	if got := rec.Result().Cookies(); len(got) != 0 {
		t.Fatalf("unchanged cookie was set again: %+v", got)
	}
}

func TestUnknownStickyCookieIsRejectedAndRecorded(t *testing.T) {
	ups := upstreams.NewMemory()
	if _, err := ups.Create("OKYA", "http://127.0.0.1:9", "/okya"); err != nil {
		t.Fatal(err)
	}
	log := history.New(5)
	h := New(Options{Upstreams: ups, History: log, Timeout: time.Second})
	req := httptest.NewRequest("POST", "/login", strings.NewReader("email=a&password=b"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", "http://10.0.2.2:8080/login")
	req.AddCookie(&http.Cookie{Name: stickyCookieName, Value: "0123456789abcdef"})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), noUpstreamMessage) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Fatal("a rejection should not set the sticky cookie")
	}
	entries := log.List()
	if len(entries) != 1 {
		t.Fatalf("entries = %d", len(entries))
	}
	got := entries[0]
	if got.Method != "POST" || got.Path != "/login" || got.Source != history.SourceRejected || got.Status != http.StatusNotFound {
		t.Fatalf("entry = %+v", got)
	}
	if got.RequestBody != "email=a&password=b" || got.RequestHeaders.Get("Referer") != "http://10.0.2.2:8080/login" {
		t.Fatalf("body=%q referer=%q", got.RequestBody, got.RequestHeaders.Get("Referer"))
	}
}

func TestStripStickyCookie(t *testing.T) {
	header := http.Header{}
	header.Add("Cookie", "a=1; "+stickyCookieName+"=abc")
	header.Add("Cookie", "b=2")
	rewriteRequestCookies(header)
	if got := header.Values("Cookie"); len(got) != 1 || got[0] != "a=1; b=2" {
		t.Fatalf("cookie = %q", got)
	}
	only := http.Header{"Cookie": {stickyCookieName + "=abc"}}
	rewriteRequestCookies(only)
	if _, ok := only["Cookie"]; ok {
		t.Fatalf("cookie = %q", only.Values("Cookie"))
	}
	untouched := http.Header{"Cookie": {"a=1;b=2"}}
	rewriteRequestCookies(untouched)
	if untouched.Get("Cookie") != "a=1;b=2" {
		t.Fatalf("cookie = %q", untouched.Get("Cookie"))
	}
}
