package api

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Shayokh144/Mock_API_Service/internal/admin"
	"github.com/Shayokh144/Mock_API_Service/internal/history"
	"github.com/Shayokh144/Mock_API_Service/internal/rules"
	"github.com/Shayokh144/Mock_API_Service/internal/sessions"
	"github.com/Shayokh144/Mock_API_Service/internal/upstreams"
)

func TestUpstreamRoutesPersistAndSessionsSeeThem(t *testing.T) {
	dir := t.TempDir()
	rulesStore, err := rules.Load(filepath.Join(dir, "rules.json"))
	if err != nil {
		t.Fatal(err)
	}
	ups, err := upstreams.Load(filepath.Join(dir, "upstreams.json"))
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := LoadToken(filepath.Join(dir, "admin.token"))
	if err != nil {
		t.Fatal(err)
	}
	manager := sessions.New(sessions.Options{
		Rules: rulesStore, History: history.New(10), Upstreams: ups,
		ProxyAddr: "127.0.0.1:8080", AdminAddr: "127.0.0.1:8081",
		Timeout: time.Second, HistoryLimit: 10,
	})
	defer manager.Close()
	srv := New(tokens, "127.0.0.1:8081", "test")
	srv.UseSessions(manager)
	token := readToken(t, tokens)
	session := postSession(t, srv, token, `{"name":"app"}`)

	miss := readProxy(t, session.ProxyURL+"/staging/v1/health")
	if !strings.Contains(miss, "no upstream matches this path") {
		t.Fatalf("before create = %s", miss)
	}

	var logs strings.Builder
	prev := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(prev)

	created := apiJSON(t, srv, http.MethodPost, "/_api/upstreams?confirm=default", token, `{"name":"staging","base_url":"http://127.0.0.1:1/secret-path","prefix":"/staging"}`)
	if created.Code != http.StatusCreated || !strings.Contains(created.Body, `"name":"staging"`) || !strings.Contains(created.Body, `"prefix":"/staging"`) || !strings.Contains(created.Body, "secret-path") {
		t.Fatalf("create = %d %s", created.Code, created.Body)
	}
	if strings.Contains(created.Body, "password") || strings.Contains(created.Body, "token") || strings.Contains(created.Body, "Authorization") {
		t.Fatalf("create response has secret fields: %s", created.Body)
	}
	var made upstreams.Upstream
	if err := json.Unmarshal([]byte(created.Body), &made); err != nil || made.ID == "" {
		t.Fatalf("created upstream = %v %s", err, created.Body)
	}
	logText := logs.String()
	if !strings.Contains(logText, "upstream created") || !strings.Contains(logText, made.ID) || !strings.Contains(logText, `name="staging"`) || !strings.Contains(logText, `prefix="/staging"`) || !strings.Contains(logText, "host=127.0.0.1") || strings.Contains(logText, "secret-path") {
		t.Fatalf("log = %s", logText)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "upstreams.json"))
	if err != nil || !strings.Contains(string(raw), `"name": "staging"`) || !strings.Contains(string(raw), "/staging") {
		t.Fatalf("upstreams file = %s %v", raw, err)
	}
	listed := apiJSON(t, srv, http.MethodGet, "/_api/upstreams", token, "")
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body, made.ID) || strings.Contains(listed.Body, "s3cret") {
		t.Fatalf("list = %d %s", listed.Code, listed.Body)
	}
	one := apiJSON(t, srv, http.MethodGet, "/_api/upstreams/"+made.ID, token, "")
	if one.Code != http.StatusOK || !strings.Contains(one.Body, "http://127.0.0.1:1/secret-path") {
		t.Fatalf("get = %d %s", one.Code, one.Body)
	}

	dashboard, err := admin.New(rulesStore, history.New(5), ups)
	if err != nil {
		t.Fatal(err)
	}
	dash := httptest.NewServer(dashboard)
	defer dash.Close()
	page := readProxy(t, dash.URL+"/_admin/upstreams")
	if !strings.Contains(page, "staging") || !strings.Contains(page, "/staging") {
		t.Fatalf("dashboard = %s", page)
	}

	routed := readProxy(t, session.ProxyURL+"/staging/v1/health")
	if strings.Contains(routed, "no upstream matches this path") {
		t.Fatalf("session did not see the new upstream: %s", routed)
	}

	replaced := apiJSON(t, srv, http.MethodPut, "/_api/upstreams/"+made.ID+"?confirm=default", token, `{"name":"develop","base_url":"http://127.0.0.1:1","prefix":"/develop"}`)
	if replaced.Code != http.StatusOK || !strings.Contains(replaced.Body, `"name":"develop"`) || !strings.Contains(replaced.Body, `"prefix":"/develop"`) {
		t.Fatalf("put = %d %s", replaced.Code, replaced.Body)
	}
	if !strings.Contains(logs.String(), "upstream updated") || strings.Contains(logs.String(), "secret-path") {
		t.Fatalf("update log = %s", logs.String())
	}
	again := readProxy(t, dash.URL+"/_admin/upstreams")
	if !strings.Contains(again, "develop") || !strings.Contains(again, "/develop") {
		t.Fatalf("dashboard after put = %s", again)
	}

	sessionRule, err := mustSession(t, manager, session.ID).Rules.Create(rules.Draft{
		Name: "session", Enabled: true, Method: "GET", PathPattern: "/v1/session", StatusCode: 200, UpstreamID: made.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	linked, err := rulesStore.Create(rules.Draft{
		Name: "linked", Enabled: true, Method: "GET", PathPattern: "/v1/only", StatusCode: 200, UpstreamID: made.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	anyRule, err := rulesStore.Create(rules.Draft{
		Name: "any", Enabled: true, Method: "GET", PathPattern: "/v1/only", StatusCode: 200,
	})
	if err != nil {
		t.Fatal(err)
	}
	blocked := apiJSON(t, srv, http.MethodDelete, "/_api/upstreams/"+made.ID+"?confirm=default", token, "")
	if blocked.Code != http.StatusConflict || !strings.Contains(blocked.Body, "duplicate Any rule") {
		t.Fatalf("blocked delete = %d %s", blocked.Code, blocked.Body)
	}
	if _, ok := ups.Get(made.ID); !ok {
		t.Fatal("blocked delete removed the upstream")
	}
	if got, ok := rulesStore.Get(linked.ID); !ok || got.UpstreamID != made.ID {
		t.Fatalf("linked rule changed on blocked delete: %+v", got)
	}

	if err := rulesStore.Delete(anyRule.ID); err != nil {
		t.Fatal(err)
	}
	removed := apiJSON(t, srv, http.MethodDelete, "/_api/upstreams/"+made.ID+"?confirm=default", token, "")
	if removed.Code != http.StatusNoContent {
		t.Fatalf("delete = %d %s", removed.Code, removed.Body)
	}
	if !strings.Contains(logs.String(), "upstream deleted") {
		t.Fatalf("delete log = %s", logs.String())
	}
	missing := apiJSON(t, srv, http.MethodGet, "/_api/upstreams/"+made.ID, token, "")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("get deleted = %d %s", missing.Code, missing.Body)
	}
	if got, ok := rulesStore.Get(linked.ID); !ok || got.UpstreamID != "" {
		t.Fatalf("developer rule was not set to Any: %+v", got)
	}
	if got, ok := mustSession(t, manager, session.ID).Rules.Get(sessionRule.ID); !ok || got.UpstreamID != made.ID {
		t.Fatalf("test session rule changed: %+v", got)
	}
	saved, err := os.ReadFile(filepath.Join(dir, "upstreams.json"))
	if err != nil || strings.Contains(string(saved), "develop") {
		t.Fatalf("file after delete = %s %v", saved, err)
	}

	absent := apiJSON(t, srv, http.MethodPut, "/_api/upstreams/0123456789abcdef?confirm=default", token, `{"name":"n","base_url":"http://127.0.0.1:1","prefix":"/n"}`)
	if absent.Code != http.StatusNotFound {
		t.Fatalf("put missing = %d %s", absent.Code, absent.Body)
	}
	gone := apiJSON(t, srv, http.MethodDelete, "/_api/upstreams/0123456789abcdef?confirm=default", token, "")
	if gone.Code != http.StatusNotFound {
		t.Fatalf("delete missing = %d %s", gone.Code, gone.Body)
	}
}

func TestUpstreamWriteSafeguards(t *testing.T) {
	dir := t.TempDir()
	ups, err := upstreams.Load(filepath.Join(dir, "upstreams.json"))
	if err != nil {
		t.Fatal(err)
	}
	rulesStore, err := rules.Load(filepath.Join(dir, "rules.json"))
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := LoadToken(filepath.Join(dir, "admin.token"))
	if err != nil {
		t.Fatal(err)
	}
	manager := sessions.New(sessions.Options{
		Rules: rulesStore, History: history.New(5), Upstreams: ups,
		ProxyAddr: "127.0.0.1:8080", AdminAddr: "127.0.0.1:8081", HistoryLimit: 5,
	})
	defer manager.Close()
	srv := New(tokens, "127.0.0.1:8081", "test")
	srv.UseSessions(manager)
	token := readToken(t, tokens)
	body := `{"name":"staging","base_url":"https://staging.example.com","prefix":"/staging"}`

	blocked := apiJSON(t, srv, http.MethodPost, "/_api/upstreams", token, body)
	if blocked.Code != http.StatusConflict || !strings.Contains(blocked.Body, `"code":"conflict"`) || !strings.Contains(blocked.Body, "confirm=default") {
		t.Fatalf("confirm = %d %s", blocked.Code, blocked.Body)
	}
	if _, err := os.Stat(filepath.Join(dir, "upstreams.json")); !os.IsNotExist(err) {
		t.Fatal("missing confirm wrote upstreams.json")
	}
	if len(ups.List()) != 0 {
		t.Fatal("missing confirm stored an upstream")
	}

	scheme := apiJSON(t, srv, http.MethodPost, "/_api/upstreams?confirm=default", token, `{"name":"files","base_url":"ftp://files.example.com","prefix":"/files"}`)
	if scheme.Code != http.StatusBadRequest || !strings.Contains(scheme.Body, "http or https") {
		t.Fatalf("scheme = %d %s", scheme.Code, scheme.Body)
	}
	userinfo := apiJSON(t, srv, http.MethodPost, "/_api/upstreams?confirm=default", token, `{"name":"creds","base_url":"https://alice:s3cret@staging.example.com","prefix":"/creds"}`)
	if userinfo.Code != http.StatusBadRequest || !strings.Contains(userinfo.Body, "username or password") || strings.Contains(userinfo.Body, "s3cret") {
		t.Fatalf("userinfo = %d %s", userinfo.Code, userinfo.Body)
	}
	emptyHost := apiJSON(t, srv, http.MethodPost, "/_api/upstreams?confirm=default", token, `{"name":"blank","base_url":"http://","prefix":"/blank"}`)
	if emptyHost.Code != http.StatusBadRequest {
		t.Fatalf("empty host = %d %s", emptyHost.Code, emptyHost.Body)
	}

	first := apiJSON(t, srv, http.MethodPost, "/_api/upstreams?confirm=default", token, body)
	if first.Code != http.StatusCreated {
		t.Fatalf("first = %d %s", first.Code, first.Body)
	}
	dup := apiJSON(t, srv, http.MethodPost, "/_api/upstreams?confirm=default", token, `{"name":"other","base_url":"https://other.example.com","prefix":"/staging"}`)
	if dup.Code != http.StatusConflict || !strings.Contains(dup.Body, "prefix /staging") {
		t.Fatalf("duplicate prefix = %d %s", dup.Code, dup.Body)
	}
	if len(ups.List()) != 1 {
		t.Fatalf("duplicate prefix stored a second upstream: %d", len(ups.List()))
	}

	unknownField := apiJSON(t, srv, http.MethodPost, "/_api/upstreams?confirm=default", token, `{"name":"extra","base_url":"https://extra.example.com","prefix":"/extra","token":"secret"}`)
	if unknownField.Code != http.StatusBadRequest || strings.Contains(unknownField.Body, "secret") {
		t.Fatalf("unknown field = %d %s", unknownField.Code, unknownField.Body)
	}
	plain := callUpstream(t, srv, http.MethodPost, "/_api/upstreams?confirm=default", token, "text/plain", body, "")
	if plain.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("content type = %d %s", plain.Code, plain.Body)
	}
	origin := callUpstream(t, srv, http.MethodPost, "/_api/upstreams?confirm=default", token, "application/json", body, "http://127.0.0.1:8081")
	if origin.Code != http.StatusForbidden {
		t.Fatalf("origin = %d %s", origin.Code, origin.Body)
	}
	forged := callUpstream(t, srv, http.MethodGet, "/_api/upstreams", token, "", "", "bad-host")
	if forged.Code != http.StatusForbidden {
		t.Fatalf("host = %d %s", forged.Code, forged.Body)
	}
	if len(ups.List()) != 1 {
		t.Fatal("rejected writes changed the store")
	}

	noToken := callUpstream(t, srv, http.MethodGet, "/_api/upstreams", "", "", "", "")
	if noToken.Code != http.StatusUnauthorized {
		t.Fatalf("token = %d %s", noToken.Code, noToken.Body)
	}
	method := apiJSON(t, srv, http.MethodPatch, "/_api/upstreams", token, body)
	if method.Code != http.StatusMethodNotAllowed {
		t.Fatalf("patch = %d %s", method.Code, method.Body)
	}
}

func TestUpstreamResponseMasksUserinfo(t *testing.T) {
	masked := maskUpstream(upstreams.Upstream{
		ID:      "0123456789abcdef",
		Name:    "staging",
		BaseURL: "https://alice:s3cret@staging.example.com/v1",
		Prefix:  "/staging",
	})
	if strings.Contains(masked.BaseURL, "alice") || strings.Contains(masked.BaseURL, "s3cret") {
		t.Fatalf("masked url = %s", masked.BaseURL)
	}
	if !strings.Contains(masked.BaseURL, "https://***:***@staging.example.com/v1") {
		t.Fatalf("masked url = %s", masked.BaseURL)
	}
	userOnly := maskBaseURL("https://alice@staging.example.com/v1")
	if strings.Contains(userOnly, "alice") || !strings.Contains(userOnly, "***@staging.example.com") {
		t.Fatalf("user only = %s", userOnly)
	}
}

func TestHistoryRejectsUnknownUpstreamName(t *testing.T) {
	_, srv, manager := testAPI(t, t.TempDir())
	defer manager.Close()
	token := readToken(t, srv.tokens)
	okya, err := manager.Upstreams().Create("OKYA", "https://okya.example", "/okya")
	if err != nil {
		t.Fatal(err)
	}
	session := postSession(t, srv, token, `{"name":"hist"}`)
	info := mustSession(t, manager, session.ID)
	info.History.Add(history.Entry{
		Method: "GET", Path: "/v1/ok", Status: 200, Source: history.SourceUpstream,
		UpstreamID: okya.ID, UpstreamName: "OKYA",
	})
	info.History.Add(history.Entry{
		Method: "GET", Path: "/v1/any", Status: 200, Source: history.SourceMock,
		UpstreamID: history.UpstreamAny, UpstreamName: "any",
	})

	named := apiJSON(t, srv, http.MethodGet, "/_api/sessions/"+session.ID+"/history?upstream=OKYA", token, "")
	if named.Code != http.StatusOK || !strings.Contains(named.Body, "/v1/ok") || strings.Contains(named.Body, "/v1/any") {
		t.Fatalf("name = %d %s", named.Code, named.Body)
	}
	anyCase := apiJSON(t, srv, http.MethodGet, "/_api/sessions/"+session.ID+"/history?upstream=aNy", token, "")
	if anyCase.Code != http.StatusOK || !strings.Contains(anyCase.Body, "/v1/any") || strings.Contains(anyCase.Body, "/v1/ok") {
		t.Fatalf("any = %d %s", anyCase.Code, anyCase.Body)
	}
	unknown := apiJSON(t, srv, http.MethodGet, "/_api/sessions/"+session.ID+"/history?upstream=OKYAA", token, "")
	if unknown.Code != http.StatusBadRequest || !strings.Contains(unknown.Body, `"code":"unknown_upstream"`) || !strings.Contains(unknown.Body, `unknown upstream \"OKYAA\"`) {
		t.Fatalf("unknown = %d %s", unknown.Code, unknown.Body)
	}
	byID := apiJSON(t, srv, http.MethodGet, "/_api/sessions/"+session.ID+"/history?upstream="+okya.ID, token, "")
	if byID.Code != http.StatusOK || !strings.Contains(byID.Body, "/v1/ok") {
		t.Fatalf("id = %d %s", byID.Code, byID.Body)
	}
	if err := manager.Upstreams().Delete(okya.ID); err != nil {
		t.Fatal(err)
	}
	deletedName := apiJSON(t, srv, http.MethodGet, "/_api/sessions/"+session.ID+"/history?upstream=OKYA", token, "")
	if deletedName.Code != http.StatusBadRequest || !strings.Contains(deletedName.Body, "unknown_upstream") {
		t.Fatalf("deleted name = %d %s", deletedName.Code, deletedName.Body)
	}
	deletedID := apiJSON(t, srv, http.MethodGet, "/_api/sessions/"+session.ID+"/history?upstream="+okya.ID, token, "")
	if deletedID.Code != http.StatusOK || !strings.Contains(deletedID.Body, "/v1/ok") {
		t.Fatalf("deleted id = %d %s", deletedID.Code, deletedID.Body)
	}
}

func callUpstream(t *testing.T, srv *Server, method, path, token, contentType, body, origin string) apiResult {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if origin == "bad-host" {
		req.Host = "rebind.evil.example:8081"
	} else {
		req.Host = "127.0.0.1:8081"
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if origin != "" && origin != "bad-host" {
		req.Header.Set("Origin", origin)
	}
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	return apiResult{Code: rr.Code, Body: rr.Body.String()}
}

func readProxy(t *testing.T, rawURL string) string {
	t.Helper()
	resp, err := http.Get(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	buf, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(buf)
}
