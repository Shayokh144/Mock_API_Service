package admin

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Shayokh144/Mock_API_Service/internal/history"
	"github.com/Shayokh144/Mock_API_Service/internal/rules"
	"github.com/Shayokh144/Mock_API_Service/internal/upstreams"
)

func TestRuleFormOptionalGroups(t *testing.T) {
	dir := t.TempDir()
	store, err := rules.Load(filepath.Join(dir, "rules.json"))
	if err != nil {
		t.Fatal(err)
	}
	ups, err := upstreams.Load(filepath.Join(dir, "upstreams.json"))
	if err != nil {
		t.Fatal(err)
	}
	demo, err := ups.Create("demo", "http://127.0.0.1:9", "/demo")
	if err != nil {
		t.Fatal(err)
	}
	query, err := rules.ParseConditions("page=2\nflag", "query")
	if err != nil {
		t.Fatal(err)
	}
	saved, err := store.Create(rules.Draft{
		Name: "List items", APIName: "list-items", Enabled: true, UpstreamID: demo.ID,
		Method: "GET", PathPattern: "/v1/items", StatusCode: 200, DelayMS: 40,
		Headers: map[string]string{"Content-Type": "application/json", "X-Demo": "1"},
		Body:    `{"ok":true}`,
		Match:   rules.MatchSpec{Query: query},
	})
	if err != nil {
		t.Fatal(err)
	}
	log := history.New(4)
	entry := log.Add(history.Entry{
		Method: "GET", Path: "/v1/items", RawQuery: "page=2", ForwardedPath: "/v1/items",
		Status: 200, Source: history.SourceMock, UpstreamID: demo.ID, UpstreamName: demo.Name,
		ResponseHeaders: http.Header{"Content-Type": {"application/json"}},
		ResponsePlain:   `{"ok":true}`,
	})
	rejected := log.Add(history.Entry{
		Method: "GET", Path: "/missing", Status: 0, Source: history.SourceRejected,
	})

	dashboard, err := New(store, log, ups)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(dashboard)
	defer srv.Close()
	client := dashboardClient()

	blankResp, err := client.Get(srv.URL + "/_admin/rules/new")
	if err != nil {
		t.Fatal(err)
	}
	blank := readAll(t, blankResp)
	csp := blankResp.Header.Get("Content-Security-Policy")
	if !strings.Contains(csp, "script-src 'self'") || strings.Contains(csp, "unsafe-inline") {
		t.Fatalf("rule form csp = %q", csp)
	}
	for _, id := range []string{"rule-name", "rule-upstream", "rule-query", "rule-header-match", "rule-body-match", "rule-timing", "rule-headers"} {
		if detailsOpen(blank, id) {
			t.Fatalf("blank form opened %s\n%s", id, blank)
		}
	}
	for _, id := range []string{"method", "path_pattern", "status_code", "body", "raw_response"} {
		if !strings.Contains(blank, `id="`+id+`"`) {
			t.Fatalf("blank form hid %s", id)
		}
	}

	edit := get(t, client, srv.URL+"/_admin/rules/"+saved.ID+"/edit")
	for _, id := range []string{"rule-name", "rule-upstream", "rule-query", "rule-headers", "rule-timing"} {
		if !detailsOpen(edit, id) {
			t.Fatalf("edit form left %s closed\n%s", id, edit)
		}
	}
	if detailsOpen(edit, "rule-body-match") || detailsOpen(edit, "rule-header-match") {
		t.Fatalf("empty condition groups opened:\n%s", edit)
	}
	assertContains(t, edit, `>Name <span class="badge badge-neutral">2</span>`)
	assertContains(t, edit, `>Upstream <span class="badge badge-neutral">set</span>`)
	assertContains(t, edit, `>Query conditions <span class="badge badge-neutral">2</span>`)
	assertContains(t, edit, `>Response headers <span class="badge badge-neutral">2</span>`)

	fromHistory := get(t, client, srv.URL+"/_admin/rules/new?from="+entry.ID)
	if !detailsOpen(fromHistory, "rule-name") || !detailsOpen(fromHistory, "rule-upstream") || !detailsOpen(fromHistory, "rule-query") || !detailsOpen(fromHistory, "rule-headers") {
		t.Fatalf("history prefill left a filled group closed:\n%s", fromHistory)
	}
	if detailsOpen(fromHistory, "rule-timing") || detailsOpen(fromHistory, "rule-body-match") {
		t.Fatalf("history prefill opened an empty group:\n%s", fromHistory)
	}
	assertContains(t, fromHistory, `class="mode-raw"`)

	requestOnly := get(t, client, srv.URL+"/_admin/rules/new?from="+rejected.ID)
	assertContains(t, requestOnly, `class="mode-fields"`)
	if detailsOpen(requestOnly, "rule-query") || detailsOpen(requestOnly, "rule-headers") || detailsOpen(requestOnly, "rule-timing") {
		t.Fatalf("rejected row opened a response group:\n%s", requestOnly)
	}

	badSequence, err := client.PostForm(srv.URL+"/_admin/rules", url.Values{
		"method": {"GET"}, "path_pattern": {"/v1/new"}, "status_code": {"200"},
		"response_mode": {"fields"}, "sequence": {"nope"}, "enabled": {"on"}, "delay_ms": {"0"},
	})
	if err != nil {
		t.Fatal(err)
	}
	sequencePage := readAll(t, badSequence)
	if badSequence.StatusCode != http.StatusBadRequest {
		t.Fatalf("sequence status = %d\n%s", badSequence.StatusCode, sequencePage)
	}
	assertContains(t, sequencePage, "sequence must be stick or loop")
	if !detailsOpen(sequencePage, "rule-timing") {
		t.Fatalf("sequence error left timing closed:\n%s", sequencePage)
	}
	if strings.Contains(sequencePage, `>Timing and limits <span class="badge`) {
		t.Fatalf("invalid sequence counted as a set value:\n%s", sequencePage)
	}
	if detailsOpen(sequencePage, "rule-query") {
		t.Fatal("sequence error opened query conditions")
	}

	badQuery, err := client.PostForm(srv.URL+"/_admin/rules", url.Values{
		"method": {"GET"}, "path_pattern": {"/v1/new"}, "status_code": {"200"},
		"response_mode": {"fields"}, "query_conditions": {"a=1\na"},
	})
	if err != nil {
		t.Fatal(err)
	}
	queryPage := readAll(t, badQuery)
	assertContains(t, queryPage, "query condition")
	if !detailsOpen(queryPage, "rule-query") {
		t.Fatalf("query error left that group closed:\n%s", queryPage)
	}
	if detailsOpen(queryPage, "rule-timing") {
		t.Fatal("query error opened timing")
	}
}

func detailsOpen(page, id string) bool {
	needle := `id="` + id + `"`
	at := strings.Index(page, needle)
	if at < 0 {
		return false
	}
	start := strings.LastIndex(page[:at], "<details")
	if start < 0 {
		return false
	}
	end := strings.Index(page[at:], ">")
	if end < 0 {
		return false
	}
	tag := page[start : at+end]
	return strings.Contains(tag, " open")
}
