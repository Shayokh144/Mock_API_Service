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

func TestConditionFormListAndValidation(t *testing.T) {
	dir := t.TempDir()
	store, err := rules.Load(filepath.Join(dir, "rules.json"))
	if err != nil {
		t.Fatal(err)
	}
	dashboard, err := New(store, history.New(10), nil)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(dashboard)
	defer srv.Close()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	form := get(t, client, srv.URL+"/_admin/rules/new")
	pathAt := strings.Index(form, `id="path_pattern"`)
	queryAt := strings.Index(form, `id="query_conditions"`)
	bodyAt := strings.Index(form, `id="body_conditions"`)
	delayAt := strings.Index(form, `id="delay_ms"`)
	if pathAt < 0 || queryAt < pathAt || bodyAt < queryAt || delayAt < bodyAt {
		t.Fatalf("order path=%d query=%d body=%d delay=%d", pathAt, queryAt, bodyAt, delayAt)
	}

	bad, err := client.PostForm(srv.URL+"/_admin/rules", url.Values{
		"name": {"Bad"}, "method": {"POST"}, "path_pattern": {"/oauth/token"},
		"status_code": {"401"}, "body_conditions": {"user..role=admin"},
	})
	if err != nil {
		t.Fatal(err)
	}
	badBody := readAll(t, bad)
	if bad.StatusCode != http.StatusBadRequest || !strings.Contains(badBody, "empty path segment") {
		t.Fatalf("status %d body %s", bad.StatusCode, badBody)
	}

	raw := "HTTP/1.1 401 Unauthorized\nContent-Type: application/json\n\n{\"error\":\"unauthorized\"}\n"
	created, err := client.PostForm(srv.URL+"/_admin/rules", url.Values{
		"name": {"Refresh"}, "enabled": {"on"}, "method": {"POST"}, "path_pattern": {"/oauth/token"},
		"query_conditions": {"refresh_token"}, "body_conditions": {"grant_type=refresh_token"},
		"response_mode": {"raw"}, "raw_response": {raw},
	})
	if err != nil {
		t.Fatal(err)
	}
	created.Body.Close()
	if created.StatusCode != http.StatusSeeOther {
		t.Fatalf("create status %d", created.StatusCode)
	}
	other, err := client.PostForm(srv.URL+"/_admin/rules", url.Values{
		"name": {"Login"}, "enabled": {"on"}, "method": {"POST"}, "path_pattern": {"/oauth/token"},
		"body_conditions": {"grant_type=authorization_code"},
		"status_code":     {"200"}, "response_mode": {"fields"},
	})
	if err != nil {
		t.Fatal(err)
	}
	other.Body.Close()
	if other.StatusCode != http.StatusSeeOther {
		t.Fatalf("second create status %d", other.StatusCode)
	}
	dup, err := client.PostForm(srv.URL+"/_admin/rules", url.Values{
		"name": {"Again"}, "method": {"POST"}, "path_pattern": {"/oauth/token"},
		"body_conditions": {"grant_type=authorization_code"},
		"status_code":     {"200"},
	})
	if err != nil {
		t.Fatal(err)
	}
	dupBody := readAll(t, dup)
	if dup.StatusCode != http.StatusBadRequest || !strings.Contains(dupBody, "already exists") {
		t.Fatalf("dup status %d body %s", dup.StatusCode, dupBody)
	}

	page := get(t, client, srv.URL+"/_admin")
	if !strings.Contains(page, "<th scope=\"col\">Conditions</th>") || !strings.Contains(page, "body grant_type=refresh_token") || !strings.Contains(page, "query refresh_token") {
		t.Fatalf("list = %s", page)
	}
	saved := store.List()
	edit := get(t, client, srv.URL+"/_admin/rules/"+saved[0].ID+"/edit")
	if !strings.Contains(edit, "grant_type=refresh_token") || !strings.Contains(edit, "refresh_token") {
		t.Fatalf("edit = %s", edit)
	}
}
