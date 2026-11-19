package admin

import (
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Shayokh144/Mock_API_Service/internal/history"
	"github.com/Shayokh144/Mock_API_Service/internal/rules"
	"github.com/Shayokh144/Mock_API_Service/internal/upstreams"
)

func TestGroupHistoryKeyOrderAndSummary(t *testing.T) {
	start := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	entries := []history.Entry{
		{ID: "older", StartedAt: start, Method: "GET", Path: "/v1/items", RawQuery: "a=1", Status: 304, Source: history.SourceMock, UpstreamID: "u1", UpstreamName: "OKYA"},
		{ID: "other-path", StartedAt: start.Add(time.Second), Method: "GET", Path: "/v1/users/111", Status: 200, Source: history.SourceUpstream, UpstreamID: "u1", UpstreamName: "OKYA"},
		{ID: "uuid", StartedAt: start.Add(2 * time.Second), Method: "GET", Path: "/v1/users/222", Status: 200, Source: history.SourceUpstream, UpstreamID: "u1", UpstreamName: "OKYA"},
		{ID: "newer", StartedAt: start.Add(3 * time.Second), Method: "GET", Path: "/v1/items", RawQuery: "b=2", Status: 200, Source: history.SourceUpstream, UpstreamID: "u1", UpstreamName: "OKYA", ViaReferer: true},
		{ID: "other-upstream", StartedAt: start.Add(4 * time.Second), Method: "GET", Path: "/v1/items", Status: 201, Source: history.SourceMock, UpstreamID: "u2", UpstreamName: "Other"},
		{ID: "rejected-new", StartedAt: start.Add(5 * time.Second), Method: "GET", Path: "/v1/missing", Status: 0, Source: history.SourceRejected},
		{ID: "rejected-old", StartedAt: start.Add(-time.Second), Method: "GET", Path: "/v1/missing", Status: 0, Source: history.SourceRejected},
		{ID: "post", StartedAt: start.Add(6 * time.Second), Method: "POST", Path: "/v1/items", Status: 204, Source: history.SourceUpstream, UpstreamID: "u1", UpstreamName: "OKYA"},
	}
	lines := historyLines(entries, "", false)
	if historyGroupKey(entries[0]) != historyGroupKey(entries[3]) {
		t.Fatal("query string split a group")
	}
	if historyGroupKey(entries[1]) == historyGroupKey(entries[2]) {
		t.Fatal("different paths were merged")
	}
	if historyGroupKey(entries[0]) == historyGroupKey(entries[4]) {
		t.Fatal("different upstreams were merged")
	}
	groups := groupHistory(entries, lines)
	if len(groups) != 6 {
		t.Fatalf("groups = %d", len(groups))
	}
	if groups[0].Method != "POST" || groups[0].Path != "/v1/items" || len(groups[0].Rows) != 1 {
		t.Fatalf("newest group = %+v", groups[0])
	}
	rejected := groups[1]
	if rejected.Path != "/v1/missing" || rejected.Upstream != "" || len(rejected.Rows) != 2 || rejected.Rows[0].ID != "rejected-new" {
		t.Fatalf("rejected group = %+v rows=%+v", rejected, rejected.Rows)
	}
	if rejected.CountLabel != "2 requests" || rejected.StatusSummary != "0 ×2" || rejected.Sources != "rejected" {
		t.Fatalf("rejected summary = %s %s %s", rejected.CountLabel, rejected.StatusSummary, rejected.Sources)
	}
	var items historyGroup
	for _, group := range groups {
		if group.Path == "/v1/items" && strings.Contains(group.Key, "u1") && group.Method == "GET" {
			items = group
		}
	}
	if len(items.Rows) != 2 || items.Rows[0].ID != "newer" || items.Rows[1].ID != "older" {
		t.Fatalf("item rows = %+v", items.Rows)
	}
	if items.Upstream != "OKYA (via Referer)" || items.Path != "/v1/items" {
		t.Fatalf("collapsed item = %s %s", items.Upstream, items.Path)
	}
	if items.StatusSummary != "200 ×1, 304 ×1" || items.Sources != "upstream, mock" {
		t.Fatalf("item summary status=%s sources=%s", items.StatusSummary, items.Sources)
	}
	if groups[len(groups)-1].Path != "/v1/users/111" {
		t.Fatalf("oldest group = %+v", groups[len(groups)-1])
	}
}

