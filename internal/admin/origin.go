package admin

import (
	"net"
	"net/http"
	"net/url"
	"strings"
)

// sameOrigin allows a dashboard form post from this process.
//
// Fetch says a request whose referrer policy is no-referrer serializes Origin
// as the string "null", even for a same-origin form POST. Browsers still send
// Sec-Fetch-Site: same-origin on that navigation. Trust same-origin and none.
// A concrete Origin and the Host must both be loopback, on the same port.
// localhost, 127.0.0.1, and [::1] are the same local server. A matching
// non-loopback name is rejected, so a DNS rebinding page cannot post here.
// A missing Origin falls back to Referer. Neither header means a non-browser client.
func sameOrigin(r *http.Request) bool {
	switch fetchSite(r) {
	case "cross-site", "same-site":
		return false
	}
	if raw, ok := statedSource(r.Header.Get("Origin")); ok {
		return loopbackSamePort(raw, r.Host)
	}
	if referer := strings.TrimSpace(r.Header.Get("Referer")); referer != "" {
		return loopbackSamePort(referer, r.Host)
	}
	if strings.EqualFold(strings.TrimSpace(r.Header.Get("Origin")), "null") {
		site := fetchSite(r)
		return site == "same-origin" || site == "none"
	}
	return true
}

func fetchSite(r *http.Request) string {
	return strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site")))
}

func statedSource(origin string) (string, bool) {
	origin = strings.TrimSpace(origin)
	if origin == "" || strings.EqualFold(origin, "null") {
		return "", false
	}
	return origin, true
}

func loopbackSamePort(raw, requestHost string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" {
		return false
	}
	originHost, originPort := hostAndPort(parsed.Host, parsed.Scheme)
	reqHost, reqPort := hostAndPort(requestHost, "http")
	if originPort == "" || reqPort == "" || originPort != reqPort {
		return false
	}
	return isLoopbackHost(originHost) && isLoopbackHost(reqHost)
}

// allowedAdminHost is true when Host is localhost, 127.0.0.1, or [::1].
// A non-empty listenPort must be that port. Proxied API traffic does not use this check.
func allowedAdminHost(host, listenPort string) bool {
	name, port, err := net.SplitHostPort(strings.TrimSpace(host))
	if err != nil || port == "" || !isLoopbackHost(name) {
		return false
	}
	if listenPort != "" && port != listenPort {
		return false
	}
	return true
}

func hostAndPort(hostport, scheme string) (string, string) {
	host, port, err := net.SplitHostPort(hostport)
	if err == nil {
		return trimBrackets(host), port
	}
	host = trimBrackets(hostport)
	if host == "" || strings.Contains(host, ":") {
		return "", ""
	}
	if strings.EqualFold(scheme, "https") {
		return host, "443"
	}
	return host, "80"
}

func trimBrackets(host string) string {
	return strings.Trim(host, "[]")
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(trimBrackets(host))
	return ip != nil && ip.IsLoopback()
}
