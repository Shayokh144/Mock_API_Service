package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTokenFilePermissionsAndReuse(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	path := filepath.Join(dir, "admin.token")
	first, err := LoadToken(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %o", info.Mode().Perm())
	}
	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode = %o", dirInfo.Mode().Perm())
	}
	second, err := LoadToken(path)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	token := strings.TrimSpace(string(raw))
	if !first.Match(token) || !second.Match(token) {
		t.Fatal("reloaded token did not match")
	}
	if strings.Contains(path, token) {
		t.Fatal("token leaked into the path")
	}
}

func TestAPIAuthHostOriginAndJSON(t *testing.T) {
	dir := t.TempDir()
	tokens, err := LoadToken(filepath.Join(dir, "admin.token"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(tokens.Path())
	if err != nil {
		t.Fatal(err)
	}
	token := strings.TrimSpace(string(raw))
	srv := New(tokens, "127.0.0.1:8081", "test")

	missing := call(t, srv, http.MethodGet, "/_api/health", "127.0.0.1:8081", "", nil)
	if missing.Code != http.StatusUnauthorized || !strings.Contains(missing.Body.String(), `"code":"unauthorized"`) {
		t.Fatalf("missing token = %d %s", missing.Code, missing.Body.String())
	}
	wrong := call(t, srv, http.MethodGet, "/_api/health", "127.0.0.1:8081", "Bearer nope", nil)
	if wrong.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token = %d", wrong.Code)
	}

	forged := call(t, srv, http.MethodGet, "/_api/health", "rebind.evil.example:8081", "Bearer "+token, nil)
	if forged.Code != http.StatusForbidden {
		t.Fatalf("forged host = %d %s", forged.Code, forged.Body.String())
	}
	wrongPort := call(t, srv, http.MethodGet, "/_api/health", "127.0.0.1:8080", "Bearer "+token, nil)
	if wrongPort.Code != http.StatusForbidden {
		t.Fatalf("wrong port = %d", wrongPort.Code)
	}

	origin := httptest.NewRequest(http.MethodGet, "/_api/health", nil)
	origin.Host = "127.0.0.1:8081"
	origin.Header.Set("Authorization", "Bearer "+token)
	origin.Header.Set("Origin", "http://127.0.0.1:8081")
	originRR := httptest.NewRecorder()
	srv.ServeHTTP(originRR, origin)
	if originRR.Code != http.StatusForbidden || originRR.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("origin = %d cors %q", originRR.Code, originRR.Header().Get("Access-Control-Allow-Origin"))
	}

	options := call(t, srv, http.MethodOptions, "/_api/health", "127.0.0.1:8081", "Bearer "+token, nil)
	if options.Code != http.StatusForbidden {
		t.Fatalf("options = %d", options.Code)
	}

	plainReq := httptest.NewRequest(http.MethodPost, "/_api/token/rotate", strings.NewReader("hello"))
	plainReq.Host = "127.0.0.1:8081"
	plainReq.Header.Set("Authorization", "Bearer "+token)
	plainReq.Header.Set("Content-Type", "text/plain")
	plainRR := httptest.NewRecorder()
	srv.ServeHTTP(plainRR, plainReq)
	if plainRR.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("content type = %d %s", plainRR.Code, plainRR.Body.String())
	}

	unknown := httptest.NewRequest(http.MethodPost, "/_api/token/rotate", strings.NewReader(`{"extra":true}`))
	unknown.Host = "127.0.0.1:8081"
	unknown.Header.Set("Authorization", "Bearer "+token)
	unknown.Header.Set("Content-Type", "application/json")
	unknownRR := httptest.NewRecorder()
	srv.ServeHTTP(unknownRR, unknown)
	if unknownRR.Code != http.StatusBadRequest || !strings.Contains(unknownRR.Body.String(), "unknown field") {
		t.Fatalf("unknown = %d %s", unknownRR.Code, unknownRR.Body.String())
	}
	if tokens.Match(token) == false {
		t.Fatal("rejected body rotated the token")
	}

	big := bytes.Repeat([]byte("a"), MaxBody+8)
	bigReq := httptest.NewRequest(http.MethodPost, "/_api/token/rotate", bytes.NewReader(big))
	bigReq.Host = "127.0.0.1:8081"
	bigReq.Header.Set("Authorization", "Bearer "+token)
	bigReq.Header.Set("Content-Type", "application/json")
	bigRR := httptest.NewRecorder()
	srv.ServeHTTP(bigRR, bigReq)
	if bigRR.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("large = %d %s", bigRR.Code, bigRR.Body.String())
	}

	ok := call(t, srv, http.MethodGet, "/_api/health", "localhost:8081", "Bearer "+token, nil)
	if ok.Code != http.StatusOK {
		t.Fatalf("health = %d %s", ok.Code, ok.Body.String())
	}
	var health struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(ok.Body.Bytes(), &health); err != nil {
		t.Fatal(err)
	}
	if health.Status != "ok" || health.Version != "test" {
		t.Fatalf("health = %+v", health)
	}
	if strings.Contains(ok.Body.String(), token) {
		t.Fatal("health echoed the token")
	}

	rotated := call(t, srv, http.MethodPost, "/_api/token/rotate", "[::1]:8081", "Bearer "+token, nil)
	if rotated.Code != http.StatusNoContent {
		t.Fatalf("rotate = %d %s", rotated.Code, rotated.Body.String())
	}
	stale := call(t, srv, http.MethodGet, "/_api/health", "127.0.0.1:8081", "Bearer "+token, nil)
	if stale.Code != http.StatusUnauthorized {
		t.Fatalf("old token = %d", stale.Code)
	}
	nextRaw, err := os.ReadFile(tokens.Path())
	if err != nil {
		t.Fatal(err)
	}
	next := strings.TrimSpace(string(nextRaw))
	if next == token {
		t.Fatal("token file did not change")
	}
	fresh := call(t, srv, http.MethodGet, "/_api/health", "127.0.0.1:8081", "Bearer "+next, nil)
	if fresh.Code != http.StatusOK {
		t.Fatalf("new token = %d %s", fresh.Code, fresh.Body.String())
	}
}

func call(t *testing.T, srv http.Handler, method, path, host, auth string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, body)
	req.Host = host
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	return rr
}
