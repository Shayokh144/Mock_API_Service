package admin

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Shayokh144/Mock_API_Service/internal/history"
	"github.com/Shayokh144/Mock_API_Service/internal/proxy"
	"github.com/Shayokh144/Mock_API_Service/internal/rules"
	"github.com/Shayokh144/Mock_API_Service/internal/upstreams"
)

func TestUpstreamDashboardCRUDSwitchAndHistory(t *testing.T) {
	dir := t.TempDir()
	ruleStore, err := rules.Load(filepath.Join(dir, "rules.json"))
	if err != nil {
		t.Fatal(err)
	}
	ups, err := upstreams.Load(filepath.Join(dir, "upstreams.json"))
	if err != nil {
		t.Fatal(err)
	}
	log := history.New(20)
	dashboard, err := New(ruleStore, log, ups)
	if err != nil {
		t.Fatal(err)
	}
	var gotPath string
	stagingSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = io.WriteString(w, "staging-real")
	}))
	defer stagingSrv.Close()
	developSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "develop-real")
	}))
	defer developSrv.Close()

	proxySrv := httptest.NewServer(proxy.New(proxy.Options{
		Upstreams: ups,
		Timeout:   time.Second,
		Rules:     ruleStore,
		History:   log,
		Admin:     dashboard,
	}))
	defer proxySrv.Close()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	page := get(t, client, proxySrv.URL+"/_admin/upstreams")
	if !strings.Contains(page, "No upstreams yet") {
		t.Fatalf("empty page = %s", page)
	}
	resp, err := client.Get(proxySrv.URL + "/_admin/upstreams")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.Header.Get("Referrer-Policy") != "same-origin" {
		t.Fatalf("referrer = %q", resp.Header.Get("Referrer-Policy"))
	}
	if !strings.Contains(resp.Header.Get("Content-Security-Policy"), "default-src 'none'") || strings.Contains(resp.Header.Get("Content-Security-Policy"), "script-src") {
		t.Fatalf("csp = %q", resp.Header.Get("Content-Security-Policy"))
	}

	evil, err := http.NewRequest(http.MethodPost, proxySrv.URL+"/_admin/upstreams", strings.NewReader(url.Values{
		"name": {"evil"}, "base_url": {"https://evil.example"},
	}.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	evil.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	evil.Header.Set("Origin", "http://evil.example")
	evilResp, err := client.Do(evil)
	if err != nil {
		t.Fatal(err)
	}
	evilResp.Body.Close()
	if evilResp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross origin status = %d", evilResp.StatusCode)
	}

	bad := url.Values{"name": {"staging"}, "base_url": {"ftp://staging.example"}}
	badResp, err := client.PostForm(proxySrv.URL+"/_admin/upstreams", bad)
	if err != nil {
		t.Fatal(err)
	}
	badBody := readAll(t, badResp)
	if badResp.StatusCode != http.StatusBadRequest || !strings.Contains(badBody, "http or https") {
		t.Fatalf("bad url status=%d body=%s", badResp.StatusCode, badBody)
	}

	created, err := client.PostForm(proxySrv.URL+"/_admin/upstreams", url.Values{
		"name": {"staging"}, "base_url": {stagingSrv.URL + "/api"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.StatusCode != http.StatusSeeOther {
		t.Fatalf("create status = %d", created.StatusCode)
	}
	created.Body.Close()
	developResp, err := client.PostForm(proxySrv.URL+"/_admin/upstreams", url.Values{
		"name": {"develop"}, "base_url": {developSrv.URL},
	})
	if err != nil {
		t.Fatal(err)
	}
	developResp.Body.Close()

	list := ups.List()
	if len(list) != 2 || list[0].Name != "staging" {
		t.Fatalf("list = %+v", list)
	}
	staging := list[0]
	develop := list[1]
	shown := get(t, client, proxySrv.URL+"/_admin/upstreams")
	if !strings.Contains(shown, "Active upstream: staging") || !strings.Contains(shown, developSrv.URL) {
		t.Fatalf("list page = %s", shown)
	}

	form := get(t, client, proxySrv.URL+"/_admin/rules/new")
	if !strings.Contains(form, "Any upstream") || !strings.Contains(form, "staging") || !strings.Contains(form, staging.ID) {
		t.Fatalf("rule form = %s", form)
	}
	ruleResp, err := client.PostForm(proxySrv.URL+"/_admin/rules", url.Values{
		"name": {"Profile"}, "enabled": {"on"}, "method": {"GET"}, "path_pattern": {"/v1/profile"},
		"status_code": {"500"}, "body": {`{"mocked":true}`}, "upstream_id": {staging.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	ruleResp.Body.Close()
	if ruleResp.StatusCode != http.StatusSeeOther {
		t.Fatalf("rule status = %d", ruleResp.StatusCode)
	}
	rulesPage := get(t, client, proxySrv.URL+"/_admin")
	if !strings.Contains(rulesPage, ">staging<") {
		t.Fatalf("rules page = %s", rulesPage)
	}

	pass, err := http.Get(proxySrv.URL + "/v1/health")
	if err != nil {
		t.Fatal(err)
	}
	passBody := readAll(t, pass)
	if passBody != "staging-real" || gotPath != "/api/v1/health" {
		t.Fatalf("passthrough body=%s path=%s", passBody, gotPath)
	}
	mock, err := http.Get(proxySrv.URL + "/v1/profile")
	if err != nil {
		t.Fatal(err)
	}
	mockBody := readAll(t, mock)
	if mock.StatusCode != http.StatusInternalServerError || !strings.Contains(mockBody, "mocked") {
		t.Fatalf("mock status=%d body=%s", mock.StatusCode, mockBody)
	}

	use, err := client.PostForm(proxySrv.URL+"/_admin/upstreams/"+develop.ID+"/activate", nil)
	if err != nil {
		t.Fatal(err)
	}
	use.Body.Close()
	if active, _ := ups.Active(); active.ID != develop.ID {
		t.Fatalf("active = %+v", active)
	}
	miss, err := http.Get(proxySrv.URL + "/v1/profile")
	if err != nil {
		t.Fatal(err)
	}
	missBody := readAll(t, miss)
	if strings.Contains(missBody, "mocked") || missBody != "develop-real" {
		t.Fatalf("staging rule matched while develop was active: %d %s", miss.StatusCode, missBody)
	}

	historyPage := get(t, client, proxySrv.URL+"/_admin/history")
	if !strings.Contains(historyPage, ">staging<") {
		t.Fatalf("history = %s", historyPage)
	}
	feed := get(t, client, proxySrv.URL+"/_admin/history/feed")
	if !strings.Contains(feed, `"upstream":"staging"`) {
		t.Fatalf("feed = %s", feed)
	}
	detailID := log.List()[0].ID
	for _, entry := range log.List() {
		if entry.Source == history.SourceMock {
			detailID = entry.ID
		}
	}
	detail := get(t, client, proxySrv.URL+"/_admin/history/"+detailID)
	if !strings.Contains(detail, "staging") {
		t.Fatalf("detail = %s", detail)
	}

	blocked, err := client.PostForm(proxySrv.URL+"/_admin/upstreams/"+develop.ID+"/delete", nil)
	if err != nil {
		t.Fatal(err)
	}
	blockedBody := readAll(t, blocked)
	if blocked.StatusCode != http.StatusBadRequest || !strings.Contains(blockedBody, "switch to another first") {
		t.Fatalf("delete active status=%d body=%s", blocked.StatusCode, blockedBody)
	}
	deleted, err := client.PostForm(proxySrv.URL+"/_admin/upstreams/"+staging.ID+"/delete", nil)
	if err != nil {
		t.Fatal(err)
	}
	deleted.Body.Close()
	if deleted.StatusCode != http.StatusSeeOther {
		t.Fatalf("delete status = %d", deleted.StatusCode)
	}
	if _, ok := ups.Get(staging.ID); ok {
		t.Fatal("staging still stored")
	}
	saved := ruleStore.List()
	if len(saved) != 1 || saved[0].UpstreamID != "" {
		t.Fatalf("rules after delete = %+v", saved)
	}
	after := get(t, client, proxySrv.URL+"/_admin")
	if !strings.Contains(after, ">any<") {
		t.Fatalf("rules after delete page = %s", after)
	}
}
