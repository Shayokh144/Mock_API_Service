package proxy

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Shayokh144/Mock_API_Service/internal/config"
	"github.com/Shayokh144/Mock_API_Service/internal/history"
	"github.com/Shayokh144/Mock_API_Service/internal/rules"
)

func TestHistoryRecordsSourcesAndPreservesRules(t *testing.T) {
	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		body, _ := io.ReadAll(r.Body)
		if r.URL.Path == "/v1/echo" && string(body) != `{"n":1}` {
			t.Errorf("forwarded body = %s", body)
		}
		if r.URL.RawQuery != "q=1" && r.URL.Path == "/v1/echo" {
			t.Errorf("query = %s", r.URL.RawQuery)
		}
		w.Header().Set("X-Upstream", "yes")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"from":"ruby"}`)
	}))
	defer upstream.Close()
	base, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	rulesPath := filepath.Join(dir, "rules.json")
	store, err := rules.Load(rulesPath)
	if err != nil {
		t.Fatal(err)
	}
	rule, err := store.Create(rules.Draft{
		Enabled:     true,
		Method:      "GET",
		PathPattern: "/v1/mock",
		StatusCode:  http.StatusNotFound,
		Body:        `{"error":"nope"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(rulesPath)
	if err != nil {
		t.Fatal(err)
	}

	log := history.New(10)
	proxySrv := httptest.NewServer(New(Options{
		Upstream: base,
		Timeout:  time.Second,
		Rules:    store,
		History:  log,
	}))
	defer proxySrv.Close()

	mockResp, err := http.Get(proxySrv.URL + "/v1/mock?q=1")
	if err != nil {
		t.Fatal(err)
	}
	mockResp.Body.Close()

	req, err := http.NewRequest(http.MethodPost, proxySrv.URL+"/v1/echo?q=1", strings.NewReader(`{"n":1}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Request-Id", "abc")
	echoResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	echoBody, _ := io.ReadAll(echoResp.Body)
	echoResp.Body.Close()
	if string(echoBody) != `{"from":"ruby"}` {
		t.Fatalf("echo = %s", echoBody)
	}

	adminResp, err := http.Get(proxySrv.URL + "/_admin")
	if err != nil {
		t.Fatal(err)
	}
	adminResp.Body.Close()

	dead, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	upstream.Close()
	deadSrv := httptest.NewServer(New(Options{Upstream: dead, Timeout: time.Second, History: log}))
	defer deadSrv.Close()
	badResp, err := http.Get(deadSrv.URL + "/v1/down")
	if err != nil {
		t.Fatal(err)
	}
	badResp.Body.Close()

	list := log.List()
	if len(list) != 3 {
		t.Fatalf("entries = %d, admin should be absent", len(list))
	}
	if list[0].Source != history.SourceUpstreamError || list[0].Status != http.StatusBadGateway || list[0].RuleID != "" {
		t.Fatalf("error entry = %+v", list[0])
	}
	if !strings.Contains(list[0].ResponseBody, "upstream unavailable") {
		t.Fatalf("error body = %s", list[0].ResponseBody)
	}
	if list[0].ResponseHeaders.Get("X-Mock-Response") != "" {
		t.Fatal("upstream error has mock header")
	}
	echo := list[1]
	if echo.Source != history.SourceUpstream || echo.Method != http.MethodPost || echo.Path != "/v1/echo" || echo.RawQuery != "q=1" || echo.Status != http.StatusCreated {
		t.Fatalf("echo entry = %+v", echo)
	}
	if echo.RequestHeaders.Get("X-Request-Id") != "abc" || echo.RequestBody != `{"n":1}` || echo.ResponseBody != `{"from":"ruby"}` {
		t.Fatalf("echo details = %+v", echo)
	}
	if echo.ResponseHeaders.Get("X-Upstream") != "yes" || echo.ResponseHeaders.Get("X-Mock-Response") != "" {
		t.Fatalf("echo headers = %v", echo.ResponseHeaders)
	}
	mock := list[2]
	if mock.Source != history.SourceMock || mock.RuleID != rule.ID || mock.Status != http.StatusNotFound || !strings.Contains(mock.ResponseBody, "nope") {
		t.Fatalf("mock entry = %+v", mock)
	}
	if mock.ResponseHeaders.Get("X-Mock-Response") != "true" {
		t.Fatal("mock history missing sentry header")
	}
	if hits.Load() != 1 {
		t.Fatalf("hits = %d", hits.Load())
	}

	if !log.Delete(echo.ID) {
		t.Fatal("delete failed")
	}
	if len(log.List()) != 2 {
		t.Fatalf("after delete = %d", len(log.List()))
	}
	log.Clear()
	if len(log.List()) != 0 {
		t.Fatal("clear failed")
	}
	after, err := os.ReadFile(rulesPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("history deletion changed the rules file")
	}
	if len(history.New(3).List()) != 0 {
		t.Fatal("restart log was not empty")
	}
}

func TestHistoryTruncatesPreviewAndRejectsHugeBodies(t *testing.T) {
	var hits atomic.Int32
	large := bytes.Repeat([]byte("a"), config.HistoryBodyPreview+32)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = w.Write(large)
	}))
	defer upstream.Close()
	base, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	log := history.New(10)
	proxySrv := httptest.NewServer(New(Options{Upstream: base, Timeout: 5 * time.Second, History: log}))
	defer proxySrv.Close()

	resp, err := http.Get(proxySrv.URL + "/v1/big")
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, large) {
		t.Fatalf("client length = %d", len(got))
	}
	entry := log.List()[0]
	if !entry.ResponseBodyTruncated || len(entry.ResponseBody) != config.HistoryBodyPreview || entry.Source != history.SourceUpstream {
		t.Fatalf("preview entry = %+v len=%d", entry.Source, len(entry.ResponseBody))
	}

	tooBig := bytes.Repeat([]byte("b"), config.MaxBufferedBody+1)
	req, err := http.NewRequest(http.MethodPost, proxySrv.URL+"/v1/big", bytes.NewReader(tooBig))
	if err != nil {
		t.Fatal(err)
	}
	beforeHits := hits.Load()
	rejected, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	rejectedBody, _ := io.ReadAll(rejected.Body)
	rejected.Body.Close()
	if rejected.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d body=%s", rejected.StatusCode, rejectedBody)
	}
	if hits.Load() != beforeHits {
		t.Fatal("huge request was forwarded")
	}
	rej := log.List()[0]
	if rej.Source != history.SourceRejected || !rej.RequestBodyTruncated || len(rej.RequestBody) != config.HistoryBodyPreview {
		t.Fatalf("rejected = source %s trunc %v len %d", rej.Source, rej.RequestBodyTruncated, len(rej.RequestBody))
	}
	if rej.ResponseHeaders.Get("X-Mock-Response") != "" || !strings.Contains(rej.ResponseBody, "request body too large") {
		t.Fatalf("rejected response = %s headers=%v", rej.ResponseBody, rej.ResponseHeaders)
	}

	hugeUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(bytes.Repeat([]byte("c"), config.MaxBufferedBody+1))
	}))
	defer hugeUpstream.Close()
	hugeBase, err := url.Parse(hugeUpstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	hugeSrv := httptest.NewServer(New(Options{Upstream: hugeBase, Timeout: 5 * time.Second, History: log}))
	defer hugeSrv.Close()
	over, err := http.Get(hugeSrv.URL + "/v1/over")
	if err != nil {
		t.Fatal(err)
	}
	overBody, _ := io.ReadAll(over.Body)
	over.Body.Close()
	if over.StatusCode != http.StatusBadGateway || !strings.Contains(string(overBody), "upstream response too large") {
		t.Fatalf("over status=%d body=%s", over.StatusCode, overBody)
	}
	overEntry := log.List()[0]
	if overEntry.Source != history.SourceUpstreamError || overEntry.ResponseHeaders.Get("X-Mock-Response") != "" {
		t.Fatalf("over entry = %+v", overEntry)
	}
}
