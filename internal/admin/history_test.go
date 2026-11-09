package admin

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Shayokh144/Mock_API_Service/internal/history"
	"github.com/Shayokh144/Mock_API_Service/internal/rules"
)

func TestHistoryDashboard(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.json")
	store, err := rules.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(rules.Draft{
		Name:        "Keep me",
		Enabled:     true,
		Method:      "GET",
		PathPattern: "/v1/keep",
		StatusCode:  200,
		Body:        `{"ok":true}`,
	}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	log := history.New(10)
	older := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	newer := older.Add(time.Minute)
	log.Add(history.Entry{
		StartedAt:       older,
		Method:          http.MethodGet,
		Path:            "/v1/items",
		RawQuery:        "page=2",
		Status:          200,
		Source:          history.SourceUpstream,
		RequestHeaders:  http.Header{"X-Request-Id": []string{"abc"}},
		RequestBody:     `{"q":1}`,
		ResponseHeaders: http.Header{"X-Upstream": []string{"yes"}},
		ResponseBody:    `{"from":"upstream"}`,
	})
	latest := log.Add(history.Entry{
		StartedAt:             newer,
		Method:                http.MethodPost,
		Path:                  "/v1/fail",
		Status:                500,
		Source:                history.SourceMock,
		RuleID:                "0123456789abcdef",
		ResponseBody:          `{"error":"boom"}`,
		ResponseBodyTruncated: true,
	})

	dashboard, err := New(store, log, nil)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(dashboard)
	defer srv.Close()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	policyResp, err := client.Get(srv.URL + "/_admin/history")
	if err != nil {
		t.Fatal(err)
	}
	policyResp.Body.Close()
	if got := policyResp.Header.Get("Referrer-Policy"); got != "same-origin" {
		t.Fatalf("Referrer-Policy = %q", got)
	}

	page := get(t, client, srv.URL+"/_admin/history")
	if !strings.Contains(page, "Rules are kept") {
		t.Fatal("list does not say rules are kept")
	}
	newestAt := strings.Index(page, "/v1/fail")
	olderAt := strings.Index(page, "/v1/items?page=2")
	if newestAt < 0 || olderAt < 0 || newestAt > olderAt {
		t.Fatalf("order is wrong: %s", page)
	}
	shownAt := templateText(formatHistoryTime(newer))
	for _, want := range []string{"POST", "500", "mock", shownAt} {
		if !strings.Contains(page, want) {
			t.Fatalf("list missing %s\n%s", want, page)
		}
	}

	detail := get(t, client, srv.URL+"/_admin/history/"+latest.ID)
	for _, want := range []string{"Rules are kept", "0123456789abcdef", "&#34;error&#34;:&#34;boom&#34;", "Response body preview was truncated", shownAt} {
		if !strings.Contains(detail, want) {
			t.Fatalf("detail missing %s\n%s", want, detail)
		}
	}
	olderEntry := log.List()[1]
	inspected := get(t, client, srv.URL+"/_admin/history/"+olderEntry.ID)
	if !strings.Contains(inspected, "X-Request-Id: abc") || !strings.Contains(inspected, "&#34;from&#34;:&#34;upstream&#34;") || !strings.Contains(inspected, "&#34;q&#34;:1") {
		t.Fatalf("detail headers or bodies missing: %s", inspected)
	}

	missing, err := client.Get(srv.URL + "/_admin/history/0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	missing.Body.Close()
	if missing.StatusCode != http.StatusNotFound {
		t.Fatalf("missing status = %d", missing.StatusCode)
	}

	confirm := get(t, client, srv.URL+"/_admin/history/clear")
	if !strings.Contains(confirm, "Rules are kept") || !strings.Contains(confirm, "Clear all history") {
		t.Fatalf("confirm = %s", confirm)
	}

	deleted, err := client.PostForm(srv.URL+"/_admin/history/"+latest.ID+"/delete", nil)
	if err != nil {
		t.Fatal(err)
	}
	deleted.Body.Close()
	if deleted.StatusCode != http.StatusSeeOther {
		t.Fatalf("delete status = %d", deleted.StatusCode)
	}
	if _, ok := log.Get(latest.ID); ok {
		t.Fatal("entry still present")
	}
	if len(log.List()) != 1 {
		t.Fatalf("remaining = %d", len(log.List()))
	}

	cleared, err := client.PostForm(srv.URL+"/_admin/history/clear", nil)
	if err != nil {
		t.Fatal(err)
	}
	cleared.Body.Close()
	if len(log.List()) != 0 {
		t.Fatal("clear left history")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("history deletion changed the rules file")
	}
	rulesPage := get(t, client, srv.URL+"/_admin")
	if !strings.Contains(rulesPage, "Keep me") {
		t.Fatal("rules list changed")
	}
}

// templateText matches html/template text escaping. A positive offset contains '+',
// which the template writes as &#43;.
func templateText(s string) string {
	return strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&#34;",
		"'", "&#39;",
		"+", "&#43;",
	).Replace(s)
}
