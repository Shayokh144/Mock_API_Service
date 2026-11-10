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

func TestRawRuleFormAndList(t *testing.T) {
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
	if !strings.Contains(form, "Raw response") || !strings.Contains(form, `value="fields"`) || !strings.Contains(form, "checked") {
		t.Fatalf("new form = %s", form)
	}

	raw := "HTTP/1.1 401 Unauthorized\nContent-Type: application/json; charset=utf-8\nConnection: close\n\n{\n  \"error\": \"unauthorized\",\n  \"message\": \"The access token has expired or is invalid.\"\n}\n"
	badCases := []struct {
		raw    string
		substr string
	}{
		{"", "status line"},
		{"nope\n\n", "status line"},
		{"HTTP/1.1 99 Nope\n\n", "status code"},
		{"HTTP/1.1 200 OK\nNot a header\n\n", "Name: value"},
	}
	for _, tc := range badCases {
		resp, err := client.PostForm(srv.URL+"/_admin/rules", url.Values{
			"name": {"Expired"}, "enabled": {"on"}, "method": {"GET"}, "path_pattern": {"/v1/auth"},
			"status_code": {"200"}, "response_mode": {"raw"}, "raw_response": {tc.raw},
		})
		if err != nil {
			t.Fatal(err)
		}
		body := readAll(t, resp)
		if resp.StatusCode != http.StatusBadRequest || !strings.Contains(body, tc.substr) || !strings.Contains(body, `role="alert"`) {
			t.Fatalf("raw %q status=%d body=%s", tc.raw, resp.StatusCode, body)
		}
	}

	created, err := client.PostForm(srv.URL+"/_admin/rules", url.Values{
		"name": {"Expired"}, "enabled": {"on"}, "method": {"GET"}, "path_pattern": {"/v1/auth"},
		"status_code": {"200"}, "delay_ms": {"1500"}, "response_mode": {"raw"}, "raw_response": {raw},
	})
	if err != nil {
		t.Fatal(err)
	}
	created.Body.Close()
	if created.StatusCode != http.StatusSeeOther {
		t.Fatalf("create status = %d", created.StatusCode)
	}
	page := get(t, client, srv.URL+"/_admin")
	if !strings.Contains(page, "<th scope=\"col\">Mode</th>") || !strings.Contains(page, "<td>Raw</td>") || !strings.Contains(page, "<td>401</td>") {
		t.Fatalf("list = %s", page)
	}
	saved := store.List()[0]
	if saved.ResponseMode != rules.ModeRaw || saved.StatusCode != 401 || saved.DelayMS != 1500 || saved.MockHeader {
		t.Fatalf("stored = %+v", saved)
	}
	edit := get(t, client, srv.URL+"/_admin/rules/"+saved.ID+"/edit")
	if !strings.Contains(edit, "401 Unauthorized") || !strings.Contains(edit, `value="raw"`) {
		t.Fatalf("edit = %s", edit)
	}
}
