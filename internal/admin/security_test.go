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

func TestAdminRejectsForgedHost(t *testing.T) {
	dir := t.TempDir()
	store, err := rules.Load(filepath.Join(dir, "rules.json"))
	if err != nil {
		t.Fatal(err)
	}
	dashboard, err := New(store, history.New(5), nil)
	if err != nil {
		t.Fatal(err)
	}
	dashboard.UseListenAddr("127.0.0.1:8081")

	for _, host := range []string{
		"rebind.evil.example:8081",
		"10.0.2.2:8081",
		"127.0.0.1:8080",
		"localhost:8080",
		"evil.example",
	} {
		req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8081/_admin", nil)
		req.Host = host
		rr := httptest.NewRecorder()
		dashboard.ServeHTTP(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Fatalf("host %q status = %d", host, rr.Code)
		}
	}

	body := url.Values{
		"name": {"evil"}, "base_url": {"https://evil.example"},
	}
	req := httptest.NewRequest(http.MethodPost, "http://rebind.evil.example:8081/_admin/upstreams", strings.NewReader(body.Encode()))
	req.Host = "rebind.evil.example:8081"
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "http://rebind.evil.example:8081")
	rr := httptest.NewRecorder()
	dashboard.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("forged post status = %d", rr.Code)
	}

	for _, host := range []string{"localhost:8081", "127.0.0.1:8081", "[::1]:8081"} {
		req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8081/_admin", nil)
		req.Host = host
		rr := httptest.NewRecorder()
		dashboard.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("host %q status = %d body %s", host, rr.Code, rr.Body.String())
		}
	}
}

func TestAdminRejectsNonLoopbackOrigin(t *testing.T) {
	dir := t.TempDir()
	store, err := rules.Load(filepath.Join(dir, "rules.json"))
	if err != nil {
		t.Fatal(err)
	}
	dashboard, err := New(store, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	dashboard.UseListenAddr("127.0.0.1:8081")

	body := url.Values{
		"name": {"Nope"}, "method": {"GET"}, "path_pattern": {"/v1/nope"}, "status_code": {"200"},
	}
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8081/_admin/rules", strings.NewReader(body.Encode()))
	req.Host = "127.0.0.1:8081"
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "http://rebind.evil.example:8081")
	rr := httptest.NewRecorder()
	dashboard.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d", rr.Code)
	}
	if len(store.List()) != 0 {
		t.Fatal("non-loopback origin saved a rule")
	}
}

func TestHistoryDetailMasksSecrets(t *testing.T) {
	log := history.New(5)
	entry := log.Add(history.Entry{
		StartedAt: time.Date(2026, 11, 12, 8, 0, 0, 0, time.UTC),
		Method:    http.MethodGet,
		Path:      "/v1/me",
		Status:    http.StatusOK,
		Source:    history.SourceUpstream,
		RequestHeaders: http.Header{
			"Authorization": {"Bearer real-token"},
			"Cookie":        {"session=real-cookie"},
			"X-Request-Id":  {"abc"},
		},
		ResponseHeaders: http.Header{
			"Content-Type": {"application/json"},
		},
	})
	dashboard, err := New(rulesStore(t), log, nil)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(dashboard)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/_admin/history/" + entry.ID)
	if err != nil {
		t.Fatal(err)
	}
	body := readAll(t, resp)
	if strings.Contains(resp.Header.Get("Content-Security-Policy"), "script-src") {
		t.Fatal("detail page allows scripts")
	}
	for _, want := range []string{
		"Authorization: ••••••••",
		"Cookie: ••••••••",
		"<summary>Show value</summary>",
		"Bearer real-token",
		"session=real-cookie",
		"X-Request-Id: abc",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("detail missing %q\n%s", want, body)
		}
	}
	auth := strings.Index(body, "Authorization: ••••••••")
	show := strings.Index(body, "<summary>Show value</summary>")
	token := strings.Index(body, "Bearer real-token")
	if auth < 0 || show < auth || token < show {
		t.Fatalf("secret value is visible before the reveal control:\n%s", body)
	}
	plain := strings.Index(body, "<code>X-Request-Id: abc</code>")
	if plain < 0 || strings.Contains(body[plain:plain+len("<code>X-Request-Id: abc</code>")+20], "Show value") {
		t.Fatal("ordinary header is masked")
	}
}

func rulesStore(t *testing.T) *rules.Store {
	t.Helper()
	store, err := rules.Load(filepath.Join(t.TempDir(), "rules.json"))
	if err != nil {
		t.Fatal(err)
	}
	return store
}
