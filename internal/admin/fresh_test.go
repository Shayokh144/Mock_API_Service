package admin

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Shayokh144/Mock_API_Service/internal/rules"
)

func TestFreshRequestDashboardPersists(t *testing.T) {
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
	srv := httptest.NewServer(dashboard)
	defer srv.Close()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	created, err := client.PostForm(srv.URL+"/_admin/rules", url.Values{
		"name":          {"Fresh"},
		"enabled":       {"on"},
		"fresh_request": {"on"},
		"method":        {"POST"},
		"path_pattern":  {"/v1/items"},
		"status_code":   {"500"},
		"body":          {`{"error":"unused"}`},
	})
	if err != nil {
		t.Fatal(err)
	}
	created.Body.Close()
	if created.StatusCode != http.StatusSeeOther {
		t.Fatalf("create status = %d", created.StatusCode)
	}
	page := get(t, client, srv.URL+"/_admin")
	if !strings.Contains(page, ">fresh<") {
		t.Fatalf("list missing fresh state: %s", page)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"fresh_request": true`) {
		t.Fatalf("file = %s", raw)
	}
	saved := store.List()[0]
	edit := get(t, client, srv.URL+"/_admin/rules/"+saved.ID+"/edit")
	if !strings.Contains(edit, `name="fresh_request"`) || !strings.Contains(edit, "checked") {
		t.Fatalf("edit form lost fresh request: %s", edit)
	}

	toggled, err := client.PostForm(srv.URL+"/_admin/rules/"+saved.ID+"/toggle", nil)
	if err != nil {
		t.Fatal(err)
	}
	toggled.Body.Close()
	afterToggle, ok := store.Get(saved.ID)
	if !ok || afterToggle.Enabled || !afterToggle.FreshRequest {
		t.Fatalf("toggle dropped fresh request: %+v", afterToggle)
	}

	cleared, err := client.PostForm(srv.URL+"/_admin/rules/"+saved.ID, url.Values{
		"name":         {"Fresh"},
		"enabled":      {"on"},
		"method":       {"POST"},
		"path_pattern": {"/v1/items"},
		"status_code":  {"500"},
		"body":         {`{"error":"unused"}`},
	})
	if err != nil {
		t.Fatal(err)
	}
	cleared.Body.Close()
	normal, ok := store.Get(saved.ID)
	if !ok || normal.FreshRequest || !normal.Enabled {
		t.Fatalf("cleared = %+v", normal)
	}
	if !strings.Contains(get(t, client, srv.URL+"/_admin"), ">mock<") {
		t.Fatal("list did not return to mock")
	}
}