func TestHistoryPageGroupsAfterFiltersAndToggle(t *testing.T) {
	dir := t.TempDir()
	store, err := rules.Load(filepath.Join(dir, "rules.json"))
	if err != nil {
		t.Fatal(err)
	}
	ups, err := upstreams.Load(filepath.Join(dir, "upstreams.json"))
	if err != nil {
		t.Fatal(err)
	}
	okya, err := ups.Create("OKYA", "http://127.0.0.1:9", "/okya")
	if err != nil {
		t.Fatal(err)
	}
	log := history.New(20)
	start := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	older := log.Add(history.Entry{StartedAt: start, Method: "GET", Path: "/v1/items", RawQuery: "a=1", Status: 304, Source: history.SourceMock, UpstreamID: okya.ID, UpstreamName: okya.Name, RuleID: "aaaaaaaaaaaaaaaa", RuleName: "Saved"})
	other := log.Add(history.Entry{StartedAt: start.Add(time.Second), Method: "GET", Path: "/v1/users/111", Status: 200, Source: history.SourceUpstream, UpstreamID: okya.ID, UpstreamName: okya.Name})
	newer := log.Add(history.Entry{StartedAt: start.Add(2 * time.Second), Method: "GET", Path: "/v1/items", RawQuery: "b=2", Status: 200, Source: history.SourceUpstream, UpstreamID: okya.ID, UpstreamName: okya.Name})
	log.Add(history.Entry{StartedAt: start.Add(3 * time.Second), Method: "GET", Path: "/v1/items", Status: 500, Source: history.SourceMock, UpstreamID: okya.ID, UpstreamName: okya.Name})

	dashboard, err := New(store, log, ups)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(dashboard)
	defer srv.Close()
	client := dashboardClient()

	page := get(t, client, srv.URL+"/_admin/history")
	assertContains(t, page, "Filter: All. Showing 4 of 4 requests in 2 groups.")
	assertContains(t, page, `id="history-group"`)
	assertContains(t, page, `<option value="1" selected>On</option>`)
	assertContains(t, page, "<details>")
	assertContains(t, page, "<summary>")
	assertContains(t, page, "3 requests")
	assertContains(t, page, "200 ×1, 304 ×1, 500 ×1")
	assertContains(t, page, "mock, upstream")
	assertContains(t, page, "/v1/items?b=2")
	assertContains(t, page, "/v1/items?a=1")
	assertContains(t, page, "Create rule")
	assertContains(t, page, "/delete")
	assertContains(t, page, `data-id="`+newer.ID+`"`)
	assertContains(t, page, `data-id="`+older.ID+`"`)
	if strings.Contains(page, `href="/_admin/history?group=0"`) {
		t.Fatalf("default links turned grouping off:\n%s", page)
	}
	itemsAt := strings.Index(page, `data-field="path">/v1/items`)
	usersAt := strings.Index(page, "/v1/users/111")
	if itemsAt < 0 || usersAt < 0 || itemsAt > usersAt {
		t.Fatalf("group order put the older path first:\n%s", page)
	}
	if strings.Count(page, "<details>") != 1 {
		t.Fatalf("single requests were wrapped:\n%s", page)
	}
	if strings.Contains(page, "/v1/users/111?") {
		t.Fatal("a different path was pulled into the items group")
	}

	filtered := get(t, client, srv.URL+"/_admin/history?source=upstream")
	assertContains(t, filtered, "Filter: Real server. Showing 2 of 4 requests in 2 groups.")
	assertContains(t, filtered, "/v1/items?b=2")
	assertContains(t, filtered, "/v1/users/111")
	if strings.Contains(filtered, "<details>") || strings.Contains(filtered, "/v1/items?a=1") {
		t.Fatalf("filter ran after grouping:\n%s", filtered)
	}

	flat := get(t, client, srv.URL+"/_admin/history?group=0")
	assertContains(t, flat, "Filter: All. Showing 4 of 4.")
	assertContains(t, flat, `<option value="0" selected>Off</option>`)
	assertContains(t, flat, `href="/_admin/history?group=0&amp;source=mock"`)
	assertContains(t, flat, "/delete?group=0")
	if strings.Contains(flat, "<details>") || strings.Contains(flat, "requests in") {
		t.Fatalf("group=0 still grouped:\n%s", flat)
	}
	if strings.Index(flat, newer.ID) > strings.Index(flat, other.ID) || strings.Index(flat, other.ID) > strings.Index(flat, older.ID) {
		t.Fatalf("flat list is not newest first:\n%s", flat)
	}

	feed := getFeed(t, srv.URL+"/_admin/history/feed")
	if !feed.Grouped || feed.Groups != 2 || feed.Shown != 4 {
		t.Fatalf("feed groups = %+v", feed)
	}
	if feed.Entries[0].GroupKey == "" || feed.Entries[0].Path != "/v1/items" {
		t.Fatalf("feed row = %+v", feed.Entries[0])
	}
	off := getFeed(t, srv.URL+"/_admin/history/feed?group=0")
	if off.Grouped || off.Groups != 0 || off.Shown != 4 {
		t.Fatalf("ungrouped feed = %+v", off)
	}

	script := readBody(t, mustGet(t, srv.URL+"/_admin/history.js"))
	for _, want := range []string{`data-kind`, `group_key`, `params.get("group")`, `history-group`, `createElement("details")`} {
		if !strings.Contains(script, want) {
			t.Fatalf("history.js missing %q", want)
		}
	}
	if strings.Contains(script, "innerHTML") {
		t.Fatal("history.js inserts html")
	}
}
