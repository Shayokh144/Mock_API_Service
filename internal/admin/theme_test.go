package admin

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestThemeCookieIsSetBySameOriginPost(t *testing.T) {
	dashboard, err := New(rulesStore(t), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(dashboard)
	defer srv.Close()
	client := dashboardClient()

	resp, err := client.PostForm(srv.URL+"/_admin/theme", url.Values{
		"theme": {"dark"},
		"next":  {"/_admin/history?source=mock"},
	})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/_admin/history?source=mock" {
		t.Fatalf("location = %q", loc)
	}
	cookie := findCookie(resp, "mock_theme")
	if cookie == nil || cookie.Value != "dark" || cookie.Path != "/_admin" || !cookie.HttpOnly {
		t.Fatalf("cookie = %#v", cookie)
	}

	pageReq, err := http.NewRequest(http.MethodGet, srv.URL+"/_admin/upstreams", nil)
	if err != nil {
		t.Fatal(err)
	}
	pageReq.AddCookie(cookie)
	pageResp, err := http.DefaultClient.Do(pageReq)
	if err != nil {
		t.Fatal(err)
	}
	page := readAll(t, pageResp)
	if !strings.Contains(page, `data-theme="dark"`) || !strings.Contains(page, `<option value="dark" selected>Dark</option>`) {
		t.Fatalf("dark theme not applied:\n%s", page)
	}

	light, err := client.PostForm(srv.URL+"/_admin/theme", url.Values{
		"theme": {"light"},
		"next":  {"https://evil.example/phish"},
	})
	if err != nil {
		t.Fatal(err)
	}
	light.Body.Close()
	if light.Header.Get("Location") != "/_admin" {
		t.Fatalf("open redirect location = %q", light.Header.Get("Location"))
	}
}

func TestThemePostRejectsCrossOrigin(t *testing.T) {
	dashboard, err := New(rulesStore(t), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(dashboard)
	defer srv.Close()
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/_admin/theme", strings.NewReader("theme=dark&next=/_admin"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden || findCookie(resp, "mock_theme") != nil {
		t.Fatalf("status = %d cookie = %#v", resp.StatusCode, findCookie(resp, "mock_theme"))
	}
}

func findCookie(resp *http.Response, name string) *http.Cookie {
	for _, cookie := range resp.Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}
