package sessions

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/Shayokh144/Mock_API_Service/internal/upstreams"
)

func TestTestSessionRewritesUpstreamRedirect(t *testing.T) {
	var upstreamBase *url.URL
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/login" {
			http.Error(w, r.URL.Path, http.StatusNotFound)
			return
		}
		w.Header().Set("Location", upstreamBase.String()+"/next")
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "abc", Domain: upstreamBase.Hostname(), Path: "/", Secure: true})
		w.WriteHeader(http.StatusFound)
	}))
	defer upstream.Close()
	var err error
	upstreamBase, err = url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	ups := upstreams.NewMemory()
	if _, err := ups.Create("staging", upstreamBase.String(), "/okya"); err != nil {
		t.Fatal(err)
	}
	manager := New(Options{
		Upstreams:    ups,
		ProxyAddr:    "127.0.0.1:8080",
		AdminAddr:    "127.0.0.1:8081",
		Timeout:      time.Second,
		HistoryLimit: 5,
	})
	t.Cleanup(manager.Close)
	info, err := manager.Create("qa", 0, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Get(info.ProxyURL + "/okya/login")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Location"); got != info.ProxyURL+"/okya/next" {
		t.Fatalf("Location = %s", got)
	}
	var session *http.Cookie
	for _, cookie := range resp.Cookies() {
		if cookie.Name == "session" {
			session = cookie
		}
	}
	if session == nil || session.Domain != "" || session.Secure || session.Path != "/okya" {
		t.Fatalf("cookie = %+v", resp.Cookies())
	}
}
