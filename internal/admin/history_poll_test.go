package admin

import (
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Shayokh144/Mock_API_Service/internal/history"
	"github.com/Shayokh144/Mock_API_Service/internal/rules"
)

// The live refresh must not replace the filter form. When it did, every poll
// swapped out the Upstream and Group selects, which closed an open dropdown
// and threw away the user's pick.
func TestHistoryPollLeavesFiltersAlone(t *testing.T) {
	store, err := rules.Load(filepath.Join(t.TempDir(), "rules.json"))
	if err != nil {
		t.Fatal(err)
	}
	log := history.New(10)
	log.Add(history.Entry{StartedAt: time.Now(), Method: "GET", Path: "/v1/me", Status: 200, Source: history.SourceUpstream})
	dashboard, err := New(store, log, nil)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(dashboard)
	defer srv.Close()

	page := get(t, dashboardClient(), srv.URL+"/_admin/history")
	if n := strings.Count(page, `data-hx-trigger="every`); n != 1 {
		t.Fatalf("history page has %d polling elements, want 1", n)
	}
	poll := strings.Index(page, `data-hx-trigger="every 1s"`)
	open := strings.LastIndex(page[:poll], "<")
	tag := page[open : poll+strings.Index(page[poll:], ">")+1]
	for _, want := range []string{`<div id="history-results"`, `data-hx-select="#history-results"`, `data-hx-swap="outerHTML"`, `data-hx-include="#history-filters"`} {
		if !strings.Contains(tag, want) {
			t.Fatalf("poller %s missing %q", tag, want)
		}
	}
	filters := strings.Index(page, `id="history-filters"`)
	if filters < 0 || filters > open {
		t.Fatal("filter form is inside the polled element")
	}
	polled := page[open:]
	for _, bad := range []string{"<select", "<form id=\"history-filters\"", `id="history-text-filter"`} {
		if strings.Contains(polled, bad) {
			t.Fatalf("polled region contains %q", bad)
		}
	}
	live := strings.Index(page, `<div id="history-live"`)
	liveTag := page[live : live+strings.Index(page[live:], ">")+1]
	if strings.Contains(liveTag, "data-hx-trigger") {
		t.Fatalf("history-live still polls: %s", liveTag)
	}

	// The poll response carries the results block that hx-select picks out.
	partial := htmxGet(t, srv.URL+"/_admin/history")
	if !strings.Contains(partial, `<div id="history-results"`) || !strings.Contains(partial, "/v1/me") || strings.Contains(partial, "<html") {
		t.Fatalf("poll response = %s", partial)
	}
}
