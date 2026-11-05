package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Shayokh144/Mock_API_Service/internal/rules"
)

func TestPassthroughPreservesMethodPathQueryBodyAndResponse(t *testing.T) {
	var hits atomic.Int32
	var gotMethod, gotPath, gotQuery, gotBody, gotHost, gotRequestID string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotHost = r.Host
		gotRequestID = r.Header.Get("X-Request-Id")
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		gotBody = string(body)
		w.Header().Set("X-Upstream", "yes")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"from":"upstream"}`))
	}))
	defer upstream.Close()

	base, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	base.Path = "/prefix"
	handler := New(Options{Upstream: base, Timeout: 2 * time.Second})
	proxySrv := httptest.NewServer(handler)
	defer proxySrv.Close()

	req, err := http.NewRequest(http.MethodPut, proxySrv.URL+"/v1/users/?page=2", strings.NewReader(`{"n":1}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-Id", "abc")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}

	if hits.Load() != 1 {
		t.Fatalf("upstream hits = %d", hits.Load())
	}
	if gotMethod != http.MethodPut {
		t.Fatalf("method = %s", gotMethod)
	}
	if gotPath != "/prefix/v1/users/" {
		t.Fatalf("path = %s", gotPath)
	}
	if gotQuery != "page=2" {
		t.Fatalf("query = %s", gotQuery)
	}
	if gotBody != `{"n":1}` {
		t.Fatalf("body = %s", gotBody)
	}
	if gotHost != base.Host {
		t.Fatalf("host = %s, want %s", gotHost, base.Host)
	}
	if gotRequestID != "abc" {
		t.Fatalf("request id = %s", gotRequestID)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if string(respBody) != `{"from":"upstream"}` {
		t.Fatalf("response = %s", respBody)
	}
	if resp.Header.Get("X-Upstream") != "yes" {
		t.Fatalf("upstream header missing: %v", resp.Header)
	}
	if resp.Header.Get("X-Mock-Response") != "" {
		t.Fatalf("passthrough added X-Mock-Response: %q", resp.Header.Get("X-Mock-Response"))
	}
}

func TestExactRuleMatchesIgnoringQuery(t *testing.T) {
	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path == "/v1/other" && r.URL.RawQuery != "x=1" {
			t.Errorf("query = %s", r.URL.RawQuery)
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, `{"from":"upstream"}`)
	}))
	defer upstream.Close()

	base, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	store := rules.NewMemory()
	if _, err := store.Create(rules.Draft{
		Name:        "Items",
		Enabled:     true,
		Method:      "GET",
		PathPattern: "/v1/ping",
		StatusCode:  http.StatusOK,
		Body:        `{"ok":true}`,
	}); err != nil {
		t.Fatal(err)
	}
	disabled := rules.Draft{
		Name:        "Off",
		Enabled:     false,
		Method:      "GET",
		PathPattern: "/v1/off",
		StatusCode:  http.StatusOK,
		Body:        `{"off":true}`,
	}
	if _, err := store.Create(disabled); err != nil {
		t.Fatal(err)
	}
	proxySrv := httptest.NewServer(New(Options{Upstream: base, Timeout: time.Second, Rules: store}))
	defer proxySrv.Close()

	mockResp, err := http.Get(proxySrv.URL + "/v1/ping?page=2")
	if err != nil {
		t.Fatal(err)
	}
	mockBody, _ := io.ReadAll(mockResp.Body)
	mockResp.Body.Close()
	if mockResp.StatusCode != http.StatusOK {
		t.Fatalf("mock status = %d", mockResp.StatusCode)
	}
	if string(mockBody) != `{"ok":true}` {
		t.Fatalf("mock body = %s", mockBody)
	}
	if mockResp.Header.Get("X-Mock-Response") != "true" {
		t.Fatalf("mock header = %q", mockResp.Header.Get("X-Mock-Response"))
	}
	if hits.Load() != 0 {
		t.Fatalf("exact rule reached upstream, hits = %d", hits.Load())
	}

	offResp, err := http.Get(proxySrv.URL + "/v1/off")
	if err != nil {
		t.Fatal(err)
	}
	offResp.Body.Close()
	if offResp.StatusCode != http.StatusAccepted {
		t.Fatalf("disabled status = %d", offResp.StatusCode)
	}

	passResp, err := http.Get(proxySrv.URL + "/v1/other?x=1")
	if err != nil {
		t.Fatal(err)
	}
	passBody, _ := io.ReadAll(passResp.Body)
	passResp.Body.Close()
	if passResp.StatusCode != http.StatusAccepted || string(passBody) != `{"from":"upstream"}` {
		t.Fatalf("passthrough status=%d body=%s", passResp.StatusCode, passBody)
	}
	if passResp.Header.Get("X-Mock-Response") != "" {
		t.Fatal("passthrough set X-Mock-Response")
	}

	postResp, err := http.Post(proxySrv.URL+"/v1/ping", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	postResp.Body.Close()
	if postResp.StatusCode != http.StatusAccepted {
		t.Fatalf("POST status = %d", postResp.StatusCode)
	}

	sliceResp, err := http.Get(proxySrv.URL + "/__slice/ping")
	if err != nil {
		t.Fatal(err)
	}
	sliceResp.Body.Close()
	if sliceResp.StatusCode != http.StatusAccepted {
		t.Fatalf("former slice path status = %d", sliceResp.StatusCode)
	}
	if hits.Load() != 4 {
		t.Fatalf("hits = %d", hits.Load())
	}

	adminResp, err := http.Get(proxySrv.URL + "/_admin")
	if err != nil {
		t.Fatal(err)
	}
	adminResp.Body.Close()
	if adminResp.StatusCode != http.StatusNotFound {
		t.Fatalf("admin status = %d", adminResp.StatusCode)
	}
	if hits.Load() != 4 {
		t.Fatalf("admin was forwarded, hits = %d", hits.Load())
	}
}

