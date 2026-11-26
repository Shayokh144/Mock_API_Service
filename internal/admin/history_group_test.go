package admin

import (
	"html"
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
		{ID: "older", StartedAt: start, Method: "GET", Path: "/v1/items", RawQuery: "a=1", Status: 304, Source: history.SourceMock, UpstreamID: "u1", UpstreamName: "SHOP", DelayMS: 1500, RuleID: "abc", RuleName: "Saved"},
		{ID: "other-path", StartedAt: start.Add(time.Second), Method: "GET", Path: "/v1/users/111", Status: 200, Source: history.SourceUpstream, UpstreamID: "u1", UpstreamName: "SHOP"},
		{ID: "uuid", StartedAt: start.Add(2 * time.Second), Method: "GET", Path: "/v1/users/222", Status: 200, Source: history.SourceUpstream, UpstreamID: "u1", UpstreamName: "SHOP"},
		{ID: "newer", StartedAt: start.Add(3 * time.Second), Method: "GET", Path: "/v1/items", RawQuery: "b=2", Status: 200, Source: history.SourceUpstream, UpstreamID: "u1", UpstreamName: "SHOP", ViaReferer: true, DelayMS: 1500, RuleID: "abc", RuleName: "Saved"},
		{ID: "other-upstream", StartedAt: start.Add(4 * time.Second), Method: "GET", Path: "/v1/items", Status: 201, Source: history.SourceMock, UpstreamID: "u2", UpstreamName: "Other"},
		{ID: "rejected-new", StartedAt: start.Add(5 * time.Second), Method: "GET", Path: "/v1/missing", Status: 0, Source: history.SourceRejected},
		{ID: "rejected-old", StartedAt: start.Add(-time.Second), Method: "GET", Path: "/v1/missing", Status: 0, Source: history.SourceRejected},
		{ID: "post", StartedAt: start.Add(6 * time.Second), Method: "POST", Path: "/v1/items", Status: 204, Source: history.SourceUpstream, UpstreamID: "u1", UpstreamName: "SHOP"},
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
	if items.Upstream != "SHOP (via Referer)" || items.Path != "/v1/items" {
		t.Fatalf("collapsed item = %s %s", items.Upstream, items.Path)
	}
	if items.StatusSummary != "200 ×1, 304 ×1" || items.Sources != "upstream (fresh rule), mock" {
		t.Fatalf("item summary status=%s sources=%s", items.StatusSummary, items.Sources)
	}
	if items.Delay != "1500 ms" || items.Rule != "Saved (abc)" || len(items.StatusLines) != 2 {
		t.Fatalf("shared summary delay=%q rule=%q lines=%v", items.Delay, items.Rule, items.StatusLines)
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
	shop, err := ups.Create("SHOP", "http://127.0.0.1:9", "/shop")
	if err != nil {
		t.Fatal(err)
	}
	log := history.New(20)
	start := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	older := log.Add(history.Entry{StartedAt: start, Method: "GET", Path: "/v1/items", RawQuery: "a=1", Status: 304, Source: history.SourceMock, UpstreamID: shop.ID, UpstreamName: shop.Name, RuleID: "aaaaaaaaaaaaaaaa", RuleName: "Saved", DelayMS: 1500})
	other := log.Add(history.Entry{StartedAt: start.Add(time.Second), Method: "GET", Path: "/v1/users/111", Status: 200, Source: history.SourceUpstream, UpstreamID: shop.ID, UpstreamName: shop.Name})
	newer := log.Add(history.Entry{StartedAt: start.Add(2 * time.Second), Method: "GET", Path: "/v1/items", RawQuery: "b=2", Status: 200, Source: history.SourceUpstream, UpstreamID: shop.ID, UpstreamName: shop.Name, DelayMS: 1500})
	log.Add(history.Entry{StartedAt: start.Add(3 * time.Second), Method: "GET", Path: "/v1/items", Status: 500, Source: history.SourceMock, UpstreamID: shop.ID, UpstreamName: shop.Name, DelayMS: 1500})

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
	assertContains(t, page, "history-toggle")
	assertContains(t, page, "history-count")
	assertContains(t, page, "/v1/items?b=2")
	assertContains(t, page, "/v1/items?a=1")
	assertContains(t, page, "Create rule")
	assertContains(t, page, "/delete")
	assertContains(t, page, `data-id="`+newer.ID+`"`)
	assertContains(t, page, `data-id="`+older.ID+`"`)
	assertContains(t, page, `class="history-child"`)
	if strings.Contains(historyTable(page), "<details>") || strings.Contains(page, `colspan="9"`) {
		t.Fatalf("group summary is not a table row:\n%s", page)
	}
	if strings.Contains(page, `href="/_admin/history?group=0"`) {
		t.Fatalf("default links turned grouping off:\n%s", page)
	}
	cells := historyGroupCells(t, page)
	if len(cells) != 9 {
		t.Fatalf("group cells = %d\n%s", len(cells), page)
	}
	for i, want := range []string{"GET", "▶", "200 ×1", "mock", "SHOP", "1500 ms", "", formatHistoryTime(start.Add(3 * time.Second)), ""} {
		if want != "" && !strings.Contains(html.UnescapeString(cells[i]), want) {
			t.Fatalf("cell %d = %q, want %q", i, cells[i], want)
		}
	}
	if !strings.Contains(cells[1], `data-field="path">/v1/items</span>`) || !strings.Contains(cells[1], "3 requests") {
		t.Fatalf("url cell = %s", cells[1])
	}
	if !strings.Contains(cells[2], "<br>") || !strings.Contains(cells[2], "304 ×1") || !strings.Contains(cells[2], "500 ×1") {
		t.Fatalf("status cell = %s", cells[2])
	}
	if !strings.Contains(cells[3], "<br>") || !strings.Contains(cells[3], "upstream") {
		t.Fatalf("source cell = %s", cells[3])
	}
	if strings.Contains(cells[6], "Saved") || strings.Contains(cells[8], "Delete") {
		t.Fatalf("rule/actions cells = %q %q", cells[6], cells[8])
	}
	if strings.Index(page, `data-field="path">/v1/items`) > strings.Index(page, "/v1/users/111") {
		t.Fatalf("group order put the older path first:\n%s", page)
	}
	if strings.Count(page, `class="history-group"`) != 1 {
		t.Fatalf("single requests were wrapped:\n%s", page)
	}
	if !strings.Contains(page, `class="history-child" data-id="`+newer.ID+`" data-group="`) || !strings.Contains(page, " hidden>") {
		t.Fatalf("closed children are visible:\n%s", page)
	}

	opened := get(t, client, srv.URL+historyTogglePath(t, page))
	if strings.Contains(opened, `class="history-child" data-id="`+newer.ID+`" data-group="`) && strings.Contains(opened, `data-id="`+newer.ID+`" data-group="`) {
		child := opened[strings.Index(opened, `class="history-child"`):]
		end := strings.Index(child, ">")
		if strings.Contains(child[:end], "hidden") {
			openAt := strings.Index(opened, `data-open=`)
			snippet := ""
			if openAt >= 0 {
				snippet = opened[openAt : openAt+80]
			}
			t.Fatalf("open link left the child hidden: %s\npath %s\n%s", child[:end], historyTogglePath(t, page), snippet)
		}
	} else {
		t.Fatalf("opened page lost the child row:\n%s", opened)
	}
	assertContains(t, opened, "▼")
	assertContains(t, opened, `aria-expanded="true"`)

	filtered := get(t, client, srv.URL+"/_admin/history?source=upstream")
	assertContains(t, filtered, "Filter: Real server. Showing 2 of 4 requests in 2 groups.")
	assertContains(t, filtered, "/v1/items?b=2")
	assertContains(t, filtered, "/v1/users/111")
	if strings.Contains(historyTable(filtered), "<details>") || strings.Contains(filtered, "/v1/items?a=1") {
		t.Fatalf("filter ran after grouping:\n%s", filtered)
	}

	flat := get(t, client, srv.URL+"/_admin/history?group=0")
	assertContains(t, flat, "Filter: All. Showing 4 of 4.")
	assertContains(t, flat, `<option value="0" selected>Off</option>`)
	assertContains(t, flat, `href="/_admin/history?group=0&amp;source=mock"`)
	assertContains(t, flat, "/delete?group=0")
	if strings.Contains(historyTable(flat), "<details>") || strings.Contains(flat, "requests in") {
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
	for _, want := range []string{`data-kind`, `group_key`, `params.get("group")`, `history-group`, `history-toggle`, `data-open`} {
		if !strings.Contains(script, want) {
			t.Fatalf("history.js missing %q", want)
		}
	}
	if strings.Contains(script, "innerHTML") {
		t.Fatal("history.js inserts html")
	}
}

func historyTable(page string) string {
	start := strings.Index(page, `<table class="history"`)
	if start < 0 {
		return ""
	}
	return page[start:]
}

func historyGroupCells(t *testing.T, page string) []string {
	t.Helper()
	start := strings.Index(page, `<tr class="history-group"`)
	if start < 0 {
		t.Fatalf("no group row:\n%s", page)
	}
	row := page[start:]
	end := strings.Index(row, "</tr>")
	if end < 0 {
		t.Fatal("group row did not end")
	}
	row = row[:end]
	var cells []string
	for {
		td := strings.Index(row, "<td")
		if td < 0 {
			break
		}
		row = row[td:]
		close := strings.Index(row, "</td>")
		if close < 0 {
			t.Fatal("cell did not end")
		}
		cells = append(cells, row[:close])
		row = row[close+5:]
	}
	return cells
}

func historyTogglePath(t *testing.T, page string) string {
	t.Helper()
	const marker = `class="history-toggle" href="`
	start := strings.Index(page, marker)
	if start < 0 {
		t.Fatalf("no toggle link:\n%s", page)
	}
	rest := page[start+len(marker):]
	end := strings.Index(rest, `"`)
	if end < 0 {
		t.Fatal("toggle href did not end")
	}
	return html.UnescapeString(rest[:end])
}
