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

	formResp, err := client.Get(srv.URL + "/_admin/rules/new")
	if err != nil {
		t.Fatal(err)
	}
	form := readAll(t, formResp)
	if !strings.Contains(formResp.Header.Get("Content-Security-Policy"), "script-src 'unsafe-inline'") {
		t.Fatalf("csp = %q", formResp.Header.Get("Content-Security-Policy"))
	}
	pathAt := strings.Index(form, `id="path_pattern"`)
	delayAt := strings.Index(form, `id="delay_ms"`)
	modeAt := strings.Index(form, `id="mode_fields"`)
	rawAt := strings.Index(form, `id="raw_response"`)
	statusAt := strings.Index(form, `id="status_code"`)
	if pathAt < 0 || delayAt < pathAt || modeAt < delayAt || rawAt < modeAt || statusAt < modeAt {
		t.Fatalf("field order path=%d delay=%d mode=%d raw=%d status=%d", pathAt, delayAt, modeAt, rawAt, statusAt)
	}
	for _, want := range []string{
		`id="rule-form" class="mode-fields"`,
		`class="raw-only"`,
		`class="fields-only"`,
		`placeholder="HTTP/1.1 401 Unauthorized`,
		`Content-Type: application/json`,
		`{&#34;error&#34;:&#34;unauthorized&#34;}"`,
		`addEventListener("change", apply)`,
		`id="status_code" name="status_code" type="number" min="100" max="599" required`,
	} {
		if !strings.Contains(form, want) {
			t.Fatalf("new form missing %q\n%s", want, form)
		}
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
	if !strings.Contains(edit, "401 Unauthorized") || !strings.Contains(edit, `class="mode-raw"`) || strings.Contains(edit, `id="status_code" name="status_code" type="number" min="100" max="599" required`) {
		t.Fatalf("edit = %s", edit)
	}

	ignored, err := client.PostForm(srv.URL+"/_admin/rules", url.Values{
		"name": {"Ignored fields"}, "enabled": {"on"}, "method": {"POST"}, "path_pattern": {"/v1/raw-only"},
		"response_mode": {"raw"}, "raw_response": {raw},
		"status_code": {""}, "headers": {"not a header"}, "body": {"{"},
	})
	if err != nil {
		t.Fatal(err)
	}
	ignored.Body.Close()
	if ignored.StatusCode != http.StatusSeeOther {
		body := get(t, client, srv.URL+"/_admin/rules")
		t.Fatalf("raw mode rejected unused fields: %d %s", ignored.StatusCode, body)
	}

	for _, tc := range []struct{ pattern, substr string }{
		{"oauth/token", "start with /"},
		{"/oauth/token?refresh_token", "Use /oauth/token"},
		{"/oauth/token#section", "include a fragment"},
	} {
		resp, err := client.PostForm(srv.URL+"/_admin/rules", url.Values{
			"name": {"Path"}, "method": {"GET"}, "path_pattern": {tc.pattern},
			"status_code": {"200"}, "response_mode": {"fields"},
		})
		if err != nil {
			t.Fatal(err)
		}
		body := readAll(t, resp)
		if resp.StatusCode != http.StatusBadRequest || !strings.Contains(body, tc.substr) || !strings.Contains(body, `role="alert"`) {
			t.Fatalf("pattern %q status=%d body=%s", tc.pattern, resp.StatusCode, body)
		}
	}
}
