package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Shayokh144/Mock_API_Service/internal/admin"
	"github.com/Shayokh144/Mock_API_Service/internal/history"
	"github.com/Shayokh144/Mock_API_Service/internal/rules"
	"github.com/Shayokh144/Mock_API_Service/internal/sessions"
)

func TestDefaultSessionWritesRequireConfirm(t *testing.T) {
	dir := t.TempDir()
	rulesPath := filepath.Join(dir, "rules.json")
	store, err := rules.Load(rulesPath)
	if err != nil {
		t.Fatal(err)
	}
	kept, err := store.Create(rules.Draft{
		Name: "Kept", APIName: "kept", Enabled: true, Method: "GET",
		PathPattern: "/v1/kept", StatusCode: 200, Body: `{"ok":true}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(rulesPath)
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := LoadToken(filepath.Join(dir, "admin.token"))
	if err != nil {
		t.Fatal(err)
	}
	manager := sessions.New(sessions.Options{
		Rules: store, History: history.New(5),
		ProxyAddr: "127.0.0.1:8080", AdminAddr: "127.0.0.1:8081", HistoryLimit: 5,
	})
	defer manager.Close()
	srv := New(tokens, "127.0.0.1:8081", "test")
	srv.UseSessions(manager)
	token := readToken(t, tokens)
	body := `{"description":"new","name":"new-rule","method":"GET","path":"/v1/new","status":200}`
	scenario := "version: 1\nname: swap\nrules:\n  - name: other\n    method: GET\n    path: /v1/other\n    status: 200\n"
	blocked := []struct{ method, path, payload string }{
		{http.MethodPost, "/_api/sessions/default/rules", body},
		{http.MethodPut, "/_api/sessions/default/rules/" + kept.ID, body},
		{http.MethodDelete, "/_api/sessions/default/rules/" + kept.ID, ""},
		{http.MethodPost, "/_api/sessions/default/rules/" + kept.ID + "/disable", ""},
		{http.MethodPost, "/_api/sessions/default/rules/" + kept.ID + "/enable", ""},
		{http.MethodPut, "/_api/sessions/default/rules/by-name/kept", body},
		{http.MethodDelete, "/_api/sessions/default/rules/by-name/kept", ""},
		{http.MethodPost, "/_api/sessions/default/rules/by-name/kept/disable", ""},
		{http.MethodPost, "/_api/sessions/default/reset", ""},
		{http.MethodPost, "/_api/sessions/default/rules?confirm=yes", body},
	}
	for _, tc := range blocked {
		got := apiJSON(t, srv, tc.method, tc.path, token, tc.payload)
		if got.Code != http.StatusConflict || !strings.Contains(got.Body, "confirm=default") {
			t.Fatalf("%s %s = %d %s", tc.method, tc.path, got.Code, got.Body)
		}
	}
	loaded := apiRaw(t, srv, http.MethodPost, "/_api/sessions/default/scenarios/load", token, "application/yaml", scenario)
	if loaded.Code != http.StatusConflict {
		t.Fatalf("scenario = %d %s", loaded.Code, loaded.Body)
	}
	if len(store.List()) != 1 || store.List()[0].PathPattern != "/v1/kept" || !store.List()[0].Enabled {
		t.Fatalf("rules changed: %+v", store.List())
	}
	raw, err := os.ReadFile(rulesPath)
	if err != nil || string(raw) != string(before) {
		t.Fatalf("rules.json changed:\n%s", raw)
	}

	listed := apiJSON(t, srv, http.MethodGet, "/_api/sessions/default/rules", token, "")
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body, "/v1/kept") {
		t.Fatalf("get = %d %s", listed.Code, listed.Body)
	}
	removed := apiJSON(t, srv, http.MethodDelete, "/_api/sessions/default?confirm=default", token, "")
	if removed.Code != http.StatusConflict || !strings.Contains(removed.Body, "cannot be deleted") {
		t.Fatalf("delete session = %d %s", removed.Code, removed.Body)
	}

	created := apiJSON(t, srv, http.MethodPost, "/_api/sessions/default/rules?confirm=default", token, body)
	if created.Code != http.StatusCreated {
		t.Fatalf("confirmed create = %d %s", created.Code, created.Body)
	}
	off := apiJSON(t, srv, http.MethodPost, "/_api/sessions/default/rules/"+kept.ID+"/disable?confirm=default", token, "")
	if off.Code != http.StatusOK {
		t.Fatalf("disable = %d %s", off.Code, off.Body)
	}
	on := apiJSON(t, srv, http.MethodPost, "/_api/sessions/default/rules/by-name/kept/enable?confirm=default", token, "")
	if on.Code != http.StatusOK {
		t.Fatalf("enable = %d %s", on.Code, on.Body)
	}

	session := postSession(t, srv, token, `{"name":"qa"}`)
	testWrite := apiJSON(t, srv, http.MethodPost, "/_api/sessions/"+session.ID+"/rules", token, body)
	if testWrite.Code != http.StatusCreated {
		t.Fatalf("test session = %d %s", testWrite.Code, testWrite.Body)
	}

	dashStore, err := rules.Load(filepath.Join(dir, "dash.json"))
	if err != nil {
		t.Fatal(err)
	}
	dashboard, err := admin.New(dashStore, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	dashboard.UseListenAddr("127.0.0.1:8081")
	form := url.Values{
		"name": {"Dash"}, "enabled": {"on"}, "method": {"GET"},
		"path_pattern": {"/v1/dash"}, "status_code": {"200"}, "body": {`{"ok":true}`},
	}
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8081/_admin/rules", strings.NewReader(form.Encode()))
	req.Host = "127.0.0.1:8081"
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "http://127.0.0.1:8081")
	rr := httptest.NewRecorder()
	dashboard.ServeHTTP(rr, req)
	if rr.Code != http.StatusSeeOther || len(dashStore.List()) != 1 {
		t.Fatalf("dashboard = %d rules %d body %s", rr.Code, len(dashStore.List()), rr.Body.String())
	}
}
