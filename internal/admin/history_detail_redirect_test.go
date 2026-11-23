package admin

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Shayokh144/Mock_API_Service/internal/history"
	"github.com/Shayokh144/Mock_API_Service/internal/rules"
)

func TestHistoryDetailShowsRedirectAndSentUpstream(t *testing.T) {
	store, err := rules.Load(filepath.Join(t.TempDir(), "rules.json"))
	if err != nil {
		t.Fatal(err)
	}
	log := history.New(10)
	entry := log.Add(history.Entry{
		StartedAt:       time.Now(),
		Method:          "POST",
		Path:            "/login",
		Status:          http.StatusSeeOther,
		Source:          history.SourceUpstream,
		RequestHeaders:  http.Header{"Cookie": {"_okya_session=secret-value"}},
		ResponseHeaders: http.Header{"Location": {"/okya-auth/dashboard"}},
		UpstreamHeaders: http.Header{"Host": {"auth.okya.example"}, "Origin": {"https://auth.okya.example"}, "Referer": {"https://auth.okya.example/login"}},
	})
	dashboard, err := New(store, log, nil)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(dashboard)
	defer srv.Close()
	page := get(t, dashboardClient(), srv.URL+"/_admin/history/"+entry.ID)
	for _, want := range []string{
		`<dt>Redirects to</dt>`,
		`<code id="detail-location">/okya-auth/dashboard</code>`,
		`id="detail-sent-upstream"`,
		"https://auth.okya.example/login",
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("detail page missing %q", want)
		}
	}
	sent := page[strings.Index(page, `id="detail-sent-upstream"`):]
	sent = sent[:strings.Index(sent, "</details>")]
	if strings.Contains(sent, "secret-value") || strings.Contains(sent, "Cookie") {
		t.Fatal("cookies must not appear under Sent upstream")
	}
	if strings.Index(page, `id="detail-sent-upstream"`) > strings.Index(page, "<summary>Request headers</summary>") {
		t.Fatal("Sent upstream should come before the request headers")
	}
}
