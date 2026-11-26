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
	shop, err := ups.Create("SHOP_AUTH_DEV", "http://127.0.0.1:9", "/shop-auth")
	if err != nil {
		t.Fatal(err)
	}
	other, err := ups.Create("develop", "http://127.0.0.1:9", "/develop")
	if err != nil {
		t.Fatal(err)
	}
	log := history.New(10)
	start := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	log.Add(history.Entry{StartedAt: start, Method: "GET", Path: "/API/User/Me", Status: 200, Source: history.SourceMock, UpstreamID: shop.ID, UpstreamName: shop.Name})
	log.Add(history.Entry{StartedAt: start.Add(time.Second), Method: "GET", Path: "/oauth/token", Status: 200, Source: history.SourceUpstream, UpstreamID: shop.ID, UpstreamName: shop.Name})
	log.Add(history.Entry{StartedAt: start.Add(2 * time.Second), Method: "GET", Path: "/v1/items", RawQuery: "name=user/me", Status: 200, Source: history.SourceMock, UpstreamID: other.ID, UpstreamName: other.Name})
	log.Add(history.Entry{StartedAt: start.Add(3 * time.Second), Method: "GET", Path: "/api/users", Status: 200, Source: history.SourceMock, UpstreamID: other.ID, UpstreamName: other.Name})

	dashboard, err := New(store, log, ups)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(dashboard)
	defer srv.Close()
	client := dashboardClient()

	page := get(t, client, srv.URL+"/_admin/history?q=user/me")
	assertContains(t, page, "Filter: All. Text: user/me. Showing 2 of 4 requests in 2 groups.")
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
	assertContains(t, combined, "Filter: Mocked. Text: USER/ME. Showing 1 of 4 requests in 1 group.")
	assertContains(t, combined, "/v1/items?name=user/me")
	if strings.Contains(combined, "/API/User/Me") || strings.Contains(combined, "/api/users") {
		t.Fatalf("combined filter showed the wrong rows:\n%s", combined)
	}
	assertContains(t, combined, "q=USER%2FME")
	assertContains(t, combined, "source=mock")
	assertContains(t, combined, "upstream="+other.ID)
	assertContains(t, combined, `href="/_admin/history/clear?q=USER%2FME&amp;source=mock&amp;upstream=`+other.ID+`"`)
	assertContains(t, combined, `data-confirm-post="/_admin/history/clear?q=USER%2FME&amp;source=mock&amp;upstream=`+other.ID+`"`)

	blank := get(t, client, srv.URL+"/_admin/history?q=%20%20")
	assertContains(t, blank, "Filter: All. Showing 4 of 4 requests in 4 groups.")
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
	for _, want := range []string{`kept.set("q"`, `addEventListener("input"`, "history-text-filter", "Text: ", "showModal", "history-clear-dialog"} {
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
	shop, err := ups.Create("SHOP_AUTH_DEV", "http://127.0.0.1:9", "/shop-auth")
	if err != nil {
		t.Fatal(err)
	}
	other, err := ups.Create("develop", "http://127.0.0.1:9", "/develop")
	if err != nil {
		t.Fatal(err)
	}
	for _, draft := range []rules.Draft{
		{Name: "Refresh", Enabled: true, Method: "POST", PathPattern: "/oauth/token", StatusCode: 401, UpstreamID: shop.ID},
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
	client := dashboardClient()

	named := get(t, client, srv.URL+"/_admin?upstream="+url.QueryEscape("SHOP_AUTH_DEV"))
	assertContains(t, named, "Upstream: SHOP_AUTH_DEV. Showing 2 of 3.")
	assertContains(t, named, "Refresh")
	assertContains(t, named, "Shared")
	assertContains(t, named, `method="get"`)
	assertContains(t, named, `value="SHOP_AUTH_DEV" selected`)
	assertContains(t, named, "includes rules set to Any")
	assertContains(t, named, `<details class="help">`)
	assertContains(t, named, `<summary>How this works</summary>`)
	if strings.Contains(named, `<details class="help" open`) {
		t.Fatal("rules help starts open")
	}
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

	anyCase := get(t, client, srv.URL+"/_admin/rules?upstream=ANY")
	assertContains(t, anyCase, "Upstream: Any. Showing 1 of 3.")
	assertContains(t, anyCase, "Shared")
	assertContains(t, anyCase, `value="any" selected`)
	if strings.Contains(anyCase, "Refresh") || strings.Contains(anyCase, "Develop only") {
		t.Fatalf("ANY filter showed a named rule:\n%s", anyCase)
	}

	unknown := get(t, client, srv.URL+"/_admin?upstream=SHOPP")
	assertContains(t, unknown, "Upstream: Unknown upstream SHOPP. Showing 0 of 3.")
	assertContains(t, unknown, "Unknown upstream SHOPP")
	assertContains(t, unknown, "No rules for this upstream.")
	if strings.Contains(unknown, "Refresh") || strings.Contains(unknown, "Develop only") || strings.Contains(unknown, "Shared") {
		t.Fatalf("unknown upstream filter showed rules:\n%s", unknown)
	}
	if strings.Contains(unknown, `value="" selected`) || strings.Contains(unknown, `value="any" selected`) {
		t.Fatal("unknown upstream filter selected a real choice")
	}

	all := get(t, client, srv.URL+"/_admin")
	shopAt := strings.Index(all, "<h2>SHOP_AUTH_DEV · /shop-auth</h2>")
	developAt := strings.Index(all, "<h2>develop · /develop</h2>")
	anyAt := strings.Index(all, "<h2>Any upstream</h2>")
	if shopAt < 0 || developAt < shopAt || anyAt < developAt {
		t.Fatalf("blocks are not in upstream order:\n%s", all)
	}
	if strings.Contains(all, `scope="col">Upstream<`) {
		t.Fatal("grouped tables still have an Upstream column")
	}

	assertContains(t, named, "<h2>SHOP_AUTH_DEV · /shop-auth</h2>")
	assertContains(t, named, "<h2>Any upstream</h2>")
	assertContains(t, named, "1 rule")
	if strings.Contains(named, "<h2>develop · /develop</h2>") {
		t.Fatal("empty develop block was shown")
	}
}

func TestRuleGroupsKeepOrderAndFilter(t *testing.T) {
	dir := t.TempDir()
	store, err := rules.Load(filepath.Join(dir, "rules.json"))
	if err != nil {
		t.Fatal(err)
	}
	ups, err := upstreams.Load(filepath.Join(dir, "upstreams.json"))
	if err != nil {
		t.Fatal(err)
	}
	shop, err := ups.Create("SHOP_AUTH_DEV", "http://127.0.0.1:9", "/shop-auth")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ups.Create("unused", "http://127.0.0.1:9", "/unused"); err != nil {
		t.Fatal(err)
	}
	gone := "0123456789abcdef"
	var first, second rules.Rule
	for _, draft := range []rules.Draft{
		{Name: "First", Enabled: true, Method: "GET", PathPattern: "/first", StatusCode: 200, UpstreamID: shop.ID},
		{Name: "Second", Enabled: true, Method: "GET", PathPattern: "/second", StatusCode: 200, UpstreamID: shop.ID},
		{Name: "Shared", Enabled: true, Method: "GET", PathPattern: "/shared", StatusCode: 200},
		{Name: "Orphan", Enabled: true, Method: "GET", PathPattern: "/orphan", StatusCode: 200, UpstreamID: gone},
	} {
		saved, err := store.Create(draft)
		if err != nil {
			t.Fatal(err)
		}
		switch saved.Name {
		case "First":
			first = saved
		case "Second":
			second = saved
		}
	}
	dashboard, err := New(store, history.New(10), ups)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(dashboard)
	defer srv.Close()
	client := dashboardClient()

	page := get(t, client, srv.URL+"/_admin")
	shopAt := strings.Index(page, "<h2>SHOP_AUTH_DEV · /shop-auth</h2>")
	unknownAt := strings.Index(page, "<h2>Unknown upstream: "+gone+"</h2>")
	anyAt := strings.Index(page, "<h2>Any upstream</h2>")
	if shopAt < 0 || unknownAt < shopAt || anyAt < unknownAt {
		t.Fatalf("block order:\n%s", page)
	}
	if strings.Index(page, "First") > strings.Index(page, "Second") {
		t.Fatalf("rule order inside the block changed:\n%s", page)
	}
	assertContains(t, page, "2 rules")
	if strings.Contains(page, "<h2>unused") {
		t.Fatal("empty upstream block was shown")
	}

	filter := "?upstream=" + url.QueryEscape("SHOP_AUTH_DEV")
	filtered := get(t, client, srv.URL+"/_admin"+filter)
	assertContains(t, filtered, "Showing 3 of 4.")
	assertContains(t, filtered, "First")
	assertContains(t, filtered, "Second")
	assertContains(t, filtered, "Shared")
	if strings.Contains(filtered, "Orphan") {
		t.Fatal("named filter included an unknown upstream")
	}
	assertContains(t, filtered, `/_admin/rules/`+first.ID+`/edit`+filter)
	assertContains(t, filtered, `/_admin/rules/`+first.ID+`/toggle`+filter)
	assertContains(t, filtered, `/_admin/rules/`+second.ID+`/delete`+filter)

	toggled, err := client.PostForm(srv.URL+"/_admin/rules/"+first.ID+"/toggle"+filter, nil)
	if err != nil {
		t.Fatal(err)
	}
	toggled.Body.Close()
	if toggled.StatusCode != http.StatusSeeOther || toggled.Header.Get("Location") != "/_admin"+filter {
		t.Fatalf("toggle location = %d %q", toggled.StatusCode, toggled.Header.Get("Location"))
	}

	edit := get(t, client, srv.URL+"/_admin/rules/"+second.ID+"/edit"+filter)
	assertContains(t, edit, `action="/_admin/rules/`+second.ID+filter+`"`)
	saved, err := client.PostForm(srv.URL+"/_admin/rules/"+second.ID+filter, url.Values{
		"name":         {"Second"},
		"enabled":      {"on"},
		"method":       {"GET"},
		"path_pattern": {"/second"},
		"status_code":  {"201"},
		"upstream_id":  {shop.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	saved.Body.Close()
	if saved.StatusCode != http.StatusSeeOther || saved.Header.Get("Location") != "/_admin"+filter {
		t.Fatalf("save location = %d %q", saved.StatusCode, saved.Header.Get("Location"))
	}

	deleted, err := client.PostForm(srv.URL+"/_admin/rules/"+second.ID+"/delete"+filter, nil)
	if err != nil {
		t.Fatal(err)
	}
	deleted.Body.Close()
	if deleted.Header.Get("Location") != "/_admin"+filter {
		t.Fatalf("delete location = %q", deleted.Header.Get("Location"))
	}
	after := get(t, client, srv.URL+"/_admin"+filter)
	assertContains(t, after, "Upstream: SHOP_AUTH_DEV. Showing 2 of 3.")
	if strings.Contains(after, "Second") {
		t.Fatal("deleted rule stayed on the filtered page")
	}
}
