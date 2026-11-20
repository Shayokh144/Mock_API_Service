package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDashboardStylesAndCSP(t *testing.T) {
	dashboard, err := New(rulesStore(t), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(dashboard)
	defer srv.Close()

	pageResp, err := http.Get(srv.URL + "/_admin")
	if err != nil {
		t.Fatal(err)
	}
	page := readAll(t, pageResp)
	csp := pageResp.Header.Get("Content-Security-Policy")
	for _, want := range []string{
		"default-src 'none'",
		"style-src 'self'",
		"form-action 'self'",
		"frame-ancestors 'none'",
		"base-uri 'none'",
	} {
		if !strings.Contains(csp, want) {
			t.Fatalf("csp = %q, missing %s", csp, want)
		}
	}
	if strings.Contains(csp, "unsafe-inline") || strings.Contains(csp, "script-src") {
		t.Fatalf("rules page csp = %q", csp)
	}
	if strings.Contains(page, "<style") || strings.Contains(page, "style=") {
		t.Fatal("page still has inline style")
	}
	if !strings.Contains(page, `<link rel="stylesheet" href="/_admin/admin.css">`) {
		t.Fatal("stylesheet link missing")
	}

	cssResp, err := http.Get(srv.URL + "/_admin/admin.css")
	if err != nil {
		t.Fatal(err)
	}
	css := readAll(t, cssResp)
	if cssResp.Header.Get("Content-Type") != "text/css; charset=utf-8" {
		t.Fatalf("css type = %s", cssResp.Header.Get("Content-Type"))
	}
	for _, want := range []string{
		"--color-bg:",
		"--font-mono:",
		".status-2xx",
		".status-4xx",
		".badge-mock",
		".badge-rejected",
		":focus-visible",
		`prefers-color-scheme: dark`,
		`[data-theme="dark"]`,
		`[data-theme="light"]`,
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("css missing %s", want)
		}
	}
	if strings.Contains(css, "http://") || strings.Contains(css, "https://") || strings.Contains(css, "@import") {
		t.Fatal("css references a remote asset")
	}

	scriptResp, err := http.Get(srv.URL + "/_admin/rule-form.js")
	if err != nil {
		t.Fatal(err)
	}
	script := readAll(t, scriptResp)
	if !strings.Contains(script, `addEventListener("change", apply)`) || strings.Contains(script, "innerHTML") {
		t.Fatal("rule form script lost the mode toggle")
	}
}

func TestStatusAndSourceClasses(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"200", "badge status-2xx"},
		{"204", "badge status-2xx"},
		{"302", "badge status-3xx"},
		{"404", "badge status-4xx"},
		{"500", "badge status-5xx"},
		{"0", "badge status-rejected"},
		{"200 ×2", "badge status-2xx"},
	}
	for _, tc := range cases {
		if got := statusClass(tc.in); got != tc.want {
			t.Fatalf("statusClass(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	sources := []struct {
		in, want string
	}{
		{"mock", "badge badge-mock"},
		{"upstream", "badge badge-neutral"},
		{"upstream (fresh rule)", "badge badge-neutral"},
		{"upstream-error", "badge status-5xx"},
		{"rejected", "badge badge-rejected"},
		{"fault", "badge badge-rejected"},
	}
	for _, tc := range sources {
		if got := sourceClass(tc.in); got != tc.want {
			t.Fatalf("sourceClass(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
