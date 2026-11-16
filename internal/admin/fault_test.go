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

func TestFaultFormAndList(t *testing.T) {
	dir := t.TempDir()
	store, err := rules.Load(filepath.Join(dir, "rules.json"))
	if err != nil {
		t.Fatal(err)
	}
	dashboard, err := New(store, history.New(5), nil)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(dashboard)
	defer srv.Close()
	client := dashboardClient()
	form := get(t, client, srv.URL+"/_admin/rules/new")
	if !strings.Contains(form, `name="fault"`) || !strings.Contains(form, "reset") || !strings.Contains(form, "hang") {
		t.Fatalf("form missing fault\n%s", form)
	}
	resp, err := client.PostForm(srv.URL+"/_admin/rules", url.Values{
		"name": {"Drop"}, "enabled": {"on"}, "method": {"GET"},
		"path_pattern": {"/v1/drop"}, "status_code": {"200"}, "fault": {"reset"},
		"body": {`{"ok":true}`},
	})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther || len(store.List()) != 1 || store.List()[0].Fault != "reset" {
		t.Fatalf("create = %d rules %+v", resp.StatusCode, store.List())
	}
	page := get(t, client, srv.URL+"/_admin/rules")
	if !strings.Contains(page, "<td>reset</td>") {
		t.Fatalf("list missing fault\n%s", page)
	}
}
