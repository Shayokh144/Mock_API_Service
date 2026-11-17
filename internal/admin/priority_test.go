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
)

func TestPriorityFormColumnAndHistoryName(t *testing.T) {
	dir := t.TempDir()
	store, err := rules.Load(filepath.Join(dir, "rules.json"))
	if err != nil {
		t.Fatal(err)
	}
	log := history.New(5)
	dashboard, err := New(store, log, nil)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(dashboard)
	defer srv.Close()
	client := dashboardClient()

	form := get(t, client, srv.URL+"/_admin/rules/new")
	if !strings.Contains(form, `name="priority"`) || !strings.Contains(form, "Higher numbers win") {
		t.Fatalf("form missing priority\n%s", form)
	}
	values := url.Values{
		"name": {"Preferred"}, "enabled": {"on"}, "method": {"GET"},
		"path_pattern": {"/v1/items/*"}, "status_code": {"201"}, "priority": {"-2"},
		"body": {`{"ok":true}`},
	}
	resp, err := client.PostForm(srv.URL+"/_admin/rules", values)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("create = %d", resp.StatusCode)
	}
	list := store.List()
	if len(list) != 1 || list[0].Priority != -2 {
		t.Fatalf("saved = %+v", list)
	}
	page := get(t, client, srv.URL+"/_admin/rules")
	if !strings.Contains(page, "<td>-2</td>") {
		t.Fatalf("list missing priority\n%s", page)
	}

	entry := log.Add(history.Entry{
		Method: http.MethodGet, Path: "/v1/items/1", Status: 201,
		Source: history.SourceMock, RuleID: list[0].ID, RuleName: "Preferred",
	})
	historyPage := get(t, client, srv.URL+"/_admin/history")
	if !strings.Contains(historyPage, "Preferred ("+list[0].ID+")") {
		t.Fatalf("history list missing rule\n%s", historyPage)
	}
	detail := get(t, client, srv.URL+"/_admin/history/"+entry.ID)
	if !strings.Contains(detail, "Preferred") || !strings.Contains(detail, list[0].ID) {
		t.Fatalf("detail missing name or id\n%s", detail)
	}
}
