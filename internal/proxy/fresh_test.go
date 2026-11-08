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

	"github.com/Shayokh144/Mock_API_Service/internal/history"
	"github.com/Shayokh144/Mock_API_Service/internal/rules"
)

func TestPrepareFreshRequestRemovesValidators(t *testing.T) {
	header := http.Header{}
	header.Set("If-None-Match", `"old"`)
	header.Set("If-Modified-Since", "Tue, 15 Nov 1994 08:12:31 GMT")
	header.Set("If-Match", `"old"`)
	header.Set("If-Unmodified-Since", "Tue, 15 Nov 1994 08:12:31 GMT")
	header.Set("If-Range", `"old"`)
	header.Set("Cache-Control", "max-age=60")
	header.Set("X-Request-Id", "abc")
	prepareFreshRequest(header)
	for _, name := range conditionalRequestHeaders {
		if header.Get(name) != "" {
			t.Fatalf("%s = %q", name, header.Get(name))
		}
	}
	if header.Get("Cache-Control") != "no-cache" || header.Get("Pragma") != "no-cache" {
		t.Fatalf("cache headers = %v", header)
	}
	if header.Get("X-Request-Id") != "abc" {
		t.Fatal("unrelated header was removed")
	}
}

func TestFreshRequestReturnsUpstreamBodyAndMockStaysMock(t *testing.T) {
	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		for _, name := range conditionalRequestHeaders {
			if r.Header.Get(name) != "" {
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}
		if r.Header.Get("Cache-Control") != "no-cache" || r.Header.Get("Pragma") != "no-cache" {
			t.Errorf("cache headers = %q %q", r.Header.Get("Cache-Control"), r.Header.Get("Pragma"))
		}
		if r.Method != http.MethodPost || r.URL.RawQuery != "page=2" || r.Header.Get("X-Request-Id") != "abc" {
			t.Errorf("method=%s query=%s id=%s", r.Method, r.URL.RawQuery, r.Header.Get("X-Request-Id"))
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != `{"n":1}` {
			t.Errorf("body = %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"fresh":true}`)
	}))
	defer upstream.Close()
	base, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	store := rules.NewMemory()
	fresh, err := store.Create(rules.Draft{
		Name:         "Fresh items",
		Enabled:      true,
		FreshRequest: true,
		Method:       http.MethodPost,
		PathPattern:  "/v1/items",
		StatusCode:   http.StatusInternalServerError,
		Body:         `{"error":"mock"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(rules.Draft{
		Name:        "Normal",
		Enabled:     true,
		Method:      http.MethodGet,
		PathPattern: "/v1/mock",
		StatusCode:  http.StatusOK,
		Body:        `{"mocked":true}`,
	}); err != nil {
		t.Fatal(err)
	}
	log := history.New(10)
	proxySrv := httptest.NewServer(New(Options{Upstream: base, Timeout: time.Second, Rules: store, History: log}))
	defer proxySrv.Close()

	req, err := http.NewRequest(http.MethodPost, proxySrv.URL+"/v1/items?page=2", strings.NewReader(`{"n":1}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-Id", "abc")
	req.Header.Set("If-None-Match", `"old"`)
	req.Header.Set("If-Modified-Since", "Tue, 15 Nov 1994 08:12:31 GMT")
	req.Header.Set("If-Match", `"old"`)
	req.Header.Set("If-Unmodified-Since", "Tue, 15 Nov 1994 08:12:31 GMT")
	req.Header.Set("If-Range", `"old"`)
	req.Header.Set("Cache-Control", "max-age=60")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != `{"fresh":true}` {
		t.Fatalf("fresh status=%d body=%s", resp.StatusCode, body)
	}
	if resp.Header.Get("X-Mock-Response") != "" {
		t.Fatal("fresh upstream response was marked as a mock")
	}
	if hits.Load() != 1 {
		t.Fatalf("hits = %d", hits.Load())
	}
	entry := log.List()[0]
	if entry.Source != history.SourceUpstream || entry.RuleID != fresh.ID || entry.Status != http.StatusOK {
		t.Fatalf("history = %+v", entry)
	}

	mockReq, err := http.NewRequest(http.MethodGet, proxySrv.URL+"/v1/mock", nil)
	if err != nil {
		t.Fatal(err)
	}
	mockReq.Header.Set("If-None-Match", `"old"`)
	mockResp, err := http.DefaultClient.Do(mockReq)
	if err != nil {
		t.Fatal(err)
	}
	mockBody, _ := io.ReadAll(mockResp.Body)
	mockResp.Body.Close()
	if mockResp.StatusCode != http.StatusOK || string(mockBody) != `{"mocked":true}` {
		t.Fatalf("mock status=%d body=%s", mockResp.StatusCode, mockBody)
	}
	if mockResp.Header.Get("X-Mock-Response") != "true" {
		t.Fatal("normal mock lost X-Mock-Response")
	}
	if hits.Load() != 1 {
		t.Fatal("mock rule called upstream")
	}
}

func TestDisabledFreshRuleDoesNotStripValidators(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"old"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"full":true}`)
	}))
	defer upstream.Close()
	base, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	store := rules.NewMemory()
	if _, err := store.Create(rules.Draft{
		Enabled:      false,
		FreshRequest: true,
		Method:       http.MethodGet,
		PathPattern:  "/v1/items",
		StatusCode:   200,
		Body:         `{"mocked":true}`,
	}); err != nil {
		t.Fatal(err)
	}
	proxySrv := httptest.NewServer(New(Options{Upstream: base, Timeout: time.Second, Rules: store}))
	defer proxySrv.Close()
	req, err := http.NewRequest(http.MethodGet, proxySrv.URL+"/v1/items", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("If-None-Match", `"old"`)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotModified {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}
