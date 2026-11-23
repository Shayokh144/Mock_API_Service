package proxy

import (
	"net/http"
	"strings"
)

// Every upstream shares one browser origin, such as http://10.0.2.2:8080, so
// the proxy decides where an upstream cookie lives and which upstream gets it.
//
// Path is always /. The same upstream is reached at /okya-auth/... and, via
// the Referer or sticky cookie, at /login. A cookie stored on /okya-auth was
// not sent to POST /login, so Rails lost the session holding return_to and
// sent the user to its 404 root page.
//
// An HttpOnly cookie is stored as mp_<upstream id>_<name>. Page scripts
// cannot read it, so the rename is invisible to them. On the way upstream
// only that upstream's cookies are restored, so OKYA and EasyHotel can both
// set _session without seeing each other's. A cookie scripts can read keeps
// its name, because the page may look it up, and is shared between upstreams.
const cookieNamespace = "mp_"

// Names with these prefixes need Secure, which plain HTTP cannot set.
// They are renamed when they are not namespaced.
var securePrefixes = [][2]string{
	{"__Host-", "__mp_Host-"},
	{"__Secure-", "__mp_Secure-"},
}

func namespacedName(upstreamID, name string) string {
	return cookieNamespace + upstreamID + "_" + name
}

// splitNamespaced returns the upstream id and real name of mp_<id>_<name>.
func splitNamespaced(name string) (string, string, bool) {
	if !strings.HasPrefix(name, cookieNamespace) {
		return "", "", false
	}
	rest := name[len(cookieNamespace):]
	id, real, ok := strings.Cut(rest, "_")
	if !ok || len(id) != 16 || real == "" || !isHex(id) {
		return "", "", false
	}
	return id, real, true
}

func isHex(s string) bool {
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

func rewriteSetCookies(header http.Header, upstreamID string) {
	values := header.Values("Set-Cookie")
	if len(values) == 0 {
		return
	}
	next := make([]string, len(values))
	changed := false
	for i, value := range values {
		rewritten := rewriteSetCookie(value, upstreamID)
		next[i] = rewritten
		if rewritten != value {
			changed = true
		}
	}
	if !changed {
		return
	}
	header.Del("Set-Cookie")
	for _, value := range next {
		header.Add("Set-Cookie", value)
	}
}

// rewriteSetCookie makes an upstream cookie usable on the plain HTTP proxy
// origin. A browser drops a cookie whose Domain is another host, a
// SameSite=None cookie without Secure, and a __Host- or __Secure- cookie
// without Secure. The value is not changed or logged.
func rewriteSetCookie(raw, upstreamID string) string {
	cookie, err := http.ParseSetCookie(raw)
	if err != nil || cookie.Name == "" {
		return raw
	}
	cookie.Domain = ""
	cookie.Secure = false
	cookie.Partitioned = false
	if cookie.SameSite == http.SameSiteNoneMode {
		cookie.SameSite = http.SameSiteLaxMode
	}
	cookie.Path = "/"
	switch {
	case cookie.HttpOnly && upstreamID != "":
		cookie.Name = namespacedName(upstreamID, cookie.Name)
	default:
		cookie.Name = renameSecurePrefix(cookie.Name)
	}
	serialized := cookie.String()
	if serialized == "" {
		return raw
	}
	return serialized
}

func renameSecurePrefix(name string) string {
	for _, pair := range securePrefixes {
		if len(name) >= len(pair[0]) && strings.EqualFold(name[:len(pair[0])], pair[0]) {
			return pair[1] + name[len(pair[0]):]
		}
	}
	return name
}

func restoreSecurePrefix(name string) string {
	for _, pair := range securePrefixes {
		if strings.HasPrefix(name, pair[1]) {
			return pair[0] + name[len(pair[1]):]
		}
	}
	return name
}

type cookiePair struct{ name, value string }

func parseCookieHeader(header http.Header) []cookiePair {
	var out []cookiePair
	for _, value := range header.Values("Cookie") {
		for _, part := range strings.Split(value, ";") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			name, val, _ := strings.Cut(part, "=")
			out = append(out, cookiePair{name: strings.TrimSpace(name), value: val})
		}
	}
	return out
}

// rewriteRequestCookies builds the Cookie header one upstream should see.
// Its namespaced cookies come first under their real names. Another
// upstream's namespaced cookies and the proxy cookie are dropped. A plain
// cookie is kept unless a namespaced one already has its name, so a stale
// cookie from an older proxy version cannot shadow the live session.
func rewriteRequestCookies(header http.Header, upstreamID string) {
	pairs := parseCookieHeader(header)
	if len(pairs) == 0 {
		return
	}
	var own, plain []cookiePair
	seen := map[string]bool{}
	special := false
	for _, pair := range pairs {
		if pair.name == stickyCookieName {
			special = true
			continue
		}
		if id, real, ok := splitNamespaced(pair.name); ok {
			special = true
			if id == upstreamID && !seen[real] {
				seen[real] = true
				own = append(own, cookiePair{name: real, value: pair.value})
			}
			continue
		}
		restored := restoreSecurePrefix(pair.name)
		if restored != pair.name {
			special = true
		}
		plain = append(plain, cookiePair{name: restored, value: pair.value})
	}
	if !special {
		return
	}
	parts := make([]string, 0, len(own)+len(plain))
	for _, pair := range own {
		parts = append(parts, pair.name+"="+pair.value)
	}
	for _, pair := range plain {
		if seen[pair.name] {
			continue
		}
		parts = append(parts, pair.name+"="+pair.value)
	}
	header.Del("Cookie")
	if len(parts) > 0 {
		header.Set("Cookie", strings.Join(parts, "; "))
	}
}

// cookieNames lists the cookie names in a Cookie header, without values.
func cookieNames(header http.Header) string {
	pairs := parseCookieHeader(header)
	names := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		names = append(names, pair.name)
	}
	return strings.Join(names, ", ")
}
