package proxy

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Shayokh144/Mock_API_Service/internal/rules"
)

func TestOAuthRefreshMockLeavesLoginAndOriginalBody(t *testing.T) {
	var seen [][]byte
	var seenEncoding []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read: %v", err)
		}
		seen = append(seen, body)
		seenEncoding = append(seenEncoding, r.Header.Get("Content-Encoding"))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"access_token":"real"}`))
	}))
	defer upstream.Close()
	base, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	store := rules.NewMemory()
	raw := "HTTP/1.1 401 Unauthorized\nContent-Type: application/json\n\n{\"error\":\"unauthorized\"}\n"
	if _, err := store.Create(rules.Draft{
		Name:         "Refresh failure",
		Enabled:      true,
		Method:       http.MethodPost,
		PathPattern:  "/oauth/token",
		ResponseMode: rules.ModeRaw,
		RawResponse:  raw,
		Match: rules.MatchSpec{
			Body: mustConditions(t, "grant_type=refresh_token", "body"),
		},
	}); err != nil {
		t.Fatal(err)
	}
	freshBody := gzipBytes(t, []byte(`{"grant_type":"refresh_token"}`))
	if _, err := store.Create(rules.Draft{
		Name:         "Fresh refresh",
		Enabled:      true,
		FreshRequest: true,
		Method:       http.MethodPost,
		PathPattern:  "/oauth/fresh",
		StatusCode:   http.StatusInternalServerError,
		Match: rules.MatchSpec{
			Body: mustConditions(t, "grant_type=refresh_token", "body"),
		},
	}); err != nil {
		t.Fatal(err)
	}
	proxySrv := httptest.NewServer(New(Options{Upstream: base, Timeout: time.Second, Rules: store}))
	defer proxySrv.Close()

	refresh := postForm(t, proxySrv.URL+"/oauth/token", "grant_type=refresh_token&refresh_token=abc")
	if refresh.StatusCode != http.StatusUnauthorized {
		t.Fatalf("refresh status = %d", refresh.StatusCode)
	}
	refreshBody, _ := io.ReadAll(refresh.Body)
	refresh.Body.Close()
	if !bytes.Contains(refreshBody, []byte(`"error":"unauthorized"`)) || refresh.Header.Get("X-Mock-Response") != "" {
		t.Fatalf("refresh = %s header %q", refreshBody, refresh.Header.Get("X-Mock-Response"))
	}

	loginBody := "grant_type=authorization_code&code=xyz"
	login := postForm(t, proxySrv.URL+"/oauth/token", loginBody)
	login.Body.Close()
	if login.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d", login.StatusCode)
	}

	gzipLogin := gzipBytes(t, []byte(`{"grant_type":"authorization_code"}`))
	gzipResp := postGzip(t, proxySrv.URL+"/oauth/token", gzipLogin)
	gzipResp.Body.Close()
	if gzipResp.StatusCode != http.StatusOK {
		t.Fatalf("gzip login status = %d", gzipResp.StatusCode)
	}

	freshResp := postGzip(t, proxySrv.URL+"/oauth/fresh", freshBody)
	freshResp.Body.Close()
	if freshResp.StatusCode != http.StatusOK {
		t.Fatalf("fresh status = %d", freshResp.StatusCode)
	}

	if len(seen) != 3 {
		t.Fatalf("upstream hits = %d", len(seen))
	}
	if string(seen[0]) != loginBody || seenEncoding[0] != "" {
		t.Fatalf("login forwarded %q encoding %q", seen[0], seenEncoding[0])
	}
	if !bytes.Equal(seen[1], gzipLogin) || seenEncoding[1] != "gzip" {
		t.Fatalf("gzip passthrough = %d bytes encoding %q", len(seen[1]), seenEncoding[1])
	}
	if !bytes.Equal(seen[2], freshBody) || seenEncoding[2] != "gzip" {
		t.Fatalf("fresh gzip = %d bytes encoding %q", len(seen[2]), seenEncoding[2])
	}
}

func TestOversizedBodySkipsBodyConditions(t *testing.T) {
	var got []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		got, err = io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()
	base, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	store := rules.NewMemory()
	if _, err := store.Create(rules.Draft{
		Name:        "Refresh",
		Enabled:     true,
		Method:      http.MethodPost,
		PathPattern: "/oauth/token",
		StatusCode:  http.StatusUnauthorized,
		Match: rules.MatchSpec{
			Body: mustConditions(t, "grant_type=refresh_token", "body"),
		},
	}); err != nil {
		t.Fatal(err)
	}
	proxySrv := httptest.NewServer(New(Options{Upstream: base, Timeout: time.Second, Rules: store}))
	defer proxySrv.Close()
	payload := []byte("grant_type=refresh_token&pad=" + strings.Repeat("a", rules.MaxMatchBody))
	req, err := http.NewRequest(http.MethodPost, proxySrv.URL+"/oauth/token", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent || !bytes.Equal(got, payload) {
		t.Fatalf("status %d forwarded %d want %d", resp.StatusCode, len(got), len(payload))
	}
}

func postForm(t *testing.T, rawURL, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, rawURL, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func postGzip(t *testing.T, rawURL string, body []byte) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, rawURL, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func mustConditions(t *testing.T, text, label string) []rules.Condition {
	t.Helper()
	list, err := rules.ParseConditions(text, label)
	if err != nil {
		t.Fatal(err)
	}
	return list
}
