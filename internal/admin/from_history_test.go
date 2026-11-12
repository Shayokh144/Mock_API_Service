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
	"github.com/Shayokh144/Mock_API_Service/internal/upstreams"
)

func TestCreateRuleFromHistory(t *testing.T) {
	dir := t.TempDir()
	store, err := rules.Load(filepath.Join(dir, "rules.json"))
	if err != nil {
		t.Fatal(err)
	}
	ups, err := upstreams.Load(filepath.Join(dir, "upstreams.json"))
	if err != nil {
		t.Fatal(err)
	}
	okya, err := ups.Create("OKYA", "http://127.0.0.1:9", "/okya")
	if err != nil {
		t.Fatal(err)
	}
	log := history.New(10)
	start := time.Date(2026, 11, 11, 8, 0, 0, 0, time.UTC)
	entry := log.Add(history.Entry{
		StartedAt:       start,
		Method:          http.MethodGet,
		Path:            "/okya/api/user/me",
		RawQuery:        "page=2&name=A%20B&flag",
		ForwardedPath:   "/api/user/me",
		Status:          http.StatusOK,
		Source:          history.SourceUpstream,
		UpstreamID:      okya.ID,
		UpstreamName:    okya.Name,
		ResponseDecoded: true,
		ResponsePlain:   `{"ok":true}`,
		ResponseBody:    "{\n  \"ok\": true\n}",
		ResponseHeaders: http.Header{
			"Content-Type":      {"application/json"},
			"X-Extra":           {"yes"},
			"Content-Length":    {"99"},
			"Transfer-Encoding": {"chunked"},
			"Content-Encoding":  {"gzip"},
			"Connection":        {"close"},
			"Set-Cookie":        {"session=real-token; HttpOnly"},
		},
	})
	failed := log.Add(history.Entry{
		StartedAt:    start.Add(time.Second),
		Method:       http.MethodPost,
		Path:         "/okya/v1/down",
		Status:       http.StatusBadGateway,
		Source:       history.SourceUpstreamError,
		UpstreamID:   okya.ID,
		UpstreamName: okya.Name,
		ResponseBody: `{"error":"upstream unavailable"}`,
	})
	gone := log.Add(history.Entry{
		StartedAt:     start.Add(2 * time.Second),
		Method:        http.MethodGet,
		Path:          "/v1/old",
		ForwardedPath: "/v1/old",
		Status:        http.StatusOK,
		Source:        history.SourceMock,
		UpstreamID:    "aaaaaaaaaaaaaaaa",
		UpstreamName:  "retired",
		ResponsePlain: `{"n":1}`,
	})

	dashboard, err := New(store, log, ups)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(dashboard)
	defer srv.Close()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	list := get(t, client, srv.URL+"/_admin/history")
	assertContains(t, list, `/_admin/rules/new?from=`+entry.ID)
	assertContains(t, list, "Create rule")
	detail := get(t, client, srv.URL+"/_admin/history/"+entry.ID)
	assertContains(t, detail, `/_admin/rules/new?from=`+entry.ID)

	form := get(t, client, srv.URL+"/_admin/rules/new?from="+entry.ID)
	for _, want := range []string{
		`value="GET /api/user/me"`,
		`value="/api/user/me"`,
		`value="` + okya.ID + `" selected`,
		`checked`,
		`value="0"`,
		`class="mode-fields"`,
		"page=2",
		"name=A B",
		"flag",
		"X-Extra: yes",
		"Content-Type: application/json",
		`HTTP/1.1 200 OK`,
		`&#34;ok&#34;:true`,
		"Replace real tokens, cookies, and passwords with dummy values before saving.",
	} {
		if !strings.Contains(form, want) {
			t.Fatalf("form missing %q\n%s", want, form)
		}
	}
	for _, absent := range []string{
		"/okya/api/user/me",
		`&#34;ok&#34;: true`,
		"Content-Length: 99",
		"Transfer-Encoding: chunked",
		"Content-Encoding: gzip",
		"Connection: close",
		"Set-Cookie",
		"session=real-token",
	} {
		if strings.Contains(form, absent) {
			t.Fatalf("form kept %q\n%s", absent, form)
		}
	}

	requestOnly := get(t, client, srv.URL+"/_admin/rules/new?from="+failed.ID)
	assertContains(t, requestOnly, `value="POST /v1/down"`)
	assertContains(t, requestOnly, `value="/v1/down"`)
	if strings.Contains(requestOnly, "upstream unavailable") || strings.Contains(requestOnly, "HTTP/1.1 502") {
		t.Fatalf("error row filled a response:\n%s", requestOnly)
	}

	retired := get(t, client, srv.URL+"/_admin/rules/new?from="+gone.ID)
	assertContains(t, retired, `value="" selected>Any upstream`)
	if strings.Contains(retired, "aaaaaaaaaaaaaaaa") {
		t.Fatalf("deleted upstream was kept:\n%s", retired)
	}

	missing := get(t, client, srv.URL+"/_admin/rules/new?from=0123456789abcdef")
	assertContains(t, missing, "That history entry is no longer available. The form is empty.")
	assertContains(t, missing, `value=""`)
	if strings.Contains(missing, "/api/user/me") || strings.Contains(missing, "Replace real tokens") {
		t.Fatalf("missing id kept another entry:\n%s", missing)
	}

	if len(store.List()) != 0 {
		t.Fatal("opening the form saved a rule")
	}

	script := get(t, client, srv.URL+"/_admin/history.js")
	assertContains(t, script, `/_admin/rules/new?from=`)
}
