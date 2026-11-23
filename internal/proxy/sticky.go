package proxy

import "net/http"

// stickyCookieName remembers the last upstream this browser reached. It lets a
// page served without its prefix, such as /login reached through the Referer,
// post its form to /login and load assets named by its CSS. The Referer of
// those requests is /login, which has no prefix.
const stickyCookieName = "mockproxy_upstream"

// routeVia says how an upstream was chosen.
type routeVia int

const (
	viaPrefix routeVia = iota
	viaReferer
	viaCookie
)

func (t target) cookiePrefix() string {
	if t.via != viaPrefix {
		return ""
	}
	return t.prefix
}

func stickyUpstream(r *http.Request) (string, bool) {
	cookie, err := r.Cookie(stickyCookieName)
	if err != nil || cookie.Value == "" {
		return "", false
	}
	return cookie.Value, true
}

// setStickyUpstream sets the cookie when the browser does not hold id yet.
func setStickyUpstream(w http.ResponseWriter, r *http.Request, id string) {
	if current, ok := stickyUpstream(r); ok && current == id {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     stickyCookieName,
		Value:    id,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}
