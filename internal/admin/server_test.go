package admin

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Shayokh144/Mock_API_Service/internal/rules"
)

func TestRulesDashboard(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.json")
	store, err := rules.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	dashboard, err := New(store, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	proxySrv := httptest.NewServer(dashboard)
	defer proxySrv.Close()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	bad := url.Values{}
	bad.Set("name", "Broken")
	bad.Set("method", "GET")
	bad.Set("path_pattern", "/v1/broken")
	bad.Set("status_code", "500")
	bad.Set("body", "{")
	badResp, err := client.PostForm(proxySrv.URL+"/_admin/rules", bad)
	if err != nil {
		t.Fatal(err)
	}
	badBody := readAll(t, badResp)
	if badResp.StatusCode != http.StatusBadRequest || !strings.Contains(badBody, "JSON") || !strings.Contains(badBody, "/v1/broken") {
		t.Fatalf("bad form status=%d body=%s", badResp.StatusCode, badBody)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("invalid form wrote the rules file")
	}

	evil, err := http.NewRequest(http.MethodPost, proxySrv.URL+"/_admin/rules", strings.NewReader(url.Values{
		"name": {"Nope"}, "method": {"GET"}, "path_pattern": {"/v1/nope"}, "status_code": {"200"},
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
		t.Fatalf("origin status = %d", evilResp.StatusCode)
	}

	created, err := client.PostForm(proxySrv.URL+"/_admin/rules", url.Values{
		"name":         {"<script>"},
		"enabled":      {"on"},
		"method":       {"get"},
		"path_pattern": {"/api/users/*"},
		"status_code":  {"500"},
		"headers":      {"X-Test: yes"},
		"body":         {`{"error":"planned"}`},
	})
	if err != nil {
		t.Fatal(err)
	}
	created.Body.Close()
	if created.StatusCode != http.StatusSeeOther || created.Header.Get("Location") != "/_admin" {
		t.Fatalf("create status=%d location=%s", created.StatusCode, created.Header.Get("Location"))
	}
	page := get(t, client, proxySrv.URL+"/_admin")
	if strings.Contains(page, "<script>") || !strings.Contains(page, "&lt;script&gt;") {
		t.Fatalf("name was not escaped: %s", page)
	}
	for _, want := range []string{"wildcard", "GET", "/api/users/*", "enabled", "500"} {
		if !strings.Contains(page, want) {
			t.Fatalf("list missing %s", want)
		}
	}
	saved := store.List()
	if len(saved) != 1 {
		t.Fatalf("saved = %d", len(saved))
	}
	createdAt := saved[0].CreatedAt

	edited, err := client.PostForm(proxySrv.URL+"/_admin/rules/"+saved[0].ID, url.Values{
		"name":         {"Renamed"},
		"enabled":      {"on"},
		"method":       {"GET"},
		"path_pattern": {"/api/users/*"},
		"status_code":  {"500"},
		"headers":      {"X-Test: yes"},
		"body":         {`{"error":"planned"}`},
	})
	if err != nil {
		t.Fatal(err)
	}
	edited.Body.Close()
	if edited.StatusCode != http.StatusSeeOther {
		t.Fatalf("edit status = %d", edited.StatusCode)
	}
	updated, ok := store.Get(saved[0].ID)
	if !ok || updated.Name != "Renamed" || !updated.CreatedAt.Equal(createdAt.Time) {
		t.Fatalf("updated = %+v", updated)
	}

	toggled, err := client.PostForm(proxySrv.URL+"/_admin/rules/"+saved[0].ID+"/toggle", nil)
	if err != nil {
		t.Fatal(err)
	}
	toggled.Body.Close()
	if !strings.Contains(get(t, client, proxySrv.URL+"/_admin"), "disabled") {
		t.Fatal("toggle did not disable the rule")
	}

	missing, err := client.Get(proxySrv.URL + "/_admin/rules/0123456789abcdef/edit")
	if err != nil {
		t.Fatal(err)
	}
	missing.Body.Close()
	if missing.StatusCode != http.StatusNotFound {
		t.Fatalf("missing status = %d", missing.StatusCode)
	}

	deleted, err := client.PostForm(proxySrv.URL+"/_admin/rules/"+saved[0].ID+"/delete", nil)
	if err != nil {
		t.Fatal(err)
	}
	deleted.Body.Close()
	if !strings.Contains(get(t, client, proxySrv.URL+"/_admin"), "No rules yet") {
		t.Fatal("delete left the rule visible")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	reloaded, err := rules.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.List()) != 0 {
		t.Fatal("delete did not persist")
	}
}

func get(t *testing.T, client *http.Client, rawURL string) string {
	t.Helper()
	resp, err := client.Get(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	return readAll(t, resp)
}

func readAll(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
