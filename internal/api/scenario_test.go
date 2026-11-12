package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestScenarioLoadMergeAndSession(t *testing.T) {
	_, srv, manager := testAPI(t, t.TempDir())
	defer manager.Close()
	token := readToken(t, srv.tokens)
	session := postSession(t, srv, token, `{"name":"pre","scenario":{"version":1,"name":"boot","description":"","rules":[{"name":"ping","method":"GET","path":"/v1/ping","status":200,"body":{"ok":true}}]}}`)
	resp, err := http.Get(session.ProxyURL + "/v1/ping")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("preloaded = %d", resp.StatusCode)
	}

	body := "version: 1\nname: more\ndescription: add\nrules:\n  - name: ping\n    method: GET\n    path: /v1/ping\n    status: 201\n    body:\n      ok: true\n  - name: other\n    method: GET\n    path: /v1/other\n    status: 200\n    body:\n      n: 1\n"
	merged := apiRaw(t, srv, http.MethodPost, "/_api/sessions/"+session.ID+"/scenarios/load?mode=merge", token, "application/yaml", body)
	if merged.Code != http.StatusOK || !strings.Contains(merged.Body, `"updated"`) || !strings.Contains(merged.Body, "other") {
		t.Fatalf("merge = %d %s", merged.Code, merged.Body)
	}
	before := len(mustSession(t, manager, session.ID).Rules.List())
	bad := apiRaw(t, srv, http.MethodPost, "/_api/sessions/"+session.ID+"/scenarios/load?mode=replace", token, "application/yaml", "version: 1\nname: bad\nrules:\n  - name: nope\n    method: GET\n    path: /_api/secret\n    status: 200\n")
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("atomic = %d %s", bad.Code, bad.Body)
	}
	if len(mustSession(t, manager, session.ID).Rules.List()) != before {
		t.Fatal("failed load changed rules")
	}
	exported := apiJSON(t, srv, http.MethodGet, "/_api/sessions/"+session.ID+"/scenario?format=yaml", token, "")
	if exported.Code != http.StatusOK || !strings.Contains(exported.Body, "name: ping") {
		t.Fatalf("export = %d %s", exported.Code, exported.Body)
	}
	replaced := apiRaw(t, srv, http.MethodPost, "/_api/sessions/"+session.ID+"/scenarios/load", token, "application/yaml", "version: 1\nname: only\nrules:\n  - name: only-one\n    method: GET\n    path: /v1/only\n    status: 200\n    body:\n      ok: true\n")
	if replaced.Code != http.StatusOK || !strings.Contains(replaced.Body, "only-one") {
		t.Fatalf("replace = %d %s", replaced.Code, replaced.Body)
	}
	if rulesLeft := mustSession(t, manager, session.ID).Rules.List(); len(rulesLeft) != 1 || rulesLeft[0].APIName != "only-one" {
		t.Fatalf("replaced rules = %+v", rulesLeft)
	}
}

func apiRaw(t *testing.T, srv *Server, method, path, token, contentType, body string) apiResult {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Host = "127.0.0.1:8081"
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", contentType)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	return apiResult{Code: rr.Code, Body: rr.Body.String()}
}
