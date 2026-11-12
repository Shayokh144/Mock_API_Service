package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Shayokh144/Mock_API_Service/internal/history"
	"github.com/Shayokh144/Mock_API_Service/internal/rules"
	"github.com/Shayokh144/Mock_API_Service/internal/sessions"
	"github.com/Shayokh144/Mock_API_Service/internal/upstreams"
)

func TestRuleCRUDIsolationAndPersistence(t *testing.T) {
	dir := t.TempDir()
	store, err := rules.Load(filepath.Join(dir, "rules.json"))
	if err != nil {
		t.Fatal(err)
	}
	ups, err := upstreams.Load(filepath.Join(dir, "upstreams.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ups.Create("OKYA", "http://127.0.0.1:9", ""); err != nil {
		t.Fatal(err)
	}
	tokens, err := LoadToken(filepath.Join(dir, "admin.token"))
	if err != nil {
		t.Fatal(err)
	}
	manager := sessions.New(sessions.Options{
		Rules: store, History: history.New(20), Upstreams: ups,
		ProxyAddr: "127.0.0.1:8080", AdminAddr: "127.0.0.1:8081", HistoryLimit: 20,
	})
	defer manager.Close()
	srv := New(tokens, "127.0.0.1:8081", "test")
	srv.UseSessions(manager)
	token := readToken(t, tokens)

	session := postSession(t, srv, token, `{"name":"a"}`)
	other := postSession(t, srv, token, `{"name":"b"}`)
	body := `{"description":"profile","upstream":"OKYA","method":"GET","path":"/v1/profile","query_conditions":["page=2"],"status":201,"headers":{"Content-Type":"application/json"},"body":{"ok":true},"delay_ms":0,"enabled":true}`
	created := apiJSON(t, srv, http.MethodPost, "/_api/sessions/"+session.ID+"/rules", token, body)
	if created.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", created.Code, created.Body)
	}
	var rule ruleResponse
	if err := json.Unmarshal([]byte(created.Body), &rule); err != nil {
		t.Fatal(err)
	}
	if rule.ID == "" || rule.Upstream != "OKYA" || rule.Path != "/v1/profile" || !rule.Enabled {
		t.Fatalf("rule = %+v", rule)
	}
	resp, err := http.Get(session.ProxyURL + "/v1/profile?page=2")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("proxied status = %d", resp.StatusCode)
	}
	miss, err := http.Get(other.ProxyURL + "/v1/profile?page=2")
	if err != nil {
		t.Fatal(err)
	}
	miss.Body.Close()
	if miss.StatusCode == http.StatusCreated {
		t.Fatal("other session returned the rule")
	}

	again := apiJSON(t, srv, http.MethodPost, "/_api/sessions/"+session.ID+"/rules/"+rule.ID+"/disable", token, "")
	if again.Code != http.StatusOK || strings.Contains(again.Body, `"enabled":true`) {
		t.Fatalf("disable = %d %s", again.Code, again.Body)
	}
	twice := apiJSON(t, srv, http.MethodPost, "/_api/sessions/"+session.ID+"/rules/"+rule.ID+"/disable", token, "")
	if twice.Code != http.StatusOK {
		t.Fatalf("second disable = %d", twice.Code)
	}
	on := apiJSON(t, srv, http.MethodPost, "/_api/sessions/"+session.ID+"/rules/"+rule.ID+"/enable", token, "")
	if on.Code != http.StatusOK || !strings.Contains(on.Body, `"enabled":true`) {
		t.Fatalf("enable = %d %s", on.Code, on.Body)
	}

	bad := apiJSON(t, srv, http.MethodPost, "/_api/sessions/"+session.ID+"/rules", token, `{"method":"GET","path":"/_api/health","status":200,"extra":1}`)
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("unknown field = %d %s", bad.Code, bad.Body)
	}
	reserved := apiJSON(t, srv, http.MethodPost, "/_api/sessions/"+session.ID+"/rules", token, `{"method":"GET","path":"/_api/health","status":200}`)
	if reserved.Code != http.StatusBadRequest || !strings.Contains(reserved.Body, "/_api") {
		t.Fatalf("reserved = %d %s", reserved.Code, reserved.Body)
	}
	unknownUp := apiJSON(t, srv, http.MethodPost, "/_api/sessions/"+session.ID+"/rules", token, `{"method":"GET","path":"/v1/x","status":200,"upstream":"missing"}`)
	if unknownUp.Code != http.StatusBadRequest || !strings.Contains(unknownUp.Body, "Any upstream") {
		t.Fatalf("upstream = %d %s", unknownUp.Code, unknownUp.Body)
	}

	filtered := apiJSON(t, srv, http.MethodGet, "/_api/sessions/"+session.ID+"/rules?upstream=OKYA", token, "")
	if filtered.Code != http.StatusOK || !strings.Contains(filtered.Body, "profile") {
		t.Fatalf("filter = %s", filtered.Body)
	}

	replaced := apiJSON(t, srv, http.MethodPut, "/_api/sessions/"+session.ID+"/rules/"+rule.ID, token, `{"description":"renamed","upstream":"Any","method":"GET","path":"/v1/profile","status":200,"body":{"ok":false}}`)
	if replaced.Code != http.StatusOK || !strings.Contains(replaced.Body, "renamed") {
		t.Fatalf("put = %d %s", replaced.Code, replaced.Body)
	}
	deleted := apiJSON(t, srv, http.MethodDelete, "/_api/sessions/"+session.ID+"/rules/"+rule.ID, token, "")
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete = %d", deleted.Code)
	}
	if _, err := os.Stat(filepath.Join(dir, "rules.json")); !os.IsNotExist(err) {
		t.Fatal("test session wrote rules.json")
	}

	persisted := apiJSON(t, srv, http.MethodPost, "/_api/sessions/default/rules", token, `{"description":"kept","method":"GET","path":"/v1/kept","status":200,"body":{"n":1}}`)
	if persisted.Code != http.StatusCreated {
		t.Fatalf("default create = %d %s", persisted.Code, persisted.Body)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "rules.json"))
	if err != nil || !strings.Contains(string(raw), "/v1/kept") {
		t.Fatalf("default rules file = %s %v", raw, err)
	}
	missing := apiJSON(t, srv, http.MethodGet, "/_api/sessions/does-not-exist/rules", token, "")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing session = %d", missing.Code)
	}
}

