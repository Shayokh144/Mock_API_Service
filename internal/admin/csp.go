package admin

import (
	"net/http"
	"strings"
)

// dashboardCSP is the page policy. Styles and scripts are same-origin files.
// There is no inline script and no inline style.
func dashboardCSP(script, connect bool) string {
	parts := []string{"default-src 'none'"}
	if script {
		parts = append(parts, "script-src 'self'")
	}
	if connect {
		parts = append(parts, "connect-src 'self'")
	}
	parts = append(parts,
		"style-src 'self'",
		"form-action 'self'",
		"frame-ancestors 'none'",
		"base-uri 'none'",
	)
	return strings.Join(parts, "; ")
}

func writeHTMLHeaders(w http.ResponseWriter, script, connect bool) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "same-origin")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", dashboardCSP(script, connect))
}