func TestMockStatusHeadersAndForcedSentryHeader(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("upstream saw %s", r.URL.Path)
	}))
	defer upstream.Close()
	base, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	store := rules.NewMemory()
	cases := []rules.Draft{
		{Enabled: true, Method: "GET", PathPattern: "/v1/created", StatusCode: http.StatusCreated, Headers: map[string]string{"X-Extra": "yes", "X-Mock-Response": "false"}, Body: `{"created":true}`},
		{Enabled: true, Method: "GET", PathPattern: "/v1/missing", StatusCode: http.StatusNotFound, Body: `{"error":"missing"}`},
		{Enabled: true, Method: "GET", PathPattern: "/v1/boom", StatusCode: http.StatusInternalServerError, Headers: map[string]string{"Content-Type": "application/problem+json"}, Body: `{"error":"boom"}`},
		{Enabled: true, Method: "DELETE", PathPattern: "/v1/gone", StatusCode: http.StatusNoContent},
	}
	for _, draft := range cases {
		if _, err := store.Create(draft); err != nil {
			t.Fatal(err)
		}
	}
	proxySrv := httptest.NewServer(New(Options{Upstream: base, Timeout: time.Second, Rules: store}))
	defer proxySrv.Close()

	created, err := http.Get(proxySrv.URL + "/v1/created")
	if err != nil {
		t.Fatal(err)
	}
	createdBody, _ := io.ReadAll(created.Body)
	created.Body.Close()
	if created.StatusCode != http.StatusCreated || string(createdBody) != `{"created":true}` {
		t.Fatalf("created status=%d body=%s", created.StatusCode, createdBody)
	}
	if created.Header.Get("X-Extra") != "yes" {
		t.Fatalf("custom header = %q", created.Header.Get("X-Extra"))
	}
	if created.Header.Get("X-Mock-Response") != "true" {
		t.Fatalf("sentry header = %q", created.Header.Get("X-Mock-Response"))
	}
	if created.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("content type = %s", created.Header.Get("Content-Type"))
	}

	missing, err := http.Get(proxySrv.URL + "/v1/missing")
	if err != nil {
		t.Fatal(err)
	}
	missingBody, _ := io.ReadAll(missing.Body)
	missing.Body.Close()
	if missing.StatusCode != http.StatusNotFound || string(missingBody) != `{"error":"missing"}` {
		t.Fatalf("missing status=%d body=%s", missing.StatusCode, missingBody)
	}
	if missing.Header.Get("X-Mock-Response") != "true" {
		t.Fatal("404 missing sentry header")
	}

	boom, err := http.Get(proxySrv.URL + "/v1/boom")
	if err != nil {
		t.Fatal(err)
	}
	boomBody, _ := io.ReadAll(boom.Body)
	boom.Body.Close()
	if boom.StatusCode != http.StatusInternalServerError || string(boomBody) != `{"error":"boom"}` {
		t.Fatalf("boom status=%d body=%s", boom.StatusCode, boomBody)
	}
	if boom.Header.Get("Content-Type") != "application/problem+json" {
		t.Fatalf("content type = %s", boom.Header.Get("Content-Type"))
	}

	req, err := http.NewRequest(http.MethodDelete, proxySrv.URL+"/v1/gone", nil)
	if err != nil {
		t.Fatal(err)
	}
	empty, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	emptyBody, _ := io.ReadAll(empty.Body)
	empty.Body.Close()
	if empty.StatusCode != http.StatusNoContent || len(emptyBody) != 0 {
		t.Fatalf("empty status=%d body=%q", empty.StatusCode, emptyBody)
	}
	if empty.Header.Get("X-Mock-Response") != "true" {
		t.Fatal("204 missing sentry header")
	}
}

