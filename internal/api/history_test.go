package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Shayokh144/Mock_API_Service/internal/history"
	"github.com/Shayokh144/Mock_API_Service/internal/rules"
	"github.com/Shayokh144/Mock_API_Service/internal/sessions"
)

func TestResetAndHistory(t *testing.T) {
	_, srv, manager := testAPI(t, t.TempDir())
	defer manager.Close()
	token := readToken(t, srv.tokens)
	a := postSession(t, srv, token, `{"name":"a"}`)
	b := postSession(t, srv, token, `{"name":"b"}`)
	created := apiJSON(t, srv, http.MethodPut, "/_api/sessions/"+a.ID+"/rules/by-name/profile", token, `{"description":"Profile","method":"GET","path":"/v1/profile","status":200,"body":{"ok":true}}`)
	if created.Code != http.StatusCreated {
		t.Fatal(created.Body)
	}
	rule, ok := mustSession(t, manager, a.ID).Rules.FindByAPIName("profile")
	if !ok {
		t.Fatal("missing rule")
	}
	if _, err := mustSession(t, manager, b.ID).Rules.Create(rules.Draft{Method: "GET", PathPattern: "/v1/keep", StatusCode: 200, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	older := mustSession(t, manager, a.ID).History.Add(history.Entry{
		Method: "POST", Path: "/v1/other", Status: 500, Source: history.SourceUpstream,
		UpstreamName: "OKYA", RequestHeaders: http.Header{"Accept": {"json"}},
	})
	newer := mustSession(t, manager, a.ID).History.Add(history.Entry{
		Method: "GET", Path: "/v1/profile", RawQuery: "page=2", Status: 200,
		Source: history.SourceMock, RuleID: rule.ID, UpstreamName: "OKYA",
		RequestHeaders: http.Header{"Authorization": {"secret-token"}, "Accept": {"json"}},
		RequestBody:    "hello", ResponseBody: "world",
	})
	mustSession(t, manager, b.ID).History.Add(history.Entry{Method: "GET", Path: "/v1/keep", Status: 200, Source: history.SourceMock})

	list := apiJSON(t, srv, http.MethodGet, "/_api/sessions/"+a.ID+"/history?method=GET&path=profile&path_exact=/v1/profile&upstream=OKYA&status=200&mocked=true&rule_name=profile&rule_id="+rule.ID, token, "")
	if list.Code != http.StatusOK || !strings.Contains(list.Body, newer.ID) || strings.Contains(list.Body, older.ID) {
		t.Fatalf("filter = %d %s", list.Code, list.Body)
	}
	if strings.Contains(list.Body, "secret-token") || !strings.Contains(list.Body, "••••••••") {
		t.Fatalf("mask = %s", list.Body)
	}
	shown := apiJSON(t, srv, http.MethodGet, "/_api/sessions/"+a.ID+"/history?reveal=1&rule_id="+rule.ID, token, "")
	if !strings.Contains(shown.Body, "secret-token") {
		t.Fatalf("reveal = %s", shown.Body)
	}
	polled := apiJSON(t, srv, http.MethodGet, "/_api/sessions/"+a.ID+"/history?since="+older.ID, token, "")
	if !strings.Contains(polled.Body, newer.ID) || strings.Contains(polled.Body, older.ID) {
		t.Fatalf("since = %s", polled.Body)
	}

	reset := apiJSON(t, srv, http.MethodPost, "/_api/sessions/"+a.ID+"/reset", token, `{"rules":true,"history":true}`)
	if reset.Code != http.StatusNoContent {
		t.Fatalf("reset = %d %s", reset.Code, reset.Body)
	}
	if rulesLeft := mustSession(t, manager, a.ID).Rules.List(); len(rulesLeft) != 0 {
		t.Fatalf("a rules = %d", len(rulesLeft))
	}
	if hist := mustSession(t, manager, a.ID).History.List(); len(hist) != 0 {
		t.Fatalf("a history = %d", len(hist))
	}
	if kept := mustSession(t, manager, b.ID).Rules.List(); len(kept) != 1 || len(mustSession(t, manager, b.ID).History.List()) != 1 {
		t.Fatal("reset touched the other session")
	}

	if _, err := mustSession(t, manager, sessions.DefaultID).Rules.Create(rules.Draft{Method: "GET", PathPattern: "/v1/dev", StatusCode: 200, Enabled: true, Name: "keep-me"}); err != nil {
		t.Fatal(err)
	}
	blocked := apiJSON(t, srv, http.MethodPost, "/_api/sessions/default/reset", token, "")
	if blocked.Code != http.StatusConflict {
		t.Fatalf("confirm = %d %s", blocked.Code, blocked.Body)
	}
	if len(mustSession(t, manager, sessions.DefaultID).Rules.List()) != 1 {
		t.Fatal("default was reset without confirmation")
	}
	confirmed := apiJSON(t, srv, http.MethodPost, "/_api/sessions/default/reset?confirm=default", token, "")
	if confirmed.Code != http.StatusNoContent || len(mustSession(t, manager, sessions.DefaultID).Rules.List()) != 0 {
		t.Fatalf("confirmed = %d rules %d", confirmed.Code, len(mustSession(t, manager, sessions.DefaultID).Rules.List()))
	}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			resp, err := http.Get(b.ProxyURL + "/v1/keep")
			if err == nil {
				resp.Body.Close()
			}
		}()
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/_api/sessions/"+b.ID+"/history", strings.NewReader(""))
			req.Host = "127.0.0.1:8081"
			req.Header.Set("Authorization", "Bearer "+token)
			srv.ServeHTTP(httptest.NewRecorder(), req)
		}()
	}
	wg.Wait()
}

func mustSession(t *testing.T, manager *sessions.Manager, id string) sessions.Info {
	t.Helper()
	info, ok := manager.Get(id)
	if !ok {
		t.Fatalf("session %s missing", id)
	}
	return info
}
