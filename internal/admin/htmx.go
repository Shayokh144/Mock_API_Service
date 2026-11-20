package admin

import (
	"net/http"
	"strings"
)

func htmxRequest(r *http.Request) bool {
	return strings.EqualFold(strings.TrimSpace(r.Header.Get("HX-Request")), "true")
}

// rejectCrossOriginHTMX blocks an htmx request that fails the same-origin check.
// A normal document navigation has no HX-Request header and is left alone.
func rejectCrossOriginHTMX(w http.ResponseWriter, r *http.Request) bool {
	if r == nil || !htmxRequest(r) || sameOrigin(r) {
		return false
	}
	http.Error(w, "cross-origin form posts are not allowed", http.StatusForbidden)
	return true
}