type apiResult struct {
	Code int
	Body string
}

func TestRuleLimit(t *testing.T) {
	_, srv, manager := testAPI(t, t.TempDir())
	defer manager.Close()
	token := readToken(t, srv.tokens)
	session := postSession(t, srv, token, `{"name":"full"}`)
	for i := 0; i < maxRulesPerSession; i++ {
		payload := fmt.Sprintf(`{"method":"GET","path":"/v1/n/%d","status":200,"body":{"ok":true}}`, i)
		got := apiJSON(t, srv, http.MethodPost, "/_api/sessions/"+session.ID+"/rules", token, payload)
		if got.Code != http.StatusCreated {
			t.Fatalf("create %d = %d %s", i, got.Code, got.Body)
		}
	}
	over := apiJSON(t, srv, http.MethodPost, "/_api/sessions/"+session.ID+"/rules", token, `{"method":"POST","path":"/v1/extra","status":200}`)
	if over.Code != http.StatusTooManyRequests {
		t.Fatalf("limit = %d %s", over.Code, over.Body)
	}
}

func apiJSON(t *testing.T, srv *Server, method, path, token, body string) apiResult {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Host = "127.0.0.1:8081"
	req.Header.Set("Authorization", "Bearer "+token)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	return apiResult{Code: rr.Code, Body: rr.Body.String()}
}
