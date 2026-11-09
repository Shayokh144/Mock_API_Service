package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Shayokh144/Mock_API_Service/internal/history"
	"github.com/Shayokh144/Mock_API_Service/internal/rules"
	"github.com/Shayokh144/Mock_API_Service/internal/upstreams"
)

func TestSwitchingUpstreamChangesTheNextRequest(t *testing.T) {
	var stagingHits, developHits atomic.Int32
	staging := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stagingHits.Add(1)
		if r.URL.Path != "/api/v1/health" {
			t.Errorf("staging path = %s", r.URL.Path)
		}
		_, _ = io.WriteString(w, "staging")
	}))
	defer staging.Close()
	develop := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		developHits.Add(1)
		if r.URL.Path != "/v1/health" {
			t.Errorf("develop path = %s", r.URL.Path)
		}
		_, _ = io.WriteString(w, "develop")
	}))
	defer develop.Close()

	ups := upstreams.NewMemory()
	stagingUp, err := ups.Create("staging", staging.URL+"/api")
	if err != nil {
		t.Fatal(err)
	}
	developUp, err := ups.Create("develop", develop.URL)
	if err != nil {
		t.Fatal(err)
	}
	rulesStore := rules.NewMemory()
	stagingRule, err := rulesStore.Create(rules.Draft{
		Name: "staging failure", Enabled: true, Method: "GET", PathPattern: "/v1/profile",
		StatusCode: 500, UpstreamID: stagingUp.ID, Body: `{"from":"staging-rule"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rulesStore.Create(rules.Draft{
		Name: "any", Enabled: true, Method: "GET", PathPattern: "/v1/profile",
		StatusCode: 201, Body: `{"from":"any"}`,
	}); err != nil {
		t.Fatal(err)
	}
	log := history.New(20)
	handler := New(Options{Upstreams: ups, Rules: rulesStore, History: log, Timeout: time.Second})
	proxySrv := httptest.NewServer(handler)
	defer proxySrv.Close()

	if body := getBody(t, proxySrv.URL+"/v1/health"); body != "staging" {
		t.Fatalf("first body = %s", body)
	}
	if stagingHits.Load() != 1 || developHits.Load() != 0 {
		t.Fatalf("hits staging=%d develop=%d", stagingHits.Load(), developHits.Load())
	}
	mockBody := getBody(t, proxySrv.URL+"/v1/profile")
	if mockBody != `{"from":"staging-rule"}` {
		t.Fatalf("mock body = %s", mockBody)
	}

	if err := ups.Activate(developUp.ID); err != nil {
		t.Fatal(err)
	}
	if body := getBody(t, proxySrv.URL+"/v1/health"); body != "develop" {
		t.Fatalf("switched body = %s", body)
	}
	if developHits.Load() != 1 {
		t.Fatalf("develop hits = %d", developHits.Load())
	}
	anyBody := getBody(t, proxySrv.URL+"/v1/profile")
	if anyBody != `{"from":"any"}` {
		t.Fatalf("any body = %s", anyBody)
	}

	entries := log.List()
	if len(entries) != 4 {
		t.Fatalf("history = %d", len(entries))
	}
	if entries[0].UpstreamName != "any" || entries[0].UpstreamID != history.UpstreamAny || entries[0].Source != history.SourceMock {
		t.Fatalf("newest = %+v", entries[0])
	}
	if entries[1].UpstreamName != "develop" || entries[1].UpstreamID != developUp.ID || entries[1].Source != history.SourceUpstream {
		t.Fatalf("develop entry = %+v", entries[1])
	}
	if entries[2].UpstreamName != "staging" || entries[2].UpstreamID != stagingUp.ID || entries[2].RuleID != stagingRule.ID {
		t.Fatalf("staging mock = %+v", entries[2])
	}
	if entries[3].UpstreamName != "staging" || entries[3].UpstreamID != stagingUp.ID {
		t.Fatalf("staging pass = %+v", entries[3])
	}
}

func TestNoUpstreamReturnsDashboardHint(t *testing.T) {
	log := history.New(5)
	handler := New(Options{Upstreams: upstreams.NewMemory(), History: log, Timeout: time.Second})
	proxySrv := httptest.NewServer(handler)
	defer proxySrv.Close()
	resp, err := http.Get(proxySrv.URL + "/v1/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusServiceUnavailable || !strings.Contains(string(body), noUpstreamMessage) {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	entries := log.List()
	if len(entries) != 1 || entries[0].UpstreamName != "none" || entries[0].UpstreamID != "" || entries[0].Source != history.SourceUpstreamError {
		t.Fatalf("entry = %+v", entries)
	}
}

func getBody(t *testing.T, rawURL string) string {
	t.Helper()
	resp, err := http.Get(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
