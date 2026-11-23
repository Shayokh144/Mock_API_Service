package admin

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Shayokh144/Mock_API_Service/internal/history"
	"github.com/Shayokh144/Mock_API_Service/internal/rules"
	"github.com/Shayokh144/Mock_API_Service/internal/upstreams"
)

// Destructive actions open one shared in-page dialog driven by data
// attributes. Without JavaScript the plain form or link still works.
func TestConfirmDialogForDestructiveActions(t *testing.T) {
	dir := t.TempDir()
	store, err := rules.Load(filepath.Join(dir, "rules.json"))
	if err != nil {
		t.Fatal(err)
	}
	ups, err := upstreams.Load(filepath.Join(dir, "upstreams.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ups.Create("staging", "https://staging.example", "/staging"); err != nil {
		t.Fatal(err)
	}
	rule, err := store.Create(rules.Draft{
		Name: "Login", APIName: "auth.login", Enabled: true, Method: "POST",
		PathPattern: "/v1/auth/login", StatusCode: 401, DelayMS: 250,
	})
	if err != nil {
		t.Fatal(err)
	}
	log := history.New(10)
	log.Add(history.Entry{StartedAt: time.Now(), Method: "GET", Path: "/v1/me", Status: 200, Source: history.SourceUpstream})
	dashboard, err := New(store, log, ups)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(dashboard)
	defer srv.Close()
	client := dashboardClient()

	dialog := `<dialog id="confirm-dialog"`
	script := `<script src="/_admin/confirm.js"></script>`

	rulesPage := get(t, client, srv.URL+"/_admin")
	assertContains(t, rulesPage, dialog)
	assertContains(t, rulesPage, script)
	assertContains(t, rulesPage, `action="/_admin/rules/`+rule.ID+`/delete" data-confirm="Delete the rule Login? This cannot be undone."`)
	assertContains(t, rulesPage, `data-confirm-label="Delete rule"`)
	// Compact table: five columns, stable name shown under the name.
	if n := strings.Count(rulesPage, `<th scope="col">`); n != 5 {
		t.Fatalf("rules table has %d header cells, want 5", n)
	}
	assertContains(t, rulesPage, `Stable name <code>auth.login</code>`)
	assertContains(t, rulesPage, `data-label="Settings"`)

	upPage := get(t, client, srv.URL+"/_admin/upstreams")
	assertContains(t, upPage, dialog)
	assertContains(t, upPage, `data-confirm="Delete the upstream staging?`)

	historyPage := get(t, client, srv.URL+"/_admin/history")
	assertContains(t, historyPage, dialog)
	assertContains(t, historyPage, `href="/_admin/history/clear" data-confirm=`)
	assertContains(t, historyPage, `data-confirm-post="/_admin/history/clear"`)

	for name, page := range map[string]string{"rules": rulesPage, "upstreams": upPage, "history": historyPage} {
		if strings.Contains(page, "<script>") || strings.Contains(page, "onclick=") || strings.Contains(page, "style=") {
			t.Fatalf("%s page has inline script or style", name)
		}
	}

	// No JavaScript: the link still opens the confirmation page with a POST form.
	fallback := get(t, client, srv.URL+"/_admin/history/clear?source=mock")
	assertContains(t, fallback, `<form method="post" action="/_admin/history/clear?source=mock">`)

	// Pages without destructive actions do not load the dialog.
	sessionsPage := get(t, client, srv.URL+"/_admin/sessions")
	if strings.Contains(sessionsPage, dialog) || strings.Contains(sessionsPage, "confirm.js") {
		t.Fatal("sessions page loads the confirm dialog")
	}

	resp, err := client.Get(srv.URL + "/_admin/confirm.js")
	if err != nil {
		t.Fatal(err)
	}
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/javascript") {
		t.Fatalf("confirm.js status %d type %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	for _, want := range []string{"showModal", "data-confirm-post", "textContent", "form.submit()"} {
		if !strings.Contains(body, want) {
			t.Fatalf("confirm.js missing %q", want)
		}
	}
	if strings.Contains(body, "innerHTML") {
		t.Fatal("confirm.js inserts html")
	}
}

// A request that is alone in its group renders as a plain row. It used to be
// marked hidden like a collapsed child, so it never showed without JavaScript.
func TestHistorySingleRequestGroupIsVisible(t *testing.T) {
	store, err := rules.Load(filepath.Join(t.TempDir(), "rules.json"))
	if err != nil {
		t.Fatal(err)
	}
	log := history.New(10)
	start := time.Now()
	single := log.Add(history.Entry{StartedAt: start, Method: "POST", Path: "/login", Status: 404, Source: history.SourceRejected})
	log.Add(history.Entry{StartedAt: start.Add(time.Second), Method: "GET", Path: "/v1/me", Status: 200, Source: history.SourceUpstream})
	log.Add(history.Entry{StartedAt: start.Add(2 * time.Second), Method: "GET", Path: "/v1/me", Status: 200, Source: history.SourceUpstream})
	dashboard, err := New(store, log, nil)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(dashboard)
	defer srv.Close()
	page := get(t, dashboardClient(), srv.URL+"/_admin/history")
	row := `<tr data-id="` + single.ID + `"`
	at := strings.Index(page, row)
	if at < 0 {
		t.Fatalf("single row missing or marked as a child:\n%s", page)
	}
	end := strings.Index(page[at:], ">")
	if strings.Contains(page[at:at+end], "hidden") {
		t.Fatalf("single row is hidden: %s", page[at:at+end+1])
	}
	assertContains(t, page, `class="history-child"`)
}
