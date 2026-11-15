package admin

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Shayokh144/Mock_API_Service/internal/history"
	"github.com/Shayokh144/Mock_API_Service/internal/rules"
)

func TestHistorySourceFilter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.json")
	store, err := rules.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(rules.Draft{
		Name:        "Keep me",
		Enabled:     true,
		Method:      "GET",
		PathPattern: "/v1/keep",
		StatusCode:  200,
	}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	log := history.New(10)
	start := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	log.Add(history.Entry{StartedAt: start, Method: "GET", Path: "/v1/real", Status: 200, Source: history.SourceUpstream})
	fresh := log.Add(history.Entry{StartedAt: start.Add(time.Second), Method: "GET", Path: "/v1/fresh", Status: 200, Source: history.SourceUpstream, RuleID: "0123456789abcdef"})
	mockA := log.Add(history.Entry{StartedAt: start.Add(2 * time.Second), Method: "POST", Path: "/v1/mock-a", Status: 500, Source: history.SourceMock, RuleID: "aaaaaaaaaaaaaaaa"})
	log.Add(history.Entry{StartedAt: start.Add(3 * time.Second), Method: "POST", Path: "/v1/mock-b", Status: 404, Source: history.SourceMock, RuleID: "bbbbbbbbbbbbbbbb"})
	log.Add(history.Entry{StartedAt: start.Add(4 * time.Second), Method: "GET", Path: "/v1/down", Status: 502, Source: history.SourceUpstreamError})
	log.Add(history.Entry{StartedAt: start.Add(5 * time.Second), Method: "POST", Path: "/v1/huge", Status: 413, Source: history.SourceRejected})

	dashboard, err := New(store, log, nil)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(dashboard)
	defer srv.Close()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	all := get(t, client, srv.URL+"/_admin/history")
	assertContains(t, all, "Filter: All. Showing 6 of 6.")
	assertContains(t, all, `aria-current="page">All`)
	assertContains(t, all, "upstream (fresh rule)")
	if strings.Contains(all, "<script>") || strings.Contains(all, "onclick=") {
		t.Fatal("history page added an inline script")
	}
	assertContains(t, all, `<script src="/_admin/history.js"></script>`)

	mocked := get(t, client, srv.URL+"/_admin/history?source=mock")
	assertContains(t, mocked, "Filter: Mocked. Showing 2 of 6.")
	assertContains(t, mocked, `aria-current="page">Mocked`)
	assertContains(t, mocked, "/v1/mock-a")
	assertContains(t, mocked, "/v1/mock-b")
	if strings.Contains(mocked, "/v1/fresh") || strings.Contains(mocked, "/v1/real") || strings.Contains(mocked, "/v1/down") {
		t.Fatalf("mocked filter showed a real response:\n%s", mocked)
	}
	assertContains(t, mocked, "/delete?source=mock")
	assertContains(t, mocked, `<dialog id="history-clear-dialog">`)
	assertContains(t, mocked, "Delete all recorded requests? Rules are kept.")
	assertContains(t, mocked, `href="/_admin/history/clear?source=mock"`)
	assertContains(t, mocked, `id="history-clear-form" method="post" action="/_admin/history/clear?source=mock"`)
	assertContains(t, mocked, `id="history-clear-cancel">Cancel`)
	assertContains(t, mocked, `class="danger" type="submit">Clear all`)

	real := get(t, client, srv.URL+"/_admin/history?source=upstream")
	assertContains(t, real, "Filter: Real server. Showing 2 of 6.")
	assertContains(t, real, `aria-current="page">Real server`)
	assertContains(t, real, "/v1/fresh")
	assertContains(t, real, "upstream (fresh rule)")
	assertContains(t, real, "/v1/real")
	if strings.Contains(real, "/v1/mock-a") || strings.Contains(real, "/v1/down") {
		t.Fatalf("real filter showed the wrong rows:\n%s", real)
	}

	invalid := get(t, client, srv.URL+"/_admin/history?source=nope")
	assertContains(t, invalid, "Filter: All. Showing 6 of 6.")
	assertContains(t, invalid, `aria-current="page">All`)
	assertContains(t, invalid, "/v1/huge")
	if strings.Contains(invalid, "source=nope") {
		t.Fatal("invalid filter was kept in a link")
	}

	detail := get(t, client, srv.URL+"/_admin/history/"+fresh.ID+"?source=upstream")
	assertContains(t, detail, "upstream (fresh rule)")
	assertContains(t, detail, `href="/_admin/history?source=upstream"`)
	assertContains(t, detail, "/delete?source=upstream")

	deleted, err := client.PostForm(srv.URL+"/_admin/history/"+mockA.ID+"/delete?source=mock", nil)
	if err != nil {
		t.Fatal(err)
	}
	deleted.Body.Close()
	if deleted.StatusCode != http.StatusSeeOther || deleted.Header.Get("Location") != "/_admin/history?source=mock" {
		t.Fatalf("delete redirect = %d %s", deleted.StatusCode, deleted.Header.Get("Location"))
	}
	if _, ok := log.Get(mockA.ID); ok {
		t.Fatal("deleted mock is still stored")
	}
	afterDelete := get(t, client, srv.URL+"/_admin/history?source=mock")
	assertContains(t, afterDelete, "Filter: Mocked. Showing 1 of 5.")
	assertContains(t, afterDelete, "/v1/mock-b")
	if strings.Contains(afterDelete, "/v1/mock-a") {
		t.Fatal("deleted row is still listed")
	}

	confirm := get(t, client, srv.URL+"/_admin/history/clear?source=mock")
	assertContains(t, confirm, "including entries hidden")
	assertContains(t, confirm, `action="/_admin/history/clear?source=mock"`)
	if len(log.List()) == 0 {
		t.Fatal("opening the confirm page cleared history")
	}
	cleared, err := client.PostForm(srv.URL+"/_admin/history/clear?source=mock", nil)
	if err != nil {
		t.Fatal(err)
	}
	cleared.Body.Close()
	if cleared.StatusCode != http.StatusSeeOther || cleared.Header.Get("Location") != "/_admin/history?source=mock" {
		t.Fatalf("clear redirect = %d %s", cleared.StatusCode, cleared.Header.Get("Location"))
	}
	if len(log.List()) != 0 {
		t.Fatalf("clear left %d entries", len(log.List()))
	}
	empty := get(t, client, srv.URL+cleared.Header.Get("Location"))
	assertContains(t, empty, "Filter: Mocked. Showing 0 of 0.")
	assertContains(t, empty, "No history yet.")
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("history filter actions changed the rules file")
	}
}

func assertContains(t *testing.T, page, want string) {
	t.Helper()
	if !strings.Contains(page, want) {
		t.Fatalf("missing %q\n%s", want, page)
	}
}
