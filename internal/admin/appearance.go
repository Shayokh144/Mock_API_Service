package admin

import (
	"embed"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

//go:embed static/admin.css static/rule-form.js
var assetFiles embed.FS

func (s *Server) asset(w http.ResponseWriter, name, contentType string) {
	body, err := assetFiles.ReadFile(name)
	if err != nil {
		http.NotFound(w, nil)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// statusClass maps an HTTP status, or a summary such as "200 ×2", to a badge class.
// 2xx is green, 3xx is neutral, 4xx is amber, 5xx is red, and 0 or anything else is rejected.
func statusClass(value any) string {
	text := strings.TrimSpace(fmt.Sprint(value))
	end := 0
	for end < len(text) && text[end] >= '0' && text[end] <= '9' {
		end++
	}
	code := 0
	if end > 0 {
		code, _ = strconv.Atoi(text[:end])
	}
	switch {
	case code >= 200 && code < 300:
		return "badge status-2xx"
	case code >= 300 && code < 400:
		return "badge status-3xx"
	case code >= 400 && code < 500:
		return "badge status-4xx"
	case code >= 500 && code < 600:
		return "badge status-5xx"
	default:
		return "badge status-rejected"
	}
}

// sourceClass colors a history source. Mocked is blue. Passthrough is neutral.
// A rejected or fault row uses the dark rejected color. An upstream error uses red.
func sourceClass(source string) string {
	switch {
	case source == "mock":
		return "badge badge-mock"
	case strings.HasPrefix(source, "upstream-error"):
		return "badge status-5xx"
	case source == "rejected" || strings.HasPrefix(source, "fault"):
		return "badge badge-rejected"
	default:
		return "badge badge-neutral"
	}
}
