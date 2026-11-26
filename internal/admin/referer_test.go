package admin

import (
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Shayokh144/Mock_API_Service/internal/history"
	"github.com/Shayokh144/Mock_API_Service/internal/rules"
	"github.com/Shayokh144/Mock_API_Service/internal/upstreams"
)

func TestHistoryShowsRefererNote(t *testing.T) {
	dir := t.TempDir()
	store, err := rules.Load(filepath.Join(dir, "rules.json"))
	if err != nil {
		t.Fatal(err)
	}
	ups, err := upstreams.Load(filepath.Join(dir, "upstreams.json"))
	if err != nil {
		t.Fatal(err)
	}
	log := history.New(5)
	added := log.Add(history.Entry{
		StartedAt:     time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC),
		Method:        "GET",
		Path:          "/login",
		ForwardedPath: "/login",
		Status:        200,
		Source:        history.SourceUpstream,
		UpstreamID:    "0123456789abcdef",
		UpstreamName:  "SHOP_AUTH_DEV",
		ViaReferer:    true,
	})
	dashboard, err := New(store, log, ups)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(dashboard)
	defer srv.Close()
	client := dashboardClient()

	page := get(t, client, srv.URL+"/_admin/history")
	if !strings.Contains(page, "SHOP_AUTH_DEV (via Referer)") {
		t.Fatalf("list = %s", page)
	}
	detail := get(t, client, srv.URL+"/_admin/history/"+added.ID)
	if !strings.Contains(detail, "SHOP_AUTH_DEV (via Referer)") || !strings.Contains(detail, "<dt>Upstream</dt>") {
		t.Fatalf("detail = %s", detail)
	}
	feed := get(t, client, srv.URL+"/_admin/history/feed")
	if !strings.Contains(feed, `"upstream":"SHOP_AUTH_DEV (via Referer)"`) {
		t.Fatalf("feed = %s", feed)
	}
}
