package api

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Shayokh144/Mock_API_Service/internal/history"
	"github.com/Shayokh144/Mock_API_Service/internal/rules"
	"github.com/Shayokh144/Mock_API_Service/internal/sessions"
)

func TestSessionsAreIsolated(t *testing.T) {
	dir := t.TempDir()
	tokens, srv, manager := testAPI(t, dir)
	before := dirEntries(t, dir)
	defer manager.Close()
	token := readToken(t, tokens)

	first := postSession(t, srv, token, `{"name":"login-tests","port":0,"idle_timeout_seconds":1800}`)
	second := postSession(t, srv, token, `{"name":"other"}`)
	if first.ID == second.ID || first.Port == second.Port || first.Port == 0 {
		t.Fatalf("sessions = %+v %+v", first, second)
	}
	if first.ProxyURL != "http://127.0.0.1:"+strconv.Itoa(first.Port) || first.EmulatorURL != "http://10.0.2.2:"+strconv.Itoa(first.Port) {
		t.Fatalf("urls = %+v", first)
	}
	one, ok := manager.Get(first.ID)
	if !ok {
		t.Fatal("missing session")
	}
	if _, err := one.Rules.Create(rules.Draft{
		Name: "Only A", Enabled: true, Method: "GET", PathPattern: "/v1/only", StatusCode: 201, Body: `{"ok":true}`,
	}); err != nil {
		t.Fatal(err)
	}
	got := httpGet(t, first.ProxyURL+"/v1/only")
	if got.StatusCode != http.StatusCreated {
		t.Fatalf("session A status = %d", got.StatusCode)
	}
	got.Body.Close()
	other := httpGet(t, second.ProxyURL+"/v1/only")
	other.Body.Close()
	if other.StatusCode == http.StatusCreated {
		t.Fatal("session B saw session A's rule")
	}
	if len(one.History.List()) != 1 {
		t.Fatalf("session A history = %d", len(one.History.List()))
	}
	otherInfo, ok := manager.Get(second.ID)
	if !ok {
		t.Fatal("missing second session")
	}
	for _, entry := range otherInfo.History.List() {
		if entry.RuleID != "" {
			t.Fatalf("session B matched a rule: %+v", entry)
		}
	}
	def, ok := manager.Get(sessions.DefaultID)
	if !ok || len(def.Rules.List()) != 0 || len(def.History.List()) != 0 {
		t.Fatal("default session changed")
	}
	if len(dirEntries(t, dir)) != len(before) {
		t.Fatalf("test session wrote files: %v", dirEntries(t, dir))
	}

	listed := call(t, srv, http.MethodGet, "/_api/sessions", "127.0.0.1:8081", "Bearer "+token, nil)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), first.ID) || !strings.Contains(listed.Body.String(), `"id":"default"`) {
		t.Fatalf("list = %d %s", listed.Code, listed.Body.String())
	}
	onePage := call(t, srv, http.MethodGet, "/_api/sessions/"+first.ID, "127.0.0.1:8081", "Bearer "+token, nil)
	if onePage.Code != http.StatusOK || !strings.Contains(onePage.Body.String(), `"name":"login-tests"`) {
		t.Fatalf("get = %d %s", onePage.Code, onePage.Body.String())
	}

	blocked := call(t, srv, http.MethodDelete, "/_api/sessions/default", "127.0.0.1:8081", "Bearer "+token, nil)
	if blocked.Code != http.StatusConflict {
		t.Fatalf("delete default = %d %s", blocked.Code, blocked.Body.String())
	}
	removed := call(t, srv, http.MethodDelete, "/_api/sessions/"+first.ID, "127.0.0.1:8081", "Bearer "+token, nil)
	if removed.Code != http.StatusNoContent {
		t.Fatalf("delete = %d", removed.Code)
	}
	closed := httpGet(t, first.ProxyURL+"/v1/only")
	if closed.StatusCode != 0 {
		closed.Body.Close()
		t.Fatal("deleted session still accepted a connection")
	}
}

func TestSessionPortConflictIdleAndLimit(t *testing.T) {
	dir := t.TempDir()
	_, srv, manager := testAPI(t, dir)
	defer manager.Close()
	token := readToken(t, srv.tokens)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	req := httptest.NewRequest(http.MethodPost, "/_api/sessions", strings.NewReader(`{"port":`+strconv.Itoa(port)+`}`))
	req.Host = "127.0.0.1:8081"
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusConflict {
		t.Fatalf("busy port = %d %s", rr.Code, rr.Body.String())
	}

	now := time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)
	manager.SetNow(func() time.Time { return now })
	idleReq := httptest.NewRequest(http.MethodPost, "/_api/sessions", strings.NewReader(`{"name":"idle","idle_timeout_seconds":1}`))
	idleReq.Host = "127.0.0.1:8081"
	idleReq.Header.Set("Authorization", "Bearer "+token)
	idleReq.Header.Set("Content-Type", "application/json")
	idleRR := httptest.NewRecorder()
	srv.ServeHTTP(idleRR, idleReq)
	if idleRR.Code != http.StatusCreated {
		t.Fatalf("idle create = %d %s", idleRR.Code, idleRR.Body.String())
	}
	var created sessionResponse
	if err := json.Unmarshal(idleRR.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Second)
	manager.Sweep()
	if _, ok := manager.Get(created.ID); ok {
		t.Fatal("idle session stayed open")
	}

	for i := 0; i < sessions.MaxTestSessions; i++ {
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/_api/sessions", strings.NewReader(`{"name":"n"}`))
		req.Host = "127.0.0.1:8081"
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		srv.ServeHTTP(resp, req)
		if resp.Code != http.StatusCreated {
			t.Fatalf("create %d = %d %s", i, resp.Code, resp.Body.String())
		}
	}
	full := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/_api/sessions", strings.NewReader(`{}`))
	req.Host = "127.0.0.1:8081"
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	srv.ServeHTTP(full, req)
	if full.Code != http.StatusTooManyRequests {
		t.Fatalf("limit = %d %s", full.Code, full.Body.String())
	}
}

func testAPI(t *testing.T, dir string) (*TokenFile, *Server, *sessions.Manager) {
	t.Helper()
	tokens, err := LoadToken(filepath.Join(dir, "admin.token"))
	if err != nil {
		t.Fatal(err)
	}
	store := rules.NewMemory()
	manager := sessions.New(sessions.Options{
		Rules:        store,
		History:      history.New(20),
		ProxyAddr:    "127.0.0.1:8080",
		AdminAddr:    "127.0.0.1:8081",
		HistoryLimit: 20,
	})
	srv := New(tokens, "127.0.0.1:8081", "test")
	srv.UseSessions(manager)
	return tokens, srv, manager
}

func readToken(t *testing.T, tokens *TokenFile) string {
	t.Helper()
	raw, err := os.ReadFile(tokens.Path())
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(raw))
}

func postSession(t *testing.T, srv *Server, token, body string) sessionResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/_api/sessions", strings.NewReader(body))
	req.Host = "127.0.0.1:8081"
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", rr.Code, rr.Body.String())
	}
	var out sessionResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func httpGet(t *testing.T, rawURL string) *http.Response {
	t.Helper()
	client := &http.Client{Timeout: time.Second}
	resp, err := client.Get(rawURL)
	if err != nil {
		return &http.Response{StatusCode: 0, Body: io.NopCloser(strings.NewReader(""))}
	}
	return resp
}

func dirEntries(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}
