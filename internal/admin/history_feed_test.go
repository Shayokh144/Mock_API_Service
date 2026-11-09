package admin

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Shayokh144/Mock_API_Service/internal/history"
	"github.com/Shayokh144/Mock_API_Service/internal/rules"
)

func TestHistoryFeedFiltersAndGrows(t *testing.T) {
	dir := t.TempDir()
	store, err := rules.Load(filepath.Join(dir, "rules.json"))
	if err != nil {
		t.Fatal(err)
	}
	log := history.New(10)
	start := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	mock := log.Add(history.Entry{
		StartedAt: start,
		Method:    http.MethodPost,
		Path:      "/v1/mock",
		Status:    500,
		Source:    history.SourceMock,
		RuleID:    "aaaaaaaaaaaaaaaa",
	})
	fresh := log.Add(history.Entry{
		StartedAt: start.Add(time.Second),
		Method:    http.MethodGet,
		Path:      "/v1/fresh",
		RawQuery:  "page=2",
		Status:    200,
		Source:    history.SourceUpstream,
		RuleID:    "bbbbbbbbbbbbbbbb",
	})
	dashboard, err := New(store, log, nil)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(dashboard)
	defer srv.Close()

	mocked := getFeed(t, srv.URL+"/_admin/history/feed?source=mock")
	if mocked.Filter != "mock" || mocked.Label != "Mocked" || mocked.Shown != 1 || mocked.Total != 2 {
		t.Fatalf("mock feed = %+v", mocked)
	}
	if len(mocked.Entries) != 1 || mocked.Entries[0].ID != mock.ID || mocked.Entries[0].URL != "/v1/mock" {
		t.Fatalf("mock rows = %+v", mocked.Entries)
	}

	real := getFeed(t, srv.URL+"/_admin/history/feed?source=upstream")
	if real.Shown != 1 || real.Entries[0].ID != fresh.ID || real.Entries[0].URL != "/v1/fresh?page=2" {
		t.Fatalf("upstream rows = %+v", real.Entries)
	}
	if real.Entries[0].Source != "upstream (fresh rule)" {
		t.Fatalf("source = %q", real.Entries[0].Source)
	}
	if real.Entries[0].When != formatHistoryTime(fresh.StartedAt) {
		t.Fatalf("when = %q", real.Entries[0].When)
	}

	all := getFeed(t, srv.URL+"/_admin/history/feed?source=nope")
	if all.Filter != "all" || all.Shown != 2 || all.Total != 2 || all.Entries[0].ID != fresh.ID {
		t.Fatalf("invalid source feed = %+v", all)
	}

	newer := log.Add(history.Entry{
		StartedAt: start.Add(2 * time.Second),
		Method:    http.MethodGet,
		Path:      "/v1/new",
		Status:    204,
		Source:    history.SourceUpstream,
	})
	again := getFeed(t, srv.URL+"/_admin/history/feed?source=upstream")
	if again.Shown != 2 || again.Total != 3 || again.Entries[0].ID != newer.ID || again.Entries[1].ID != fresh.ID {
		t.Fatalf("grown feed = %+v", again.Entries)
	}

	pageResp, err := http.Get(srv.URL + "/_admin/history?source=upstream")
	if err != nil {
		t.Fatal(err)
	}
	page := readBody(t, pageResp)
	csp := pageResp.Header.Get("Content-Security-Policy")
	if !strings.Contains(csp, "script-src 'self'") || !strings.Contains(csp, "connect-src 'self'") {
		t.Fatalf("list CSP = %s", csp)
	}
	for _, want := range []string{
		`<script src="/_admin/history.js"></script>`,
		`id="history-summary"`,
		`data-id="` + fresh.ID + `"`,
		"/v1/fresh?page=2",
		"Reload the page if JavaScript is off.",
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("list missing %s\n%s", want, page)
		}
	}

	detailResp, err := http.Get(srv.URL + "/_admin/history/" + fresh.ID)
	if err != nil {
		t.Fatal(err)
	}
	detailResp.Body.Close()
	if strings.Contains(detailResp.Header.Get("Content-Security-Policy"), "script-src") {
		t.Fatal("detail page allows scripts")
	}

	scriptResp, err := http.Get(srv.URL + "/_admin/history.js")
	if err != nil {
		t.Fatal(err)
	}
	script := readBody(t, scriptResp)
	if scriptResp.Header.Get("Content-Type") != "text/javascript; charset=utf-8" {
		t.Fatalf("script type = %s", scriptResp.Header.Get("Content-Type"))
	}
	if !strings.Contains(script, "/_admin/history/feed") || !strings.Contains(script, "textContent") || strings.Contains(script, "innerHTML") {
		t.Fatal("history.js does not insert text safely")
	}
}

func getFeed(t *testing.T, rawURL string) historyFeed {
	t.Helper()
	resp, err := http.Get(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var payload historyFeed
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	return payload
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
