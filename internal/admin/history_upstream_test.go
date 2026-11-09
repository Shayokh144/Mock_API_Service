package admin

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Shayokh144/Mock_API_Service/internal/history"
	"github.com/Shayokh144/Mock_API_Service/internal/rules"
	"github.com/Shayokh144/Mock_API_Service/internal/upstreams"
)

func TestHistoryUpstreamFilter(t *testing.T) {
	dir := t.TempDir()
	store, err := rules.Load(filepath.Join(dir, "rules.json"))
	if err != nil {
		t.Fatal(err)
	}
	ups, err := upstreams.Load(filepath.Join(dir, "upstreams.json"))
	if err != nil {
		t.Fatal(err)
	}
	staging, err := ups.Create("staging", "https://staging.example.com")
	if err != nil {
		t.Fatal(err)
	}
	develop, err := ups.Create("develop", "https://develop.example.com")
	if err != nil {
		t.Fatal(err)
	}
	log := history.New(20)
	start := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	log.Add(history.Entry{StartedAt: start, Method: "GET", Path: "/v1/staging-pass", Status: 200, Source: history.SourceUpstream, UpstreamID: staging.ID, UpstreamName: "staging"})
	log.Add(history.Entry{StartedAt: start.Add(time.Second), Method: "GET", Path: "/v1/staging-mock", Status: 500, Source: history.SourceMock, UpstreamID: staging.ID, UpstreamName: "staging"})
	log.Add(history.Entry{StartedAt: start.Add(2 * time.Second), Method: "GET", Path: "/v1/any-mock", Status: 201, Source: history.SourceMock, UpstreamID: history.UpstreamAny, UpstreamName: "any"})
	log.Add(history.Entry{StartedAt: start.Add(3 * time.Second), Method: "GET", Path: "/v1/develop-pass", Status: 200, Source: history.SourceUpstream, UpstreamID: develop.ID, UpstreamName: "develop"})

	dashboard, err := New(store, log, ups)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(dashboard)
	defer srv.Close()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	page := get(t, client, srv.URL+"/_admin/history")
	assertContains(t, page, "All upstreams")
	assertContains(t, page, "Any upstream (mock rule not linked)")
	assertContains(t, page, "Upstream: All upstreams.")
	assertContains(t, page, `method="get"`)
	if pageResp, err := client.Get(srv.URL + "/_admin/history"); err != nil {
		t.Fatal(err)
	} else {
		pageResp.Body.Close()
		if pageResp.Header.Get("Referrer-Policy") != "same-origin" {
			t.Fatalf("referrer = %q", pageResp.Header.Get("Referrer-Policy"))
		}
		if !strings.Contains(pageResp.Header.Get("Content-Security-Policy"), "script-src 'self'") {
			t.Fatalf("csp = %q", pageResp.Header.Get("Content-Security-Policy"))
		}
	}

	onlyStaging := get(t, client, srv.URL+"/_admin/history?upstream="+staging.ID)
	assertContains(t, onlyStaging, "/v1/staging-pass")
	assertContains(t, onlyStaging, "/v1/staging-mock")
	assertContains(t, onlyStaging, "Upstream: staging.")
	if strings.Contains(onlyStaging, "/v1/develop-pass") || strings.Contains(onlyStaging, "/v1/any-mock") {
		t.Fatalf("staging filter showed other rows:\n%s", onlyStaging)
	}
	assertContains(t, onlyStaging, "/_admin/history?upstream="+url.QueryEscape(staging.ID))
	assertContains(t, onlyStaging, "source=mock&amp;upstream="+staging.ID)

	combined := get(t, client, srv.URL+"/_admin/history?source=mock&upstream="+staging.ID)
	assertContains(t, combined, "Filter: Mocked. Showing 1 of 4.")
	assertContains(t, combined, "/v1/staging-mock")
	if strings.Contains(combined, "/v1/staging-pass") || strings.Contains(combined, "/v1/any-mock") {
		t.Fatalf("combined filter showed the wrong rows:\n%s", combined)
	}
	assertContains(t, combined, "/delete?source=mock&amp;upstream="+staging.ID)

	anyPage := get(t, client, srv.URL+"/_admin/history?upstream=any")
	assertContains(t, anyPage, "/v1/any-mock")
	assertContains(t, anyPage, "Upstream: Any upstream.")
	if strings.Contains(anyPage, "/v1/staging-mock") || strings.Contains(anyPage, "/v1/develop-pass") {
		t.Fatalf("any filter showed a linked row:\n%s", anyPage)
	}

	bad := get(t, client, srv.URL+"/_admin/history?upstream=nope")
	assertContains(t, bad, "Filter: All. Showing 4 of 4.")
	assertContains(t, bad, "Upstream: All upstreams.")
	assertContains(t, bad, "/v1/develop-pass")
	if strings.Contains(bad, "upstream=nope") {
		t.Fatal("unknown upstream filter was kept")
	}

	feed := getFeed(t, srv.URL+"/_admin/history/feed?source=mock&upstream="+staging.ID)
	if feed.Filter != "mock" || feed.Upstream != staging.ID || feed.Shown != 1 || feed.Total != 4 || len(feed.Entries) != 1 {
		t.Fatalf("feed = %+v", feed)
	}
	if feed.Entries[0].URL != "/v1/staging-mock" || feed.Entries[0].Upstream != "staging" {
		t.Fatalf("feed row = %+v", feed.Entries[0])
	}

	script := readBody(t, mustGet(t, srv.URL+"/_admin/history.js"))
	if !strings.Contains(script, `get("upstream")`) || !strings.Contains(script, "/_admin/history/feed") {
		t.Fatal("history.js does not pass the upstream filter to the feed")
	}

	if _, err := ups.Update(staging.ID, "staging-renamed", "https://staging.example.com/v2"); err != nil {
		t.Fatal(err)
	}
	renamed := get(t, client, srv.URL+"/_admin/history?upstream="+staging.ID)
	assertContains(t, renamed, "/v1/staging-mock")
	assertContains(t, renamed, "Upstream: staging-renamed.")
	assertContains(t, renamed, ">staging<")
	assertContains(t, renamed, `value="`+staging.ID+`" selected>staging-renamed`)

	if err := ups.Activate(develop.ID); err != nil {
		t.Fatal(err)
	}
	if err := ups.Delete(staging.ID); err != nil {
		t.Fatal(err)
	}
	deleted := get(t, client, srv.URL+"/_admin/history?upstream="+staging.ID)
	assertContains(t, deleted, "/v1/staging-pass")
	assertContains(t, deleted, "/v1/staging-mock")
	assertContains(t, deleted, "Upstream: staging (deleted).")
	assertContains(t, deleted, staging.ID+`" selected>staging (deleted)`)
	if strings.Contains(deleted, "staging-renamed") {
		t.Fatal("deleted upstream still uses the new name")
	}

	missing := get(t, client, srv.URL+"/_admin/history?upstream=0011223344556677")
	assertContains(t, missing, "Filter: All. Showing 0 of 4.")
	assertContains(t, missing, "No entries for this filter.")
	assertContains(t, missing, "Deleted upstream")

	clientNoRedirect := client
	gone, err := clientNoRedirect.PostForm(srv.URL+"/_admin/history/"+log.List()[0].ID+"/delete?source=upstream&upstream="+develop.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	gone.Body.Close()
	if gone.StatusCode != http.StatusSeeOther || gone.Header.Get("Location") != "/_admin/history?source=upstream&upstream="+develop.ID {
		t.Fatalf("delete redirect = %d %s", gone.StatusCode, gone.Header.Get("Location"))
	}
}

func mustGet(t *testing.T, rawURL string) *http.Response {
	t.Helper()
	resp, err := http.Get(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}
