package admin

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Shayokh144/Mock_API_Service/internal/history"
	"github.com/Shayokh144/Mock_API_Service/internal/rules"
)

func TestHTMXHistoryPartialAndSameOrigin(t *testing.T) {
	log := history.New(5)
	log.Add(history.Entry{Method: "GET", Path: "/v1/items", Status: 200, Source: history.SourceMock})
	dashboard, err := New(rulesStore(t), log, nil)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(dashboard)
	defer srv.Close()

	page := get(t, dashboardClient(), srv.URL+"/_admin/history")
	for _, want := range []string{
		`<script src="/_admin/htmx.js"></script>`,
		`includeIndicatorCSS:false`,
		`data-hx-trigger="every 1s"`,
		`data-hx-trigger="keyup changed delay:300ms"`,
		`id="history-live"`,
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("history page missing %q", want)
		}
	}
	if strings.Contains(page, "/_admin/history.js") {
		t.Fatal("history page still loads history.js")
	}
	csp := headerFor(t, srv.URL+"/_admin/history", "Content-Security-Policy")
	if !strings.Contains(csp, "script-src 'self'") || !strings.Contains(csp, "connect-src 'self'") || strings.Contains(csp, "unsafe-inline") {
		t.Fatalf("history csp = %q", csp)
	}

	partial := htmxGet(t, srv.URL+"/_admin/history?source=mock")
	if strings.Contains(partial, "<html") || strings.Contains(partial, "<script") {
		t.Fatal("history partial included the document shell")
	}
	if !strings.Contains(partial, `id="history-live"`) || !strings.Contains(partial, "/v1/items") {
		t.Fatalf("partial = %s", partial)
	}

	log.Add(history.Entry{Method: "GET", Path: "/v1/fresh", Status: 204, Source: history.SourceUpstream})
	again := htmxGet(t, srv.URL+"/_admin/history")
	if !strings.Contains(again, "/v1/fresh") || !strings.Contains(again, "/v1/items") {
		t.Fatalf("partial missed a new row:\n%s", again)
	}

	cross := htmxRequestTo(t, http.MethodGet, srv.URL+"/_admin/history", nil, "cross-site")
	if cross.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin htmx status = %d", cross.StatusCode)
	}
	cross.Body.Close()
}

func TestHTMXRuleToggleReturnsTheList(t *testing.T) {
	store := rulesStore(t)
	created, err := store.Create(rules.Draft{
		Name: "List items", Enabled: true, Method: "GET", PathPattern: "/v1/items", StatusCode: 200,
	})
	if err != nil {
		t.Fatal(err)
	}
	dashboard, err := New(store, history.New(5), nil)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(dashboard)
	defer srv.Close()

	rulesPage := get(t, dashboardClient(), srv.URL+"/_admin")
	if !strings.Contains(rulesPage, `data-hx-trigger="change delay:300ms"`) || !strings.Contains(rulesPage, `data-hx-post="/_admin/rules/`+created.ID+`/toggle"`) {
		t.Fatal("rules page is missing htmx filters or the toggle")
	}

	resp := htmxRequestTo(t, http.MethodPost, srv.URL+"/_admin/rules/"+created.ID+"/toggle", strings.NewReader(""), "same-origin")
	body := readAll(t, resp)
	if resp.StatusCode != http.StatusOK || strings.Contains(body, "<html") {
		t.Fatalf("toggle status = %d body = %s", resp.StatusCode, body)
	}
	if !strings.Contains(body, ">Enable<") || !strings.Contains(body, `id="rules-list"`) {
		t.Fatalf("toggle partial = %s", body)
	}
	saved, _ := store.Get(created.ID)
	if saved.Enabled {
		t.Fatal("rule stayed enabled")
	}

	script := readAll(t, mustGet(t, srv.URL+"/_admin/htmx.js"))
	if !strings.Contains(script, "4.0.2") {
		t.Fatal("vendored htmx is not 4.0.2")
	}
	license, err := assetFiles.ReadFile("static/vendor/htmx-4.0.2/LICENSE")
	if err != nil || !strings.Contains(string(license), "Zero-Clause BSD") {
		t.Fatalf("license = %s %v", license, err)
	}
}

func htmxGet(t *testing.T, rawURL string) string {
	t.Helper()
	resp := htmxRequestTo(t, http.MethodGet, rawURL, nil, "same-origin")
	return readAll(t, resp)
}

func htmxRequestTo(t *testing.T, method, rawURL string, body *strings.Reader, site string) *http.Response {
	t.Helper()
	var reader *strings.Reader
	if body != nil {
		reader = body
	} else {
		reader = strings.NewReader("")
	}
	req, err := http.NewRequest(method, rawURL, reader)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("HX-Request", "true")
	req.Header.Set("Sec-Fetch-Site", site)
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.PostForm = url.Values{}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}
