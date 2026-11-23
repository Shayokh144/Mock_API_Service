package admin

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Shayokh144/Mock_API_Service/internal/sessions"
)

const themeCookie = "mock_theme"

func (s *Server) setTheme(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		http.Error(w, "cross-origin form posts are not allowed", http.StatusForbidden)
		return
	}
	theme := r.FormValue("theme")
	switch theme {
	case "light", "dark", "system":
	default:
		theme = "system"
	}
	http.SetCookie(w, &http.Cookie{
		Name:     themeCookie,
		Value:    theme,
		Path:     "/_admin",
		MaxAge:   int((365 * 24 * time.Hour).Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	http.Redirect(w, r, safeAdminPath(r.FormValue("next")), http.StatusSeeOther)
}

func themeFrom(r *http.Request) string {
	if r == nil {
		return "system"
	}
	cookie, err := r.Cookie(themeCookie)
	if err != nil {
		return "system"
	}
	switch cookie.Value {
	case "light", "dark", "system":
		return cookie.Value
	default:
		return "system"
	}
}

func nextFrom(r *http.Request) string {
	if r == nil || r.URL == nil {
		return "/_admin"
	}
	return safeAdminPath(r.URL.RequestURI())
}

func currentSessionName(choices []sessionChoice) string {
	for _, item := range choices {
		if item.Selected && item.Name != "" {
			return item.Name
		}
	}
	return "default"
}

func sessionQuery(id string) string {
	if id == "" || id == sessions.DefaultID {
		return ""
	}
	return "?session=" + url.QueryEscape(id)
}

func navFrom(path string) string {
	switch {
	case strings.HasPrefix(path, "/_admin/upstreams"):
		return "upstreams"
	case strings.HasPrefix(path, "/_admin/history"):
		return "history"
	case strings.HasPrefix(path, "/_admin/sessions"):
		return "sessions"
	default:
		return "rules"
	}
}

// safeAdminPath keeps the theme redirect on this dashboard.
// An open redirect, a scheme, or a different path prefix falls back to /_admin.
func safeAdminPath(raw string) string {
	if raw == "/_admin" || strings.HasPrefix(raw, "/_admin/") || strings.HasPrefix(raw, "/_admin?") {
		if strings.Contains(raw, "://") || strings.Contains(raw, "//") || strings.ContainsAny(raw, "\\\r\n\x00 <>\"'") {
			return "/_admin"
		}
		return raw
	}
	return "/_admin"
}
