package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
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

func TestSliceMockAndPassthrough(t *testing.T) {
	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path == "/v1/ping" && r.URL.RawQuery != "x=1" {
			t.Errorf("query = %s", r.URL.RawQuery)
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, `{"from":"ruby"}`)
	}))
	defer upstream.Close()

	base, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxySrv := httptest.NewServer(New(Options{Upstream: base, Timeout: time.Second}))
	defer proxySrv.Close()

	mockResp, err := http.Get(proxySrv.URL + "/__slice/ping")
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
	if mockResp.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("content type = %s", mockResp.Header.Get("Content-Type"))
	}
	if hits.Load() != 0 {
		t.Fatalf("slice reached upstream, hits = %d", hits.Load())
	}

	passResp, err := http.Get(proxySrv.URL + "/v1/ping?x=1")
	if err != nil {
		t.Fatal(err)
	}
	passBody, _ := io.ReadAll(passResp.Body)
	passResp.Body.Close()
	if passResp.StatusCode != http.StatusAccepted || string(passBody) != `{"from":"ruby"}` {
		t.Fatalf("passthrough status=%d body=%s", passResp.StatusCode, passBody)
	}
	if passResp.Header.Get("X-Mock-Response") != "" {
		t.Fatal("passthrough set X-Mock-Response")
	}

	postResp, err := http.Post(proxySrv.URL+"/__slice/ping", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	postResp.Body.Close()
	if postResp.StatusCode != http.StatusAccepted {
		t.Fatalf("POST slice status = %d", postResp.StatusCode)
	}
	if hits.Load() != 2 {
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
	if hits.Load() != 2 {
		t.Fatalf("admin was forwarded, hits = %d", hits.Load())
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
