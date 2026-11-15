package admin

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Shayokh144/Mock_API_Service/internal/history"
	"github.com/Shayokh144/Mock_API_Service/internal/rules"
	"github.com/Shayokh144/Mock_API_Service/internal/sessions"
)

func TestDashboardSessionPickerIsReadOnly(t *testing.T) {
	dir := t.TempDir()
	store, err := rules.Load(filepath.Join(dir, "rules.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(rules.Draft{
		Name: "Developer", Enabled: true, Method: "GET", PathPattern: "/v1/dev", StatusCode: 200,
	}); err != nil {
		t.Fatal(err)
	}
	log := history.New(10)
	dashboard, err := New(store, log, nil)
	if err != nil {
		t.Fatal(err)
	}
	manager := sessions.New(sessions.Options{
		Rules: store, History: log, ProxyAddr: "127.0.0.1:8080", AdminAddr: "127.0.0.1:8081", HistoryLimit: 10,
	})
	defer manager.Close()
	dashboard.UseSessions(manager)
	info, err := manager.Create("login-tests", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := info.Rules.Create(rules.Draft{
		Name: "Only test", Enabled: true, Method: "GET", PathPattern: "/v1/test", StatusCode: 200,
	}); err != nil {
		t.Fatal(err)
	}
	info.History.Add(history.Entry{Method: "GET", Path: "/v1/test", Status: 200, Source: history.SourceMock})

	srv := httptest.NewServer(dashboard)
	defer srv.Close()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	page := get(t, client, srv.URL+"/_admin?session="+info.ID)
	if !strings.Contains(page, "Only test") || !strings.Contains(page, "read-only") || !strings.Contains(page, "login-tests") {
		t.Fatalf("session rules page = %s", page)
	}
	if strings.Contains(page, "Developer") || strings.Contains(page, "New rule") || strings.Contains(page, "Delete") {
		t.Fatalf("read-only page still offers edits:\n%s", page)
	}
	home := get(t, client, srv.URL+"/_admin")
	if !strings.Contains(home, "Developer") || !strings.Contains(home, "New rule") {
		t.Fatalf("default page = %s", home)
	}
	if strings.Contains(home, "Only test") {
		t.Fatal("default page showed the test rule")
	}
	hist := get(t, client, srv.URL+"/_admin/history?session="+info.ID)
	if !strings.Contains(hist, "/v1/test") || strings.Contains(hist, "Clear all history") {
		t.Fatalf("session history = %s", hist)
	}
}
