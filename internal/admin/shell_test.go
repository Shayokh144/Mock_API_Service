package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Shayokh144/Mock_API_Service/internal/history"
	"github.com/Shayokh144/Mock_API_Service/internal/sessions"
)

func TestShellAndSessionsPage(t *testing.T) {
	store := rulesStore(t)
	log := history.New(5)
	dashboard, err := New(store, log, nil)
	if err != nil {
		t.Fatal(err)
	}
	manager := sessions.New(sessions.Options{
		Rules: store, History: log, ProxyAddr: "127.0.0.1:8080", AdminAddr: "127.0.0.1:8081", HistoryLimit: 5,
	})
	defer manager.Close()
	info, err := manager.Create("login-tests", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	dashboard.UseSessions(manager)
	srv := httptest.NewServer(dashboard)
	defer srv.Close()

	rules := get(t, dashboardClient(), srv.URL+"/_admin")
	for _, want := range []string{
		`class="sidebar"`,
		`href="/_admin/sessions"`,
		`Session <strong>default</strong>`,
		`class="page-head"`,
		`href="/_admin/rules/new">New rule`,
	} {
		if !strings.Contains(rules, want) {
			t.Fatalf("rules shell missing %q", want)
		}
	}

	sessionsPage := get(t, dashboardClient(), srv.URL+"/_admin/sessions")
	if !strings.Contains(sessionsPage, "login-tests") || !strings.Contains(sessionsPage, "read-only") || !strings.Contains(sessionsPage, ">default<") {
		t.Fatalf("sessions page = %s", sessionsPage)
	}
	if !strings.Contains(sessionsPage, `href="/_admin?session=`+info.ID+`"`) {
		t.Fatal("sessions page did not link to the test session")
	}
	csp := headerFor(t, srv.URL+"/_admin/sessions", "Content-Security-Policy")
	if strings.Contains(csp, "script-src") || !strings.Contains(csp, "style-src 'self'") {
		t.Fatalf("sessions csp = %q", csp)
	}
}

func TestHistoryDetailCopyAndPrettyJSON(t *testing.T) {
	if prettyBody(`{"ok":true}`) != "{\n  \"ok\": true\n}" {
		t.Fatalf("pretty = %q", prettyBody(`{"ok":true}`))
	}
	if prettyBody("not json") != "not json" {
		t.Fatal("non-json body was rewritten")
	}
	log := history.New(2)
	entry := log.Add(history.Entry{
		Method: "POST", Path: "/v1/items", Status: 201, Source: history.SourceMock,
		RequestBody:  `{"name":"Sample"}`,
		ResponseBody: `{"id":"item-1","ok":true}`,
	})
	dashboard, err := New(rulesStore(t), log, nil)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(dashboard)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/_admin/history/" + entry.ID)
	if err != nil {
		t.Fatal(err)
	}
	body := readAll(t, resp)
	for _, want := range []string{
		`<summary>Request headers</summary>`,
		`<summary>Response headers</summary>`,
		`id="request-body"`,
		`id="response-body"`,
		`&#34;id&#34;: &#34;item-1&#34;`,
		`data-copy="response-body"`,
		`<script src="/_admin/copy.js"></script>`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("detail missing %q\n%s", want, body)
		}
	}
	scriptResp := mustGet(t, srv.URL+"/_admin/copy.js")
	script := readAll(t, scriptResp)
	if strings.Contains(script, "innerHTML") || !strings.Contains(script, "textContent") {
		t.Fatal("copy.js is not text-only")
	}
}

func headerFor(t *testing.T, rawURL, name string) string {
	t.Helper()
	resp, err := http.Get(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.Header.Get(name)
}
