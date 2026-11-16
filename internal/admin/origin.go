package admin

import (
	"net"
	"net/http"
	"net/url"
	"strings"
)

// sameOrigin allows a state-changing dashboard request from a browser on this
// admin origin. Scripts change state through /_api with the bearer token.
//
// Accept Sec-Fetch-Site: same-origin. A concrete Origin must be loopback, on
// the same port as Host. localhost, 127.0.0.1, and [::1] are the same local
// server. A matching non-loopback name is rejected, so a DNS rebinding page
// cannot post here. A request with neither a same-origin fetch site nor a
// loopback Origin is rejected, including a curl-style request that sends no
// Origin and no Sec-Fetch-Site. Referer alone is not enough.
func sameOrigin(r *http.Request) bool {
	switch fetchSite(r) {
	case "cross-site", "same-site":
		return false
	}
	if raw, ok := statedSource(r.Header.Get("Origin")); ok {
		return loopbackSamePort(raw, r.Host)
	}
	return fetchSite(r) == "same-origin"
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

// AllowedHost reports whether Host is localhost, 127.0.0.1, or [::1] on the admin port.
func AllowedHost(host, listenPort string) bool {
	return allowedAdminHost(host, listenPort)
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
