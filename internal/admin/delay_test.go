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
)

func TestDelayFormListAndHistory(t *testing.T) {
	dir := t.TempDir()
	store, err := rules.Load(filepath.Join(dir, "rules.json"))
	if err != nil {
		t.Fatal(err)
	}
	log := history.New(10)
	dashboard, err := New(store, log, nil)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(dashboard)
	defer srv.Close()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	form := get(t, client, srv.URL+"/_admin/rules/new")
	for _, want := range []string{
		"Delay (ms)",
		"loading states",
		`name="delay_ms"`,
		`value="0"`,
	} {
		if !strings.Contains(form, want) {
			t.Fatalf("new form missing %q\n%s", want, form)
		}
	}

	for _, raw := range []string{"-1", "60001", "abc", "1.5"} {
		resp, err := client.PostForm(srv.URL+"/_admin/rules", url.Values{
			"name": {"Slow"}, "enabled": {"on"}, "method": {"GET"}, "path_pattern": {"/v1/slow"},
			"status_code": {"200"}, "delay_ms": {raw}, "body": {`{"ok":true}`},
		})
		if err != nil {
			t.Fatal(err)
		}
		body := readAll(t, resp)
		if resp.StatusCode != http.StatusBadRequest || !strings.Contains(body, "delay_ms must be from 0 through 60000") {
			t.Fatalf("delay %q status=%d body=%s", raw, resp.StatusCode, body)
		}
	}

	created, err := client.PostForm(srv.URL+"/_admin/rules", url.Values{
		"name": {"Slow"}, "enabled": {"on"}, "method": {"GET"}, "path_pattern": {"/v1/slow"},
		"status_code": {"201"}, "delay_ms": {"1500"}, "body": {`{"ok":true}`},
	})
	if err != nil {
		t.Fatal(err)
	}
	created.Body.Close()
	if created.StatusCode != http.StatusSeeOther {
		t.Fatalf("create status = %d", created.StatusCode)
	}
	zero, err := client.PostForm(srv.URL+"/_admin/rules", url.Values{
		"name": {"Fast"}, "enabled": {"on"}, "method": {"GET"}, "path_pattern": {"/v1/fast"},
		"status_code": {"200"}, "delay_ms": {""}, "body": {`{"ok":true}`},
	})
	if err != nil {
		t.Fatal(err)
	}
	zero.Body.Close()

	page := get(t, client, srv.URL+"/_admin")
	if !strings.Contains(page, "<th scope=\"col\">Delay</th>") || !strings.Contains(page, "<td>1500 ms</td>") || strings.Contains(page, "<td>0 ms</td>") {
		t.Fatalf("rules page = %s", page)
	}
	var slow rules.Rule
	for _, rule := range store.List() {
		if rule.PathPattern == "/v1/slow" {
			slow = rule
		}
	}
	if slow.DelayMS != 1500 {
		t.Fatalf("stored = %+v", slow)
	}
	edit := get(t, client, srv.URL+"/_admin/rules/"+slow.ID+"/edit")
	if !strings.Contains(edit, `value="1500"`) {
		t.Fatalf("edit form = %s", edit)
	}

	entry := log.Add(history.Entry{
		StartedAt: time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC),
		Method:    "GET",
		Path:      "/v1/slow",
		Status:    201,
		Source:    history.SourceMock,
		RuleID:    slow.ID,
		DelayMS:   1500,
	})
	list := get(t, client, srv.URL+"/_admin/history")
	if !strings.Contains(list, "<th scope=\"col\">Delay</th>") || !strings.Contains(list, "1500 ms") {
		t.Fatalf("history list = %s", list)
	}
	detail := get(t, client, srv.URL+"/_admin/history/"+entry.ID)
	if !strings.Contains(detail, "<dt>Delay</dt><dd>1500 ms</dd>") {
		t.Fatalf("detail = %s", detail)
	}
	plain := log.Add(history.Entry{
		StartedAt: time.Date(2026, 11, 5, 12, 0, 1, 0, time.UTC),
		Method:    "GET",
		Path:      "/v1/fast",
		Status:    200,
		Source:    history.SourceUpstream,
	})
	plainDetail := get(t, client, srv.URL+"/_admin/history/"+plain.ID)
	if !strings.Contains(plainDetail, "<dt>Delay</dt><dd>0 ms</dd>") {
		t.Fatalf("plain detail = %s", plainDetail)
	}
}
