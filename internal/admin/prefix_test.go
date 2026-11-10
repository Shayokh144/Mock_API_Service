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

func TestPrefixFormTableAndHistoryDetail(t *testing.T) {
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
	upstreamSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "real")
	}))
	defer upstreamSrv.Close()
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

	bad, err := client.PostForm(proxySrv.URL+"/_admin/upstreams", url.Values{
		"name": {"okya"}, "base_url": {upstreamSrv.URL}, "prefix": {"/okya/"},
	})
	if err != nil {
		t.Fatal(err)
	}
	badBody := readAll(t, bad)
	if bad.StatusCode != http.StatusBadRequest || !strings.Contains(badBody, "one path segment") {
		t.Fatalf("bad prefix status=%d body=%s", bad.StatusCode, badBody)
	}

	created, err := client.PostForm(proxySrv.URL+"/_admin/upstreams", url.Values{
		"name": {"OKYA"}, "base_url": {upstreamSrv.URL}, "prefix": {"/okya"},
	})
	if err != nil {
		t.Fatal(err)
	}
	created.Body.Close()
	if created.StatusCode != http.StatusSeeOther {
		t.Fatalf("create status = %d", created.StatusCode)
	}
	dup, err := client.PostForm(proxySrv.URL+"/_admin/upstreams", url.Values{
		"name": {"other"}, "base_url": {upstreamSrv.URL}, "prefix": {"/okya"},
	})
	if err != nil {
		t.Fatal(err)
	}
	dupBody := readAll(t, dup)
	if dup.StatusCode != http.StatusBadRequest || !strings.Contains(dupBody, "/okya") {
		t.Fatalf("duplicate status=%d body=%s", dup.StatusCode, dupBody)
	}
	plain, err := client.PostForm(proxySrv.URL+"/_admin/upstreams", url.Values{
		"name": {"plain"}, "base_url": {upstreamSrv.URL},
	})
	if err != nil {
		t.Fatal(err)
	}
	plain.Body.Close()
	if plain.StatusCode != http.StatusSeeOther {
		t.Fatalf("plain status = %d", plain.StatusCode)
	}

	port := proxySrv.Listener.Addr().String()
	if i := strings.LastIndex(port, ":"); i >= 0 {
		port = port[i+1:]
	}
	page := get(t, client, proxySrv.URL+"/_admin/upstreams")
	for _, want := range []string{
		"fallback for requests that match no prefix",
		"<th scope=\"col\">Prefix</th>",
		"/okya",
		"http://127.0.0.1:" + port + "/okya",
		"http://10.0.2.2:" + port + "/okya",
		"Use",
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("missing %q in %s", want, page)
		}
	}
	item := ups.List()[0]
	form := get(t, client, proxySrv.URL+"/_admin/upstreams/"+item.ID+"/edit")
	if !strings.Contains(form, `value="/okya"`) {
		t.Fatalf("edit form = %s", form)
	}

	resp, err := http.Get(proxySrv.URL + "/okya/api/v1/x?page=2")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	entries := log.List()
	if len(entries) == 0 {
		t.Fatal("no history")
	}
	detail := get(t, client, proxySrv.URL+"/_admin/history/"+entries[0].ID)
	if !strings.Contains(detail, "/okya/api/v1/x?page=2") || !strings.Contains(detail, "/api/v1/x?page=2") || !strings.Contains(detail, "Forwarded path") || !strings.Contains(detail, "OKYA") {
		t.Fatalf("detail = %s", detail)
	}
	filtered := get(t, client, proxySrv.URL+"/_admin/history?upstream="+item.ID)
	if !strings.Contains(filtered, "/okya/api/v1/x") {
		t.Fatalf("filtered history = %s", filtered)
	}
}
