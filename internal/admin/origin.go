package admin

import (
	"net"
	"net/http"
	"net/url"
	"strings"
)

// sameOrigin allows a dashboard form post from this process.
// Browsers may name the loopback host as localhost, 127.0.0.1, or [::1]
// while the Host header uses a different one of those names. The port must match.
// A foreign origin is rejected. A missing Origin falls back to Referer.
// A request with neither header is allowed so non-browser clients still work.
func sameOrigin(r *http.Request) bool {
	if raw, ok := statedSource(r.Header.Get("Origin")); ok {
		return loopbackSamePort(raw, r.Host)
	}
	if referer := strings.TrimSpace(r.Header.Get("Referer")); referer != "" {
		return loopbackSamePort(referer, r.Host)
	}
	return true
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
	if strings.EqualFold(originHost, reqHost) {
		return true
	}
	return isLoopbackHost(originHost) && isLoopbackHost(reqHost)
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