func TestWildcardRuleDoesNotReachUpstream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("upstream saw %s", r.URL.RequestURI())
	}))
	defer upstream.Close()
	base, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	store := rules.NewMemory()
	for _, pattern := range []string{"/api/users/1", "/api/users/*", "/api/*/*"} {
		if _, err := store.Create(rules.Draft{
			Enabled:     true,
			Method:      "GET",
			PathPattern: pattern,
			StatusCode:  http.StatusOK,
			Body:        `{"pattern":"` + pattern + `"}`,
		}); err != nil {
			t.Fatal(err)
		}
	}
	proxySrv := httptest.NewServer(New(Options{Upstream: base, Timeout: time.Second, Rules: store}))
	defer proxySrv.Close()

	exact, err := http.Get(proxySrv.URL + "/api/users/1?page=2")
	if err != nil {
		t.Fatal(err)
	}
	exactBody, _ := io.ReadAll(exact.Body)
	exact.Body.Close()
	if string(exactBody) != `{"pattern":"/api/users/1"}` {
		t.Fatalf("exact body = %s", exactBody)
	}
	wild, err := http.Get(proxySrv.URL + "/api/users/2")
	if err != nil {
		t.Fatal(err)
	}
	wildBody, _ := io.ReadAll(wild.Body)
	wild.Body.Close()
	if string(wildBody) != `{"pattern":"/api/users/*"}` || wild.Header.Get("X-Mock-Response") != "true" {
		t.Fatalf("wildcard body = %s header=%s", wildBody, wild.Header.Get("X-Mock-Response"))
	}
}

func TestLoadedRuleMatchesFirstRequest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.json")
	store, err := rules.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(rules.Draft{
		Name:        "Saved",
		Enabled:     true,
		Method:      "GET",
		PathPattern: "/v1/saved",
		StatusCode:  http.StatusOK,
		Body:        `{"saved":true}`,
	}); err != nil {
		t.Fatal(err)
	}
	reloaded, err := rules.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("upstream saw %s", r.URL.Path)
	}))
	defer upstream.Close()
	base, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxySrv := httptest.NewServer(New(Options{Upstream: base, Timeout: time.Second, Rules: reloaded}))
	defer proxySrv.Close()
	resp, err := http.Get(proxySrv.URL + "/v1/saved?page=1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || string(body) != `{"saved":true}` {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	if resp.Header.Get("X-Mock-Response") != "true" {
		t.Fatal("missing mock header")
	}
}

func TestAdminPrefixIsNotForwarded(t *testing.T) {
	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	base, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxySrv := httptest.NewServer(New(Options{Upstream: base, Timeout: time.Second}))
	defer proxySrv.Close()

	for _, path := range []string{"/_admin", "/_admin/anything"} {
		resp, err := http.Get(proxySrv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s status = %d", path, resp.StatusCode)
		}
		if !strings.Contains(string(body), "admin is not ready") {
			t.Fatalf("%s body = %s", path, body)
		}
	}
	if hits.Load() != 0 {
		t.Fatalf("upstream hits = %d", hits.Load())
	}

	resp, err := http.Get(proxySrv.URL + "/_adminfoo")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/_adminfoo status = %d", resp.StatusCode)
	}
	if hits.Load() != 1 {
		t.Fatalf("expected /_adminfoo to be forwarded, hits = %d", hits.Load())
	}
}

func TestUpstreamFailureReturnsJSON502(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	base, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	upstream.Close()

	proxySrv := httptest.NewServer(New(Options{Upstream: base, Timeout: time.Second}))
	defer proxySrv.Close()

	resp, err := http.Get(proxySrv.URL + "/v1/ping")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	if strings.TrimSpace(string(body)) != `{"error":"upstream unavailable"}` {
		t.Fatalf("body = %s", body)
	}
	if resp.Header.Get("X-Mock-Response") != "" {
		t.Fatalf("error response added X-Mock-Response")
	}
	if !strings.Contains(resp.Header.Get("Content-Type"), "application/json") {
		t.Fatalf("content type = %s", resp.Header.Get("Content-Type"))
	}
}
