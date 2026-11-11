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

func TestHistoryTextFilter(t *testing.T) {
	dir := t.TempDir()
	store, err := rules.Load(filepath.Join(dir, "rules.json"))
	if err != nil {
		t.Fatal(err)
	}
	ups, err := upstreams.Load(filepath.Join(dir, "upstreams.json"))
	if err != nil {
		t.Fatal(err)
	}
	okya, err := ups.Create("OKYA_AUTH_DEV", "http://127.0.0.1:9", "")
	if err != nil {
		t.Fatal(err)
	}
	other, err := ups.Create("develop", "http://127.0.0.1:9", "")
	if err != nil {
		t.Fatal(err)
	}
	log := history.New(10)
	start := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	log.Add(history.Entry{StartedAt: start, Method: "GET", Path: "/API/User/Me", Status: 200, Source: history.SourceMock, UpstreamID: okya.ID, UpstreamName: okya.Name})
	log.Add(history.Entry{StartedAt: start.Add(time.Second), Method: "GET", Path: "/oauth/token", Status: 200, Source: history.SourceUpstream, UpstreamID: okya.ID, UpstreamName: okya.Name})
	log.Add(history.Entry{StartedAt: start.Add(2 * time.Second), Method: "GET", Path: "/v1/items", RawQuery: "name=user/me", Status: 200, Source: history.SourceMock, UpstreamID: other.ID, UpstreamName: other.Name})
	log.Add(history.Entry{StartedAt: start.Add(3 * time.Second), Method: "GET", Path: "/api/users", Status: 200, Source: history.SourceMock, UpstreamID: other.ID, UpstreamName: other.Name})

	dashboard, err := New(store, log, ups)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(dashboard)
	defer srv.Close()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	page := get(t, client, srv.URL+"/_admin/history?q=user/me")
	assertContains(t, page, "Filter: All. Text: user/me. Showing 2 of 4.")
	assertContains(t, page, `name="q"`)
	assertContains(t, page, `value="user/me"`)
	assertContains(t, page, "/API/User/Me")
	assertContains(t, page, "/v1/items?name=user/me")
	if strings.Contains(page, "/oauth/token") || strings.Contains(page, "/api/users") {
		t.Fatalf("text filter showed a non-matching row:\n%s", page)
	}
	assertContains(t, page, `href="/_admin/history?q=user%2Fme&amp;source=mock"`)
	assertContains(t, page, `href="/_admin/history?q=user%2Fme&amp;source=upstream"`)
	assertContains(t, page, `href="/_admin/history?q=user%2Fme"`)

	combined := get(t, client, srv.URL+"/_admin/history?source=mock&upstream="+other.ID+"&q=USER/ME")
	assertContains(t, combined, "Filter: Mocked. Text: USER/ME. Showing 1 of 4.")
	assertContains(t, combined, "/v1/items?name=user/me")
	if strings.Contains(combined, "/API/User/Me") || strings.Contains(combined, "/api/users") {
		t.Fatalf("combined filter showed the wrong rows:\n%s", combined)
	}
	assertContains(t, combined, "q=USER%2FME")
	assertContains(t, combined, "source=mock")
	assertContains(t, combined, "upstream="+other.ID)

	blank := get(t, client, srv.URL+"/_admin/history?q=%20%20")
	assertContains(t, blank, "Filter: All. Showing 4 of 4.")
	if strings.Contains(blank, "q=") {
		t.Fatal("blank text filter was kept")
	}

	feed := getFeed(t, srv.URL+"/_admin/history/feed?q=user/me&source=mock")
	if feed.Text != "user/me" || feed.Filter != "mock" || feed.Shown != 2 || feed.Total != 4 || len(feed.Entries) != 2 {
		t.Fatalf("feed = %+v", feed)
	}
	if feed.Entries[0].URL != "/v1/items?name=user/me" || feed.Entries[1].URL != "/API/User/Me" {
		t.Fatalf("feed urls = %+v", feed.Entries)
	}

	scriptResp, err := client.Get(srv.URL + "/_admin/history.js")
	if err != nil {
		t.Fatal(err)
	}
	script := readAll(t, scriptResp)
	for _, want := range []string{`kept.set("q"`, `addEventListener("input"`, "history-text-filter", "Text: "} {
		if !strings.Contains(script, want) {
			t.Fatalf("history.js missing %q", want)
		}
	}
}

func TestRulesUpstreamFilter(t *testing.T) {
	dir := t.TempDir()
	store, err := rules.Load(filepath.Join(dir, "rules.json"))
	if err != nil {
		t.Fatal(err)
	}
	ups, err := upstreams.Load(filepath.Join(dir, "upstreams.json"))
	if err != nil {
		t.Fatal(err)
	}
	okya, err := ups.Create("OKYA_AUTH_DEV", "http://127.0.0.1:9", "")
	if err != nil {
		t.Fatal(err)
	}
	other, err := ups.Create("develop", "http://127.0.0.1:9", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, draft := range []rules.Draft{
		{Name: "Refresh", Enabled: true, Method: "POST", PathPattern: "/oauth/token", StatusCode: 401, UpstreamID: okya.ID},
		{Name: "Shared", Enabled: true, Method: "GET", PathPattern: "/v1/health", StatusCode: 200},
		{Name: "Develop only", Enabled: true, Method: "GET", PathPattern: "/v1/dev", StatusCode: 200, UpstreamID: other.ID},
	} {
		if _, err := store.Create(draft); err != nil {
			t.Fatal(err)
		}
	}
	dashboard, err := New(store, history.New(10), ups)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(dashboard)
	defer srv.Close()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	named := get(t, client, srv.URL+"/_admin?upstream="+url.QueryEscape("OKYA_AUTH_DEV"))
	assertContains(t, named, "Upstream: OKYA_AUTH_DEV. Showing 2 of 3.")
	assertContains(t, named, "Refresh")
	assertContains(t, named, "Shared")
	assertContains(t, named, `method="get"`)
	assertContains(t, named, `value="OKYA_AUTH_DEV" selected`)
	assertContains(t, named, "includes rules set to Any")
	if strings.Contains(named, "Develop only") {
		t.Fatalf("named filter showed the other upstream:\n%s", named)
	}

	anyPage := get(t, client, srv.URL+"/_admin/rules?upstream=any")
	assertContains(t, anyPage, `action="/_admin/rules"`)
	assertContains(t, anyPage, "Upstream: Any. Showing 1 of 3.")
	assertContains(t, anyPage, "Shared")
	assertContains(t, anyPage, `value="any" selected`)
	if strings.Contains(anyPage, "Refresh") || strings.Contains(anyPage, "Develop only") {
		t.Fatalf("any filter showed a named rule:\n%s", anyPage)
	}

	unknown := get(t, client, srv.URL+"/_admin?upstream=missing")
	assertContains(t, unknown, "Upstream: All upstreams. Showing 3 of 3.")
	assertContains(t, unknown, "Develop only")
	if strings.Contains(unknown, "upstream=missing") || strings.Contains(unknown, `value="missing"`) {
		t.Fatal("unknown upstream filter was kept")
	}
}
